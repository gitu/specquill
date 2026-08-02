// derive.ts — pure "model → view data" functions ported from the prototype's
// build*/renderVals methods. Styles are composed as strings (rendered via sx())
// exactly like the design; navigation is carried as paths, not callbacks.

import { DataField, DocNode, PropEntry, Requirement, TimedDoc, WorkspaceModel, getList, isReservedMd, parseDriverEntries, parseProps, resolveFmRef, stripFrontmatter } from './model';
import type { EntityDef } from './entities';
import { GROUP_ORDER, primaryEntity, workspaceConfig } from './config';
import type { PropertySchema } from './config';

export const srcMeta = (s: string) =>
  ({
    regulatory: { icon: '⚖', label: 'Regulatory', fg: 'var(--reg)', bg: 'var(--reg-bg)' },
    product: { icon: '◆', label: 'Product', fg: 'var(--prod)', bg: 'var(--prod-bg)' },
    technical: { icon: '⚙', label: 'Technical', fg: 'var(--text-2)', bg: 'var(--surface-2)' },
  })[s] || { icon: '•', label: s || 'Change', fg: 'var(--text-2)', bg: 'var(--surface-2)' };

/**
 * Chip meta for a driver/source key: the built-in trio keeps its themed
 * fg/bg pairs, custom taxonomy entries take their configured icon + color —
 * so a workspace's own driver kinds render first-class, not as gray dots.
 */
export function driverMeta(model: WorkspaceModel, key: string) {
  const builtin = { regulatory: 1, product: 1, technical: 1 } as Record<string, 1>;
  if (builtin[key]) return srcMeta(key);
  const dd = model.driverDefs.find((d) => d.key === key);
  if (dd) return { icon: dd.icon, label: dd.label, fg: dd.color, bg: 'var(--surface-2)' };
  return srcMeta(key);
}

export function statusMeta(s: string): { label: string; color: string } {
  const v = String(s || '').toLowerCase();
  const m: Record<string, [string, string]> = {
    triage: ['Triage', 'var(--reg)'], in_progress: ['In progress', 'var(--prod)'],
    auto_remapped: ['Auto-remapped', 'var(--data)'], backlog: ['Backlog', 'var(--text-3)'],
    done: ['Done', 'var(--data)'], merged: ['Merged', 'var(--data)'],
  };
  return m[v] ? { label: m[v][0], color: m[v][1] } : { label: (s || '').replace(/_/g, ' '), color: 'var(--text-2)' };
}

/** Today as YYYY-MM-DD in LOCAL time — the clock seam of timed dependencies. */
export function todayISO(): string {
  const d = new Date();
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
}

/** "in 12d" / "today" / "8d ago" — signed day counts as the timeline reads them. */
export function daysLabel(days: number): string {
  if (days === 0) return 'today';
  const n = Math.abs(days);
  const unit = n < 45 ? `${n}d` : n < 365 ? `${Math.round(n / 30)}mo` : `${(n / 365).toFixed(1)}y`;
  return days > 0 ? `in ${unit}` : `${unit} ago`;
}

export function daysAgo(d: string): string {
  if (!d) return '';
  const then = new Date(d + 'T00:00:00').getTime();
  const n = Math.round((Date.now() - then) / 86400000);
  if (n <= 0) return 'today';
  if (n < 7) return n + 'd';
  if (n < 30) return Math.round(n / 7) + 'w';
  return Math.round(n / 30) + 'mo';
}

// ---------------------------------------------------------------- tree

/**
 * The document the editor opens when the URL names none: the first document
 * in entity-family order, else the workspace index, else the first markdown
 * file. '' while the snapshot is still loading (the file query stays idle).
 */
export function defaultDoc(files: Record<string, string> | undefined, entities: EntityDef[]): string {
  if (!files) return '';
  for (const e of entities) {
    const hit = Object.keys(files).filter((p) => p.startsWith(e.folder) && p.endsWith('.md') && !isReservedMd(p)).sort()[0];
    if (hit) return hit;
  }
  if (files['index.md'] !== undefined) return 'index.md';
  return Object.keys(files).filter((p) => p.endsWith('.md')).sort()[0] || '';
}

export interface TreeFile {
  path: string; name: string; icon: string; color: string;
  badge: string; badgeStyle: string; active: boolean;
  generated: boolean; // OKF reserved file, regenerated at commit time
  title?: string;     // frontmatter title (filter + optional row detail)
  docType?: string;   // the classified family's doc type, ditto
}
/** A workspace directory: nested subdirectories plus its direct files. */
export interface TreeDir { name: string; path: string; desc?: string; dirs: TreeDir[]; files: TreeFile[] }
export type TreeFolder = TreeDir; // top-level entry — same shape

