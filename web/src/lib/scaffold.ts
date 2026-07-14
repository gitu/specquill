// scaffold.ts — starter content for the optional .specquill/ files. A
// workspace without them runs entirely on built-in defaults; these templates
// are what "create config.yml" seeds so users customize a working example
// instead of a blank page.

export function scaffoldConfigYml(project: string): string {
  return `# specquill workspace config — everything here is optional; delete what you
# don't customize and the built-in defaults apply.
version: 2
project: ${project}

# view shown when opening the workspace (dashboard | editor | changes | graph | matrix | model)
ui:
  default_view: editor

statuses: [draft, in_review, approved, deprecated]

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

# read-only reference sources this project selects (must be granted first)
references: []
`;
}

export function scaffoldSchemaJson(): string {
  return JSON.stringify({
    $comment: 'Property schema for the Properties panel. Edit labels/types/colors/order here.',
    order: ['id', 'type', 'status', 'priority', 'owner', 'implements', 'updated'],
    fields: {
      id: { label: 'ID', type: 'code' },
      type: { label: 'Type', type: 'tag' },
      status: { label: 'Status', type: 'enum', values: { draft: 'slate', in_review: 'amber', approved: 'green', deprecated: 'slate' } },
      priority: { label: 'Priority', type: 'enum', values: { must: 'amber', should: 'blue', could: 'slate' } },
      owner: { label: 'Owner', type: 'user' },
      implements: { label: 'Implements', type: 'links' },
      updated: { label: 'Updated', type: 'date' },
    },
  }, null, 2) + '\n';
}

/** Starter content for a missing workspace file, or null if it has none. */
export function scaffoldFor(path: string, project: string): string | null {
  if (path === '.specquill/config.yml') return scaffoldConfigYml(project);
  if (path === '.specquill/schema.json') return scaffoldSchemaJson();
  return null;
}
