import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useNarrow } from '../hooks/useMediaQuery';
import { useApp } from '../state/AppContext';
import { useBranches, useCreateBranch, useMe, usePRs, useStatus, useSync } from '../api/hooks';
import { api } from '../api/client';
import { CreatePRDialog } from './CreatePRDialog';
import { IconBranch, IconChevD, IconLock, IconMenu, IconPR, IconQuill, IconSearch, IconUp, IconDown } from './icons';

export function TopBar() {
  const nav = useNavigate();
  const app = useApp();
  const branches = useBranches(app.repoId);
  const me = useMe();
  const status = useStatus(app.repoId, app.branch);
  const sync = useSync(app.repoId, app.branch);
  const createBranch = useCreateBranch(app.repoId);
  const prs = usePRs(app.repoId, 'open');
  const [open, setOpen] = useState(false);
  const [prDialog, setPrDialog] = useState(false);
  const narrow = useNarrow();
  const { pathname } = useLocation();
  const onTreeRoute = pathname.startsWith('/editor') || pathname.startsWith('/diff');
  const branchPR = prs.data?.find((p) => p.source === app.branch);
  const ahead = status.data?.ahead ?? 0;
  const behind = status.data?.behind ?? 0;
  const logout = async () => {
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

  return (
    <header style={sx('height:46px;flex:none;display:flex;align-items:center;gap:' + (narrow ? '8px' : '12px') + ';padding:0 12px 0 14px;background:var(--surface);border-bottom:1px solid var(--border);position:relative;z-index:5')}>
      {narrow && onTreeRoute && (
        <button onClick={() => window.dispatchEvent(new CustomEvent('specquill:tree'))} title="Files"
          style={sx('flex:none;width:32px;height:32px;display:flex;align-items:center;justify-content:center;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text-2);cursor:pointer')}>
          <IconMenu />
        </button>
      )}
      <div style={sx('flex:none;display:flex;align-items:center;gap:8px')}>
        <div style={sx('width:22px;height:22px;border-radius:6px;background:var(--text);color:var(--surface);display:flex;align-items:center;justify-content:center')}>
          <IconQuill size={14} />
        </div>
        {!narrow && <span style={sx('font-weight:700;font-size:14px;letter-spacing:-.2px')}>SpecQuill</span>}
      </div>

      {/* branch switcher */}
      <div style={sx('position:relative')}>
        <div
          onClick={() => setOpen((v) => !v)}
          style={sx('display:flex;align-items:center;gap:6px;padding:4px 9px;border:1px solid var(--border-2);border-radius:7px;cursor:pointer;background:var(--surface-2)')}
        >
          <IconBranch />
          <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:11.5px;font-weight:500")}>{app.branch}</span>
          {app.isProtectedBranch && <span title="protected — edits move to your workspace" style={sx('display:inline-flex;color:var(--text-3)')}><IconLock /></span>}
          <span style={sx('color:var(--text-3)')}><IconChevD /></span>
        </div>
        {app.branch.startsWith('ws/') && (
          <span style={sx('position:absolute;left:0;top:32px;font-size:9px;color:var(--ai);font-weight:700;letter-spacing:.4px;white-space:nowrap')}>PERSONAL WORKSPACE</span>
        )}
        {open && (
          <div style={sx('position:absolute;left:0;top:34px;min-width:230px;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow-lg);overflow:hidden;z-index:20')}>
            {(branches.data || []).map((b) => (
              <div
                key={b.name}
                onClick={() => { app.switchBranch(b.name); setOpen(false); }}
                style={sx('display:flex;align-items:center;gap:8px;padding:8px 12px;cursor:pointer;font-size:12.5px;' + (b.name === app.branch ? 'background:var(--surface-2);font-weight:600' : ''))}
              >
                <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:11.5px;flex:1")}>{b.name}</span>
                {app.protectedBranches.includes(b.name) && <span title="protected" style={sx('display:inline-flex;color:var(--text-3)')}><IconLock /></span>}
                {b.isDefault && <span style={sx('font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:4px;padding:1px 5px')}>default</span>}
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
          style={sx('width:340px;height:30px;display:flex;align-items:center;gap:8px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text-3);cursor:pointer')}
        >
          <IconSearch />
          <span style={sx('font-size:12.5px')}>Search requirements, specs, fields, changes…</span>
          <div style={sx('flex:1')} />
          <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:11px;padding:1px 5px;border:1px solid var(--border-2);border-radius:4px")}>⌘K</span>
        </div>
      )}
      <div style={sx('flex:1')} />

      {!narrow && (
        <div
          title={sync.isPending ? 'syncing…' : `ahead ${ahead} / behind ${behind} — click to fetch${ahead > 0 ? ' + push' : ''}`}
          onClick={() => !sync.isPending && sync.mutate({ push: ahead > 0 })}
          style={sx("display:flex;align-items:center;gap:5px;font-family:'IBM Plex Mono',monospace;font-size:11.5px;color:var(--text-2);padding:4px 8px;border:1px solid var(--border);border-radius:7px;cursor:pointer;" + (sync.isPending ? 'opacity:.5' : ''))}
        >
          <IconUp />{ahead} <IconDown />{behind}
        </div>
      )}
      <button
        onClick={() => (branchPR ? nav(`/prs/${branchPR.number}`) : setPrDialog(true))}
        title={branchPR ? `open PR #${branchPR.number} for ${app.branch}` : 'create a PR from ' + app.branch}
        style={sx('flex:none;display:flex;align-items:center;gap:6px;height:30px;padding:0 ' + (narrow ? '9px' : '12px') + ';border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}
      >
        <IconPR /> {narrow ? (branchPR ? `#${branchPR.number}` : 'PR') : branchPR ? `PR #${branchPR.number}` : 'Open PR'}
      </button>
      {prDialog && <CreatePRDialog onClose={() => setPrDialog(false)} />}
      <div
        title={me.data ? `${me.data.name} <${me.data.email}> — click to sign out` : ''}
        onClick={logout}
        style={sx('width:28px;height:28px;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));display:flex;align-items:center;justify-content:center;color:#fff;font-weight:600;font-size:11px;cursor:pointer')}
      >
        {me.data?.initials || '…'}
      </div>
    </header>
  );
}
