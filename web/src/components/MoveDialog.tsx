import { useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { sx } from '../lib/sx';
import { referencingDocs, rewriteRefs } from '../lib/refactor';
import { useApp } from '../state/AppContext';
import { useNav } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import { useToasts } from './Toast';

/**
 * Move / rename a document: the file moves via a server-side `git mv` on the
 * writable branch, and every document referencing it (any link style, typed
 * frontmatter included) is offered for rewrite to the new location.
 */
export function MoveDialog({ path, onClose }: { path: string; onClose: () => void }) {
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const toasts = useToasts();
  const { ensureWritableBranch } = useWorkspace();
  const [to, setTo] = useState(path);
  const [updateRefs, setUpdateRefs] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const refs = useMemo(() => (app.files ? referencingDocs(app.files, path) : []), [app.files, path]);

  const target = to.trim().replace(/^\/+/, '');
  const valid = !!target && target !== path && !target.endsWith('/');

  const submit = async () => {
    if (!valid || busy) return;
    setBusy(true);
    setError('');
    try {
      const branch = await ensureWritableBranch();
      await api(`/api/repos/${app.repoId}/move?branch=${encodeURIComponent(branch)}`, {
        method: 'POST',
        body: JSON.stringify({ from: path, to: target }),
      });
      let rewritten = 0;
      if (updateRefs) {
        for (const p of refs) {
          // fresh read: the rewrite must apply to the branch's current content
          const f = await api<{ content: string; sha: string }>(`/api/repos/${app.repoId}/files/${p}?ref=${encodeURIComponent(branch)}`);
          const next = rewriteRefs(p, f.content, path, target);
          if (next != null) {
            await api(`/api/repos/${app.repoId}/files/${p}?branch=${encodeURIComponent(branch)}`, {
              method: 'PUT',
              body: JSON.stringify({ content: next, baseSha: f.sha }),
            });
            rewritten++;
          }
        }
      }
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

        {refs.length > 0 ? (
          <label style={sx('display:flex;align-items:flex-start;gap:8px;margin-top:14px;font-size:12.5px;cursor:pointer')}>
            <input type="checkbox" checked={updateRefs} onChange={(e) => setUpdateRefs(e.target.checked)} style={sx('margin-top:2px')} />
            <span>
              Update <b>{refs.length}</b> referencing document{refs.length === 1 ? '' : 's'} to the new location
              <span style={sx("display:block;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);margin-top:4px;max-height:90px;overflow-y:auto")}>
                {refs.join('\n')}
              </span>
            </span>
          </label>
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
