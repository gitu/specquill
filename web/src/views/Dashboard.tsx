import { useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useBranches, useMergePreview } from '../api/hooks';
import { buildDashboard, driverMeta } from '../lib/derive';
import { LinkCheckCard } from '../components/LinkCheck';
import { ForgeReview } from '../components/ForgeReview';
import { AlignmentSummary } from '../components/AlignmentSummary';
import { NewDocDialog } from '../components/NewDocDialog';

// one row in the "Needs your review" card — derived, never hard-coded
interface ReviewItem { key: string; icon: string; fg: string; bg: string; title: string; sub: string; go?: string }

export function Dashboard() {
  const nav = useNav();
  const app = useApp();
  const branches = useBranches(app.repoId);
  const defaultBranch = branches.data?.find((b) => b.isDefault)?.name;
  const onFeature = !!defaultBranch && app.branch !== defaultBranch;
  const merge = useMergePreview(app.repoId, onFeature ? app.branch : undefined, defaultBranch);
  const [newDoc, setNewDoc] = useState(false);
  if (!app.model) return <Loading />;
  const d = buildDashboard(app.model);
  const covColor = d.cov > 80 ? 'var(--data)' : d.cov > 60 ? 'var(--prod)' : 'var(--reg)';

  // needs-your-attention: committed work not yet on the default branch,
  // mapping docs with drifted fields (only when the workspace HAS mappings),
  // and change records still in triage (ditto)
  const review: ReviewItem[] = [];
  const pending = merge.data?.files?.length ?? 0;
  if (pending > 0) {
    review.push({
      key: 'merge', icon: '⑂', fg: 'var(--prod)', bg: 'var(--prod-bg)',
      title: `${pending} file${pending === 1 ? '' : 's'} ready to merge`,
      sub: `committed on ${app.branch}, not yet on ${defaultBranch} — use Merge in the header`,
    });
  }
  if (d.mapEntity) {
    const driftByMap: Record<string, number> = {};
    app.model.fields.forEach((f) => { if (f.drift) driftByMap[f.map] = (driftByMap[f.map] || 0) + 1; });
    Object.entries(driftByMap).forEach(([map, n]) => review.push({
      key: 'drift' + map, icon: '⇄', fg: 'var(--data)', bg: 'var(--data-bg)',
      title: (map.split('/').pop() || map) + ' mapping',
      sub: `${n} drift${n === 1 ? '' : 's'} to confirm`,
      go: '/editor/' + map,
    }));
  }
  if (d.changeEntity) {
    // the ATTENTION statuses come from the inbox entity's config, not a
    // hardcoded 'triage'
    const attention = new Set(app.model.inbox?.attention || []);
    app.model.changes.filter((c) => attention.has(c.status)).forEach((c) => review.push({
      key: 'chg' + c.path, icon: '⚑', fg: 'var(--reg)', bg: 'var(--reg-bg)',
      title: c.name, sub: `${d.changeEntity!.lower.replace(/s$/, '')} in ${c.status.replace(/_/g, ' ')}`,
      go: '/changes?sel=' + encodeURIComponent(c.path),
    }));
  }
  const kpiCols = d.tiles.length + (d.showCov ? 1 : 0);

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1020px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx('display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap')}>
          <div>
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
            <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Overview</h1>
          </div>
          <div style={sx('display:flex;gap:8px')}>
            {d.newDoc && (
              <button onClick={() => setNewDoc(true)} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>+ New {d.newDoc.label}</button>
            )}
            {d.changeEntity && (
              <button onClick={() => nav('/changes')} style={sx('height:32px;padding:0 13px;border:none;border-radius:8px;background:var(--text);color:var(--bg);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
                Review {d.changeEntity.lower} · {d.openCount}
              </button>
            )}
          </div>
        </div>

        <div style={sx(`display:grid;grid-template-columns:repeat(${Math.max(kpiCols, 1)},1fr);gap:14px;margin-top:22px`)}>
          {d.tiles.map((t) => (
            <Kpi key={t.key} label={t.label} value={t.value} sub={t.sub} valueStyle={t.valueStyle} />
          ))}
          {d.showCov && (
            <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)')}>
              <div style={sx('font-size:11.5px;color:var(--text-2)')}>Trace coverage</div>
              <div style={sx('display:flex;align-items:baseline;gap:8px;margin-top:8px')}>
                <span style={sx('font-size:27px;font-weight:700;letter-spacing:-.5px')}>{d.cov}<span style={sx('font-size:15px')}>%</span></span>
              </div>
              <div style={sx('height:5px;border-radius:3px;background:var(--surface-2);margin-top:8px;overflow:hidden')}>
                <div style={sx(`width:${d.cov}%;height:100%;background:${covColor}`)} />
              </div>
            </div>
          )}
        </div>

        <div style={sx(`display:grid;grid-template-columns:${d.changeEntity ? '1.65fr 1fr' : '1fr'};gap:18px;margin-top:20px;align-items:start`)}>
          {d.changeEntity && (
          <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden')}>
            <div style={sx('display:flex;align-items:center;gap:8px;padding:13px 16px;border-bottom:1px solid var(--border)')}>
              <span style={sx('font-weight:700;font-size:13.5px')}>{d.changeEntity.label}</span>
              <span style={sx('font-size:11px;color:var(--text-3)')}>— all sources</span>
              <div style={sx('flex:1')} />
              <span onClick={() => nav('/changes')} style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>Open inbox →</span>
            </div>
            {d.feed.map((c) => {
              const m = driverMeta(app.model!, c.source);
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
          )}

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
            {d.health.length > 0 && (
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
            )}
            <AlignmentSummary repo={app.repoId} branch={app.branch} />
            <LinkCheckCard />
            <ForgeReview repo={app.repoId} branch={app.branch} />
          </div>
        </div>
      </div>
      {newDoc && d.newDoc && <NewDocDialog initialKind={d.newDoc.kind} onClose={() => setNewDoc(false)} />}
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
