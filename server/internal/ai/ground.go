package ai

import (
	"fmt"
	"sort"
	"strings"
)

const groundingBudget = 48 * 1024 // default chars of content in the system prompt

// GroundingSource is a read-only reference repo whose files ground the speccy
// alongside the writable workspace. Paths are relative within the source; they
// are surfaced under `~<name>/<path>` headings and are NEVER editable — the
// draft path (see speccy.go) refuses any `~`-prefixed target.
type GroundingSource struct {
	Name  string
	Files map[string]string // path (within the source) → content
}

const systemHeader = `You are the specquill speccy — an assistant embedded in a
requirements-engineering workspace stored as markdown files in git. Requirements
(requirements/REQ-*.md) are driven by regulations (regulations/), implement into
specs (specs/), map to data fields (data-mappings/), and change records
(changes/) track incoming deltas. Typed frontmatter links (drivers, implements,
maps_to, verifies) define traceability.

Ground every answer in the workspace files below. Reference files by their path
(e.g. specs/txn-report.md) and requirements by their id (e.g. REQ-042). Grounded
reference sources appear under ~<source>/<path> headings — they are READ-ONLY
context (regulations, upstream specs); cite them as ~<source>/<path> but never
propose edits to them. If the material does not contain the answer, say so
instead of guessing. Be concise; plain prose, minimal markdown.`

// ToolRules is appended to the chat system prompt whenever tools are
// registered (read-only conversations included — read_file/ask_user are
// always available).
const ToolRules = `

# Tool use
- read_file returns the full current content of any workspace file or
  ~source/path reference — use it instead of answering from a truncated
  grounding excerpt.
- When you need the user to decide something — pick between assumptions,
  confirm a plan, choose options — you MUST call the ask_user tool. Never ask
  in plain text and never end a reply with "reply X to proceed": ask_user
  renders as clickable answer options, a plain-text question does not.
- Ask ONE question per ask_user call, with the choices as its options.
- Prefer asking over assuming. When a request or spec leaves behavior
  undefined — actors, permissions, notifications, timing, edge cases, scope —
  enumerate the gaps and work through them with ask_user, most consequential
  first. Do not silently fill gaps with plausible defaults; proceed on your
  own assumptions only when the user explicitly tells you to.
- Ground every question in evidence first: read the relevant workspace
  documents and reference sources (read_file, including ~source/path) and say
  what the existing implementation or documentation already does, then ask
  which behavior the spec should codify. A question that cites current
  behavior ("~backend retries 3 times today — keep that?") beats an abstract
  one.`

// EditingRules is appended to the chat system prompt when write tools are
// registered — the speccy may then change workspace files directly.
const EditingRules = `

# Editing rules
You can read and edit workspace files with your tools. When editing:
- Make minimal, surgical edits: edit_file replaces ONE unique occurrence of a
  search string copied verbatim from the file. Never rewrite a document unasked.
- Preserve frontmatter keys and formatting. Keep typed links (drivers,
  implements, satisfies, maps_to, verifies) consistent on both sides of a
  relation when you add or change one.
- Requirement statements are atomic, testable blockquotes using RFC-2119
  keywords (MUST/SHALL/SHOULD/MAY); no vague language without a measurable bound.
- New documents follow the family conventions given in the create_file tool
  description (folder, id pattern, frontmatter type) and start with complete
  frontmatter.
- The server maintains the created:/updated: dates on every save — never edit
  those fields yourself.
- When the request is ambiguous or a change would restructure documents
  broadly, ask first via ask_user. Your edits are uncommitted drafts the user
  reviews in the changes drawer — say what you changed when you finish.
- Persist durable project decisions — ask_user answers, constraints, facts
  that no document states — as project memory: create_file at
  .specquill/memory/<kebab-slug>.md with frontmatter (type: Memory, title:)
  and a few sentences. ONE decision per file, named after the decision (no
  dates in names), so parallel branches merge without conflicts; when a
  decision changes, edit or delete that file. Never duplicate what the
  documents already say — memory is for what would otherwise be re-asked.`

