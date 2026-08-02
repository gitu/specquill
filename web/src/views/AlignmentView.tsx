import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { localizeFeed } from '../lib/feed';
import { useApp } from '../state/AppContext';
import { projectPath } from '../state/nav';
import { DriftRunSummary, useDrift } from '../api/hooks';
import { DriftControls, DriftFindings, driftModeLabel } from '../components/DriftCard';

/**
 * Source alignment as its own page: the run controls stay compact at the top,
 * and the panel below spans the FULL width — findings carry their evidence,
 * paths and actions without wrapping, and the run activity gets the same room
 * when you switch to it.
 */
export function AlignmentView() {
  const app = useApp();
  const navigate = useNavigate();
  // 0 = the newest run, and keep following it as new ones start
  const [runId, setRunId] = useState(0);
  const drift = useDrift(app.repoId, app.branch, runId);
  const [tab, setTab] = useState<'findings' | 'activity'>('findings');
  const run = drift.data?.run;
  const running = run?.status === 'running';
  const open = (drift.data?.findings ?? []).filter((f) => f.status !== 'dismissed').length;
  const lines = run?.activity?.length ?? 0;
  const runs = drift.data?.runs ?? [];
  const newest = runs[0]?.id ?? 0;

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1020px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
        <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Source alignment</h1>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:6px;line-height:1.5')}>
          Drift checks and gap analysis against the reference sources, with work-item filing.
          Reports are living documents in the repository — commit them with your work.
        </div>

        {/* controls + last-run meta: compact, side by side */}
        <div style={sx('display:grid;grid-template-columns:1.65fr 1fr;gap:18px;margin-top:22px;align-items:start')}>
          <DriftControls repo={app.repoId} branch={app.branch} runId={runId} onSelectRun={setRunId} />
          {run && (
            <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
              <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
                {runId === 0 || run.id === newest ? 'Last run' : 'Run ' + run.id}
              </div>
              {/* every run of this branch stays reachable; picking the newest
                  goes back to following it as further runs start */}
              <div style={sx('padding:9px 14px 0')}>
                <select data-drift-run-picker value={run.id}
                  onChange={(e) => setRunId(Number(e.target.value) === newest ? 0 : Number(e.target.value))}
                  style={sx("width:100%;height:26px;padding:0 6px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:10.5px")}>
                  {runs.map((r) => <option key={r.id} value={r.id}>{runOption(r, r.id === newest)}</option>)}
                  {!runs.some((r) => r.id === run.id) && <option value={run.id}>{'#' + run.id}</option>}
                </select>
              </div>
              <div style={sx('padding:11px 14px;font-size:11.5px;color:var(--text-2);display:flex;flex-direction:column;gap:5px')}>
                <Meta k="recipe" v={run.recipeName || driftModeLabel(run.mode)} />
                <Meta k="status" v={running ? `running · ${run.docsDone}/${run.docsTotal}` : run.status} />
                <Meta k="started" v={new Date(run.startedAt * 1000).toLocaleString()} />
                <Meta k="scope" v={`${run.scope?.length ?? 0} ${run.mode === 'drift'
                  ? (run.scope?.length === 1 ? 'doc' : 'docs')
                  : (run.scope?.length === 1 ? 'source' : 'sources')}`} />
                {run.focus !== '' && <Meta k="focus" v={run.focus} />}
                {run.headSha !== '' && <Meta k="checked at" v={run.headSha.slice(0, 10)} />}
                {run.droppedUnverified > 0 && <Meta k="dropped" v={`${run.droppedUnverified} unverified`} />}
                {run.aiCalls > 0 && <Meta k="model calls" v={String(run.aiCalls)} />}
                {run.resumedFrom > 0 && <Meta k="resumed" v={`picked up run ${run.resumedFrom}`} />}
              </div>
            </div>
          )}
        </div>

        {/* full width: the findings need the room, and so does the log */}
        <div style={sx('display:flex;align-items:center;gap:2px;margin:20px 0 10px')}>
          <Tab label={`Findings${open ? ' · ' + open : ''}`} active={tab === 'findings'} onClick={() => setTab('findings')} />
          <Tab label={`Run activity${lines ? ' · ' + lines : ''}`} active={tab === 'activity'} onClick={() => setTab('activity')} />
          <span style={sx('flex:1')} />
          {run?.reportPath !== undefined && run?.reportPath !== '' && (
            <span onClick={() => navigate(projectPath(app.repoId, '/editor/' + run.reportPath, run.reportBranch || app.branch))}
              style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--prod);cursor:pointer")}>
              ⎙ {run.reportPath}{running ? ' — updating live' : ''}
            </span>
          )}
        </div>

        {tab === 'findings' ? (
          <DriftFindings repo={app.repoId} branch={app.branch} runId={runId} onSelectRun={setRunId} />
        ) : (
          <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
            {lines === 0 ? (
              <div style={sx('padding:11px 14px;font-size:12px;color:var(--text-3)')}>no run yet</div>
            ) : (
              // stored in UTC (the feed lands in the report doc), read here in
              // the viewer's own clock
              <div style={sx("padding:12px 16px;font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2);line-height:1.7;white-space:pre-wrap")}>
                {localizeFeed(run!.activity, run!.startedAt).join('\n')}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** One line of the run picker: when it ran, what it did and what it left. */
function runOption(r: DriftRunSummary, newest: boolean) {
  const when = new Date(r.startedAt * 1000).toLocaleString([], {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
  const parts = [`#${r.id}`, when, driftModeLabel(r.mode), r.status];
  if (r.focus) parts.push('“' + r.focus + '”');
  if (r.findings) parts.push(`${r.findings} finding${r.findings === 1 ? '' : 's'}`);
  if (newest) parts.push('newest');
  return parts.join(' · ');
}

function Tab({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick}
      style={sx('height:28px;padding:0 13px;border:1px solid ' + (active ? 'var(--border)' : 'transparent') +
        ';border-bottom-color:' + (active ? 'var(--surface)' : 'var(--border)') +
        ';border-radius:8px 8px 0 0;background:' + (active ? 'var(--surface)' : 'transparent') +
        ';color:' + (active ? 'var(--text)' : 'var(--text-3)') +
        ';font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;margin-bottom:-1px')}>
      {label}
    </button>
  );
}

function Meta({ k, v }: { k: string; v: string }) {
  return (
    <div style={sx('display:flex;gap:10px')}>
      <span style={sx("width:74px;flex:none;font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);text-transform:uppercase;letter-spacing:.3px;padding-top:1px")}>{k}</span>
      <span style={sx('flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis')}>{v}</span>
    </div>
  );
}
