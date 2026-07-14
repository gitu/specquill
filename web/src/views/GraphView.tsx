import { useEffect, useMemo, useRef, useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildGraph, edgeCurve } from '../lib/derive';
import { Loading } from './Dashboard';
import { docTabsStrip } from './EditorView';

const ZOOMS = [0.5, 0.65, 0.8, 1, 1.2, 1.45, 1.75];
const NODE_H = 46; // approximate box height for collision purposes

// a node's live physics state (positions are box centers)
interface Body {
  id: string; x: number; y: number; vx: number; vy: number;
  w: number; h: number;
  hx: number; hy: number; // "home" anchor — dragging re-homes the node
}

export function GraphView() {
  const nav = useNav();
  const app = useApp();
  const [hover, setHover] = useState<string | null>(null);
  const [zi, setZi] = useState(3); // index into ZOOMS, 3 = 100%
  const [, setFrame] = useState(0); // bumped per simulation tick
  const g = useMemo(() => (app.model ? buildGraph(app.model) : null), [app.model]);
  const bodies = useRef<Map<string, Body>>(new Map());
  const alpha = useRef(0);
  const raf = useRef(0);
  const drag = useRef<{ id: string; offX: number; offY: number; moved: boolean } | null>(null);
  const canvas = useRef<HTMLDivElement>(null);
  const zoom = ZOOMS[zi];

  // the hovered node's lineage: itself plus every node it shares an edge with
  const linked = useMemo(() => {
    if (!hover || !g) return null;
    const s = new Set([hover]);
    g.edges.forEach((e) => { if (e.a === hover) s.add(e.b); if (e.b === hover) s.add(e.a); });
    return s;
  }, [hover, g]);

  // (re)seed bodies from the deterministic layout whenever the model changes
  useEffect(() => {
    if (!g) return;
    const m = new Map<string, Body>();
    g.nodes.forEach((n) => {
      const x = n.x + n.w / 2, y = n.y;
      m.set(n.id, { id: n.id, x, y, vx: 0, vy: 0, w: n.w, h: NODE_H, hx: x, hy: y });
    });
    bodies.current = m;
    alpha.current = 1; // settle overlaps of the seed layout once
  }, [g]);

  // force simulation: weak home springs keep the lineage columns readable,
  // edges pull linked nodes toward the same height, and boxes push each
  // other apart (rectangle-aware). Dragging re-heats it; it cools to rest.
  const wake = () => {
    alpha.current = Math.max(alpha.current, 0.35);
    if (!raf.current) setFrame((f) => f + 1); // the effect below restarts the loop
  };
  useEffect(() => {
    if (!g) return;
    if (raf.current || (alpha.current <= 0.02 && !drag.current)) return;
    const H = g.H;
    const tick = () => {
      const a = alpha.current;
      const bs = [...bodies.current.values()];
      // pairwise repulsion — boxes intruding on each other's padding push off
      for (let i = 0; i < bs.length; i++) {
        for (let j = i + 1; j < bs.length; j++) {
          const p = bs[i], q = bs[j];
          const dx = q.x - p.x, dy = q.y - p.y;
          const minX = (p.w + q.w) / 2 + 24, minY = (p.h + q.h) / 2 + 14;
          if (Math.abs(dx) >= minX || Math.abs(dy) >= minY) continue;
          const sy = dy !== 0 ? Math.sign(dy) : (i % 2 ? 1 : -1);
          const py = (minY - Math.abs(dy)) * 0.5;
          p.vy -= sy * py * 0.16 * a; q.vy += sy * py * 0.16 * a;
          const sxn = dx !== 0 ? Math.sign(dx) : (j % 2 ? 1 : -1);
          const px = (minX - Math.abs(dx)) * 0.5;
          p.vx -= sxn * px * 0.02 * a; q.vx += sxn * px * 0.02 * a;
        }
      }
      // edge springs — gentle vertical alignment of connected nodes
      g.edges.forEach((e) => {
        const p = bodies.current.get(e.a), q = bodies.current.get(e.b);
        if (!p || !q) return;
        const f = (q.y - p.y) * 0.012 * a;
        p.vy += f; q.vy -= f;
      });
      // home anchors + integration
      bs.forEach((b) => {
        if (drag.current?.id === b.id) { b.vx = 0; b.vy = 0; return; }
        b.vx += (b.hx - b.x) * 0.1 * a;
        b.vy += (b.hy - b.y) * 0.02 * a;
        b.vx *= 0.72; b.vy *= 0.72;
        b.x = Math.min(920, Math.max(-20, b.x + b.vx));
        b.y = Math.min(H - 26, Math.max(26, b.y + b.vy));
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
              p.y = Math.min(H - 26, Math.max(26, p.y)); q.y = Math.min(H - 26, Math.max(26, q.y));
            } else {
              const s = (dx !== 0 ? Math.sign(dx) : 1) * ox * 0.85;
              p.x -= s * (pFree / tot); q.x += s * (qFree / tot);
            }
          }
        }
      }
      if (!drag.current) alpha.current *= 0.97;
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

  const seg = (on: boolean) => (on ? 'background:var(--text);color:var(--surface)' : 'color:var(--text-2);cursor:pointer');

  return (
    <div style={sx('flex:1;min-height:0;display:flex;flex-direction:column')}>
      {docTabsStrip('graph', 'txn-report.md', nav)}
      <div style={sx('flex:1;min-height:0;position:relative;overflow:auto;background:radial-gradient(circle,var(--border) 1px,transparent 1px);background-size:22px 22px')}>
        <div style={sx('position:absolute;left:50%;top:14px;transform:translateX(-50%);z-index:4;display:flex;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow-lg);padding:3px')}>
          <span style={sx('padding:5px 15px;border-radius:6px;font-size:12px;font-weight:600;' + seg(true))}>Graph</span>
          <span onClick={() => nav('/matrix')} style={sx('padding:5px 15px;border-radius:6px;font-size:12px;font-weight:600;' + seg(false))}>Matrix</span>
        </div>
        <div style={sx('position:absolute;left:16px;top:14px;z-index:3;display:flex;align-items:center;gap:6px;padding:6px;background:var(--surface);border:1px solid var(--border);border-radius:10px;box-shadow:var(--shadow-lg);flex-wrap:wrap;max-width:calc(100% - 32px)')}>
          <span style={sx('font-size:10.5px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;padding:0 6px')}>Layers</span>
          <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--reg-bg);color:var(--reg);font-size:11.5px;font-weight:600')}>◉ Sources</span>
          <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--prod-bg);color:var(--prod);font-size:11.5px;font-weight:600')}>◉ Requirements</span>
          <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--surface-2);color:var(--text-2);font-size:11.5px;font-weight:600')}>◉ Specs</span>
          <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--data-bg);color:var(--data);font-size:11.5px;font-weight:600')}>◉ Data fields</span>
          <span style={sx('width:1px;height:18px;background:var(--border);margin:0 2px')} />
          <span onClick={app.toggleAI} style={sx('display:inline-flex;align-items:center;gap:6px;padding:4px 9px;border-radius:6px;background:var(--ai-bg);color:var(--ai);font-size:11.5px;font-weight:600;cursor:pointer')}>
            <span style={sx(`width:22px;height:13px;border-radius:8px;background:${app.aiSuggestions ? 'var(--ai)' : 'var(--border-2)'};position:relative;display:inline-block`)}>
              <span style={sx(`position:absolute;${app.aiSuggestions ? 'right' : 'left'}:1px;top:1px;width:11px;height:11px;border-radius:50%;background:#fff`)} />
            </span>
            AI suggestions
          </span>
        </div>

        <div style={{ width: 900 * zoom, height: g.H * zoom + 110, margin: '0 auto' }}>
          <div ref={canvas} style={{ ...sx('position:relative;width:900px;min-width:900px;transform-origin:0 0'), height: g.H, marginTop: 70, transform: `scale(${zoom})` }}>
            <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', overflow: 'visible' }}>
              {g.edges.map((e, i) => {
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
            {g.nodes.map((n) => {
              const b = bodies.current.get(n.id);
              if (!b) return null;
              const active = hover === n.id || drag.current?.id === n.id;
              return (
                <div
                  key={n.id}
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
                    // dropping re-homes the node — the graph stays unraveled
                    b.hx = b.x; b.hy = b.y;
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
        </div>

        <div style={sx('position:absolute;right:16px;top:14px;z-index:3;width:210px;background:var(--surface);border:1px solid var(--border);border-radius:11px;box-shadow:var(--shadow-lg);overflow:hidden')}>
          <div style={sx("padding:10px 14px;border-bottom:1px solid var(--border);background:var(--surface-2);font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Lineage · from links</div>
          <div style={sx('padding:11px 14px;display:flex;flex-direction:column;gap:9px;font-size:12.5px')}>
            {[['Sources', g.stats.s], ['Requirements', g.stats.r], ['Specs', g.stats.sp], ['Data fields', g.stats.f]].map(([label, n]) => (
              <div key={label} style={sx('display:flex;justify-content:space-between;align-items:center')}>
                <span style={sx('color:var(--text-2)')}>{label}</span><b>{n}</b>
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
          <span onClick={() => setZi((i) => Math.max(0, i - 1))}
            style={sx('width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-right:1px solid var(--border);user-select:none')}>−</span>
          <span onClick={() => setZi(3)} title="reset zoom"
            style={sx("padding:0 10px;font-family:'JetBrains Mono',monospace;font-size:11px;cursor:pointer;user-select:none")}>{Math.round(zoom * 100)}%</span>
          <span onClick={() => setZi((i) => Math.min(ZOOMS.length - 1, i + 1))}
            style={sx('width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-left:1px solid var(--border);user-select:none')}>+</span>
        </div>
      </div>
    </div>
  );
}
