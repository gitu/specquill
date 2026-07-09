import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useMe, usePresence, useRepos, useStatus, useTree } from '../api/hooks';
import { buildTree } from '../lib/derive';
import { newDocTemplate } from '../lib/newdoc';
import { api } from '../api/client';
import { useWorkspace } from '../hooks/useWorkspace';
import { CommitDialog } from './CommitDialog';
import { IconChevD, IconChevR, IconLock, IconPlus, IconSync } from './icons';

function agoLabel(iso?: string): string {
  if (!iso) return '';
  const mins = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  if (mins < 1) return 'synced just now';
  if (mins < 60) return `synced ${mins}m ago`;
  return `synced ${Math.round(mins / 60)}h ago`;
}

// Read-only input repo: lock glyph, files open read-only, footer shows sync age.
function ReadOnlyRepoSection({ repoId, syncedAt, openPath }: { repoId: string; syncedAt?: string; openPath?: string }) {
  const nav = useNavigate();
  const tree = useTree(repoId, '');
  return (
    <div style={sx('margin-top:10px;border-top:1px solid var(--border);padding-top:6px')}>
      <div style={sx('display:flex;align-items:center;gap:5px;padding:4px 8px;color:var(--text-3);font-weight:700;font-size:10.5px;letter-spacing:.5px')}>
        <span title="read-only input repo" style={sx('display:inline-flex')}><IconLock /></span>{repoId.toUpperCase()}
        <div style={sx('flex:1')} />
        <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:9px;font-weight:400")}>{agoLabel(syncedAt)}</span>
      </div>
      {(tree.data || []).map((e) => {
        const target = `~${repoId}/${e.path}`;
        const active = openPath === target;
        return (
          <div
            key={e.path}
            onClick={() => nav('/editor/' + target)}
            style={sx('display:flex;align-items:center;gap:7px;padding:5px 8px 5px 26px;border-radius:6px;cursor:pointer;' +
              (active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600;color:var(--text)' : 'color:var(--text-3)'))}
          >
            <span style={sx('color:var(--reg);flex:none')}>◈</span>
            <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{e.path.split('/').pop()}</span>
          </div>
        );
      })}
    </div>
  );
}

export function Tree() {
  const nav = useNavigate();
  const app = useApp();
  const { '*': openPath } = useParams();
  const status = useStatus(app.repoId, app.branch);
  const repos = useRepos();
  const me = useMe();
  const presence = usePresence(app.repoId);
  const [commitOpen, setCommitOpen] = useState(false);
  const readOnlyRepos = (repos.data || []).filter((r) => r.mode === 'readonly');
  const qc = useQueryClient();
  const { ensureWritableBranch } = useWorkspace();

  // new markdown document: family-typed frontmatter, saved as a draft on the
  // (auto-created) writable branch, opened in the editor
  const newFile = async () => {
    const raw = window.prompt('New file path (e.g. requirements/REQ-101.md):');
    if (!raw) return;
    let path = raw.trim().replace(/^\/+/, '');
    if (!path) return;
    if (!path.endsWith('.md')) path += '.md';
    try {
      const branch = await ensureWritableBranch();
      await api<{ sha: string }>(`/api/repos/${app.repoId}/files/${path}?branch=${encodeURIComponent(branch)}`, {
        method: 'PUT',
        body: JSON.stringify({ content: newDocTemplate(path), baseSha: '' }),
      });
      qc.invalidateQueries({ queryKey: ['status', app.repoId] });
      qc.invalidateQueries({ queryKey: ['snapshot', app.repoId] });
      nav('/editor/' + path);
    } catch (e) {
      window.alert('create failed: ' + String((e as Error).message || e));
    }
  };

  // who is co-editing what: dots on files roomed on this branch, a status
  // line for live sessions on other branches (discoverability)
  const liveHere: Record<string, string[]> = {};
  const orphanedHere: Record<string, boolean> = {};
  const elsewhere: { path: string; branch: string; names: string[] }[] = [];
  for (const room of presence.data || []) {
    if (room.branch === app.branch) {
      if (room.orphaned) orphanedHere[room.path] = true;
      else if (room.users.length) liveHere[room.path] = room.users.map((u) => u.name);
    } else if (room.users.some((u) => u.userId !== me.data?.id)) {
      elsewhere.push({ path: room.path, branch: room.branch, names: room.users.map((u) => u.name) });
    }
  }

  const gitStatus: Record<string, string> = {};
  status.data?.dirty.forEach((f) => { gitStatus[f.path] = f.state; });
  const folders = app.files ? buildTree(app.files, openPath, gitStatus) : [];
  const nDirty = status.data?.dirty.length ?? 0;

  return (
    <aside style={sx('width:250px;flex:none;background:var(--panel);border-right:1px solid var(--border);display:flex;flex-direction:column')}>
      <div style={sx('height:38px;flex:none;display:flex;align-items:center;justify-content:space-between;padding:0 8px 0 14px;border-bottom:1px solid var(--border)')}>
        <div style={sx('display:flex;align-items:center;gap:5px;font-weight:700;font-size:11px;letter-spacing:.5px;color:var(--text-2)')}>
          <IconChevR />{(app.repoId || '').toUpperCase()}
        </div>
        <div style={sx('display:flex;gap:2px;color:var(--text-3)')}>
          <span title="New file" onClick={newFile} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer')}><IconPlus /></span>
          <span title="Refresh" onClick={() => status.refetch()} style={sx('width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer')}><IconSync /></span>
        </div>
      </div>
      <div style={sx('flex:1;overflow-y:auto;padding:8px 6px;font-size:12.5px;user-select:none')}>
        {folders.map((folder) => (
          <div key={folder.name}>
            <div style={sx('display:flex;align-items:center;gap:5px;padding:4px 8px;margin-top:3px;color:var(--text-2);font-weight:600')}>
              <IconChevD /><span style={sx('opacity:.9')}>{folder.name}</span>
            </div>
            {folder.files.map((f) => (
              <div
                key={f.path}
                onClick={() => nav('/editor/' + f.path)}
                style={sx('display:flex;align-items:center;gap:7px;padding:5px 8px 5px 26px;border-radius:6px;cursor:pointer;' +
                  (f.active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600;color:var(--text)' : 'color:var(--text-2)'))}
              >
                <span style={{ color: f.color, flex: 'none' }}>{f.icon}</span>
                <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.name}</span>
                <div style={sx('flex:1')} />
                {liveHere[f.path] && (
                  <span title={`co-editing live: ${liveHere[f.path].join(', ')}`}
                    style={sx('width:7px;height:7px;flex:none;border-radius:50%;background:var(--data);box-shadow:0 0 0 2px color-mix(in srgb, var(--data) 25%, transparent)')} />
                )}
                {orphanedHere[f.path] && !liveHere[f.path] && (
                  <span title="unsaved co-editing changes — open the file to recover them"
                    style={sx('width:7px;height:7px;flex:none;border-radius:50%;background:var(--reg)')} />
                )}
                <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10px;" + f.badgeStyle)}>{f.badge}</span>
              </div>
            ))}
          </div>
        ))}
        {readOnlyRepos.map((r) => (
          <ReadOnlyRepoSection key={r.id} repoId={r.id} syncedAt={r.syncedAt} openPath={openPath} />
        ))}
        {elsewhere.length > 0 && (
          <div style={sx('margin-top:10px;border-top:1px solid var(--border);padding:8px 8px 0')}>
            {elsewhere.map((r) => (
              <div key={r.branch + r.path}
                onClick={() => nav('/editor/' + r.path + '?branch=' + encodeURIComponent(r.branch) + '&invite=1')}
                title="open and join"
                style={sx('display:flex;align-items:center;gap:6px;padding:3px 0;font-size:11px;color:var(--text-3);cursor:pointer')}>
                <span style={sx('width:6px;height:6px;flex:none;border-radius:50%;background:var(--data)')} />
                <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>
                  {r.names.join(', ')} editing {r.path.split('/').pop()} on {r.branch}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
      <div style={sx("height:34px;flex:none;display:flex;align-items:center;gap:8px;padding:0 12px;border-top:1px solid var(--border);font-family:'IBM Plex Mono',monospace;font-size:10.5px;color:var(--text-2)")}>
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
    </aside>
  );
}
