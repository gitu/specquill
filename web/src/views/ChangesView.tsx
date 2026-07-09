import { useNavigate, useSearchParams } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildChanges, reqByName, srcMeta, statusMeta } from '../lib/derive';
import { Loading } from './Dashboard';
import { IconSpark } from '../components/icons';

export function ChangesView() {
  const nav = useNavigate();
  const app = useApp();
  const [params, setParams] = useSearchParams();
  if (!app.model) return <Loading />;

  const filter = params.get('filter') || 'all';
  const { items, sel } = buildChanges(app.model, filter, params.get('sel'));
  const fSeg = (on: boolean) => 'flex:1;text-align:center;padding:4px 0;border-radius:6px;font-size:11.5px;cursor:pointer;' + (on ? 'font-weight:600;background:var(--surface);box-shadow:var(--shadow)' : 'color:var(--text-3)');
  const setFilter = (f: string) => setParams({ filter: f });
  const select = (path: string) => setParams({ filter, sel: path });

  const selMeta = sel ? srcMeta(sel.source) : null;
  const impacts = sel ? [
    ...sel.impReqs.map((r) => ({
      key: 'r' + r, badge: r, label: reqByName(app.model!, r)?.title || r, tag: 'needs update', tagColor: 'var(--reg)',
      style: 'display:flex;align-items:center;gap:10px;padding:11px 14px;border:1px solid var(--border);border-radius:9px',
      open: () => { const req = reqByName(app.model!, r); if (req) nav('/editor/' + req.path); },
    })),
    ...sel.impSpecs.map((s) => ({
      key: 's' + s, badge: '◈', label: s.split('/').pop()!, tag: 'spec', tagColor: 'var(--text-3)',
      style: 'display:flex;align-items:center;gap:10px;padding:11px 14px;border:1px solid var(--border);border-radius:9px',
      open: () => nav('/editor/' + s),
    })),
    ...sel.impMaps.map((mp) => {
      const drift = /executionTimestamp|drift/.test(mp);
      return {
        key: 'm' + mp, badge: '⇄', label: mp.split('#')[1] || mp.split('/').pop()!, tag: drift ? '⚠ drift' : 'mapping', tagColor: drift ? 'var(--reg)' : 'var(--text-3)',
        style: 'display:flex;align-items:center;gap:10px;padding:11px 14px;border-radius:9px;' + (drift ? 'border:1px solid var(--reg-line);background:var(--reg-bg)' : 'border:1px solid var(--border)'),
        open: () => nav('/editor/' + mp.split('#')[0]),
      };
    }),
  ] : [];

  return (
    <div style={sx('flex:1;min-height:0;display:flex;background:var(--bg)')}>
      <div style={sx('width:328px;flex:none;border-right:1px solid var(--border);background:var(--panel);display:flex;flex-direction:column')}>
        <div style={sx('padding:12px 14px 10px;border-bottom:1px solid var(--border)')}>
          <div style={sx('display:flex;align-items:center;gap:8px')}>
            <span style={sx('font-weight:700;font-size:14px')}>Changes</span>
            <span style={sx('font-size:11px;color:var(--text-3)')}>{items.length} open</span>
          </div>
          <div style={sx('display:flex;gap:4px;margin-top:10px;background:var(--surface-2);border:1px solid var(--border);border-radius:8px;padding:3px')}>
            <span onClick={() => setFilter('all')} style={sx(fSeg(filter === 'all'))}>All</span>
            <span onClick={() => setFilter('regulatory')} style={sx(fSeg(filter === 'regulatory'))}>Reg</span>
            <span onClick={() => setFilter('product')} style={sx(fSeg(filter === 'product'))}>Product</span>
            <span onClick={() => setFilter('technical')} style={sx(fSeg(filter === 'technical'))}>Tech</span>
          </div>
        </div>
        <div style={sx('flex:1;overflow-y:auto')}>
          {items.map((c) => {
            const m = srcMeta(c.source), st = statusMeta(c.status);
            const active = sel && sel.path === c.path;
            return (
              <div key={c.path} onClick={() => select(c.path)}
                style={sx('padding:13px 14px;border-bottom:1px solid var(--border);cursor:pointer;' + (active ? `border-left:3px solid ${m.fg};background:var(--surface)` : 'border-left:3px solid transparent'))}>
                <div style={sx('display:flex;align-items:center;gap:7px')}>
                  <span style={sx(`display:inline-flex;align-items:center;gap:3px;padding:2px 7px;border-radius:5px;font-size:10px;font-weight:600;background:${m.bg};color:${m.fg}`)}>{m.icon} {m.label}</span>
                  <div style={sx('flex:1')} />
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>{c.ago}</span>
                </div>
                <div style={sx('font-weight:600;font-size:12.5px;margin-top:7px')}>{c.title}</div>
                <div style={sx('font-size:11.5px;color:var(--text-2);margin-top:3px;line-height:1.45;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden')}>{c.summary}</div>
                <div style={sx('display:flex;align-items:center;gap:6px;margin-top:8px')}>
                  <span style={sx(`width:6px;height:6px;border-radius:50%;background:${st.color}`)} />
                  <span style={sx('font-size:10.5px;color:var(--text-2)')}>{st.label} · {c.nImpacted} impacted</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>
      <div style={sx('flex:1;min-width:0;overflow-y:auto;background:var(--surface)')}>
        {sel && selMeta && (
          <div style={sx('max-width:680px;margin:0 auto;padding:26px 30px 60px')}>
            <div style={sx('display:flex;align-items:center;gap:9px;flex-wrap:wrap')}>
              <span style={sx(`display:inline-flex;align-items:center;gap:4px;padding:3px 9px;border-radius:6px;font-size:11px;font-weight:600;background:${selMeta.bg};color:${selMeta.fg}`)}>{selMeta.icon} {selMeta.label}</span>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>{sel.name}</span>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>· {sel.published}</span>
            </div>
            <h1 style={sx('margin:14px 0 0;font-size:23px;font-weight:700;letter-spacing:-.4px')}>{sel.title}</h1>
            <div style={sx('margin-top:16px;border:1px solid var(--ai-line);border-radius:11px;overflow:hidden')}>
              <div style={sx('display:flex;align-items:center;gap:8px;padding:9px 14px;background:var(--ai-bg)')}>
                <IconSpark size={13} stroke="var(--ai)" width={1.9} />
                <span style={sx('font-size:12px;font-weight:600;color:var(--ai)')}>Copilot summary</span>
              </div>
              <div style={sx('padding:12px 14px;font-size:13px;line-height:1.65;color:var(--text)')}>{sel.summary}</div>
            </div>
            <h2 style={sx('margin:24px 0 10px;font-size:14px;font-weight:700;color:var(--text-2)')}>Impacted artifacts</h2>
            <div style={sx('display:flex;flex-direction:column;gap:8px')}>
              {impacts.map((a) => (
                <div key={a.key} onClick={a.open} style={{ ...sx(a.style), cursor: 'pointer' }}>
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--prod);background:var(--prod-bg);padding:2px 7px;border-radius:5px")}>{a.badge}</span>
                  <span style={sx('font-size:12.5px;font-weight:500')}>{a.label}</span>
                  <div style={sx('flex:1')} />
                  <span style={{ ...sx('font-size:11px;font-weight:600'), color: a.tagColor }}>{a.tag}</span>
                </div>
              ))}
            </div>
            <div style={sx('display:flex;gap:8px;margin-top:22px;flex-wrap:wrap')}>
              {sel.diff && (
                <button onClick={() => nav('/diff?change=' + encodeURIComponent(sel.path))} style={sx('height:34px;padding:0 15px;border:none;border-radius:8px;background:var(--ai);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
                  ✦ Draft edits &amp; open as diff
                </button>
              )}
              <button onClick={() => nav('/graph')} style={sx('height:34px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
                Open impact graph
              </button>
              <button style={sx('height:34px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Assign</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
