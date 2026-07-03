import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { CollabSession, SessionUser, acquireSession } from './session';

/**
 * Acquire (and reactively observe) the collab session for a room. The
 * session and its Y.Doc live in a module cache and survive editor remounts.
 *
 * Acquisition happens in an effect — never during render. A render React
 * aborts (suspense, concurrent restarts) would leak the refcount and keep
 * the websocket (and the server-side room) alive forever.
 */
export function useCollabSession(opts: {
  enabled: boolean;
  repo: string | undefined;
  branch: string;
  path: string;
  baseSha: string | undefined;
  initialFm: string;
  me: SessionUser | undefined;
}): CollabSession | null {
  const { enabled, repo, branch, path, baseSha, initialFm, me } = opts;
  const active = enabled && !!repo && !!baseSha && !!me;
  const key = `${repo}:${branch}:${path}`;

  // creation-time snapshot: the session captures these once; later changes
  // must not re-create it (the room is the source of truth from then on)
  const seed = useRef({ baseSha, initialFm, me });
  seed.current = { baseSha, initialFm, me };

  const [session, setSession] = useState<CollabSession | null>(null);
  useEffect(() => {
    if (!active) {
      setSession(null);
      return;
    }
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const url = `${proto}://${location.host}/api/repos/${repo}/collab/${path}?branch=${encodeURIComponent(branch)}`;
    const { session: s, release } = acquireSession(key, () =>
      new CollabSession(key, url, seed.current.baseSha!, seed.current.initialFm, seed.current.me!));
    setSession(s);
    return () => {
      release();
      setSession((cur) => (cur === s ? null : cur));
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, key]);

  useSyncExternalStore(
    (cb) => (session ? session.subscribe(cb) : () => {}),
    // version counter: any emit changes the snapshot identity
    () => (session ? `${session.status}:${session.peers.length}:${session.dirty}:${session.savedSha}:${session.isLeader}` : ''),
  );
  return session;
}
