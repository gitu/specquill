import { useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { ProposeResp } from '../api/hooks';
import { useBranches, useForgeRequest, useMergePreview, usePropose, useStatus } from '../api/hooks';
import { CommitDialog } from './CommitDialog';
import { forgeRef, forgeTerms } from '../lib/forge';

/**
 * Forge-mode counterpart of MergeDialog: main only moves on the forge, so
 * "landing" work means pushing the workspace branch (with the user's own
 * token) and opening a merge request / pull request there. Re-proposing an
 * already-open branch just pushes the new commits and links the existing MR.
 */
export function ProposeDialog({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const branches = useBranches(app.repoId);
  const status = useStatus(app.repoId, app.branch);
  const defaultBranch = branches.data?.find((b) => b.isDefault)?.name || 'main';
  const sameBranch = app.branch === defaultBranch;
  const preview = useMergePreview(app.repoId, sameBranch ? undefined : app.branch, defaultBranch);
  const existing = useForgeRequest(app.repoId, sameBranch ? undefined : app.branch);
  const propose = usePropose(app.repoId);

  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [error, setError] = useState('');
  const [commitFirst, setCommitFirst] = useState(false);
  const [result, setResult] = useState<ProposeResp | null>(null);

  // GitLab and GitHub name this object differently — say it the host's way
  const kind = result?.kind ?? existing.data?.kind;
  const terms = forgeTerms(kind);

  const dirty = status.data?.dirty.length ?? 0;
  const files = preview.data?.files ?? [];
  const open = existing.data?.request && existing.data.request.state !== 'closed' ? existing.data.request : null;
  const blocked = sameBranch || dirty > 0 || propose.isPending;

  const submit = async () => {
    setError('');
    try {
      const res = await propose.mutateAsync({
        source: app.branch,
        title: title.trim() || undefined,
        body: body.trim() || undefined,
      });
      setResult(res);
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  if (commitFirst && status.data) {
    return <CommitDialog status={status.data} onClose={() => setCommitFirst(false)} />;
  }

  const chip = 'padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)';
  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:460px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>Propose changes</div>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:4px;display:flex;align-items:center;gap:5px")}>
          <span style={sx(chip)}>{defaultBranch}</span>←
          <span style={sx(chip)}>{app.branch}</span>
        </div>

        {result ? (
          <>
            <div style={sx('margin-top:14px;padding:11px 12px;border:1px solid var(--border);background:var(--surface-2);border-radius:9px;font-size:12.5px;line-height:1.5')}>
              {result.created ? `${terms.Noun} opened` : `Branch pushed — ${terms.noun} already open`}:{' '}
              <a href={result.url} target="_blank" rel="noreferrer" style={sx('color:var(--brand,var(--text));font-weight:600')}>
                {forgeRef(kind, result.number)} ↗
              </a>
              <br />Review and merge happen there; the merged result arrives here on the next sync.
            </div>
            <div style={sx('display:flex;gap:8px;justify-content:flex-end;margin-top:16px')}>
              <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Close</button>
            </div>
          </>
        ) : (
          <>
            {sameBranch && (
              <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
                You are on {app.branch} — switch to (or create) a workspace branch first.
              </div>
            )}
            {dirty > 0 && !sameBranch && (
              <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px;display:flex;align-items:center;gap:8px')}>
                {dirty} uncommitted change{dirty === 1 ? '' : 's'} on {app.branch} — only commits travel.
                <button onClick={() => setCommitFirst(true)} style={sx('height:24px;padding:0 10px;border:1px solid var(--reg-line);border-radius:6px;background:var(--surface);color:var(--reg);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
                  Commit them now
                </button>
              </div>
            )}
            {open && (
              <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--border);background:var(--surface-2);border-radius:8px;font-size:12px')}>
                This branch already has an open request:{' '}
                <a href={open.url} target="_blank" rel="noreferrer" style={sx('color:var(--brand,var(--text));font-weight:600')}>{forgeRef(kind, open.number)} {open.title} ↗</a>
                <br />Proposing again pushes your new commits onto it.
              </div>
            )}

            {!sameBranch && (
              <div style={sx('margin-top:14px;border:1px solid var(--border);border-radius:9px;overflow:hidden')}>
                <div style={sx('padding:7px 11px;background:var(--surface-2);border-bottom:1px solid var(--border);font-size:11px;font-weight:600;color:var(--text-3)')}>
                  {preview.isPending ? 'Checking…' : files.length === 0 ? 'Nothing to propose' : `${files.length} file${files.length === 1 ? '' : 's'} in the proposal`}
                </div>
                {files.slice(0, 6).map((f) => (
                  <div key={f.path} style={sx("display:flex;align-items:center;gap:8px;padding:6px 11px;font-family:'JetBrains Mono',monospace;font-size:11.5px;border-bottom:1px solid var(--border)")}>
                    <span style={sx('flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.path}</span>
                    <span style={sx('color:var(--data)')}>+{f.additions}</span>
                    <span style={sx('color:var(--del)')}>−{f.deletions}</span>
                  </div>
                ))}
                {files.length > 6 && (
                  <div style={sx('padding:6px 11px;font-size:11.5px;color:var(--text-3)')}>+{files.length - 6} more</div>
                )}
              </div>
            )}

            {!open && (
              <>
                <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:14px 0 5px')}>Title</label>
                <input value={title} onChange={(e) => setTitle(e.target.value)}
                  placeholder={`Merge ${app.branch} into ${defaultBranch}`}
                  style={sx('width:100%;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px')} />
                <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:12px 0 5px')}>Description (optional)</label>
                <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={3}
                  style={sx('width:100%;padding:8px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;resize:vertical')} />
              </>
            )}

            {error && <div style={sx('margin-top:10px;color:var(--del);font-size:12px')}>{error}</div>}
            <div style={sx('display:flex;gap:8px;justify-content:flex-end;margin-top:16px')}>
              <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Cancel</button>
              <button onClick={submit} disabled={blocked}
                style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (blocked ? 'opacity:.5' : ''))}>
                {propose.isPending ? 'Proposing…' : open ? 'Push to ' + forgeRef(kind, open.number) : 'Propose'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