// AuthoringRules collects the workspace's authoring guidance: pinned
// .specquill/skills/*.md files, then `speccy.instructions` from config.yml
// and .specquill/instructions.md as `# Workspace instructions`. Shared by the
// chat prompt (GroundingPrompt) and the draft prompt so both flows write
// specs the same way.
func AuthoringRules(files map[string]string, cfgInstructions string) string {
	var b strings.Builder
	var skills []string
	for p := range files {
		if strings.HasPrefix(p, ".specquill/skills/") {
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
	instr := strings.TrimSpace(cfgInstructions)
	if md := strings.TrimSpace(files[".specquill/instructions.md"]); md != "" {
		if instr != "" {
			instr += "\n\n"
		}
		instr += md
	}
	if instr != "" {
		b.WriteString("\n\n# Workspace instructions (structure and content expectations for this workspace)\n\n" + instr + "\n")
	}

	// project memory: one decision per file under .specquill/memory/ — written
	// by the speccy itself (chat write tools), merge-friendly by construction
	var memories []string
	for p := range files {
		if strings.HasPrefix(p, ".specquill/memory/") && strings.HasSuffix(p, ".md") {
			memories = append(memories, p)
		}
	}
	if len(memories) > 0 {
		sort.Strings(memories)
		b.WriteString("\n\n# Project memory (decisions and clarifications recorded in .specquill/memory/ — treat as current project truth; a document wins on conflict)\n")
		for _, p := range memories {
			b.WriteString("\n" + files[p] + "\n")
		}
	}
	return b.String()
}

// GroundingPrompt builds the speccy system prompt from the workspace snapshot
// plus any grounded reference sources. The workspace keeps a 60% floor of the
// budget; grounded sources share the remainder proportionally to their size
// (min 4KB each) and appear under `## ~source/path` headings. focusPath pins the
// viewed document first; budget (0 = package default) is the byte cap.
// instructions is the workspace's `speccy.instructions` config text (may be
// empty; .specquill/instructions.md is read from the snapshot itself).
func GroundingPrompt(workspace map[string]string, refs []GroundingSource, focusPath string, budget int, instructions string) string {
	if budget <= 0 {
		budget = groundingBudget
	}
	// budget split: sources share up to 40% of the total, proportional to their
	// content with a 4KB floor; the workspace keeps at least the remaining 60%.
	wsBudget, shares := budget, map[string]int{}
	if len(refs) > 0 {
		pool, total := budget*40/100, 0
		for _, s := range refs {
			total += sourceLen(s)
		}
		used := 0
		for _, s := range refs {
			share := 4 * 1024
			if total > 0 {
				if prop := pool * sourceLen(s) / total; prop > share {
					share = prop
				}
			}
			shares[s.Name] = share
			used += share
		}
		if wsBudget = budget - used; wsBudget < budget*60/100 {
			wsBudget = budget * 60 / 100
		}
	}

	var b strings.Builder
	b.WriteString(systemHeader)
	b.WriteString(AuthoringRules(workspace, instructions))
	writeWorkspace(&b, workspace, focusPath, wsBudget)

	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	for _, src := range refs {
		writeSource(&b, src, shares[src.Name])
	}

	if focusPath != "" {
		b.WriteString("\nThe user is currently viewing: " + focusPath + "\n")
	}
	return b.String()
}

func sourceLen(s GroundingSource) int {
	n := 0
	for _, c := range s.Files {
		n += len(c)
	}
	return n
}

// writeWorkspace emits the workspace files (focus first), staying inside
// budget. Authoring guidance (skills/instructions) is pinned separately via
// AuthoringRules so it always survives the budget.
func writeWorkspace(b *strings.Builder, files map[string]string, focusPath string, budget int) {
	b.WriteString("\n\n# Workspace files\n")
	paths := make([]string, 0, len(files))
	for p := range files {
		if strings.HasSuffix(p, ".excalidraw") || strings.HasPrefix(p, "uploads/") ||
			strings.HasPrefix(p, ".specquill/skills/") || p == ".specquill/instructions.md" ||
			strings.HasPrefix(p, ".specquill/memory/") {
			continue // sketch JSON is noise; skills/instructions/memory are pinned above
		}
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if (paths[i] == focusPath) != (paths[j] == focusPath) {
			return paths[i] == focusPath
		}
		return paths[i] < paths[j]
	})
	emitFiles(b, paths, func(p string) string { return files[p] }, func(p string) string { return p }, budget)
}

// writeSource emits one grounded reference source under `## ~name/path`
// headings, staying inside its share of the budget.
func writeSource(b *strings.Builder, src GroundingSource, budget int) {
	if len(src.Files) == 0 {
		return
	}
	fmt.Fprintf(b, "\n\n# Reference source ~%s (read-only — cite as ~%s/<path>, never edit)\n", src.Name, src.Name)
	paths := make([]string, 0, len(src.Files))
	for p := range src.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	emitFiles(b, paths, func(p string) string { return src.Files[p] }, func(p string) string { return "~" + src.Name + "/" + p }, budget)
}

// emitFiles writes each path as a fenced block headed by label(path), skipping
// entries once the running total would exceed budget; oversized files truncate.
func emitFiles(b *strings.Builder, paths []string, content, label func(string) string, budget int) {
	used, skipped := 0, []string{}
	for _, p := range paths {
		body := content(p)
		if len(body) > 8*1024 {
			body = body[:8*1024] + "\n… (truncated)"
		}
		entry := fmt.Sprintf("\n## %s\n```\n%s\n```\n", label(p), body)
		if used+len(entry) > budget {
			skipped = append(skipped, label(p))
			continue
		}
		b.WriteString(entry)
		used += len(entry)
	}
	if len(skipped) > 0 {
		b.WriteString("\n(omitted for length: " + strings.Join(skipped, ", ") + ")\n")
	}
}

const draftSystem = `You are the specquill speccy drafting edits to workspace
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

// DraftPrompt builds the conversation asking for structured edits. authoring
// is the AuthoringRules output for the workspace — the draft flow follows the
// same skills/instructions as the chat.
func DraftPrompt(changeContent string, files map[string]string, authoring string) []Message {
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
		{Role: "system", Content: draftSystem + authoring},
		{Role: "user", Content: b.String()},
	}
}
