import { useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useBranches, useCreatePR, useStatus } from '../api/hooks';
import { CommitDialog } from './CommitDialog';

/**
 * "Open PR" flow: prompts to commit pending changes first (the decided
 * commit model), then creates a branch-based PR against the target.
 */
export function CreatePRDialog({ onClose }: { onClose: () => void }) {
  const nav = useNav();
  const app = useApp();
  const branches = useBranches(app.repoId);
  const status = useStatus(app.repoId, app.branch);
  const createPR = useCreatePR(app.repoId);
  const defaultBranch = branches.data?.find((b) => b.isDefault)?.name || 'main';
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [target, setTarget] = useState(defaultBranch);
  const [error, setError] = useState('');
  const [commitFirst, setCommitFirst] = useState(false);

  const dirty = status.data?.dirty.length ?? 0;
  const sameBranch = app.branch === (target || defaultBranch);

  const submit = async () => {
    setError('');
    try {
      const pr = await createPR.mutateAsync({ title, body, source: app.branch, target: target || defaultBranch });
      onClose();
      nav(`/prs/${pr.number}`);
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  if (commitFirst && status.data) {
    return <CommitDialog status={status.data} onClose={() => setCommitFirst(false)} />;
  }

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:460px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Open pull request</div>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:4px;display:flex;align-items:center;gap:5px")}>
          <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)')}>{target || defaultBranch}</span>←
          <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)')}>{app.branch}</span>
        </div>

        {sameBranch && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            You are on {app.branch} — switch to (or create) a feature branch first.
          </div>
        )}
        {dirty > 0 && !sameBranch && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px;display:flex;align-items:center;gap:8px')}>
            {dirty} uncommitted change{dirty === 1 ? '' : 's'} on {app.branch}.
            <button onClick={() => setCommitFirst(true)} style={sx('height:24px;padding:0 10px;border:1px solid var(--reg-line);border-radius:6px;background:var(--surface);color:var(--reg);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
              Commit them now
            </button>
          </div>
        )}

        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:14px 0 5px')}>Title</label>
        <input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus
          style={sx('width:100%;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px')} />
        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:12px 0 5px')}>Description</label>
        <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={3}
          style={sx('width:100%;padding:9px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12.5px;resize:vertical')} />
        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:12px 0 5px')}>Target branch</label>
        <select value={target} onChange={(e) => setTarget(e.target.value)}
          style={sx("width:100%;height:32px;padding:0 8px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:12px")}>
          {(branches.data || []).filter((b) => b.name !== app.branch).map((b) => <option key={b.name} value={b.name}>{b.name}</option>)}
        </select>

        {error && <div style={sx('margin-top:10px;color:var(--del);font-size:12px')}>{error}</div>}
        <div style={sx('display:flex;gap:8px;justify-content:flex-end;margin-top:16px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Cancel</button>
          <button onClick={submit} disabled={!title.trim() || sameBranch || dirty > 0 || createPR.isPending}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + ((!title.trim() || sameBranch || dirty > 0) ? 'opacity:.5' : ''))}>
            {createPR.isPending ? 'Creating…' : 'Create PR'}
          </button>
        </div>
      </div>
    </div>
  );
}
