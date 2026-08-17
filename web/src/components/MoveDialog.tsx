import { useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { sx } from '../lib/sx';
import { referencingDocs } from '../lib/refactor';
import { isReservedMd } from '../lib/model';
import { useApp } from '../state/AppContext';
import { useNav } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import { useToasts } from './Toast';

/**
 * Move / rename a whole folder: one server-side `git mv` on the directory,
 * with every reference to any contained file rewritten — including the
 * references between the moved files themselves.
 */
export function FolderMoveDialog({ folder, openPath, onClose }: { folder: string; openPath?: string; onClose: () => void }) {
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const toasts = useToasts();
  const { ensureWritableBranch } = useWorkspace();
  const [to, setTo] = useState(folder);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const nFiles = useMemo(
    () => Object.keys(app.files || {}).filter((p) => p.startsWith(folder + '/')).length,
    [app.files, folder],
  );

  const target = to.trim().replace(/^\/+|\/+$/g, '');
  const valid = !!target && target !== folder && !target.startsWith(folder + '/') && /^[\w.-]+(\/[\w.-]+)*$/.test(target);

  const submit = async () => {
    if (!valid || busy) return;
    setBusy(true);
    setError('');
    try {
      const branch = await ensureWritableBranch();
      // trailing slashes make this a folder move; the server rewrites every
      // reference to the contained files in the same request
      const res = await api<{ moved: number; rewritten: string[] }>(
        `/api/repos/${app.repoId}/move?branch=${encodeURIComponent(branch)}`, {
          method: 'POST',
          body: JSON.stringify({ from: folder + '/', to: target + '/' }),
        });
      const rewritten = res.rewritten?.length ?? 0;
      qc.invalidateQueries();
      toasts.push({
        text: `Moved ${folder}/ → ${target}/ (${res.moved} file${res.moved === 1 ? '' : 's'}` +
          (rewritten ? `, ${rewritten} document${rewritten === 1 ? '' : 's'} rewritten)` : ')'),
        kind: 'success',
      });
      onClose();
      if (openPath?.startsWith(folder + '/')) nav('/editor/' + target + openPath.slice(folder.length));
    } catch (e) {
      setError(String((e as Error).message || e));
      setBusy(false);
    }
  };

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:480px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Move / rename folder</div>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:4px")}>{folder}/ · {nFiles} file{nFiles === 1 ? '' : 's'}</div>

        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:14px 0 5px')}>New folder path</label>
        <input value={to} onChange={(e) => setTo(e.target.value)} autoFocus onKeyDown={(e) => { if (e.key === 'Enter') void submit(); }}
          style={sx("width:100%;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:12px")} />
        <div style={sx('margin-top:14px;font-size:12px;color:var(--text-3)')}>
          Every reference to the moved files — in other documents and between them — is rewritten automatically.
        </div>

        {error && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            {error}
          </div>
        )}

        <div style={sx('display:flex;justify-content:flex-end;gap:8px;margin-top:18px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            Cancel
          </button>
          <button onClick={() => void submit()} disabled={!valid || busy}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (!valid || busy ? 'opacity:.5' : ''))}>
            {busy ? 'Moving…' : 'Move folder'}
          </button>
        </div>
      </div>
    </div>
  );
}

/**
 * Move / rename a document: the file moves via a server-side `git mv` on the
 * writable branch, and the server rewrites every document referencing it
 * (any link style, typed frontmatter included) to the new location. The
 * dialog only previews which documents that will touch.
 */
export function MoveDialog({ path, onClose }: { path: string; onClose: () => void }) {
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const toasts = useToasts();
  const { ensureWritableBranch } = useWorkspace();
  const [to, setTo] = useState(path);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const refs = useMemo(() => (app.files ? referencingDocs(app.files, path) : []), [app.files, path]);

  const target = to.trim().replace(/^\/+/, '');
  // reserved OKF names are generated at commit time — never a valid move
  // source (it would regenerate) or target (it would be overwritten)
  const reserved = isReservedMd(target) || isReservedMd(path);
  const valid = !!target && target !== path && !target.endsWith('/') && !reserved;

  const submit = async () => {
    if (!valid || busy) return;
    setBusy(true);
    setError('');
    try {
      const branch = await ensureWritableBranch();
      // the server rewrites referencing documents in the same request
      const res = await api<{ from: string; to: string; rewritten: string[] }>(
        `/api/repos/${app.repoId}/move?branch=${encodeURIComponent(branch)}`, {
          method: 'POST',
          body: JSON.stringify({ from: path, to: target }),
        });
      const rewritten = res.rewritten?.length ?? 0;
      qc.invalidateQueries();
      toasts.push({
        text: `Moved to ${target}` + (rewritten ? ` — ${rewritten} referencing document${rewritten === 1 ? '' : 's'} updated` : ''),
        kind: 'success',
      });
      onClose();
      nav('/editor/' + target);
    } catch (e) {
      setError(String((e as Error).message || e));
      setBusy(false);
    }
  };

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:480px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Move / rename</div>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:4px")}>{path}</div>

        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:14px 0 5px')}>New path</label>
        <input value={to} onChange={(e) => setTo(e.target.value)} autoFocus onKeyDown={(e) => { if (e.key === 'Enter') void submit(); }}
          style={sx("width:100%;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:12px")} />
        {reserved && (
          <div style={sx('margin-top:8px;font-size:12px;color:var(--del)')}>
            index.md and log.md are reserved — these files are generated automatically (indexes at commit time, the log at bundle export).
          </div>
        )}

        {refs.length > 0 ? (
          <div style={sx('margin-top:14px;font-size:12.5px')}>
            <span>
              <b>{refs.length}</b> referencing document{refs.length === 1 ? '' : 's'} will be updated to the new location
              <span style={sx("display:block;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);margin-top:4px;max-height:90px;overflow-y:auto")}>
                {refs.join('\n')}
              </span>
            </span>
          </div>
        ) : (
          <div style={sx('margin-top:14px;font-size:12px;color:var(--text-3)')}>No other document references this file.</div>
        )}

        {error && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            {error}
          </div>
        )}

        <div style={sx('display:flex;justify-content:flex-end;gap:8px;margin-top:18px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            Cancel
          </button>
          <button onClick={() => void submit()} disabled={!valid || busy}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (!valid || busy ? 'opacity:.5' : ''))}>
            {busy ? 'Moving…' : 'Move'}
          </button>
        </div>
      </div>
    </div>
  );
}
