// Package scaffold bootstraps a new reqbase workspace repository: folder
// skeleton per document type, the .reqbase/ property schema, and AI authoring
// skills the copilot grounds itself on.
package scaffold

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"reqbase/server/internal/okf"
)

// SpecType is one onboardable document family.
type SpecType struct {
	Key     string
	Dir     string
	Title   string
	Starter string // example document (empty = folder README only)
	Skill   string // AI authoring skill for .reqbase/skills/
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

// Init scaffolds dir as a reqbase workspace with the chosen spec types and
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
		".reqbase/schema.json":         schemaJSON,
		".reqbase/skills/authoring.md": authoringSkill,
	}
	for _, t := range picked {
		files[".reqbase/skills/"+t.Key+".md"] = t.Skill
		if t.Starter != "" {
			files[t.Dir+"/"+starterName(t)] = t.Starter
		} else {
			files[t.Dir+"/README.md"] = "---\ntype: Guide\ntitle: " + t.Title + "\n---\n\n# " + t.Title + "\n\nDocuments of type `" + t.Key + "` live here.\n"
		}
	}
	files["reqbase.yml.example"] = serverConfigStub(project)

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
			{"-c", "user.name=reqbase", "-c", "user.email=reqbase@local", "commit", "-m", "reqbase workspace scaffold"},
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
	fmt.Println("next: adapt reqbase.yml.example into your server config and point repos[0].url at this repo's origin")
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
	fmt.Fprintf(&b, "---\ntype: Guide\ntitle: %s\ndescription: reqbase workspace — requirements engineering as plain markdown in git.\n---\n\n", project)
	fmt.Fprintf(&b, "# %s\n\nA reqbase workspace: requirements engineering as plain markdown in git.\n\n## Layout\n\n", project)
	keys := make([]string, 0, len(picked))
	for k := range picked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := picked[k]
		fmt.Fprintf(&b, "- `%s/` — %s\n", t.Dir, t.Title)
	}
	b.WriteString("- `.reqbase/` — property schema and AI authoring skills (the copilot follows these)\n")
	b.WriteString("\nDocuments carry typed frontmatter links (`drivers`, `implements`, `maps_to`, `verifies`) that build the traceability graph.\n")
	return b.String()
}

func serverConfigStub(project string) string {
	return `# reqbase server config stub for this workspace — merge into your reqbase.yml
repos:
  - id: ` + strings.ToLower(strings.ReplaceAll(project, " ", "-")) + `
    url: <git remote url>          # server pushes/fetches with token_env
    mode: writable
    default_branch: main
    token_env: REQBASE_TOKEN

ai:
  enabled: true
  base_url: https://api.openai.com/v1   # any OpenAI-compatible endpoint
  model: <thinking-class model>          # chat + draft edits
  quick_model: <small fast model>        # commit messages, one-shot tasks
  api_key_env: REQBASE_AI_KEY
`
}
