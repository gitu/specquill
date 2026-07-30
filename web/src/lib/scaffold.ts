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

# view shown when opening the workspace (dashboard | editor | changes | graph | model)
ui:
  default_view: editor

# ── document families ──────────────────────────────────────────────────────
# \`group\` places a family on the model axis (why | what | how | when).
# Add your own families, override single fields of a built-in (only the keys
# you provide change), or remove one with \`hidden: true\`. \`attributes\` seeds
# the frontmatter of documents created in the family; colors take any CSS
# color (hex or the app's var(--…) palette).
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
  # decision: { group: why, folder: "decisions/", label: "Decisions", icon: "◆", color: "#7c5cd6", description: "Why the system is shaped this way." }

# ── WHY: driver taxonomy ───────────────────────────────────────────────────
# The categories behind \`drivers:\` frontmatter entries — what a requirement
# can be driven by (chips in the Properties panel and the dashboards).
drivers:
  regulatory: { label: "Regulatory", icon: "⚖", color: "#b06f16" }
  product:    { label: "Product",    icon: "◆", color: "#2563c9" }
  technical:  { label: "Technical",  icon: "⚙", color: "#5a616b" }

# document lifecycle states
statuses: [draft, in_review, approved, deprecated]

# ── linkage: the typed edges of the traceability graph ─────────────────────
link_types:
  drives:     { from: [regulation, change], to: requirement }         # WHY → WHAT
  implements: { from: requirement,          to: spec }                # WHAT → HOW
  satisfies:  { from: spec,                 to: requirement }         # HOW → WHAT
  delivers:   { from: work_item,            to: [requirement, spec] } # WHEN → WHAT/HOW
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
- Specs describe current behavior only; planned work belongs in change records.
- Use tables for field mappings, mermaid flowcharts for branching flows.
`;
}

/** Starter content for a missing workspace file, or null if it has none. */
export function scaffoldFor(path: string, project: string): string | null {
  if (path === '.specquill/config.yml') return scaffoldConfigYml(project);
  if (path === '.specquill/instructions.md') return scaffoldInstructionsMd();
  return null;
}
