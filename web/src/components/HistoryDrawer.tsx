import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { sx } from '../lib/sx';
import { daysAgo } from '../lib/derive';
import { useApp } from '../state/AppContext';

interface HistoryEntry { sha: string; author: string; email: string; date: string; subject: string }

/**
 * Git history of one document (renames followed server-side via --follow).
 * Selecting a commit shows the document as it was committed then.
 */
export function HistoryDrawer({ path, onClose }: { path: string; onClose: () => void }) {
  const app = useApp();
  const [entries, setEntries] = useState<HistoryEntry[] | null>(null);
  const [error, setError] = useState('');
  const [sel, setSel] = useState<HistoryEntry | null>(null);
  const [content, setContent] = useState<string | null>(null);
  const [contentErr, setContentErr] = useState('');

  useEffect(() => {
    api<HistoryEntry[]>(`/api/repos/${app.repoId}/history?path=${encodeURIComponent(path)}&ref=${encodeURIComponent(app.branch)}`)
      .then(setEntries)
      .catch((e) => setError(String((e as Error).message || e)));
  }, [app.repoId, app.branch, path]);

  useEffect(() => {
    if (!sel) return;
    setContent(null);
    setContentErr('');
    api<{ content: string }>(`/api/repos/${app.repoId}/files/${path}?ref=${encodeURIComponent(sel.sha)}&at=head`)
      .then((f) => setContent(f.content))
      // before a rename the file lived elsewhere — the listing still shows the commit
      .catch(() => setContentErr('not readable at this commit (the file had a different path)'));
  }, [sel, app.repoId, path]);

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;top:46px;z-index:40;background:rgba(10,12,16,.35)')}>
      <div onClick={(e) => e.stopPropagation()}
        style={sx('position:absolute;right:0;top:0;bottom:0;width:' + (sel ? 'min(880px,92vw)' : '400px') + ';background:var(--panel);border-left:1px solid var(--border);box-shadow:var(--shadow-lg);display:flex')}>
        <div style={sx('width:400px;flex:none;display:flex;flex-direction:column;border-right:1px solid var(--border)')}>
          <div style={sx('height:44px;flex:none;display:flex;align-items:center;gap:8px;padding:0 14px;border-bottom:1px solid var(--border)')}>
            <span style={sx('font-weight:700;font-size:13px')}>History</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap")}>{path}</span>
            <div style={sx('flex:1')} />
            <span onClick={onClose} style={sx('cursor:pointer;color:var(--text-3);font-size:15px')}>✕</span>
          </div>
          <div style={sx('flex:1;overflow-y:auto')}>
            {error && <div style={sx('padding:14px;font-size:12px;color:var(--reg)')}>{error}</div>}
            {entries && entries.length === 0 && (
              <div style={sx('padding:14px;font-size:12px;color:var(--text-3)')}>No commits touch this file yet — drafts live in the worktree until committed.</div>
            )}
            {(entries || []).map((e) => (
              <div key={e.sha} onClick={() => setSel(e)}
                style={sx('padding:10px 14px;border-bottom:1px solid var(--border);cursor:pointer;' + (sel?.sha === e.sha ? 'background:var(--surface)' : ''))}>
                <div style={sx('font-size:12.5px;font-weight:600;line-height:1.4')}>{e.subject}</div>
                <div style={sx("display:flex;gap:8px;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);margin-top:3px")}>
                  <span>{e.sha.slice(0, 7)}</span>
                  <span style={sx('color:var(--text-2)')}>{e.author}</span>
                  <span>{daysAgo(e.date.slice(0, 10))}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
        {sel && (
          <div style={sx('flex:1;min-width:0;display:flex;flex-direction:column;background:var(--bg)')}>
            <div style={sx("height:44px;flex:none;display:flex;align-items:center;gap:8px;padding:0 14px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2)")}>
              {path} @ {sel.sha.slice(0, 7)}
              <div style={sx('flex:1')} />
              <span onClick={() => setSel(null)} style={sx('cursor:pointer;color:var(--text-3)')}>close preview</span>
            </div>
            <div style={sx('flex:1;overflow:auto;padding:16px 18px')}>
              {contentErr && <div style={sx('font-size:12px;color:var(--text-3)')}>{contentErr}</div>}
              {content != null && (
                <pre style={sx("margin:0;font-family:'JetBrains Mono',monospace;font-size:12px;line-height:1.6;white-space:pre-wrap;color:var(--text)")}>{content}</pre>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
