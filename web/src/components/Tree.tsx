import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useProjects, useRepos, useStatus, useTree } from '../api/hooks';
import { buildRefTree, buildTree, filterRefPaths, RefDir } from '../lib/derive';
import { CommitDialog } from './CommitDialog';
import { NewDocDialog } from './NewDocDialog';
import { IconChevD, IconChevR, IconLock, IconPlus, IconSync } from './icons';

function agoLabel(iso?: string): string {
  if (!iso) return '';
  const mins = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  if (mins < 1) return 'synced just now';
  if (mins < 60) return `synced ${mins}m ago`;
  return `synced ${Math.round(mins / 60)}h ago`;
}

function RefFileRow({ repoId, file, depth, openPath }: { repoId: string; file: { name: string; path: string }; depth: number; openPath?: string }) {
  const nav = useNav();
  const target = `~${repoId}/${file.path}`;
  const active = openPath === target;
  return (
    <div
      onClick={() => nav('/editor/' + target)}
      style={sx(`display:flex;align-items:center;gap:7px;padding:5px 8px 5px ${26 + depth * 14}px;border-radius:6px;cursor:pointer;` +
        (active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600;color:var(--text)' : 'color:var(--text-3)'))}
    >
      <span style={sx('color:var(--reg);flex:none')}>◈</span>
      <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{file.name}</span>
    </div>
  );
}

function RefDirRow({ repoId, dir, depth, openPath }: { repoId: string; dir: RefDir; depth: number; openPath?: string }) {
  // collapsed by default; the chain holding the open document expands itself
  const inPath = !!openPath?.startsWith(`~${repoId}/${dir.path}/`);
  const [open, setOpen] = useState(inPath);
  useEffect(() => { if (inPath) setOpen(true); }, [inPath]);
  return (
    <div>
      <div
        onClick={() => setOpen(!open)}
        style={sx(`display:flex;align-items:center;gap:5px;padding:4px 8px 4px ${10 + depth * 14}px;border-radius:6px;cursor:pointer;color:var(--text-3);font-weight:600`)}
      >
        {open ? <IconChevD /> : <IconChevR />}
        <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{dir.name}</span>
      </div>
      {open && <RefDirBody repoId={repoId} dir={dir} depth={depth + 1} openPath={openPath} />}
    </div>
  );
}

function RefDirBody({ repoId, dir, depth, openPath }: { repoId: string; dir: RefDir; depth: number; openPath?: string }) {
  return (
    <>
      {dir.dirs.map((d) => <RefDirRow key={d.path} repoId={repoId} dir={d} depth={depth} openPath={openPath} />)}
      {dir.files.map((f) => <RefFileRow key={f.path} repoId={repoId} file={f} depth={depth} openPath={openPath} />)}
    </>
  );
}

// Read-only input repo: lock glyph, files open read-only, footer shows sync age.
// Listing honors the reference's `paths` prefixes (same semantics as grounding).
// Sections start collapsed (the tree isn't even fetched until expanded) —
// except the one holding the open document.
function ReadOnlyRepoSection({ repoId, syncedAt, okf, paths, openPath }: { repoId: string; syncedAt?: string; okf?: boolean; paths?: string[]; openPath?: string }) {
  const holdsOpenDoc = !!openPath?.startsWith(`~${repoId}/`);
  const [open, setOpen] = useState(holdsOpenDoc);
  // navigating into this reference (search palette, doc link) expands it
  useEffect(() => { if (holdsOpenDoc) setOpen(true); }, [holdsOpenDoc]);
  const tree = useTree(open ? repoId : undefined, '');
  const root = useMemo(
    () => buildRefTree(filterRefPaths((tree.data || []).map((e) => e.path), paths)),
    [tree.data, paths],
  );
  return (
    <div style={sx('margin-top:10px;border-top:1px solid var(--border);padding-top:6px')}>
      <div
        onClick={() => setOpen(!open)}
        style={sx('display:flex;align-items:center;gap:5px;padding:4px 8px;border-radius:6px;cursor:pointer;color:var(--text-3);font-weight:700;font-size:10.5px;letter-spacing:.5px;user-select:none')}
      >
        {open ? <IconChevD /> : <IconChevR />}
        <span title="read-only input repo" style={sx('display:inline-flex')}><IconLock /></span>{repoId.toUpperCase()}
        {okf && <span title="OKF bundle" style={sx("font-family:'JetBrains Mono',monospace;font-size:8px;font-weight:700;padding:1px 5px;border-radius:4px;background:var(--data-bg);color:var(--data)")}>OKF</span>}
        <div style={sx('flex:1')} />
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:9px;font-weight:400")}>{agoLabel(syncedAt)}</span>
      </div>
      {!open ? null : tree.isLoading ? (
        // unfetched repos (lazy PAT clones, importer mirrors mid-sync) can sit
        // here a while — shimmer keeps the section from reading as empty
        <>
          {[62, 45, 54].map((w, i) => (
            <div key={i} style={sx('display:flex;align-items:center;padding:6px 8px 6px 26px')}>
              <div style={{ ...sx('height:9px;border-radius:5px;background:var(--surface-2);animation:skel 1.3s ease-in-out infinite'), width: w + '%', animationDelay: i * 0.18 + 's' }} />
            </div>
          ))}
          <div style={sx("padding:2px 8px 4px 26px;font-family:'JetBrains Mono',monospace;font-size:9.5px;color:var(--text-3)")}>fetching {repoId}…</div>
        </>
      ) : tree.error ? (
        <div style={sx('display:flex;align-items:center;gap:8px;padding:6px 8px 6px 26px;font-size:11.5px;color:var(--text-3)')}>
          <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')} title={String((tree.error as Error).message || tree.error)}>
            couldn't load
          </span>
          <span onClick={() => void tree.refetch()} style={sx('flex:none;cursor:pointer;text-decoration:underline;text-decoration-color:var(--border-2);color:var(--text-2)')}>
            retry
          </span>
        </div>
      ) : root.dirs.length === 0 && root.files.length === 0 ? (
        <div style={sx('padding:6px 8px 6px 26px;font-size:11.5px;color:var(--text-3)')}>
          {paths?.length ? 'no files match the reference paths' : 'no files'}
        </div>
      ) : (
        <RefDirBody repoId={repoId} dir={root} depth={0} openPath={openPath} />
      )}
    </div>
  );
}

export function Tree() {
  const nav = useNav();
  const app = useApp();
  const { '*': openPath } = useParams();
  const status = useStatus(app.repoId, app.branch);
  const repos = useRepos();
  const [commitOpen, setCommitOpen] = useState(false);
  // reference section: the ACTIVE project's effective references (stage-3
  // selection ∩ grants); a project without references falls back to every
  // granted source (self-host default)
  const projectsQ = useProjects(app.branch);
  const activeRefs = projectsQ.data?.find((p) => p.id === app.repoId)?.references || [];
  const refNames = activeRefs.map((r) => r.source);
  const refPaths = Object.fromEntries(activeRefs.map((r) => [r.source, r.paths]));
  const readOnlyRepos = (repos.data || []).filter(
    (r) => r.kind === 'source' && (refNames.length === 0 || refNames.includes(r.id)),
  );
  // guided document creation (family, subfolder, auto-ID) lives in the
  // dialog; a string preselects that entity family
  const [newDoc, setNewDoc] = useState<{ kind?: string } | null>(null);

  const gitStatus: Record<string, string> = {};
  status.data?.dirty.forEach((f) => { gitStatus[f.path] = f.state; });
  const folders = app.files ? buildTree(app.files, openPath, gitStatus, app.entities) : [];
  const nDirty = status.data?.dirty.length ?? 0;

  return (
    <aside style={sx('width:250px;flex:none;background:var(--panel);border-right:1px solid var(--border);display:flex;flex-direction:column')}>
      <div style={sx('height:38px;flex:none;display:flex;align-items:center;justify-content:space-between;padding:0 8px 0 14px;border-bottom:1px solid var(--border)')}>
        <div style={sx('display:flex;align-items:center;gap:5px;font-weight:700;font-size:11px;letter-spacing:.5px;color:var(--text-2)')}>
          <IconChevR />{(app.repoId || '').toUpperCase()}
        </div>
        <div style={sx('display:flex;gap:2px;color:var(--text-3)')}>
          <span title="New document" onClick={() => setNewDoc({})} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer')}><IconPlus /></span>
          <span title="Refresh" onClick={() => status.refetch()} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer')}><IconSync /></span>
        </div>
      </div>
      <div style={sx('flex:1;overflow-y:auto;padding:8px 6px;font-size:12.5px;user-select:none')}>
        {folders.map((folder) => (
          <div key={folder.name}>
            <div title={folder.desc || undefined} style={sx('display:flex;align-items:center;gap:5px;padding:4px 8px;margin-top:3px;color:var(--text-2);font-weight:600')}>
              <IconChevD /><span style={sx('opacity:.9')}>{folder.name}</span>
              <div style={sx('flex:1')} />
              <span title={`New document in ${folder.name}/`}
                onClick={() => setNewDoc({ kind: app.entities.find((e) => e.folder === folder.name + '/')?.kind })}
                style={sx('width:18px;height:18px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer;color:var(--text-3);opacity:.6')}>
                <IconPlus />
              </span>
            </div>
            {folder.files.map((f) => (
              <div
                key={f.path}
                onClick={() => nav('/editor/' + f.path)}
                style={sx('display:flex;align-items:center;gap:7px;padding:5px 8px 5px 26px;border-radius:6px;cursor:pointer;' +
                  (f.active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600;color:var(--text)' : 'color:var(--text-2)') +
                  (f.generated ? ';opacity:.55' : ''))}
              >
                <span style={{ color: f.generated ? 'var(--text-3)' : f.color, flex: 'none' }}>{f.icon}</span>
                <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.name}</span>
                {f.generated && (
                  <span title="generated automatically at commit time"
                    style={sx("flex:none;font-family:'JetBrains Mono',monospace;font-size:8px;font-weight:700;padding:1px 5px;border-radius:4px;background:var(--surface-2);color:var(--text-3)")}>
                    GEN
                  </span>
                )}
                <div style={sx('flex:1')} />
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;" + f.badgeStyle)}>{f.badge}</span>
              </div>
            ))}
          </div>
        ))}
        {readOnlyRepos.map((r) => (
          <ReadOnlyRepoSection key={r.id} repoId={r.id} syncedAt={r.syncedAt} okf={r.okf} paths={refPaths[r.id]} openPath={openPath} />
        ))}
      </div>
      <div style={sx("height:34px;flex:none;display:flex;align-items:center;gap:8px;padding:0 12px;border-top:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-2)")}>
        {nDirty > 0 ? (
          <>
            <span
              onClick={() => window.dispatchEvent(new CustomEvent('specquill:changes'))}
              title="Show uncommitted changes"
              style={sx('cursor:pointer;text-decoration:underline;text-decoration-color:var(--border-2)')}
            >
              <span style={sx('color:var(--reg)')}>●</span> {nDirty} change{nDirty === 1 ? '' : 's'}
            </span>
            <div style={sx('flex:1')} />
            <button onClick={() => setCommitOpen(true)}
              style={sx('height:22px;padding:0 10px;border:none;border-radius:6px;background:var(--data);color:#fff;font-family:inherit;font-size:10.5px;font-weight:700;cursor:pointer')}>
              Commit
            </button>
          </>
        ) : (
          <>
            <span style={sx('color:var(--data)')}>●</span> clean
            <div style={sx('flex:1')} />
            <span>{app.branch}</span>
          </>
        )}
      </div>
      {commitOpen && status.data && <CommitDialog status={status.data} onClose={() => setCommitOpen(false)} />}
      {newDoc && <NewDocDialog initialKind={newDoc.kind} onClose={() => setNewDoc(null)} />}
    </aside>
  );
}
