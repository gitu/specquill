import { useState } from 'react';
import { sx } from '../lib/sx';
import { useNarrow } from '../hooks/useMediaQuery';
import { useApp } from '../state/AppContext';
import { useAppPath, useNav } from '../state/nav';
import { useBranches, useCreateBranch, useMe, useStatus, useSync } from '../api/hooks';
import { api, clearStoredPat } from '../api/client';
import { useDynamicInfo } from '../api/dynamic';
import { MergeDialog } from './MergeDialog';
import { OpenRepoDialog } from './OpenRepoDialog';
import { ProposeDialog } from './ProposeDialog';
import { IconBranch, IconChevD, IconLock, IconMenu, IconMerge, IconQuill, IconSearch, IconUp, IconDown } from './icons';

export function TopBar() {
  const nav = useNav();
  const app = useApp();
  const branches = useBranches(app.repoId);
  const me = useMe();
  const status = useStatus(app.repoId, app.branch);
  const sync = useSync(app.repoId, app.branch);
  const createBranch = useCreateBranch(app.repoId);
  const [open, setOpen] = useState(false);
  const [userMenu, setUserMenu] = useState(false);
  const [mergeDialog, setMergeDialog] = useState(false);
  const [openRepo, setOpenRepo] = useState(false);
  const dynamic = useDynamicInfo();
  const narrow = useNarrow();
  const pathname = useAppPath();
  const onTreeRoute = pathname.startsWith('/editor');
  const ahead = status.data?.ahead ?? 0;
  const behind = status.data?.behind ?? 0;
  const logout = async () => {
    clearStoredPat(); // else the 401 handler would silently sign right back in
    await api('/auth/logout', { method: 'POST' });
    window.location.href = '/auth/login';
  };
  const newBranch = async () => {
    const name = window.prompt('New branch name (from ' + app.branch + '):');
    if (!name) return;
    await createBranch.mutateAsync({ name, from: app.branch });
    app.switchBranch(name, { carryDraft: true });
    setOpen(false);
  };
  const localBranches = (branches.data || []).filter((b) => !b.isRemote);
  const remoteBranches = (branches.data || []).filter((b) => b.isRemote);
  // a remote-only branch has no local head yet — materialize it on switch.
  // Close the menu before awaiting: a still-open menu invites a double-click
  // and a second CreateBranch ("already exists").
  const switchToRemote = async (name: string) => {
    if (createBranch.isPending) return;
    setOpen(false);
    await createBranch.mutateAsync({ name, from: 'origin/' + name });
    app.switchBranch(name);
  };

  return (
    <header style={sx('height:46px;flex:none;display:flex;align-items:center;gap:' + (narrow ? '8px' : '12px') + ';padding:0 12px 0 14px;background:var(--surface);border-bottom:1px solid var(--border);position:relative;z-index:5')}>
      {narrow && onTreeRoute && (
        <button onClick={() => window.dispatchEvent(new CustomEvent('specquill:tree'))} title="Files"
          style={sx('flex:none;width:32px;height:32px;display:flex;align-items:center;justify-content:center;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text-2);cursor:pointer')}>
          <IconMenu />
        </button>
      )}
      <div style={sx('flex:none;display:flex;align-items:center;gap:8px')}>
        <div style={sx('width:22px;height:22px;border-radius:6px;background:var(--brand);color:var(--brand-fg);display:flex;align-items:center;justify-content:center')}>
          <IconQuill size={14} />
        </div>
        {!narrow && <span style={sx('font-weight:700;font-size:14px;letter-spacing:-.2px')}>SpecQuill</span>}
      </div>

      {/* project switcher (hidden with a single project — unless dynamic
          projects can add more, REQ-025) */}
      {(app.projects.length > 1 || dynamic.data?.enabled) && (
        <select
          value={app.repoId || ''}
          onChange={(e) => {
            if (e.target.value === '__open__') setOpenRepo(true);
            else app.switchProject(e.target.value);
          }}
          title="Project"
          style={sx("height:26px;padding:0 6px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:11.5px;font-weight:500;cursor:pointer;max-width:170px")}
        >
          {app.projects.map((p) => (
            <option key={p.id} value={p.id}>{p.id}</option>
          ))}
          {dynamic.data?.enabled && <option value="__open__">＋ Open repository…</option>}
        </select>
      )}
      {openRepo && <OpenRepoDialog onClose={() => setOpenRepo(false)} />}

      {/* branch switcher */}
      <div style={sx('position:relative;flex:none')}>
        <div
          onClick={() => setOpen((v) => !v)}
          style={sx('display:flex;align-items:center;gap:6px;padding:4px 9px;border:1px solid var(--border-2);border-radius:7px;cursor:pointer;background:var(--surface-2)')}
        >
          <IconBranch />
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;font-weight:500;max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap")}>{app.branch}</span>
          {/* inline chip — an absolutely-positioned label below the pill overlapped the content under the header */}
          {app.branch.startsWith('ws/') && (
            <span title="personal workspace" style={sx('flex:none;font-size:8.5px;font-weight:700;letter-spacing:.4px;color:var(--ai);background:var(--ai-bg);padding:2px 5px;border-radius:4px')}>PERSONAL</span>
          )}
          {app.isProtectedBranch && <span title="protected — edits move to your workspace" style={sx('display:inline-flex;color:var(--text-3)')}><IconLock /></span>}
          <span style={sx('color:var(--text-3)')}><IconChevD /></span>
        </div>
        {open && (
          <div style={sx('position:absolute;left:0;top:34px;min-width:230px;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow-lg);overflow:hidden;z-index:20')}>
            {localBranches.map((b) => (
              <div
                key={b.name}
                onClick={() => { app.switchBranch(b.name); setOpen(false); }}
                style={sx('display:flex;align-items:center;gap:8px;padding:8px 12px;cursor:pointer;font-size:12.5px;' + (b.name === app.branch ? 'background:var(--surface-2);font-weight:600' : ''))}
              >
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;flex:1")}>{b.name}</span>
                {app.protectedBranches.includes(b.name) && <span title="protected" style={sx('display:inline-flex;color:var(--text-3)')}><IconLock /></span>}
                {b.isDefault && <span style={sx('font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:4px;padding:1px 5px')}>default</span>}
              </div>
            ))}
            {remoteBranches.map((b) => (
              <div
                key={'origin/' + b.name}
                onClick={() => switchToRemote(b.name)}
                title={'origin/' + b.name + ' — switching creates a local branch'}
                style={sx('display:flex;align-items:center;gap:8px;padding:8px 12px;cursor:pointer;font-size:12.5px;' + (remoteBranches[0] === b ? 'border-top:1px solid var(--border);' : ''))}
              >
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;flex:1;color:var(--text-2)")}>{b.name}</span>
                <span style={sx('font-size:10px;color:var(--text-3);border:1px dashed var(--border);border-radius:4px;padding:1px 5px')}>remote</span>
              </div>
            ))}
            <div onClick={newBranch} style={sx('display:flex;align-items:center;gap:6px;padding:8px 12px;cursor:pointer;font-size:12px;color:var(--prod);border-top:1px solid var(--border);font-weight:600')}>
              + New branch from {app.branch}
            </div>
          </div>
        )}
      </div>

      <div style={sx('flex:1')} />
      {narrow ? (
        <button onClick={() => window.dispatchEvent(new CustomEvent('specquill:search'))} title="Search"
          style={sx('flex:none;width:32px;height:32px;display:flex;align-items:center;justify-content:center;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text-3);cursor:pointer')}>
          <IconSearch />
        </button>
      ) : (
        <div
          onClick={() => window.dispatchEvent(new CustomEvent('specquill:search'))}
          // the search bar is the header's designated shrink element — a fixed
          // width made it overlap its neighbours on mid-size windows
          style={sx('flex:0 1 340px;min-width:110px;height:30px;display:flex;align-items:center;gap:8px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text-3);cursor:pointer;overflow:hidden')}
        >
          <span style={sx('flex:none;display:inline-flex')}><IconSearch /></span>
          <span style={sx('font-size:12.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>Search requirements, specs, fields, documents…</span>
          <div style={sx('flex:1')} />
          <span style={sx("flex:none;font-family:'JetBrains Mono',monospace;font-size:11px;padding:1px 5px;border:1px solid var(--border-2);border-radius:4px")}>⌘K</span>
        </div>
      )}
      <div style={sx('flex:1')} />

      {!narrow && (
        <div
          title={sync.isPending ? 'syncing…' : `ahead ${ahead} / behind ${behind} — click to fetch${ahead > 0 ? ' + push' : ''}`}
          onClick={() => !sync.isPending && sync.mutate({ push: ahead > 0 })}
          style={sx("display:flex;align-items:center;gap:5px;font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-2);padding:4px 8px;border:1px solid var(--border);border-radius:7px;cursor:pointer;" + (sync.isPending ? 'opacity:.5' : ''))}
        >
          <IconUp />{ahead} <IconDown />{behind}
        </div>
      )}
      <button
        onClick={() => setMergeDialog(true)}
        aria-label={app.mergeMode === 'forge' ? 'Propose changes' : 'Merge branch'}
        title={app.mergeMode === 'forge'
          ? 'push ' + app.branch + ' and open a merge request'
          : 'merge ' + app.branch + ' into the default branch'}
        style={sx('flex:none;display:flex;align-items:center;gap:6px;height:30px;padding:0 ' + (narrow ? '9px' : '12px') + ';border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}
      >
        <IconMerge /> {narrow ? '' : app.mergeMode === 'forge' ? 'Propose' : 'Merge'}
      </button>
      {mergeDialog && (app.mergeMode === 'forge'
        ? <ProposeDialog onClose={() => setMergeDialog(false)} />
        : <MergeDialog onClose={() => setMergeDialog(false)} />)}
      {/* signing out is destructive (forge mode drops the stored PAT) — it
          sits behind the menu, never on the chip's single click */}
      <div style={sx('position:relative;flex:none')}>
        <button
          title={me.data ? `${me.data.name} <${me.data.email}>` : ''}
          aria-label="account menu"
          aria-haspopup="menu"
          aria-expanded={userMenu}
          onClick={() => setUserMenu((v) => !v)}
          style={sx('width:28px;height:28px;border:none;padding:0;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));display:flex;align-items:center;justify-content:center;color:#fff;font-family:inherit;font-weight:600;font-size:11px;cursor:pointer')}
        >
          {me.data?.initials || '…'}
        </button>
        {userMenu && (
          <div role="menu" style={sx('position:absolute;right:0;top:34px;min-width:200px;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow-lg);overflow:hidden;z-index:20')}>
            <div style={sx('padding:9px 12px;border-bottom:1px solid var(--border)')}>
              <div style={sx('font-size:12.5px;font-weight:600')}>{me.data?.name || '…'}</div>
              <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>{me.data?.email || ''}</div>
            </div>
            <button
              role="menuitem"
              onClick={() => { setUserMenu(false); void logout(); }}
              style={sx('display:flex;align-items:center;gap:6px;width:100%;padding:8px 12px;border:none;background:transparent;text-align:left;cursor:pointer;font-family:inherit;font-size:12.5px;color:var(--reg);font-weight:600')}
            >
              Sign out
            </button>
          </div>
        )}
      </div>
    </header>
  );
}
