// derive.ts — pure "model → view data" functions ported from the prototype's
// build*/renderVals methods. Styles are composed as strings (rendered via sx())
// exactly like the design; navigation is carried as paths, not callbacks.

import { Change, DataField, PropEntry, Requirement, WorkspaceModel, isReservedMd, parseProps } from './model';
import type { EntityDef } from './entities';
import type { PropertySchema } from '../state/AppContext';

export const srcMeta = (s: string) =>
  ({
    regulatory: { icon: '⚖', label: 'Regulatory', fg: 'var(--reg)', bg: 'var(--reg-bg)' },
    product: { icon: '◆', label: 'Product', fg: 'var(--prod)', bg: 'var(--prod-bg)' },
    technical: { icon: '⚙', label: 'Technical', fg: 'var(--text-2)', bg: 'var(--surface-2)' },
  })[s] || { icon: '•', label: s || 'Change', fg: 'var(--text-2)', bg: 'var(--surface-2)' };

export function statusMeta(s: string): { label: string; color: string } {
  const v = String(s || '').toLowerCase();
  const m: Record<string, [string, string]> = {
    triage: ['Triage', 'var(--reg)'], in_progress: ['In progress', 'var(--prod)'],
    auto_remapped: ['Auto-remapped', 'var(--data)'], backlog: ['Backlog', 'var(--text-3)'],
    done: ['Done', 'var(--data)'], merged: ['Merged', 'var(--data)'],
  };
  return m[v] ? { label: m[v][0], color: m[v][1] } : { label: (s || '').replace(/_/g, ' '), color: 'var(--text-2)' };
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
}
export interface TreeFolder { name: string; desc?: string; files: TreeFile[] }

export function buildTree(files: Record<string, string>, openPath: string | undefined, gitStatus: Record<string, string>, entities: EntityDef[]): TreeFolder[] {
  const meta: Record<string, { icon: string; color: string; desc: string }> = {};
  const order: string[] = [];
  entities.forEach((e) => {
    const name = e.folder.replace(/\/$/, '');
    meta[name] = { icon: e.icon, color: e.color, desc: e.description };
    order.push(name);
  });
  const byFolder: Record<string, string[]> = {};
  Object.keys(files).forEach((p) => {
    const folder = p.split('/')[0];
    // root files and dot-folders (.specquill) stay out of the tree
    if (!p.includes('/') || folder.startsWith('.')) return;
    (byFolder[folder] = byFolder[folder] || []).push(p);
  });
  // entity folders first (config order), then any other folder alphabetically
  const names = [...order.filter((f) => byFolder[f]), ...Object.keys(byFolder).filter((f) => !meta[f]).sort()];
  return names.map((folder) => {
    const fm = meta[folder] || { icon: '▢', color: 'var(--text-2)', desc: '' };
    return {
      name: folder,
      desc: fm.desc,
      files: byFolder[folder].sort().map((path) => {
        const n = path.split('/').pop()!;
        const badge = gitStatus[path] || '';
        return {
          path, name: n,
          icon: n.endsWith('.mermaid') ? '⌗' : fm.icon,
          color: fm.color,
          badge,
          badgeStyle: badge === 'A' ? 'color:var(--add)' : 'color:var(--reg)',
          active: path === openPath,
        };
      }),
    };
  });
}

// ---------------------------------------------------------------- properties

export interface PropItem { text: string; style: string; openPath?: string }
export interface PropRow { key: string; items: PropItem[] }

