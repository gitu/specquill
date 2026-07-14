import { useMemo, useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildGraph } from '../lib/derive';
import { Loading } from './Dashboard';
import { docTabsStrip } from './EditorView';

const ZOOMS = [0.5, 0.65, 0.8, 1, 1.2, 1.45, 1.75];

export function GraphView() {
  const nav = useNav();
  const app = useApp();
  const [hover, setHover] = useState<string | null>(null);
  const [zi, setZi] = useState(3); // index into ZOOMS, 1 = 100%
  const g = useMemo(() => (app.model ? buildGraph(app.model) : null), [app.model]);
  // the hovered node's lineage: itself plus every node it shares an edge with
  const linked = useMemo(() => {
    if (!hover || !g) return null;
    const s = new Set([hover]);
    g.edges.forEach((e) => { if (e.a === hover) s.add(e.b); if (e.b === hover) s.add(e.a); });
    return s;
  }, [hover, g]);
  if (!app.model || !g) return <Loading />;
  const zoom = ZOOMS[zi];
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
          <div style={{ ...sx('position:relative;width:900px;min-width:900px;transform-origin:0 0'), height: g.H, marginTop: 70, transform: `scale(${zoom})` }}>
            <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', overflow: 'visible' }}>
              {g.edges.map((e, i) => {
                const hot = !!hover && (e.a === hover || e.b === hover);
                return (
                  <path key={i} d={e.d} fill="none" stroke={e.stroke}
                    strokeWidth={hot ? 2.6 : 1.8}
                    strokeDasharray={e.dash ? '5 4' : undefined}
                    opacity={hover ? (hot ? 1 : 0.12) : 0.9}
                    style={{ transition: 'opacity .12s' }} />
                );
              })}
            </svg>
            {g.nodes.map((n) => (
              <div
                key={n.id}
                title={n.go ? 'open ' + n.go : undefined}
                onMouseEnter={() => setHover(n.id)}
                onMouseLeave={() => setHover(null)}
                onClick={n.go ? () => nav('/editor/' + n.go) : undefined}
                style={{
                  ...sx(n.boxStyle),
                  opacity: linked && !linked.has(n.id) ? 0.3 : 1,
                  cursor: n.go ? 'pointer' : 'default',
                  transition: 'opacity .12s, box-shadow .12s',
                  boxShadow: hover === n.id ? '0 0 0 2px var(--prod-line), var(--shadow-lg)' : undefined,
                }}
              >
                <div style={sx(n.labelStyle)}>{n.label}</div>
                <div style={sx(n.subStyle)}>{n.sub}</div>
              </div>
            ))}
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
            <span style={sx('width:16px;height:2px;background:var(--text-2)')} />lineage · computed from frontmatter links
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
