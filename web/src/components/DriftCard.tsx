import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { projectPath, useNav } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import {
  DriftFinding, DriftMode, useCancelDrift, useDismissFinding, useDraftRequirement, useDrift,
  FocusArea, useFileFinding, useFocusAreas, useRemedyFinding, useRunDrift, useTree,
} from '../api/hooks';

const SEV: Record<string, { label: string; fg: string; bg: string; rank: number }> = {
  high: { label: 'high', fg: 'var(--reg)', bg: 'var(--reg-bg)', rank: 0 },
  medium: { label: 'med', fg: 'var(--prod)', bg: 'var(--prod-bg)', rank: 1 },
  low: { label: 'low', fg: 'var(--text-3)', bg: 'var(--surface-2)', rank: 2 },
};

/**
 * Source alignment: AI runs that hold the workspace and its read-only
 * reference sources against each other. Two modes — "Drift" verifies each
 * document against the sources ("source" drift; "mapping drifts" are the
 * frontmatter-flagged fields elsewhere), "Gaps" sweeps each source for
 * capabilities no document covers, from which the missing requirement can be
 * reverse-engineered as a draft. Review-then-file: every work item and every
 * draft is an explicit human click on a displayed finding.
 */
export function DriftCard({ repo, branch }: { repo: string | undefined; branch: string }) {
  const nav = useNav();
  const navigate = useNavigate();
  const drift = useDrift(repo, branch);
  const tree = useTree(repo, branch);
  const run = useRunDrift(repo, branch);
  const cancel = useCancelDrift(repo, branch);
  const dismiss = useDismissFinding(repo, branch);
  const file = useFileFinding(repo, branch);
  const draft = useDraftRequirement(repo, branch);
  const remedy = useRemedyFinding(repo, branch);
  const { ensureWritableBranch } = useWorkspace();
  const suggest = useFocusAreas(repo, branch);
  const qc = useQueryClient();
  const [mode, setMode] = useState<DriftMode>('drift');
  const [scope, setScope] = useState<string[]>([]); // folder prefixes; [] = everything
  const [report, setReport] = useState(''); // '' = follow the last run / default
  const [pickSources, setPickSources] = useState<string[]>([]); // [] = every selected source
  const [focus, setFocus] = useState('');
  const [areas, setAreas] = useState<FocusArea[] | null>(null);
  const [pickTarget, setPickTarget] = useState<Record<string, string>>({});
  const [err, setErr] = useState('');

  const docs = useMemo(() => (tree.data ?? []).map((e) => e.path)
    .filter((p) => p.endsWith('.md') && !p.startsWith('.') && !p.startsWith('uploads/')
      && !p.endsWith('/index.md') && !p.endsWith('/log.md') && p !== 'index.md' && p !== 'log.md'), [tree.data]);
  const folders = useMemo(() => {
    const seen = new Set<string>();
    docs.forEach((p) => { const i = p.indexOf('/'); if (i > 0) seen.add(p.slice(0, i + 1)); });
    return [...seen].sort();
  }, [docs]);
  const scopedCount = scope.length === 0 ? docs.length
    : docs.filter((p) => scope.some((f) => p.startsWith(f))).length;

  const data = drift.data;
  if (!data?.enabled) return null;
  const running = data.run?.status === 'running';
  const findings = (data.findings ?? []).filter((f) => f.status !== 'dismissed')
    .sort((a, b) => (SEV[a.severity]?.rank ?? 3) - (SEV[b.severity]?.rank ?? 3));
  const dismissed = (data.findings ?? []).filter((f) => f.status === 'dismissed');
  const targets = data.targets ?? [];
  const sources = data.sources ?? [];
  const unitNoun = data.run?.mode === 'gaps' ? 'sources' : 'docs';

  // never hardcode a path here: the standing report is the PROJECT's
  // (drift.report: in its .specquill/config.yml), reported by the server
  const reportTarget = report || data.run?.reportPath || data.defaultReport || '';
  const reportExists = (data.reports ?? []).includes(reportTarget);
  const start = () => {
    setErr('');
    // a run writes (its report, and later drafts/remedies/backlinks): move
    // off a protected branch FIRST, so the whole run — findings included —
    // belongs to the branch the user is now on and stays visible here
    void ensureWritableBranch().then(async (on) => {
      const body = {
        mode, report: reportTarget, branch: on,
        ...(pickSources.length ? { sources: pickSources } : {}),
        ...(mode === 'gaps' && focus.trim() ? { focus: focus.trim() } : {}),
        ...(mode === 'drift' ? { paths: scope } : {}),
      };
      await run.mutateAsync(body);
      // the switch remounts this card's query against the new branch; that
      // mount fetch can land BEFORE the run row exists, and a card showing
      // "no run" never polls. Re-ask once the remount has settled.
      setTimeout(() => qc.invalidateQueries({ queryKey: ['drift', repo] }), 400);
    }).catch((e) => setErr(String((e as Error).message ?? e)));
  };
  const doFile = (f: DriftFinding) => {
    setErr('');
    const target = pickTarget[f.fingerprint] || targets[0]?.name;
    if (!target) { setErr('no work-item target configured (forge or work_item_targets)'); return; }
    file.mutate({ fingerprint: f.fingerprint, target, docPath: f.docPath || f.draftPath }, {
      onError: (e) => setErr(String((e as Error).message ?? e)),
      onSuccess: (resp) => { if (resp.backlinkError) setErr('work item created; backlink failed: ' + resp.backlinkError); },
    });
  };
  const doRemedy = (f: DriftFinding, kind: 'change' | 'work_item') => {
    setErr('');
    remedy.mutate({ fingerprint: f.fingerprint, kind }, {
      onError: (e) => setErr(String((e as Error).message ?? e)),
      // open the created change/work item on the branch it landed on
      onSuccess: (resp) => navigate(projectPath(repo, '/editor/' + resp.path, resp.branch)),
    });
  };
  const doDraft = (f: DriftFinding) => {
    setErr('');
    draft.mutate({ fingerprint: f.fingerprint }, {
      onError: (e) => setErr(String((e as Error).message ?? e)),
      // open the fresh draft on the branch it landed on (ws when main is protected)
      onSuccess: (resp) => navigate(projectPath(repo, '/editor/' + resp.path, resp.branch)),
    });
  };

  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
          Source alignment
        </span>
        <span style={sx('flex:1')} />
        {data.run && !running && (
          <span title={data.run.error} style={sx(`font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;flex:none;background:${data.run.status === 'ok' ? 'var(--data-bg)' : 'var(--reg-bg)'};color:${data.run.status === 'ok' ? 'var(--data)' : 'var(--reg)'}`)}>
            {data.run.status}
          </span>
        )}
      </div>

      {running ? (
        <div style={sx('padding:12px 14px')}>
          <div style={sx('display:flex;align-items:center;gap:8px;font-size:12px;color:var(--text-2)')}>
            <span style={sx('flex:1')}>checking {data.run!.docsDone}/{data.run!.docsTotal} {unitNoun}…</span>
            <button onClick={() => cancel.mutate()} style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px;cursor:pointer;flex:none')}>Cancel</button>
          </div>
          <div style={sx('height:5px;border-radius:3px;background:var(--surface-2);margin-top:8px;overflow:hidden')}>
            <div style={sx(`width:${data.run!.docsTotal ? Math.round((100 * data.run!.docsDone) / data.run!.docsTotal) : 0}%;height:100%;background:var(--ai)`)} />
          </div>
          {(data.run!.activity?.length ?? 0) > 0 && (
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);margin-top:8px;line-height:1.6")}>
              {data.run!.activity.slice(-3).map((line, i) => <div key={i} style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{line}</div>)}
            </div>
          )}
        </div>
      ) : (
        <div style={sx('padding:10px 14px;border-bottom:1px solid var(--border)')}>
          <div style={sx('display:flex;gap:2px;background:var(--surface-2);border-radius:7px;padding:2px;width:fit-content')}>
            <ModeSeg label="Drift" title="verify each document against the reference sources" active={mode === 'drift'} onClick={() => setMode('drift')} />
            <ModeSeg label="Gaps" title="sweep each reference source for capabilities no document covers" active={mode === 'gaps'} onClick={() => setMode('gaps')} />
            <ModeSeg label="Extract" title="analyze the application sources into a grouped requirement inventory, persisted beside the report" active={mode === 'extract'} onClick={() => setMode('extract')} />
          </div>
          {mode === 'drift' ? (
            <div style={sx('display:flex;flex-wrap:wrap;gap:5px;margin-top:9px')}>
              <ScopeChip label="Everything" active={scope.length === 0} onClick={() => setScope([])} />
              {folders.map((f) => (
                <ScopeChip key={f} label={f} active={scope.includes(f)}
                  onClick={() => setScope((s) => (s.includes(f) ? s.filter((x) => x !== f) : [...s, f]))} />
              ))}
            </div>
          ) : (
            <div style={sx('margin-top:9px')}>
              <div style={sx('display:flex;flex-wrap:wrap;gap:5px;align-items:center')}>
                <span style={sx('font-size:11px;color:var(--text-3);flex:none')}>
                  {mode === 'extract' ? 'analyze' : 'sweep'}
                </span>
                <ScopeChip label="All sources" active={pickSources.length === 0} onClick={() => setPickSources([])} />
                {sources.map((n) => (
                  <ScopeChip key={n} label={'~' + n} active={pickSources.includes(n)}
                    onClick={() => setPickSources((p) => (p.includes(n) ? p.filter((x) => x !== n) : [...p, n]))} />
                ))}
              </div>
              {mode === 'gaps' && (
                <>
                  <div style={sx('display:flex;align-items:center;gap:6px;margin-top:7px')}>
                    <span style={sx('font-size:10.5px;color:var(--text-3);flex:none')}>focus</span>
                    <input value={focus} placeholder="whole source — or name an area to aim at"
                      onChange={(e) => setFocus(e.target.value)}
                      title="Restrict this sweep to one area; gaps outside it are another sweep's job"
                      style={sx('flex:1;min-width:0;height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px')} />
                    {focus && (
                      <button onClick={() => setFocus('')}
                        style={sx('height:24px;padding:0 8px;border:none;border-radius:6px;background:none;color:var(--text-3);font-family:inherit;font-size:11px;cursor:pointer;flex:none')}>clear</button>
                    )}
                    <button onClick={() => { setErr(''); suggest.mutate(pickSources.length ? pickSources : undefined, { onSuccess: (r) => setAreas(r.areas), onError: (e) => setErr(String((e as Error).message ?? e)) }); }}
                      disabled={suggest.isPending}
                      title="Ask where a gap sweep would pay off, based on what has been extracted"
                      style={sx('height:24px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:10.5px;font-weight:600;cursor:pointer;flex:none')}>
                      {suggest.isPending ? 'Thinking…' : 'Suggest areas'}
                    </button>
                  </div>
                  {areas?.length === 0 && (
                    <div style={sx('font-size:10.5px;color:var(--text-3);margin-top:5px')}>no focus areas proposed</div>
                  )}
                  {(areas ?? []).map((a) => (
                    <div key={a.name} onClick={() => { setFocus(a.name); if (a.sources.length) setPickSources(a.sources); }}
                      title={'focus on ' + a.name}
                      style={sx('display:flex;gap:7px;align-items:baseline;margin-top:5px;padding:5px 8px;border:1px solid var(--border);border-radius:7px;cursor:pointer;background:' + (focus === a.name ? 'var(--ai-bg)' : 'var(--surface)'))}>
                      <span style={sx('font-size:11.5px;font-weight:600;flex:none')}>{a.name}</span>
                      <span style={sx('font-size:10.5px;color:var(--text-3);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{a.reason}</span>
                      {a.sources.length > 0 && (
                        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);flex:none")}>~{a.sources.join(' ~')}</span>
                      )}
                    </div>
                  ))}
                </>
              )}
            </div>
          )}
          <div style={sx('display:flex;align-items:center;gap:6px;margin-top:9px')}>
            <span style={sx('font-size:10.5px;color:var(--text-3);flex:none')}>report</span>
            <input value={reportTarget} list="drift-report-docs" spellCheck={false}
              onChange={(e) => setReport(e.target.value)}
              title="The run report doc: pick an existing one to continue it (your text outside the engine block is preserved) or type a new path to start a fresh report"
              style={sx("flex:1;min-width:0;height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:'JetBrains Mono',monospace;font-size:10.5px")} />
            <datalist id="drift-report-docs">
              {(data.reports ?? []).map((p) => <option key={p} value={p} />)}
            </datalist>
            <button onClick={() => setReport(`reports/alignment-${new Date().toISOString().slice(0, 10)}.md`)}
              title="Start a fresh, dated report instead of continuing the standing one"
              style={sx('height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-3);font-family:inherit;font-size:10.5px;cursor:pointer;flex:none')}>
              new
            </button>
            <span style={sx('font-size:10px;color:var(--text-3);flex:none')}>{reportExists ? 'continue' : 'create'}</span>
          </div>
          <div style={sx('display:flex;align-items:center;gap:8px;margin-top:9px')}>
            <span style={sx('font-size:11px;color:var(--text-3);flex:1')}>
              {mode === 'drift' ? `${scopedCount} doc${scopedCount === 1 ? '' : 's'} in scope`
                : `${pickSources.length || sources.length} source${(pickSources.length || sources.length) === 1 ? '' : 's'}` +
                  (mode === 'extract' ? ' → grouped requirement inventory'
                    : focus.trim() ? ` · focused on “${focus.trim()}”` : ' · uncovered capabilities')}
            </span>
            <button onClick={start} disabled={run.isPending || (mode === 'drift' ? scopedCount === 0 : sources.length === 0)}
              style={sx('height:26px;padding:0 12px;border:none;border-radius:7px;background:var(--text);color:var(--bg);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer;flex:none')}>
              {mode === 'drift' ? 'Check drift' : mode === 'extract' ? 'Analyze app' : 'Find gaps'}
            </button>
          </div>
        </div>
      )}

      {err && (
        <div style={sx('padding:8px 14px;font-size:11.5px;color:var(--reg);background:var(--reg-bg);border-bottom:1px solid var(--border)')}>{err}</div>
      )}
      {data.run?.status === 'error' && data.run.error && !err && (
        <div style={sx('padding:8px 14px;font-size:11.5px;color:var(--reg);background:var(--reg-bg);border-bottom:1px solid var(--border)')}>{data.run.error}</div>
      )}

      {(data.extractions ?? []).map((e) => (
        <div key={e.path} onClick={() => navigate(projectPath(repo, '/editor/' + e.path, data.run?.reportBranch || branch))}
          style={sx("display:flex;align-items:center;gap:6px;padding:6px 14px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--prod);cursor:pointer")}>
          ⌗ {e.path}<span style={sx('color:var(--text-3)')}>— extracted requirements of ~{e.source}</span>
        </div>
      ))}
      {data.run && data.run.reportPath !== '' && (
        <div onClick={() => navigate(projectPath(repo, '/editor/' + data.run!.reportPath, data.run!.reportBranch || branch))}
          style={sx("display:flex;align-items:center;gap:6px;padding:7px 14px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--prod);cursor:pointer")}>
          ⎙ {data.run.reportPath}
          <span style={sx('color:var(--text-3)')}>— {running ? 'updating live' : 'run report in the repo'}{data.run.reportBranch && data.run.reportBranch !== branch ? ` on ${data.run.reportBranch}` : ''}</span>
        </div>
      )}

      {findings.map((f) => {
        const sev = SEV[f.severity] ?? SEV.low;
        const gap = f.docPath === '';
        // both kinds propose a NEW document, so both can be drafted
        const proposes = gap || f.kind === 'new-requirement';
        return (
          <div key={f.fingerprint} data-drift-finding={f.kind} style={sx('padding:10px 14px;border-bottom:1px solid var(--border)')}>
            <div style={sx('display:flex;align-items:center;gap:7px')}>
              <span style={sx(`flex:none;padding:2px 7px;border-radius:6px;font-size:10px;font-weight:700;background:${sev.bg};color:${sev.fg}`)}>{sev.label}</span>
              {gap && <span style={sx('flex:none;padding:2px 7px;border-radius:6px;font-size:10px;font-weight:700;background:var(--ai-bg);color:var(--ai)')}>gap</span>}
              {f.kind === 'new-requirement' && <span style={sx('flex:none;padding:2px 7px;border-radius:6px;font-size:10px;font-weight:700;background:var(--ai-bg);color:var(--ai)')}>new</span>}
              <span style={sx('font-size:12.5px;font-weight:600;flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')} title={f.detail}>{f.title}</span>
            </div>
            {gap ? (
              <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-2);margin-top:4px")}>
                ~{f.source}/{f.anchor}
                {f.draftPath
                  ? <span onClick={() => nav('/editor/' + f.draftPath)} style={sx('color:var(--prod);cursor:pointer')}> → {f.draftPath}</span>
                  : f.suggestedPath && <span style={sx('color:var(--text-3)')}> → suggests {f.suggestedPath}</span>}
              </div>
            ) : (
              <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;margin-top:4px")}>
                <span onClick={() => nav('/editor/' + f.docPath)} style={sx('color:var(--prod);cursor:pointer')}>
                  {f.docPath}{f.anchor ? ` · ${f.anchor}` : ''}
                </span> <span style={sx('color:var(--text-3)')}>vs ~{f.source}</span>
                {f.draftPath
                  ? <span onClick={() => nav('/editor/' + f.draftPath)} style={sx('color:var(--prod);cursor:pointer')}> → {f.draftPath}</span>
                  : f.suggestedPath && <span style={sx('color:var(--text-3)')}> → suggests {f.suggestedPath}</span>}
              </div>
            )}
            <div style={sx('font-size:11.5px;color:var(--text-2);margin-top:3px;line-height:1.45')}>{f.detail}</div>
            {(f.evidence?.length ?? 0) > 0 && (
              <details style={{ marginTop: 4 }}>
                <summary style={sx('font-size:10.5px;color:var(--text-3);cursor:pointer;user-select:none')}>
                  evidence ({f.evidence.length}) — verified against ~{f.source}
                </summary>
                {f.evidence.map((ev, i) => (
                  <div key={i} style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-2);margin:4px 0 0 12px;line-height:1.5")}>
                    <span style={sx('color:var(--text-3)')}>{ev.path}:</span> “{ev.quote}”
                  </div>
                ))}
              </details>
            )}
            <div style={sx('display:flex;align-items:center;gap:7px;margin-top:7px;flex-wrap:wrap')}>
              {proposes && !f.draftPath && (
                <button onClick={() => doDraft(f)} disabled={draft.isPending}
                  title="Reverse-engineer the missing requirement document from the source evidence (AI draft, uncommitted)"
                  style={sx('height:24px;padding:0 10px;border:1px solid var(--ai);border-radius:6px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
                  {draft.isPending ? 'Drafting…' : 'Draft requirement'}
                </button>
              )}
              {f.remedyPath ? (
                <span onClick={() => nav('/editor/' + f.remedyPath)}
                  style={sx('font-size:11px;font-weight:600;color:var(--data);cursor:pointer')}>
                  ✓ {f.remedyKind.replace('_', ' ')}: {f.remedyPath.split('/').pop()}
                </span>
              ) : (
                <>
                  <button onClick={() => doRemedy(f, 'change')} disabled={remedy.isPending}
                    title="Draft a change record (WHY) in the workspace that drives updating the requirements"
                    style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
                    + Change
                  </button>
                  <button onClick={() => doRemedy(f, 'work_item')} disabled={remedy.isPending}
                    title="Draft a work item (WHEN) in the workspace, linked to the affected document"
                    style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
                    + Work item
                  </button>
                </>
              )}
              {f.status === 'filed' && f.workItemUrl ? (
                <a href={f.workItemUrl} target="_blank" rel="noopener noreferrer"
                  style={sx('font-size:11px;font-weight:600;color:var(--data);text-decoration:none')}>
                  ↗ work item filed{f.workItemTarget ? ` · ${f.workItemTarget}` : ''}
                </a>
              ) : (
                <>
                  {targets.length > 1 && (
                    <select value={pickTarget[f.fingerprint] || targets[0]?.name}
                      onChange={(e) => setPickTarget((m) => ({ ...m, [f.fingerprint]: e.target.value }))}
                      style={sx('height:24px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;flex:none')}>
                      {targets.map((tg) => <option key={tg.name} value={tg.name}>{tg.name}</option>)}
                    </select>
                  )}
                  {targets.length > 0 && (
                    <button onClick={() => doFile(f)} disabled={file.isPending}
                      style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
                      File issue
                    </button>
                  )}
                </>
              )}
              <span style={sx('flex:1')} />
              <button onClick={() => dismiss.mutate({ fingerprint: f.fingerprint })}
                style={sx('height:24px;padding:0 10px;border:none;border-radius:6px;background:none;color:var(--text-3);font-family:inherit;font-size:11px;cursor:pointer;flex:none')}>
                Dismiss
              </button>
            </div>
          </div>
        );
      })}

      {!running && findings.length === 0 && data.run && data.run.status === 'ok' && (
        <div style={sx('padding:11px 14px;font-size:12px;color:var(--text-3)')}>
          <span style={sx('color:var(--data)')}>✓</span> {data.run.mode === 'gaps' ? 'no coverage gaps found in the last run' : 'no drift found in the last run'}
        </div>
      )}

      {(data.run?.droppedUnverified || dismissed.length > 0) && (
        <div style={sx('padding:7px 14px;font-size:10.5px;color:var(--text-3)')}>
          {data.run?.droppedUnverified ? `${data.run.droppedUnverified} finding${data.run.droppedUnverified === 1 ? '' : 's'} dropped (evidence did not verify)` : ''}
          {data.run?.droppedUnverified && dismissed.length > 0 ? ' · ' : ''}
          {dismissed.length > 0 ? `${dismissed.length} dismissed` : ''}
        </div>
      )}
    </div>
  );
}

function ModeSeg({ label, title, active, onClick }: { label: string; title: string; active: boolean; onClick: () => void }) {
  return (
    <span onClick={onClick} title={title}
      style={sx(`padding:3px 12px;border-radius:6px;font-size:11.5px;font-weight:600;cursor:pointer;user-select:none;${active ? 'background:var(--surface);color:var(--text);box-shadow:var(--shadow)' : 'color:var(--text-3)'}`)}>
      {label}
    </span>
  );
}

function ScopeChip({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick}
      style={sx(`height:22px;padding:0 9px;border:1px solid ${active ? 'var(--text)' : 'var(--border-2)'};border-radius:99px;background:${active ? 'var(--text)' : 'var(--surface)'};color:${active ? 'var(--bg)' : 'var(--text-2)'};font-family:inherit;font-size:10.5px;font-weight:600;cursor:pointer;flex:none`)}>
      {label}
    </button>
  );
}
