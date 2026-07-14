import { useEffect, useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { api } from '../api/client';
import { useToasts } from './Toast';

interface ShareResp { url: string | null; createdAt?: number }

// Share the workspace as an OKF bundle: a zip of the default branch behind
// an unauthenticated URL whose secret token is the only credential — made to
// be pasted into an LLM chat (or fetched by an agent) without a login.
export function ShareDialog({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const toasts = useToasts();
  const [share, setShare] = useState<ShareResp | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const load = async () => {
    try { setShare(await api<ShareResp>(`/api/repos/${app.repoId}/share`)); }
    catch (e) { setError(String((e as Error).message || e)); }
  };
  useEffect(() => { void load(); /* eslint-disable-line react-hooks/exhaustive-deps */ }, []);

  const call = async (method: 'POST' | 'DELETE') => {
    setBusy(true);
    setError('');
    try {
      const res = await api<ShareResp>(`/api/repos/${app.repoId}/share`, { method, body: '{}' });
      setShare(method === 'DELETE' ? { url: null } : res);
    } catch (e) {
      setError(String((e as Error).message || e));
    }
    setBusy(false);
  };

  const link = share?.url ? location.origin + share.url : '';
  const btn = (extra = '') => sx('height:30px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;' + extra);

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div data-testid="share-dialog" onClick={(e) => e.stopPropagation()}
        style={sx('width:480px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Share OKF bundle</div>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:6px;line-height:1.55')}>
          A zip of <b>{app.repoId}</b> at the default branch behind an unauthenticated URL —
          paste it into an LLM chat or hand it to an agent. The secret in the link is the only credential;
          anyone who has it can download the bundle until you revoke it. Drafts and workspace branches are never included.
        </div>
        {share?.url ? (
          <>
            <div style={sx("display:flex;gap:6px;margin-top:14px")}>
              <input readOnly value={link} onFocus={(e) => e.target.select()} data-testid="share-url"
                style={sx("flex:1;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:11px")} />
              <button onClick={() => { void navigator.clipboard.writeText(link); toasts.push({ text: 'Share link copied', kind: 'success' }); }} style={btn()}>Copy</button>
            </div>
            <div style={sx('display:flex;gap:8px;margin-top:12px')}>
              <button disabled={busy} onClick={() => void call('POST')} style={btn()} title="Mint a new token — the old link stops working">↻ Rotate</button>
              <button disabled={busy} onClick={() => void call('DELETE')} style={btn('color:var(--del);border-color:var(--reg-line)')}>Revoke</button>
              <div style={sx('flex:1')} />
              <button onClick={onClose} style={btn()}>Done</button>
            </div>
          </>
        ) : (
          <div style={sx('display:flex;gap:8px;margin-top:16px')}>
            <button disabled={busy || !share} data-testid="share-create" onClick={() => void call('POST')}
              style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
              {busy ? 'Creating…' : 'Create share link'}
            </button>
            <div style={sx('flex:1')} />
            <button onClick={onClose} style={btn()}>Cancel</button>
          </div>
        )}
        {error && <div style={sx('margin-top:10px;color:var(--del);font-size:12px')}>{error}</div>}
      </div>
    </div>
  );
}
