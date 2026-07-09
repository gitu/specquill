// Package scaffold bootstraps a new specquill workspace repository: folder
// skeleton per document type, the .specquill/ property schema, and AI authoring
// skills the copilot grounds itself on.
package scaffold

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"specquill/server/internal/okf"
)

// SpecType is one onboardable document family.
type SpecType struct {
	Key     string
	Dir     string
	Title   string
	Starter string // example document (empty = folder README only)
	Skill   string // AI authoring skill for .specquill/skills/
}

// DefaultTypes is what `init` scaffolds without an explicit --types.
var DefaultTypes = []string{"requirements", "specs", "changes"}

// AllTypes lists every onboardable family (requirements is always included).
func AllTypes() []string {
	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Init scaffolds dir as a specquill workspace with the chosen spec types and
// commits the result on a fresh `main` when the directory is not a git repo.
func Init(dir, project string, chosen []string) error {
	if project == "" {
		project = filepath.Base(absOr(dir))
	}
	picked := map[string]SpecType{"requirements": types["requirements"]}
	for _, k := range chosen {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" {
			continue
		}
		t, ok := types[k]
		if !ok {
			return fmt.Errorf("unknown spec type %q (available: %s)", k, strings.Join(AllTypes(), ", "))
		}
		picked[k] = t
	}

	files := map[string]string{
		"README.md":                    workspaceReadme(project, picked),
		"index.md":                     "---\nokf_version: \"" + okf.Version + "\"\n---\n\n# Index\n",
		".specquill/schema.json":         schemaJSON,
		".specquill/skills/authoring.md": authoringSkill,
	}
	for _, t := range picked {
		files[".specquill/skills/"+t.Key+".md"] = t.Skill
		if t.Starter != "" {
			files[t.Dir+"/"+starterName(t)] = t.Starter
		} else {
			files[t.Dir+"/README.md"] = "---\ntype: Guide\ntitle: " + t.Title + "\n---\n\n# " + t.Title + "\n\nDocuments of type `" + t.Key + "` live here.\n"
		}
	}
	files["specquill.yml.example"] = serverConfigStub(project)

	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err == nil {
			return fmt.Errorf("%s already exists — refusing to overwrite (init wants a fresh directory)", rel)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// the workspace is an OKF bundle (docs/okf: index.md carries okf_version)
	// — generate the per-directory listings before the scaffold commit
	if _, err := okf.GenerateIndexes(dir); err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		for _, args := range [][]string{
			{"init", "-b", "main"},
			{"add", "-A"},
			{"-c", "user.name=specquill", "-c", "user.email=specquill@local", "commit", "-m", "specquill workspace scaffold"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
			}
		}
	}

	names := make([]string, 0, len(picked))
	for k := range picked {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Printf("scaffolded %s with types: %s\n", dir, strings.Join(names, ", "))
	fmt.Println("next: adapt specquill.yml.example into your server config and point repos[0].url at this repo's origin")
	return nil
}

func absOr(dir string) string {
	if a, err := filepath.Abs(dir); err == nil {
		return a
	}
	return dir
}

func starterName(t SpecType) string {
	switch t.Key {
	case "requirements":
		return "REQ-001.md"
	case "decisions":
		return "ADR-001.md"
	default:
		return "example.md"
	}
}

func workspaceReadme(project string, picked map[string]SpecType) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntype: Guide\ntitle: %s\ndescription: specquill workspace — requirements engineering as plain markdown in git.\n---\n\n", project)
	fmt.Fprintf(&b, "# %s\n\nA specquill workspace: requirements engineering as plain markdown in git.\n\n## Layout\n\n", project)
	keys := make([]string, 0, len(picked))
	for k := range picked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := picked[k]
		fmt.Fprintf(&b, "- `%s/` — %s\n", t.Dir, t.Title)
	}
	b.WriteString("- `.specquill/` — property schema and AI authoring skills (the copilot follows these)\n")
	b.WriteString("\nDocuments carry typed frontmatter links (`drivers`, `implements`, `maps_to`, `verifies`) that build the traceability graph.\n")
	return b.String()
}

