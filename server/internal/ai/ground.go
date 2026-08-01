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
- Preserve frontmatter keys and formatting. Typed links live on the LOWER
  level pointing up (drivers on requirements, implements on specs, delivers
  on work items) — a new document must carry its upward link; write drivers
  as a plain path list, never {type, ref} maps; when the right upper-level
  document is unclear, ask_user.
- move_file renames/moves a file and rewrites inbound references in other
  documents automatically — use it instead of delete+create. delete_file does
  NOT touch inbound references: search for them first and confirm via
  ask_user when other documents still reference the file.
- Diagrams: draw_sketch creates/replaces .excalidraw.png sketches from scene
  JSON (the server renders the pixels and embeds the scene — natively viewable,
  editable in the sketch editor). Caption boxes/arrows with their label
  property instead of placing text elements. To change an existing sketch,
  read_file it (returns the embedded scene) and draw_sketch the same path.
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

const driftSystem = `You are the specquill source-drift auditor. You verify ONE
requirements document against the workspace's read-only reference sources
(source code, API contracts, documentation) and report where they diverge.

Investigate with the tools first: list_files/search/read_file over the
~source/... references to find the places that implement or describe what the
document specifies. Then reply with ONLY a JSON object, no prose:

{
  "findings": [
    {
      "anchor": "REQ-012",
      "source": "platform-api",
      "kind": "contradiction",
      "severity": "high",
      "title": "Timestamp precision differs from the spec",
      "detail": "The document requires microsecond precision; the API contract declares millisecond timestamps.",
      "sourcePaths": ["openapi.json"],
      "evidence": [{"path": "openapi.json", "quote": "\"format\": \"date-time-ms\""}]
    },
    {
      "anchor": "REQ-012",
      "source": "platform-api",
      "kind": "new-requirement",
      "severity": "medium",
      "title": "Retry/backoff behaviour has no requirement",
      "detail": "The client retries failed submissions with backoff; no requirement covers retry semantics.",
      "suggestedPath": "requirements/REQ-submission-retry.md",
      "sourcePaths": ["client.go"],
      "evidence": [{"path": "client.go", "quote": "backoff.Retry(submit, "}]
    }
  ]
}

Rules:
- "anchor" identifies the drifted section STRUCTURALLY: the requirement id from
  the document's frontmatter/filename (e.g. REQ-012) or the nearest heading
  text. Never invent ids.
- "source" is the reference source name the finding is against (no ~).
- "kind" is one of: missing-implementation | undocumented-behavior |
  contradiction | outdated-requirement | new-requirement.
  "severity": high | medium | low.
- Use "new-requirement" when the source mandates a capability, rule or
  constraint in THIS document's area that no requirement states at all — the
  document is not wrong, something is missing beside it. Such a finding also
  carries "suggestedPath": where that new document should live, following the
  workspace's family folders and id conventions. Do NOT use it for details
  that belong inside the document under audit (that is undocumented-behavior
  or missing-implementation).
- "evidence" quotes are VERBATIM excerpts copied from the named source file —
  they are checked against the file and the finding is discarded when they do
  not match. Quote the smallest decisive fragment.
- Report only real divergence you have evidence for. A consistent document
  yields {"findings": []}. Never report style or formatting.`

