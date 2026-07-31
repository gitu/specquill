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
 * Delete a file from the branch worktree (an uncommitted change the user
 * reviews like any other — Discard brings the file back). Unlike a move,
 * deletion does NOT touch inbound references: the dialog warns with the
 * referencing documents so "Move instead" is a conscious choice.
 */
export function DeleteDialog({ path, openPath, onClose }: { path: string; openPath?: string; onClose: () => void }) {
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const toasts = useToasts();
  const { ensureWritableBranch } = useWorkspace();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const refs = useMemo(() => (app.files ? referencingDocs(app.files, path) : []), [app.files, path]);
  const reserved = isReservedMd(path);

  const submit = async () => {
    if (reserved || busy) return;
    setBusy(true);
    setError('');
    try {
      const branch = await ensureWritableBranch();
      await api(`/api/repos/${app.repoId}/files/${path}?branch=${encodeURIComponent(branch)}`, { method: 'DELETE' });
      qc.invalidateQueries();
      toasts.push({ text: `Deleted ${path} (uncommitted — Discard restores it)`, kind: 'success' });
      onClose();
      if (openPath === path) nav('/editor');
    } catch (e) {
      setError(String((e as Error).message || e));
      setBusy(false);
    }
  };

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:480px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Delete file</div>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:4px")}>{path}</div>

        {reserved ? (
          <div style={sx('margin-top:14px;font-size:12px;color:var(--del)')}>
            index.md and log.md are reserved — these files are generated automatically and cannot be deleted.
          </div>
        ) : refs.length > 0 ? (
          <div style={sx('margin-top:14px;padding:10px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;font-size:12.5px;color:var(--reg)')}>
            <b>{refs.length}</b> document{refs.length === 1 ? '' : 's'} still reference{refs.length === 1 ? 's' : ''} this file —
            those links will break. Consider Move instead, which rewrites them.
            <span style={sx("display:block;font-family:'JetBrains Mono',monospace;font-size:10.5px;margin-top:4px;max-height:90px;overflow-y:auto")}>
              {refs.join('\n')}
            </span>
          </div>
        ) : (
          <div style={sx('margin-top:14px;font-size:12px;color:var(--text-3)')}>
            No other document references this file. The deletion is an uncommitted change — Discard restores it.
          </div>
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
          <button onClick={() => void submit()} disabled={reserved || busy}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--del);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (reserved || busy ? 'opacity:.5' : ''))}>
            {busy ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </div>
  );
}
