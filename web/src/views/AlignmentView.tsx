import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { projectPath } from '../state/nav';
import { useDrift } from '../api/hooks';
import { DriftCard } from '../components/DriftCard';
import { LinkerCard } from '../components/LinkerCard';

/**
 * Source alignment as its own page: drift and gap reporting against the
 * reference sources, with the full run activity, the git-native report, and
 * the linker beside it. The Dashboard keeps only a compact summary card.
 */
export function AlignmentView() {
  const app = useApp();
  const navigate = useNavigate();
  const drift = useDrift(app.repoId, app.branch);
  const run = drift.data?.run;
  const running = run?.status === 'running';

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1020px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
        <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Source alignment</h1>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:6px;line-height:1.5')}>
          Drift checks and gap analysis against the reference sources, work-item filing, and link suggestions.
          Reports are living documents in the repository — commit them with your work.
        </div>

        <div style={sx('display:grid;grid-template-columns:1.65fr 1fr;gap:18px;margin-top:22px;align-items:start')}>
          <DriftCard repo={app.repoId} branch={app.branch} />

          <div style={sx('display:flex;flex-direction:column;gap:18px')}>
            {run && (
              <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
                <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
                  Last run
                </div>
                <div style={sx('padding:11px 14px;font-size:11.5px;color:var(--text-2);display:flex;flex-direction:column;gap:5px')}>
                  <Meta k="mode" v={run.mode === 'gaps' ? 'gap analysis' : 'drift check'} />
                  <Meta k="status" v={running ? `running · ${run.docsDone}/${run.docsTotal}` : run.status} />
                  <Meta k="started" v={new Date(run.startedAt * 1000).toLocaleString()} />
                  <Meta k="scope" v={`${run.scope?.length ?? 0} ${run.mode === 'gaps'
                    ? (run.scope?.length === 1 ? 'source' : 'sources')
                    : (run.scope?.length === 1 ? 'doc' : 'docs')}`} />
                  {run.headSha !== '' && <Meta k="checked at" v={run.headSha.slice(0, 10)} />}
                  {run.droppedUnverified > 0 && <Meta k="dropped" v={`${run.droppedUnverified} unverified`} />}
                </div>
                {run.reportPath !== '' && (
                  <div onClick={() => navigate(projectPath(app.repoId, '/editor/' + run.reportPath, run.reportBranch || app.branch))}
                    style={sx("display:flex;align-items:center;gap:6px;padding:8px 14px;border-top:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--prod);cursor:pointer")}>
                    ⎙ {run.reportPath}
                    <span style={sx('color:var(--text-3)')}>{running ? '— updating live' : ''}</span>
                  </div>
                )}
              </div>
            )}

            {(run?.activity?.length ?? 0) > 0 && (
              <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
                <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
                  Run activity
                </div>
                <div style={sx("padding:10px 14px;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-2);line-height:1.7;max-height:300px;overflow-y:auto")}>
                  {run!.activity.map((line, i) => <div key={i}>{line}</div>)}
                </div>
              </div>
            )}

            <LinkerCard repo={app.repoId} branch={app.branch} />
          </div>
        </div>
      </div>
    </div>
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
