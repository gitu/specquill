import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { projectPath } from '../state/nav';
import { useDrift } from '../api/hooks';
import { DriftControls, DriftFindings } from '../components/DriftCard';

/**
 * Source alignment as its own page: the run controls stay compact at the top,
 * and the panel below spans the FULL width — findings carry their evidence,
 * paths and actions without wrapping, and the run activity gets the same room
 * when you switch to it.
 */
export function AlignmentView() {
  const app = useApp();
  const navigate = useNavigate();
  const drift = useDrift(app.repoId, app.branch);
  const [tab, setTab] = useState<'findings' | 'activity'>('findings');
  const run = drift.data?.run;
  const running = run?.status === 'running';
  const open = (drift.data?.findings ?? []).filter((f) => f.status !== 'dismissed').length;
  const lines = run?.activity?.length ?? 0;

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
          <DriftControls repo={app.repoId} branch={app.branch} />
          {run && (
            <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
              <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
                Last run
              </div>
              <div style={sx('padding:11px 14px;font-size:11.5px;color:var(--text-2);display:flex;flex-direction:column;gap:5px')}>
                <Meta k="mode" v={run.mode === 'gaps' ? 'gap analysis' : run.mode === 'extract' ? 'app analysis' : 'drift check'} />
                <Meta k="status" v={running ? `running · ${run.docsDone}/${run.docsTotal}` : run.status} />
                <Meta k="started" v={new Date(run.startedAt * 1000).toLocaleString()} />
                <Meta k="scope" v={`${run.scope?.length ?? 0} ${run.mode === 'drift'
                  ? (run.scope?.length === 1 ? 'doc' : 'docs')
                  : (run.scope?.length === 1 ? 'source' : 'sources')}`} />
                {run.headSha !== '' && <Meta k="checked at" v={run.headSha.slice(0, 10)} />}
                {run.droppedUnverified > 0 && <Meta k="dropped" v={`${run.droppedUnverified} unverified`} />}
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
          <DriftFindings repo={app.repoId} branch={app.branch} />
        ) : (
          <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
            {lines === 0 ? (
              <div style={sx('padding:11px 14px;font-size:12px;color:var(--text-3)')}>no run yet</div>
            ) : (
              <div style={sx("padding:12px 16px;font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2);line-height:1.7;white-space:pre-wrap")}>
                {run!.activity.join('\n')}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
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
