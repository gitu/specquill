package scaffold

// Document-type registry: folders, starter documents, and the AI authoring
// skills the copilot loads from .reqbase/skills/.

var types = map[string]SpecType{
	"requirements": {
		Key: "requirements", Dir: "requirements", Title: "Requirements (REQ-*)",
		Starter: `---
id: REQ-001
title: Example requirement
status: draft
priority: must
owner: unassigned
drivers: []
implements: []
verifies: []
---

# Example requirement

The system MUST demonstrate what a well-formed requirement looks like.

> **REQ-001.1 · MUST** — Each requirement SHALL contain at least one atomic,
> testable statement using RFC-2119 language.
`,
		Skill: `# Skill: writing requirements

When asked to draft or edit a requirement (requirements/REQ-*.md):

- One requirement per file, id ` + "`REQ-<nnn>`" + `; the frontmatter id, filename and title heading agree.
- Frontmatter: id, title, status (draft|review|approved), priority (must|should|could), owner, and the traceability links — drivers (regulations/change records that motivate it), implements (specs realizing it), verifies (test artifacts).
- Body: one short context paragraph, then atomic sub-requirements as blockquotes: "**REQ-<nnn>.<m> · MUST** — <single testable statement>" using RFC-2119 keywords (MUST/SHALL/SHOULD/MAY).
- Each statement is verifiable: no "user-friendly", "fast", "appropriate" without a measurable bound.
- Never invent regulation references — link only files that exist in the workspace.
`,
	},
	"specs": {
		Key: "specs", Dir: "specs", Title: "Specifications",
		Starter: `---
title: Example specification
status: draft
satisfies: []
---

# Example specification

Describes HOW requirements are realized. Link the requirements this spec
satisfies in the frontmatter; requirements point back via implements.
`,
		Skill: `# Skill: writing specifications

When asked to draft or edit a spec (specs/*.md):

- A spec describes HOW one or more requirements are realized — mechanisms, flows, formats, interfaces. Keep normative language out; the WHAT lives in requirements.
- Frontmatter: title, status, satisfies (list of requirement files/ids this spec realizes).
- Structure: overview paragraph → behavior sections → edge cases. Prefer a mermaid flowchart for branching flows and tables for field/format definitions.
- When a spec changes behavior a requirement depends on, call out the affected REQ ids so reviewers see the blast radius.
`,
	},
	"regulations": {
		Key: "regulations", Dir: "regulations", Title: "Regulations & external drivers (often a read-only reference repo)",
		Skill: `# Skill: referencing regulations

When working with regulations/*.md:

- Regulation files are reference material — quote and link them (path#anchor), never rewrite their normative text.
- Requirements cite them via the drivers frontmatter list with type: regulatory and a ref like regulations/<file>.md#<article-anchor>.
- When summarizing a regulatory change, list the driven requirements and where their coverage stands.
`,
	},
	"data-mappings": {
		Key: "data-mappings", Dir: "data-mappings", Title: "Data mappings (field-level lineage)",
		Starter: `---
title: Example entity mapping
entity: example
---

# Example entity mapping

| field | source | target | rule |
|---|---|---|---|
| example.id | upstream.id | report.ID | copy |
`,
		Skill: `# Skill: writing data mappings

When asked to draft or edit a data mapping (data-mappings/*.md):

- One entity per file; a table with field, source, target and transformation rule columns.
- Field names are referenced from requirements via maps_to links — keep them stable; renames are breaking changes worth a change record.
- Every transformation rule is deterministic and testable; mark lossy or defaulted mappings explicitly.
`,
	},
	"changes": {
		Key: "changes", Dir: "changes", Title: "Change records (incoming deltas: regulatory, product, technical)",
		Starter: `---
title: Example change record
status: triage
source: product
---

# Example change record

What changed upstream, which requirements/specs/mappings it reaches, and the
decision taken. Change records drive the change inbox on the dashboard.
`,
		Skill: `# Skill: writing change records

When asked to draft a change record (changes/*.md):

- Name files <yyyy-mm>-<slug>.md. Frontmatter: title, status (triage|in_progress|done), source (regulatory|product|technical).
- Body answers three questions: what changed, what it reaches (list affected requirement/spec/mapping paths), what we decided.
- Keep the impact list honest — it feeds the traceability graph; do not pad it.
`,
	},
	"decisions": {
		Key: "decisions", Dir: "decisions", Title: "Decision records (ADRs)",
		Starter: `---
id: ADR-001
title: Example decision
status: accepted
---

# Example decision

## Context

## Decision

## Consequences
`,
		Skill: `# Skill: writing decision records

When asked to draft an ADR (decisions/ADR-*.md):

- Frontmatter: id ADR-<nnn>, title, status (proposed|accepted|superseded).
- Sections: Context (forces at play), Decision (one clear choice, active voice), Consequences (what becomes easier/harder).
- Supersede rather than edit: a changed decision is a new ADR linking the old one.
`,
	},
	"glossary": {
		Key: "glossary", Dir: "glossary", Title: "Glossary (shared vocabulary)",
		Starter: `---
title: Glossary
---

# Glossary

**Term** — definition. Keep one canonical definition per term; requirements
and specs link here instead of redefining.
`,
		Skill: `# Skill: maintaining the glossary

- One canonical definition per term; when documents disagree with the glossary, the glossary wins and the documents get fixed.
- Definitions are one or two sentences, no circularity, no examples baked in.
- When drafting requirements that introduce a new term, add it to the glossary in the same change.
`,
	},
}

const schemaJSON = `{
  "order": ["id", "status", "priority", "owner", "drivers", "implements", "satisfies", "maps_to", "verifies", "updated"],
  "fields": {
    "id": { "label": "ID", "type": "mono" },
    "status": { "label": "Status", "type": "badge", "values": { "draft": "reg", "review": "prod", "approved": "data", "accepted": "data", "triage": "reg", "superseded": "text" } },
    "priority": { "label": "Priority", "type": "badge", "values": { "must": "reg", "should": "prod", "could": "text" } },
    "owner": { "label": "Owner", "type": "text" },
    "drivers": { "label": "Drivers", "type": "links" },
    "implements": { "label": "Implements", "type": "links" },
    "satisfies": { "label": "Satisfies", "type": "links" },
    "maps_to": { "label": "Maps to", "type": "links" },
    "verifies": { "label": "Verified by", "type": "links" },
    "updated": { "label": "Updated", "type": "date" }
  }
}
`

const authoringSkill = `# Skill: authoring in this workspace

General rules the AI follows for every document it drafts or edits here:

- Plain markdown with YAML frontmatter; typed links (drivers, implements, satisfies, maps_to, verifies) build the traceability graph — keep them accurate and minimal.
- Reference other documents by relative path (e.g. ../specs/example.md); never link files that do not exist.
- RFC-2119 keywords (MUST/SHALL/SHOULD/MAY) appear only in requirements, and only in their normative statements.
- Edits are surgical: preserve the author's structure and wording outside the requested change.
- When unsure which document type a change belongs to, prefer a change record and list the documents it will touch.
`
