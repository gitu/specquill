// Guided authoring (the "spec wizard"): the prompt contracts behind the
// staged intent → related → interview → draft flow. Every stage answers with
// a JSON object (parsed via StreamToolsJSON) so the SPA can render structure —
// a rubric, question chips, section cards — instead of a wall of prose.
//
// These flows are READ-ONLY: they explore with read_file/list_files/search and
// never write. The document is created by the client through the normal file
// endpoint once the human accepts the draft, so an abandoned wizard leaves
// nothing behind in the worktree.
package ai

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultSections is the section outline a family's draft is written into
// when the workspace configures none. Keep the last entry a place for
// residual uncertainty — a spec that admits its gaps beats one that hides
// them behind confident prose.
var DefaultSections = map[string][]string{
	"requirement":  {"Context", "Requirement statements", "Acceptance criteria", "Traceability", "Open questions"},
	"spec":         {"Overview", "Behaviour", "Interfaces & data", "Edge cases", "Open questions"},
	"change":       {"Summary", "Driver", "Impact", "Required updates", "Open questions"},
	"regulation":   {"Summary", "Obligations", "Scope", "Affected documents", "Open questions"},
	"data_mapping": {"Overview", "Field mapping", "Transformations", "Validation", "Open questions"},
	"decision":     {"Context", "Decision", "Alternatives considered", "Consequences", "Open questions"},
}

var genericSections = []string{"Context", "Details", "Acceptance criteria", "Open questions"}

// SectionsFor resolves the outline for a family, falling back to a generic
// one. Callers normally pass the client's resolved template instead — this is
// the server-side floor for requests that omit it.
func SectionsFor(family string) []string {
	if s, ok := DefaultSections[family]; ok {
		return append([]string(nil), s...)
	}
	return append([]string(nil), genericSections...)
}

// altitudeRule turns the optional business/technical hint into one line of
// guidance. Anything else (including "") yields nothing — the family's
// authoring skill already sets the register.
func altitudeRule(altitude string) string {
	switch strings.ToLower(strings.TrimSpace(altitude)) {
	case "business":
		return "\nAltitude: BUSINESS. Write for non-technical stakeholders — the problem, the users, the value, the workflow, observable outcomes. No frameworks, class names, tables or endpoints unless the author supplied them.\n"
	case "technical":
		return "\nAltitude: TECHNICAL. Be concrete and grounded — name the real documents, fields, interfaces and constraints you found in the workspace and reference sources.\n"
	}
	return ""
}

// WizardContext is the stage-independent framing every wizard prompt opens
// with: what the human is trying to author, and where it will land.
type WizardContext struct {
	Intent   string
	Family   string // entity kind: requirement | spec | change | …
	Folder   string // target folder, e.g. "specs/"
	Altitude string // business | technical | ""
}

func (c WizardContext) brief() string {
	var b strings.Builder
	b.WriteString("\n# What the author is writing\n")
	fmt.Fprintf(&b, "Document family: %s\n", or(c.Family, "document"))
	if c.Folder != "" {
		fmt.Fprintf(&b, "Target folder: %s\n", c.Folder)
	}
	b.WriteString(altitudeRule(c.Altitude))
	if strings.TrimSpace(c.Intent) != "" {
		b.WriteString("\nTheir intent, in their own words:\n\"\"\"\n" + strings.TrimSpace(c.Intent) + "\n\"\"\"\n")
	}
	return b.String()
}

