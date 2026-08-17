import { sx } from '../lib/sx';
import { useNav } from '../state/nav';
import { useDrift } from '../api/hooks';

/**
 * Compact Overview card for source alignment — counts and last-run state
 * only; the work happens on the dedicated Alignment page.
 */
export function AlignmentSummary({ repo, branch }: { repo: string | undefined; branch: string }) {
  const nav = useNav();
  const drift = useDrift(repo, branch);
  const data = drift.data;
  if (!data?.enabled) return null;
  const run = data.run;
  const running = run?.status === 'running';
  const open = (data.findings ?? []).filter((f) => f.status !== 'dismissed');
  const gaps = open.filter((f) => f.docPath === '').length;
  const drifts = open.length - gaps;

  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
          Source alignment
        </span>
        <span style={sx('flex:1')} />
        {run && (
          <span title={run.error} style={sx(`font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;flex:none;background:${running ? 'var(--ai-bg)' : run.status === 'ok' ? 'var(--data-bg)' : 'var(--reg-bg)'};color:${running ? 'var(--ai)' : run.status === 'ok' ? 'var(--data)' : 'var(--reg)'}`)}>
            {running ? `running ${run.docsDone}/${run.docsTotal}` : run.status}
          </span>
        )}
      </div>
      <div onClick={() => nav('/alignment')} style={sx('padding:11px 14px;cursor:pointer')}>
        {run === null ? (
          <div style={sx('font-size:12px;color:var(--text-3)')}>never checked — run drift or gap analysis</div>
        ) : open.length === 0 && !running ? (
          <div style={sx('font-size:12px;color:var(--text-3)')}>
            <span style={sx('color:var(--data)')}>✓</span> workspace and sources aligned
          </div>
        ) : (
          <div style={sx('font-size:12.5px;color:var(--text)')}>
            {drifts > 0 && <b>{drifts} drift finding{drifts === 1 ? '' : 's'}</b>}
            {drifts > 0 && gaps > 0 && ' · '}
            {gaps > 0 && <b>{gaps} coverage gap{gaps === 1 ? '' : 's'}</b>}
            {open.length === 0 && running && 'checking…'}
          </div>
        )}
        <div style={sx('font-size:11px;color:var(--prod);font-weight:600;margin-top:6px')}>Open alignment →</div>
      </div>
    </div>
  );
}
