// scaffold.ts — starter content for the optional .specquill/ files. A
// workspace without them runs entirely on built-in defaults; these templates
// are what "create config.yml" seeds so users customize a working example
// instead of a blank page.

export function scaffoldConfigYml(project: string): string {
  return `# specquill workspace config — everything here is optional; delete what you
# don't customize and the built-in defaults apply.
version: 2
project: ${project}

# view shown when opening the workspace (dashboard | editor | changes | graph | model)
ui:
  default_view: editor

statuses: [draft, in_review, approved, deprecated]

# driver taxonomy — the categories behind \`drivers:\` frontmatter entries
# (chips in the Properties panel and the dashboards); inline maps, one per line
# drivers:
#   regulatory: { label: "Regulatory", icon: "⚖", color: "#b06f16" }
#   product:    { label: "Product",    icon: "◆", color: "#2563c9" }
#   technical:  { label: "Technical",  icon: "⚙", color: "#5a616b" }

# typed link relations that build the traceability graph (Model view)
# link_types:
#   drives:     { from: [regulation, change], to: requirement }
#   implements: { from: requirement,          to: spec }
#   verifies:   { from: test,                 to: requirement }

# ID schemes for new documents. Tokens: {seq} / {seq:N} (next number in the
# family, zero-padded), {rand:N} digits, {hex:N}, {adj} {word} (memorable
# pairs like "brisk-heron"), {yy} {yyyy}, {slug} (from the title). Families
# without a scheme use built-ins (REQ-/REG-/CHG-/MAP-/ADR-{seq:3}) or {slug}.
ids:
  requirement: { pattern: "REQ-{seq:3}" }

# custom document families beyond the built-ins — labeled in the tree and
# the Model view; new files under these folders get the entity's type
# entities:
#   decision: { folder: "decisions/", label: "Decisions", icon: "◆", color: "#7c5cd6", description: "Why the system is shaped this way." }

# Read-only reference sources this project selects. Local-auth deployments:
# names must exist in the server's catalog (selection can never mint access).
# Forge-PAT deployments: define the repos here under sources: instead — each
# user fetches them with their own token.
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

export function scaffoldSchemaJson(): string {
  return JSON.stringify({
    $comment: 'Property schema for the Properties panel. Edit labels/types/colors/order here.',
    order: ['id', 'type', 'status', 'priority', 'owner', 'implements', 'created', 'updated'],
    fields: {
      id: { label: 'ID', type: 'code' },
      type: { label: 'Type', type: 'tag' },
      status: { label: 'Status', type: 'enum', values: { draft: 'slate', in_review: 'amber', approved: 'green', deprecated: 'slate' } },
      priority: { label: 'Priority', type: 'enum', values: { must: 'amber', should: 'blue', could: 'slate' } },
      owner: { label: 'Owner', type: 'user' },
      implements: { label: 'Implements', type: 'links' },
      created: { label: 'Created', type: 'date' },
      updated: { label: 'Updated', type: 'date' },
    },
  }, null, 2) + '\n';
}

/** Starter content for a missing workspace file, or null if it has none. */
export function scaffoldFor(path: string, project: string): string | null {
  if (path === '.specquill/config.yml') return scaffoldConfigYml(project);
  if (path === '.specquill/schema.json') return scaffoldSchemaJson();
  if (path === '.specquill/instructions.md') return scaffoldInstructionsMd();
  return null;
}