// DriftPrompt builds the per-document drift-check conversation. The document
// is inlined (it is the subject under audit) together with its LINKED
// documents (linkedBlock — the WHY above and the HOW below it, so the check
// sees the chain context); the reference sources are reachable through the
// read tools.
func DriftPrompt(docPath, docContent, linkedBlock, extracted, instructions string, sourceNames []string) []Message {
	system := driftSystem
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("Reference sources to verify against: ")
	for i, n := range sourceNames {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("~" + n)
	}
	b.WriteString("\n\n# Document under audit: " + docPath + "\n```\n" + docContent + "\n```\n")
	if linkedBlock != "" {
		b.WriteString("\n# Linked documents (context only — report drift of the document under audit, not of these)\n")
		b.WriteString(linkedBlock)
	}
	if extracted != "" {
		b.WriteString("\n# What the application requires (extracted earlier from the sources)\n" +
			"Start from this inventory — it is the analyzed baseline. Use the tools to confirm " +
			"or extend it against the source itself.\n\n" + extracted + "\n")
	}
	b.WriteString("\nVerify this document against the reference sources and report drift as JSON.")
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const surveySystem = `You are the specquill application surveyor. You take ONE
read-only reference source — an application's code, API contract or
documentation — and divide it into the AREAS a requirements analyst would work
through one at a time.

Explore with the tools (list_files, read_file, search) before answering. Then
reply with ONLY a JSON object, no prose:

{
  "areas": [
    {
      "name": "Transaction reporting",
      "summary": "Submitting executed trades to the competent authority.",
      "paths": ["reporting/submit.go", "openapi.json"]
    }
  ]
}

Rules:
- Divide by CAPABILITY — what the application does for its users — not by
  directory layout or language artefacts.
- "paths" lists the source files that carry that area, so the next pass can
  read just those. Copy paths exactly as list_files reports them (without the
  ~source/ prefix); every area needs at least one.
- Cover the whole source: an area for each substantial capability, none for
  build files, fixtures or vendored code. 2-8 areas is typical; never more
  than 12.
- Areas must not overlap — each capability belongs to exactly one.`

// SurveyPrompt builds the divide-the-application conversation: partition one
// source into the capability areas that are then extracted one at a time.
func SurveyPrompt(sourceName, instructions string) []Message {
	system := surveySystem
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: "# Application source to divide: ~" + sourceName +
			"\n\nExplore it and return its capability areas as JSON."},
	}
}

const focusSystem = `You are the specquill focus adviser. You propose the AREAS
a requirements analyst could aim the next gap analysis at, so they can work
through a large application deliberately instead of sweeping everything.

Base the proposals on what you are given: the extracted requirement
inventories (with their coverage), the reference sources, and the workspace's
documents. Explore with the tools where the material is thin. Reply with ONLY
a JSON object, no prose:

{
  "areas": [
    {
      "name": "Data retention",
      "reason": "4 of 6 extracted retention requirements have no document.",
      "sources": ["regulations"]
    }
  ]
}

Rules:
- Propose 3-6 areas, most valuable first. "reason" says concretely why it is
  worth a sweep — uncovered requirements, thin documentation, recent source
  activity — never generic praise.
- "name" is short enough to use as a filter (2-5 words) and describes a
  capability, not a file or folder.
- "sources" names the reference sources that area lives in, exactly as given.
- Prefer areas with real coverage gaps over areas that are already well
  documented.`