export function buildTree(files: Record<string, string>, openPath: string | undefined, gitStatus: Record<string, string>, entities: EntityDef[], model?: WorkspaceModel, opts?: { all?: boolean; extraPaths?: string[] }): TreeDir[] {
  const all = !!opts?.all;
  const meta: Record<string, { icon: string; color: string; desc: string }> = {};
  const order: string[] = [];
  entities.forEach((e) => {
    const name = e.folder.replace(/\/$/, '');
    meta[name] = { icon: e.icon, color: e.color, desc: e.description };
    order.push(name);
  });
  // per-document meta from the CLASSIFIED model: a doc typed into another
  // family shows that family's icon/color, not its folder's
  const docMeta: Record<string, { title: string; docType: string; icon?: string; color?: string }> = {};
  model?.docs.forEach((d) => {
    const e = model.entities.find((en) => en.kind === d.kind);
    docMeta[d.path] = { title: d.title, docType: e?.docType || d.kind, icon: e?.icon, color: e?.color };
  });
  const byFolder: Record<string, string[]> = {};
  const rootFiles: string[] = [];
  // all mode: merge the FULL repo listing (binaries the text snapshot skips,
  // e.g. *.excalidraw.png) into the visible set
  const paths = new Set(Object.keys(files));
  if (all) opts?.extraPaths?.forEach((p) => paths.add(p));
  paths.forEach((p) => {
    const folder = p.split('/')[0];
    // root files and dot-folders (.specquill) stay out of the tree — unless
    // the all-files mode asks for the whole repository
    if (!p.includes('/')) {
      if (all) rootFiles.push(p);
      return;
    }
    if (folder.startsWith('.') && !all) return;
    (byFolder[folder] = byFolder[folder] || []).push(p);
  });
  // entity folders first (config order), then any other folder alphabetically
  const names = [...order.filter((f) => byFolder[f]), ...Object.keys(byFolder).filter((f) => !meta[f]).sort()];
  const dirs = names.map((folder) => {
    const fm = meta[folder] || { icon: '▢', color: 'var(--text-2)', desc: '' };
    const root: TreeDir = { name: folder, path: folder, desc: fm.desc, dirs: [], files: [] };
    // nest subdirectories, mirroring buildRefTree: sorted input lands dirs
    // and files in each parent already sorted
    const dirAt = new Map<string, TreeDir>([[folder, root]]);
    const dirFor = (p: string): TreeDir => {
      const hit = dirAt.get(p);
      if (hit) return hit;
      const cut = p.lastIndexOf('/');
      const node: TreeDir = { name: p.slice(cut + 1), path: p, dirs: [], files: [] };
      dirAt.set(p, node);
      dirFor(p.slice(0, cut)).dirs.push(node);
      return node;
    };
    for (const path of byFolder[folder].sort()) {
      const n = path.split('/').pop()!;
      const badge = gitStatus[path] || '';
      const dm = docMeta[path];
      dirFor(path.slice(0, path.lastIndexOf('/'))).files.push({
        path, name: n,
        icon: n.endsWith('.mermaid') ? '⌗' : dm?.icon || fm.icon,
        color: dm?.color || fm.color,
        badge,
        badgeStyle: badge === 'A' ? 'color:var(--add)' : 'color:var(--reg)',
        active: path === openPath,
        generated: isReservedMd(path),
        title: dm?.title || undefined,
        docType: dm?.docType,
      });
    }
    return root;
  });
  if (rootFiles.length) {
    dirs.push({
      name: '/', path: '', desc: 'repository root', dirs: [],
      files: rootFiles.sort().map((path) => ({
        path, name: path,
        icon: '▢', color: 'var(--text-2)',
        badge: gitStatus[path] || '',
        badgeStyle: (gitStatus[path] || '') === 'A' ? 'color:var(--add)' : 'color:var(--reg)',
        active: path === openPath,
        generated: isReservedMd(path),
      })),
    });
  }
  return dirs;
}

// ---------------------------------------------------------------- reference tree

export interface RefDir { name: string; path: string; dirs: RefDir[]; files: { name: string; path: string }[] }

/**
 * Mirrors the server's filterByPaths prefix semantics (speccy.go): a file
 * survives when it equals a prefix or sits under one; no prefixes keeps all.
 */
export function filterRefPaths(paths: string[], prefixes?: string[]): string[] {
  if (!prefixes || prefixes.length === 0) return paths;
  return paths.filter((p) => prefixes.some((pre) => p === pre || p.startsWith(pre.replace(/\/+$/, '') + '/')));
}

/** Nests a flat recursive file listing into directories, everything sorted. */
export function buildRefTree(paths: string[]): RefDir {
  const root: RefDir = { name: '', path: '', dirs: [], files: [] };
  const dirAt = new Map<string, RefDir>([['', root]]);
  const dirFor = (p: string): RefDir => {
    const hit = dirAt.get(p);
    if (hit) return hit;
    const cut = p.lastIndexOf('/');
    const node: RefDir = { name: p.slice(cut + 1), path: p, dirs: [], files: [] };
    dirAt.set(p, node);
    dirFor(cut < 0 ? '' : p.slice(0, cut)).dirs.push(node);
    return node;
  };
  // sorted input ⇒ dirs and files land in each parent already sorted
  for (const p of [...paths].sort()) {
    const cut = p.lastIndexOf('/');
    dirFor(cut < 0 ? '' : p.slice(0, cut)).files.push({ name: p.slice(cut + 1), path: p });
  }
  return root;
}

// ---------------------------------------------------------------- properties

export interface PropItem { text: string; style: string; openPath?: string; href?: string }
export interface PropRow { key: string; rawKey: string; items: PropItem[] }

// property-schema `values` colors — the second row aliases the css-var names
// some workspaces use (e.g. the specquill product repo) onto the same palette
export const PAL: Record<string, { fg: string; bg: string }> = {
  green: { fg: 'var(--data)', bg: 'var(--data-bg)' }, amber: { fg: 'var(--reg)', bg: 'var(--reg-bg)' },
  blue: { fg: 'var(--prod)', bg: 'var(--prod-bg)' }, violet: { fg: 'var(--ai)', bg: 'var(--ai-bg)' },
  slate: { fg: 'var(--text-2)', bg: 'var(--surface-2)' },
  data: { fg: 'var(--data)', bg: 'var(--data-bg)' }, reg: { fg: 'var(--reg)', bg: 'var(--reg-bg)' },
  prod: { fg: 'var(--prod)', bg: 'var(--prod-bg)' }, ai: { fg: 'var(--ai)', bg: 'var(--ai-bg)' },
  text: { fg: 'var(--text-2)', bg: 'var(--surface-2)' },
};