func or(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// InterviewRules drives the grilling turn: investigate, then ask the smallest
// set of sharp questions, and keep a running readiness rubric so the human can
// see how far off "draftable" they are.
func InterviewRules(c WizardContext, sections []string) string {
	return c.brief() + `
# Your job this turn
You are interviewing the author so the eventual draft is grounded and specific.

1. GROUND YOURSELF FIRST. Before asking anything, use search/list_files/read_file
   to find what the workspace and the reference sources already say about this —
   existing documents, conventions, ids, current behaviour. A question that cites
   what exists ("~backend retries 3 times today — should the spec keep that?")
   is worth five abstract ones.
2. ASK THE SMALLEST SET of questions that would actually change the draft. Never
   re-ask something the transcript already answers. Give each question 2-5
   CONCRETE options the author can just pick — options are the whole point, a
   question with none makes them compose prose. Ground the options in what you
   found: real values, real conventions, the behaviour that exists today.
   Include an option for the status quo when there is one. Omit options only
   when the answer is genuinely open (a name, a number you cannot bracket).
3. MAINTAIN THE RUBRIC: concrete criteria this specific document needs before it
   is worth drafting, each marked met or not. Criteria are about substance
   (scope, actors, edge cases, acceptance), never formatting. Keep the same
   criteria across turns — flip them to met as answers arrive, and only add one
   when the conversation reveals a genuinely new gap.
4. Set readyToDraft true only when the essentials are captured. NEVER on the
   first turn — you have only their rough idea then. If the author says "just
   draft it", "you decide" or similar, stop asking, set readyToDraft true, and
   plan to record your assumptions in the draft.

The draft will be written into these sections, so interview towards them: ` +
		strings.Join(sections, ", ") + `

# Reply format
Reply with ONLY a JSON object, no prose around it:

{
  "reply": "markdown shown to the author: what you found and what you still need. Concise — a few sentences.",
  "questions": [
    {"question": "Does seven years replace the five-year window, or run alongside it?",
     "options": ["Replaces it", "Runs alongside for existing records", "Only for trade records"]},
    {"question": "What should the retention clock start from?", "options": ["Execution date", "Booking date"]}
  ],
  "rubric": [{"criterion": "Retention period is stated as a number", "met": false}],
  "readyToDraft": false
}`
}

// ComposeRules writes the actual draft, one block per requested section.
func ComposeRules(c WizardContext, sections []string) string {
	return c.brief() + `
# Your job now
Write the document. Use the intent, the full interview transcript, and the
workspace/reference material (read_file, search) to fill each section.

Rules:
- Concrete and grounded. Reference real documents by path and requirements by id.
- Where a flow, state machine or interaction is genuinely branching, include a
  ` + "```mermaid```" + ` block — the editor renders them.
- Acceptance criteria must be specific and testable.
- Follow the authoring skills above for this family (statement style, RFC-2119
  keywords, frontmatter link fields).
- Keep each section tight. A section that genuinely does not apply gets one
  honest line, not padding.
- Record every assumption you had to make — in the section it affects, or under
  open questions. Do not launder a guess into a confident statement.
- Write section BODIES only: no "## " heading inside the content, no H1 title,
  no frontmatter. The workspace adds those.

Produce exactly these sections, in this order: ` + strings.Join(sections, ", ") + `

# Reply format
Reply with the document itself — markdown, nothing else. No JSON, no code
fence around the whole reply, no commentary before or after:

# <a short imperative document title>

## <the first requested section name>
<its body>

## <the next requested section name>
<its body>

Use the section names exactly as given. Markdown INSIDE a section is free —
tables, fenced code, quotes, backslashes — because nothing here is escaped.`
}

// SectionRules revises one section in isolation — the workbench affordance
// ("redraft", "tighten", or a free instruction) without a workbench.
func SectionRules(c WizardContext, title, section, content, instruction string) string {
	return c.brief() + `
# Your job now
Revise ONE section of the document "` + title + `", staying consistent with the
intent, the interview transcript and the rest of the document. You may use the
read tools to verify details before rewriting.

Section: ` + section + `

Current content:
"""
` + or(content, "(empty)") + `
"""

Instruction from the author: ` + instruction + `

Return the rewritten BODY only — no heading, no frontmatter.

# Reply format
One line saying what changed, then the body. Markdown, not JSON, so nothing
in the body needs escaping:

NOTE: <one line on what changed>
<the rewritten section body>`
}

// RelatedRules judges whether the workspace already covers the intent. The
// point is to stop duplicate documents being born, so it is deliberately
// conservative: an empty match list is the common, correct answer.
func RelatedRules(c WizardContext) string {
	return c.brief() + `
# Your job now
Decide whether this workspace ALREADY covers the author's intent, so they can
extend an existing document instead of creating a near-duplicate.

Search the workspace before judging — list_files to see what exists, search for
the intent's key terms, read_file the plausible hits. Judge on content, not on
filename similarity.

Relations:
- "covers"   — the existing document already describes what the intent asks for.
- "overlaps" — the intent would change or extend part of that document.
- "related"  — same functional area; useful context, but a separate concern.

Be conservative. When nothing is genuinely about the same thing, return an empty
list and "new" — a forced match costs the author more than a missed one. Only
return paths that actually exist in the workspace.

# Reply format
Reply with ONLY a JSON object, no prose around it:

{
  "matches": [{"path": "specs/retention.md", "title": "Retention rules", "relation": "overlaps", "reason": "one sentence citing the document"}],
  "recommendation": "specs/retention.md"
}

"recommendation" is the path of the document to extend, or "new".`
}

// TranscriptMessages builds the model conversation for a wizard stage: the
// intent as the opening user turn, then the interview transcript, then an
// optional closing instruction.
func TranscriptMessages(intent string, transcript []Message, final string) []Message {
	msgs := make([]Message, 0, len(transcript)+2)
	if strings.TrimSpace(intent) != "" {
		msgs = append(msgs, Message{Role: "user", Content: "Intent: " + strings.TrimSpace(intent)})
	}
	for _, m := range transcript {
		if m.Role != "user" && m.Role != "assistant" {
			continue // the wizard transcript is plain text turns only
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		msgs = append(msgs, Message{Role: m.Role, Content: m.Content})
	}
	if strings.TrimSpace(final) != "" {
		msgs = append(msgs, Message{Role: "user", Content: final})
	}
	if len(msgs) == 0 {
		msgs = append(msgs, Message{Role: "user", Content: "Begin."})
	}
	return msgs
}

// AssembleDocument renders a composed draft as the markdown body (H1 + one
// "## " block per section). Frontmatter is the client's business — it owns the
// id scheme and the family's link fields.
func AssembleDocument(title string, sections []Section) string {
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("# " + t + "\n")
	}
	for _, s := range sections {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		b.WriteString("\n## " + name + "\n\n")
		if c := strings.TrimSpace(s.Content); c != "" {
			b.WriteString(c + "\n")
		}
	}
	return b.String()
}

// Section is one named block of a composed draft.
type Section struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ParseDraft reads a composed reply back into title + sections. The compose
// stage asks for a markdown DOCUMENT rather than JSON: section bodies are long
// prose full of quotes, backslashes, table pipes and fences, and models get the
// escaping wrong often enough that a JSON envelope was the single biggest
// source of failed drafts (observed on gpt-5.5: "invalid character 'B'/'\\'
// looking for beginning of object key string"). Markdown needs no escaping, so
// the failure mode disappears rather than being retried.
//
// Tolerant by design: a leading fence is stripped, prose before the first
// heading is ignored, and a missing H1 just yields an empty title.
func ParseDraft(reply string) (title string, sections []Section) {
	body := strings.TrimSpace(reply)
	// a model that fences the whole document anyway
	if strings.HasPrefix(body, "```") {
		if nl := strings.IndexByte(body, '\n'); nl >= 0 {
			body = body[nl+1:]
		}
		if i := strings.LastIndex(body, "```"); i >= 0 {
			body = body[:i]
		}
	}
	var cur *Section
	var buf []string
	flush := func() {
		if cur != nil {
			cur.Content = strings.TrimSpace(strings.Join(buf, "\n"))
			sections = append(sections, *cur)
		}
		buf = nil
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(t, "## "); ok {
			flush()
			n := strings.TrimSpace(name)
			cur = &Section{Name: n}
			continue
		}
		if h1, ok := strings.CutPrefix(t, "# "); ok && cur == nil && title == "" {
			title = strings.TrimSpace(h1)
			continue
		}
		if cur != nil {
			buf = append(buf, line)
		}
	}
	flush()
	return title, sections
}

// ParseSectionReply splits a refinement reply into the rewritten body and the
// one-line note. Same reasoning as ParseDraft — the body is prose, so it is
// never escaped. The NOTE: line is only consumed when it opens the reply AND
// something follows it, so a body that merely starts with the word is kept.
func ParseSectionReply(reply string) (content, note string) {
	body := strings.TrimSpace(reply)
	if rest, ok := strings.CutPrefix(body, "NOTE:"); ok {
		line, after, found := strings.Cut(rest, "\n")
		if found && strings.TrimSpace(after) != "" {
			return strings.TrimSpace(after), strings.TrimSpace(line)
		}
	}
	return body, ""
}

// SortSectionsLike reorders (and completes) a model-returned section list to
// match the requested outline: models drop, rename-case and reorder blocks,
// and the UI is built around a stable outline. Unrequested extras are kept at
// the end rather than discarded — an unprompted "Assumptions" block is
// usually worth reading.
func SortSectionsLike(want []string, got []Section) []Section {
	byName := map[string]Section{}
	for _, s := range got {
		byName[strings.ToLower(strings.TrimSpace(s.Name))] = s
	}
	out := make([]Section, 0, len(want)+len(got))
	used := map[string]bool{}
	for _, name := range want {
		key := strings.ToLower(strings.TrimSpace(name))
		used[key] = true
		if s, ok := byName[key]; ok {
			out = append(out, Section{Name: name, Content: s.Content})
			continue
		}
		out = append(out, Section{Name: name})
	}
	extra := make([]string, 0, len(got))
	for key := range byName {
		if !used[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		out = append(out, byName[key])
	}
	return out
}
