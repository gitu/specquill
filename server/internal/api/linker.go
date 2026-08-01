package api

// The linker proposes missing typed frontmatter links between workspace
// documents, following the workspace's configured link types (WHY ← WHAT ←
// HOW ← WHEN). Proposals are ephemeral — the client reviews them and applies
// the accepted ones as normal uncommitted worktree saves. The same link
// machinery feeds the drift engine: a document under audit is checked with
// its linked documents inlined as context.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/ai"
	"specquill/server/internal/mdfm"
	"specquill/server/internal/project"
)

// workspaceEntity is one document family's effective configuration.
type workspaceEntity struct {
	Kind   string // regulation | change | requirement | spec | work_item | …
	Folder string // "changes/" — the default location for new documents
	Group  string // why | what | how | when
}

// workspaceLink is one configured link type: the field the LOWER document
// carries to point UP at the level it exists for.
type workspaceLink struct {
	Name     string
	From, To []string
}

// workspaceModel resolves the workspace's effective document model from
// .specquill/config.yml over the built-in defaults (mirrors
// web/src/lib/config.ts). The single parse behind modelRules, the linker and
// the remedy actions.
func workspaceModel(files map[string]string) (map[string]workspaceEntity, []workspaceLink) {
	entities := map[string]workspaceEntity{
		"regulation":   {"regulation", "regulations/", "why"},
		"change":       {"change", "changes/", "why"},
		"requirement":  {"requirement", "requirements/", "what"},
		"spec":         {"spec", "specs/", "how"},
		"data_mapping": {"data_mapping", "data-mappings/", "how"},
		"diagram":      {"diagram", "diagrams/", "how"},
		"work_item":    {"work_item", "work-items/", "when"},
	}
	links := []workspaceLink{
		{"drivers", []string{"requirement"}, []string{"regulation", "change"}},
		{"implements", []string{"spec", "data_mapping"}, []string{"requirement"}},
		{"delivers", []string{"work_item"}, []string{"spec", "requirement"}},
	}

	var cfg struct {
		Entities map[string]struct {
			Folder string `yaml:"folder"`
			Group  string `yaml:"group"`
			Hidden bool   `yaml:"hidden"`
		} `yaml:"entities"`
		LinkTypes map[string]struct {
			From any `yaml:"from"`
			To   any `yaml:"to"`
		} `yaml:"link_types"`
	}
	_ = yaml.Unmarshal([]byte(files[".specquill/config.yml"]), &cfg)
	for kind, e := range cfg.Entities {
		if e.Hidden {
			delete(entities, kind)
			continue
		}
		cur, ok := entities[kind]
		if !ok {
			cur = workspaceEntity{Kind: kind}
		}
		if e.Folder != "" {
			cur.Folder = strings.TrimSuffix(e.Folder, "/") + "/"
		} else if cur.Folder == "" {
			cur.Folder = kind + "s/"
		}
		if e.Group != "" {
			cur.Group = e.Group
		}
		entities[kind] = cur
	}
	if len(cfg.LinkTypes) > 0 { // a declared section replaces the defaults wholesale
		kinds := func(v any) []string {
			switch t := v.(type) {
			case string:
				var out []string
				for _, k := range strings.Split(t, ",") {
					if k = strings.TrimSpace(k); k != "" {
						out = append(out, k)
					}
				}
				return out
			case []any:
				var out []string
				for _, k := range t {
					if s, ok := k.(string); ok {
						out = append(out, strings.TrimSpace(s))
					}
				}
				return out
			}
			return nil
		}
		links = links[:0]
		names := make([]string, 0, len(cfg.LinkTypes))
		for name := range cfg.LinkTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			l := cfg.LinkTypes[name]
			links = append(links, workspaceLink{name, kinds(l.From), kinds(l.To)})
		}
	}
	return entities, links
}

// docKind classifies a workspace document into its family: by folder first
// (the reliable signal), then by frontmatter `type:` matched loosely against
// the kind name. "" when it belongs to no known family.
func docKind(path, content string, entities map[string]workspaceEntity) string {
	for _, e := range entities {
		if e.Folder != "" && strings.HasPrefix(path, e.Folder) {
			return e.Kind
		}
	}
	fm, _, _ := mdfm.Split(content)
	var meta struct {
		Type string `yaml:"type"`
	}
	_ = yaml.Unmarshal([]byte(fm), &meta)
	norm := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", ""), "_", "")
	}
	t := norm(meta.Type)
	if t == "" {
		return ""
	}
	for _, e := range entities {
		if k := norm(e.Kind); k == t || strings.HasPrefix(t, k) {
			return e.Kind
		}
	}
	return ""
}

// linkBetween finds the typed link connecting two families. It returns the
// field name and which document carries it — the model's rule is that the
// LOWER document points UP, so either direction may be the right one.
func linkBetween(links []workspaceLink, fromKind, toKind string) (field string, onFrom bool) {
	has := func(ks []string, k string) bool {
		for _, v := range ks {
			if v == k {
				return true
			}
		}
		return false
	}
	for _, l := range links {
		if has(l.From, fromKind) && has(l.To, toKind) {
			return l.Name, true
		}
	}
	for _, l := range links {
		if has(l.From, toKind) && has(l.To, fromKind) {
			return l.Name, false
		}
	}
	return "", false
}