const PAL: Record<string, { fg: string; bg: string }> = {
  green: { fg: 'var(--data)', bg: 'var(--data-bg)' }, amber: { fg: 'var(--reg)', bg: 'var(--reg-bg)' },
  blue: { fg: 'var(--prod)', bg: 'var(--prod-bg)' }, violet: { fg: 'var(--ai)', bg: 'var(--ai-bg)' },
  slate: { fg: 'var(--text-2)', bg: 'var(--surface-2)' },
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
      if (type === 'enum') { const cn = (def.values || {})[String(v).toLowerCase()] || 'slate'; items = [{ text: v, style: badge(PAL[cn] || PAL.slate) }]; }
      else if (type === 'percent') { const n = parseFloat(v) || 0; const pct = Math.round(n <= 1 ? n * 100 : n); const c = pct > 80 ? 'var(--data)' : pct > 60 ? 'var(--prod)' : 'var(--reg)'; items = [{ text: pct + '%', style: 'display:inline-flex;padding:2px 10px;border-radius:20px;font-size:11.5px;font-weight:600;background:var(--surface-2);color:' + c }]; }
      else if (type === 'user') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text)', true) }];
      else if (type === 'code') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text-2)', true) }];
      else if (type === 'tag') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text-2)', false, true) }];
      else if (type === 'date') items = [{ text: v, style: "font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-2)" }];
      else items = [{ text: v, style: 'font-size:13px;color:var(--text);line-height:1.5' }];
    } else {
      items = e.items.map((it) => (type === 'code' || type === 'anchors') ? { text: it, style: chip('var(--surface-2)', 'var(--text-2)', true) } : linkItem(it));
    }
    return { key: label, items };
  });
}

// ---------------------------------------------------------------- changes

export interface ChangeItem extends Change {
  ago: string; nImpacted: number;
}

export function buildChanges(model: WorkspaceModel, filter: string, selPath?: string | null) {
  const order: Record<string, number> = { triage: 0, in_progress: 1, auto_remapped: 2, backlog: 3 };
  const all: ChangeItem[] = model.changes
    .map((c) => ({ ...c, ago: daysAgo(c.published), nImpacted: c.impReqs.length + c.impSpecs.length + c.impMaps.length }))
    .sort((a, b) => (order[a.status] ?? 9) - (order[b.status] ?? 9));
  const counts = { all: all.length, regulatory: 0, product: 0, technical: 0 };
  all.forEach((c) => { if (c.source in counts) counts[c.source as keyof typeof counts]++; });
  const items = all.filter((c) => filter === 'all' || c.source === filter);
  const sel = all.find((c) => c.path === (selPath || (items[0] && items[0].path))) || items[0] || null;
  return { items, sel, counts };
}

// ---------------------------------------------------------------- dashboard

export function buildDashboard(model: WorkspaceModel) {
  const openChanges = model.changes.filter((c) => c.status !== 'done' && c.status !== 'merged');
  const bySource = { regulatory: 0, product: 0, technical: 0 };
  openChanges.forEach((c) => { if (c.source in bySource) bySource[c.source as keyof typeof bySource]++; });
  const drifts = model.fields.filter((f) => f.drift).length;
  const covVals = model.requirements.map((r) => r.coverage).filter((n) => n > 0);
  const cov = covVals.length ? Math.round((covVals.reduce((a, b) => a + b, 0) / covVals.length) * 100) : 0;
  const reqWithDriver = model.requirements.filter((r) => r.drivers.length).length;
  const reqWithSpec = model.requirements.filter((r) => r.implements.some((p) => p.startsWith('specs/'))).length;
  const specWithField = model.specs.filter((s) => s.maps_to.length).length;
  const pct = (a: number, b: number) => (b ? Math.round((a / b) * 100) : 0);
  return {
    openCount: openChanges.length, bySource,
    specCount: model.specs.length, reqCount: model.requirements.length, drifts, cov,
    feed: buildChanges(model, 'all').items.slice(0, 3),
    health: [
      { label: 'Requirements → drivers', pct: pct(reqWithDriver, model.requirements.length), color: 'var(--reg)' },
      { label: 'Requirements → specs', pct: pct(reqWithSpec, model.requirements.length), color: 'var(--prod)' },
      { label: 'Specs → data fields', pct: pct(specWithField, model.specs.length), color: 'var(--data)' },
    ],
  };
}

// ---------------------------------------------------------------- graph

export interface GraphNode {
  id: string; col: number; label: string; sub: string; kind: string;
  x: number; y: number; w: number; boxStyle: string; labelStyle: string; subStyle: string;
  go?: string; // editor path this node opens (documents only)
}
export interface GraphEdge { d: string; stroke: string; dash?: boolean; a: string; b: string }

