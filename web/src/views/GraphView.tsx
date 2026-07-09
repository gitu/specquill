import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildGraph } from '../lib/derive';
import { Loading } from './Dashboard';
import { docTabsStrip } from './EditorView';

export function GraphView() {
  const nav = useNavigate();
  const app = useApp();
  if (!app.model) return <Loading />;
  const g = buildGraph(app.model);
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

        <div style={{ ...sx('position:relative;width:900px;margin:70px auto 40px;min-width:900px'), height: g.H }}>
          <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', overflow: 'visible' }}>
            {g.edges.map((e, i) => <path key={i} d={e.d} fill="none" stroke={e.stroke} strokeWidth="1.8" strokeDasharray={e.dash ? '5 4' : undefined} />)}
          </svg>
          {g.nodes.map((n) => (
            <div key={n.id} style={sx(n.boxStyle)}>
              <div style={sx(n.labelStyle)}>{n.label}</div>
              <div style={sx(n.subStyle)}>{n.sub}</div>
            </div>
          ))}
        </div>

        <div style={sx('position:absolute;right:16px;top:14px;z-index:3;width:210px;background:var(--surface);border:1px solid var(--border);border-radius:11px;box-shadow:var(--shadow-lg);overflow:hidden')}>
          <div style={sx("padding:10px 14px;border-bottom:1px solid var(--border);background:var(--surface-2);font-family:'IBM Plex Mono',monospace;font-size:9.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Lineage · from links</div>
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
          <span style={sx('width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-right:1px solid var(--border)')}>−</span>
          <span style={sx("padding:0 10px;font-family:'IBM Plex Mono',monospace;font-size:11px")}>100%</span>
          <span style={sx('width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-left:1px solid var(--border)')}>+</span>
        </div>
      </div>
    </div>
  );
}
