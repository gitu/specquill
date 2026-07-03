import { useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useFileAtHead, useFileQuery, useStatus, useWorktreeDiff } from '../api/hooks';
import { DiffCard } from './DiffCard';
import { CommitDialog } from './CommitDialog';
import { EXCALIDRAW_CMAP, excalidrawToSvg } from '../lib/model';
import { useState } from 'react';

// before/after sketch preview for uncommitted .excalidraw changes
function WorktreeArtifact({ path }: { path: string }) {
  const app = useApp();
  const before = useFileAtHead(app.repoId, app.branch, path, true);
  const after = useFileQuery(app.repoId, app.branch, path);
  const render = (raw?: string) => {
    if (!raw) return '<div style="padding:20px;color:var(--text-3);font-size:11px;text-align:center">—</div>';
    try { return excalidrawToSvg(JSON.parse(raw), EXCALIDRAW_CMAP); } catch { return '<div style="padding:20px;color:var(--reg)">malformed</div>'; }
  };
  return (
    <div style={sx('display:grid;grid-template-columns:1fr 1fr')}>
      <div style={sx('padding:14px;border-right:1px solid var(--border)')}>
        <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;margin-bottom:8px")}>committed</div>
        <div dangerouslySetInnerHTML={{ __html: render(before.data?.content) }} />
      </div>
      <div style={sx('padding:14px')}>
        <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;margin-bottom:8px")}>uncommitted</div>
        <div dangerouslySetInnerHTML={{ __html: render(after.data?.content) }} />
      </div>
    </div>
  );
}

/** Right-side drawer showing every uncommitted change on the current branch. */
export function WorktreeChangesDrawer({ onClose }: { onClose: () => void }) {
  const nav = useNavigate();
  const app = useApp();
  const diff = useWorktreeDiff(app.repoId, app.branch, true);
  const status = useStatus(app.repoId, app.branch);
  const [commitOpen, setCommitOpen] = useState(false);
  const files = diff.data?.files || [];

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.35);z-index:45;display:flex;justify-content:flex-end')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:640px;max-width:90vw;height:100%;background:var(--bg);border-left:1px solid var(--border);box-shadow:var(--shadow-lg);display:flex;flex-direction:column')}>
        <div style={sx('height:46px;flex:none;display:flex;align-items:center;gap:10px;padding:0 16px;background:var(--surface);border-bottom:1px solid var(--border)')}>
          <span style={sx('font-weight:700;font-size:13.5px')}>Uncommitted changes</span>
          <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:11px;color:var(--text-3)")}>on {app.branch}</span>
          <div style={sx('flex:1')} />
          {files.length > 0 && status.data && (
            <button onClick={() => setCommitOpen(true)}
              style={sx('height:28px;padding:0 13px;border:none;border-radius:7px;background:var(--data);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
              Commit {files.length} file{files.length === 1 ? '' : 's'}
            </button>
          )}
          <span onClick={onClose} style={sx('cursor:pointer;color:var(--text-3);font-size:16px')}>×</span>
        </div>
        <div style={sx('flex:1;overflow-y:auto;padding:16px')}>
          {files.map((f) => (
            <div key={f.path}>
              <DiffCard file={f} artifact={f.binaryLike ? <WorktreeArtifact path={f.path} /> : undefined} />
              <div style={sx('margin:-10px 0 16px;display:flex;justify-content:flex-end')}>
                <span onClick={() => { onClose(); nav('/editor/' + f.path); }}
                  style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>
                  Open in editor →
                </span>
              </div>
            </div>
          ))}
          {files.length === 0 && !diff.isLoading && (
            <div style={sx("padding:32px;text-align:center;color:var(--text-3);font-family:'IBM Plex Mono',monospace;font-size:12px")}>
              working tree clean — nothing to commit
            </div>
          )}
        </div>
        {commitOpen && status.data && <CommitDialog status={status.data} onClose={() => setCommitOpen(false)} />}
      </div>
    </div>
  );
}
