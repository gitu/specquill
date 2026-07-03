// model.ts — typed port of repo-render.js: frontmatter parsing, workspace
// model building, and the read-only excalidraw→SVG renderer.

export interface Driver { type: string; ref: string }
export interface Regulation { path: string; name: string; id: string; title: string; drives: string[] }
export interface Requirement {
  path: string; name: string; id: string; title: string; status: string; owner: string;
  priority: string; businessValue: string; coverage: number;
  drivers: Driver[]; implements: string[]; maps_to: string[]; verifies: string[];
}
export interface Spec { path: string; name: string; id: string; title: string; status: string; implements: string[]; maps_to: string[] }
export interface MappingField { source: string; target: string; transform: string; status: string; drift: boolean }
export interface Mapping { path: string; name: string; fields: MappingField[] }
export interface Change {
  path: string; name: string; id: string; title: string; source: string; published: string; status: string;
  summary: string; impReqs: string[]; impSpecs: string[]; impMaps: string[]; diff: string;
}
export interface DataField { name: string; source: string; transform: string; status: string; drift: boolean; map: string }
export interface WorkspaceModel {
  regs: Regulation[]; requirements: Requirement[]; specs: Spec[];
  maps: Mapping[]; changes: Change[]; fields: DataField[];
}
export type PropEntry =
  | { key: string; type: 'scalar'; value: string }
  | { key: string; type: 'list'; items: string[] };

export function stripFrontmatter(md: string): { fm: string; body: string } {
  const m = md.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  return m ? { fm: m[1], body: m[2] } : { fm: '', body: md };
}

