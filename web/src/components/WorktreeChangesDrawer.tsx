import { useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useDiscard, useStatus, useWorktreeDiff } from '../api/hooks';
import { CommitDialog } from './CommitDialog';
import { WorktreeDiffList } from './WorktreeDiffList';

/**
 * Right-side drawer showing every uncommitted change on the current branch —
 * the quick path from the header. The full picture (uncommitted + ahead of
 * the default branch + the open merge request) lives in the Changes view,
 * and both render the file list through WorktreeDiffList.
 */
export function WorktreeChangesDrawer({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const diff = useWorktreeDiff(app.repoId, app.branch, true);
  const status = useStatus(app.repoId, app.branch);
  const discard = useDiscard(app.repoId, app.branch);
  const [commitOpen, setCommitOpen] = useState(false);
  const files = diff.data?.files || [];

  const reject = (paths?: string[]) => {
    const what = paths ? `changes to ${paths[0]}` : `all ${files.length} pending change${files.length === 1 ? '' : 's'}`;
    if (!window.confirm(`Discard ${what}? This cannot be undone.`)) return;
    discard.mutate({ paths });
  };

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.35);z-index:45;display:flex;justify-content:flex-end')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:640px;max-width:90vw;height:100%;background:var(--bg);border-left:1px solid var(--border);box-shadow:var(--shadow-lg);display:flex;flex-direction:column')}>
        <div style={sx('height:46px;flex:none;display:flex;align-items:center;gap:10px;padding:0 16px;background:var(--surface);border-bottom:1px solid var(--border)')}>
          <span style={sx('font-weight:700;font-size:13.5px')}>Uncommitted changes</span>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>on {app.branch}</span>
          <div style={sx('flex:1')} />
          {files.length > 0 && (
            <button onClick={() => reject()} disabled={discard.isPending}
              style={sx('height:28px;padding:0 13px;border:1px solid var(--border-2);border-radius:7px;background:transparent;color:var(--del);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
              Discard all
            </button>
          )}
          {files.length > 0 && status.data && (
            <button onClick={() => setCommitOpen(true)}
              style={sx('height:28px;padding:0 13px;border:none;border-radius:7px;background:var(--data);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
              Commit {files.length} file{files.length === 1 ? '' : 's'}
            </button>
          )}
          <span onClick={onClose} style={sx('cursor:pointer;color:var(--text-3);font-size:16px')}>×</span>
        </div>
        <div style={sx('flex:1;overflow-y:auto;padding:16px')}>
          {!diff.isLoading && <WorktreeDiffList files={files} onDiscard={reject} onNavigate={onClose} />}
        </div>
        {commitOpen && status.data && <CommitDialog status={status.data} onClose={() => setCommitOpen(false)} />}
      </div>
    </div>
  );
}