export function buildProps(fm: string | undefined, schema: PropertySchema | undefined): PropRow[] {
  if (!fm) return [];
  const sch = schema || { fields: {}, order: [] };
  const entries = parseProps(fm);
  const byKey: Record<string, PropEntry> = {};
  entries.forEach((e) => { byKey[e.key] = e; });
  const order = sch.order || [];
  const keys = [...order.filter((k) => byKey[k]), ...entries.map((e) => e.key).filter((k) => order.indexOf(k) < 0)].filter((k) => k !== 'title');
  const chip = (bg: string, fg: string, mono?: boolean, cap?: boolean) =>
    'display:inline-flex;align-items:center;padding:2px 9px;border-radius:6px;font-size:11.5px;' +
    (mono ? "font-family:'JetBrains Mono',monospace;" : '') + (cap ? 'text-transform:capitalize;' : '') +
    'background:' + bg + ';color:' + fg;
  const badge = (c: { fg: string; bg: string }) =>
    'display:inline-flex;align-items:center;padding:2px 10px;border-radius:20px;font-size:11.5px;font-weight:600;text-transform:capitalize;background:' + c.bg + ';color:' + c.fg;
  const linkStyle = "color:var(--prod);cursor:pointer;text-decoration:underline;text-decoration-color:var(--prod-line);font-family:'JetBrains Mono',monospace;font-size:12px";
  const linkItem = (t: string): PropItem => {
    // external URLs (work-items backlinks et al.) open in a new tab
    if (/^https?:\/\//.test(String(t))) return { text: t, style: linkStyle, href: t };
    const pm = String(t).match(/([\w-]+\/[\w.\/-]+\.(?:md|excalidraw|mermaid))/);
    if (pm) return { text: t, style: linkStyle, openPath: pm[1] };
    return { text: t, style: chip('var(--surface-2)', 'var(--text-2)', true) };
  };
  return keys.map((key) => {
    const e = byKey[key];
    const def = (sch.fields || {})[key] || {};
    const label = def.label || key.replace(/_/g, ' ');
    const type = def.type;
    let items: PropItem[];
    if (e.type === 'scalar') {
      const v = e.value;
      if (type === 'enum' || def.values) { const cn = (def.values || {})[String(v).toLowerCase()] || 'slate'; items = [{ text: v, style: badge(PAL[cn] || PAL.slate) }]; }
      else if (type === 'percent') { const n = parseFloat(v) || 0; const pct = Math.round(n <= 1 ? n * 100 : n); const c = pct > 80 ? 'var(--data)' : pct > 60 ? 'var(--prod)' : 'var(--reg)'; items = [{ text: pct + '%', style: 'display:inline-flex;padding:2px 10px;border-radius:20px;font-size:11.5px;font-weight:600;background:var(--surface-2);color:' + c }]; }
      else if (type === 'user') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text)', true) }];
      else if (type === 'code') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text-2)', true) }];
      else if (type === 'tag') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text-2)', false, true) }];
      else if (type === 'date') items = [{ text: v, style: "font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-2)" }];
      else items = [{ text: v, style: 'font-size:13px;color:var(--text);line-height:1.5' }];
    } else {
      items = e.items.map((it) => (type === 'code' || type === 'anchors') ? { text: it, style: chip('var(--surface-2)', 'var(--text-2)', true) } : linkItem(it));
    }
    return { key: label, rawKey: key, items };
  });
}

/**
 * Distinct scalar frontmatter values per key across the workspace's concept
 * documents — the option pool behind the properties-form comboboxes. Keys
 * whose values are lists or prose (long strings) still get an entry with no
 * values, so the add-property row can offer them as known keys.
 */
export function collectFieldValues(files: Record<string, string>): Record<string, string[]> {
  const sets: Record<string, Set<string>> = {};
  for (const p of Object.keys(files)) {
    if (!p.endsWith('.md') || isReservedMd(p)) continue;
    const { fm } = stripFrontmatter(files[p]);
    if (!fm) continue;
    for (const e of parseProps(fm)) {
      const set = (sets[e.key] = sets[e.key] || new Set());
      if (e.type === 'scalar' && e.value && e.value.length <= 40) set.add(e.value);
    }
  }
  const out: Record<string, string[]> = {};
  for (const k of Object.keys(sets).sort()) out[k] = [...sets[k]].sort();
  return out;
}

// ---------------------------------------------------------------- ref targets

export interface RefTarget { value: string; hint?: string }

/**
 * Link targets for the properties-form pickers (drivers, implements, maps_to,
 * diagrams, …): every non-reserved workspace file, plus `path#anchor` for
 * anchors a document declares (frontmatter `anchors:`) or carries as explicit
 * `{#id}` heading attributes, plus `path#field` for data-mapping fields.
 * Hints carry titles / field names so search can match on them too.
 */
