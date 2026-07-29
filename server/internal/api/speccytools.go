package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/ai"
	"specquill/server/internal/mdfm"
	"specquill/server/internal/project"
)

// speccyToolbox binds the chat tool set to one request: a project, the branch
// the conversation works on, and whether writes are allowed. Writes are
// uncommitted worktree saves — the human reviews them in the changes drawer.
type speccyToolbox struct {
	repo     *project.Project
	branch   string // resolved
	writable bool
	sources  []ai.GroundingSource // ALL selected references (grounded ⊆ these)
	files    map[string]string    // workspace snapshot (list_files/search)
	publish  func()               // save-event hook (SSE fanout)
}

// specs declares the tools for this request. Read tools and ask_user are
// always available; edit/create only when the conversation is writable.
// files (the branch snapshot) feeds the workspace vocabulary into the
// descriptions so the model uses real statuses/folders/id patterns.
func (tb *speccyToolbox) specs(files map[string]string) []ai.ToolSpec {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		// a nil slice marshals as `required: null`, which providers reject —
		// omit the field entirely when nothing is required
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	srcNote := ""
	if len(tb.sources) > 0 {
		names := make([]string, 0, len(tb.sources))
		for _, s := range tb.sources {
			names = append(names, "~"+s.Name)
		}
		sort.Strings(names)
		srcNote = " Available reference sources: " + strings.Join(names, ", ") + "."
	}
	tools := []ai.ToolSpec{
		{
			Name:        "read_file",
			Description: "Read the full current content of a workspace file (worktree state of this conversation's branch), or a read-only reference file via its ~source/path form. Use this when the grounding excerpt above was truncated or a file is missing from it." + srcNote,
			Parameters:  obj(map[string]any{"path": str("workspace-relative path, e.g. specs/txn-report.md, or ~source/path for a reference source")}, "path"),
		},
		{
			Name:        "list_files",
			Description: "List file paths — the workspace when source is omitted, or one read-only reference source. Use it to discover what exists before reading or searching." + srcNote,
			Parameters:  obj(map[string]any{"source": str("reference source name (with or without the ~); omit for the workspace")}),
		},
		{
			Name:        "search",
			Description: "Case-insensitive text search over the workspace AND every reference source (or one source when given). Returns path:line: matches — reference hits are ~source/path. Use it to find how existing software and documentation handle something before asking or editing." + srcNote,
			Parameters: obj(map[string]any{
				"query":  str("literal text to find (not a regex)"),
				"source": str("restrict to one reference source name; omit to search everything"),
			}, "query"),
		},
		{
			Name:        "ask_user",
			Description: "Ask the user ONE clarifying question when the request is ambiguous or a decision is theirs to make. ALWAYS use this tool for questions and confirmations — never ask in plain text; only this tool renders clickable answer options. Provide 2-5 concrete options; the user may also answer in free text. The conversation pauses until they answer.",
			Parameters: obj(map[string]any{
				"question": str("the question to ask, one sentence"),
				"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "2-5 short answer options"},
			}, "question"),
		},
	}
	if !tb.writable {
		return tools
	}
	vocab := workspaceVocabulary(files)
	tools = append(tools,
		ai.ToolSpec{
			Name:        "edit_file",
			Description: "Edit one workspace file by replacing a unique text occurrence. search must be copied VERBATIM from the file and occur exactly once; keep edits minimal. Reference sources (~source/...) are read-only. The save is an uncommitted draft on the current branch." + vocab,
			Parameters: obj(map[string]any{
				"path":    str("workspace-relative path of the file to edit"),
				"search":  str("exact text to replace (verbatim, unique in the file)"),
				"replace": str("replacement text"),
			}, "path", "search", "replace"),
		},
		ai.ToolSpec{
			Name:        "create_file",
			Description: "Create a new workspace file (fails if it already exists). Markdown documents must start with complete frontmatter (type, title, status, links); follow the family conventions." + vocab,
			Parameters: obj(map[string]any{
				"path":    str("workspace-relative path for the new file, in the right family folder"),
				"content": str("full file content"),
			}, "path", "content"),
		},
	)
	return tools
}

