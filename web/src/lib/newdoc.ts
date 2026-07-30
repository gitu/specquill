// Frontmatter template for files created in the app. Every document carries
// an OKF `type` (the only frontmatter field the format requires), derived
// from the folder family it lands in; the family's `attributes` seed the
// typed link lists so traceability starts at creation time.

import type { EntityDef } from './entities';
import { isLinkAttr, type PropertySchema } from './config';
import { todayStr } from './frontmatter';

export const DOC_TYPES: Record<string, string> = {
  requirements: 'Requirement',
  specs: 'Specification',
  regulations: 'Regulation',
  'data-mappings': 'Data Mapping',
  changes: 'Change Record',
  'work-items': 'Work Item',
  decisions: 'Decision',
  glossary: 'Glossary',
};

const titleCase = (s: string) => s.split(/[_-]/).map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');

export function newDocTemplate(path: string, entities?: EntityDef[], opts?: { id?: string; title?: string; schema?: PropertySchema }): string {
  const family = path.includes('/') ? path.split('/')[0] : '';
  // custom entity families type their documents after the entity kind
  const ent = entities?.find((e) => e.folder === family + '/');
  const type = DOC_TYPES[family] || (ent ? titleCase(ent.kind) : 'Document');
  const name = opts?.title || path.split('/').pop()!.replace(/\.md$/, '');
  const idLine = opts?.id ? `id: ${opts.id}\n` : '';
  const today = todayStr();
  // link-typed attributes per the workspace's effective schema, with the
  // built-in link fields as the floor (a trimmed properties: section must
  // not stop delivers/satisfies & co from seeding)
  const isLink = (k: string) => opts?.schema?.fields?.[k]?.type === 'links' || isLinkAttr(k);
  const links = (ent?.attributes || []).filter(isLink).map((k) => `${k}: []\n`).join('');
  return `---\n${idLine}type: ${type}\ntitle: ${name}\nstatus: draft\n${links}created: ${today}\nupdated: ${today}\n---\n\n# ${name}\n`;
}
