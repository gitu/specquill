// Thin fetch wrapper for the specquill API. Every non-GET carries the
// X-SpecQuill header (CSRF guard, enforced server-side from M3 on).

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// Forge-PAT deployments keep the personal access token here — it is the
// credential of record; sessions are just its short-lived server-side shadow.
const PAT_KEY = 'specquill-pat';

export const getStoredPat = () => localStorage.getItem(PAT_KEY);
export const storePat = (token: string) => localStorage.setItem(PAT_KEY, token);
export const clearStoredPat = () => localStorage.removeItem(PAT_KEY);

// One in-flight re-login at a time: a burst of 401s (page load after a server
// restart) must not fire N parallel logins.
let reauth: Promise<boolean> | null = null;

/** Re-establish the session from the stored PAT. Resolves false when there is
 * no stored token or the login failed.
 *
 * The token is only forgotten when the forge itself rejects it (401 invalid,
 * 403 no access) — a 5xx or a dead network means "try again later", and
 * discarding the token there would force everyone to re-mint one over a
 * transient outage. */
function reloginWithPat(): Promise<boolean> {
  if (reauth) return reauth; // a login is already in flight — join it
  const attempt = (async () => {
    const token = getStoredPat();
    if (!token) return false;
    try {
      const res = await fetch('/auth/pat/login', {
        method: 'POST',
        headers: { 'X-SpecQuill': '1', 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      });
      if (res.status === 401 || res.status === 403) clearStoredPat();
      return res.ok;
    } catch {
      return false; // offline/aborted — keep the token, retry on the next 401
    }
  })();
  reauth = attempt;
  // released as soon as it settles: callers already in flight hold the
  // promise itself, and a LATER 401 deserves a fresh attempt rather than
  // this one's stale verdict
  void attempt.finally(() => { if (reauth === attempt) reauth = null; });
  return attempt;
}

/** fetch + session care: a 401 triggers one silent PAT re-login and retry;
 * without a recoverable session the browser goes to the login page. */
export async function authFetch(path: string, init: RequestInit): Promise<Response> {
  let res = await fetch(path, init);
  if (res.status === 401 && !path.startsWith('/auth/')) {
    if (await reloginWithPat()) {
      res = await fetch(path, init);
    }
    if (res.status === 401) {
      window.location.href = '/auth/login';
      throw new ApiError(401, 'unauthenticated');
    }
  }
  return res;
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await authFetch(path, {
    ...init,
    headers: {
      'X-SpecQuill': '1',
      // FormData bodies set their own multipart boundary
      ...(init?.body && !(init.body instanceof FormData) ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = ((await res.json()) as { error?: string }).error || msg; } catch { /* keep statusText */ }
    throw new ApiError(res.status, msg);
  }
  return res.json() as Promise<T>;
}

/** URL serving a repo file's raw bytes (images embedded in documents). */
export function rawUrl(repo: string, ref: string, path: string): string {
  return `/api/repos/${repo}/raw/${path}?ref=${encodeURIComponent(ref)}`;
}

/** binary-safe file save (excalidraw PNGs); same baseSha contract as PUT files */
export async function putRaw(repo: string, branch: string, path: string, body: Blob, baseSha: string): Promise<{ sha: string }> {
  const res = await authFetch(`/api/repos/${repo}/raw/${path}?branch=${encodeURIComponent(branch)}&baseSha=${encodeURIComponent(baseSha)}`, {
    method: 'PUT',
    headers: { 'X-SpecQuill': '1' },
    body,
  });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = ((await res.json()) as { error?: string }).error || msg; } catch { /* keep */ }
    throw new ApiError(res.status, msg);
  }
  return res.json() as Promise<{ sha: string }>;
}

/** multipart image upload into the branch worktree; returns the repo-relative path */
export async function uploadAsset(repo: string, branch: string, dir: string, file: File): Promise<{ path: string; sha: string }> {
  const form = new FormData();
  form.append('file', file, file.name || 'pasted-image.png');
  return api<{ path: string; sha: string }>(
    `/api/repos/${repo}/assets?branch=${encodeURIComponent(branch)}&dir=${encodeURIComponent(dir)}`,
    { method: 'POST', body: form },
  );
}

export interface RepoInfo {
  id: string;
  kind: 'project' | 'source';
  mode: 'writable' | 'readonly'; // legacy alias of kind
  contentRoot?: string;          // monorepo projects: subfolder the API roots at
  okf?: boolean;                 // source is an OKF bundle
  importer?: string;             // non-git source kind: url | openapi | confluence
  syncStatus?: 'ok' | 'error';   // last import outcome (importer sources)
  syncError?: string;
  defaultBranch: string;
  protectedBranches: string[];
  syncedAt?: string;
  role?: 'viewer' | 'editor' | 'maintainer' | 'admin'; // the caller's effective role on this repo (REQ-021)
  mergeMode?: 'local' | 'forge'; // how work lands on main: in-app merge, or push + MR/PR on the forge
}
export interface Branch { name: string; head: string; isDefault: boolean; ahead: number; behind: number }
export interface FileResp { content: string; sha: string }
export interface SnapshotResp { ref: string; files: Record<string, string> }
export interface TreeEntry { path: string; size: number }
