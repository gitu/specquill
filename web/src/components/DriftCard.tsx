import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { localizeFeed } from '../lib/feed';
import { projectPath, useNav } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import {
  AlignmentRecipe, DriftFinding, DriftMode, DriftRun, RecipeCheck, useAlignmentRecipes, useCancelDrift,
  useCheckRecipe, useDismissFinding, useDraftRequirement, useDrift,
  FocusArea, PlannedDoc, useCreateDocuments, useFileFinding, useFocusAreas, usePlanDocuments,
  useRemedyFinding, useRunDrift, useTree,
} from '../api/hooks';

const SEV: Record<string, { label: string; fg: string; bg: string; rank: number }> = {
  high: { label: 'high', fg: 'var(--reg)', bg: 'var(--reg-bg)', rank: 0 },
  medium: { label: 'med', fg: 'var(--prod)', bg: 'var(--prod-bg)', rank: 1 },
  low: { label: 'low', fg: 'var(--text-3)', bg: 'var(--surface-2)', rank: 2 },
};

/**
 * The run controls: pick a mode (Drift verifies each document against the
 * sources, Gaps sweeps the sources for uncovered capabilities, Extract
 * analyzes the app into a requirement inventory), aim it, choose the report,
 * and watch it go. Every mode can be narrowed the same three ways — which
 * reference sources it touches, which units it covers, and one area to
 * concentrate on. The findings it produces render full-width in
 * DriftFindings — they need the room.
 */
