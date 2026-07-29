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
  // no flow padding: the workspace convention is `[a, b]`, and padding would
  // reformat untouched inline lists on every unrelated edit
  return doc.toString({ lineWidth: 0, flowCollectionPadding: false }).replace(/\n$/, '');
}

/** Local calendar date as YYYY-MM-DD — the workspace's frontmatter date format. */
export function todayStr(now = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}

/**
 * Maintain the `updated` date on a real edit: set it to today. Documents
 * without frontmatter are left alone (callers guard on non-empty fm), and an
 * already-current value returns the fm string unchanged so unrelated
 * keystrokes never reformat it.
 */
export function touchUpdated(fm: string, now = new Date()): string {
  const today = todayStr(now);
  if (!fm.trim() || fmToJS(fm).updated === today) return fm;
  return setFmValue(fm, 'updated', today);
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