func serverConfigStub(project string) string {
	return `# specquill server config stub for this workspace — merge into your specquill.yml
repos:
  - id: ` + strings.ToLower(strings.ReplaceAll(project, " ", "-")) + `
    url: <git remote url>          # server pushes/fetches with token_env
    mode: writable
    default_branch: main
    token_env: SPECQUILL_TOKEN

ai:
  enabled: true
  base_url: https://api.openai.com/v1   # any OpenAI-compatible endpoint
  model: <thinking-class model>          # chat + draft edits
  quick_model: <small fast model>        # commit messages, one-shot tasks
  api_key_env: SPECQUILL_AI_KEY
`
}

// families maps CLI singulars onto the registry ("specquill add requirement").
var families = map[string]string{
	"requirement": "requirements", "req": "requirements",
	"spec": "specs", "specification": "specs",
	"regulation": "regulations",
	"data-mapping": "data-mappings", "mapping": "data-mappings",
	"change": "changes", "decision": "decisions", "adr": "decisions",
	"glossary": "glossary",
}

var idPattern = regexp.MustCompile(`(REQ|ADR)-(\d+)`)

// Add creates one new document of the given family inside the workspace at
// dir and returns its path. Numbered families (requirements → REQ-NNN,
// decisions → ADR-NNN) pick the next free number; the others need a name.
// OKF indexes are refreshed when the workspace opted in.
func Add(dir, family, name string) (string, error) {
	fam, ok := families[strings.ToLower(strings.TrimSpace(family))]
	if !ok {
		if _, direct := types[family]; direct {
			fam = family
		} else {
			keys := make([]string, 0, len(families))
			for k := range families {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return "", fmt.Errorf("unknown document type %q (one of: %s)", family, strings.Join(keys, ", "))
		}
	}
	t := types[fam]
	sub := filepath.Join(dir, t.Dir)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return "", err
	}

	var rel, content string
	switch fam {
	case "requirements", "decisions":
		prefix := "REQ"
		if fam == "decisions" {
			prefix = "ADR"
		}
		next := 1
		entries, _ := os.ReadDir(sub)
		for _, e := range entries {
			if m := idPattern.FindStringSubmatch(e.Name()); m != nil && m[1] == prefix {
				if n, err := strconv.Atoi(m[2]); err == nil && n >= next {
					next = n + 1
				}
			}
		}
		id := fmt.Sprintf("%s-%03d", prefix, next)
		rel = t.Dir + "/" + id + ".md"
		content = strings.ReplaceAll(t.Starter, prefix+"-001", id)
		if name != "" {
			content = strings.ReplaceAll(content, "Example requirement", name)
			content = strings.ReplaceAll(content, "Example decision", name)
		}
	default:
		if name == "" {
			return "", fmt.Errorf("%s needs a name: specquill add %s <name>", fam, family)
		}
		slug := strings.ToLower(strings.Join(strings.Fields(name), "-"))
		if fam == "changes" {
			slug = time.Now().Format("2006-01") + "-" + slug
		}
		rel = t.Dir + "/" + slug + ".md"
		if t.Starter != "" {
			content = t.Starter
			for _, ex := range []string{"Example specification", "Example entity mapping", "Example change record", "Glossary"} {
				content = strings.Replace(content, ex, name, 1)
			}
		} else {
			content = "---\ntype: Regulation\ntitle: " + name + "\nstatus: active\n---\n\n# " + name + "\n"
		}
	}

	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("%s already exists", rel)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", err
	}
	if okf.Enabled(dir) {
		if _, err := okf.GenerateIndexes(dir); err != nil {
			return rel, fmt.Errorf("created %s but index regeneration failed: %w", rel, err)
		}
	}
	return rel, nil
}
