import { useMemo } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useFileAtHead, useFileQuery } from '../api/hooks';
import type { DiffFile } from '../api/hooks';
import { rawUrl } from '../api/client';
import { DiffCard } from './DiffCard';
import { EXCALIDRAW_CMAP, excalidrawToSvg } from '../lib/model';

const IMG_STYLE = 'max-width:100%;border:1px solid var(--border);border-radius:8px;background:var(--surface);padding:6px';
const EMPTY = "<div style=\"padding:20px;color:var(--text-3);font-size:11px;text-align:center\">—</div>";

// before/after preview for binary-like changes: PNG sketches (and plain
// images) render as images — the committed side reads the HEAD blob via
// at=head, the uncommitted side the worktree state. Legacy .excalidraw JSON
// keeps the themed SVG shim. Renames read the committed side from the OLD
// path — that is where HEAD still has the blob.
export function WorktreeArtifact({ path, oldPath, status }: { path: string; oldPath?: string; status: string }) {
  const app = useApp();
  const beforePath = oldPath || path;
  const legacyJson = /\.excalidraw$/i.test(path);
  const before = useFileAtHead(legacyJson ? app.repoId : undefined, app.branch, beforePath, true);
  const after = useFileQuery(legacyJson ? app.repoId : undefined, app.branch, path);
  // bust the raw endpoint's short cache per mount AND whenever sketch bytes
  // change while the list is open (speccy draw + pixel upgrade)
  const v = useMemo(() => Date.now().toString(36) + '-' + app.sketchGen, [app.sketchGen]);
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
            src={rawUrl(app.repoId!, app.branch, right ? path : beforePath) + (right ? '' : '&at=head') + '&v=' + v}
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

/**
 * The uncommitted diff as cards, with per-file discard/open actions. Shared
 * by the header drawer and the pending-changes view so both render exactly
 * the same thing — the artifact previewer included.
 */
export function WorktreeDiffList({ files, onDiscard, onNavigate, emptyText }: {
  files: DiffFile[];
  onDiscard?: (paths: string[]) => void;
  onNavigate?: () => void;              // e.g. close the drawer before routing
  emptyText?: string;
}) {
  const nav = useNav();
  if (!files.length) {
    return (
      <div style={sx("padding:32px;text-align:center;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>
        {emptyText || 'working tree clean — nothing to commit'}
      </div>
    );
  }
  return (
    <>
      {files.map((f) => (
        <div key={f.path}>
          <DiffCard file={f} artifact={f.binaryLike ? <WorktreeArtifact path={f.path} oldPath={f.oldPath} status={f.status} /> : undefined} />
          <div style={sx('margin:-10px 0 16px;display:flex;justify-content:flex-end;gap:14px')}>
            {onDiscard && (
              <span onClick={() => onDiscard(f.oldPath ? [f.path, f.oldPath] : [f.path])}
                style={sx('font-size:11.5px;color:var(--del);cursor:pointer;font-weight:600')}>
                Discard
              </span>
            )}
            <span onClick={() => { onNavigate?.(); nav('/editor/' + f.path); }}
              style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>
              Open in editor →
            </span>
          </div>
        </div>
      ))}
    </>
  );
}
