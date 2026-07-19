// Thin fetch wrapper for the specquill API. Every non-GET carries the
// X-SpecQuill header (CSRF guard, enforced server-side from M3 on).

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** The tenant named by the current /t/<tenant> route — set synchronously
 * during the tenant layout's render (top-down, before any child queryFn
 * fires). The API path is the only tenant channel (REQ-022). */
let routeTenant = '';
export function setRouteTenant(slug: string) {
  routeTenant = slug;
}

/** Rewrites a tenant-scoped '/api/x' path onto '/api/t/<tenant>/x'. Global
 * endpoints (/api/me, /auth/*) pass through. Throws outside a /t/ route —
 * failing loud beats a cross-tenant request. */
export function apiPath(path: string): string {
  if (!path.startsWith('/api/')) return path;
  const bare = path.split('?')[0];
  if (bare === '/api/me') return path;
  if (!routeTenant) throw new Error('tenant-scoped API call outside /t/ route: ' + path);
  return '/api/t/' + encodeURIComponent(routeTenant) + path.slice('/api'.length);
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiPath(path), {
    ...init,
    headers: {
      'X-SpecQuill': '1',
      // FormData bodies set their own multipart boundary
      ...(init?.body && !(init.body instanceof FormData) ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  });
  if (res.status === 401 && !path.startsWith('/auth/')) {
    window.location.href = '/auth/login';
    throw new ApiError(401, 'unauthenticated');
  }
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = ((await res.json()) as { error?: string }).error || msg; } catch { /* keep statusText */ }
    throw new ApiError(res.status, msg);
  }
  return res.json() as Promise<T>;
}

/** URL serving a repo file's raw bytes (images embedded in documents).
 * The tenant rides the path, so <img> tags need no header or query param. */
export function rawUrl(repo: string, ref: string, path: string): string {
  return apiPath(`/api/repos/${repo}/raw/${path}?ref=${encodeURIComponent(ref)}`);
}

/** binary-safe file save (excalidraw PNGs); same baseSha contract as PUT files */
export async function putRaw(repo: string, branch: string, path: string, body: Blob, baseSha: string): Promise<{ sha: string }> {
  const res = await fetch(apiPath(`/api/repos/${repo}/raw/${path}?branch=${encodeURIComponent(branch)}&baseSha=${encodeURIComponent(baseSha)}`), {
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
}
export interface Branch { name: string; head: string; isDefault: boolean; ahead: number; behind: number }
export interface FileResp { content: string; sha: string }
export interface SnapshotResp { ref: string; files: Record<string, string> }
export interface TreeEntry { path: string; size: number }