// exec dispatches one model tool call. Result strings go back to the model;
// errors are surfaced as tool errors (the model can retry), never as HTTP
// failures. ask_user halts the loop.
func (tb *speccyToolbox) exec(name, args string) (string, bool, error) {
	switch name {
	case "read_file":
		var a struct{ Path string }
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", false, fmt.Errorf("invalid arguments: %v", err)
		}
		out, err := tb.readFile(a.Path)
		return out, false, err
	case "list_files":
		var a struct{ Source string }
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", false, fmt.Errorf("invalid arguments: %v", err)
		}
		out, err := tb.listFiles(a.Source)
		return out, false, err
	case "search":
		var a struct{ Query, Source string }
		if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Query) == "" {
			return "", false, fmt.Errorf("search needs a query")
		}
		out, err := tb.search(a.Query, a.Source)
		return out, false, err
	case "ask_user":
		var a struct {
			Question string
			Options  []string
		}
		if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Question) == "" {
			return "", false, fmt.Errorf("ask_user needs a question")
		}
		return "", true, nil
	case "edit_file":
		var a struct{ Path, Search, Replace string }
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", false, fmt.Errorf("invalid arguments: %v", err)
		}
		out, err := tb.editFile(a.Path, a.Search, a.Replace)
		return out, false, err
	case "create_file":
		var a struct{ Path, Content string }
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", false, fmt.Errorf("invalid arguments: %v", err)
		}
		out, err := tb.createFile(a.Path, a.Content)
		return out, false, err
	}
	return "", false, fmt.Errorf("unknown tool %q", name)
}

func (tb *speccyToolbox) readFile(path string) (string, error) {
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		name, sub, ok := strings.Cut(rest, "/")
		if !ok {
			return "", fmt.Errorf("reference path must be ~source/path")
		}
		src, err := tb.source(name)
		if err != nil {
			return "", err
		}
		if content, ok := src.Files[sub]; ok {
			return content, nil
		}
		return "", fmt.Errorf("not found in ~%s: %s (list_files finds the right path)", name, sub)
	}
	content, _, err := tb.repo.File(tb.branch, path)
	if err != nil {
		return "", fmt.Errorf("not found: %s", path)
	}
	return content, nil
}

// source resolves a reference source by name (leading ~ tolerated).
func (tb *speccyToolbox) source(name string) (*ai.GroundingSource, error) {
	name = strings.TrimPrefix(name, "~")
	for i := range tb.sources {
		if tb.sources[i].Name == name {
			return &tb.sources[i], nil
		}
	}
	return nil, fmt.Errorf("unknown reference source ~%s", name)
}

func (tb *speccyToolbox) listFiles(source string) (string, error) {
	files := tb.files
	prefix := ""
	if source != "" {
		src, err := tb.source(source)
		if err != nil {
			return "", err
		}
		files, prefix = src.Files, "~"+src.Name+"/"
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, prefix+p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return "(no files)", nil
	}
	return strings.Join(paths, "\n"), nil
}

const maxSearchHits = 120

// search scans the workspace and reference-source snapshots for a literal,
// case-insensitive match, git-grep style: path:line: text.
func (tb *speccyToolbox) search(query, source string) (string, error) {
	type corpus struct {
		prefix string
		files  map[string]string
	}
	var corpora []corpus
	if source != "" {
		src, err := tb.source(source)
		if err != nil {
			return "", err
		}
		corpora = []corpus{{"~" + src.Name + "/", src.Files}}
	} else {
		corpora = []corpus{{"", tb.files}}
		for i := range tb.sources {
			corpora = append(corpora, corpus{"~" + tb.sources[i].Name + "/", tb.sources[i].Files})
		}
	}
	q := strings.ToLower(query)
	var hits []string
	for _, c := range corpora {
		paths := make([]string, 0, len(c.files))
		for p := range c.files {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if len(hits) >= maxSearchHits {
				break
			}
			for i, line := range strings.Split(c.files[p], "\n") {
				if strings.Contains(strings.ToLower(line), q) {
					hits = append(hits, fmt.Sprintf("%s%s:%d: %s", c.prefix, p, i+1, strings.TrimSpace(line)))
					if len(hits) >= maxSearchHits {
						break
					}
				}
			}
		}
	}
	if len(hits) == 0 {
		return "no matches for " + strconv.Quote(query), nil
	}
	out := strings.Join(hits, "\n")
	if len(hits) >= maxSearchHits {
		out += fmt.Sprintf("\n… capped at %d matches — narrow the query", maxSearchHits)
	}
	return out, nil
}