export function buildGraph(model: WorkspaceModel) {
  const reqs = model.requirements, specs = model.specs, fields = model.fields;
  const srcMap: Record<string, { key: string; type: string; ref: string }> = {};
  reqs.forEach((r) => r.drivers.forEach((d) => { const k = d.type + '|' + d.ref; if (!srcMap[k]) srcMap[k] = { key: k, type: d.type, ref: d.ref }; }));
  const sources = Object.values(srcMap);
  const short = (ref: string) => ref.split('/').pop()!.split('#')[0].replace('.md', '');
  const sColor = (t: string) => (t === 'regulatory' ? 'var(--reg)' : t === 'product' ? 'var(--prod)' : 'var(--text-2)');
  const sIcon = (t: string) => (t === 'regulatory' ? '⚖' : t === 'product' ? '◆' : '⚙');
  const colX = [16, 250, 486, 712], colW = [156, 150, 150, 176], H = 540;
  const nodes: GraphNode[] = [];
  const idOf: Record<string, GraphNode> = {};
  // deterministic per-node/per-edge variation (FNV-1a → [0,1)) — the layout
  // reads organic instead of grid-locked, yet never moves between renders
  const h01 = (s: string, salt = 0) => {
    let h = 2166136261 ^ salt;
    for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
    return ((h >>> 0) % 1024) / 1024;
  };
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
  // clicking a node opens its document; only refs that are workspace docs get one
  const docRef = (ref: string) => { const p = ref.split('#')[0]; return /\.md$/.test(p) ? p : undefined; };
  sources.forEach((s) => push('src:' + s.key, 0, { label: sIcon(s.type) + ' ' + short(s.ref), sub: s.type, kind: 'src', color: sColor(s.type), go: docRef(s.ref) }));
  reqs.forEach((r) => push('req:' + r.path, 1, { label: r.id, sub: r.title, kind: 'req', go: r.path }));
  specs.forEach((sp) => push('spec:' + sp.path, 2, { label: sp.name, sub: 'spec', kind: 'spec', go: sp.path }));
  fields.forEach((f) => push('field:' + f.name, 3, { label: f.name, sub: f.drift ? '⚠ drift' : '', kind: 'field', drift: f.drift, go: f.map }));
  [0, 1, 2, 3].forEach((c) => {
    const col = nodes.filter((n) => n.col === c);
    col.forEach((o, i) => scatter(o, c, i, col.length));
  });
  nodes.forEach((o) => {
    const n = o as GraphNode & { color?: string; drift?: boolean };
    const base = 'position:absolute;left:' + o.x + 'px;top:' + (o.y - 20) + 'px;width:' + o.w + 'px;padding:8px 10px;border-radius:9px;box-shadow:var(--shadow);';
    if (o.kind === 'src') o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2);border-left:3px solid ' + n.color;
    else if (o.kind === 'field') o.boxStyle = base + (n.drift ? 'background:var(--reg-bg);border:1px solid var(--reg-line)' : 'background:var(--data-bg);border:1px solid var(--data-line)');
    else o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2)';
    o.labelStyle = o.kind === 'src' ? "font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:" + n.color
      : o.kind === 'field' ? "font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:600;color:var(--data)"
      : o.kind === 'spec' ? 'font-size:12px;font-weight:600'
      : "font-family:'JetBrains Mono',monospace;font-size:9.5px;color:var(--text-3)";
    o.subStyle = o.kind === 'field' ? 'font-size:10px;color:var(--reg);margin-top:1px'
      : o.kind === 'spec' ? "font-family:'JetBrains Mono',monospace;font-size:9.5px;color:var(--text-3);margin-top:1px"
      : 'font-size:12px;font-weight:600;margin-top:1px;text-transform:capitalize';
  });
  const edges: GraphEdge[] = [];
  const edge = (a: string, b: string, stroke: string, dash?: boolean) => {
    const p = idOf[a], q = idOf[b];
    if (!p || !q) return;
    const x1 = p.x + p.w, y1 = p.y, x2 = q.x, y2 = q.y;
    // asymmetric control points + a slight bow per edge — flowing strands
    // instead of uniform S-curves; t is stable per (from,to) pair
    const t = h01(a + '>' + b, 3);
    const dx = Math.max(24, x2 - x1);
    const c1x = x1 + dx * (0.3 + t * 0.25), c2x = x2 - dx * (0.3 + (1 - t) * 0.25);
    const bow = (t - 0.5) * Math.min(28, Math.abs(y2 - y1) * 0.35 + 10);
    edges.push({ d: `M${x1} ${y1} C${c1x} ${y1 + bow} ${c2x} ${y2 - bow} ${x2} ${y2}`, stroke, dash, a, b });
  };
  const resolveField = (ref: string): DataField | undefined => {
    const a = ref.split('#')[1] || '';
    return fields.find((f) => f.name === a || f.name.endsWith('.' + a));
  };
  reqs.forEach((r) => {
    r.drivers.forEach((d) => edge('src:' + (d.type + '|' + d.ref), 'req:' + r.path, sColor(d.type)));
    r.implements.filter((p) => p.startsWith('specs/')).forEach((sp) => edge('req:' + r.path, 'spec:' + sp, 'var(--border-2)'));
    r.maps_to.forEach((ref) => { const f = resolveField(ref); if (f) edge('req:' + r.path, 'field:' + f.name, 'var(--border-2)'); });
  });
  // untyped body-link references (OKF linking model) — dashed, and only
  // where both documents have a node; typed edges take precedence
  const nodeIdFor = (path: string) => (idOf['req:' + path] ? 'req:' + path : idOf['spec:' + path] ? 'spec:' + path : '');
  const typed = new Set(reqs.flatMap((r) => r.implements.map((sp) => 'req:' + r.path + '>spec:' + sp)));
  (model.references || []).forEach((ref) => {
    if (ref.external) return; // handled below
    const a = nodeIdFor(ref.from), b = nodeIdFor(ref.to);
    if (a && b && !typed.has(a + '>' + b) && !typed.has(b + '>' + a)) edge(a, b, 'var(--border-2)', true);
  });
  // cross-repo references ("~source/path"): external nodes join the sources
  // column with dashed borders + dashed edges from the linking document
  const externals = (model.references || []).filter((r) => r.external && nodeIdFor(r.from));
  if (externals.length) {
    const seenExt = new Set<string>();
    externals.forEach((ref) => {
      const extID = 'ext:' + ref.to;
      if (!seenExt.has(extID)) {
        seenExt.add(extID);
        const short = ref.to.split('/').pop()!.replace('.md', '');
        const srcName = ref.to.slice(1).split('/')[0];
        push(extID, 0, { label: '⇲ ' + short, sub: '~' + srcName, kind: 'src', color: 'var(--text-2)', go: ref.to });
      }
    });
    // relayout + restyle the sources column with the new members
    const col = nodes.filter((n) => n.col === 0);
    col.forEach((o, i) => scatter(o, 0, i, col.length));
    seenExt.forEach((extID) => {
      const n = idOf[extID];
      const base = 'position:absolute;left:' + n.x + 'px;top:' + (n.y - 20) + 'px;width:' + n.w + 'px;padding:8px 10px;border-radius:9px;box-shadow:var(--shadow);';
      n.boxStyle = base + 'background:var(--surface);border:1px dashed var(--border-2)';
      n.labelStyle = "font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:var(--text-2)";
      n.subStyle = 'font-size:11px;font-weight:600;margin-top:1px;color:var(--text-3)';
    });
    externals.forEach((ref) => edge('ext:' + ref.to, nodeIdFor(ref.from), 'var(--border-2)', true));
  }
  return { nodes, edges, H, stats: { s: sources.length, r: reqs.length, sp: specs.length, f: fields.length } };
}

