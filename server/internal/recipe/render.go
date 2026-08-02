package recipe

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Render expands a recipe's prompt template: {{name}} substitutions and
// {{#name}}…{{/name}} conditional sections.
//
// A conditional is kept only when its name has a non-empty value, and it
// carries its OWN whitespace — that is what lets a recipe reproduce a prompt
// whose optional blocks each have their own spacing, without the runner
// owning any of the prose.
//
// An UNKNOWN placeholder is left verbatim on purpose: prompt bodies are prose
// that talks about JSON, and a stage that shows the model `{"findings": [...]}`
// must not have its braces eaten. Only names the runner actually supplies are
// replaced, so a typo shows up in the prompt (visible, debuggable) instead of
// silently blanking a line.
func Render(text string, vars map[string]string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	text = renderConditionals(text, vars)
	var b strings.Builder
	for {
		i := strings.Index(text, "{{")
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		j := strings.Index(text[i:], "}}")
		if j < 0 {
			b.WriteString(text)
			return b.String()
		}
		j += i
		b.WriteString(text[:i])
		name := strings.TrimSpace(text[i+2 : j])
		if v, ok := vars[name]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(text[i : j+2]) // leave it alone
		}
		text = text[j+2:]
	}
}

// renderConditionals resolves {{#name}}…{{/name}} blocks innermost-last: it
// scans for an opening marker and its matching close, keeps or drops the span,
// and continues AFTER the substituted text so a dropped block cannot leave a
// half-open marker behind. An unmatched opener is left verbatim, like an
// unknown placeholder.
func renderConditionals(text string, vars map[string]string) string {
	var b strings.Builder
	for {
		i, negated := strings.Index(text, "{{#"), false
		if inv := strings.Index(text, "{{^"); inv >= 0 && (i < 0 || inv < i) {
			i, negated = inv, true
		}
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		j := strings.Index(text[i:], "}}")
		if j < 0 {
			b.WriteString(text)
			return b.String()
		}
		j += i
		name := strings.TrimSpace(text[i+3 : j])
		closer := "{{/" + name + "}}"
		k := strings.Index(text[j:], closer)
		if name == "" || k < 0 {
			b.WriteString(text[:j+2])
			text = text[j+2:]
			continue
		}
		k += j
		b.WriteString(text[:i])
		if (vars[name] != "") != negated {
			b.WriteString(text[j+2 : k])
		}
		text = text[k+len(closer):]
	}
}

// Placeholders lists the {{names}} a text uses, sorted. The dry-run endpoint
// reports the ones the runner will not supply, which is the cheapest way to
// catch a recipe that quietly renders half a prompt.
func Placeholders(text string) []string {
	seen := map[string]bool{}
	for {
		i := strings.Index(text, "{{")
		if i < 0 {
			break
		}
		j := strings.Index(text[i:], "}}")
		if j < 0 {
			break
		}
		j += i
		// a conditional's open/close markers name the same variable
		n := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(text[i+2:j]), "#/^"))
		if n != "" {
			seen[n] = true
		}
		text = text[j+2:]
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ItemVars flattens one upstream item into {{item.<field>}} substitutions,
// plus {{item}} for the whole thing as JSON. Scalars render plainly, lists of
// scalars as ", "-joined text (a stage prompt saying "read {{item.paths}}"
// wants a readable list, not a JSON array), anything else as compact JSON.
func ItemVars(item map[string]any) map[string]string {
	vars := map[string]string{}
	for k, v := range item {
		vars["item."+k] = scalarish(v)
	}
	if raw, err := json.Marshal(item); err == nil {
		vars["item"] = string(raw)
	}
	return vars
}

func scalarish(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	case float64:
		// JSON numbers decode as float64; render whole ones without the .0
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case []any:
		parts := make([]string, 0, len(t))
		allScalar := true
		for _, e := range t {
			switch e.(type) {
			case string, float64, bool, nil:
				parts = append(parts, scalarish(e))
			default:
				allScalar = false
			}
		}
		if allScalar {
			return strings.Join(parts, ", ")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}
