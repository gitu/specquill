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
- Frontmatter: id, title, status (draft|review|approved), priority (must|should|could), owner, and drivers — the WHY documents (regulations/change records) that motivate it, as a plain path list. Specs point back via their implements list.
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
implements: []
---

# Example specification

Describes HOW requirements are realized. Link the requirements this spec
implements in the frontmatter; backlinks on the requirement are computed.
`,
		Skill: `---
type: Skill
title: Writing specifications
---

# Skill: writing specifications

When asked to draft or edit a spec (specs/*.md):

- A spec describes HOW one or more requirements are realized — mechanisms, flows, formats, interfaces. Keep normative language out; the WHAT lives in requirements.
- Frontmatter: title, status, implements (list of requirement files this spec realizes).
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
- Requirements cite them via the drivers frontmatter list as plain paths like regulations/<file>.md#<article-anchor>; the regulatory type is derived from the target.
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
	// Optional family — nothing depends on it. What changed in the workspace
	// is read from git history (the Changes/History views); a change record
	// is for tracking an EXTERNAL delta a team wants a document for.
	"changes": {
		Key: "changes", Dir: "changes", Title: "Change records (optional: external deltas worth their own document)",
		Starter: `---
type: Change Record
title: Example change record
status: triage
source: product
---

# Example change record

What changed upstream, which requirements/specs/mappings it reaches, and the
decision taken. Declare the family in .specquill/config.yml (entities:) to give
these documents their own folder and icon.
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

# view shown when opening the workspace
# (dashboard | editor | timed | changes | history | graph | model)
ui:
  default_view: editor

# ── document families ──────────────────────────────────────────────────────
# A document's family is decided by its frontmatter "type:" matching the
# family's "doc_type" (or the family key); "folder" is only the DEFAULT
# location new documents go — files keep their family wherever they live.
# "group" places a family on the model axis (why | what | how | when) — the
# lower level always links UP: WHAT documents cite WHY, HOW implements WHAT,
# WHEN delivers HOW. "driver" (WHY families) names the driver type documents
# of the family stand for; families without one derive it from each
# document's "source:" frontmatter.
# Add your own families, override single fields of a built-in (only the keys
# you provide change), or remove one with "hidden: true". "attributes" seeds
# the frontmatter of documents created in the family.
entities:
  regulation:
    doc_type: "Regulation"
    group: why
    driver: regulatory
    folder: "regulations/"
    label: "Regulations"
    icon: "◈"
    color: "var(--reg)"
    attributes: [id, title, status]
    description: "External rules the product must comply with — the origin of regulatory drivers."
  requirement:
    doc_type: "Requirement"
    group: what
    folder: "requirements/"
    label: "Requirements"
    icon: "▤"
    color: "var(--prod)"
    attributes: [id, title, status, priority, owner, drivers]
    description: "WHAT the product must do — atomic, testable statements citing the WHY documents that drive them."
  spec:
    doc_type: "Specification"
    group: how
    folder: "specs/"
    label: "Specs"
    icon: "◈"
    color: "var(--text-2)"
    attributes: [title, status, implements, maps_to]
    description: "HOW requirements are realized — designs that implement requirements and map onto data fields."
  data_mapping:
    doc_type: "Data Mapping"
    group: how
    folder: "data-mappings/"
    label: "Data mappings"
    icon: "⇄"
    color: "var(--data)"
    attributes: [title]
    description: "Field-level source → target mappings; drift against the specs is detected here."
  diagram:
    doc_type: "Diagram"
    group: how
    folder: "diagrams/"
    label: "Diagrams"
    icon: "✎"
    color: "var(--ai)"
    attributes: []
    description: "Sketches and text diagrams embedded in documents — portable formats, no tool lock-in."
  work_item:
    doc_type: "Work Item"
    group: when
    folder: "work-items/"
    label: "Work items"
    icon: "⧗"
    color: "var(--data)"
    attributes: [id, title, status, priority, owner, delivers, due]
    description: "WHEN work lands — planned units of delivery that schedule requirements and specs from backlog to done."

# ── WHY: driver taxonomy ───────────────────────────────────────────────────
# The categories a driver can fall into (chips in the Properties panel and
# the dashboards). A drivers link's type is DERIVED from the referenced
# document — its "source:" frontmatter, else its family's "driver" key —
# never written on the link itself.
drivers:
  regulatory: { label: "Regulatory", icon: "⚖", color: "#b06f16" }
  product:    { label: "Product",    icon: "◆", color: "#2563c9" }
  technical:  { label: "Technical",  icon: "⚙", color: "#5a616b" }

# document lifecycle states
statuses: [draft, in_review, approved, deprecated]

# ── linkage: the typed edges of the traceability graph ─────────────────────
# Chain links live on the LOWER level pointing up; backlinks are computed.
# "inverse" names the relation as read from the TARGET side (the Context
# panel shows "implemented by" on a requirement, not "implements").
link_types:
  drivers:    { from: requirement,            to: regulation,           inverse: "drives" }        # WHAT -> WHY
  implements: { from: [spec, data_mapping],   to: requirement,          inverse: "implemented by" } # HOW -> WHAT
  delivers:   { from: work_item,              to: [spec, requirement],  inverse: "delivered by" }  # WHEN -> HOW (or WHAT directly)
  maps_to:    { from: spec,                   to: data_field,           inverse: "mapped by" }
  verifies:   { from: requirement,            to: test,                 inverse: "verified by" }

# ── traceability: the dashboard's health bars ──────────────────────────────
# One bar per entry. "measure: from" = share of source docs CARRYING the
# link; "measure: to" = share of target docs COVERED by it (computed from
# backlinks). The population is the first kind on the measured side;
# "when" hides the bar unless that entity exists. label/color optional.
traceability:
  - { link: drivers,    measure: from }
  - { link: implements, measure: to }
  - { link: delivers,   measure: to }
  - { link: maps_to,    measure: from, label: "Specs → data fields", when: data_mapping }

# ── timed dependencies: documents with a validity window ───────────────────
# Any document carrying one of these frontmatter keys joins the Timed view,
# whatever family it belongs to — a regulation that comes into force, a
# requirement that only applies for a season, a work item with a due date.
# The FIRST key present wins, so regulatory wording and plain starts/ends mix
# freely. "ready_statuses" decides when a dependent document counts as done
# in time; "horizon_days" is how far ahead "starting soon"/"expiring" reaches;
# "kinds" narrows the timeline to named families (empty = all).
timed:
  start: [starts, effective_from, valid_from]
  end: [ends, effective_until, valid_until, due]
  ready_statuses: [approved, done]
  horizon_days: 90
  kinds: []

# ── ID schemes for new documents ───────────────────────────────────────────
# Tokens: {seq} / {seq:N} (next number in the family, zero-padded), {rand:N}
# digits, {hex:N}, {adj} {word} (memorable pairs like "brisk-heron"), {yy}
# {yyyy}, {slug} (from the title). Families without a scheme use built-ins
# (REQ-/REG-/CHG-/MAP-/WI-/ADR-{seq:3}) or {slug}.
ids:
  requirement:  { pattern: "REQ-{seq:3}" }
  regulation:   { pattern: "REG-{seq:3}" }
  data_mapping: { pattern: "MAP-{seq:3}" }
  work_item:    { pattern: "WI-{seq:3}" }
  # change:     { pattern: "CHG-{seq:3}" }   # if you add a change-record family

# ── attributes: the property schema ────────────────────────────────────────
# Frontmatter fields — labels, types and enum colors drive the Properties
# panel. Types: text | code | tag | user | date | enum | percent | links.
# Enum colors: green | blue | amber | violet | slate.
# (Replaces the former .specquill/schema.json, which is still honored while
# this section is absent.)
properties:
  order: [id, type, status, priority, owner, source, starts, ends, due, drivers, implements, delivers, maps_to, verifies, created, updated]
  fields:
    id:         { label: "ID", type: code }
    type:       { label: "Type", type: tag }
    status:     { label: "Status", type: enum, values: { draft: slate, in_review: amber, approved: green, deprecated: slate, triage: amber, backlog: slate, in_progress: blue, done: green, active: green } }
    priority:   { label: "Priority", type: enum, values: { must: amber, should: blue, could: slate } }
    owner:      { label: "Owner", type: user }
    source:     { label: "Source", type: enum, values: { regulatory: amber, product: blue, technical: slate } }
    starts:     { label: "Starts", type: date }
    ends:       { label: "Ends", type: date }
    due:        { label: "Due", type: date }
    drivers:    { label: "Drivers", type: links }
    implements: { label: "Implements", type: links }
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
- Specs describe current behavior only; planned work belongs in work items.
- Use tables for field mappings, mermaid flowcharts for branching flows.
`

const authoringSkill = `---
type: Skill
title: Authoring in this workspace
---

# Skill: authoring in this workspace

General rules the AI follows for every document it drafts or edits here:

- Plain markdown with YAML frontmatter; typed links (drivers, implements, delivers, maps_to, verifies) build the traceability graph — keep them accurate and minimal.
- Reference other documents by relative path (e.g. ../specs/example.md); never link files that do not exist.
- RFC-2119 keywords (MUST/SHALL/SHOULD/MAY) appear only in requirements, and only in their normative statements.
- Edits are surgical: preserve the author's structure and wording outside the requested change.
- When unsure which document type a change belongs to, prefer a change record and list the documents it will touch.
`

// documentModelSkill spells out the WHY ← WHAT ← HOW ← WHEN chain and the
// create / ensure / migrate playbooks the user can invoke by name in chat.
// Deliberately compact — skills are pinned into every speccy prompt.
const documentModelSkill = `---
type: Skill
title: Document model — create, ensure, migrate
---

# Skill: the document model (WHY ← WHAT ← HOW ← WHEN)

The lower level always carries the frontmatter link UP: requirements cite
their WHY docs in ` + "`drivers:`" + `, specs cite requirements in ` + "`implements:`" + `,
work items cite specs (or requirements) in ` + "`delivers:`" + `. All link values are
plain root-relative path lists — a driver's type (regulatory/product/…) is
derived from the referenced document, never written on the link.

## Create
Place the document in its family folder, seed the family's attributes, and
set the upward link to a REAL upper-level document — list_files/search first
to find it, ask_user when ambiguous. Never invent target paths.

## Ensure ("audit the model")
Walk the workspace and report per level: documents missing their upward link,
links whose target is the wrong kind, and legacy shapes (` + "`{type, ref}`" + ` driver
maps, ` + "`satisfies:`" + `, ` + "`implements:`" + ` on requirements). Report first; fix only on
request.

## Migrate
Per file, as uncommitted drafts the user reviews: flatten driver maps to path
lists (drop the type — it derives from the target); move ` + "`implements:`" + ` values
found on requirements onto the referenced spec's ` + "`implements:`" + ` (merge, dedupe)
and delete the requirement-side field; rename ` + "`satisfies:`" + ` to ` + "`implements:`" + `;
move_file stray documents into their family's folder (the folder is the
default location; the frontmatter type decides the family). ask_user before
any delete_file. Work in small batches and summarize what changed.
`
