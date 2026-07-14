// ids.ts — document ID schemes. Every entity family can carry an ID pattern
// (config `ids:` block, falling back to sensible built-ins); patterns expand
// tokens into concrete, conflict-checked IDs against the workspace snapshot.
//
//   ids:
//     requirement: { pattern: "REQ-{seq:3}" }
//     decision:    { pattern: "ADR-{yyyy}-{seq:2}" }
//     spec:        { pattern: "{adj}-{word}" }        # memorable word pairs
//
// Tokens:
//   {seq} {seq:N}  next sequential number across the family's existing IDs
//                  (N = zero-pad width, default 3)
//   {rand:N}       N random digits
//   {hex:N}        N random hex chars
//   {adj} {word}   memorable adjective / noun (e.g. "brisk-heron")
//   {yy} {yyyy}    current year
//   {slug}         kebab-case of the document title (memorable word pair
//                  while the title is still empty)

// Curated for memorability: short, concrete, visually distinct, no two words
// that sound alike. 40×44 word pairs ≈ 1760 combos per family.
const ADJECTIVES = [
  'amber', 'bold', 'brisk', 'calm', 'cedar', 'clear', 'coral', 'crisp',
  'deep', 'eager', 'fleet', 'fresh', 'gold', 'grand', 'green', 'hardy',
  'ivory', 'jade', 'keen', 'lively', 'lucid', 'mellow', 'noble', 'opal',
  'pale', 'quiet', 'rapid', 'royal', 'sage', 'sharp', 'silver', 'solid',
  'sunny', 'swift', 'tidal', 'urban', 'vivid', 'warm', 'wild', 'young',
];
const NOUNS = [
  'aspen', 'badger', 'birch', 'comet', 'condor', 'crane', 'delta', 'ember',
  'falcon', 'fjord', 'gale', 'glade', 'harbor', 'heron', 'iris', 'jetty',
  'kestrel', 'lagoon', 'lark', 'linden', 'lynx', 'maple', 'marten', 'mesa',
  'nectar', 'osprey', 'otter', 'pine', 'plume', 'quartz', 'raven', 'reef',
  'ridge', 'river', 'sable', 'sparrow', 'summit', 'tarn', 'thicket', 'tundra',
  'vale', 'walnut', 'willow', 'wren',
];

const pick = <T>(xs: T[]): T => xs[Math.floor(Math.random() * xs.length)];

// Built-in patterns; a config `ids:` entry for the kind overrides, any other
// kind (custom entities, diagrams, glossary…) names files after the title.
const DEFAULT_PATTERNS: Record<string, string> = {
  requirement: 'REQ-{seq:3}',
  regulation: 'REG-{seq:3}',
  change: 'CHG-{seq:3}',
  data_mapping: 'MAP-{seq:3}',
  decision: 'ADR-{seq:3}',
};

/** The config's `ids:` entries (kind → pattern), in file order. */
export function idSchemes(configYml?: string): { kind: string; pattern: string }[] {
  const block = ((configYml || '').match(/(?:^|\n)ids:\s*\n([\s\S]*?)(?=\n[a-z_]+:|$)/) || [])[1] || '';
  const out: { kind: string; pattern: string }[] = [];
  for (const line of block.split('\n')) {
    const m = line.match(/^\s{2}([\w-]+):\s*\{(.*)\}\s*$/);
    const p = m && (m[2].match(/pattern:\s*"([^"]*)"/) || [])[1];
    if (m && p) out.push({ kind: m[1], pattern: p });
  }
  return out;
}

/** Effective ID pattern for an entity kind (config `ids:` > built-in > {slug}). */
export function idPattern(kind: string, configYml?: string): string {
  const hit = idSchemes(configYml).find((s) => s.kind === kind);
  return hit?.pattern || DEFAULT_PATTERNS[kind] || '{slug}';
}

export function slugify(title: string): string {
  return title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 64);
}

/** Slugify a folder path segment-wise: 'Auth / Session Mgmt' → 'auth/session-mgmt'. */
export function slugifyPath(p: string): string {
  return p.split('/').map(slugify).filter(Boolean).join('/');
}

