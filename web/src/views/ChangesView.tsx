import { useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useBranches, useDiscard, useMergePreview, useStatus, useWorktreeDiff } from '../api/hooks';
import type { DiffFile } from '../api/hooks';
import { classifier } from '../lib/model';
import { pluralLabel, singularLabel } from '../lib/history';
import { WorktreeDiffList } from '../components/WorktreeDiffList';
import { CommitDialog } from '../components/CommitDialog';
import { MergeDialog } from '../components/MergeDialog';
import { ProposeDialog } from '../components/ProposeDialog';
import { ForgeReview } from '../components/ForgeReview';
import { DiffCard } from '../components/DiffCard';
import { Loading } from './Dashboard';

/**
 * Pending changes — everything on this branch that has not landed yet, in
 * the order it travels: uncommitted work in the worktree, then commits ahead
 * of the default branch, then the open merge request on the forge. The
 * committed history is a separate view (/history).
 */
export function ChangesView() {
  const nav = useNav();
  const app = useApp();
  const branches = useBranches(app.repoId);
  const defaultBranch = branches.data?.find((b) => b.isDefault)?.name;
  const onFeature = !!defaultBranch && app.branch !== defaultBranch;
  const diff = useWorktreeDiff(app.repoId, app.branch, true);
  const status = useStatus(app.repoId, app.branch);
  const merge = useMergePreview(app.repoId, onFeature ? app.branch : undefined, defaultBranch);
  const discard = useDiscard(app.repoId, app.branch);
  const [commitOpen, setCommitOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [proposeOpen, setProposeOpen] = useState(false);
  const mergeMode = app.mergeMode;
  if (!app.model) return <Loading />;

  const files = diff.data?.files || [];
  const ahead = merge.data?.files || [];
  const conflicts = merge.data?.conflicts || [];

  const reject = (paths?: string[]) => {
    const what = paths ? `changes to ${paths[0]}` : `all ${files.length} pending change${files.length === 1 ? '' : 's'}`;
    if (!window.confirm(`Discard ${what}? This cannot be undone.`)) return;
    discard.mutate({ paths });
  };

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:900px;margin:0 auto;padding:26px 30px 60px')}>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
        <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Pending changes</h1>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:6px')}>
          What has not landed on {defaultBranch || 'the default branch'} yet. Committed history lives in{' '}
          <span onClick={() => nav('/history')} style={sx('color:var(--prod);cursor:pointer;font-weight:600')}>Change history →</span>
        </div>

        <Section
          title="Uncommitted"
          count={files.length}
          sub={`drafts in your worktree on ${app.branch}`}
          actions={files.length > 0 && status.data ? (
            <>
              <button onClick={() => reject()} disabled={discard.isPending}
                style={sx('height:30px;padding:0 13px;border:1px solid var(--border-2);border-radius:7px;background:transparent;color:var(--del);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
                Discard all
              </button>
              <button onClick={() => setCommitOpen(true)}
                style={sx('height:30px;padding:0 13px;border:none;border-radius:7px;background:var(--data);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
                Commit {files.length} file{files.length === 1 ? '' : 's'}
              </button>
            </>
          ) : null}
        >
          <FamilyRollup files={files} model={app.model} />
          {!diff.isLoading && <WorktreeDiffList files={files} onDiscard={reject} />}
        </Section>

        {onFeature && (
          <Section
            title={`Ahead of ${defaultBranch}`}
            count={ahead.length}
            sub={merge.data?.dirty?.length ? 'commit the worktree first — a merge only lands committed work' : `committed on ${app.branch}`}
            actions={ahead.length > 0 ? (
              mergeMode === 'forge' ? (
                <button onClick={() => setProposeOpen(true)}
                  style={sx('height:30px;padding:0 13px;border:none;border-radius:7px;background:var(--prod);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
                  Propose changes
                </button>
              ) : (
                <button onClick={() => setMergeOpen(true)}
                  style={sx('height:30px;padding:0 13px;border:none;border-radius:7px;background:var(--prod);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
                  Merge into {defaultBranch}
                </button>
              )
            ) : null}
          >
            {conflicts.length > 0 && (
              <div style={sx('margin-bottom:14px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:10px;padding:11px 14px;font-size:12.5px')}>
                <b>Conflicts with {defaultBranch}</b> — {conflicts.join(', ')}. Pull first, resolve, then merge.
              </div>
            )}
            <FamilyRollup files={ahead} model={app.model} />
            {ahead.map((f) => <DiffCard key={f.path} file={f} />)}
            {!ahead.length && !merge.isLoading && (
              <div style={sx("padding:18px;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>
                nothing committed beyond {defaultBranch}
              </div>
            )}
          </Section>
        )}

        <Section title="On the forge" sub="the open merge request for this branch, if any">
          <ForgeReview repo={app.repoId} branch={app.branch} />
        </Section>
      </div>
      {commitOpen && status.data && <CommitDialog status={status.data} onClose={() => setCommitOpen(false)} />}
      {mergeOpen && <MergeDialog onClose={() => setMergeOpen(false)} />}
      {proposeOpen && <ProposeDialog onClose={() => setProposeOpen(false)} />}
    </div>
  );
}

function Section({ title, count, sub, actions, children }: {
  title: string; count?: number; sub?: string; actions?: React.ReactNode; children: React.ReactNode;
}) {
  return (
    <div style={sx('margin-top:24px')}>
      <div style={sx('display:flex;align-items:center;gap:9px;margin-bottom:12px;flex-wrap:wrap')}>
        <h2 style={sx('margin:0;font-size:15px;font-weight:700')}>{title}</h2>
        {count !== undefined && (
          <span style={sx('padding:1px 8px;border-radius:20px;background:var(--surface-2);border:1px solid var(--border);font-size:11px;font-weight:600;color:var(--text-2)')}>{count}</span>
        )}
        {sub && <span style={sx('font-size:11.5px;color:var(--text-3)')}>{sub}</span>}
        <div style={sx('flex:1')} />
        {actions}
      </div>
      {children}
    </div>
  );
}

/** "2 requirements · 1 spec" for a set of changed paths, via the config. */
function FamilyRollup({ files, model }: { files: DiffFile[]; model: NonNullable<ReturnType<typeof useApp>['model']> }) {
  if (!files.length) return null;
  const classify = classifier(model.entities);
  const byLabel = new Map<string, number>();
  files.forEach((f) => {
    const ent = classify(f.path);
    const key = ent ? singularLabel(ent) : 'file';
    byLabel.set(key, (byLabel.get(key) || 0) + 1);
  });
  return (
    <div style={sx('display:flex;gap:7px;flex-wrap:wrap;margin-bottom:12px')}>
      {[...byLabel.entries()].map(([label, n]) => (
        <span key={label} style={sx('padding:2px 9px;border-radius:20px;background:var(--surface-2);border:1px solid var(--border);font-size:11px;color:var(--text-2)')}>
          {n} {n === 1 ? label : pluralLabel(label)}
        </span>
      ))}
    </div>
  );
}
