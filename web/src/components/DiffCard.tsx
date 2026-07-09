import { Fragment, ReactNode } from 'react';
import { sx } from '../lib/sx';
import { DiffFile, PRComment } from '../api/hooks';

export const FILE_META = (path: string) => {
  if (path.startsWith('regulations/')) return { icon: '◈', color: 'var(--reg)' };
  if (path.startsWith('requirements/')) return { icon: '▤', color: 'var(--prod)' };
  if (path.startsWith('data-mappings/')) return { icon: '⇄', color: 'var(--data)' };
  if (path.startsWith('diagrams/') || path.endsWith('.excalidraw')) return { icon: '✎', color: 'var(--ai)' };
  return { icon: '◈', color: 'var(--reg)' };
};

export interface LineReview {
  lineComment: { path: string; line: number; text: string } | null;
  setLineComment: (v: { path: string; line: number; text: string } | null) => void;
  onSubmit: (text: string, path: string, line: number) => void;
}

/**
 * One file's diff as a card: header with counts, unified hunks (or a custom
 * artifact rendering for binary-like files), optional inline line-commenting
 * and attached comments (PR review mode).
 */
export function DiffCard({ file, artifact, review, comments = [] }: {
  file: DiffFile;
  artifact?: ReactNode;   // rendered instead of hunks when file.binaryLike
  review?: LineReview;    // enables click-to-comment on lines
  comments?: PRComment[];
}) {
  const meta = FILE_META(file.path);
  let newLineNo = 0;
  const lc = review?.lineComment;
  return (
    <div id={'file-' + file.path} style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;margin-bottom:16px;background:var(--surface)')}>
      <div style={sx("display:flex;align-items:center;gap:8px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:11.5px")}>
        <span style={{ color: meta.color }}>{meta.icon}</span>{file.path}
        {file.status === 'A' && <span style={sx('color:var(--add);font-size:10px;font-weight:700')}>NEW</span>}
        {file.status === 'D' && <span style={sx('color:var(--del);font-size:10px;font-weight:700')}>DELETED</span>}
        <div style={sx('flex:1')} />
        <span style={sx('color:var(--add);font-size:10.5px')}>+{file.additions}</span>
        <span style={sx('color:var(--del);font-size:10.5px')}>−{file.deletions}</span>
      </div>
      {file.binaryLike ? (
        artifact ?? <div style={sx("padding:14px;color:var(--text-3);font-size:11.5px;font-family:'JetBrains Mono',monospace")}>binary-like file changed</div>
      ) : (
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:12px;line-height:1.85")}>
          {(file.hunks || []).map((h, hi) => (
            <Fragment key={hi}>
              <div style={sx('padding:4px 14px;background:var(--surface-2);color:var(--text-3);border-bottom:1px solid var(--border)')}>{h.header}</div>
              {h.lines.map((ln, li) => {
                if (ln.op !== '-') newLineNo++;
                const lineNo = newLineNo;
                const rowStyle = ln.op === '+' ? 'background:var(--add-bg)' : ln.op === '-' ? 'background:var(--del-bg)' : '';
                const signColor = ln.op === '+' ? 'var(--add)' : ln.op === '-' ? 'var(--del)' : 'var(--text-3)';
                return (
                  <Fragment key={li}>
                    <div
                      className="diff-line"
                      onClick={() => review && ln.op !== '-' && review.setLineComment({ path: file.path, line: lineNo, text: '' })}
                      style={sx('display:flex;' + (review ? 'cursor:pointer;' : '') + rowStyle)}
                      title={review ? 'Click to comment on this line' : undefined}
                    >
                      <span style={{ ...sx('width:26px;flex:none;text-align:center;user-select:none'), color: signColor }}>{ln.op}</span>
                      <span style={{ ...sx('flex:1;white-space:pre-wrap'), color: ln.op === ' ' ? 'var(--text-2)' : 'var(--text)' }}>{ln.text}</span>
                    </div>
                    {review && lc && lc.path === file.path && lc.line === lineNo && ln.op !== '-' && (
                      <div style={sx('display:flex;gap:8px;padding:8px 12px;background:var(--surface-2);border-top:1px solid var(--border);border-bottom:1px solid var(--border)')}>
                        <input
                          autoFocus
                          value={lc.text}
                          onChange={(e) => review.setLineComment({ ...lc, text: e.target.value })}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') review.onSubmit(lc.text, file.path, lineNo);
                            if (e.key === 'Escape') review.setLineComment(null);
                          }}
                          placeholder="Comment on this line… (Enter to post, Esc to cancel)"
                          style={sx('flex:1;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px')}
                        />
                      </div>
                    )}
                  </Fragment>
                );
              })}
            </Fragment>
          ))}
        </div>
      )}
      {comments.length > 0 && (
        <div style={sx('border-top:1px solid var(--border)')}>
          {comments.map((c) => <CommentRow key={c.id} c={c} inline />)}
        </div>
      )}
    </div>
  );
}

export function CommentRow({ c, inline }: { c: PRComment; inline?: boolean }) {
  const initials = c.author.name.split(/[\s._-]+/).slice(0, 2).map((w) => w[0]).join('');
  return (
    <div style={sx('display:flex;gap:10px;padding:12px 14px;' + (inline ? 'background:var(--surface-2)' : 'border:1px solid var(--border);border-radius:10px;margin-bottom:8px'))}>
      <div style={sx('width:24px;height:24px;flex:none;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));color:#fff;display:flex;align-items:center;justify-content:center;font-size:10px;font-weight:600')}>{initials}</div>
      <div style={sx('flex:1')}>
        <div style={sx('font-size:12px')}>
          <b>{c.author.name}</b>
          {c.line ? <span style={sx("color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:10.5px")}> · line {c.line}</span> : null}
          {c.outdated && <span style={sx('margin-left:6px;font-size:10px;color:var(--reg);border:1px solid var(--reg-line);border-radius:4px;padding:0 4px')}>outdated</span>}
        </div>
        <div style={sx('font-size:12.5px;color:var(--text);margin-top:3px;line-height:1.5')}>{c.body}</div>
      </div>
    </div>
  );
}
