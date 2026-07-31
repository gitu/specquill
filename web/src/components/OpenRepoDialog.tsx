import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError } from '../api/client';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useToasts } from './Toast';
import {
  DynRepo, fmtBytes, openDynamic, reclaimCheckout, searchDynamic,
  useCheckouts, useDynamicInfo,
} from '../api/dynamic';

/**
 * Open a repository as a dynamic project (REQ-025): anything on the
 * deployment's forge the caller's own token reaches, addressed as
 * owner/repo[#name]. Doubles as the checkout overview — every clone the
 * server holds for this user, with reclaim/close (REQ-025.9).
 */
export function OpenRepoDialog({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const qc = useQueryClient();
  const toasts = useToasts();
  const info = useDynamicInfo();
  const checkouts = useCheckouts(!!info.data?.enabled);
  const [spec, setSpec] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [choices, setChoices] = useState<{ name: string; root: string }[]>([]);
  const [results, setResults] = useState<DynRepo[]>([]);
  const [confirmDiscard, setConfirmDiscard] = useState<string | null>(null);

  // debounced as-you-type search (only when the deployment proxies it)
  useEffect(() => {
    if (!info.data?.search) return;
    const q = spec.trim();
    if (!q || q.includes('#')) { setResults([]); return; }
    const t = setTimeout(() => {
      searchDynamic(q).then((r) => setResults(r.repos.slice(0, 8))).catch(() => setResults([]));
    }, 250);
    return () => clearTimeout(t);
  }, [spec, info.data?.search]);

  const open = async (s: string) => {
    if (busy) return;
    setBusy(true);
    setError('');
    setChoices([]);
    const res = await openDynamic(s).catch((e) => ({ kind: 'error' as const, message: String(e) }));
    setBusy(false);
    if (res.kind === 'opened') {
      await qc.invalidateQueries({ queryKey: ['repos'] });
      toasts.push({ text: `Opened ${res.project.spelling}${res.project.readonly ? ' (read-only)' : ''}`, kind: 'success' });
      onClose();
      app.switchProject(res.project.id);
    } else if (res.kind === 'choose') {
      setChoices(res.choices);
    } else {
      setError(res.message);
    }
  };

  const reclaim = async (id: string, force: boolean, close: boolean) => {
    try {
      await reclaimCheckout(id, { force, close });
      setConfirmDiscard(null);
      await qc.invalidateQueries({ queryKey: ['checkouts'] });
      await qc.invalidateQueries({ queryKey: ['repos'] });
    } catch (e) {
      // 409 IS the unsynced guard (REQ-025.5) — key the confirmation off the
      // status, never off the wording of the message
      if (e instanceof ApiError && e.status === 409) setConfirmDiscard(id);
      else toasts.push({ text: String((e as Error).message || e), kind: 'error' });
    }
  };

  const base = spec.split('#')[0];
  const list = checkouts.data?.checkouts || [];
  const used = checkouts.data?.used ?? 0;
  const budget = checkouts.data?.budget ?? info.data?.budget ?? 0;

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:520px;max-height:80vh;overflow-y:auto;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Open repository</div>
        <div style={sx('font-size:12px;color:var(--text-3);margin-top:4px')}>
          Any repository on {info.data?.host || 'the forge'} your token can reach — it must carry a root
          {' '}<code>.specquill/config.yml</code> declaring its workspaces.
        </div>

        <input
          value={spec}
          onChange={(e) => setSpec(e.target.value)}
          placeholder="owner/repo#workspace"
          autoFocus
          onKeyDown={(e) => { if (e.key === 'Enter' && spec.trim()) void open(spec.trim()); if (e.key === 'Escape') onClose(); }}
          style={sx("width:100%;box-sizing:border-box;height:32px;margin-top:14px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:12px")}
        />

        {results.length > 0 && (
          <div style={sx('margin-top:8px;border:1px solid var(--border);border-radius:9px;overflow:hidden')}>
            {results.map((r) => (
              <div key={r.id} onClick={() => { setSpec(r.path); setResults([]); void open(r.path); }}
                style={sx("padding:8px 11px;font-family:'JetBrains Mono',monospace;font-size:12px;cursor:pointer;border-bottom:1px solid var(--border)")}>
                {r.path}
              </div>
            ))}
          </div>
        )}

        {choices.length > 0 && (
          <div style={sx('margin-top:10px')}>
            <div style={sx('font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px')}>This repository declares several workspaces — pick one:</div>
            <div style={sx('border:1px solid var(--border);border-radius:9px;overflow:hidden')}>
              {choices.map((c) => (
                <div key={c.name} onClick={() => void open(base + '#' + c.name)}
                  style={sx('display:flex;justify-content:space-between;padding:8px 11px;font-size:12px;cursor:pointer;border-bottom:1px solid var(--border)')}>
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-weight:600")}>{c.name}</span>
                  <span style={sx('color:var(--text-3)')}>{c.root || '/'}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {error && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            {error}
          </div>
        )}

        {/* checkout overview (REQ-025.9): everything the server holds for you */}
        {list.length > 0 && (
          <div style={sx('margin-top:18px')}>
            <div style={sx('display:flex;align-items:baseline;justify-content:space-between')}>
              <div style={sx('font-size:11.5px;font-weight:600;color:var(--text-2)')}>Your checkouts</div>
              {budget > 0 && (
                <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>
                  {fmtBytes(used)} of {fmtBytes(budget)}
                </div>
              )}
            </div>
            <div style={sx('margin-top:6px;border:1px solid var(--border);border-radius:9px;overflow:hidden')}>
              {list.map((c) => (
                <div key={c.repoId} style={sx('display:flex;align-items:center;gap:8px;padding:8px 11px;border-bottom:1px solid var(--border);font-size:12px')}>
                  <div style={sx('flex:1;min-width:0')}>
                    <span style={sx("font-family:'JetBrains Mono',monospace;font-weight:600")}>{c.spelling || c.repoId}</span>
                    <span style={sx('color:var(--text-3);margin-left:8px;font-size:10.5px')}>
                      {c.kind}{c.unsynced ? ' · unsynced work' : ''}{!c.materialized ? ' · not on disk' : ' · ' + fmtBytes(c.bytes)}
                    </span>
                  </div>
                  {confirmDiscard === c.repoId ? (
                    <button onClick={() => void reclaim(c.repoId, true, c.kind === 'dynamic')}
                      style={sx('height:24px;padding:0 9px;border:none;border-radius:6px;background:var(--del);color:#fff;font-family:inherit;font-size:11px;font-weight:600;cursor:pointer')}>
                      Discard unsynced work
                    </button>
                  ) : (
                    <>
                      {c.materialized && (
                        <button onClick={() => void reclaim(c.repoId, false, false)} title="Free the server-side clone — reopening re-clones"
                          style={sx('height:24px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer')}>
                          Reclaim
                        </button>
                      )}
                      {c.kind === 'dynamic' && (
                        <button onClick={() => void reclaim(c.repoId, false, true)} title="Close the project (keeps nothing server-side)"
                          style={sx('height:24px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer')}>
                          Close
                        </button>
                      )}
                    </>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        <div style={sx('display:flex;justify-content:flex-end;gap:8px;margin-top:18px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            Close
          </button>
          <button onClick={() => void open(spec.trim())} disabled={!spec.trim() || busy}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (!spec.trim() || busy ? 'opacity:.5' : ''))}>
            {busy ? 'Opening…' : 'Open'}
          </button>
        </div>
      </div>
    </div>
  );
}