export function DriftControls({ repo, branch, runId = 0, onSelectRun }: {
  repo: string | undefined; branch: string;
  runId?: number; onSelectRun?: (id: number) => void;
}) {
  const navigate = useNavigate();
  const drift = useDrift(repo, branch, runId);
  const tree = useTree(repo, branch);
  const run = useRunDrift(repo, branch);
  const cancel = useCancelDrift(repo, branch);
  const { ensureWritableBranch } = useWorkspace();
  const suggest = useFocusAreas(repo, branch);
  const recipes = useAlignmentRecipes(repo, branch);
  const check = useCheckRecipe(repo, branch);
  const qc = useQueryClient();
  // the mode IS a recipe slug: the three built-ins are recipes like any other
  const [mode, setMode] = useState<DriftMode>('drift');
  const [dryRun, setDryRun] = useState<RecipeCheck | null>(null);
  // folder prefixes (trailing '/') and/or exact document paths; [] = everything
  const [scope, setScope] = useState<string[]>([]);
  const [docFilter, setDocFilter] = useState('');
  const [report, setReport] = useState(''); // '' = follow the last run / default
  const [pickSources, setPickSources] = useState<string[]>([]); // [] = every selected source
  const [focus, setFocus] = useState('');
  const [areas, setAreas] = useState<FocusArea[] | null>(null);
  const [err, setErr] = useState('');

  const docs = useMemo(() => (tree.data ?? []).map((e) => e.path)
    .filter((p) => p.endsWith('.md') && !p.startsWith('.') && !p.startsWith('uploads/')
      && !p.endsWith('/index.md') && !p.endsWith('/log.md') && p !== 'index.md' && p !== 'log.md'), [tree.data]);
  const folders = useMemo(() => {
    const seen = new Set<string>();
    docs.forEach((p) => { const i = p.indexOf('/'); if (i > 0) seen.add(p.slice(0, i + 1)); });
    return [...seen].sort();
  }, [docs]);
  // a folder entry covers everything under it; anything else is one document
  const inScope = (p: string) => scope.some((s) => (s.endsWith('/') ? p.startsWith(s) : p === s));
  const scopedCount = scope.length === 0 ? docs.length : docs.filter(inScope).length;
  const pickedDocs = scope.filter((s) => !s.endsWith('/'));
  const toggle = (list: string[], v: string) =>
    (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);

  const all = recipes.data?.recipes ?? [];
  const custom = all.filter((r) => !r.builtin);
  const activeRecipe: AlignmentRecipe | undefined = all.find((r) => r.slug === mode);
  const recipeErrors = Object.entries(recipes.data?.errors ?? {});
  // WHICH units a run iterates is the recipe's call, not the mode's: drift
  // walks documents, gaps and extraction walk sources, and a project recipe
  // says for itself.
  const unitsAreDocs = activeRecipe ? activeRecipe.units === 'docs' : mode === 'drift';

  const data = drift.data;
  if (!data?.enabled) return null;
  const shown = data.run;
  // a run in flight may not be the one on screen — the user can look back at
  // an older run while it works
  const running = shown?.status === 'running';
  const active = data.activeRunId !== 0 && !running;
  const sources = data.sources ?? [];
  // what the SHOWN run was walking (its own recipe's call, not the picker's)
  const shownScope = data.runs?.find((r) => r.id === shown?.id);
  const unitNoun = (shown?.scope?.length ?? 0) > 0 && shown!.scope[0].endsWith('.md') ? 'docs' : 'sources';

  // never hardcode a path here: the standing report is the PROJECT's
  // (drift.report: in its .specquill/config.yml), reported by the server.
  // A new run continues the NEWEST run's report, not the one being looked at.
  const reportTarget = report || data.runs?.[0]?.reportPath || data.defaultReport || '';
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
        ...(focus.trim() ? { focus: focus.trim() } : {}),
        ...(unitsAreDocs ? { paths: scope } : {}),
      };
      await run.mutateAsync(body);
      setDryRun(null);
      onSelectRun?.(0); // follow the run that was just started
      // the switch remounts this card's query against the new branch; that
      // mount fetch can land BEFORE the run row exists, and a card showing
      // "no run" never polls. Re-ask once the remount has settled.
      setTimeout(() => qc.invalidateQueries({ queryKey: ['drift', repo] }), 400);
    }).catch((e) => setErr(String((e as Error).message ?? e)));
  };
  // pick up a run that stopped with units left: it keeps its own mode,
  // sources, focus and report — the server has all of that
  const resume = () => {
    setErr('');
    void ensureWritableBranch().then(async (on) => {
      await run.mutateAsync({ branch: on, resume: shown!.id });
      onSelectRun?.(0);
      setTimeout(() => qc.invalidateQueries({ queryKey: ['drift', repo] }), 400);
    }).catch((e) => setErr(String((e as Error).message ?? e)));
  };
  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
          Source alignment
        </span>
        <span style={sx('flex:1')} />
        {shown && !running && (
          <span title={shown.error} style={sx(`font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;flex:none;background:${shown.status === 'ok' ? 'var(--data-bg)' : 'var(--reg-bg)'};color:${shown.status === 'ok' ? 'var(--data)' : 'var(--reg)'}`)}>
            {shown.status}
          </span>
        )}
      </div>

      {running ? (
        <div style={sx('padding:12px 14px')}>
          <div style={sx('display:flex;align-items:center;gap:8px;font-size:12px;color:var(--text-2)')}>
            <span style={sx('flex:1')}>checking {shown!.docsDone}/{shown!.docsTotal} {unitNoun}…</span>
            <button onClick={() => cancel.mutate()} style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px;cursor:pointer;flex:none')}>Cancel</button>
          </div>
          <div style={sx('height:5px;border-radius:3px;background:var(--surface-2);margin-top:8px;overflow:hidden')}>
            <div style={sx(`width:${shown!.docsTotal ? Math.round((100 * shown!.docsDone) / shown!.docsTotal) : 0}%;height:100%;background:var(--ai)`)} />
          </div>
          {/* the run is a server-side worker, not a browser task — say so, or
              people sit and watch a page they do not need to watch */}
          <div style={sx('font-size:11px;color:var(--text-3);margin-top:8px;line-height:1.5')}>
            Runs on the server — you can close this page or work elsewhere. Findings and the
            report are written as it goes, and this page picks the run back up when you return.
          </div>
          {(shown!.activity?.length ?? 0) > 0 && (
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);margin-top:8px;line-height:1.6")}>
              {localizeFeed(shown!.activity.slice(-3), shown!.startedAt).map((line, i) => <div key={i} style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{line}</div>)}
            </div>
          )}
        </div>
      ) : (
        <div style={sx('padding:10px 14px;border-bottom:1px solid var(--border)')}>
          {/* looking back at an older run while another one works: the controls
              stay put (a second run would 409), the live one is one click away */}
          {active && (
            <div data-drift-active style={sx('display:flex;align-items:center;gap:9px;margin-bottom:10px;padding:8px 10px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2)')}>
              <span style={sx('flex:1;min-width:0;font-size:11.5px;color:var(--text-2)')}>
                Run {data.activeRunId} is in progress on this branch.
              </span>
              <button onClick={() => onSelectRun?.(0)}
                style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
                Follow it
              </button>
            </div>
          )}
          {shown?.resumable && (
            <div data-drift-resume style={sx('display:flex;align-items:center;gap:9px;margin-bottom:10px;padding:8px 10px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2)')}>
              <div style={sx('flex:1;min-width:0;font-size:11.5px;color:var(--text-2);line-height:1.5')}>
                {shown.status === 'interrupted'
                  ? 'The server restarted during this run.'
                  : shown.status === 'cancelled' ? 'This run was stopped.' : 'This run failed part way.'}
                {' '}
                <strong>{shown.docsDone} of {shown.docsTotal}</strong> {unitNoun === 'docs' ? 'documents' : 'sources'} were
                checked — the rest can be picked up.
              </div>
              <button data-drift-resume-start onClick={resume} disabled={run.isPending || active}
                style={sx('height:26px;padding:0 11px;border:1px solid var(--ai);border-radius:7px;background:var(--ai);color:#fff;font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer;flex:none')}>
                {run.isPending ? 'Resuming…' : `Resume (${shown.docsTotal - shown.docsDone} left)`}
              </button>
            </div>
          )}
          {/* the three shipped pipelines stay one click away — they are
              recipes now, but they are still what people reach for */}
          <div style={sx('display:flex;align-items:center;gap:8px;flex-wrap:wrap')}>
            <div style={sx('display:flex;gap:2px;background:var(--surface-2);border-radius:7px;padding:2px;width:fit-content')}>
              <ModeSeg label="Drift" title="verify each document against the reference sources" active={mode === 'drift'} onClick={() => { setMode('drift'); setDryRun(null); }} />
              <ModeSeg label="Gaps" title="sweep each reference source for capabilities no document covers" active={mode === 'gaps'} onClick={() => { setMode('gaps'); setDryRun(null); }} />
              <ModeSeg label="Extract" title="analyze the application sources into a grouped requirement inventory, persisted beside the report" active={mode === 'extract'} onClick={() => { setMode('extract'); setDryRun(null); }} />
            </div>
            {/* …and the project's own pipelines from .specquill/alignment/ */}
            {custom.length > 0 && (
              <select data-drift-recipe value={custom.some((r) => r.slug === mode) ? mode : ''}
                onChange={(e) => { if (e.target.value) { setMode(e.target.value); setDryRun(null); } }}
                title="This project's own alignment recipes"
                style={sx("height:26px;padding:0 6px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px")}>
                <option value="">Project recipe…</option>
                {custom.map((r) => <option key={r.slug} value={r.slug}>{r.name}</option>)}
              </select>
            )}
          </div>

          {/* a recipe that IS there but does not load must say so — otherwise
              it is simply missing from the picker and nobody knows why */}
          {recipeErrors.map(([slug, msg]) => (
            <div key={slug} style={sx('margin-top:7px;padding:6px 9px;border:1px solid var(--reg);border-radius:7px;background:var(--reg-bg);font-size:10.5px;color:var(--reg);line-height:1.5')}>
              {recipes.data?.dir}{slug}.md — {msg}
            </div>
          ))}

          {/* what the selected recipe is and where it lives */}
          {activeRecipe && !activeRecipe.builtin && (
            <div style={sx('margin-top:8px;padding:7px 10px;border:1px solid var(--border);border-radius:8px;background:var(--surface-2)')}>
              <div style={sx('display:flex;align-items:baseline;gap:8px')}>
                <span style={sx('font-size:11.5px;font-weight:600;flex:none')}>{activeRecipe.name}</span>
                <span style={sx('font-size:10.5px;color:var(--text-3);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>
                  {activeRecipe.description}
                </span>
                {/* a recipe is an ordinary document — edit it in the editor */}
                <span onClick={() => navigate(projectPath(repo, '/editor/' + activeRecipe.path, branch))}
                  style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--prod);cursor:pointer;flex:none")}>
                  ✎ edit
                </span>
              </div>
              <div style={sx("margin-top:4px;font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>
                {activeRecipe.stages.map((st) => st.label || st.id).join(' → ')}
                {activeRecipe.files.include?.length ? ' · files: ' + activeRecipe.files.include.join(', ') : ''}
                {activeRecipe.files.describe ? ' · “' + activeRecipe.files.describe + '”' : ''}
              </div>
              {activeRecipe.warnings.map((wmsg, i) => (
                <div key={i} style={sx('margin-top:4px;font-size:10px;color:var(--prod)')}>⚠ {wmsg}</div>
              ))}
            </div>
          )}

          {/* documents are the unit only when the recipe says so — a
              per-source recipe's unit picker IS the source row below */}
          {unitsAreDocs && (
            <div style={sx('margin-top:9px')}>
              <div style={sx('display:flex;flex-wrap:wrap;gap:5px;align-items:center')}>
                <span style={sx('font-size:11px;color:var(--text-3);flex:none')}>check</span>
                <ScopeChip label="Everything" active={scope.length === 0} onClick={() => setScope([])} />
                {folders.map((f) => (
                  <ScopeChip key={f} label={f} active={scope.includes(f)}
                    onClick={() => setScope((s) => toggle(s, f))} />
                ))}
              </div>
              {/* a folder is often still too much: name the documents outright */}
              <details style={{ marginTop: 6 }}>
                <summary style={sx('font-size:10.5px;color:var(--text-3);cursor:pointer;user-select:none')}>
                  pick individual documents{pickedDocs.length ? ` (${pickedDocs.length} picked)` : ''}
                </summary>
                <div style={sx('margin-top:5px')}>
                  <input value={docFilter} onChange={(e) => setDocFilter(e.target.value)}
                    placeholder="filter documents" spellCheck={false}
                    style={sx("width:100%;height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:'JetBrains Mono',monospace;font-size:10.5px")} />
                  <div data-drift-doc-picker style={sx('max-height:150px;overflow-y:auto;margin-top:5px;border:1px solid var(--border);border-radius:7px')}>
                    {docs.filter((p) => p.toLowerCase().includes(docFilter.toLowerCase())).map((p) => (
                      <label key={p} title={p}
                        style={sx("display:flex;align-items:center;gap:7px;padding:3px 8px;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-2);cursor:pointer")}>
                        <input type="checkbox" checked={scope.includes(p)}
                          onChange={() => setScope((s) => toggle(s, p))} style={{ flex: 'none', cursor: 'pointer' }} />
                        <span style={sx('flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{p}</span>
                      </label>
                    ))}
                    {docs.length === 0 && (
                      <div style={sx('padding:5px 8px;font-size:10.5px;color:var(--text-3)')}>no documents on this branch</div>
                    )}
                  </div>
                </div>
              </details>
            </div>
          )}

          {/* which references the run touches — a drift check verifies against
              them, a gaps sweep and an extraction work through them */}
          <div style={sx('display:flex;flex-wrap:wrap;gap:5px;align-items:center;margin-top:9px')}>
            <span style={sx('font-size:11px;color:var(--text-3);flex:none')}>
              {mode === 'extract' ? 'analyze' : unitsAreDocs ? 'against' : 'sweep'}
            </span>
            <ScopeChip label="All sources" active={pickSources.length === 0} onClick={() => setPickSources([])} />
            {sources.map((n) => (
              <ScopeChip key={n} label={'~' + n} active={pickSources.includes(n)}
                onClick={() => setPickSources((p) => toggle(p, n))} />
            ))}
          </div>

          {/* one area to concentrate on — every mode honours it, so a large
              application can be worked through deliberately */}
          <div style={sx('display:flex;align-items:center;gap:6px;margin-top:7px')}>
            <span style={sx('font-size:10.5px;color:var(--text-3);flex:none')}>focus</span>
            <input value={focus} onChange={(e) => setFocus(e.target.value)}
              placeholder={unitsAreDocs ? 'whole documents — or name an area to concentrate on'
                : mode === 'extract' ? 'whole application — or name an area to analyze'
                : 'whole source — or name an area to aim at'}
              title={unitsAreDocs ? 'Restrict this check to one area; drift outside it is another check’s job'
                : mode === 'extract' ? 'Extract only this area of the application; the rest is another analysis’ job'
                : 'Restrict this sweep to one area; gaps outside it are another sweep’s job'}
              style={sx('flex:1;min-width:0;height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px')} />
            {focus && (
              <button onClick={() => setFocus('')}
                style={sx('height:24px;padding:0 8px;border:none;border-radius:6px;background:none;color:var(--text-3);font-family:inherit;font-size:11px;cursor:pointer;flex:none')}>clear</button>
            )}
            <button onClick={() => { setErr(''); suggest.mutate(pickSources.length ? pickSources : undefined, { onSuccess: (r) => setAreas(r.areas), onError: (e) => setErr(String((e as Error).message ?? e)) }); }}
              disabled={suggest.isPending}
              title="Ask where the next run would pay off, based on what has been extracted"
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
          <div style={sx('display:flex;align-items:center;gap:6px;margin-top:9px')}>
            <span style={sx('font-size:10.5px;color:var(--text-3);flex:none')}>report</span>
            <input value={reportTarget} list="drift-report-docs" spellCheck={false}
              onChange={(e) => setReport(e.target.value)}
              title="The run report doc: pick an existing one to continue it (your text outside the engine block is preserved) or type a new path to start a fresh report"
              style={sx("flex:1;min-width:0;height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:'JetBrains Mono',monospace;font-size:10.5px")} />
            <datalist id="drift-report-docs">
              {(data.reports ?? []).map((p) => <option key={p} value={p} />)}
            </datalist>
            <button onClick={() => {
              // the default is already dated, so "new" must be finer-grained
              // to start a SEPARATE report within the same day. UTC end to end
              // (like the server's own date tokens) — the name lands in git.
              const t = new Date();
              const p = (n: number) => String(n).padStart(2, '0');
              const stamp = t.toISOString().slice(0, 10) + '-' +
                p(t.getUTCHours()) + p(t.getUTCMinutes());
              setReport(`reports/alignment-${stamp}.md`);
            }}
              title="Start a separate report now instead of continuing today's"
              style={sx('height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-3);font-family:inherit;font-size:10.5px;cursor:pointer;flex:none')}>
              new
            </button>
            <span style={sx('font-size:10px;color:var(--text-3);flex:none')}>{reportExists ? 'continue' : 'create'}</span>
          </div>
          {/* what this recipe would actually do, before it does it: a pipeline
              multiplies stages by items by units, and that number is not
              visible by reading the document */}
          {dryRun && (
            <div data-drift-dryrun style={sx('margin-top:9px;padding:8px 10px;border:1px solid ' +
              (dryRun.ok ? 'var(--border)' : 'var(--reg)') + ';border-radius:8px;background:' +
              (dryRun.ok ? 'var(--surface-2)' : 'var(--reg-bg)'))}>
              {!dryRun.ok ? (
                <div style={sx('font-size:11px;color:var(--reg);line-height:1.5')}>{dryRun.error}</div>
              ) : (
                <>
                  <div style={sx('font-size:11px;color:var(--text-2)')}>
                    {dryRun.units} {dryRun.unitKind === 'docs' ? 'document' : 'source'}{dryRun.units === 1 ? '' : 's'}
                    {' · '}{dryRun.estimated ? '~' : ''}{dryRun.estimatedCalls} model call{dryRun.estimatedCalls === 1 ? '' : 's'}
                    {dryRun.maxCallsPerRun > 0 && ` of ${dryRun.maxCallsPerRun}`}
                  </div>
                  <div style={sx("margin-top:5px;font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);line-height:1.6")}>
                    {dryRun.stages.map((st) => (
                      <div key={st.id}>
                        {st.label || st.id}: {st.calls} call{st.calls === 1 ? '' : 's'}
                        {st.files && ' · ' + Object.entries(st.files).map(([n, c]) => `~${n} ${c} file${c === 1 ? '' : 's'}`).join(', ')}
                        {st.describeCalls ? ` · +${st.describeCalls} to select files` : ''}
                      </div>
                    ))}
                  </div>
                  {dryRun.note && (
                    <div style={sx('margin-top:5px;font-size:10.5px;color:var(--prod);line-height:1.5')}>⚠ {dryRun.note}</div>
                  )}
                  {(dryRun.warnings ?? []).map((wmsg, i) => (
                    <div key={i} style={sx('margin-top:4px;font-size:10px;color:var(--prod)')}>⚠ {wmsg}</div>
                  ))}
                </>
              )}
            </div>
          )}
          <div style={sx('display:flex;align-items:center;gap:8px;margin-top:9px')}>
            <span style={sx('font-size:11px;color:var(--text-3);flex:1')}>
              {summarize(mode, scopedCount, pickSources.length || sources.length, focus, unitsAreDocs)}
            </span>
            <button data-drift-dryrun-start onClick={() => {
              setErr('');
              check.mutate({
                recipe: mode,
                ...(pickSources.length ? { sources: pickSources } : {}),
                ...(unitsAreDocs ? { paths: scope } : {}),
              }, { onSuccess: setDryRun, onError: (e) => setErr(String((e as Error).message ?? e)) });
            }} disabled={check.isPending}
              title="What this recipe would read and how many model calls it would make — no run, no writes"
              style={sx('height:26px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
              {check.isPending ? 'Checking…' : 'Dry run'}
            </button>
            <button onClick={start} title={active ? 'a run is already in progress on this branch' : undefined}
              disabled={run.isPending || active || (unitsAreDocs ? scopedCount === 0 : sources.length === 0)}
              style={sx('height:26px;padding:0 12px;border:none;border-radius:7px;background:var(--text);color:var(--bg);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer;flex:none')}>
              {mode === 'drift' ? 'Check drift' : mode === 'extract' ? 'Analyze app'
                : mode === 'gaps' ? 'Find gaps' : 'Run ' + (activeRecipe?.name ?? mode)}
            </button>
          </div>
        </div>
      )}

      {err && (
        <div style={sx('padding:8px 14px;font-size:11.5px;color:var(--reg);background:var(--reg-bg);border-bottom:1px solid var(--border)')}>{err}</div>
      )}
      {shown?.status === 'error' && shown.error && !err && (
        <div style={sx('padding:8px 14px;font-size:11.5px;color:var(--reg);background:var(--reg-bg);border-bottom:1px solid var(--border)')}>{shown.error}</div>
      )}

      {(data.extractions ?? []).map((e) => (
        <div key={e.path} onClick={() => navigate(projectPath(repo, '/editor/' + e.path, shown?.reportBranch || branch))}
          style={sx("display:flex;align-items:center;gap:6px;padding:6px 14px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--prod);cursor:pointer")}>
          ⌗ {e.path}<span style={sx('color:var(--text-3)')}>— extracted requirements of ~{e.source}</span>
        </div>
      ))}
      {shown && shown.reportPath !== '' && (
        <div onClick={() => navigate(projectPath(repo, '/editor/' + shown.reportPath, shown.reportBranch || branch))}
          style={sx("display:flex;align-items:center;gap:6px;padding:7px 14px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--prod);cursor:pointer")}>
          ⎙ {shown.reportPath}
          <span style={sx('color:var(--text-3)')}>— {running ? 'updating live' : 'run report in the repo'}{shown.reportBranch && shown.reportBranch !== branch ? ` on ${shown.reportBranch}` : ''}</span>
        </div>
      )}
    </div>
  );
}

/**
 * The live findings, full width: each one keeps its evidence, the documents it
 * touches and every action (draft, change, work item, issue, dismiss) on
 * screen without wrapping. `runId` narrows them to one past run — by default
 * every live finding shows, since a scoped run never resolved the others.
 */
export function DriftFindings({ repo, branch, runId = 0, onSelectRun }: {
  repo: string | undefined; branch: string;
  runId?: number; onSelectRun?: (id: number) => void;
}) {
  const nav = useNav();
  const navigate = useNavigate();
  const drift = useDrift(repo, branch, runId);
  const dismiss = useDismissFinding(repo, branch);
  const file = useFileFinding(repo, branch);
  const draft = useDraftRequirement(repo, branch);
  const remedy = useRemedyFinding(repo, branch);
  const plan = usePlanDocuments(repo, branch);
  const create = useCreateDocuments(repo, branch);
  const [pickTarget, setPickTarget] = useState<Record<string, string>>({});
  const [plans, setPlans] = useState<Record<string, { rationale: string; documents: PlannedDoc[] }>>({});
  const [drop, setDrop] = useState<Record<string, boolean>>({}); // planned docs the user unchecked
  const [err, setErr] = useState('');

  const data = drift.data;
  if (!data?.enabled) return null;
  const running = data.run?.status === 'running';
  const scoped = runId !== 0 && data.run !== null; // showing ONE past run's findings
  const findings = (data.findings ?? []).filter((f) => f.status !== 'dismissed')
    .sort((a, b) => (SEV[a.severity]?.rank ?? 3) - (SEV[b.severity]?.rank ?? 3));
  const dismissed = (data.findings ?? []).filter((f) => f.status === 'dismissed');
  const targets = data.targets ?? [];

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
      onSuccess: (resp) => navigate(projectPath(repo, '/editor/' + resp.path, resp.branch)),
    });
  };
  const doPlan = (f: DriftFinding) => {
    setErr('');
    plan.mutate(f.fingerprint, {
      onError: (e) => setErr(String((e as Error).message ?? e)),
      onSuccess: (p) => setPlans((m) => ({ ...m, [f.fingerprint]: p })),
    });
  };
  const doCreate = (f: DriftFinding) => {
    setErr('');
    const chosen = (plans[f.fingerprint]?.documents ?? []).filter((d) => !drop[f.fingerprint + d.path]);
    if (!chosen.length) { setErr('nothing selected to create'); return; }
    create.mutate({ fingerprint: f.fingerprint, documents: chosen }, {
      onError: (e) => setErr(String((e as Error).message ?? e)),
      onSuccess: (resp) => {
        setPlans((m) => { const n = { ...m }; delete n[f.fingerprint]; return n; });
        if (resp.failures.length) setErr(resp.failures.join('; '));
      },
    });
  };
  const doDraft = (f: DriftFinding) => {
    setErr('');
    draft.mutate({ fingerprint: f.fingerprint }, {
      onError: (e) => setErr(String((e as Error).message ?? e)),
      onSuccess: (resp) => navigate(projectPath(repo, '/editor/' + resp.path, resp.branch)),
    });
  };

  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      {err && (
        <div style={sx('padding:8px 14px;font-size:11.5px;color:var(--reg);background:var(--reg-bg);border-bottom:1px solid var(--border)')}>{err}</div>
      )}
      {scoped && (
        <div data-drift-scoped style={sx('display:flex;align-items:center;gap:8px;padding:7px 14px;border-bottom:1px solid var(--border);background:var(--surface-2);font-size:11px;color:var(--text-2)')}>
          <span style={sx('flex:1;min-width:0')}>
            Showing what run {data.run!.id} ({driftModeLabel(data.run!.mode)}) found — other live findings are hidden.
          </span>
          <button onClick={() => onSelectRun?.(0)}
            style={sx('height:22px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:10.5px;font-weight:600;cursor:pointer;flex:none')}>
            Show all
          </button>
        </div>
      )}

      {findings.map((f) => {
        const sev = SEV[f.severity] ?? SEV.low;
        const gap = f.docPath === '';
        // WHICH kinds propose a new document is the recipe's declaration, not
        // a property of the finding's shape — never gate this on an empty doc
        // path, drift's `new-requirement` findings name one and are draftable
        // all the same. Falling back to the built-in kinds keeps findings from
        // before recipes were frozen onto runs actionable.
        const declared = kindOf(data.run, f.kind);
        const proposes = declared ? declared.draftable : (gap || f.kind === 'new-requirement');
        // the mutation hooks are shared by every row — isPending alone would
        // paint ALL rows as generating; variables names the one that is
        const drafting = draft.isPending && draft.variables?.fingerprint === f.fingerprint;
        const planning = plan.isPending && plan.variables === f.fingerprint;
        const remedying = remedy.isPending && remedy.variables?.fingerprint === f.fingerprint;
        const creating = create.isPending && create.variables?.fingerprint === f.fingerprint;
        const filing = file.isPending && file.variables?.fingerprint === f.fingerprint;
        const busyAny = draft.isPending || plan.isPending || remedy.isPending || create.isPending || file.isPending;
        const idle = (active: boolean) => (busyAny && !active ? ';opacity:.45;cursor:default' : '');
        return (
          <div key={f.fingerprint} data-drift-finding={f.kind} style={sx('padding:10px 14px;border-bottom:1px solid var(--border)')}>
            <div style={sx('display:flex;align-items:center;gap:7px')}>
              <span style={sx(`flex:none;padding:2px 7px;border-radius:6px;font-size:10px;font-weight:700;background:${sev.bg};color:${sev.fg}`)}>{sev.label}</span>
              {/* a recipe's own kind wears the label the recipe gave it */}
              {kindChip(data.run, f.kind, gap) && (
                <span title={declared?.label} style={sx('flex:none;padding:2px 7px;border-radius:6px;font-size:10px;font-weight:700;background:var(--ai-bg);color:var(--ai)')}>
                  {kindChip(data.run, f.kind, gap)}
                </span>
              )}
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
            <KeepOpen show={drafting || planning || remedying || creating} />
            <div style={sx('display:flex;align-items:center;gap:7px;margin-top:7px;flex-wrap:wrap')}>
              {proposes && !f.draftPath && (
                <button onClick={() => doDraft(f)} disabled={busyAny}
                  title="Reverse-engineer the missing requirement document from the source evidence (AI draft, uncommitted)"
                  style={sx('height:24px;padding:0 10px;border:1px solid var(--ai);border-radius:6px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none' + idle(drafting))}>
                  {drafting ? 'Drafting…' : 'Draft requirement'}
                </button>
              )}
              {(f.documents?.length ?? 0) > 0 ? (
                f.documents.map((d) => (
                  <span key={d.path} onClick={() => nav('/editor/' + d.path)}
                    title={'open ' + d.path}
                    style={sx('font-size:11px;font-weight:600;color:var(--data);cursor:pointer;flex:none')}>
                    ✓ {d.kind.replace('_', ' ')}: {d.path.split('/').pop()}
                  </span>
                ))
              ) : f.remedyPath ? (
                <span onClick={() => nav('/editor/' + f.remedyPath)}
                  style={sx('font-size:11px;font-weight:600;color:var(--data);cursor:pointer')}>
                  ✓ {f.remedyKind.replace('_', ' ')}: {f.remedyPath.split('/').pop()}
                </span>
              ) : (
                <>
                  <button onClick={() => doPlan(f)} disabled={busyAny}
                    title="Propose which documents to create for this finding — the families this workspace has, linked as its model prescribes"
                    style={sx('height:24px;padding:0 10px;border:1px solid var(--ai);border-radius:6px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none' + idle(planning))}>
                    {planning ? 'Planning…' : 'Plan documents'}
                  </button>
                  <button onClick={() => doRemedy(f, 'change')} disabled={busyAny}
                    title="Draft a change record (WHY) in the workspace that drives updating the requirements"
                    style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none' + idle(remedying && remedy.variables?.kind === 'change'))}>
                    {remedying && remedy.variables?.kind === 'change' ? 'Drafting…' : '+ Change'}
                  </button>
                  <button onClick={() => doRemedy(f, 'work_item')} disabled={busyAny}
                    title="Draft a work item (WHEN) in the workspace, linked to the affected document"
                    style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none' + idle(remedying && remedy.variables?.kind === 'work_item'))}>
                    {remedying && remedy.variables?.kind === 'work_item' ? 'Drafting…' : '+ Work item'}
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
                    <button onClick={() => doFile(f)} disabled={busyAny}
                      style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none' + idle(filing))}>
                      {filing ? 'Filing…' : 'File issue'}
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

            {plans[f.fingerprint] && (
              <div data-drift-plan style={sx('margin-top:8px;padding:9px 11px;border:1px solid var(--ai-line, var(--border));border-radius:8px;background:var(--ai-bg)')}>
                <div style={sx('font-size:11.5px;color:var(--text-2)')}>{plans[f.fingerprint].rationale}</div>
                {plans[f.fingerprint].documents.map((d) => {
                  const off = drop[f.fingerprint + d.path];
                  return (
                    <div key={d.path} style={sx('display:flex;align-items:baseline;gap:8px;margin-top:6px;' + (off ? 'opacity:.45' : ''))}>
                      <input type="checkbox" checked={!off}
                        onChange={() => setDrop((m) => ({ ...m, [f.fingerprint + d.path]: !off }))}
                        style={{ flex: 'none', cursor: 'pointer' }} />
                      <span style={sx('flex:none;padding:1px 7px;border-radius:5px;font-size:10px;font-weight:700;background:var(--surface);color:var(--ai)')}>{d.kind.replace('_', ' ')}</span>
                      <span style={sx('font-size:11.5px;font-weight:600;flex:none')}>{d.title}</span>
                      <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap")}>
                        {d.path}{d.field && d.linkTargets?.length ? ` · ${d.field} → ${d.linkTargets.join(', ')}` : ''}
                      </span>
                    </div>
                  );
                })}
                <div style={sx('display:flex;align-items:center;gap:7px;margin-top:8px')}>
                  <button onClick={() => doCreate(f)} disabled={busyAny}
                    style={sx('height:24px;padding:0 11px;border:none;border-radius:6px;background:var(--ai);color:#fff;font-family:inherit;font-size:11px;font-weight:600;cursor:pointer' + idle(creating))}>
                    {creating ? 'Creating…' : `Create ${plans[f.fingerprint].documents.filter((d) => !drop[f.fingerprint + d.path]).length} document(s)`}
                  </button>
                  <button onClick={() => setPlans((m) => { const n = { ...m }; delete n[f.fingerprint]; return n; })}
                    style={sx('height:24px;padding:0 9px;border:none;border-radius:6px;background:none;color:var(--text-3);font-family:inherit;font-size:11px;cursor:pointer')}>
                    discard plan
                  </button>
                </div>
              </div>
            )}
          </div>
        );
      })}

      {findings.length === 0 && (
        <div style={sx('padding:11px 14px;font-size:12px;color:var(--text-3)')}>
          {running ? (
            <>run in progress — findings appear here as each {data.run!.mode === 'gaps' ? 'source' : 'doc'} finishes</>
          ) : !data.run ? (
            'no run yet — start a check above'
          ) : data.run.status === 'ok' ? (
            <><span style={sx('color:var(--data)')}>✓</span> {data.run.mode === 'gaps' ? `no coverage gaps found in ${scoped ? 'that run' : 'the last run'}`
              : data.run.mode === 'extract' ? 'app analysis writes an extracted inventory document, not findings'
              : `no drift found in ${scoped ? 'that run' : 'the last run'}`}</>
          ) : (
            `no findings from ${scoped ? 'that run' : 'the last run'} (${data.run.status})`
          )}
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

/** What the configured run would do, in one line above the start button. */
function summarize(mode: DriftMode, docs: number, srcs: number, focus: string, unitsAreDocs: boolean) {
  const s = (n: number) => (n === 1 ? '' : 's');
  const focused = focus.trim() ? ` · focused on “${focus.trim()}”` : '';
  if (mode === 'extract') return `${srcs} source${s(srcs)} → grouped requirement inventory${focused}`;
  if (unitsAreDocs) return `${docs} doc${s(docs)} in scope · against ${srcs} source${s(srcs)}${focused}`;
  return `${srcs} source${s(srcs)}${focused || ' · uncovered capabilities'}`;
}

/** The kind a run's recipe declared, if it declared this one. */
function kindOf(run: DriftRun | null, kind: string) {
  return (run?.kinds ?? []).find((k) => k.kind === kind);
}

/**
 * The short chip beside a finding's severity. The built-in kinds keep the
 * words people know them by; a project recipe's kind gets its own slug, which
 * is the only name anyone has for it.
 */
function kindChip(run: DriftRun | null, kind: string, gap: boolean) {
  if (kind === 'coverage-gap') return 'gap';
  if (kind === 'new-requirement') return 'new';
  const declared = kindOf(run, kind);
  if (declared) return declared.kind;
  return gap ? 'gap' : '';
}

/** The human name of a run mode — the label the mode segments carry. */
export function driftModeLabel(mode: DriftMode) {
  return mode === 'gaps' ? 'gap analysis' : mode === 'extract' ? 'app analysis'
    : mode === 'drift' ? 'drift check' : mode;
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

/**
 * Unlike a run — which is a server-side worker that outlives the page — the
 * per-finding AI actions are ONE request. Navigating away cancels it and the
 * draft is lost, so say so while it is in flight.
 */
function KeepOpen({ show }: { show: boolean }) {
  if (!show) return null;
  return (
    <div data-keep-open style={sx('font-size:11px;color:var(--text-3);margin-top:7px;line-height:1.5')}>
      The AI is writing this now — keep this page open until it lands (unlike a run, this one is
      not resumable).
    </div>
  );
}
