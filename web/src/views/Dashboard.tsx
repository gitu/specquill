import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { buildDashboard, srcMeta } from '../lib/derive';

export function Dashboard() {
  const nav = useNavigate();
  const app = useApp();
  if (!app.model) return <Loading />;
  const d = buildDashboard(app.model);
  const covColor = d.cov > 80 ? 'var(--data)' : d.cov > 60 ? 'var(--prod)' : 'var(--reg)';

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1020px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx('display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap')}>
          <div>
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
            <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Overview</h1>
          </div>
          <div style={sx('display:flex;gap:8px')}>
            <button style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>+ New requirement</button>
            <button onClick={() => nav('/changes')} style={sx('height:32px;padding:0 13px;border:none;border-radius:8px;background:var(--text);color:var(--bg);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
              Review changes · {d.openCount}
            </button>
          </div>
        </div>

        <div style={sx('display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-top:22px')}>
          <Kpi label="Open changes" value={String(d.openCount)} sub={`${d.bySource.regulatory} regulatory · ${d.bySource.product} product · ${d.bySource.technical} tech`} />
          <Kpi label="Requirements" value={String(d.reqCount)} sub={`${d.specCount} specs linked`} />
          <Kpi label="Mapping drifts" value={String(d.drifts)} sub="need re-validation" valueStyle="color:var(--reg)" />
          <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)')}>
            <div style={sx('font-size:11.5px;color:var(--text-2)')}>Trace coverage</div>
            <div style={sx('display:flex;align-items:baseline;gap:8px;margin-top:8px')}>
              <span style={sx('font-size:27px;font-weight:700;letter-spacing:-.5px')}>{d.cov}<span style={sx('font-size:15px')}>%</span></span>
            </div>
            <div style={sx('height:5px;border-radius:3px;background:var(--surface-2);margin-top:8px;overflow:hidden')}>
              <div style={sx(`width:${d.cov}%;height:100%;background:${covColor}`)} />
            </div>
          </div>
        </div>

        <div style={sx('display:grid;grid-template-columns:1.65fr 1fr;gap:18px;margin-top:20px;align-items:start')}>
          <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden')}>
            <div style={sx('display:flex;align-items:center;gap:8px;padding:13px 16px;border-bottom:1px solid var(--border)')}>
              <span style={sx('font-weight:700;font-size:13.5px')}>Requirement changes</span>
              <span style={sx('font-size:11px;color:var(--text-3)')}>— all sources</span>
              <div style={sx('flex:1')} />
              <span onClick={() => nav('/changes')} style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>Open inbox →</span>
            </div>
            {d.feed.map((c) => {
              const m = srcMeta(c.source);
              return (
                <div key={c.path} onClick={() => nav('/changes?sel=' + encodeURIComponent(c.path))} style={sx('display:flex;gap:12px;padding:14px 16px;border-bottom:1px solid var(--border);cursor:pointer')}>
                  <span style={sx(`flex:none;align-self:flex-start;display:inline-flex;align-items:center;gap:4px;padding:3px 8px;border-radius:6px;font-size:10.5px;font-weight:600;background:${m.bg};color:${m.fg}`)}>
                    {m.icon} {m.label}
                  </span>
                  <div style={sx('flex:1;min-width:0')}>
                    <div style={sx('display:flex;align-items:baseline;gap:8px')}>
                      <span style={sx('font-weight:600;font-size:13px')}>{c.title}</span>
                      <div style={sx('flex:1')} />
                      <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>{c.ago}</span>
                    </div>
                    <div style={sx('font-size:12px;color:var(--text-2);margin-top:3px;line-height:1.5')}>
                      <span style={sx('color:var(--ai);font-weight:600')}>✦</span> {c.summary}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>

          <div style={sx('display:flex;flex-direction:column;gap:18px')}>
            <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden')}>
              <div style={sx('padding:13px 16px;border-bottom:1px solid var(--border);font-weight:700;font-size:13.5px')}>Needs your review</div>
              <div style={sx('display:flex;align-items:center;gap:10px;padding:11px 16px;border-bottom:1px solid var(--border)')}>
                <span style={sx('width:22px;height:22px;border-radius:6px;background:var(--reg-bg);color:var(--reg);display:flex;align-items:center;justify-content:center;font-size:12px;flex:none')}>◈</span>
                <div style={sx('flex:1;min-width:0')}>
                  <div style={sx('font-size:12.5px;font-weight:600')}>mifid-ii.md</div>
                  <div style={sx('font-size:11px;color:var(--text-3)')}>2 unresolved comments</div>
                </div>
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--reg)")}>M</span>
              </div>
              <div style={sx('display:flex;align-items:center;gap:10px;padding:11px 16px;border-bottom:1px solid var(--border)')}>
                <span style={sx('width:22px;height:22px;border-radius:6px;background:var(--prod-bg);color:var(--prod);display:flex;align-items:center;justify-content:center;font-size:12px;flex:none')}>⑂</span>
                <div style={sx('flex:1;min-width:0')}>
                  <div style={sx('font-size:12.5px;font-weight:600')}>PR #128 · RTS 22 edits</div>
                  <div style={sx('font-size:11px;color:var(--text-3)')}>you were requested</div>
                </div>
                <span onClick={() => nav('/diff')} style={sx('font-size:11px;color:var(--prod);cursor:pointer;font-weight:600')}>Open</span>
              </div>
              <div style={sx('display:flex;align-items:center;gap:10px;padding:11px 16px')}>
                <span style={sx('width:22px;height:22px;border-radius:6px;background:var(--data-bg);color:var(--data);display:flex;align-items:center;justify-content:center;font-size:12px;flex:none')}>⇄</span>
                <div style={sx('flex:1;min-width:0')}>
                  <div style={sx('font-size:12.5px;font-weight:600')}>trade.md mapping</div>
                  <div style={sx('font-size:11px;color:var(--text-3)')}>1 drift to confirm</div>
                </div>
              </div>
            </div>
            <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:14px 16px')}>
              <div style={sx('font-weight:700;font-size:13.5px;margin-bottom:12px')}>Traceability health</div>
              <div style={sx('display:flex;flex-direction:column;gap:11px')}>
                {d.health.map((h) => (
                  <div key={h.label}>
                    <div style={sx('display:flex;justify-content:space-between;font-size:11.5px;margin-bottom:4px')}>
                      <span style={sx('color:var(--text-2)')}>{h.label}</span>
                      <span style={sx("font-family:'JetBrains Mono',monospace;font-weight:600")}>{h.pct}%</span>
                    </div>
                    <div style={sx('height:6px;border-radius:3px;background:var(--surface-2);overflow:hidden')}>
                      <div style={sx(`width:${h.pct}%;height:100%;background:${h.color}`)} />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function Kpi({ label, value, sub, valueStyle = '' }: { label: string; value: string; sub: string; valueStyle?: string }) {
  return (
    <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)')}>
      <div style={sx('font-size:11.5px;color:var(--text-2)')}>{label}</div>
      <div style={sx('display:flex;align-items:baseline;gap:8px;margin-top:8px')}>
        <span style={sx('font-size:27px;font-weight:700;letter-spacing:-.5px;' + valueStyle)}>{value}</span>
      </div>
      <div style={sx('font-size:10.5px;color:var(--text-3);margin-top:4px')}>{sub}</div>
    </div>
  );
}

export function Loading() {
  return (
    <div style={sx("flex:1;display:flex;align-items:center;justify-content:center;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>
      loading workspace…
    </div>
  );
}
