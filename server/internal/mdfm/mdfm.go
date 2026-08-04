// Package mdfm gives the server a minimal, format-preserving view of markdown
// frontmatter. The SPA owns document modelling; this package exists for the
// speccy write tools, whose saves must (1) never land a document with broken
// frontmatter and (2) maintain the created/updated dates the same way the
// editor does (web/src/lib/frontmatter.ts touchUpdated).
package mdfm

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Split separates a leading frontmatter block from the body. Mirrors the
// SPA's stripFrontmatter: `---\n<fm>\n---\n` with one newline consumed after
// the closing fence, so Join(Split(raw)) is byte-identical.
func Split(content string) (fm, body string, has bool) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content, false
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", content, false
	}
	fm = rest[:end]
	body = rest[end+4:]
	body = strings.TrimPrefix(body, "\n")
	return fm, body, true
}

// Join reassembles a document split by Split.
func Join(fm, body string) string {
	if fm == "" {
		return body
	}
	return "---\n" + fm + "\n---\n" + body
}

// Validate rejects content whose frontmatter block is not a parseable YAML
// mapping. Documents without frontmatter pass — only present-but-broken
// frontmatter is an error.
func Validate(content string) error {
	fm, _, has := Split(content)
	if !has {
		if strings.HasPrefix(content, "---\n") {
			return fmt.Errorf("frontmatter fence opened but never closed")
		}
		return nil
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(fm), &v); err != nil {
		return fmt.Errorf("frontmatter does not parse as YAML: %w", err)
	}
	return nil
}

// Touch sets `updated:` to now's date (and adds `created:` when isNew and
// absent), preserving key order and comments via a yaml.v3 node round-trip.
// Content without frontmatter is returned unchanged — Touch maintains dates,
// it never invents a frontmatter block.
//
// The date is taken in UTC: what lands in git must not depend on the server's
// timezone (or the browser's — web/src/lib/frontmatter.ts todayStr does the
// same), or the same edit would carry a different date for different people
// and diff against itself around midnight.
func Touch(content string, isNew bool, now time.Time) (string, error) {
	fm, body, has := Split(content)
	if !has {
		return content, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return "", fmt.Errorf("frontmatter does not parse as YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return content, nil // scalar/list frontmatter — nothing to maintain
	}
	m := doc.Content[0]
	today := now.UTC().Format("2006-01-02")
	if isNew {
		setKey(m, "created", today, false)
	}
	setKey(m, "updated", today, true)

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return Join(strings.TrimSuffix(b.String(), "\n"), body), nil
}

// SetScalar sets (or adds) a scalar frontmatter key, preserving key order and
// comments. Returns the content unchanged when there is no frontmatter block.
func SetScalar(content, key, value string) (string, error) {
	fm, body, has := Split(content)
	if !has {
		return content, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return "", fmt.Errorf("frontmatter does not parse as YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return content, nil
	}
	setKey(doc.Content[0], key, value, true)
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return Join(strings.TrimSuffix(b.String(), "\n"), body), nil
}

// AppendListItem adds value to the list under key in the frontmatter,
// creating the key when absent and converting a scalar value into a list.
// Returns added=false (content unchanged) when the value is already present
// or the document has no frontmatter block to write into.
func AppendListItem(content, key, value string) (out string, added bool, err error) {
	fm, body, has := Split(content)
	if !has {
		return content, false, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return "", false, fmt.Errorf("frontmatter does not parse as YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return content, false, nil
	}
	m := doc.Content[0]
	item := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	var list *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		v := m.Content[i+1]
		switch v.Kind {
		case yaml.SequenceNode:
			list = v
		default:
			// scalar (or empty) value → promote to a list keeping the old entry
			seq := &yaml.Node{Kind: yaml.SequenceNode}
			if v.Kind == yaml.ScalarNode && v.Value != "" {
				seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v.Value})
			}
			*v = *seq
			list = v
		}
		break
	}
	if list == nil {
		k := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		list = &yaml.Node{Kind: yaml.SequenceNode}
		m.Content = append(m.Content, k, list)
	}
	for _, n := range list.Content {
		if n.Value == value {
			return content, false, nil
		}
	}
	list.Content = append(list.Content, item)

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", false, err
	}
	if err := enc.Close(); err != nil {
		return "", false, err
	}
	return Join(strings.TrimSuffix(b.String(), "\n"), body), true, nil
}

// setKey updates the value of key in a mapping node, appending it when
// absent; overwrite=false keeps an existing value (created is set once).
func setKey(m *yaml.Node, key, value string, overwrite bool) {
	// empty tag + plain style: the encoder emits a bare `2026-07-29` (the
	// workspace convention) instead of quoting a !!str against the timestamp
	// resolver
	plain := func(n *yaml.Node, v string) {
		n.Kind, n.Tag, n.Style, n.Value = yaml.ScalarNode, "", 0, v
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if overwrite {
				plain(m.Content[i+1], value)
			}
			return
		}
	}
	k, v := &yaml.Node{}, &yaml.Node{}
	plain(k, key)
	plain(v, value)
	m.Content = append(m.Content, k, v)
}
