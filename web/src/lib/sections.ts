// sections.ts — the section outline a guided draft is written into, per
// document family. Built-ins mirror server/internal/ai/wizard.go (the server
// falls back to them when a request omits the outline); workspaces override
// per family in .specquill/config.yml, same inline-map idiom as `entities:`
// and `ids:`:
//
//   sections:
//     spec: ["Overview", "Behaviour", "Interfaces & data", "Edge cases"]
//     requirement: ["Context", "Requirement statements", "Acceptance criteria"]

export const BUILTIN_SECTIONS: Record<string, string[]> = {
  requirement: ['Context', 'Requirement statements', 'Acceptance criteria', 'Traceability', 'Open questions'],
  spec: ['Overview', 'Behaviour', 'Interfaces & data', 'Edge cases', 'Open questions'],
  change: ['Summary', 'Driver', 'Impact', 'Required updates', 'Open questions'],
  regulation: ['Summary', 'Obligations', 'Scope', 'Affected documents', 'Open questions'],
  data_mapping: ['Overview', 'Field mapping', 'Transformations', 'Validation', 'Open questions'],
  decision: ['Context', 'Decision', 'Alternatives considered', 'Consequences', 'Open questions'],
};

const GENERIC = ['Context', 'Details', 'Acceptance criteria', 'Open questions'];

/** The config's `sections:` overrides (kind → outline), in file order. */
export function sectionSchemes(configYml?: string): Record<string, string[]> {
  const block = ((configYml || '').match(/(?:^|\n)sections:\s*\n([\s\S]*?)(?=\n[a-z_]+:|$)/) || [])[1] || '';
  const out: Record<string, string[]> = {};
  for (const line of block.split('\n')) {
    const m = line.match(/^\s{2}([\w-]+):\s*\[(.*)\]\s*$/);
    if (!m) continue;
    const names = m[2]
      .split(',')
      .map((s) => s.trim().replace(/^["']|["']$/g, '').trim())
      .filter(Boolean);
    if (names.length) out[m[1]] = names;
  }
  return out;
}

/** Effective outline for a family: config override > built-in > generic. */
export function sectionsFor(kind: string, configYml?: string): string[] {
  return [...(sectionSchemes(configYml)[kind] || BUILTIN_SECTIONS[kind] || GENERIC)];
}
