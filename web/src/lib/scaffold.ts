// scaffold.ts — starter content for the optional .specquill/ files. A
// workspace without them runs entirely on built-in defaults; the sample
// config spells out the FULL default setup (the WHY → WHAT → HOW → WHEN
// model), so importing it changes nothing until the user starts editing.
// A unit test keeps the sample and the defaults in config.ts from drifting.

export function scaffoldConfigYml(project: string): string {
  return `# specquill workspace config — the workspace model in one file.
# EVERY section is optional: delete what you don't customize and the built-in
# defaults apply. This sample spells the defaults out in full, so importing
# it as-is changes nothing — edit from here.
#
# The model reads WHY → WHAT → HOW → WHEN: drivers explain WHY work exists,
# requirements say WHAT the product must do, specs say HOW it is realized,
# work items say WHEN it lands.
version: 2
project: ${project}

# view shown when opening the workspace
# (dashboard | editor | timed | changes | history | graph | model)
ui:
  default_view: editor

# ── document families ──────────────────────────────────────────────────────
# A document's family is decided by its frontmatter \`type:\` matching the
# family's \`doc_type\` (or the family key); \`folder\` is only the DEFAULT
# location new documents go — files keep their family wherever they live.
# \`group\` places a family on the model axis (why | what | how | when) — the
# lower level always links UP: WHAT documents cite WHY, HOW implements WHAT,
# WHEN delivers HOW. \`driver\` (WHY families) names the driver type documents
# of the family stand for; families without one derive it from each
# document's \`source:\` frontmatter.
# Add your own families, override single fields of a built-in (only the keys
# you provide change), or remove one with \`hidden: true\`. \`attributes\` seeds
# the frontmatter of documents created in the family; colors take any CSS
# color (hex or the app's var(--…) palette).
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
  # decision: { group: why, folder: "decisions/", label: "Decisions", icon: "◆", color: "#7c5cd6", description: "Why the system is shaped this way." }

# ── WHY: driver taxonomy ───────────────────────────────────────────────────
# The categories a driver can fall into (chips in the Properties panel and
# the dashboards). A \`drivers:\` link's type is DERIVED from the referenced
# document — its \`source:\` frontmatter, else its family's \`driver\` key —
# never written on the link itself.
drivers:
  regulatory: { label: "Regulatory", icon: "⚖", color: "#b06f16" }
  product:    { label: "Product",    icon: "◆", color: "#2563c9" }
  technical:  { label: "Technical",  icon: "⚙", color: "#5a616b" }

# document lifecycle states
statuses: [draft, in_review, approved, deprecated]

# ── linkage: the typed edges of the traceability graph ─────────────────────
# Chain links live on the LOWER level pointing up; backlinks are computed.
# \`inverse\` names the relation as read from the TARGET side (the Context
# panel shows "implemented by" on a requirement, not "implements").
link_types:
  drivers:    { from: requirement,            to: regulation,           inverse: "drives" }        # WHAT → WHY
  implements: { from: [spec, data_mapping],   to: requirement,          inverse: "implemented by" } # HOW → WHAT
  delivers:   { from: work_item,              to: [spec, requirement],  inverse: "delivered by" }  # WHEN → HOW (or WHAT directly)
  maps_to:    { from: spec,                   to: data_field,           inverse: "mapped by" }
  verifies:   { from: requirement,            to: test,                 inverse: "verified by" }

# ── traceability: the dashboard's health bars ──────────────────────────────
# One bar per entry. \`measure: from\` = share of source docs CARRYING the
# link; \`measure: to\` = share of target docs COVERED by it (computed from
# backlinks). The population is the first kind on the measured side;
# \`when\` hides the bar unless that entity exists. label/color optional.
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
# freely. \`ready_statuses\` decides when a dependent document counts as done
# in time; \`horizon_days\` is how far ahead "starting soon"/"expiring" reaches;
# \`kinds\` narrows the timeline to named families (empty = all).
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
#
# Every selected source is explorable by the AI assistant's chat tools
# (list_files / search / read_file as ~name/path); grounding: true
# ADDITIONALLY excerpts it into the prompt — right for small regulation
# texts, wrong for big codebases.
references: []
# references:
#   - source: regulations
#     paths: [regulations/]    # optional prefix filter
#     grounding: true          # excerpt into the prompt (tools reach it either way)
#   - source: backend          # implementation repo: tools-only, no prompt-stuffing

# forge-PAT mode only — define the reference repos in-repo (https remotes on
# allowlisted hosts; fetched per user with their own token):
# sources:
#   - name: regulations
#     remote: https://git.example.com/acme/regulations.git
#     default_branch: main

# Extra rules appended to the Speccy system prompt (structure/content
# expectations). Longer-form guidance belongs in .specquill/instructions.md;
# durable decisions the assistant records live in .specquill/memory/ (one
# decision per file — keep that shape for conflict-free merges).
# speccy:
#   instructions: |
#     Every requirement gets a rationale paragraph before its statements.
`;
}

export function scaffoldInstructionsMd(): string {
  return `---
type: Instructions
title: Workspace instructions
---

# Workspace instructions

Rules the AI assistant follows for documents in THIS workspace — extend them
with your team's structure and content expectations. Examples to adapt:

- Every requirement gets a rationale paragraph before its normative statements.
- Specs describe current behavior only; planned work belongs in work items.
- Use tables for field mappings, mermaid flowcharts for branching flows.
`;
}

/** Starter content for a missing workspace file, or null if it has none. */
export function scaffoldFor(path: string, project: string): string | null {
  if (path === '.specquill/config.yml') return scaffoldConfigYml(project);
  if (path === '.specquill/instructions.md') return scaffoldInstructionsMd();
  return null;
}
