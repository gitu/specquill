import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useDiscard, useFileAtHead, useFileQuery, useStatus, useWorktreeDiff } from '../api/hooks';
import { rawUrl } from '../api/client';
import { DiffCard } from './DiffCard';
import { CommitDialog } from './CommitDialog';
import { EXCALIDRAW_CMAP, excalidrawToSvg } from '../lib/model';
import { useMemo, useState } from 'react';

const IMG_STYLE = 'max-width:100%;border:1px solid var(--border);border-radius:8px;background:var(--surface);padding:6px';
const EMPTY = "<div style=\"padding:20px;color:var(--text-3);font-size:11px;text-align:center\">—</div>";

// before/after preview for binary-like changes: PNG sketches (and plain
// images) render as images — the committed side reads the HEAD blob via
// at=head, the uncommitted side the worktree state. Legacy .excalidraw JSON
// keeps the themed SVG shim.
function WorktreeArtifact({ path, status }: { path: string; status: string }) {
  const app = useApp();
  const legacyJson = /\.excalidraw$/i.test(path);
  const before = useFileAtHead(legacyJson ? app.repoId : undefined, app.branch, path, true);
  const after = useFileQuery(legacyJson ? app.repoId : undefined, app.branch, path);
  // bust the raw endpoint's short cache once per drawer mount
  const v = useMemo(() => Date.now().toString(36), []);
  const renderJson = (raw?: string) => {
    if (!raw) return EMPTY;
    try { return excalidrawToSvg(JSON.parse(raw), EXCALIDRAW_CMAP); } catch { return '<div style="padding:20px;color:var(--reg)">malformed</div>'; }
  };
  const side = (label: string, right: boolean) => {
    const missing = right ? status === 'D' : status === 'A';
    return (
      <div style={sx('padding:14px;min-width:0;' + (right ? '' : 'border-right:1px solid var(--border)'))}>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;margin-bottom:8px")}>{label}</div>
        {legacyJson ? (
          <div dangerouslySetInnerHTML={{ __html: renderJson(right ? after.data?.content : before.data?.content) }} />
        ) : missing ? (
          <div style={sx('padding:20px;color:var(--text-3);font-size:11px;text-align:center')}>—</div>
        ) : (
          <img
            src={rawUrl(app.repoId!, app.branch, path) + (right ? '' : '&at=head') + '&v=' + v}
            alt={label + ' ' + path}
            style={sx(IMG_STYLE)}
          />
        )}
      </div>
    );
  };
  return (
    <div style={sx('display:grid;grid-template-columns:1fr 1fr')}>
      {side('committed', false)}
      {side('uncommitted', true)}
    </div>
  );
}

/** Right-side drawer showing every uncommitted change on the current branch. */
export function WorktreeChangesDrawer({ onClose }: { onClose: () => void }) {
  const nav = useNav();
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
          {files.map((f) => (
            <div key={f.path}>
              <DiffCard file={f} artifact={f.binaryLike ? <WorktreeArtifact path={f.path} status={f.status} /> : undefined} />
              <div style={sx('margin:-10px 0 16px;display:flex;justify-content:flex-end;gap:14px')}>
                <span onClick={() => reject(f.oldPath ? [f.path, f.oldPath] : [f.path])}
                  style={sx('font-size:11.5px;color:var(--del);cursor:pointer;font-weight:600')}>
                  Discard
                </span>
                <span onClick={() => { onClose(); nav('/editor/' + f.path); }}
                  style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>
                  Open in editor →
                </span>
              </div>
            </div>
          ))}
          {files.length === 0 && !diff.isLoading && (
            <div style={sx("padding:32px;text-align:center;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>
              working tree clean — nothing to commit
            </div>
          )}
        </div>
        {commitOpen && status.data && <CommitDialog status={status.data} onClose={() => setCommitOpen(false)} />}
      </div>
    </div>
  );
}
