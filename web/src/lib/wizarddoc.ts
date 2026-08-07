// Turning an accepted wizard draft into a workspace document: the family's
// normal frontmatter (same template as the New-document dialog) plus the
// drafted sections as `## ` blocks. Kept out of the view so it is testable
// and so document shape stays in one place.
import type { DraftSection } from '../api/wizard';
import type { EntityDef } from './entities';
import { assemble, setFmValue } from './frontmatter';
import { newDocTemplate } from './newdoc';

/** `# Title` + one `## Section` block per drafted section. */
export function draftBody(title: string, sections: DraftSection[]): string {
  let out = title.trim() ? `# ${title.trim()}\n` : '';
  for (const s of sections) {
    const name = s.name.trim();
    if (!name) continue;
    out += `\n## ${name}\n\n`;
    const content = s.content.trim();
    // an empty section keeps its heading: the gap is information, and the
    // author lands in the editor with a visible place to fill
    if (content) out += `${content}\n`;
  }
  return out;
}

/**
 * The full file content for a new document. `related` records the existing
 * documents the author chose to keep as context when they declined to extend
 * one of them — a plain frontmatter list, not a traceability edge.
 */
export function draftDocument(
  path: string,
  entities: EntityDef[] | undefined,
  opts: { id?: string; title: string },
  sections: DraftSection[],
  related: string[] = [],
): string {
  const template = newDocTemplate(path, entities, opts);
  const fmMatch = template.match(/^---\n([\s\S]*?)\n---\n/);
  let fm = fmMatch ? fmMatch[1] : '';
  if (related.length && fm) fm = setFmValue(fm, 'related', related);
  return assemble(fm, '\n' + draftBody(opts.title, sections));
}