/**
 * IDs already taken in a family: filename stems under the family folder plus
 * every frontmatter `id:` anywhere in the workspace (IDs are cross-referenced
 * from other documents, so they must be globally unambiguous). Lowercased —
 * conflicts are detected case-insensitively.
 */
export function existingIds(files: Record<string, string>, folder: string): Set<string> {
  const taken = new Set<string>();
  for (const [path, content] of Object.entries(files)) {
    if (path.startsWith(folder)) {
      const stem = path.split('/').pop()!.replace(/\.[^.]*$/, '');
      taken.add(stem.toLowerCase());
    }
    const id = (content.match(/^---\n[\s\S]*?^id:\s*([^\n]+)$/m) || [])[1];
    if (id) taken.add(id.trim().toLowerCase());
  }
  return taken;
}

const TOKEN = /\{(seq|rand|hex|adj|word|yy|yyyy|slug)(?::(\d+))?\}/g;

/** Whether the pattern has dice-able tokens (regenerate button is useful). */
export const hasRandomToken = (pattern: string) => /\{(rand|hex|adj|word)(?::\d+)?\}/.test(pattern);
/** Whether the pattern derives from the title (ID should track title edits). */
export const hasSlugToken = (pattern: string) => /\{slug\}/.test(pattern);

// {seq} scans the family's existing IDs with the pattern's literal parts
// anchored, so "REQ-{seq:3}" after REQ-005 and REQ-090 yields REQ-091 — and
// stray files that don't match the pattern can't poison the counter.
function nextSeq(pattern: string, taken: Set<string>): number {
  const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  let re = '';
  let last = 0;
  pattern.replace(TOKEN, (m, tok, n, off: number) => {
    re += esc(pattern.slice(last, off).toLowerCase());
    last = off + m.length;
    re += tok === 'seq' ? '(\\d+)'
      : tok === 'rand' ? `\\d{${n || 1}}`
      : tok === 'hex' ? `[0-9a-f]{${n || 1}}`
      : tok === 'yy' ? '\\d{2}'
      : tok === 'yyyy' ? '\\d{4}'
      : '[a-z0-9-]+';
    return m;
  });
  re += esc(pattern.slice(last).toLowerCase());
  const rx = new RegExp('^' + re + '$');
  let max = 0;
  for (const id of taken) {
    const m = id.match(rx);
    if (m && m[1]) max = Math.max(max, parseInt(m[1], 10));
  }
  return max + 1;
}

export interface GeneratedId { id: string; conflict: boolean }

/**
 * Expand a pattern into a concrete ID. Random tokens re-roll until the ID is
 * free (bounded); sequential tokens are conflict-free by construction. A
 * returned `conflict: true` means every attempt collided — the caller shows
 * the clash and lets the user edit.
 */
export function generateId(pattern: string, taken: Set<string>, title?: string): GeneratedId {
  const seq = /\{seq(?::\d+)?\}/.test(pattern) ? nextSeq(pattern, taken) : 0;
  // a {slug} without a title degrades to a memorable random pair — that makes
  // the expansion random, so it gets the same re-roll budget
  const blankSlug = hasSlugToken(pattern) && !slugify(title || '');
  const attempts = hasRandomToken(pattern) || blankSlug ? 100 : 1;
  let id = '';
  for (let i = 0; i < attempts; i++) {
    id = pattern.replace(TOKEN, (_, tok, n) => {
      switch (tok) {
        case 'seq': return String(seq).padStart(parseInt(n || '3', 10), '0');
        case 'rand': return Array.from({ length: parseInt(n || '3', 10) }, () => pick('0123456789'.split(''))).join('');
        case 'hex': return Array.from({ length: parseInt(n || '4', 10) }, () => pick('0123456789abcdef'.split(''))).join('');
        case 'adj': return pick(ADJECTIVES);
        case 'word': return pick(NOUNS);
        case 'yy': return String(new Date().getFullYear()).slice(2);
        case 'yyyy': return String(new Date().getFullYear());
        case 'slug': return slugify(title || '') || pick(ADJECTIVES) + '-' + pick(NOUNS);
        default: return '';
      }
    });
    if (!taken.has(id.toLowerCase())) return { id, conflict: false };
  }
  return { id, conflict: true };
}
