// entities.ts — the document families a workspace is made of. The built-ins
// carry user-facing descriptions; workspaces add or override families via the
// `entities:` section of .specquill/config.yml (inline-map style, like
// `drivers:`):
//
//   entities:
//     decision: { folder: "decisions/", label: "Decisions", icon: "◆",
//                 color: "#7c5cd6", description: "Why the system is shaped this way." }

export interface EntityDef {
  kind: string;        // stable key ('requirement', 'decision', …)
  folder: string;      // top-level folder, with trailing slash
  label: string;       // plural display name
  icon: string;        // single glyph used in the tree
  color: string;       // CSS color (var(--…) or hex)
  description: string; // one-sentence "what is this" shown to users
  builtin: boolean;
}

export const BUILTIN_ENTITIES: EntityDef[] = [
  {
    kind: 'regulation', folder: 'regulations/', label: 'Regulations', icon: '◈', color: 'var(--reg)', builtin: true,
    description: 'External rules the product must comply with — the origin of regulatory drivers and change records.',
  },
  {
    kind: 'requirement', folder: 'requirements/', label: 'Requirements', icon: '▤', color: 'var(--prod)', builtin: true,
    description: 'WHAT the product must do — atomic, testable statements carrying drivers and traceability links.',
  },
  {
    kind: 'spec', folder: 'specs/', label: 'Specs', icon: '◈', color: 'var(--text-2)', builtin: true,
    description: 'HOW requirements are realized — designs that satisfy requirements and map onto data fields.',
  },
  {
    kind: 'data_mapping', folder: 'data-mappings/', label: 'Data mappings', icon: '⇄', color: 'var(--data)', builtin: true,
    description: 'Field-level source → target mappings; drift against the specs is detected here.',
  },
  {
    kind: 'diagram', folder: 'diagrams/', label: 'Diagrams', icon: '✎', color: 'var(--ai)', builtin: true,
    description: 'Sketches and text diagrams embedded in documents — portable formats, no tool lock-in.',
  },
  {
    kind: 'change', folder: 'changes/', label: 'Changes', icon: '⚑', color: 'var(--reg)', builtin: true,
    description: 'Incoming change records (regulatory, product, technical) triaged against the documents they impact.',
  },
];

/**
 * Effective entity families: built-ins, overridden/extended by the workspace
 * config's `entities:` block. Follows the same regex-based parsing idiom as
 * parseTaxonomy — inline maps, quoted values.
 */
export function parseEntities(configYml: string): EntityDef[] {
  const block = ((configYml || '').match(/(?:^|\n)entities:\s*\n([\s\S]*?)(?=\n[a-z_]+:|$)/) || [])[1] || '';
  const out = BUILTIN_ENTITIES.map((e) => ({ ...e }));
  for (const line of block.split('\n')) {
    const m = line.match(/^\s{2}([\w-]+):\s*\{(.*)\}\s*$/);
    if (!m) continue;
    const kind = m[1];
    const field = (k: string) => ((m[2].match(new RegExp(k + ':\\s*"([^"]*)"')) || [])[1] || '').trim();
    const folderRaw = field('folder');
    const folder = folderRaw ? (folderRaw.endsWith('/') ? folderRaw : folderRaw + '/') : '';
    const i = out.findIndex((e) => e.kind === kind);
    if (i >= 0) {
      // override: only the fields the config provides
      const cur = out[i];
      out[i] = {
        ...cur,
        folder: folder || cur.folder,
        label: field('label') || cur.label,
        icon: field('icon') || cur.icon,
        color: field('color') || cur.color,
        description: field('description') || cur.description,
      };
    } else {
      out.push({
        kind,
        folder: folder || kind + 's/',
        label: field('label') || kind.replace(/_/g, ' '),
        icon: field('icon') || '▢',
        color: field('color') || 'var(--text-2)',
        description: field('description') || '',
        builtin: false,
      });
    }
  }
  return out;
}
