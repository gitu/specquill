package ai

import (
	"fmt"
	"sort"
	"strings"
)

const groundingBudget = 48 * 1024 // chars of file content in the system prompt

const systemHeader = `You are the reqbase copilot — an assistant embedded in a
requirements-engineering workspace stored as markdown files in git. Requirements
(requirements/REQ-*.md) are driven by regulations (regulations/), implement into
specs (specs/), map to data fields (data-mappings/), and change records
(changes/) track incoming deltas. Typed frontmatter links (drivers, implements,
maps_to, verifies) define traceability.

Ground every answer in the workspace files below. Reference files by their path
(e.g. specs/txn-report.md) and requirements by their id (e.g. REQ-042). If the
workspace does not contain the answer, say so instead of guessing. Be concise;
plain prose, minimal markdown.`

// GroundingPrompt builds the system prompt from the branch snapshot,
// pinning the focused document first and staying inside the budget.
func GroundingPrompt(files map[string]string, focusPath string) string {
	var b strings.Builder
	b.WriteString(systemHeader)

	// .reqbase/skills/* are authoring instructions, not data — pin them right
	// after the header so they always survive the budget
	var skills []string
	for p := range files {
		if strings.HasPrefix(p, ".reqbase/skills/") {
			skills = append(skills, p)
		}
	}
	if len(skills) > 0 {
		sort.Strings(skills)
		b.WriteString("\n\n# Authoring skills (follow these when drafting or editing documents)\n")
		for _, p := range skills {
			b.WriteString("\n" + files[p] + "\n")
		}
	}

	b.WriteString("\n\n# Workspace files\n")

	paths := make([]string, 0, len(files))
	for p := range files {
		if strings.HasSuffix(p, ".excalidraw") || strings.HasPrefix(p, "uploads/") ||
			strings.HasPrefix(p, ".reqbase/skills/") {
			continue // sketch JSON is noise; skills are already pinned above
		}
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if (paths[i] == focusPath) != (paths[j] == focusPath) {
			return paths[i] == focusPath
		}
		return paths[i] < paths[j]
	})

	used := 0
	skipped := []string{}
	for _, p := range paths {
		content := files[p]
		if len(content) > 8*1024 {
			content = content[:8*1024] + "\n… (truncated)"
		}
		entry := fmt.Sprintf("\n## %s\n```\n%s\n```\n", p, content)
		if used+len(entry) > groundingBudget {
			skipped = append(skipped, p)
			continue
		}
		b.WriteString(entry)
		used += len(entry)
	}
	if len(skipped) > 0 {
		b.WriteString("\n(omitted for length: " + strings.Join(skipped, ", ") + ")\n")
	}
	if focusPath != "" {
		b.WriteString("\nThe user is currently viewing: " + focusPath + "\n")
	}
	return b.String()
}

const draftSystem = `You are the reqbase copilot drafting edits to workspace
files in response to a change record. Reply with ONLY a JSON object, no prose.
The shape, shown with example values:

{
  "summary": "Raised the retention window to 7 years per the amendment.",
  "edits": [
    {"path": "specs/retention.md", "search": "retained for 5 years", "replace": "retained for 7 years"}
  ]
}

Rules:
- "path" must be exactly one of the file paths listed below (copy it verbatim,
  e.g. "data-mappings/trade.md"); never invent paths.
- "search" must be copied verbatim from that file and occur exactly once.
- Keep edits minimal and surgical; preserve frontmatter formatting.
- Update statuses/links where the change demands it (e.g. a drifted mapping
  that the edit fixes becomes ok).`

// DraftPrompt builds the conversation asking for structured edits.
func DraftPrompt(changeContent string, files map[string]string) []Message {
	var b strings.Builder
	b.WriteString("# Change record\n```\n" + changeContent + "\n```\n\n# Files you may edit\n")
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		b.WriteString(fmt.Sprintf("\n## %s\n```\n%s\n```\n", p, files[p]))
	}
	b.WriteString("\nDraft the edits that implement this change.")
	return []Message{
		{Role: "system", Content: draftSystem},
		{Role: "user", Content: b.String()},
	}
}