func (tb *speccyToolbox) editFile(path, search, replace string) (string, error) {
	if !tb.writable {
		return "", fmt.Errorf("editing is disabled for this conversation")
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("reference sources are read-only")
	}
	if search == "" || search == replace {
		return "", fmt.Errorf("empty or no-op edit")
	}
	content, sha, err := tb.repo.File(tb.branch, path)
	if err != nil {
		return "", fmt.Errorf("not found: %s", path)
	}
	switch strings.Count(content, search) {
	case 0:
		if replace != "" && strings.Contains(content, replace) {
			return "already applied — the file contains the replacement text", nil
		}
		return "", fmt.Errorf("search text not found in %s — copy it verbatim from read_file", path)
	case 1:
	default:
		return "", fmt.Errorf("search text occurs more than once in %s — extend it until unique", path)
	}
	next := strings.Replace(content, search, replace, 1)
	next, err = tb.finishMarkdown(path, next, false)
	if err != nil {
		return "", err
	}
	if _, err := tb.repo.SaveFile(tb.branch, path, next, sha); err != nil {
		return "", err
	}
	tb.publish()
	return "edited " + path + " (uncommitted draft)", nil
}

func (tb *speccyToolbox) createFile(path, content string) (string, error) {
	if !tb.writable {
		return "", fmt.Errorf("editing is disabled for this conversation")
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("reference sources are read-only")
	}
	if _, _, err := tb.repo.File(tb.branch, path); err == nil {
		return "", fmt.Errorf("%s already exists — use edit_file", path)
	}
	content, err := tb.finishMarkdown(path, content, true)
	if err != nil {
		return "", err
	}
	if _, err := tb.repo.SaveFile(tb.branch, path, content, ""); err != nil {
		return "", err
	}
	tb.publish()
	return "created " + path + " (uncommitted draft)", nil
}

// finishMarkdown enforces the write-tool post-conditions on markdown files:
// the frontmatter must still parse (broken YAML bounces back to the model as
// a tool error) and the created/updated dates are maintained server-side.
func (tb *speccyToolbox) finishMarkdown(path, content string, isNew bool) (string, error) {
	if !strings.HasSuffix(path, ".md") {
		return content, nil
	}
	if err := mdfm.Validate(content); err != nil {
		return "", fmt.Errorf("rejected: %v — fix the edit so the frontmatter stays valid", err)
	}
	return mdfm.Touch(content, isNew, time.Now())
}

// workspaceVocabulary summarizes the workspace's real value sets for the
// write-tool descriptions: statuses and id patterns from .specquill/config.yml,
// enum values from .specquill/schema.json, and the document family folders.
// The model is told the valid values instead of inventing them.
func workspaceVocabulary(files map[string]string) string {
	var b strings.Builder

	var cfg struct {
		Statuses []string `yaml:"statuses"`
		IDs      map[string]struct {
			Pattern string `yaml:"pattern"`
		} `yaml:"ids"`
		Entities map[string]struct {
			Folder string `yaml:"folder"`
		} `yaml:"entities"`
	}
	_ = yaml.Unmarshal([]byte(files[".specquill/config.yml"]), &cfg)
	if len(cfg.Statuses) > 0 {
		b.WriteString(" Valid status values: " + strings.Join(cfg.Statuses, ", ") + ".")
	}

	var schema struct {
		Fields map[string]struct {
			Values map[string]string `json:"values"`
		} `json:"fields"`
	}
	_ = json.Unmarshal([]byte(files[".specquill/schema.json"]), &schema)
	fields := make([]string, 0, len(schema.Fields))
	for name, f := range schema.Fields {
		if name == "status" || len(f.Values) == 0 {
			continue // statuses come from config.yml
		}
		vals := make([]string, 0, len(f.Values))
		for v := range f.Values {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		fields = append(fields, name+": "+strings.Join(vals, "|"))
	}
	if len(fields) > 0 {
		sort.Strings(fields)
		b.WriteString(" Enum fields — " + strings.Join(fields, "; ") + ".")
	}

	// document families: folders that hold markdown, with their id scheme
	folders := map[string]bool{}
	for p := range files {
		if dir, _, ok := strings.Cut(p, "/"); ok && strings.HasSuffix(p, ".md") && !strings.HasPrefix(p, ".") {
			folders[dir] = true
		}
	}
	for _, e := range cfg.Entities {
		if f := strings.Trim(e.Folder, "/"); f != "" {
			folders[f] = true
		}
	}
	fams := make([]string, 0, len(folders))
	for f := range folders {
		fam := f + "/"
		// ids are keyed by family name (singular); match common folder forms
		for family, id := range cfg.IDs {
			if f == family || f == family+"s" || strings.TrimSuffix(f, "s") == family {
				fam += " (id pattern " + id.Pattern + ")"
				break
			}
		}
		fams = append(fams, fam)
	}
	if len(fams) > 0 {
		sort.Strings(fams)
		b.WriteString(" Document families: " + strings.Join(fams, ", ") + ".")
	}
	return b.String()
}
