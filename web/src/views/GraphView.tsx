import { useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useParams } from 'react-router-dom';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildGraph, edgeCurve, focusGraph } from '../lib/derive';
import { Loading } from './Dashboard';
import { docTabsStrip } from './EditorView';

const NODE_H = 46; // approximate box height for collision purposes
const ZOOM_MIN = 0.3, ZOOM_MAX = 2.5;

// a node's live physics state (positions are box centers)
interface Body {
  id: string; x: number; y: number; vx: number; vy: number;
  w: number; h: number;
  hx: number; hy: number; // "home" anchor — dragging re-homes the node
  pinned?: boolean;       // user-placed: strong anchor, springs can't drag it back
}

export function GraphView() {
  const nav = useNav();
  const app = useApp();
  // /graph/<docPath> scopes the graph to that document's chain; ?full=1
  // keeps the doc context (for the editor tab) but shows everything
  const { '*': focusPath = '' } = useParams();
  const full = new URLSearchParams(useLocation().search).has('full');
  const [hover, setHover] = useState<string | null>(null);
  const [zoom, setZoom] = useState(1);
  const [, setFrame] = useState(0); // bumped per simulation tick
  const g = useMemo(() => {
    if (!app.model) return null;
    const base = buildGraph(app.model);
    return focusPath && !full ? focusGraph(base, focusPath) : base;
  }, [app.model, focusPath, full]);
  const focusName = focusPath ? focusPath.split('/').pop()! : '';
  const bodies = useRef<Map<string, Body>>(new Map());
  const alpha = useRef(0);
  const raf = useRef(0);
  const drag = useRef<{ id: string; offX: number; offY: number; moved: boolean } | null>(null);
  const canvas = useRef<HTMLDivElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  // the graph is a pan/zoom VIEWPORT, never a scrolling page: plain wheel
  // pans, ctrl/cmd+wheel (and trackpad pinch) zooms at the cursor, dragging
  // the background pans, and the layout auto-fits into view
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const zoomRef = useRef(zoom);
  const panRef = useRef(pan);
  zoomRef.current = zoom;
  panRef.current = pan;
  const visibleRef = useRef(new Set<string>());
  const fitOnce = useRef('');
  const zoomBy = (f: number) => setZoom((z) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z * f)));
  const zoomAt = (clientX: number, clientY: number, f: number) => {
    const el = scroller.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const z0 = zoomRef.current;
    const z1 = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z0 * f));
    if (z1 === z0) return;
    const px = clientX - r.left, py = clientY - r.top;
    // keep the world point under the cursor fixed while the scale changes
    setPan({ x: px - ((px - panRef.current.x) / z0) * z1, y: py - ((py - panRef.current.y) / z0) * z1 });
    setZoom(z1);
  };

  // native non-passive listener because React's root wheel handler cannot
  // preventDefault — and every wheel event must stay inside the viewport
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) {
        zoomAt(e.clientX, e.clientY, e.deltaY < 0 ? 1.12 : 1 / 1.12);
      } else {
        setPan((p) => ({ x: p.x - e.deltaX, y: p.y - e.deltaY }));
      }
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
    // keyed on g: the first render shows <Loading/> with no scroller element
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [g]);

  // fit the visible graph into the viewport (auto on load/focus, and via the
  // zoom-percent button)
  const fit = () => {
    const el = scroller.current;
    if (!el) return;
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    bodies.current.forEach((b) => {
      if (!visibleRef.current.has(b.id)) return;
      minX = Math.min(minX, b.x - b.w / 2); maxX = Math.max(maxX, b.x + b.w / 2);
      minY = Math.min(minY, b.y - b.h / 2); maxY = Math.max(maxY, b.y + b.h / 2);
    });
    if (minX > maxX) return;
    const pad = 48;
    const vw = el.clientWidth, vh = el.clientHeight;
    const z = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.min(vw / (maxX - minX + pad * 2), vh / (maxY - minY + pad * 2), 1.15)));
    setZoom(z);
    setPan({ x: (vw - (maxX - minX) * z) / 2 - minX * z, y: (vh - (maxY - minY) * z) / 2 - minY * z });
  };

  // selectable layers + node text filter — hidden nodes leave the simulation
  // entirely (their edges too), so the remaining graph re-settles around them
  const [layersOff, setLayersOff] = useState<Set<string>>(new Set());
  const [nodeFilter, setNodeFilter] = useState('');
  const visible = useMemo(() => {
    if (!g) return new Set<string>();
    const q = nodeFilter.trim().toLowerCase();
    return new Set(g.nodes.filter((n) =>
      !layersOff.has(n.kind === 'field' ? 'field' : g.cols[n.col] || '') &&
      (!q || n.label.toLowerCase().includes(q) || n.sub.toLowerCase().includes(q) || (n.go || '').toLowerCase().includes(q)),
    ).map((n) => n.id));
  }, [g, layersOff, nodeFilter]);
  visibleRef.current = visible;

  // the hovered node's lineage: itself plus every node it shares an edge with
  const linked = useMemo(() => {
    if (!hover || !g) return null;
    const s = new Set([hover]);
    g.edges.forEach((e) => {
      if (!visible.has(e.a) || !visible.has(e.b)) return;
      if (e.a === hover) s.add(e.b);
      if (e.b === hover) s.add(e.a);
    });
    return s;
  }, [hover, g, visible]);

  // seed bodies from the deterministic layout — keyed on the NODE SET, not
  // the model object: background query refreshes rebuild `g` with identical
  // nodes, and replacing the bodies would reset the physics mid-interaction
  // and wipe the user's arrangement
  const nodeSig = g ? g.nodes.map((n) => n.id).sort().join('|') : '';
  useEffect(() => {
    if (!g) return;
    const m = bodies.current;
    const keep = new Set<string>();
    g.nodes.forEach((n) => {
      keep.add(n.id);
      if (!m.has(n.id)) {
        const x = n.x + n.w / 2, y = n.y;
        m.set(n.id, { id: n.id, x, y, vx: 0, vy: 0, w: n.w, h: NODE_H, hx: x, hy: y });
      }
    });
    [...m.keys()].forEach((id) => { if (!keep.has(id)) m.delete(id); });
    alpha.current = 1; // settle overlaps of the (new) seed layout
    if (fitOnce.current !== nodeSig) {
      fitOnce.current = nodeSig;
      fit(); // bring the (new) layout into view — no scrolling to find it
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodeSig]);

  // force-directed simulation: EVERY visible pair repels (inverse-square),
  // connected nodes attract along their edges, and it runs until the layout
  // settles (alpha cooling). A faint x-only column gravity keeps the
  // WHY → WHEN axis reading left-to-right; dragging re-heats the system.
  const wake = () => {
    alpha.current = Math.max(alpha.current, drag.current ? 0.55 : 0.35);
    if (!raf.current) setFrame((f) => f + 1); // the effect below restarts the loop
  };
  useEffect(() => { wake(); }, [visible.size]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!g) return;
    if (raf.current || (alpha.current <= 0.02 && !drag.current)) return;
    const H = g.H;
    const tick = () => {
      const a = alpha.current;
      const bs = [...bodies.current.values()].filter((b) => visible.has(b.id));
      // repulsion — unconnected nodes push away from each other; the
      // rectangle-aware shove on top keeps overlapping boxes separating
      for (let i = 0; i < bs.length; i++) {
        for (let j = i + 1; j < bs.length; j++) {
          const p = bs[i], q = bs[j];
          const dx = q.x - p.x, dy = q.y - p.y;
          const d = Math.max(40, Math.hypot(dx, dy));
          const f = (26000 / (d * d)) * a;
          const ux = d > 0 ? dx / d : (j % 2 ? 1 : -1), uy = d > 0 ? dy / d : (i % 2 ? 1 : -1);
          p.vx -= ux * f; p.vy -= uy * f; q.vx += ux * f; q.vy += uy * f;
          const minX = (p.w + q.w) / 2 + 24, minY = (p.h + q.h) / 2 + 14;
          if (Math.abs(dx) >= minX || Math.abs(dy) >= minY) continue;
          const sy = dy !== 0 ? Math.sign(dy) : (i % 2 ? 1 : -1);
          const py = (minY - Math.abs(dy)) * 0.5;
          p.vy -= sy * py * 0.16 * a; q.vy += sy * py * 0.16 * a;
        }
      }
      // attraction — connected nodes pull toward a rest distance along the
      // edge vector, so chains contract while the repulsion spreads the rest
      g.edges.forEach((e) => {
        if (!visible.has(e.a) || !visible.has(e.b)) return;
        const p = bodies.current.get(e.a), q = bodies.current.get(e.b);
        if (!p || !q) return;
        const dx = q.x - p.x, dy = q.y - p.y;
        const dist = Math.max(1, Math.hypot(dx, dy));
        const f = Math.max(0, dist - 180) * 0.03 * a;
        const fx = (dx / dist) * f, fy = (dy / dist) * f;
        p.vx += fx; p.vy += fy; q.vx -= fx; q.vy -= fy;
      });
      // faint x-only gravity toward the column keeps the axis readable;
      // user-placed (pinned) nodes anchor hard in both axes so pulling an
      // end unravels the rest instead of snapping back
      bs.forEach((b) => {
        if (drag.current?.id === b.id) { b.vx = 0; b.vy = 0; return; }
        const kx = b.pinned ? 0.3 : 0.008, ky = b.pinned ? 0.3 : 0;
        b.vx += (b.hx - b.x) * kx * a;
        b.vy += (b.hy - b.y) * ky * a;
        // weak vertical centering stops the whole graph drifting off-canvas
        b.vy += (H / 2 - b.y) * 0.002 * a;
        b.vx *= 0.72; b.vy *= 0.72;
        // generous bounds — the canvas grows to fit the arrangement
        b.x = Math.min(2300, Math.max(-40, b.x + b.vx));
        b.y = Math.min(2300, Math.max(26, b.y + b.vy));
      });
      // hard de-overlap (position-based, NOT alpha-scaled): the soft forces
      // spread things out, this guarantees no two boxes end up stacked —
      // home springs die with alpha, so the corrected positions persist
      for (let pass = 0; pass < 2; pass++) {
        for (let i = 0; i < bs.length; i++) {
          for (let j = i + 1; j < bs.length; j++) {
            const p = bs[i], q = bs[j];
            const dx = q.x - p.x, dy = q.y - p.y;
            const minX = (p.w + q.w) / 2 + 12, minY = (p.h + q.h) / 2 + 8;
            const ox = minX - Math.abs(dx), oy = minY - Math.abs(dy);
            if (ox <= 0 || oy <= 0) continue;
            const pFree = drag.current?.id === p.id ? 0 : 1;
            const qFree = drag.current?.id === q.id ? 0 : 1;
            const tot = pFree + qFree || 1;
            if (oy / minY <= ox / minX) {
              const s = (dy !== 0 ? Math.sign(dy) : (i % 2 ? 1 : -1)) * oy * 0.85;
              p.y -= s * (pFree / tot); q.y += s * (qFree / tot);
              p.y = Math.min(2300, Math.max(26, p.y)); q.y = Math.min(2300, Math.max(26, q.y));
            } else {
              const s = (dx !== 0 ? Math.sign(dx) : 1) * ox * 0.85;
              p.x -= s * (pFree / tot); q.x += s * (qFree / tot);
            }
          }
        }
      }
      if (!drag.current) alpha.current *= 0.985;
      setFrame((f) => f + 1);
      if (alpha.current > 0.02 || drag.current) raf.current = requestAnimationFrame(tick);
      else raf.current = 0;
    };
    raf.current = requestAnimationFrame(tick);
    return () => { cancelAnimationFrame(raf.current); raf.current = 0; };
  });

  if (!app.model || !g) return <Loading />;

  const canvasPoint = (e: React.PointerEvent) => {
    const r = canvas.current!.getBoundingClientRect();
    return { x: (e.clientX - r.left) / zoom, y: (e.clientY - r.top) / zoom };
  };

  // background drag pans the viewport; node drags are handled by the nodes
  // themselves (marked data-node), so they never start a pan
  const startPan = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest('[data-node]')) return;
    e.preventDefault();
    const sx0 = e.clientX, sy0 = e.clientY;
    const p0 = panRef.current;
    const move = (ev: PointerEvent) => setPan({ x: p0.x + (ev.clientX - sx0), y: p0.y + (ev.clientY - sy0) });
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };

  return (
    <div style={sx('flex:1;min-height:0;display:flex;flex-direction:column')}>
      {docTabsStrip('graph', focusName || 'Editor', nav, undefined, focusPath || undefined)}
      <div ref={scroller} onPointerDown={startPan} style={sx('flex:1;min-height:0;position:relative;overflow:hidden;background:radial-gradient(circle,var(--border) 1px,transparent 1px);background-size:22px 22px;cursor:grab')}>
        <div style={sx('position:absolute;left:16px;top:14px;z-index:3;display:flex;flex-direction:column;gap:6px;padding:6px;background:var(--surface);border:1px solid var(--border);border-radius:10px;box-shadow:var(--shadow-lg);max-width:calc(100% - 32px)')}>
          <div style={sx('display:flex;align-items:center;gap:6px;flex-wrap:wrap')}>
            <span style={sx('font-size:10.5px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;padding:0 6px')}>Layers</span>
            {g.stats.map((s) => {
              const off = layersOff.has(s.key);
              return (
                <span
                  key={s.key}
                  onClick={() => setLayersOff((prev) => {
                    const next = new Set(prev);
                    if (next.has(s.key)) next.delete(s.key); else next.add(s.key);
                    return next;
                  })}
                  title={(off ? 'show ' : 'hide ') + s.label}
                  style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;font-size:11.5px;font-weight:600;cursor:pointer;user-select:none;' +
                    (off ? 'background:var(--surface-2);color:var(--text-3);opacity:.55' : `background:${s.bg};color:${s.fg}`))}
                >
                  {off ? '○' : '◉'} {s.label}
                </span>
              );
            })}
            <span style={sx('width:1px;height:18px;background:var(--border);margin:0 2px')} />
            <span onClick={app.toggleAI} style={sx('display:inline-flex;align-items:center;gap:6px;padding:4px 9px;border-radius:6px;background:var(--ai-bg);color:var(--ai);font-size:11.5px;font-weight:600;cursor:pointer')}>
              <span style={sx(`width:22px;height:13px;border-radius:8px;background:${app.aiSuggestions ? 'var(--ai)' : 'var(--border-2)'};position:relative;display:inline-block`)}>
                <span style={sx(`position:absolute;${app.aiSuggestions ? 'right' : 'left'}:1px;top:1px;width:11px;height:11px;border-radius:50%;background:#fff`)} />
              </span>
              AI suggestions
            </span>
          </div>
          <div style={sx('display:flex;align-items:center;gap:6px')}>
            <input
              value={nodeFilter}
              onChange={(e) => setNodeFilter(e.target.value)}
              placeholder="filter nodes…"
              aria-label="filter graph nodes"
              onKeyDown={(e) => { if (e.key === 'Escape') setNodeFilter(''); }}
              style={sx('flex:1;min-width:140px;height:26px;padding:0 9px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:11.5px;outline:none')}
            />
            {focusPath && (
              <span
                onClick={() => nav('/graph/' + focusPath + (full ? '' : '?full=1'))}
                title={full ? 'focus on ' + focusName + "'s chain" : 'show the full graph'}
                style={sx('flex:none;display:flex;align-items:center;gap:6px;padding:4px 10px;border:1px solid var(--border);border-radius:7px;font-size:12px;font-weight:600;cursor:pointer;user-select:none;' + (full ? 'color:var(--text-3)' : 'color:var(--prod);background:var(--prod-bg)'))}
              >
                ◎ {focusName}
              </span>
            )}
          </div>
        </div>

        <div ref={canvas} style={{ ...sx('position:absolute;left:0;top:0;transform-origin:0 0'), width: 0, height: 0, transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }}>
            <svg style={{ position: 'absolute', left: 0, top: 0, width: 1, height: 1, overflow: 'visible' }}>
              {g.edges.filter((e) => visible.has(e.a) && visible.has(e.b)).map((e, i) => {
                const p = bodies.current.get(e.a), q = bodies.current.get(e.b);
                if (!p || !q) return null;
                // anchor on the facing box edges, whichever way round they sit
                const flip = q.x < p.x;
                const x1 = p.x + (flip ? -p.w / 2 : p.w / 2), x2 = q.x + (flip ? q.w / 2 : -q.w / 2);
                const hot = !!hover && (e.a === hover || e.b === hover);
                return (
                  <path key={i} d={edgeCurve(x1, p.y, x2, q.y, e.a + '>' + e.b)} fill="none" stroke={e.stroke}
                    strokeWidth={hot ? 2.6 : 1.8}
                    strokeDasharray={e.dash ? '5 4' : undefined}
                    opacity={hover ? (hot ? 1 : 0.12) : 0.9}
                    style={{ transition: 'opacity .12s' }} />
                );
              })}
            </svg>
            {g.nodes.filter((n) => visible.has(n.id)).map((n) => {
              const b = bodies.current.get(n.id);
              if (!b) return null;
              const active = hover === n.id || drag.current?.id === n.id;
              return (
                <div
                  key={n.id}
                  data-node=""
                  title={n.go ? 'open ' + n.go : undefined}
                  onMouseEnter={() => setHover(n.id)}
                  onMouseLeave={() => setHover(null)}
                  onPointerDown={(e) => {
                    e.preventDefault();
                    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
                    const pt = canvasPoint(e);
                    drag.current = { id: n.id, offX: pt.x - b.x, offY: pt.y - b.y, moved: false };
                    wake();
                  }}
                  onPointerMove={(e) => {
                    if (drag.current?.id !== n.id) return;
                    const pt = canvasPoint(e);
                    const nx = pt.x - drag.current.offX, ny = pt.y - drag.current.offY;
                    if (Math.abs(nx - b.x) + Math.abs(ny - b.y) > 2) drag.current.moved = true;
                    b.x = nx; b.y = ny;
                    wake();
                  }}
                  onPointerUp={() => {
                    if (drag.current?.id !== n.id) return;
                    const wasDrag = drag.current.moved;
                    // dropping re-homes AND pins the node — the graph stays
                    // unraveled and springs tow the rest instead
                    b.hx = b.x; b.hy = b.y;
                    if (wasDrag) b.pinned = true;
                    drag.current = null;
                    wake();
                    if (!wasDrag && n.go) nav('/editor/' + n.go);
                  }}
                  style={{
                    ...sx(n.boxStyle),
                    left: b.x - n.w / 2,
                    top: b.y - NODE_H / 2 + 3,
                    opacity: linked && !linked.has(n.id) ? 0.3 : 1,
                    cursor: drag.current?.id === n.id ? 'grabbing' : n.go ? 'pointer' : 'grab',
                    zIndex: active ? 7 : 1, // hovered/dragged nodes surface above their neighbours
                    transition: drag.current?.id === n.id ? 'none' : 'opacity .12s, box-shadow .12s',
                    boxShadow: active ? '0 0 0 2px var(--prod-line), var(--shadow-lg)' : undefined,
                    touchAction: 'none',
                    userSelect: 'none',
                  }}
                >
                  <div style={sx(n.labelStyle)}>{n.label}</div>
                  <div style={sx(n.subStyle)}>{n.sub}</div>
                </div>
              );
            })}
        </div>

        <div style={sx('position:absolute;right:16px;top:14px;z-index:3;width:210px;background:var(--surface);border:1px solid var(--border);border-radius:11px;box-shadow:var(--shadow-lg);overflow:hidden')}>
          <div style={sx("padding:10px 14px;border-bottom:1px solid var(--border);background:var(--surface-2);font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Lineage · from links</div>
          <div style={sx('padding:11px 14px;display:flex;flex-direction:column;gap:9px;font-size:12.5px')}>
            {g.stats.map((s) => (
              <div key={s.key} style={sx('display:flex;justify-content:space-between;align-items:center')}>
                <span style={sx('color:var(--text-2)')}>{s.label}</span><b>{s.count}</b>
              </div>
            ))}
          </div>
        </div>

        <div style={sx('position:absolute;left:16px;bottom:14px;z-index:3;display:flex;align-items:center;gap:14px;padding:7px 12px;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow);font-size:11px;color:var(--text-2)')}>
          <span style={sx('display:flex;align-items:center;gap:6px')}>
            <span style={sx('width:16px;height:2px;background:var(--text-2)')} />lineage · computed from frontmatter links — drag nodes to untangle
          </span>
        </div>
        <div style={sx('position:absolute;right:16px;bottom:14px;z-index:3;display:flex;align-items:center;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow);overflow:hidden')}>
          <span onClick={() => zoomBy(1 / 1.25)}
            style={sx('width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-right:1px solid var(--border);user-select:none')}>−</span>
          <span onClick={fit} title="fit the graph into view (ctrl+scroll zooms, scroll/drag pans)"
            style={sx("padding:0 10px;font-family:'JetBrains Mono',monospace;font-size:11px;cursor:pointer;user-select:none")}>{Math.round(zoom * 100)}%</span>
          <span onClick={() => zoomBy(1.25)}
            style={sx('width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-left:1px solid var(--border);user-select:none')}>+</span>
        </div>
      </div>
    </div>
  );
}
