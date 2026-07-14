// Frontmatter template for files created in the app. Every document carries
// an OKF `type` (the only frontmatter field the format requires), derived
// from the folder family it lands in.

import type { EntityDef } from './entities';

export const DOC_TYPES: Record<string, string> = {
  requirements: 'Requirement',
  specs: 'Specification',
  regulations: 'Regulation',
  'data-mappings': 'Data Mapping',
  changes: 'Change Record',
  decisions: 'Decision',
  glossary: 'Glossary',
};

const titleCase = (s: string) => s.split(/[_-]/).map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');

export function newDocTemplate(path: string, entities?: EntityDef[], opts?: { id?: string; title?: string }): string {
  const family = path.includes('/') ? path.split('/')[0] : '';
  // custom entity families type their documents after the entity kind
  const ent = entities?.find((e) => e.folder === family + '/');
  const type = DOC_TYPES[family] || (ent ? titleCase(ent.kind) : 'Document');
  const name = opts?.title || path.split('/').pop()!.replace(/\.md$/, '');
  const idLine = opts?.id ? `id: ${opts.id}\n` : '';
  return `---\n${idLine}type: ${type}\ntitle: ${name}\nstatus: draft\n---\n\n# ${name}\n`;
}
