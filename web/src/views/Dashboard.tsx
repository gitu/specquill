import { useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useMe, usePRs } from '../api/hooks';
import { buildDashboard, srcMeta } from '../lib/derive';
import { LinkCheckCard } from '../components/LinkCheck';
import { NewDocDialog } from '../components/NewDocDialog';

// one row in the "Needs your review" card — derived, never hard-coded
interface ReviewItem { key: string; icon: string; fg: string; bg: string; title: string; sub: string; go?: string }

export function Dashboard() {
  const nav = useNav();
  const app = useApp();
  const me = useMe();
  const prs = usePRs(app.repoId);
  const [newDoc, setNewDoc] = useState(false);
  if (!app.model) return <Loading />;
  const d = buildDashboard(app.model);
  const covColor = d.cov > 80 ? 'var(--data)' : d.cov > 60 ? 'var(--prod)' : 'var(--reg)';

  // needs-your-review: open PRs (yours vs. awaiting your approval), mapping
  // docs with drifted fields, and change records still in triage
  const review: ReviewItem[] = [];
  for (const p of prs.data || []) {
    const mine = p.author.id === me.data?.id;
    const approvedByMe = p.approvals.some((a) => a.current && a.user.id === me.data?.id);
    const state = mine ? (p.approvals.some((a) => a.current) ? 'approved — ready to merge' : 'your open PR')
      : approvedByMe ? 'you approved' : 'awaiting your review';
    const comments = p.commentCount ? ` · ${p.commentCount} comment${p.commentCount === 1 ? '' : 's'}` : '';
    review.push({
      key: 'pr' + p.number, icon: '⑂', fg: 'var(--prod)', bg: 'var(--prod-bg)',
      title: `PR #${p.number} · ${p.title}`,
      sub: state + comments,
      go: `/prs/${p.number}`,
    });
  }
  const driftByMap: Record<string, number> = {};
  app.model.fields.forEach((f) => { if (f.drift) driftByMap[f.map] = (driftByMap[f.map] || 0) + 1; });
  Object.entries(driftByMap).forEach(([map, n]) => review.push({
    key: 'drift' + map, icon: '⇄', fg: 'var(--data)', bg: 'var(--data-bg)',
    title: (map.split('/').pop() || map) + ' mapping',
    sub: `${n} drift${n === 1 ? '' : 's'} to confirm`,
    go: '/editor/' + map,
  }));
  app.model.changes.filter((c) => c.status === 'triage').forEach((c) => review.push({
    key: 'chg' + c.path, icon: '⚑', fg: 'var(--reg)', bg: 'var(--reg-bg)',
    title: c.name, sub: 'change in triage', go: '/changes?sel=' + encodeURIComponent(c.path),
  }));

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1020px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx('display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap')}>
          <div>
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
            <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Overview</h1>
          </div>
          <div style={sx('display:flex;gap:8px')}>
            <button onClick={() => setNewDoc(true)} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>+ New requirement</button>
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
              {review.slice(0, 5).map((it, i) => (
                <div key={it.key} onClick={it.go ? () => nav(it.go!) : undefined}
                  style={sx('display:flex;align-items:center;gap:10px;padding:11px 16px;' +
                    (i < Math.min(review.length, 5) - 1 ? 'border-bottom:1px solid var(--border);' : '') + (it.go ? 'cursor:pointer' : ''))}>
                  <span style={sx(`width:22px;height:22px;border-radius:6px;background:${it.bg};color:${it.fg};display:flex;align-items:center;justify-content:center;font-size:12px;flex:none`)}>{it.icon}</span>
                  <div style={sx('flex:1;min-width:0')}>
                    <div style={sx('font-size:12.5px;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{it.title}</div>
                    <div style={sx('font-size:11px;color:var(--text-3)')}>{it.sub}</div>
                  </div>
                  {it.go && <span style={sx('font-size:11px;color:var(--prod);font-weight:600')}>Open</span>}
                </div>
              ))}
              {review.length === 0 && (
                <div style={sx('padding:14px 16px;font-size:12px;color:var(--text-3)')}>
                  <span style={sx('color:var(--data)')}>✓</span> nothing needs you right now
                </div>
              )}
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
            <LinkCheckCard />
          </div>
        </div>
      </div>
      {newDoc && <NewDocDialog initialKind="requirement" onClose={() => setNewDoc(false)} />}
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
