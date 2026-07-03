import { Fragment, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useFileQuery, usePR, usePRAction, usePRComments, usePRDiff, usePRs, DiffFile, PRComment } from '../api/hooks';
import { EXCALIDRAW_CMAP, excalidrawToSvg } from '../lib/model';
import { Loading } from './Dashboard';
import { IconSpark } from '../components/icons';
import { DiffCard, CommentRow, FILE_META } from '../components/DiffCard';

const STATE_CHIP: Record<string, string> = {
  open: 'background:var(--data-bg);color:var(--data)',
  merged: 'background:var(--ai-bg);color:var(--ai)',
  closed: 'background:var(--surface-2);color:var(--text-3)',
};

export function PRView() {
  const nav = useNavigate();
  const app = useApp();
  const { n } = useParams();
  const num = Number(n);
  const pr = usePR(app.repoId, num);
  const diff = usePRDiff(app.repoId, num);
  const comments = usePRComments(app.repoId, num);
  const act = usePRAction(app.repoId, num);
  const [strategy, setStrategy] = useState<'merge' | 'squash'>('merge');
  const [generalComment, setGeneralComment] = useState('');
  const [lineComment, setLineComment] = useState<{ path: string; line: number; text: string } | null>(null);
  const [error, setError] = useState('');

  if (!pr.data) return <Loading />;
  const p = pr.data;
  const files = diff.data?.files || [];
  const currentApprovals = p.approvals.filter((a) => a.current);
  const canMerge = p.state === 'open' && p.mergeable !== false;

  const doAction = async (action: 'approve' | 'merge' | 'close', payload?: unknown) => {
    setError('');
    try {
      await act.mutateAsync({ action, payload });
      if (action === 'merge') app.switchBranch(p.target);
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };
  const postComment = async (body: string, path?: string, line?: number) => {
    if (!body.trim()) return;
    await act.mutateAsync({ action: 'comments', payload: { body, path, line } });
    setGeneralComment('');
    setLineComment(null);
  };

  const commentsFor = (path: string): PRComment[] => (comments.data || []).filter((c) => c.filePath === path);
  const generalComments = (comments.data || []).filter((c) => !c.filePath);

  return (
    <div style={sx('flex:1;min-height:0;display:flex;flex-direction:column;background:var(--bg)')}>
      <div style={sx('flex:none;padding:14px 20px;background:var(--surface);border-bottom:1px solid var(--border)')}>
        <div style={sx('display:flex;align-items:center;gap:10px;flex-wrap:wrap')}>
          <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:12px;color:var(--text-3)")}>#{p.number}</span>
          <h1 style={sx('margin:0;font-size:16px;font-weight:700')}>{p.title}</h1>
          <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:3px 9px;border-radius:20px;font-size:11px;font-weight:600;text-transform:capitalize;' + (STATE_CHIP[p.state] || ''))}>{p.state}</span>
          {p.mergeable === false && <span style={sx('font-size:11.5px;color:var(--del);font-weight:600')}>⚠ conflicts: {(p.conflicts || []).join(', ')}</span>}
          <div style={sx('flex:1')} />
          {error && <span style={sx('color:var(--del);font-size:12px')}>{error}</span>}
          {p.state === 'open' && (
            <>
              <button onClick={() => doAction('close')} style={sx('height:32px;padding:0 12px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12.5px;cursor:pointer')}>Close</button>
              <button onClick={() => doAction('approve')} style={sx('height:32px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
                Approve{currentApprovals.length > 0 ? ` (${currentApprovals.length})` : ''}
              </button>
              <select value={strategy} onChange={(e) => setStrategy(e.target.value as 'merge' | 'squash')}
                style={sx('height:32px;padding:0 8px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px')}>
                <option value="merge">merge commit</option>
                <option value="squash">squash</option>
              </select>
              <button
                onClick={() => doAction('merge', { strategy })}
                disabled={!canMerge || act.isPending}
                style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (!canMerge ? 'opacity:.5' : ''))}
              >
                {act.isPending ? 'Working…' : 'Merge'}
              </button>
            </>
          )}
        </div>
        <div style={sx('display:flex;align-items:center;gap:12px;margin-top:11px;font-size:11.5px;color:var(--text-2);flex-wrap:wrap')}>
          <span style={sx("font-family:'IBM Plex Mono',monospace;display:inline-flex;align-items:center;gap:5px")}>
            <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)')}>{p.target}</span>←
            <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)')}>{p.source}</span>
          </span>
          <span>by <b style={sx('color:var(--text)')}>{p.author.name}</b></span>
          {p.approvals.length > 0 && (
            <span style={sx('display:inline-flex;align-items:center;gap:5px')}>
              <span style={sx('width:14px;height:14px;border-radius:50%;background:var(--data);color:#fff;display:flex;align-items:center;justify-content:center;font-size:9px')}>✓</span>
              approved by {p.approvals.map((a) => a.user.name + (a.current ? '' : ' (outdated)')).join(', ')}
            </span>
          )}
          <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;color:var(--text-3)")}>head {p.headSha.slice(0, 8)}</span>
        </div>
      </div>

      <div style={sx('flex:1;min-height:0;display:flex')}>
        <div style={sx('width:224px;flex:none;border-right:1px solid var(--border);background:var(--panel);padding:12px 8px;overflow-y:auto')}>
          <div style={sx('font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;padding:0 8px 8px')}>
            {files.length} file{files.length === 1 ? '' : 's'} changed
          </div>
          {files.map((f) => {
            const meta = FILE_META(f.path);
            return (
              <a key={f.path} href={'#file-' + f.path} style={sx('text-decoration:none;color:inherit')}>
                <div style={sx('display:flex;align-items:center;gap:8px;padding:7px 9px;border-radius:7px;font-size:12px;color:var(--text-2);cursor:pointer')}>
                  <span style={{ color: meta.color }}>{meta.icon}</span>
                  <span style={sx('flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.path.split('/').pop()}</span>
                  {f.additions > 0 && <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;color:var(--add)")}>+{f.additions}</span>}
                  {f.deletions > 0 && <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;color:var(--del)")}>−{f.deletions}</span>}
                </div>
              </a>
            );
          })}
        </div>

        <div style={sx('flex:1;min-width:0;overflow-y:auto;background:var(--surface)')}>
          <div style={sx('max-width:860px;margin:0 auto;padding:18px 22px 60px')}>
            {p.body && <div style={sx('font-size:13px;line-height:1.6;color:var(--text);padding:4px 2px 16px')}>{p.body}</div>}
            {files.map((f) => (
              <DiffCard
                key={f.path}
                file={f}
                artifact={f.binaryLike ? <ArtifactDiff path={f.path} pr={p} /> : undefined}
                comments={commentsFor(f.path)}
                review={p.state === 'open' ? {
                  lineComment,
                  setLineComment,
                  onSubmit: (text, path, line) => void postComment(text, path, line),
                } : undefined}
              />
            ))}
            {files.length === 0 && !diff.isLoading && (
              <div style={sx("padding:24px;text-align:center;color:var(--text-3);font-family:'IBM Plex Mono',monospace;font-size:12px")}>no changes between {p.target} and {p.source}</div>
            )}

            {/* general comments */}
            <div style={sx('margin-top:26px')}>
              <div style={sx('font-weight:700;font-size:13.5px;margin-bottom:10px')}>Conversation</div>
              {generalComments.map((c) => <CommentRow key={c.id} c={c} />)}
              <div style={sx('display:flex;gap:10px;margin-top:12px')}>
                <textarea
                  value={generalComment}
                  onChange={(e) => setGeneralComment(e.target.value)}
                  placeholder="Leave a comment…"
                  rows={2}
                  style={sx('flex:1;padding:9px 11px;border:1px solid var(--border-2);border-radius:9px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12.5px;resize:vertical')}
                />
              </div>
              <div style={sx('display:flex;justify-content:flex-end;margin-top:8px')}>
                <button onClick={() => postComment(generalComment)} disabled={!generalComment.trim()}
                  style={sx('height:30px;padding:0 14px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
                  Comment
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// before/after preview for binary-like artifacts (.excalidraw)
function ArtifactDiff({ path, pr }: { path: string; pr: { source: string; target: string } }) {
  const app = useApp();
  const before = useFileQuery(app.repoId, pr.target, path);
  const after = useFileQuery(app.repoId, pr.source, path);
  const render = (raw?: string) => {
    if (!raw) return '<div style="padding:20px;color:var(--text-3);font-size:11px;text-align:center">—</div>';
    try { return excalidrawToSvg(JSON.parse(raw), EXCALIDRAW_CMAP); } catch { return '<div style="padding:20px;color:var(--reg)">malformed</div>'; }
  };
  return (
    <div style={sx('display:grid;grid-template-columns:1fr 1fr;gap:0')}>
      <div style={sx('padding:14px;border-right:1px solid var(--border)')}>
        <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;margin-bottom:8px")}>before · {pr.target}</div>
        <div dangerouslySetInnerHTML={{ __html: render(before.data?.content) }} />
      </div>
      <div style={sx('padding:14px')}>
        <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;margin-bottom:8px")}>after · {pr.source}</div>
        <div dangerouslySetInnerHTML={{ __html: render(after.data?.content) }} />
      </div>
    </div>
  );
}

export function PRListView() {
  const nav = useNavigate();
  const app = useApp();
  const [state, setState] = useState('open');
  const prs = usePRs(app.repoId, state);
  const seg = (on: boolean) => 'padding:4px 12px;border-radius:6px;font-size:11.5px;cursor:pointer;' + (on ? 'font-weight:600;background:var(--surface);box-shadow:var(--shadow)' : 'color:var(--text-3)');

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:860px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx('display:flex;align-items:center;gap:14px')}>
          <h1 style={sx('margin:0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Pull requests</h1>
          <div style={sx('flex:1')} />
          <div style={sx('display:flex;gap:4px;background:var(--surface-2);border:1px solid var(--border);border-radius:8px;padding:3px')}>
            {['open', 'merged', 'closed', 'all'].map((st) => (
              <span key={st} onClick={() => setState(st)} style={sx(seg(state === st))}>{st}</span>
            ))}
          </div>
        </div>
        <div style={sx('margin-top:18px;background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden')}>
          {(prs.data || []).map((p) => (
            <div key={p.number} onClick={() => nav(`/prs/${p.number}`)} style={sx('display:flex;align-items:center;gap:12px;padding:13px 16px;border-bottom:1px solid var(--border);cursor:pointer')}>
              <span style={sx('display:inline-flex;align-items:center;padding:3px 9px;border-radius:20px;font-size:10.5px;font-weight:600;text-transform:capitalize;' + (STATE_CHIP[p.state] || ''))}>{p.state}</span>
              <div style={sx('flex:1;min-width:0')}>
                <div style={sx('font-weight:600;font-size:13px')}>#{p.number} {p.title}</div>
                <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;color:var(--text-3);margin-top:2px")}>{p.target} ← {p.source} · by {p.author.name}</div>
              </div>
              {p.approvals.some((a) => a.current) && <span title="approved" style={sx('color:var(--data);font-weight:700')}>✓</span>}
              {p.commentCount > 0 && <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:11px;color:var(--text-3)")}>💬 {p.commentCount}</span>}
            </div>
          ))}
          {prs.data?.length === 0 && (
            <div style={sx("padding:28px;text-align:center;color:var(--text-3);font-family:'IBM Plex Mono',monospace;font-size:12px")}>no {state === 'all' ? '' : state + ' '}pull requests</div>
          )}
        </div>
        <div style={sx('margin-top:14px;font-size:12px;color:var(--text-3);display:flex;align-items:center;gap:6px')}>
          <IconSpark size={12} stroke="var(--ai)" /> Create a PR from the current branch with the “Open PR” button in the top bar.
        </div>
      </div>
    </div>
  );
}
