import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { usePull, useStatus, useUpdateWorkspace } from '../api/hooks';
import { useToasts } from './Toast';

/**
 * Staleness banners under the top bar. The server auto-fast-forwards clean
 * branches after every fetch, so these mostly point at the cases it cannot
 * fix: diverged-from-origin (resolve via PR), behind-origin as a fallback
 * (self-heals within a sync interval) and, for non-default branches,
 * behind-local-main (offer a workspace update).
 */
export function SyncBanner() {
  const app = useApp();
  const toasts = useToasts();
  const status = useStatus(app.repoId, app.branch);
  const pull = usePull(app.repoId, app.branch);
  const updateWs = useUpdateWorkspace(app.repoId);

  const behind = status.data?.behind ?? 0;
  const behindDefault = status.data?.behindDefault ?? 0;
  const ahead = status.data?.ahead ?? 0;
  const isWs = app.branch.startsWith('ws/');

  const doPull = async () => {
    try {
      await pull.mutateAsync();
      toasts.push({ text: `${app.branch} updated from origin`, kind: 'success' });
    } catch (e) {
      const code = (e as { message?: string }).message || '';
      toasts.push({ text: `Update failed: ${code}`, kind: 'warn' });
    }
  };
  const doUpdateWs = async () => {
    const res = await updateWs.mutateAsync();
    toasts.push(res.state === 'current'
      ? { text: `${res.branch} updated to the latest main`, kind: 'success' }
      : res.heldByRoom
        ? { text: 'A live co-editing session is open on this branch — the update runs once it ends', kind: 'info' }
        : { text: `${res.branch} has its own commits — open a PR to bring main up to date`, kind: 'warn' });
  };

  if (behind > 0 && ahead > 0) {
    // the one state auto-sync can never fix — point at it, offer no ff
    return (
      <Banner
        text={`${app.branch} has ${ahead} local commit${ahead === 1 ? '' : 's'} and origin moved — merge via PR or resolve manually`}
      />
    );
  }
  if (behind > 0) {
    // fallback: auto-ff catches this within a sync interval (unless a draft
    // or live room holds the branch) — the button remains for immediacy
    return (
      <Banner
        text={`${app.branch} is ${behind} commit${behind === 1 ? '' : 's'} behind origin`}
        action={pull.isPending ? 'Updating…' : 'Update'}
        onAction={doPull}
      />
    );
  }
  if (behindDefault > 0 && ahead === 0 && isWs) {
    return (
      <Banner
        text={`main moved — your workspace is ${behindDefault} commit${behindDefault === 1 ? '' : 's'} behind`}
        action={updateWs.isPending ? 'Updating…' : 'Update workspace'}
        onAction={doUpdateWs}
      />
    );
  }
  return null;
}

function Banner({ text, action, onAction }: { text: string; action?: string; onAction?: () => void }) {
  return (
    <div data-banner style={sx('flex:none;display:flex;align-items:center;gap:12px;padding:7px 16px;background:var(--prod-bg);border-bottom:1px solid var(--prod-line);color:var(--prod);font-size:12.5px')}>
      <span style={sx('font-weight:600')}>{text}</span>
      <div style={sx('flex:1')} />
      {action && (
        <button onClick={onAction}
          style={sx('height:26px;padding:0 12px;border:none;border-radius:7px;background:var(--prod);color:#fff;font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
          {action}
        </button>
      )}
    </div>
  );
}
