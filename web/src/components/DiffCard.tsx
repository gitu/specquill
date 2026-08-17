import { Fragment, ReactNode } from 'react';
import { sx } from '../lib/sx';
import { DiffFile } from '../api/hooks';

export const FILE_META = (path: string) => {
  if (path.startsWith('regulations/')) return { icon: '◈', color: 'var(--reg)' };
  if (path.startsWith('requirements/')) return { icon: '▤', color: 'var(--prod)' };
  if (path.startsWith('data-mappings/')) return { icon: '⇄', color: 'var(--data)' };
  if (path.startsWith('diagrams/') || path.endsWith('.excalidraw') || path.endsWith('.excalidraw.png')) return { icon: '✎', color: 'var(--ai)' };
  return { icon: '◈', color: 'var(--reg)' };
};

/**
 * One file's diff as a card: header with counts and unified hunks (or a
 * custom artifact rendering for binary-like files).
 */
export function DiffCard({ file, artifact }: {
  file: DiffFile;
  artifact?: ReactNode;   // rendered instead of hunks when file.binaryLike
}) {
  const meta = FILE_META(file.path);
  let newLineNo = 0;
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
                const rowStyle = ln.op === '+' ? 'background:var(--add-bg)' : ln.op === '-' ? 'background:var(--del-bg)' : '';
                const signColor = ln.op === '+' ? 'var(--add)' : ln.op === '-' ? 'var(--del)' : 'var(--text-3)';
                return (
                  <div key={li} className="diff-line" style={sx('display:flex;' + rowStyle)}>
                    <span style={{ ...sx('width:26px;flex:none;text-align:center;user-select:none'), color: signColor }}>{ln.op}</span>
                    <span style={{ ...sx('flex:1;white-space:pre-wrap'), color: ln.op === ' ' ? 'var(--text-2)' : 'var(--text)' }}>{ln.text}</span>
                  </div>
                );
              })}
            </Fragment>
          ))}
        </div>
      )}
    </div>
  );
}
