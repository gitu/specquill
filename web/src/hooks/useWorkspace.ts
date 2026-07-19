// useWorkspace — transparently moves edits off protected branches onto the
// caller's personal workspace branch (ws/<user>), created/ff'd server-side.
import { useCallback, useRef } from 'react';
import { useTenant } from '../api/hooks';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useApp } from '../state/AppContext';
import { useToasts } from '../components/Toast';

export interface WorkspaceState {
  branch: string;
  created: boolean;
  state: 'current' | 'behind' | 'ahead' | 'diverged' | 'dirty';
  base: string;
}

const STATE_NOTE: Record<string, string> = {
  current: '',
  behind: ' (behind main — update blocked, commit or discard first)',
  ahead: ' (has unmerged commits — open a PR when ready)',
  diverged: ' (diverged from main — open a PR when ready)',
  dirty: ' (picking up your uncommitted changes)',
};

export function useWorkspace() {
  const tenant = useTenant();
  const app = useApp();
  const qc = useQueryClient();
  const toasts = useToasts();
  const inFlight = useRef<Promise<string> | null>(null);

  /**
   * Guarantees the current branch is writable, switching to (and creating if
   * needed) the personal workspace when the user is on a protected branch.
   * Single-flight: concurrent triggers await the same switch.
   */
  const ensureWritableBranch = useCallback((): Promise<string> => {
    if (!app.isProtectedBranch) return Promise.resolve(app.branch);
    if (inFlight.current) return inFlight.current;
    const p = (async () => {
      const ws = await api<WorkspaceState>(`/api/repos/${app.repoId}/workspace`, { method: 'POST', body: '{}' });
      app.switchBranch(ws.branch, { carryDraft: true });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'branches', app.repoId] });
      toasts.push({
        text: `${app.branch} is protected — you're now on your workspace ${ws.branch}` +
          (ws.created ? ' (created from main)' : STATE_NOTE[ws.state] || ''),
        kind: ws.state === 'diverged' || ws.state === 'behind' ? 'warn' : 'info',
      });
      return ws.branch;
    })();
    inFlight.current = p;
    p.finally(() => { inFlight.current = null; });
    return p;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app.isProtectedBranch, app.branch, app.repoId]);

  return { ensureWritableBranch };
}
