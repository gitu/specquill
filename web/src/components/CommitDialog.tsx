import { useEffect, useRef, useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useCommit, usePresence, StatusResp } from '../api/hooks';
import { useCopilotInfo } from '../api/copilot';
import { api } from '../api/client';
import { flushAllDrafts } from '../lib/draftRegistry';

const STATE_COLOR: Record<string, string> = { A: 'var(--add)', D: 'var(--del)', M: 'var(--reg)', R: 'var(--prod)' };

export function CommitDialog({ status, onClose }: { status: StatusResp; onClose: () => void }) {
  const app = useApp();
  const commit = useCommit(app.repoId, app.branch);
  const [message, setMessage] = useState('');
  // orphaned rooms hold co-editing changes that never reached the worktree —
  // this commit would not include them
  const presence = usePresence(app.repoId);
  const orphaned = (presence.data || [])
    .filter((r) => r.branch === app.branch && r.orphaned)
    .map((r) => r.path);

  // AI-drafted message (quick one-shot tier): prefill unless the user typed
  const copilot = useCopilotInfo();
  const [drafting, setDrafting] = useState(false);
  const touched = useRef(false);
  const suggest = async (force = false) => {
    setDrafting(true);
    try {
      const res = await api<{ message: string }>(
        `/api/repos/${app.repoId}/commit-message?branch=${encodeURIComponent(app.branch)}`,
        { method: 'POST', body: '{}' },
      );
      if (res.message && (force || !touched.current)) setMessage(res.message);
    } catch { /* drafting is best-effort — the user just types instead */ }
    setDrafting(false);
  };
  useEffect(() => {
    if (copilot.data?.enabled) void suggest();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [copilot.data?.enabled]);

  const doCommit = async () => {
    if (!message.trim()) return;
    // include keystrokes still sitting in editor debounce windows
    await flushAllDrafts();
    await commit.mutateAsync({ message });
    onClose();
  };

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:440px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Commit changes</div>
        <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:11px;color:var(--text-3);margin-top:3px")}>
          {app.branch} · {status.dirty.length} file{status.dirty.length === 1 ? '' : 's'}
        </div>
        <div style={sx('margin:14px 0;max-height:180px;overflow-y:auto;border:1px solid var(--border);border-radius:9px')}>
          {status.dirty.map((f) => (
            <div key={f.path} style={sx('display:flex;align-items:center;gap:8px;padding:7px 11px;border-bottom:1px solid var(--border);font-size:12px')}>
              <span style={{ ...sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;font-weight:700;width:12px"), color: STATE_COLOR[f.state] || 'var(--text-2)' }}>{f.state}</span>
              <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:11.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap")}>{f.path}</span>
            </div>
          ))}
        </div>
        {orphaned.length > 0 && (
          <div style={sx('margin:0 0 12px;padding:8px 11px;border:1px solid var(--reg);border-radius:9px;font-size:12px;color:var(--text-2);background:color-mix(in srgb, var(--reg) 8%, transparent)')}>
            <b>Unsaved co-editing changes</b> on {orphaned.map((p) => p.split('/').pop()).join(', ')} are not
            part of this commit — open the file{orphaned.length === 1 ? '' : 's'} first to recover them.
          </div>
        )}
        <textarea
          value={message}
          onChange={(e) => { touched.current = true; setMessage(e.target.value); }}
          placeholder={drafting ? 'Drafting a message…' : 'Commit message…'}
          autoFocus
          rows={3}
          style={sx('width:100%;padding:9px 11px;border:1px solid var(--border-2);border-radius:9px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12.5px;resize:vertical')}
        />
        {copilot.data?.enabled && (
          <div style={sx('display:flex;align-items:center;gap:8px;margin-top:5px;font-size:11px;color:var(--text-3)')}>
            <span>{drafting ? 'drafting…' : message && !touched.current ? 'drafted by AI — edit freely' : ''}</span>
            <div style={sx('flex:1')} />
            <button onClick={() => void suggest(true)} disabled={drafting}
              style={sx('height:22px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px;cursor:pointer')}>
              ↻ Suggest
            </button>
          </div>
        )}
        {commit.error != null && <div style={sx('margin-top:8px;color:var(--del);font-size:12px')}>{String((commit.error as Error).message)}</div>}
        <div style={sx('display:flex;gap:8px;justify-content:flex-end;margin-top:14px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Cancel</button>
          <button onClick={doCommit} disabled={!message.trim() || commit.isPending}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            {commit.isPending ? 'Committing…' : 'Commit'}
          </button>
        </div>
      </div>
    </div>
  );
}
