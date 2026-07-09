import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildMatrix } from '../lib/derive';
import { Loading } from './Dashboard';

export function MatrixView() {
  const nav = useNavigate();
  const app = useApp();
  if (!app.model) return <Loading />;
  const mx = buildMatrix(app.model);
  const seg = (on: boolean) => (on ? 'background:var(--text);color:var(--surface)' : 'color:var(--text-2);cursor:pointer');

  return (
    <div style={sx('flex:1;min-height:0;display:flex;flex-direction:column;background:var(--bg)')}>
      <div style={sx('flex:none;display:flex;align-items:center;gap:12px;padding:14px 20px;background:var(--surface);border-bottom:1px solid var(--border)')}>
        <div>
          <div style={sx('font-weight:700;font-size:15px')}>Traceability matrix</div>
          <div style={sx('font-size:11px;color:var(--text-3);margin-top:1px')}>Requirements × artifacts · coverage</div>
        </div>
        <div style={sx('flex:1')} />
        <div style={sx('display:flex;background:var(--surface-2);border:1px solid var(--border);border-radius:9px;padding:3px')}>
          <span onClick={() => nav('/graph')} style={sx('padding:5px 14px;border-radius:6px;font-size:12px;font-weight:600;' + seg(false))}>Graph</span>
          <span style={sx('padding:5px 14px;border-radius:6px;font-size:12px;font-weight:600;' + seg(true))}>Matrix</span>
        </div>
      </div>
      <div style={sx('flex:1;overflow:auto')}>
        <div style={sx('display:inline-block;min-width:100%;font-size:12px')}>
          <div style={sx('position:sticky;top:0;z-index:4')}>
            <div style={sx('display:flex;background:var(--surface);border-bottom:1px solid var(--border)')}>
              <div style={sx('position:sticky;left:0;z-index:5;width:210px;flex:none;background:var(--surface);border-right:1px solid var(--border)')} />
              {mx.mgroups.map((g) => (
                <div key={g.label} style={{ ...sx('flex:none;padding:9px 10px;border-right:1px solid var(--border);font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:.4px;white-space:nowrap;overflow:hidden'), width: g.width, color: g.color }}>
                  {g.label}
                </div>
              ))}
              <div style={sx('width:120px;flex:none;background:var(--surface);border-left:1px solid var(--border)')} />
            </div>
            <div style={sx('display:flex;background:var(--surface);border-bottom:1px solid var(--border)')}>
              <div style={sx("position:sticky;left:0;z-index:5;width:210px;flex:none;background:var(--surface);border-right:1px solid var(--border);display:flex;align-items:flex-end;padding:0 14px 9px;font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Requirement</div>
              {mx.mcolumns.map((c) => (
                <div key={c.kind + c.ref} style={sx('width:26px;flex:none;height:120px;border-right:1px solid var(--border);display:flex;align-items:flex-end;justify-content:center;padding-bottom:9px;overflow:hidden')}>
                  <span style={{ ...sx("font-family:'JetBrains Mono',monospace;font-size:10px;white-space:nowrap;color:var(--text-2)"), writingMode: 'vertical-rl', transform: 'rotate(180deg)' }}>{c.label}</span>
                </div>
              ))}
              <div style={sx("width:120px;flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;align-items:flex-end;justify-content:flex-end;padding:0 14px 9px;font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Coverage</div>
            </div>
          </div>
          {mx.mrows.map((r) => (
            <div key={r.id} className="mrow" style={sx('display:flex;border-bottom:1px solid var(--border)')}>
              <div style={sx('position:sticky;left:0;z-index:3;width:210px;flex:none;background:var(--surface);border-right:1px solid var(--border);padding:8px 14px')}>
                <div style={sx('font-size:12.5px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis')}>{r.name}</div>
                <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>{r.id}</div>
              </div>
              {r.cells.map((cell, i) => (
                <div key={i} style={sx('width:26px;flex:none;height:46px;border-right:1px solid var(--border);display:flex;align-items:center;justify-content:center')}>
                  <span style={sx(cell.sq)} />
                </div>
              ))}
              <div style={sx('width:120px;flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;align-items:center;gap:8px;padding:0 14px')}>
                <div style={sx('flex:1;height:5px;border-radius:3px;background:var(--surface-2);overflow:hidden')}><div style={sx(r.covStyle)} /></div>
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;width:28px;text-align:right")}>{r.cov}%</span>
              </div>
            </div>
          ))}
        </div>
      </div>
      <div style={sx('flex:none;display:flex;align-items:center;flex-wrap:wrap;gap:16px;padding:11px 20px;border-top:1px solid var(--border);background:var(--surface);font-size:11px;color:var(--text-2)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;color:var(--text-3)")}>{mx.caption}</span>
        <div style={sx('flex:1')} />
        <span style={sx('display:inline-flex;align-items:center;gap:6px')}><span style={sx('width:13px;height:13px;border-radius:4px;background:var(--data)')} />linked</span>
        <span style={sx('display:inline-flex;align-items:center;gap:6px')}><span style={sx('width:13px;height:13px;border-radius:4px;background:var(--reg)')} />drift / stale</span>
        <span style={sx('display:inline-flex;align-items:center;gap:6px')}><span style={sx('width:13px;height:13px;border-radius:4px;border:1.5px dashed var(--prod);box-sizing:border-box')} />planned</span>
        <span style={sx('display:inline-flex;align-items:center;gap:6px')}><span style={sx('width:5px;height:5px;border-radius:50%;background:var(--border-2)')} />no link</span>
        <span style={sx('margin-left:4px;color:var(--text-3)')}>Driver: <span style={sx('color:var(--reg)')}>⚖</span> reg · <span style={sx('color:var(--prod)')}>◆</span> product · <span style={sx('color:var(--text-2)')}>⚙</span> tech</span>
      </div>
    </div>
  );
}
