package scaffold

// Document-type registry: folders, starter documents, and the AI authoring
// skills the speccy loads from .specquill/skills/.

var types = map[string]SpecType{
	"requirements": {
		Key: "requirements", Dir: "requirements", Title: "Requirements (REQ-*)",
		Starter: `---
type: Requirement
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
		Skill: `---
type: Skill
title: Writing requirements
---

# Skill: writing requirements

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
type: Specification
title: Example specification
status: draft
satisfies: []
---

# Example specification

Describes HOW requirements are realized. Link the requirements this spec
satisfies in the frontmatter; requirements point back via implements.
`,
		Skill: `---
type: Skill
title: Writing specifications
---

# Skill: writing specifications

When asked to draft or edit a spec (specs/*.md):

- A spec describes HOW one or more requirements are realized — mechanisms, flows, formats, interfaces. Keep normative language out; the WHAT lives in requirements.
- Frontmatter: title, status, satisfies (list of requirement files/ids this spec realizes).
- Structure: overview paragraph → behavior sections → edge cases. Prefer a mermaid flowchart for branching flows and tables for field/format definitions.
- When a spec changes behavior a requirement depends on, call out the affected REQ ids so reviewers see the blast radius.
`,
	},
	"regulations": {
		Key: "regulations", Dir: "regulations", Title: "Regulations & external drivers (often a read-only reference repo)",
		Skill: `---
type: Skill
title: Referencing regulations
---

# Skill: referencing regulations

When working with regulations/*.md:

- Regulation files are reference material — quote and link them (path#anchor), never rewrite their normative text.
- Requirements cite them via the drivers frontmatter list with type: regulatory and a ref like regulations/<file>.md#<article-anchor>.
- When summarizing a regulatory change, list the driven requirements and where their coverage stands.
`,
	},
	"data-mappings": {
		Key: "data-mappings", Dir: "data-mappings", Title: "Data mappings (field-level lineage)",
		Starter: `---
type: Data Mapping
title: Example entity mapping
entity: example
resource: ""            # URI of the underlying asset, e.g. bigquery://project/dataset/table
---

# Example entity mapping

| field | source | target | rule |
|---|---|---|---|
| example.id | upstream.id | report.ID | copy |
`,
		Skill: `---
type: Skill
title: Writing data mappings
---

# Skill: writing data mappings

When asked to draft or edit a data mapping (data-mappings/*.md):

- One entity per file; a table with field, source, target and transformation rule columns.
- Field names are referenced from requirements via maps_to links — keep them stable; renames are breaking changes worth a change record.
- Every transformation rule is deterministic and testable; mark lossy or defaulted mappings explicitly.
`,
	},
	"changes": {
		Key: "changes", Dir: "changes", Title: "Change records (incoming deltas: regulatory, product, technical)",
		Starter: `---
type: Change Record
title: Example change record
status: triage
source: product
---

# Example change record

What changed upstream, which requirements/specs/mappings it reaches, and the
decision taken. Change records drive the change inbox on the dashboard.
`,
		Skill: `---
type: Skill
title: Writing change records
---

# Skill: writing change records

When asked to draft a change record (changes/*.md):

- Name files <yyyy-mm>-<slug>.md. Frontmatter: title, status (triage|in_progress|done), source (regulatory|product|technical).
- Body answers three questions: what changed, what it reaches (list affected requirement/spec/mapping paths), what we decided.
- Keep the impact list honest — it feeds the traceability graph; do not pad it.
`,
	},
	"work-items": {
		Key: "work-items", Dir: "work-items", Title: "Work items (WHEN work lands)",
		Starter: `---
type: Work Item
id: WI-001
title: Example work item
status: backlog
priority: should
owner: unassigned
delivers: []
---

# Example work item

WHEN work lands: a planned unit of delivery. Link the requirements and specs
this item delivers in the frontmatter and track it from backlog to done.
`,
		Skill: `---
type: Skill
title: Writing work items
---

# Skill: writing work items

When asked to draft or edit a work item (work-items/WI-*.md):

- One deliverable per file, id WI-<nnn>; frontmatter: id, title, status (backlog|in_progress|done), priority, owner, delivers (the requirements/specs this item ships), due (yyyy-mm-dd, optional).
- The body answers: what ships, why now (link the driving change records), and how we know it is done (acceptance checks referencing the linked requirements).
- Keep delivers accurate — it is the WHEN edge of the traceability graph; do not pad it.
`,
	},
	"decisions": {
		Key: "decisions", Dir: "decisions", Title: "Decision records (ADRs)",
		Starter: `---
type: Decision
id: ADR-001
title: Example decision
status: accepted
---

# Example decision

## Context

## Decision

## Consequences
`,
		Skill: `---
type: Skill
title: Writing decision records
---

# Skill: writing decision records

When asked to draft an ADR (decisions/ADR-*.md):

- Frontmatter: id ADR-<nnn>, title, status (proposed|accepted|superseded).
- Sections: Context (forces at play), Decision (one clear choice, active voice), Consequences (what becomes easier/harder).
- Supersede rather than edit: a changed decision is a new ADR linking the old one.
`,
	},
	"glossary": {
		Key: "glossary", Dir: "glossary", Title: "Glossary (shared vocabulary)",
		Starter: `---
type: Glossary
title: Glossary
---

# Glossary

**Term** — definition. Keep one canonical definition per term; requirements
and specs link here instead of redefining.
`,
		Skill: `---
type: Skill
title: Maintaining the glossary
---

# Skill: maintaining the glossary

- One canonical definition per term; when documents disagree with the glossary, the glossary wins and the documents get fixed.
- Definitions are one or two sentences, no circularity, no examples baked in.
- When drafting requirements that introduce a new term, add it to the glossary in the same change.
`,
	},
}

// configStarter is the combined workspace config: the WHY/WHAT/HOW/WHEN model
// (entities, drivers, statuses, link types, ID schemes) plus the property
// schema that used to live in .specquill/schema.json. Every section is
// optional and spells out the built-in default, so the file changes nothing
// until edited. MIRRORS web/src/lib/scaffold.ts scaffoldConfigYml — keep the
// two in sync (the web side carries the drift test against its defaults).
func configStarter(project string) string {
	return `# specquill workspace config — the workspace model in one file.
# EVERY section is optional: delete what you don't customize and the built-in
# defaults apply. This sample spells the defaults out in full, so importing
# it as-is changes nothing — edit from here.
#
# The model reads WHY -> WHAT -> HOW -> WHEN: drivers explain WHY work exists,
# requirements say WHAT the product must do, specs say HOW it is realized,
# work items say WHEN it lands.
version: 2
project: ` + project + `

# view shown when opening the workspace (dashboard | editor | changes | graph | model)
ui:
  default_view: editor

# ── document families ──────────────────────────────────────────────────────
# "group" places a family on the model axis (why | what | how | when).
# Add your own families, override single fields of a built-in (only the keys
# you provide change), or remove one with "hidden: true". "attributes" seeds
# the frontmatter of documents created in the family.
entities:
  regulation:
    group: why
    folder: "regulations/"
    label: "Regulations"
    icon: "◈"
    color: "var(--reg)"
    attributes: [id, title, status]
    description: "External rules the product must comply with — the origin of regulatory drivers and change records."
  change:
    group: why
    folder: "changes/"
    label: "Changes"
    icon: "⚑"
    color: "var(--reg)"
    attributes: [title, status, source, published]
    description: "Incoming change records (regulatory, product, technical) triaged against the documents they impact."
  requirement:
    group: what
    folder: "requirements/"
    label: "Requirements"
    icon: "▤"
    color: "var(--prod)"
    attributes: [id, title, status, priority, owner, drivers, implements, verifies]
    description: "WHAT the product must do — atomic, testable statements carrying drivers and traceability links."
  spec:
    group: how
    folder: "specs/"
    label: "Specs"
    icon: "◈"
    color: "var(--text-2)"
    attributes: [title, status, satisfies, maps_to]
    description: "HOW requirements are realized — designs that satisfy requirements and map onto data fields."
  data_mapping:
    group: how
    folder: "data-mappings/"
    label: "Data mappings"
    icon: "⇄"
    color: "var(--data)"
    attributes: [title]
    description: "Field-level source → target mappings; drift against the specs is detected here."
  diagram:
    group: how
    folder: "diagrams/"
    label: "Diagrams"
    icon: "✎"
    color: "var(--ai)"
    attributes: []
    description: "Sketches and text diagrams embedded in documents — portable formats, no tool lock-in."
  work_item:
    group: when
    folder: "work-items/"
    label: "Work items"
    icon: "⧗"
    color: "var(--data)"
    attributes: [id, title, status, priority, owner, delivers, due]
    description: "WHEN work lands — planned units of delivery that schedule requirements and specs from backlog to done."

# ── WHY: driver taxonomy ───────────────────────────────────────────────────
# The categories behind driver frontmatter entries — what a requirement can
# be driven by (chips in the Properties panel and the dashboards).
drivers:
  regulatory: { label: "Regulatory", icon: "⚖", color: "#b06f16" }
  product:    { label: "Product",    icon: "◆", color: "#2563c9" }
  technical:  { label: "Technical",  icon: "⚙", color: "#5a616b" }

# document lifecycle states
statuses: [draft, in_review, approved, deprecated]

# ── linkage: the typed edges of the traceability graph ─────────────────────
link_types:
  drives:     { from: [regulation, change], to: requirement }         # WHY -> WHAT
  implements: { from: requirement,          to: spec }                # WHAT -> HOW
  satisfies:  { from: spec,                 to: requirement }         # HOW -> WHAT
  delivers:   { from: work_item,            to: [requirement, spec] } # WHEN -> WHAT/HOW
  maps_to:    { from: spec,                 to: data_field }
  verifies:   { from: test,                 to: requirement }

# ── ID schemes for new documents ───────────────────────────────────────────
# Tokens: {seq} / {seq:N} (next number in the family, zero-padded), {rand:N}
# digits, {hex:N}, {adj} {word} (memorable pairs like "brisk-heron"), {yy}
# {yyyy}, {slug} (from the title). Families without a scheme use built-ins
# (REQ-/REG-/CHG-/MAP-/WI-/ADR-{seq:3}) or {slug}.
ids:
  requirement:  { pattern: "REQ-{seq:3}" }
  regulation:   { pattern: "REG-{seq:3}" }
  change:       { pattern: "CHG-{seq:3}" }
  data_mapping: { pattern: "MAP-{seq:3}" }
  work_item:    { pattern: "WI-{seq:3}" }

# ── attributes: the property schema ────────────────────────────────────────
# Frontmatter fields — labels, types and enum colors drive the Properties
# panel. Types: text | code | tag | user | date | enum | percent | links.
# Enum colors: green | blue | amber | violet | slate.
# (Replaces the former .specquill/schema.json, which is still honored while
# this section is absent.)
properties:
  order: [id, type, status, priority, owner, source, due, drivers, implements, satisfies, delivers, maps_to, verifies, created, updated]
  fields:
    id:         { label: "ID", type: code }
    type:       { label: "Type", type: tag }
    status:     { label: "Status", type: enum, values: { draft: slate, in_review: amber, approved: green, deprecated: slate, triage: amber, backlog: slate, in_progress: blue, done: green, active: green } }
    priority:   { label: "Priority", type: enum, values: { must: amber, should: blue, could: slate } }
    owner:      { label: "Owner", type: user }
    source:     { label: "Source", type: enum, values: { regulatory: amber, product: blue, technical: slate } }
    due:        { label: "Due", type: date }
    drivers:    { label: "Drivers", type: links }
    implements: { label: "Implements", type: links }
    satisfies:  { label: "Satisfies", type: links }
    delivers:   { label: "Delivers", type: links }
    maps_to:    { label: "Maps to", type: links }
    verifies:   { label: "Verified by", type: links }
    created:    { label: "Created", type: date }
    updated:    { label: "Updated", type: date }

# ── read-only reference sources ────────────────────────────────────────────
# Local-auth deployments: names must exist in the server's catalog (selection
# can never mint access). Forge-PAT deployments: define the repos here under
# sources: instead — each user fetches them with their own token.
references: []
# references:
#   - source: regulations
#     paths: [regulations/]    # optional prefix filter
#     grounding: true          # excerpt into the AI prompt (tools reach it either way)

# forge-PAT mode only — define the reference repos in-repo (https remotes on
# allowlisted hosts; fetched per user with their own token):
# sources:
#   - name: regulations
#     remote: https://git.example.com/acme/regulations.git
#     default_branch: main

# Extra rules appended to the Speccy system prompt (structure/content
# expectations). Longer-form guidance belongs in .specquill/instructions.md.
# speccy:
#   instructions: |
#     Every requirement gets a rationale paragraph before its statements.
`
}

// instructionsStarter seeds .specquill/instructions.md — the workspace's own
// structure/content expectations, pinned into every speccy prompt alongside
// the skills (config.yml speccy.instructions is the short inline companion).
const instructionsStarter = `---
type: Instructions
title: Workspace instructions
---

# Workspace instructions

Rules the AI assistant follows for documents in THIS workspace — extend them
with your team's structure and content expectations. Examples to adapt:

- Every requirement gets a rationale paragraph before its normative statements.
- Specs describe current behavior only; planned work belongs in change records.
- Use tables for field mappings, mermaid flowcharts for branching flows.
`

const authoringSkill = `---
type: Skill
title: Authoring in this workspace
---

# Skill: authoring in this workspace

General rules the AI follows for every document it drafts or edits here:

- Plain markdown with YAML frontmatter; typed links (drivers, implements, satisfies, delivers, maps_to, verifies) build the traceability graph — keep them accurate and minimal.
- Reference other documents by relative path (e.g. ../specs/example.md); never link files that do not exist.
- RFC-2119 keywords (MUST/SHALL/SHOULD/MAY) appear only in requirements, and only in their normative statements.
- Edits are surgical: preserve the author's structure and wording outside the requested change.
- When unsure which document type a change belongs to, prefer a change record and list the documents it will touch.
`