export function collectRefTargets(files: Record<string, string>, fields: DataField[] = []): RefTarget[] {
  const out: RefTarget[] = [];
  const seen = new Set<string>();
  const add = (value: string, hint?: string) => {
    if (!seen.has(value)) { seen.add(value); out.push({ value, hint }); }
  };
  for (const p of Object.keys(files).sort()) {
    if (p.split('/')[0].startsWith('.')) continue;
    if (!p.endsWith('.md')) { add(p); continue; }
    if (isReservedMd(p)) continue;
    const { fm, body } = stripFrontmatter(files[p]);
    const title = ((fm || '').match(/^title:\s*["']?(.*?)["']?\s*$/m) || [])[1] || '';
    add(p, title);
    for (const e of parseProps(fm || '')) {
      if (e.key !== 'anchors') continue;
      (e.type === 'list' ? e.items : [e.value]).forEach((a) => a && add(p + '#' + a, title));
    }
    for (const m of body.matchAll(/^#{1,6}\s[^\n]*\{#([A-Za-z0-9_-]+)\}/gm)) add(p + '#' + m[1], title);
  }
  fields.forEach((f) => add(f.map + '#' + f.name.split('.').pop(), f.name));
  return out;
}

/**
 * Anchor-id options for editing a document's OWN `anchors:` list, derived
 * from its headings: explicit `{#id}` attributes verbatim, plain headings as
 * slugs (the hint flags that the heading still lacks the `{#id}` attribute).
 */
export function docAnchorOptions(body: string): RefTarget[] {
  const out: RefTarget[] = [];
  const seen = new Set<string>();
  for (const m of body.matchAll(/^#{1,6}\s+(.+?)\s*$/gm)) {
    const em = m[1].match(/\{#([A-Za-z0-9_-]+)\}\s*$/);
    const label = m[1].replace(/\s*\{#[A-Za-z0-9_-]+\}\s*$/, '');
    const id = em ? em[1] : label.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push({ value: id, hint: em ? label : label + ' — no {#id} on the heading yet' });
  }
  return out;
}

// ---------------------------------------------------------------- backlinks

export interface DocBacklink { from: string; kind: string; type?: string; id: string; title: string }

// chain order: the level below comes first (drivers < implements < delivers)
const BACKLINK_KIND_RANK: Record<string, number> = { driver: 0, implements: 1, delivers: 2, 'maps to': 3, verifies: 4, 'in text': 9 };
const KIND_LABEL: Record<string, string> = { drivers: 'driver', maps_to: 'maps to' };

/**
 * How a backlink reads from the TARGET document's side: the link type's
 * `inverse` label ("implemented by", "drives"), falling back to the raw kind.
 * Body mentions read "mentioned in".
 */
export function backlinkLabel(model: WorkspaceModel, kind: string): string {
  if (kind === 'in text') return 'mentioned in';
  const lt = model.linkTypes.find((l) =>
    l.name === kind || l.name.replace(/_/g, ' ') === kind || (kind === 'driver' && l.name === 'drivers'));
  return lt?.inverse || kind;
}

/**
 * Every inbound link to a document, generalized over the workspace's typed
 * link fields (the chain reads upward, so a WHY doc collects `driver`
 * backlinks from WHATs, a WHAT collects `implements` from HOWs, a HOW
 * collects `delivers` from WHENs), plus untyped body-text references.
 * Computed — never stored in the target document, so backlinks can never
 * drift from the forward links. A text mention is suppressed when the same
 * source document already links the target through a typed relation.
 */
export function collectBacklinks(model: WorkspaceModel): Record<string, DocBacklink[]> {
  const meta: Record<string, { id: string; title: string }> = {};
  model.docs.forEach((d) => { meta[d.path] = { id: d.id, title: d.title }; });
  const out: Record<string, DocBacklink[]> = {};
  const seen = new Set<string>();
  const pairSeen = new Set<string>();
  const add = (to: string, from: string, kind: string, type?: string, weak = false) => {
    const p = (to || '').split('#')[0];
    if (!/\.md$/.test(p) || p === from) return; // prose refs / self-links have no backlink
    const pair = p + '|' + from;
    if (weak && pairSeen.has(pair)) return;
    const key = pair + '|' + kind;
    if (seen.has(key)) return;
    seen.add(key);
    pairSeen.add(pair);
    const m = meta[from] || { id: '', title: '' };
    (out[p] = out[p] || []).push({ from, kind, type, id: m.id || from.split('/').pop()!, title: m.title });
  };
  model.docs.forEach((d) => {
    d.driversTyped.forEach((dr) => add(dr.ref, d.path, 'driver', dr.type || undefined));
    Object.entries(d.links).forEach(([field, refs]) => {
      if (field === 'drivers') return; // handled above with the derived type
      refs.forEach((t) => add(t, d.path, KIND_LABEL[field] || field.replace(/_/g, ' ')));
    });
  });
  (model.references || []).forEach((ref) => { if (!ref.external) add(ref.to, ref.from, 'in text', undefined, true); });
  Object.values(out).forEach((links) =>
    links.sort((a, b) => (BACKLINK_KIND_RANK[a.kind] ?? 5) - (BACKLINK_KIND_RANK[b.kind] ?? 5) || a.from.localeCompare(b.from)));
  return out;
}

// ---------------------------------------------------------------- link report

export interface LinkReportEntry {
  field: string;   // frontmatter key, or 'body' for prose links
  ref: string;     // the value as written (anchor included)
  target: string;  // resolved document path ('' when not a path)
  kind: string;    // target's entity kind ('' = file exists but unclassified)
  status: 'ok' | 'missing' | 'external' | 'prose' | 'undeclared';
  type?: string;   // derived driver type (drivers entries only)
}

/**
 * Everything the model sees (and does NOT see) as links in one document —
 * the debug view behind "why doesn't this backlink/edge show up". Includes
 * every parsed typed link with its resolution, untyped body references, and
 * — the usual culprit — list fields that LOOK like doc links but are not
 * declared link fields in this workspace (status `undeclared`: they produce
 * no backlinks and no graph edges).
 */
export function docLinkReport(files: Record<string, string>, model: WorkspaceModel, path: string) {
  const doc = model.docs.find((d) => d.path === path);
  const kindOf = (p: string) => model.docs.find((d) => d.path === p)?.kind || '';
  const dir = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
  const fm = files[path] !== undefined ? stripFrontmatter(files[path]).fm : '';
  const out: LinkReportEntry[] = [];
  // ref = as written; target = what the tolerant resolution lands on (the
  // panel shows both when they differ, so relative forms are visible)
  const entry = (field: string, ref: string, type?: string): LinkReportEntry => {
    if (ref.startsWith('~')) return { field, ref, target: ref, kind: '', status: 'external', type };
    const target = resolveFmRef(dir, ref, files).split('#')[0];
    if (files[target] !== undefined) return { field, ref, target, kind: kindOf(target), status: 'ok', type };
    if (!target.includes('/')) return { field, ref, target: '', kind: '', status: 'prose', type };
    return { field, ref, target, kind: '', status: 'missing', type };
  };
  if (doc) {
    for (const field of Object.keys(doc.links)) {
      const written = field === 'drivers' ? parseDriverEntries(fm).map((x) => x.ref) : getList(fm, field);
      written.forEach((ref) => {
        const resolved = resolveFmRef(dir, ref, files);
        const type = field === 'drivers' ? doc.driversTyped.find((x) => x.ref === resolved)?.type : undefined;
        out.push(entry(field, ref, type));
      });
    }
  }
  // list fields carrying path-looking values that are NOT declared link
  // fields — invisible to backlinks/graph, which is exactly what this view
  // is for (e.g. `satisfies:` in a workspace running on the defaults)
  const declared = new Set(model.linkFields);
  const pathLike = /^[\w-]+\/[\w./-]+$/;
  if (files[path] !== undefined) {
    for (const e of parseProps(stripFrontmatter(files[path]).fm)) {
      if (e.type !== 'list' || declared.has(e.key) || e.key === 'anchors') continue;
      if (!e.items.some((it) => pathLike.test(it.split('#')[0]))) continue;
      e.items.forEach((ref) => out.push({ ...entry(e.key, ref), status: 'undeclared' }));
    }
  }
  (model.references || []).filter((r) => r.from === path).forEach((r) =>
    out.push(r.external
      ? { field: 'body', ref: r.to, target: r.to, kind: '', status: 'external' }
      : { field: 'body', ref: r.to, target: r.to, kind: kindOf(r.to), status: 'ok' }));
  return {
    classified: !!doc,
    kind: doc?.kind || '',
    group: doc?.group || '',
    outbound: out,
    inbound: collectBacklinks(model)[path] || [],
  };
}

// ---------------------------------------------------------------- timed

/** A dependent document of a timed one, with whether it is ready in time. */
export interface TimedDep { path: string; name: string; title: string; kind: string; status: string; ready: boolean }

export interface TimedItem extends TimedDoc {
  /** where the document sits on its own window, relative to `today` */
  state: 'pending' | 'active' | 'expiring' | 'expired';
  /** days until the governing date (negative = in the past) */
  days: number;
  /** the date `days` counts down to — the start while pending, else the end */
  governing: string;
  deps: TimedDep[];
  readyCount: number;
  /** starts (or ends) inside the horizon while it or its dependents are not ready */
  atRisk: boolean;
}

export const TIMED_STATES = ['pending', 'active', 'expiring', 'expired'] as const;

/**
 * How an item's countdown reads as one phrase: "starts in 19d", "ends in 2mo",
 * "started 7mo ago". The verb follows the date `days` actually counts to — an
 * active window with no end counts from its START, so labelling it "active in
 * 7mo" (or "active 7mo ago") would be nonsense.
 */
export function windowPhrase(item: Pick<TimedItem, 'state' | 'days' | 'end'>): string {
  const when = daysLabel(item.days);
  if (item.state === 'pending') return `starts ${when}`;
  if (item.state === 'expired') return `ended ${when}`;
  if (item.end) return `ends ${when}`;
  return `started ${when}`;
}

const DAY = 86400000;
const dayDiff = (from: string, to: string) =>
  Math.round((new Date(to + 'T00:00:00').getTime() - new Date(from + 'T00:00:00').getTime()) / DAY);
const isDate = (s: string) => /^\d{4}-\d{2}-\d{2}/.test(s);

/**
 * Timed dependencies: every document with a validity window, bucketed against
 * `today` and joined with the documents that depend on it (inbound typed
 * links). `pending` is the interesting bucket — a window that has not opened
 * yet, with work possibly still unfinished behind it; `atRisk` marks the ones
 * whose horizon is close while something is not `ready_statuses` yet.
 *
 * `today` is a parameter so the buckets are testable; the view passes the
 * current date.
 */
export function buildTimed(model: WorkspaceModel, today: string, filter = 'all', selPath?: string | null) {
  const def = model.timedDef;
  const ready = new Set(def.readyStatuses);
  const backlinks = collectBacklinks(model);
  const byPath: Record<string, DocNode> = {};
  model.docs.forEach((d) => { byPath[d.path] = d; });

  const all: TimedItem[] = model.timed.map((t) => {
    const start = isDate(t.start) ? t.start.slice(0, 10) : '';
    const end = isDate(t.end) ? t.end.slice(0, 10) : '';
    const toStart = start ? dayDiff(today, start) : 0;
    const toEnd = end ? dayDiff(today, end) : 0;
    let state: TimedItem['state'] = 'active';
    let days = 0, governing = '';
    if (start && toStart > 0) { state = 'pending'; days = toStart; governing = start; }
    else if (end && toEnd < 0) { state = 'expired'; days = toEnd; governing = end; }
    else if (end && toEnd <= def.horizonDays) { state = 'expiring'; days = toEnd; governing = end; }
    else { state = 'active'; days = end ? toEnd : toStart; governing = end || start; }

    const deps: TimedDep[] = (backlinks[t.path] || []).map((b) => {
      const d = byPath[b.from];
      return {
        path: b.from, name: b.from.split('/').pop() || b.from,
        title: d?.title || b.from.split('/').pop() || b.from,
        kind: d?.kind || '', status: d?.status || '',
        ready: ready.has(d?.status || ''),
      };
    }).sort((a, b) => a.path.localeCompare(b.path));
    const readyCount = deps.filter((d) => d.ready).length;
    const selfReady = ready.has(t.status);
    const atRisk = (state === 'pending' || state === 'expiring') && days <= def.horizonDays
      && (!selfReady || readyCount < deps.length);
    return { ...t, start, end, state, days, governing, deps, readyCount, atRisk };
  }).sort((a, b) => {
    // soonest first within a bucket; expired counts down from the most recent
    const rank = TIMED_STATES.indexOf(a.state) - TIMED_STATES.indexOf(b.state);
    return rank || Math.abs(a.days) - Math.abs(b.days) || a.path.localeCompare(b.path);
  });

  const counts: Record<string, number> = { all: all.length };
  TIMED_STATES.forEach((s) => { counts[s] = all.filter((i) => i.state === s).length; });
  const items = all.filter((i) => filter === 'all' || i.state === filter);
  const sel = all.find((i) => i.path === selPath) || items[0] || null;
  return { items, sel, counts, atRisk: all.filter((i) => i.atRisk) };
}

// ---------------------------------------------------------------- dashboard

export interface DashTile { key: string; label: string; value: string; sub: string; valueStyle?: string }

/**
 * The Overview, steered by the workspace's type config: KPI tiles, the
 * traceability-health bars and the feed all derive from the configured
 * entities/groups/drivers — what the workspace doesn't have simply drops out
 * instead of rendering as zeros. Health follows the upward-reference chain:
 * WHATs cite drivers (outbound), WHATs are covered by inbound `implements`
 * from HOWs, HOWs by inbound `delivers` from WHENs.
 */
export function buildDashboard(model: WorkspaceModel, today = todayISO()) {
  const ents = model.entities;
  const what = primaryEntity(ents, 'what');
  const how = primaryEntity(ents, 'how');
  const when = primaryEntity(ents, 'when');
  const mapEnt = ents.find((e) => e.kind === 'data_mapping');
  const whatDocs = model.docs.filter((d) => d.group === 'what');
  const lower = (s: string) => s.toLowerCase();
  const singular = (s: string) => lower(s).replace(/ies$/, 'y').replace(/s$/, '');

  const backlinks = collectBacklinks(model);
  const groupOf: Record<string, string> = {};
  model.docs.forEach((d) => { groupOf[d.path] = d.group || ''; });
  const covered = (docs: DocNode[], kind: string, fromGroup: string) =>
    docs.filter((t) => (backlinks[t.path] || []).some((b) => b.kind === kind && groupOf[b.from] === fromGroup)).length;

  const timed = buildTimed(model, today);
  const drifts = model.fields.filter((f) => f.drift).length;
  const covVals = model.requirements.map((r) => r.coverage).filter((n) => n > 0);
  const cov = covVals.length ? Math.round((covVals.reduce((a, b) => a + b, 0) / covVals.length) * 100) : 0;
  const pct = (a: number, b: number) => (b ? Math.round((a / b) * 100) : 0);

  // health bars come from the CONFIGURED traceability section: each entry
  // measures one link type from one side — `from` = share of source docs
  // carrying the link, `to` = share of target docs covered by it. The
  // population is the first kind on the measured side; bars whose kinds
  // aren't configured (or whose `when` entity is hidden) drop out.
  const entByKind: Record<string, EntityDef> = {};
  ents.forEach((e) => { entByKind[e.kind] = e; });
  const kindDocs = (kind: string) => model.docs.filter((d) => d.kind === kind);
  const backlinkKind = (link: string) => (link === 'drivers' ? 'driver' : link === 'maps_to' ? 'maps to' : link);
  const palette = ['var(--reg)', 'var(--prod)', 'var(--ai)', 'var(--data)'];
  const health: { label: string; pct: number; color: string }[] = [];
  model.traceability.forEach((def, i) => {
    const lt = model.linkTypes.find((l) => l.name === def.link);
    if (!lt) return;
    if (def.when && !entByKind[def.when]) return;
    const split = (v: string) => v.split(',').map((x) => x.trim()).filter(Boolean);
    const fromKinds = split(lt.from), toKinds = split(lt.to);
    const fromEnt = entByKind[fromKinds[0]];
    if (!fromEnt) return;
    const color = def.color || palette[i % palette.length];
    if (def.measure === 'to') {
      const toEnt = entByKind[toKinds[0]];
      if (!toEnt) return;
      const base = kindDocs(toEnt.kind);
      if (!base.length) return;
      const kind = backlinkKind(def.link);
      const fromSet = new Set(fromKinds);
      const cov = base.filter((t) => (backlinks[t.path] || []).some((b) =>
        b.kind === kind && fromSet.has(model.docs.find((d) => d.path === b.from)?.kind || ''))).length;
      health.push({ label: def.label || `${toEnt.label} ← ${lower(fromEnt.label)}`, pct: pct(cov, base.length), color });
    } else {
      const base = kindDocs(fromEnt.kind);
      if (!base.length) return;
      const cov = base.filter((d) => (d.links[def.link] || []).length > 0).length;
      health.push({ label: def.label || `${fromEnt.label} → ${def.link.replace(/_/g, ' ')}`, pct: pct(cov, base.length), color });
    }
  });

  // KPI tiles — the timed tile only exists once documents carry a window
  const tiles: DashTile[] = [];
  if (model.timed.length) {
    tiles.push({
      key: 'timed', label: 'Pending dependencies', value: String(timed.counts.pending),
      sub: `${timed.counts.active} active · ${timed.counts.expiring} expiring · ${timed.counts.expired} expired`,
      valueStyle: timed.atRisk.length ? 'color:var(--reg)' : undefined,
    });
  }
  if (what) {
    const whatImplemented = covered(whatDocs, 'implements', 'how');
    tiles.push({
      key: 'what', label: what.label, value: String(whatDocs.length),
      sub: how ? `${whatImplemented} of ${whatDocs.length} implemented` : `${lower(what.description).split('—')[0].trim()}`,
    });
  }
  if (mapEnt) {
    tiles.push({ key: 'drifts', label: 'Mapping drifts', value: String(drifts), sub: 'need re-validation', valueStyle: 'color:var(--reg)' });
  }

  return {
    drifts, cov,
    showCov: !!what && whatDocs.length > 0,
    tiles,
    // the WHEN card: the windows opening or closing next, at-risk ones first —
    // a card of three must not let a distant pending window push out a
    // deadline that is actually in trouble
    hasTimed: model.timed.length > 0,
    timedCounts: timed.counts,
    upcoming: timed.items
      .filter((i) => i.state !== 'expired')
      .sort((a, b) => Number(b.atRisk) - Number(a.atRisk))
      .slice(0, 3),
    atRisk: timed.atRisk,
    newDoc: what ? { kind: what.kind, label: singular(what.label) } : null,
    mapEntity: !!mapEnt,
    health,
  };
}

// ---------------------------------------------------------------- graph

export interface GraphNode {
  id: string; col: number; label: string; sub: string; kind: string;
  x: number; y: number; w: number; boxStyle: string; labelStyle: string; subStyle: string;
  go?: string; // editor path this node opens (documents only)
}
export interface GraphEdge { d: string; stroke: string; dash?: boolean; a: string; b: string }

// deterministic per-node/per-edge variation (FNV-1a → [0,1)) — the layout
// reads organic instead of grid-locked, yet never moves between renders
const h01 = (s: string, salt = 0) => {
  let h = 2166136261 ^ salt;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return ((h >>> 0) % 1024) / 1024;
};

/**
 * Bezier path between two node anchor points: asymmetric control points and
 * a slight bow, both stable per seed — flowing strands instead of uniform
 * S-curves. Also used by GraphView to redraw edges live while nodes move.
 */
export function edgeCurve(x1: number, y1: number, x2: number, y2: number, seed: string): string {
  const t = h01(seed, 3);
  const dir = x2 >= x1 ? 1 : -1;
  const dx = Math.max(40, Math.abs(x2 - x1)) * dir;
  const c1x = x1 + dx * (0.3 + t * 0.25), c2x = x2 - dx * (0.3 + (1 - t) * 0.25);
  const bow = (t - 0.5) * Math.min(28, Math.abs(y2 - y1) * 0.35 + 10);
  return `M${x1} ${y1} C${c1x} ${y1 + bow} ${c2x} ${y2 - bow} ${x2} ${y2}`;
}

// group column meta: display label + legend colors along the axis
export const GROUP_META: Record<string, { label: string; fg: string; bg: string }> = {
  why: { label: 'Why', fg: 'var(--reg)', bg: 'var(--reg-bg)' },
  what: { label: 'What', fg: 'var(--prod)', bg: 'var(--prod-bg)' },
  how: { label: 'How', fg: 'var(--text-2)', bg: 'var(--surface-2)' },
  when: { label: 'When', fg: 'var(--ai)', bg: 'var(--ai-bg)' },
  field: { label: 'Data fields', fg: 'var(--data)', bg: 'var(--data-bg)' },
};

export interface GraphStat { key: string; label: string; fg: string; bg: string; count: number }

const graphStats = (nodes: GraphNode[], cols: string[]): GraphStat[] => {
  const stats = cols.map((g, i) => ({
    key: g, ...GROUP_META[g],
    count: nodes.filter((n) => n.col === i && n.kind !== 'field' && n.kind !== 'ext').length,
  }));
  const f = nodes.filter((n) => n.kind === 'field').length;
  if (f) stats.push({ key: 'field', ...GROUP_META.field, count: f });
  return stats;
};

/**
 * The traceability graph, config-driven: nodes are the classified documents
 * (custom entities included), columns are the groups present on the
 * WHY → WHAT → HOW → WHEN axis, and edges come from every typed link field a
 * document carries — the chain links read upward (lower level holds the
 * reference), drawn left→right along the axis. Data-mapping FIELD anchors
 * stay as nodes in the `how` column; `~source` body links join the leftmost
 * column as dashed externals.
 */
export function buildGraph(model: WorkspaceModel) {
  const docs = model.docs.filter((d) => d.group && (GROUP_ORDER as readonly string[]).includes(d.group));
  const fields = model.fields;
  const cols = GROUP_ORDER.filter((g) => docs.some((d) => d.group === g));
  if (!cols.length) cols.push('what' as (typeof GROUP_ORDER)[number]);
  const colOf: Record<string, number> = {};
  cols.forEach((g, i) => { colOf[g] = i; });
  const nCols = cols.length;
  const spanX = Math.max(696, (nCols - 1) * 300);
  const colX = cols.map((_, i) => (nCols > 1 ? 16 + Math.round((spanX * i) / (nCols - 1)) : 364));
  const colW = cols.map((_, i) => (i === nCols - 1 ? 176 : i === 0 ? 156 : 150));
  let H = 540; // grows with the densest column — layout() below
  const short = (ref: string) => ref.split('/').pop()!.split('#')[0].replace('.md', '');
  const dColor = (t: string) => (t === 'regulatory' ? 'var(--reg)' : t === 'product' ? 'var(--prod)' : t === 'technical' ? 'var(--text-2)'
    : model.driverDefs.find((d) => d.key === t)?.color || 'var(--text-2)');
  const dIcon = (t: string) => (t === 'regulatory' ? '⚖' : t === 'product' ? '◆' : t === 'technical' ? '⚙'
    : model.driverDefs.find((d) => d.key === t)?.icon || '');
  const entByKind: Record<string, EntityDef> = {};
  model.entities.forEach((e) => { entByKind[e.kind] = e; });
  const nodes: GraphNode[] = [];
  const idOf: Record<string, GraphNode> = {};
  const scatter = (o: GraphNode, c: number, i: number, count: number) => {
    const gap = H / (count + 1);
    o.x = colX[c] + Math.round((h01(o.id) - 0.5) * 22);
    o.w = colW[c];
    o.y = Math.round(gap * (i + 1) + (h01(o.id, 7) - 0.5) * Math.min(26, gap * 0.5));
  };
  const push = (id: string, col: number, o: Partial<GraphNode> & { label: string; sub: string; kind: string; color?: string; drift?: boolean }) => {
    const node = { ...o, id, col, x: 0, y: 0, w: 0, boxStyle: '', labelStyle: '', subStyle: '' } as GraphNode & { color?: string; drift?: boolean };
    nodes.push(node); idOf[id] = node;
  };
  docs.forEach((d) => {
    const ent = entByKind[d.kind];
    const col = colOf[d.group!];
    if (d.group === 'why') {
      // WHY docs carry their derived driver type as icon, accent color + sub
      const type = d.source || ent?.driver || '';
      const icon = (type && dIcon(type)) || ent?.icon || '◈';
      push('doc:' + d.path, col, { label: icon + ' ' + short(d.path), sub: type || ent?.kind.replace(/_/g, ' ') || '', kind: d.kind, color: type ? dColor(type) : ent?.color || 'var(--text-2)', go: d.path });
    } else if (d.group === 'what') {
      push('doc:' + d.path, col, { label: d.id || d.name, sub: d.title, kind: d.kind, go: d.path });
    } else if (d.group === 'when') {
      push('doc:' + d.path, col, { label: d.id || d.name, sub: d.title || ent?.kind.replace(/_/g, ' ') || '', kind: d.kind, color: ent?.color, go: d.path });
    } else {
      push('doc:' + d.path, col, { label: d.name, sub: ent?.kind.replace(/_/g, ' ') || 'spec', kind: d.kind, go: d.path });
    }
  });
  if (colOf.how !== undefined) {
    fields.forEach((f) => push('field:' + f.name, colOf.how, { label: f.name, sub: f.drift ? '⚠ drift' : '', kind: 'field', drift: f.drift, go: f.map }));
  }
  // seed every node with clear air around it: a fixed height packed dense
  // columns tighter than a box is tall, and the simulation's de-overlap
  // shoves turned that into the tangle — the auto-fit absorbs the bigger canvas
  const layout = () => {
    const byCol = cols.map((_, c) => nodes.filter((n) => n.col === c));
    H = Math.max(540, Math.max(...byCol.map((col) => col.length)) * 78);
    byCol.forEach((col, c) => col.forEach((o, i) => scatter(o, c, i, col.length)));
  };
  layout();
  const styleOf = (o: GraphNode & { color?: string; drift?: boolean }) => {
    const base = 'position:absolute;left:' + o.x + 'px;top:' + (o.y - 20) + 'px;width:' + o.w + 'px;padding:8px 10px;border-radius:9px;box-shadow:var(--shadow);';
    const group = entByKind[o.kind]?.group;
    if (o.kind === 'ext') {
      o.boxStyle = base + 'background:var(--surface);border:1px dashed var(--border-2)';
      o.labelStyle = "font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:var(--text-2)";
      o.subStyle = 'font-size:11px;font-weight:600;margin-top:1px;color:var(--text-3)';
    } else if (o.kind === 'field') {
      o.boxStyle = base + (o.drift ? 'background:var(--reg-bg);border:1px solid var(--reg-line)' : 'background:var(--data-bg);border:1px solid var(--data-line)');
      o.labelStyle = "font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:600;color:var(--data)";
      o.subStyle = 'font-size:10px;color:var(--reg);margin-top:1px';
    } else if (group === 'why') {
      o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2);border-left:3px solid ' + (o.color || 'var(--text-2)');
      o.labelStyle = "font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:" + (o.color || 'var(--text-2)');
      o.subStyle = 'font-size:12px;font-weight:600;margin-top:1px;text-transform:capitalize';
    } else if (group === 'when') {
      o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2);border-left:3px solid ' + (o.color || 'var(--data)');
      o.labelStyle = "font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:var(--text-2)";
      o.subStyle = 'font-size:11.5px;font-weight:600;margin-top:1px';
    } else if (group === 'how') {
      o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2)';
      o.labelStyle = 'font-size:12px;font-weight:600';
      o.subStyle = "font-family:'JetBrains Mono',monospace;font-size:9.5px;color:var(--text-3);margin-top:1px";
    } else {
      o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2)';
      o.labelStyle = "font-family:'JetBrains Mono',monospace;font-size:9.5px;color:var(--text-3)";
      o.subStyle = 'font-size:12px;font-weight:600;margin-top:1px';
    }
  };
  nodes.forEach((o) => styleOf(o));
  const edges: GraphEdge[] = [];
  // edges draw left→right along the axis whichever side holds the link
  const edge = (a: string, b: string, stroke: string, dash?: boolean) => {
    let p = idOf[a], q = idOf[b];
    if (!p || !q || a === b) return;
    if (q.col < p.col || (q.col === p.col && q.x < p.x)) { [p, q] = [q, p]; [a, b] = [b, a]; }
    edges.push({ d: edgeCurve(p.x + p.w, p.y, q.x, q.y, a + '>' + b), stroke, dash, a, b });
  };
  const resolveField = (ref: string): DataField | undefined => {
    const a = ref.split('#')[1] || '';
    return fields.find((f) => f.name === a || f.name.endsWith('.' + a));
  };
  const typed = new Set<string>();
  docs.forEach((d) => {
    Object.entries(d.links).forEach(([field, refs]) => {
      refs.forEach((ref) => {
        if (field === 'maps_to') {
          const f = resolveField(ref);
          if (f) edge('doc:' + d.path, 'field:' + f.name, 'var(--border-2)');
          return;
        }
        const tp = ref.split('#')[0];
        if (!idOf['doc:' + tp]) return;
        const stroke = field === 'drivers' ? dColor(d.driversTyped.find((x) => x.ref === ref)?.type || '') : 'var(--border-2)';
        edge('doc:' + d.path, 'doc:' + tp, stroke);
        typed.add('doc:' + d.path + '>doc:' + tp);
        typed.add('doc:' + tp + '>doc:' + d.path);
      });
    });
  });
  // untyped body-link references (OKF linking model) — dashed, and only
  // where both documents have a node; typed edges take precedence
  (model.references || []).forEach((ref) => {
    if (ref.external) return; // handled below
    const a = 'doc:' + ref.from, b = 'doc:' + ref.to;
    if (idOf[a] && idOf[b] && !typed.has(a + '>' + b)) edge(a, b, 'var(--border-2)', true);
  });
  // cross-repo references ("~source/path"): external nodes join the leftmost
  // column with dashed borders + dashed edges from the linking document
  const externals = (model.references || []).filter((r) => r.external && idOf['doc:' + r.from]);
  if (externals.length) {
    const seenExt = new Set<string>();
    externals.forEach((ref) => {
      const extID = 'ext:' + ref.to;
      if (!seenExt.has(extID)) {
        seenExt.add(extID);
        const srcName = ref.to.slice(1).split('/')[0];
        push(extID, 0, { label: '⇲ ' + short(ref.to), sub: '~' + srcName, kind: 'ext', go: ref.to });
      }
    });
    // relayout everything: the externals can make col 0 the densest, and H
    // (every column's spacing) follows the densest column
    layout();
    nodes.forEach((o) => styleOf(o));
    externals.forEach((ref) => edge('ext:' + ref.to, 'doc:' + ref.from, 'var(--border-2)', true));
  }
  return { nodes, edges, H, cols: cols as string[], stats: graphStats(nodes, cols as string[]) };
}

/**
 * The sub-graph connected to one document: seeds are every node the doc
 * backs (its own node, source nodes for refs into it, its data fields), and
 * the whole chain is followed up AND down through every edge kind (drivers,
 * implements, maps_to, body references). Kept columns are re-spread so a
 * small focus set doesn't float sparsely in the full graph's layout.
 * Unknown docs (nothing in the graph points at them) fall back to the full
 * graph rather than an empty canvas.
 */
export function focusGraph(g: ReturnType<typeof buildGraph>, docPath: string): ReturnType<typeof buildGraph> {
  const keep = new Set(g.nodes.filter((n) => (n.go || '').split('#')[0] === docPath).map((n) => n.id));
  if (!keep.size) return g;
  let grew = true;
  while (grew) {
    grew = false;
    for (const e of g.edges) {
      const a = keep.has(e.a), b = keep.has(e.b);
      if (a !== b) { keep.add(e.a); keep.add(e.b); grew = true; }
    }
  }
  const nodes = g.nodes.filter((n) => keep.has(n.id)).map((n) => ({ ...n }));
  const byCol: Record<number, GraphNode[]> = {};
  nodes.forEach((n) => (byCol[n.col] = byCol[n.col] || []).push(n));
  Object.values(byCol).forEach((col) => {
    col.sort((p, q) => p.y - q.y);
    const gap = g.H / (col.length + 1);
    col.forEach((n, i) => { n.y = Math.round(gap * (i + 1)); });
  });
  return {
    ...g,
    nodes,
    edges: g.edges.filter((e) => keep.has(e.a) && keep.has(e.b)),
    stats: graphStats(nodes, g.cols),
  };
}

// ---------------------------------------------------------------- source view

export interface SourceLine { n: number; text: string; color: string }

export function sourceLines(raw: string): SourceLine[] {
  let dashCount = 0, inFm = false, fenced = false;
  return raw.split('\n').map((t, i) => {
    const tr = t.trimStart();
    let color = 'var(--text)';
    if (t.trim() === '---' && dashCount < 2) { dashCount++; inFm = dashCount === 1; color = 'var(--text-3)'; }
    else if (inFm) { color = /^\s/.test(t) ? 'var(--text-2)' : 'var(--prod)'; }
    else if (/^```/.test(tr)) { fenced = !fenced; color = 'var(--ai)'; }
    else if (fenced) { color = 'var(--text-2)'; }
    else if (/^#{1,6}\s/.test(tr)) { color = 'var(--reg)'; }
    else if (/^\|/.test(tr)) { color = 'var(--data)'; }
    else if (/^([-*]\s|>\s|\d+\.\s)/.test(tr)) { color = 'var(--text-2)'; }
    return { n: i + 1, text: t, color };
  });
}

// ---------------------------------------------------------------- model view (taxonomy)

/** Effective taxonomy: the config's sections over the built-in defaults. */
export function parseTaxonomy(configYml: string) {
  const c = workspaceConfig(configYml);
  return { drivers: c.drivers, statuses: c.statuses, links: c.linkTypes };
}

// ---------------------------------------------------------------- misc

export function reqByName(model: WorkspaceModel, id: string): Requirement | undefined {
  return model.requirements.find((x) => x.id === id);
}

export function firstIn(model: WorkspaceModel, prefix: string): string {
  const all = [...model.regs, ...model.requirements, ...model.specs, ...model.maps];
  const hit = all.find((x) => x.path && x.path.startsWith(prefix));
  return hit ? hit.path : prefix;
}

export function diffLines(raw: string) {
  return raw.split('\n').map((ln) => {
    const sign = ln[0] === '+' ? '+' : ln[0] === '-' ? '-' : ' ';
    return {
      sign,
      text: ln.slice(1),
      rowStyle: sign === '+' ? 'background:var(--add-bg)' : sign === '-' ? 'background:var(--del-bg)' : '',
      signColor: sign === '+' ? 'var(--add)' : sign === '-' ? 'var(--del)' : 'var(--text-3)',
      textColor: sign === ' ' ? 'var(--text-2)' : 'var(--text)',
    };
  });
}
