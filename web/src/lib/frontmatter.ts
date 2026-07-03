// frontmatter.ts — comment/format-preserving frontmatter edits via the yaml
// package's Document API. The regex readers in model.ts stay for parsing;
// this module is only used when *writing*.
import { parseDocument } from 'yaml';

export interface FmValue {
  key: string;
  value: unknown;
}

/** Parse frontmatter into plain JS values (for form initial state). */
export function fmToJS(fm: string): Record<string, unknown> {
  try {
    const doc = parseDocument(fm);
    const js = doc.toJS();
    return js && typeof js === 'object' ? (js as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/**
 * Set one top-level key, preserving comments and formatting of everything
 * else. value === undefined deletes the key.
 */
export function setFmValue(fm: string, key: string, value: unknown): string {
  const doc = parseDocument(fm);
  if (value === undefined) {
    doc.delete(key);
  } else {
    doc.set(key, value);
  }
  return doc.toString({ lineWidth: 0 }).replace(/\n$/, '');
}

/**
 * Reassemble a markdown file from frontmatter + body. Exact inverse of
 * model.ts stripFrontmatter, which consumes one newline after the closing
 * `---` — so `assemble(stripFrontmatter(raw))` is byte-identical to raw.
 */
export function assemble(fm: string, body: string): string {
  if (!fm.trim()) return body;
  return `---\n${fm}\n---\n${body}`;
}