// linkFieldNames returns the workspace's link-type field names: the declared
// link_types section of .specquill/config.yml, or the built-in defaults
// (mirrors web DEFAULT_LINK_TYPES).
func linkFieldNames(files map[string]string) []string {
	_, links := workspaceModel(files)
	names := make([]string, 0, len(links))
	for _, l := range links {
		names = append(names, l.Name)
	}
	// the built-in set carries two more fields that documents may still use
	if len(links) == 3 && links[0].Name == "drivers" {
		names = append(names, "maps_to", "verifies")
	}
	sort.Strings(names)
	return names
}

// docLinks parses one document's frontmatter and returns its typed links per
// field (fragments stripped, values as written).
func docLinks(content string, fields []string) map[string][]string {
	fm, _, has := mdfm.Split(content)
	if !has {
		return nil
	}
	var m map[string]any
	if yaml.Unmarshal([]byte(fm), &m) != nil {
		return nil
	}
	out := map[string][]string{}
	for _, field := range fields {
		switch v := m[field].(type) {
		case string:
			if v != "" {
				out[field] = []string{stripFragment(v)}
			}
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					out[field] = append(out[field], stripFragment(s))
				}
			}
		}
	}
	return out
}

func stripFragment(p string) string {
	if i := strings.IndexByte(p, '#'); i >= 0 {
		p = p[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(p), "./")
}

// linkIndex is the workspace's typed-link graph, built once per run.
type linkIndex struct {
	outbound map[string][]string            // doc → linked docs (any field, resolved)
	inbound  map[string][]string            // doc → docs linking to it
	byField  map[string]map[string][]string // doc → field → targets (as written)
}

// buildLinkIndex walks every markdown doc's frontmatter once. Only targets
// that exist in the snapshot enter outbound/inbound (byField keeps all).
func buildLinkIndex(files map[string]string, fields []string) *linkIndex {
	idx := &linkIndex{
		outbound: map[string][]string{},
		inbound:  map[string][]string{},
		byField:  map[string]map[string][]string{},
	}
	for p, content := range files {
		if !strings.HasSuffix(p, ".md") || strings.HasPrefix(p, ".") {
			continue
		}
		links := docLinks(content, fields)
		if len(links) == 0 {
			continue
		}
		idx.byField[p] = links
		seen := map[string]bool{}
		for _, targets := range links {
			for _, t := range targets {
				if _, ok := files[t]; !ok || t == p || seen[t] {
					continue
				}
				seen[t] = true
				idx.outbound[p] = append(idx.outbound[p], t)
				idx.inbound[t] = append(idx.inbound[t], p)
			}
		}
	}
	return idx
}

// linkedBlock renders a doc's linked documents (both directions) for the
// drift prompt — capped so a hub document cannot flood the context.
func (idx *linkIndex) linkedBlock(files map[string]string, doc string) string {
	const maxDocs, maxPerDoc, maxTotal = 8, 4 * 1024, 24 * 1024
	var related []string
	seen := map[string]bool{doc: true}
	for _, p := range append(append([]string{}, idx.outbound[doc]...), idx.inbound[doc]...) {
		if !seen[p] && strings.HasSuffix(p, ".md") {
			seen[p] = true
			related = append(related, p)
		}
	}
	if len(related) > maxDocs {
		related = related[:maxDocs]
	}
	var b strings.Builder
	for _, p := range related {
		content := files[p]
		if len(content) > maxPerDoc {
			content = content[:maxPerDoc] + "\n… (truncated)"
		}
		entry := "\n## " + p + "\n```\n" + content + "\n```\n"
		if b.Len()+len(entry) > maxTotal {
			break
		}
		b.WriteString(entry)
	}
	return b.String()
}

// ---------------------------------------------------------------- proposals

type linkProposal struct {
	From   string `json:"from"`
	Field  string `json:"field"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// validLinkProposal filters one proposal against the workspace: both docs
// must exist, the field must be a configured link type, and the link must
// not already be written. Returns "" when acceptable.
func validLinkProposal(p linkProposal, files map[string]string, fields []string, idx *linkIndex) string {
	if p.From == "" || p.To == "" || p.From == p.To {
		return "from/to invalid"
	}
	if _, ok := files[p.From]; !ok || !strings.HasSuffix(p.From, ".md") {
		return "unknown document " + p.From
	}
	if _, ok := files[p.To]; !ok {
		return "unknown document " + p.To
	}
	known := false
	for _, f := range fields {
		if f == p.Field {
			known = true
			break
		}
	}
	if !known {
		return "unknown link type " + p.Field
	}
	for _, t := range idx.byField[p.From][p.Field] {
		if t == p.To {
			return "already linked"
		}
	}
	return ""
}

// POST /api/repos/{repo}/linker/propose?branch= — ask the AI for missing
// links; validated proposals are returned to the client, nothing is written.
func (s *Server) postLinkerPropose(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	fields := linkFieldNames(files)
	idx := buildLinkIndex(files, fields)
	docs := resolveDriftScope(files, nil, nil)

	// compact per-doc index: path, type, title, existing links — the model
	// reads full documents through the tools before it commits to a relation
	var b strings.Builder
	for _, p := range docs {
		fm, _, _ := mdfm.Split(files[p])
		var meta struct {
			Type  string `yaml:"type"`
			Title string `yaml:"title"`
		}
		_ = yaml.Unmarshal([]byte(fm), &meta)
		fmt.Fprintf(&b, "- %s (type: %s) %s", p, meta.Type, meta.Title)
		if links := idx.byField[p]; len(links) > 0 {
			names := make([]string, 0, len(links))
			for f := range links {
				names = append(names, f)
			}
			sort.Strings(names)
			for _, f := range names {
				fmt.Fprintf(&b, " | %s: %s", f, strings.Join(links[f], ", "))
			}
		}
		b.WriteString("\n")
	}

	tb := &speccyToolbox{repo: repo, branch: branch, writable: false, files: files, publish: func() {}}
	var specs []ai.ToolSpec
	for _, spec := range tb.specs(files) {
		if spec.Name != "ask_user" {
			specs = append(specs, spec)
		}
	}
	var out struct {
		Proposals []linkProposal `json:"proposals"`
	}
	if err = s.askJSON(ai.WithLabel(r.Context(), "linker propose"),
		ai.LinkerPrompt(b.String(), modelRules(files)), specs, tb.exec, nil, &out); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	proposals := []linkProposal{}
	dropped := 0
	seen := map[string]bool{}
	for _, p := range out.Proposals {
		p.From, p.To = stripFragment(p.From), stripFragment(p.To)
		key := p.From + "\x00" + p.Field + "\x00" + p.To
		if seen[key] {
			continue
		}
		seen[key] = true
		if validLinkProposal(p, files, fields, idx) != "" {
			dropped++
			continue
		}
		proposals = append(proposals, p)
	}
	log.Printf("linker [%s@%s]: proposed %d link(s) over %d document(s) (%d dropped by validation)",
		repo.ID, branch, len(proposals), len(docs), dropped)
	jsonOK(w, map[string]any{"proposals": proposals, "dropped": dropped})
}

// POST /api/repos/{repo}/linker/apply?branch= {links} — write the accepted
// proposals into the from-documents' frontmatter as uncommitted saves.
func (s *Server) postLinkerApply(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct {
		Links []linkProposal `json:"links"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Links) == 0 {
		jsonError(w, http.StatusBadRequest, "links required")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))

	// applying IS an edit: protected branches route to the caller's workspace
	writeBranch := branch
	if repo.Repo.Cfg.IsProtected(branch) {
		var err error
		if writeBranch, err = s.claimWorkspace(r, repo); err != nil {
			gitFail(w, err)
			return
		}
	}
	files, err := repo.Snapshot(writeBranch)
	if err != nil {
		gitFail(w, err)
		return
	}
	fields := linkFieldNames(files)
	idx := buildLinkIndex(files, fields)

	applied := []linkProposal{}
	failures := []string{}
	for _, p := range body.Links {
		p.From, p.To = stripFragment(p.From), stripFragment(p.To)
		if msg := validLinkProposal(p, files, fields, idx); msg != "" {
			if msg == "already linked" {
				applied = append(applied, p) // idempotent re-apply
				continue
			}
			failures = append(failures, p.From+" "+p.Field+" → "+p.To+": "+msg)
			continue
		}
		content, sha, err := repo.File(writeBranch, p.From)
		if err != nil {
			failures = append(failures, p.From+": "+err.Error())
			continue
		}
		next, added, err := mdfm.AppendListItem(content, p.Field, p.To)
		if err != nil {
			failures = append(failures, p.From+": "+err.Error())
			continue
		}
		if added {
			if next, err = mdfm.Touch(next, false, time.Now()); err != nil {
				failures = append(failures, p.From+": "+err.Error())
				continue
			}
			if _, err := repo.SaveFile(writeBranch, p.From, next, sha); err != nil {
				failures = append(failures, p.From+": "+err.Error())
				continue
			}
			files[p.From] = next // later proposals on the same doc build on it
			idx.byField[p.From] = docLinks(next, fields)
		}
		applied = append(applied, p)
	}
	if len(applied) > 0 {
		s.publish("save", repo.Key(), writeBranch)
	}
	log.Printf("linker [%s@%s]: applied %d link(s) on %s%s", repo.ID, branch, len(applied), writeBranch,
		map[bool]string{true: fmt.Sprintf(" (%d failed: %s)", len(failures), strings.Join(failures, "; ")), false: ""}[len(failures) > 0])
	jsonOK(w, map[string]any{"applied": applied, "failures": failures, "branch": writeBranch})
}
