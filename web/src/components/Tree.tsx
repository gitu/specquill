import { useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { api } from '../api/client';
import { useApp } from '../state/AppContext';
import { useProjects, useRepos, useStatus, useTree } from '../api/hooks';
import { buildRefTree, buildTree, filterRefPaths, RefDir, TreeDir, TreeFile } from '../lib/derive';
import { useWorkspace } from '../hooks/useWorkspace';
import { CommitDialog } from './CommitDialog';
import { DeleteDialog } from './DeleteDialog';
import { FolderMoveDialog, MoveDialog } from './MoveDialog';
import { NewDocDialog } from './NewDocDialog';
import { NewFolderDialog } from './NewFolderDialog';
import { useToasts } from './Toast';
import { IconChevD, IconChevR, IconLock, IconPlus, IconSync } from './icons';

// persisted drag-to-resize width, same pattern as the Speccy panel
function useStoredSize(key: string, fallback: number, lo: number, hi: number) {
  const clamp = (v: number) => Math.min(hi, Math.max(lo, v));
  const [size, setSize] = useState(() => {
    try {
      const v = parseInt(localStorage.getItem(key) || '', 10);
      return Number.isFinite(v) ? clamp(v) : fallback;
    } catch {
      return fallback;
    }
  });
  useEffect(() => {
    try { localStorage.setItem(key, String(size)); } catch { /* quota */ }
  }, [key, size]);
  return [size, (v: number) => setSize(clamp(v))] as const;
}

// row detail modes: filename only, +title, +type, or both — cycled by the
// header toggle, persisted per browser
const DETAIL_MODES = ['off', 'title', 'type', 'both'] as const;
type DetailMode = (typeof DETAIL_MODES)[number];

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
  const [movePath, setMovePath] = useState<string | null>(null);
  const [moveFolder, setMoveFolder] = useState<string | null>(null);
  const [delPath, setDelPath] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState<string | null>(null);
  const [treeW, setTreeW] = useStoredSize('specquill-tree-w', 250, 180, 520);
  // all-files mode: the FULL repo listing (binaries, root files, .specquill)
  // instead of only the classified document families
  const [showAll, setShowAll] = useState(() => {
    try { return localStorage.getItem('specquill-tree-all') === '1'; } catch { return false; }
  });
  const toggleAll = () => setShowAll((v) => {
    try { localStorage.setItem('specquill-tree-all', v ? '0' : '1'); } catch { /* quota */ }
    return !v;
  });
  const fullTree = useTree(showAll ? app.repoId : undefined, app.branch);
  const treeW0 = useRef(0);
  const [filter, setFilter] = useState('');
  const [detail, setDetail] = useState<DetailMode>(() => {
    try { return (DETAIL_MODES as readonly string[]).includes(localStorage.getItem('specquill-tree-detail') || '') ? localStorage.getItem('specquill-tree-detail') as DetailMode : 'off'; } catch { return 'off'; }
  });
  const cycleDetail = () => {
    const next = DETAIL_MODES[(DETAIL_MODES.indexOf(detail) + 1) % DETAIL_MODES.length];
    setDetail(next);
    try { localStorage.setItem('specquill-tree-detail', next); } catch { /* quota */ }
  };
  const qc = useQueryClient();
  const toasts = useToasts();
  const { ensureWritableBranch } = useWorkspace();

  const gitStatus: Record<string, string> = {};
  status.data?.dirty.forEach((f) => { gitStatus[f.path] = f.state; });
  const allFolders = app.files
    ? buildTree(app.files, openPath, gitStatus, app.entities, app.model,
        { all: showAll, extraPaths: (fullTree.data || []).map((e) => e.path) })
    : [];
  // filter matches path, filename, frontmatter title and classified type;
  // while filtering, directories without hits (recursively) drop out
  const q = filter.trim().toLowerCase();
  const matchFile = (f: TreeFile) =>
    !q || f.path.toLowerCase().includes(q) || (f.title || '').toLowerCase().includes(q) || (f.docType || '').toLowerCase().includes(q);
  const prune = (d: TreeDir): TreeDir | null => {
    const dirs = d.dirs.map(prune).filter((x): x is TreeDir => !!x);
    const fs = d.files.filter(matchFile);
    return fs.length || dirs.length ? { ...d, dirs, files: fs } : null;
  };
  const folders = !q ? allFolders : allFolders.map(prune).filter((x): x is TreeDir => !!x);
  const nDirty = status.data?.dirty.length ?? 0;
  const [collapsedDirs, setCollapsedDirs] = useState<Set<string>>(new Set());
  const toggleDir = (path: string) => setCollapsedDirs((prev) => {
    const next = new Set(prev);
    if (next.has(path)) next.delete(path); else next.add(path);
    return next;
  });

  // drop-onto-folder move: same server-side rewrite as the Move dialog, just
  // without the dialog — the toast reports the rewritten reference count
  const moveInto = async (from: string, folder: string) => {
    const target = folder + '/' + from.split('/').pop();
    if (target === from) return;
    try {
      const branch = await ensureWritableBranch();
      const res = await api<{ rewritten: string[] }>(
        `/api/repos/${app.repoId}/move?branch=${encodeURIComponent(branch)}`, {
          method: 'POST',
          body: JSON.stringify({ from, to: target }),
        });
      const rewritten = res.rewritten?.length ?? 0;
      qc.invalidateQueries();
      toasts.push({
        text: `Moved to ${target}` + (rewritten ? ` — ${rewritten} referencing document${rewritten === 1 ? '' : 's'} updated` : ''),
        kind: 'success',
      });
      if (openPath === from) nav('/editor/' + target);
    } catch (e) {
      toasts.push({ text: String((e as Error).message || e), kind: 'error' });
    }
  };

  const [newFolderOpen, setNewFolderOpen] = useState(false);

  const renderFile = (f: TreeFile, depth: number) => (
    <div
      key={f.path}
      onClick={() => nav('/editor/' + f.path)}
      draggable={app.canEdit && !f.generated}
      onDragStart={(e) => e.dataTransfer.setData('text/specquill-path', f.path)}
      style={sx(`display:flex;align-items:center;gap:7px;padding:5px 8px 5px ${26 + depth * 14}px;border-radius:6px;cursor:pointer;` +
        (f.active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600;color:var(--text)' : 'color:var(--text-2)') +
        (f.generated ? ';opacity:.55' : ''))}
    >
      <span style={{ color: f.generated ? 'var(--text-3)' : f.color, flex: 'none' }}>{f.icon}</span>
      <span style={sx('flex:none;max-width:55%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.name}</span>
      {(detail === 'title' || detail === 'both') && f.title && (
        <span title={f.title} style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px;color:var(--text-3)')}>{f.title}</span>
      )}
      {(detail === 'type' || detail === 'both') && f.docType && (
        <span title={f.docType} style={sx("flex:none;font-family:'JetBrains Mono',monospace;font-size:8.5px;font-weight:600;padding:1px 5px;border-radius:4px;background:var(--surface-2);color:var(--text-3)")}>{f.docType}</span>
      )}
      {f.generated && (
        <span title="generated automatically at commit time"
          style={sx("flex:none;font-family:'JetBrains Mono',monospace;font-size:8px;font-weight:700;padding:1px 5px;border-radius:4px;background:var(--surface-2);color:var(--text-3)")}>
          GEN
        </span>
      )}
      <div style={sx('flex:1')} />
      {app.canEdit && !f.generated && (
        <span
          title={'move / rename ' + f.path}
          onClick={(e) => { e.stopPropagation(); setMovePath(f.path); }}
          style={sx('flex:none;width:16px;display:flex;align-items:center;justify-content:center;color:var(--text-3);font-size:11px;opacity:.5;cursor:pointer')}
        >
          ⇢
        </span>
      )}
      {app.canEdit && !f.generated && (
        <span
          title={'delete ' + f.path}
          onClick={(e) => { e.stopPropagation(); setDelPath(f.path); }}
          style={sx('flex:none;width:16px;display:flex;align-items:center;justify-content:center;color:var(--text-3);font-size:11px;opacity:.5;cursor:pointer')}
        >
          ✕
        </span>
      )}
      <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;" + f.badgeStyle)}>{f.badge}</span>
    </div>
  );

  // recursive directory renderer: any depth nests — subfolders collapse on
  // click, and every directory header is a drop target + move handle
  const renderDir = (d: TreeDir, depth: number): React.ReactNode => {
    const collapsed = collapsedDirs.has(d.path || '/');
    const top = depth === 0;
    return (
      <div key={d.path || '/'}>
        <div
          title={d.desc || undefined}
          onClick={() => toggleDir(d.path || '/')}
          // directory headers accept dropped files — the move rewrites
          // inbound references server-side, like the Move dialog
          onDragOver={app.canEdit && d.path ? (e) => { e.preventDefault(); setDragOver(d.path); } : undefined}
          onDragLeave={() => setDragOver((v) => (v === d.path ? null : v))}
          onDrop={app.canEdit && d.path ? (e) => {
            e.preventDefault();
            setDragOver(null);
            const from = e.dataTransfer.getData('text/specquill-path');
            if (from) void moveInto(from, d.path);
          } : undefined}
          style={sx(`display:flex;align-items:center;gap:5px;padding:4px 8px 4px ${top ? 8 : 10 + depth * 14}px;` +
            (top ? 'margin-top:3px;font-weight:600;color:var(--text-2);' : 'color:var(--text-3);font-weight:600;') +
            'border-radius:6px;cursor:pointer;' +
            (dragOver === d.path ? 'background:var(--prod-bg);outline:1px dashed var(--prod-line)' : ''))}
        >
          {collapsed ? <IconChevR /> : <IconChevD />}
          <span style={sx('opacity:.9;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{d.name}</span>
          <div style={sx('flex:1')} />
          {app.canEdit && !!d.path && (
            <span title={`move / rename ${d.path}/`}
              onClick={(e) => { e.stopPropagation(); setMoveFolder(d.path); }}
              style={sx('width:18px;height:18px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer;color:var(--text-3);opacity:.6;font-size:11px')}>
              ⇢
            </span>
          )}
          {top && (
            <span title={`New document in ${d.path}/`}
              onClick={(e) => { e.stopPropagation(); setNewDoc({ kind: app.entities.find((en) => en.folder === d.path + '/')?.kind }); }}
              style={sx('width:18px;height:18px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer;color:var(--text-3);opacity:.6')}>
              <IconPlus />
            </span>
          )}
        </div>
        {!collapsed && (
          <>
            {d.dirs.map((sub) => renderDir(sub, depth + 1))}
            {d.files.map((f) => renderFile(f, depth))}
          </>
        )}
      </div>
    );
  };

  return (
    <aside style={{ ...sx('flex:none;background:var(--panel);border-right:1px solid var(--border);display:flex;flex-direction:column;position:relative'), width: treeW }}>
      {/* right-edge resize handle */}
      <div
        onPointerDown={(e) => {
          e.preventDefault();
          treeW0.current = treeW;
          const startX = e.clientX;
          const move = (ev: PointerEvent) => setTreeW(treeW0.current + (ev.clientX - startX));
          const up = () => {
            window.removeEventListener('pointermove', move);
            window.removeEventListener('pointerup', up);
          };
          window.addEventListener('pointermove', move);
          window.addEventListener('pointerup', up);
        }}
        title="drag to resize"
        style={sx('position:absolute;right:-3px;top:0;bottom:0;width:6px;cursor:col-resize;z-index:6')}
      />
      <div style={sx('height:38px;flex:none;display:flex;align-items:center;justify-content:space-between;padding:0 8px 0 14px;border-bottom:1px solid var(--border)')}>
        <div style={sx('display:flex;align-items:center;gap:5px;font-weight:700;font-size:11px;letter-spacing:.5px;color:var(--text-2)')}>
          <IconChevR />{(app.repoId || '').toUpperCase()}
        </div>
        <div style={sx('display:flex;gap:2px;color:var(--text-3)')}>
          <span title="New document" onClick={() => setNewDoc({})} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer')}><IconPlus /></span>
          {app.canEdit && (
            <span title="New folder" onClick={() => setNewFolderOpen(true)} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer;font-size:13px')}>⊞</span>
          )}
          <span
            title={showAll ? 'Showing ALL repository files — click for documents only' : 'Show all repository files (binaries, root files, .specquill)'}
            onClick={toggleAll}
            style={sx('height:22px;padding:0 6px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer;font-size:12px;' + (showAll ? 'color:var(--prod);background:var(--prod-bg)' : ''))}
          >
            ∗
          </span>
          <span
            title={'Row details: ' + ({ off: 'filename only', title: 'show titles', type: 'show types', both: 'show titles + types' })[detail] + ' — click to change'}
            onClick={cycleDetail}
            style={sx('height:22px;padding:0 5px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer;font-size:9px;font-weight:700;letter-spacing:.4px;' + (detail !== 'off' ? 'color:var(--prod);background:var(--prod-bg)' : ''))}
          >
            {({ off: 'Aa', title: '“T”', type: '◈', both: '“T”◈' })[detail]}
          </span>
          <span title="Refresh" onClick={() => status.refetch()} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer')}><IconSync /></span>
        </div>
      </div>
      <div style={sx('flex:none;padding:7px 8px 0')}>
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter by type, title, path…"
          aria-label="filter files"
          onKeyDown={(e) => { if (e.key === 'Escape') setFilter(''); }}
          style={sx('width:100%;box-sizing:border-box;height:26px;padding:0 9px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:11.5px;outline:none')}
        />
      </div>
      <div style={sx('flex:1;overflow-y:auto;padding:8px 6px;font-size:12.5px;user-select:none')}>
        {folders.map((folder) => renderDir(folder, 0))}
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
      {movePath && <MoveDialog path={movePath} onClose={() => setMovePath(null)} />}
      {moveFolder && <FolderMoveDialog folder={moveFolder} openPath={openPath} onClose={() => setMoveFolder(null)} />}
      {delPath && <DeleteDialog path={delPath} openPath={openPath} onClose={() => setDelPath(null)} />}
      {newFolderOpen && <NewFolderDialog onClose={() => setNewFolderOpen(false)} />}
    </aside>
  );
}
