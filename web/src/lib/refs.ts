// refs.ts — reference detection for the editor: auto-link known entities
// (requirement ids, file paths, titles) in markdown text, and propose
// references the text mentions but never links.
import { WorkspaceModel } from './model';

export interface RefTarget {
  label: string;   // display label, e.g. "REQ-042 Transaction Reporting"
  path: string;    // workspace path the reference points at
  id?: string;     // matchable id (REQ-042)
  title?: string;  // matchable title ("Transaction Reporting")
}

export function knownTargets(model: WorkspaceModel): RefTarget[] {
  const out: RefTarget[] = [];
  model.requirements.forEach((r) => out.push({ label: `${r.id} ${r.title}`, path: r.path, id: r.id, title: r.title }));
  model.specs.forEach((s) => out.push({ label: s.title || s.name, path: s.path, title: s.title }));
  model.regs.forEach((r) => out.push({ label: r.title, path: r.path, title: r.title }));
  model.maps.forEach((m) => out.push({ label: m.name, path: m.path }));
  model.fields.forEach((f) => out.push({ label: f.name, path: f.map, id: f.name }));
  return out.filter((t) => t.path);
}

const escRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

export function relTo(fromDocPath: string, target: string): string {
  const dir = fromDocPath.split('/').slice(0, -1).join('/');
  const up = dir ? dir.split('/').map(() => '..').join('/') + '/' : '';
  return up + target;
}

// split markdown into transformable text and protected chunks (fenced code,
// inline code, existing links/images) — transforms only touch plain text
function mapPlainText(md: string, fn: (chunk: string) => string): string {
  const protectedRe = /(```[\s\S]*?```|`[^`\n]*`|!?\[[^\]]*\]\([^)]*\))/g;
  let out = '';
  let last = 0;
  for (const m of md.matchAll(protectedRe)) {
    out += fn(md.slice(last, m.index));
    out += m[0];
    last = m.index! + m[0].length;
  }
  out += fn(md.slice(last));
  return out;
}

/**
 * Convert plain-text mentions of known entities into markdown links.
 * Ids (REQ-042, trade.venue) link every mention; titles link only their first
 * mention to keep prose readable. Self-references are skipped.
 */
export function linkifyReferences(md: string, targets: RefTarget[], docPath: string, only?: string): string {
  const applicable = targets.filter((t) => t.path !== docPath && (!only || t.path === only));
  let result = md;

  for (const t of applicable) {
    const rel = relTo(docPath, t.path);
    if (t.id) {
      const re = new RegExp(`(?<![\\w./[-])${escRe(t.id)}(?!\\.\\w|[\\w/-])`, 'g');
      result = mapPlainText(result, (chunk) => chunk.replace(re, `[${t.id}](${rel})`));
    }
  }
  // titles: first plain mention only
  for (const t of applicable) {
    if (!t.title || t.title.length < 4) continue;
    if (result.includes(`](${relTo(docPath, t.path)})`) && t.id) continue; // already linked via id
    const re = new RegExp(`(?<![\\w[])${escRe(t.title)}(?![\\w\\]])`);
    let done = false;
    result = mapPlainText(result, (chunk) => {
      if (done) return chunk;
      const m = chunk.match(re);
      if (!m) return chunk;
      done = true;
      return chunk.replace(re, `[${t.title}](${relTo(docPath, t.path)})`);
    });
  }
  return result;
}

/** Targets mentioned in the text but never linked from this document. */
export function suggestReferences(md: string, targets: RefTarget[], docPath: string): RefTarget[] {
  // strip protected chunks; mentions must appear in plain text
  let plain = '';
  mapPlainText(md, (chunk) => { plain += chunk; return chunk; });
  const out: RefTarget[] = [];
  for (const t of targets) {
    if (t.path === docPath) continue;
    const rel = relTo(docPath, t.path);
    if (md.includes(`](${rel})`) || md.includes(`](${t.path})`)) continue; // already linked
    const mentioned =
      (t.id && new RegExp(`(?<![\\w./[-])${escRe(t.id)}(?!\\.\\w|[\\w/-])`).test(plain)) ||
      (t.title && t.title.length >= 4 && plain.includes(t.title));
    if (mentioned) out.push(t);
  }
  return out;
}