export function scalar(fm: string, key: string): string {
  const m = fm.match(new RegExp('^' + key + ':\\s*(.+)$', 'm'));
  return m ? m[1].trim().replace(/^["']|["']$/g, '') : '';
}

export function drivers(fm: string): Driver[] {
  const m = fm.match(/(?:^|\n)drivers:\s*\n([\s\S]*?)(?=\n[A-Za-z_][\w-]*:|$)/);
  if (!m) return [];
  return m[1].split(/-\s*type:/).slice(1).map((chunk) => {
    const t = (chunk.match(/^\s*([\w-]+)/) || [])[1] || '';
    const ref = (chunk.match(/ref:\s*(.+)/) || [])[1] || '';
    return { type: t.trim(), ref: ref.trim().replace(/^["']|["']$/g, '') };
  });
}

export function foldScalar(fm: string, key: string): string {
  const m = fm.match(new RegExp('(?:^|\\n)' + key + ':\\s*>\\s*\\n([\\s\\S]*?)(?=\\n[A-Za-z_][\\w-]*:|$)'));
  return m ? m[1].split('\n').map((l) => l.trim()).filter(Boolean).join(' ') : '';
}

const unq = (s: string) => String(s).trim().replace(/^["']|["']$/g, '');

export function getList(fm: string, key: string): string[] {
  const inl = fm.match(new RegExp('(?:^|\\n)' + key + ':\\s*\\[(.*?)\\]'));
  if (inl) return inl[1].split(',').map(unq).filter(Boolean);
  const blk = fm.match(new RegExp('(?:^|\\n)' + key + ':\\s*\\n([\\s\\S]*?)(?=\\n[a-zA-Z_]+:|$)'));
  if (!blk) return [];
  return blk[1].split('\n').map((l) => l.trim()).filter((l) => l.startsWith('- '))
    .map((l) => unq(l.slice(2))).filter((x) => x && !/^[\w-]+:/.test(x));
}

export function bracketList(fm: string, key: string): string[] {
  const m = fm.match(new RegExp('(?:^|\\n)\\s*' + key + ':\\s*\\[(.*?)\\]'));
  return m ? m[1].split(',').map(unq).filter(Boolean) : [];
}

export function parseMappingFields(body: string): MappingField[] {
  const rows: MappingField[] = [];
  (body || '').split('\n').forEach((line) => {
    if (!/^\s*\|/.test(line)) return;
    const cs = line.split('|').slice(1, -1).map((c) => c.trim());
    if (cs.length < 5 || /^-+$/.test(cs[0]) || !/^\d+$/.test(cs[0])) return;
    const status = cs[cs.length - 1];
    rows.push({ source: cs[1], target: cs[2], transform: cs[3], status, drift: /drift/i.test(status) });
  });
  return rows;
}

export function buildModel(files: Record<string, string>): WorkspaceModel {
  const base = (p: string) => p.split('/').pop()!;
  const of = (pre: string) => Object.keys(files).filter((p) => p.startsWith(pre)).sort();
  const P = (p: string) => stripFrontmatter(files[p]);

  const regs = of('regulations/').map((p) => { const d = P(p); return { path: p, name: base(p), id: scalar(d.fm, 'id'), title: scalar(d.fm, 'title'), drives: getList(d.fm, 'drives') }; });
  const requirements = of('requirements/').map((p) => { const d = P(p); return {
    path: p, name: base(p), id: scalar(d.fm, 'id'), title: scalar(d.fm, 'title'), status: scalar(d.fm, 'status'), owner: scalar(d.fm, 'owner'),
    priority: scalar(d.fm, 'priority'), businessValue: scalar(d.fm, 'business_value'), coverage: parseFloat(scalar(d.fm, 'coverage')) || 0,
    drivers: drivers(d.fm), implements: getList(d.fm, 'implements'), maps_to: getList(d.fm, 'maps_to'), verifies: getList(d.fm, 'verifies'),
  }; });
  const specs = of('specs/').map((p) => { const d = P(p); return { path: p, name: base(p), id: scalar(d.fm, 'id'), title: scalar(d.fm, 'title'), status: scalar(d.fm, 'status'), implements: getList(d.fm, 'implements'), maps_to: getList(d.fm, 'maps_to') }; });
  const maps = of('data-mappings/').map((p) => { const d = P(p); return { path: p, name: base(p), fields: parseMappingFields(d.body) }; });
  const changes = of('changes/').map((p) => { const d = P(p); return {
    path: p, name: base(p), id: scalar(d.fm, 'id'), title: scalar(d.fm, 'title'), source: scalar(d.fm, 'source'), published: scalar(d.fm, 'published'), status: scalar(d.fm, 'status'),
    summary: foldScalar(d.fm, 'ai_summary'), impReqs: bracketList(d.fm, 'requirements'), impSpecs: bracketList(d.fm, 'specs'), impMaps: bracketList(d.fm, 'mappings'), diff: extractDiff(files[p]),
  }; });
  const fields: DataField[] = [];
  maps.forEach((m) => m.fields.forEach((f) => {
    if (f.target && !fields.find((x) => x.name === f.target)) {
      fields.push({ name: f.target, source: f.source, transform: f.transform, status: f.status, drift: f.drift, map: m.path });
    }
  }));
  return { regs, requirements, specs, maps, changes, fields };
}

// Parse the whole frontmatter into ordered prop entries (scalars, inline and
// block lists, block maps like drivers, folded scalars).
export function parseProps(fm: string): PropEntry[] {
  const lines = fm.split('\n');
  const out: PropEntry[] = [];
  const topRe = /^([A-Za-z_][\w-]*):\s*(.*)$/;
  let i = 0;
  while (i < lines.length) {
    const m = lines[i].match(topRe);
    if (!m) { i++; continue; }
    const key = m[1];
    const rest = m[2].trim();
    if (rest && rest[0] === '[') {
      out.push({ key, type: 'list', items: rest.replace(/^\[|\]$/g, '').split(',').map(unq).filter(Boolean) });
      i++; continue;
    }
    if (rest === '>' || rest === '|') {
      i++; const buf: string[] = [];
      while (i < lines.length && /^\s+/.test(lines[i])) { buf.push(lines[i].trim()); i++; }
      out.push({ key, type: 'scalar', value: buf.filter(Boolean).join(' ') });
      continue;
    }
    if (rest) { out.push({ key, type: 'scalar', value: unq(rest) }); i++; continue; }
    i++;
    const items: string[] = [];
    while (i < lines.length && /^\s+\S/.test(lines[i])) {
      const li = lines[i].trim();
      if (li.startsWith('- ')) {
        const itemText = li.slice(2).trim();
        const mapM = itemText.match(/^([\w-]+):\s*(.*)$/);
        if (mapM) {
          const parts = [unq(mapM[2])].filter(Boolean);
          i++;
          while (i < lines.length && /^\s{3,}\S/.test(lines[i]) && !lines[i].trim().startsWith('- ')) {
            const sub = lines[i].trim().match(/^([\w-]+):\s*(.*)$/);
            if (sub) parts.push(unq(sub[2]));
            i++;
          }
          items.push(parts.filter(Boolean).join(' · '));
          continue;
        }
        items.push(unq(itemText)); i++; continue;
      }
      i++;
    }
    out.push({ key, type: 'list', items });
  }
  return out;
}

export function extractDiff(md: string): string {
  const m = md.match(/```diff\n([\s\S]*?)```/);
  return m ? m[1].replace(/\n+$/, '') : '';
}

export function esc(s: unknown): string {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function resolvePath(baseDir: string, rel: string): string {
  const parts = baseDir ? baseDir.split('/') : [];
  rel.split('/').forEach((seg) => {
    if (seg === '..') parts.pop();
    else if (seg === '.' || seg === '') { /* skip */ }
    else parts.push(seg);
  });
  return parts.join('/');
}

// ---------------------------------------------------------------- excalidraw

interface ExcalidrawElement {
  id: string; type: string; x: number; y: number; width?: number; height?: number;
  isDeleted?: boolean; strokeColor?: string; backgroundColor?: string; strokeWidth?: number;
  points?: [number, number][]; text?: string; fontSize?: number; containerId?: string | null;
}

// Render an .excalidraw doc (subset: rectangle, arrow, text) to an SVG string;
// cmap maps hex colors onto CSS custom properties so sketches follow the theme.
export function excalidrawToSvg(data: { elements?: ExcalidrawElement[] }, cmap: Record<string, string> = {}): string {
  const col = (c?: string) => (c && cmap[c]) || c || 'none';
  const els = (data.elements || []).filter((e) => !e.isDeleted);
  let minX = 1e9, minY = 1e9, maxX = -1e9, maxY = -1e9;
  const grow = (x: number, y: number) => { minX = Math.min(minX, x); minY = Math.min(minY, y); maxX = Math.max(maxX, x); maxY = Math.max(maxY, y); };
  els.forEach((e) => {
    if (e.type === 'rectangle' || e.type === 'text') { grow(e.x, e.y); grow(e.x + (e.width || 0), e.y + (e.height || 0)); }
    if (e.type === 'arrow') (e.points || []).forEach((p) => grow(e.x + p[0], e.y + p[1]));
  });
  const pad = 26; minX -= pad; minY -= pad; maxX += pad; maxY += pad;
  const W = Math.max(1, maxX - minX), H = Math.max(1, maxY - minY);

  const rects = els.filter((e) => e.type === 'rectangle').map((e) =>
    `<rect x="${e.x}" y="${e.y}" width="${e.width}" height="${e.height}" rx="10" fill="${col(e.backgroundColor)}" stroke="${col(e.strokeColor)}" stroke-width="${e.strokeWidth || 2}"/>`).join('');

  const arrows = els.filter((e) => e.type === 'arrow').map((e) => {
    const pts = (e.points && e.points.length ? e.points : [[0, 0], [e.width || 0, e.height || 0]] as [number, number][]).map((p) => [e.x + p[0], e.y + p[1]]);
    const d = pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1)).join(' ');
    const [ex, ey] = pts[pts.length - 1], [px, py] = pts[pts.length - 2] || pts[0];
    const a = Math.atan2(ey - py, ex - px), h = 9;
    const head = `M${(ex - h * Math.cos(a - 0.5)).toFixed(1)} ${(ey - h * Math.sin(a - 0.5)).toFixed(1)} L${ex.toFixed(1)} ${ey.toFixed(1)} L${(ex - h * Math.cos(a + 0.5)).toFixed(1)} ${(ey - h * Math.sin(a + 0.5)).toFixed(1)}`;
    const s = col(e.strokeColor);
    return `<path d="${d}" fill="none" stroke="${s}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="${head}" fill="none" stroke="${s}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`;
  }).join('');

  const texts = els.filter((e) => e.type === 'text').map((e) => {
    let x = e.x, y = e.y + (e.fontSize || 18) * 0.8, anchor = 'start';
    const cont = e.containerId && els.find((k) => k.id === e.containerId);
    if (cont) { x = cont.x + (cont.width || 0) / 2; y = cont.y + (cont.height || 0) / 2; anchor = 'middle'; }
    const lines = String(e.text || '').split('\n');
    const fs = e.fontSize || 18;
    const y0 = cont ? y - (lines.length - 1) * fs * 0.6 : y;
    return lines.map((ln, i) =>
      `<text x="${x.toFixed(1)}" y="${(y0 + i * fs * 1.2).toFixed(1)}" font-family="Kalam, cursive" font-size="${fs}" fill="${col(e.strokeColor)}" text-anchor="${anchor}" dominant-baseline="${cont ? 'middle' : 'auto'}">${esc(ln)}</text>`
    ).join('');
  }).join('');

  return `<svg viewBox="${minX} ${minY} ${W} ${H}" width="100%" style="max-width:${Math.round(W)}px;display:block;margin:0 auto" xmlns="http://www.w3.org/2000/svg">${rects}${arrows}${texts}</svg>`;
}

// theme-following color map used wherever sketches render inline
export const EXCALIDRAW_CMAP: Record<string, string> = {
  '#1a1e24': 'var(--text)', '#2563c9': 'var(--prod)', '#e5edfb': 'var(--prod-bg)',
  '#12876a': 'var(--data)', '#daf0e8': 'var(--data-bg)', '#5a616b': 'var(--text-2)',
  '#b06f16': 'var(--reg)', '#ffffff': 'var(--surface)', transparent: 'transparent',
};