// FocusPrompt builds the propose-where-to-look conversation.
func FocusPrompt(sourcesBlock, docIndex, instructions string) []Message {
	system := focusSystem
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Reference sources and what has been extracted from them\n" + sourcesBlock + "\n")
	b.WriteString("\n# Workspace documents\n" + docIndex + "\n")
	b.WriteString("\nPropose the areas worth focusing a gap analysis on, as JSON.")
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const matchSystem = `You are the specquill coverage matcher. You are given
requirements extracted from an application and the workspace's requirement
documents. For EACH extracted requirement you decide whether a document
already states it.

Use the tools: search the workspace for the terms of each requirement and
read_file the candidates before deciding. Then reply with ONLY a JSON object,
no prose:

{
  "matches": [
    {"index": 1, "coverage": "full", "document": "requirements/REQ-042.md",
     "note": "REQ-042 states the same deadline."},
    {"index": 2, "coverage": "partial", "document": "specs/txn-report.md",
     "note": "The spec mentions timestamps but sets no precision."},
    {"index": 3, "coverage": "none", "document": "", "note": ""}
  ]
}

Rules:
- Answer for EVERY index you were given, exactly once.
- "coverage": full (a document states this requirement), partial (a document
  touches it but leaves the substance open) or none.
- "document" is an exact path from the index below, "" when coverage is none.
- Match on MEANING, not wording — the same rule stated differently is full
  coverage. Never claim coverage you did not read; when unsure, say none.`

// MatchPrompt builds one batch of the walk-and-match pass: extracted
// requirements on one side, the workspace's documents on the other.
func MatchPrompt(items, docIndex, instructions string) []Message {
	system := matchSystem
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Extracted requirements to match\n" + items + "\n")
	b.WriteString("\n# Workspace documents\n" + docIndex + "\n")
	b.WriteString("\nMatch each requirement against the documents and reply as JSON.")
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const extractSystem = `You are the specquill requirements extractor. You read
ONE AREA of a read-only reference source — an application's code, API contract
or documentation — and write down what it actually requires of the system, as
atomic requirements.

Read the area's files with the tools first (read_file, search); follow what
they reference when you need to. Then reply with ONLY a JSON object, no prose:

{
  "requirements": [
    {
      "title": "Report submission deadline",
      "statement": "Executed transactions SHALL be reported no later than the close of the following working day.",
      "evidence": [{"path": "regulations/mifid-ii.md", "quote": "no later than the close of the following working day"}]
    }
  ]
}

Rules:
- Stay inside this area. Another pass covers the rest of the application.
- "statement" is ONE atomic, testable sentence using RFC-2119 keywords
  (SHALL/MUST/SHOULD/MAY) and no vague bounds. Describe WHAT is required,
  never how the source implements it.
- "evidence" quotes are VERBATIM excerpts copied from the named source file —
  they are checked and the requirement is discarded when they do not match.
- Extract what the source actually mandates. Never invent requirements the
  evidence does not support, and never restate trivia (logging, formatting).
- Whether a workspace document already covers a requirement is decided by a
  later pass — do not guess it here.`

// ExtractPrompt builds the conquer step: extract the requirements of ONE
// surveyed area of an application source.
func ExtractPrompt(sourceName, area, summary string, paths []string, instructions string) []Message {
	system := extractSystem
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Application source: ~" + sourceName + "\n")
	b.WriteString("# Area under extraction: " + area + "\n")
	if summary != "" {
		b.WriteString(summary + "\n")
	}
	if len(paths) > 0 {
		b.WriteString("\nIts files (read them with read_file, prefixed ~" + sourceName + "/):\n")
		for _, p := range paths {
			b.WriteString("- ~" + sourceName + "/" + p + "\n")
		}
	}
	b.WriteString("\nExtract this area's requirements as JSON.")
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const gapSystem = `You are the specquill coverage-gap auditor. You sweep ONE
read-only reference source (source code, API contracts, documentation) and
report the capabilities, behaviors and rules it contains that NO document in
the requirements workspace covers.

Investigate with the tools: list_files/read_file over the ~source/... files to
find what the source does, then search/read_file over the WORKSPACE documents
to check whether a requirement or spec covers it. Then reply with ONLY a JSON
object, no prose:

{
  "findings": [
    {
      "anchor": "openapi.json#POST /reports",
      "severity": "medium",
      "title": "Report submission endpoint has no requirement",
      "detail": "The API exposes report submission with validation rules that no workspace document describes.",
      "suggestedPath": "requirements/REQ-report-submission.md",
      "sourcePaths": ["openapi.json"],
      "evidence": [{"path": "openapi.json", "quote": "\"operationId\": \"submitReport\""}]
    }
  ]
}

Rules:
- "anchor" identifies the uncovered capability STRUCTURALLY and stably: a
  source file path plus the section/endpoint/function it lives in.
- "suggestedPath" is where the missing document should live, following the
  workspace's family folders and id conventions.
- "evidence" quotes are VERBATIM excerpts copied from the named source file —
  they are checked and the finding is discarded when they do not match.
- Search the workspace BEFORE reporting: something covered by any document
  (even partially, even under another name) is not a gap. Report substantive
  capabilities only — never internal helpers, boilerplate or style.
- A fully covered source yields {"findings": []}.`

// GapPrompt builds the per-source coverage-audit conversation. docIndex lists
// the workspace's document paths so the model knows what exists before it
// searches.
func GapPrompt(sourceName, docIndex, extracted, focus, instructions string) []Message {
	system := gapSystem
	if focus != "" {
		system += "\n\n# Focus\nThis sweep is aimed at ONE area: " + focus +
			".\nReport only gaps that belong to it. Capabilities outside the focus are " +
			"another sweep's job — skip them silently, and return an empty list when the " +
			"source has nothing in this area."
	}
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Reference source under audit: ~" + sourceName + "\n\n")
	if extracted != "" {
		b.WriteString("# What this source requires (extracted earlier — the analyzed baseline)\n" +
			"Each row already says whether a document covers it; confirm the uncovered ones " +
			"with the tools before reporting them as gaps.\n\n" + extracted + "\n\n")
	}
	b.WriteString("# Workspace documents (read them with read_file, search them with search)\n")
	b.WriteString(docIndex + "\n")
	b.WriteString("\nSweep ~" + sourceName + " and report what the workspace does not cover as JSON.")
	return []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const linkerSystem = `You are the specquill linker. You propose MISSING typed
frontmatter links between workspace documents, strictly following the
workspace's configured link types (which field lives on which document family
and points at which). You never invent link fields and never re-propose links
that already exist.

Reply with ONLY a JSON object, no prose:

{
  "proposals": [
    {
      "from": "specs/venue.md",
      "field": "implements",
      "to": "requirements/REQ-063.md",
      "reason": "The spec's partial-fill section realizes REQ-063 but does not declare it."
    }
  ]
}

Rules:
- "from"/"to" are exact document paths from the index below; "field" is one of
  the configured link types, placed on the LOWER document pointing UP.
- Propose only links the documents' actual content clearly supports — shared
  wording alone is not a relation. Give the decisive reason in one sentence.
- Fewer, confident proposals beat many speculative ones. An already
  well-linked workspace yields {"proposals": []}.`

// LinkerPrompt builds the propose-missing-links conversation. docIndex lists
// every document with its type, title and existing links; guidance is the
// workspace's model rules (link types, families).
func LinkerPrompt(docIndex, guidance string) []Message {
	return []Message{
		{Role: "system", Content: linkerSystem + guidance},
		{Role: "user", Content: "# Document index\n" + docIndex + "\nPropose the missing links as JSON."},
	}
}

const planSystem = `You are the specquill remediation planner. Given a
confirmed source-alignment finding, you propose WHICH workspace documents
should be created to resolve it — and how they link together.

Reply with ONLY a JSON object, no prose:

{
  "rationale": "The amendment is a product-driven change realized by two requirements.",
  "documents": [
    {"kind": "change", "title": "RTS 22 microsecond timestamps",
     "path": "changes/2026-08-rts22-precision.md",
     "purpose": "Records the amendment and why the specs must follow."},
    {"kind": "requirement", "title": "Microsecond execution timestamps",
     "path": "requirements/REQ-exec-timestamp-precision.md",
     "purpose": "States the precision the system must capture.", "linksTo": [0]},
    {"kind": "requirement", "title": "Timestamp validation on ingest",
     "path": "requirements/REQ-timestamp-validation.md",
     "purpose": "States what happens when precision is missing.", "linksTo": [0]}
  ]
}

Rules:
- "kind" MUST be one of the workspace's document families listed below, and
  "path" MUST live in that family's folder, following the naming of the
  documents already there.
- Propose the SMALLEST set that resolves the finding — often one document.
  Propose several only when the finding genuinely carries more than one
  requirement, or when a driver (a change/regulation) is needed above them.
- "linksTo" holds indices of OTHER documents in this same list that the
  document should point at, following the workspace's link types (the lower
  level points UP). Use "linksToDocument": true instead to point at the
  document the finding is about. Omit both when nothing applies — the server
  validates every link and drops the ones the model does not allow.
- "purpose" is one sentence saying what that document will state; the drafting
  pass uses it.
- Order the list so drivers come before the documents that cite them.`

// PlanPrompt asks which documents should be created for a finding, given the
// workspace's OWN families and link types.
func PlanPrompt(findingJSON, target, families, linkTypes, docIndex, guidance string) []Message {
	var b strings.Builder
	b.WriteString("# Finding to resolve\n```json\n" + findingJSON + "\n```\n")
	if target != "" {
		b.WriteString("\n# The document it is about\n" + target + "\n")
	}
	b.WriteString("\n# Document families available in this workspace\n" + families + "\n")
	b.WriteString("\n# Link types (the LOWER level carries the link UP)\n" + linkTypes + "\n")
	b.WriteString("\n# Existing documents\n" + docIndex + "\n")
	b.WriteString("\nPropose the documents to create, as JSON.")
	return []Message{
		{Role: "system", Content: planSystem + guidance},
		{Role: "user", Content: b.String()},
	}
}

const remedySystem = `You are the specquill remediation author. From a
confirmed source-alignment finding you draft the workspace document that
tracks fixing it — a change record (the WHY that drives the requirement
update) or a work item (the WHEN that schedules the work), as asked.

Reply with ONLY a JSON object, no prose:

{
  "path": "changes/2026-08-timestamp-precision.md",
  "content": "---\ntitle: ...\ntype: ...\nstatus: ...\n---\n\n# ...\n"
}

Rules:
- "path" MUST live in the family folder named below and follow the naming of
  the example document when one is given.
- Match the example's frontmatter shape (its 'type:' value, id scheme and
  fields) — those are this workspace's conventions, not yours to invent. The
  typed link back to the affected document is added by the server: do not
  invent link fields it did not ask for.
- The body states what must change and why, grounded in the finding's
  evidence: reference the affected document and the reference source, and
  keep acceptance criteria checkable. Never invent facts beyond the evidence.
- Write it ready for a human to refine, not as a placeholder.`

// RemedyPrompt builds the draft-the-remediation-document conversation.
// kindLabel/folder name the family to write into, linkNote explains the
// typed link the server will add, example is a same-family document (may be
// empty), guidance the workspace conventions.
func RemedyPrompt(kindLabel, folder, linkNote, findingJSON, target, example, guidance string) []Message {
	system := remedySystem +
		"\n\nWrite a " + kindLabel + " document in `" + folder + "`."
	if linkNote != "" {
		system += "\n" + linkNote
	}
	var b strings.Builder
	b.WriteString("# Finding to remediate\n```json\n" + findingJSON + "\n```\n")
	if target != "" {
		b.WriteString("\n# Affected document\n" + target + "\n")
	}
	if example != "" {
		b.WriteString("\n# Example " + kindLabel + " from this workspace (follow its conventions)\n" + example + "\n")
	}
	b.WriteString("\nDraft the " + kindLabel + " document as JSON.")
	return []Message{
		{Role: "system", Content: system + guidance},
		{Role: "user", Content: b.String()},
	}
}

const reverseSystem = `You are the specquill requirements reverse-engineer.
From a confirmed coverage gap (a capability a reference source implements that
no workspace document covers) you draft the MISSING requirement document.

Reply with ONLY a JSON object, no prose:

{
  "path": "requirements/REQ-report-submission.md",
  "content": "---\nid: REQ-...\ntitle: ...\ntype: requirement\nstatus: draft\n---\n\n# ...\n"
}

Rules:
- The document starts with complete frontmatter (id, title, type, status:
  draft, the family's upward links) and follows the workspace conventions
  described below.
- Write WHAT the system must do (observable behavior, constraints, acceptance
  criteria derived from the source evidence) — never HOW the source implements
  it, and never invented behavior the evidence does not support.
- Reference the evidence source honestly (e.g. a "Derived from" note naming
  the ~source path) so reviewers can trace every statement.
- "path" respects the suggested location unless it violates the conventions.`

// ReversePrompt builds the draft-the-missing-requirement conversation.
// findingJSON is the gap finding (anchor, detail, evidence), excerpts the
// quoted source files' content, guidance the workspace conventions
// (model rules, vocabulary, authoring rules).
func ReversePrompt(findingJSON, excerpts, guidance string) []Message {
	var b strings.Builder
	b.WriteString("# Coverage gap\n```json\n" + findingJSON + "\n```\n")
	b.WriteString("\n# Source material\n" + excerpts + "\n")
	b.WriteString("\nDraft the missing requirement document as JSON.")
	return []Message{
		{Role: "system", Content: reverseSystem + guidance},
		{Role: "user", Content: b.String()},
	}
}