// ---------------------------------------------------------------- matrix

export function buildMatrix(model: WorkspaceModel) {
  const specs = model.specs, fields = model.fields, reqs = model.requirements;
  const CW = 26;
  const columns: { kind: string; ref: string; label: string; drift?: boolean }[] = [];
  specs.forEach((s) => columns.push({ kind: 'spec', ref: s.path, label: s.name.replace('.md', '') }));
  fields.forEach((f) => columns.push({ kind: 'field', ref: f.name, label: f.name.split('.').pop()!, drift: f.drift }));
  columns.push({ kind: 'test', ref: 'tests', label: 'tests' });
  const mgroups = [
    { label: 'Specs', color: 'var(--text-2)', width: specs.length * CW },
    { label: 'Data fields', color: 'var(--data)', width: fields.length * CW },
    { label: 'Tests', color: 'var(--prod)', width: CW },
  ];
  const sqBase = 'width:15px;height:15px;border-radius:4px;box-sizing:border-box;';
  const sq = (t: string) =>
    t === 'linked' ? sqBase + 'background:var(--data);border:1px solid var(--data)'
    : t === 'drift' ? sqBase + 'background:var(--reg);border:1px solid var(--reg)'
    : 'width:5px;height:5px;border-radius:50%;background:var(--border-2)';
  const fieldNameFromRef = (ref: string) => {
    const a = ref.split('#')[1] || '';
    const f = fields.find((x) => x.name === a || x.name.endsWith('.' + a));
    return f ? f.name : null;
  };
  const mrows = reqs.map((r) => {
    const mappedFields = new Set(r.maps_to.map(fieldNameFromRef).filter(Boolean));
    const cells = columns.map((c) => {
      let t = 'none';
      if (c.kind === 'spec') t = r.implements.indexOf(c.ref) >= 0 ? 'linked' : 'none';
      else if (c.kind === 'field') t = mappedFields.has(c.ref) ? (c.drift ? 'drift' : 'linked') : 'none';
      else if (c.kind === 'test') t = r.verifies.length ? 'linked' : 'none';
      return { sq: sq(t) };
    });
    const cov = Math.round((r.coverage || 0) * 100);
    const covC = cov > 80 ? 'var(--data)' : cov > 64 ? 'var(--prod)' : 'var(--reg)';
    return { id: r.id, name: r.title, cells, cov, covStyle: 'width:' + cov + '%;height:100%;background:' + covC };
  });
  return { mgroups, mcolumns: columns, mrows, caption: `${reqs.length} requirements × ${columns.length} artifacts` };
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

export function parseTaxonomy(configYml: string) {
  const yml = configYml || '';
  const driverBlock = (yml.match(/drivers:\s*\n([\s\S]*?)(?=\nstatuses:|\n[a-z_]+:\s*\n|$)/) || [])[1] || '';
  const drivers: { key: string; label: string; icon: string; color: string }[] = [];
  driverBlock.split('\n').forEach((l) => {
    const m = l.match(/^\s{2}([\w-]+):\s*\{\s*label:\s*"([^"]*)",\s*icon:\s*"([^"]*)",\s*color:\s*"([^"]*)"/);
    if (m) drivers.push({ key: m[1], label: m[2], icon: m[3], color: m[4] });
  });
  const statuses = ((yml.match(/statuses:\s*\[(.*?)\]/) || [])[1] || '').split(',').map((s) => s.trim()).filter(Boolean);
  const linkBlock = (yml.match(/link_types:\s*\n([\s\S]*?)(?=\npaths:|\n[a-z_]+:\s*\n|$)/) || [])[1] || '';
  const links: { name: string; from: string; to: string }[] = [];
  linkBlock.split('\n').forEach((l) => {
    const m = l.match(/^\s{2}([\w-]+):\s*\{\s*from:\s*(\[[^\]]*\]|[\w-]+),\s*to:\s*([\w-]+)/);
    if (m) links.push({ name: m[1], from: m[2].replace(/[[\]]/g, ''), to: m[3] });
  });
  return { drivers, statuses, links };
}

// ---------------------------------------------------------------- misc

export function reqByName(model: WorkspaceModel, id: string): Requirement | undefined {
  return model.requirements.find((x) => x.id === id);
}

export function firstIn(model: WorkspaceModel, prefix: string): string {
  const all = [...model.regs, ...model.requirements, ...model.specs, ...model.maps, ...model.changes];
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
