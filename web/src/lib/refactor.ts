// refactor.ts — reference rewriting when a document moves. Detects every way
// a link can be authored (relative, root-relative, tolerant bare paths, with
// anchors, image embeds, typed frontmatter link lists) via the same
// resolution rules the rest of the app uses.

import { resolveDocHref, stripFrontmatter } from './model';
import { assemble } from './frontmatter';

/** Relative markdown path from a document's directory to a target path. */
export function relLink(fromDir: string, target: string): string {
  const from = fromDir ? fromDir.split('/') : [];
  const to = target.split('/');
  let i = 0;
  while (i < from.length && i < to.length - 1 && from[i] === to[i]) i++;
  return '../'.repeat(from.length - i) + to.slice(i).join('/');
}

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/**
 * Rewrite every reference to oldPath inside one document: body links become
 * RELATIVE links to the new location; typed frontmatter entries (stored
 * root-relative) get the new root-relative path. Returns null when the
 * document does not reference oldPath.
 */
export function rewriteRefs(docPath: string, content: string, oldPath: string, newPath: string): string | null {
  const dir = docPath.includes('/') ? docPath.slice(0, docPath.lastIndexOf('/')) : '';
  let n = 0;
  const { fm, body } = stripFrontmatter(content);
  const newBody = body.replace(/(\]\()([^)\s]+)(\))/g, (all, pre: string, href: string, post: string) => {
    const hash = href.indexOf('#');
    const target = hash >= 0 ? href.slice(0, hash) : href;
    const anchor = hash >= 0 ? href.slice(hash) : '';
    if (!target || /^[a-z][a-z+.-]*:/i.test(target) || resolveDocHref(dir, target) !== oldPath) return all;
    n++;
    return pre + relLink(dir, newPath) + anchor + post;
  });
  const fmRe = new RegExp('(^|[\\s\\[,"\'])' + escapeRe(oldPath) + '(?=[\\s\\],"\'#]|$)', 'gm');
  const newFm = fm.replace(fmRe, (_all, pre: string) => {
    n++;
    return pre + newPath;
  });
  if (n === 0) return null;
  return fm ? assemble(newFm, newBody) : newBody;
}

/** The documents in a snapshot that reference oldPath (candidates for rewrite). */
export function referencingDocs(files: Record<string, string>, oldPath: string): string[] {
  return Object.keys(files)
    .filter((p) => p.endsWith('.md') && p !== oldPath)
    .filter((p) => rewriteRefs(p, files[p], oldPath, oldPath + '.tmp') !== null)
    .sort();
}
