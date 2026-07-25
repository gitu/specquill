import { useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useBranches, useMerge, useMergePreview, useStatus } from '../api/hooks';
import { CommitDialog } from './CommitDialog';

/**
 * "Merge to main" flow: prompts to commit pending changes first (the decided
 * commit model), previews exactly what would land, then merges the branch
 * into the target. There is no review step — for a reviewed merge, push the
 * branch and open a merge request on the forge instead.
 */
export function MergeDialog({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const branches = useBranches(app.repoId);
  const status = useStatus(app.repoId, app.branch);
  const defaultBranch = branches.data?.find((b) => b.isDefault)?.name || 'main';
  const [target, setTarget] = useState(defaultBranch);
  const [strategy, setStrategy] = useState('merge');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [commitFirst, setCommitFirst] = useState(false);

  const sameBranch = app.branch === (target || defaultBranch);
  const preview = useMergePreview(app.repoId, sameBranch ? undefined : app.branch, target || defaultBranch);
  const merge = useMerge(app.repoId);

  const dirty = status.data?.dirty.length ?? 0;
  const files = preview.data?.files ?? [];
  const conflicts = preview.data?.conflicts ?? [];
  const blocked = sameBranch || dirty > 0 || conflicts.length > 0 || merge.isPending;

  const submit = async () => {
    setError('');
    try {
      await merge.mutateAsync({
        source: app.branch,
        target: target || defaultBranch,
        strategy,
        message: message.trim() || undefined,
      });
      onClose();
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  if (commitFirst && status.data) {
    return <CommitDialog status={status.data} onClose={() => setCommitFirst(false)} />;
  }

  const chip = 'padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)';
  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:460px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Merge branch</div>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:4px;display:flex;align-items:center;gap:5px")}>
          <span style={sx(chip)}>{target || defaultBranch}</span>←
          <span style={sx(chip)}>{app.branch}</span>
        </div>

        {sameBranch && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            You are on {app.branch} — switch to (or create) a feature branch first.
          </div>
        )}
        {dirty > 0 && !sameBranch && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px;display:flex;align-items:center;gap:8px')}>
            {dirty} uncommitted change{dirty === 1 ? '' : 's'} on {app.branch} — a merge only takes commits.
            <button onClick={() => setCommitFirst(true)} style={sx('height:24px;padding:0 10px;border:1px solid var(--reg-line);border-radius:6px;background:var(--surface);color:var(--reg);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
              Commit them now
            </button>
          </div>
        )}
        {conflicts.length > 0 && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--del);background:var(--reg-bg);border-radius:8px;color:var(--del);font-size:12px')}>
            Conflicts in {conflicts.join(', ')} — pull {target || defaultBranch} into {app.branch} and resolve first.
          </div>
        )}

        {!sameBranch && (
          <div style={sx('margin-top:14px;border:1px solid var(--border);border-radius:9px;overflow:hidden')}>
            <div style={sx("padding:7px 11px;background:var(--surface-2);border-bottom:1px solid var(--border);font-size:11px;font-weight:600;color:var(--text-3)")}>
              {preview.isPending ? 'Checking…' : files.length === 0 ? 'Nothing to merge' : `${files.length} file${files.length === 1 ? '' : 's'} will land`}
            </div>
            {files.slice(0, 6).map((f) => (
              <div key={f.path} style={sx("display:flex;align-items:center;gap:8px;padding:6px 11px;font-family:'JetBrains Mono',monospace;font-size:11.5px;border-bottom:1px solid var(--border)")}>
                <span style={sx('flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.path}</span>
                <span style={sx('color:var(--data)')}>+{f.additions}</span>
                <span style={sx('color:var(--del)')}>−{f.deletions}</span>
              </div>
            ))}
            {files.length > 6 && (
              <div style={sx('padding:6px 11px;font-size:11.5px;color:var(--text-3)')}>+{files.length - 6} more</div>
            )}
          </div>
        )}

        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:14px 0 5px')}>Target branch</label>
        <select value={target} onChange={(e) => setTarget(e.target.value)}
          style={sx("width:100%;height:32px;padding:0 8px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:12px")}>
          {(branches.data || []).filter((b) => b.name !== app.branch).map((b) => <option key={b.name} value={b.name}>{b.name}</option>)}
        </select>
        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:12px 0 5px')}>Strategy</label>
        <select value={strategy} onChange={(e) => setStrategy(e.target.value)}
          style={sx('width:100%;height:32px;padding:0 8px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12.5px')}>
          <option value="merge">Merge commit — keeps the branch history</option>
          <option value="squash">Squash — one commit on {target || defaultBranch}</option>
        </select>
        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:12px 0 5px')}>Message (optional)</label>
        <input value={message} onChange={(e) => setMessage(e.target.value)}
          placeholder={`Merge ${app.branch} into ${target || defaultBranch}`}
          style={sx('width:100%;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px')} />

        {error && <div style={sx('margin-top:10px;color:var(--del);font-size:12px')}>{error}</div>}
        <div style={sx('display:flex;gap:8px;justify-content:flex-end;margin-top:16px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Cancel</button>
          <button onClick={submit} disabled={blocked}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (blocked ? 'opacity:.5' : ''))}>
            {merge.isPending ? 'Merging…' : 'Merge'}
          </button>
        </div>
      </div>
    </div>
  );
}
