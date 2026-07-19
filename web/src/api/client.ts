// Thin fetch wrapper for the specquill API. Every non-GET carries the
// X-SpecQuill header (CSRF guard, enforced server-side from M3 on).

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** The pinned tenant (multi-tenant setups); '' lets the server infer the
 * user's only membership. Every API call carries it as X-SpecQuill-Tenant. */
export function activeTenant(): string {
  return localStorage.getItem('specquill-tenant') || '';
}

/** Pin a tenant and reload — all cached state is tenant-scoped. */
export function switchTenant(slug: string) {
  if (slug) localStorage.setItem('specquill-tenant', slug);
  else localStorage.removeItem('specquill-tenant');
  window.location.href = '/';
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      'X-SpecQuill': '1',
      ...(activeTenant() ? { 'X-SpecQuill-Tenant': activeTenant() } : {}),
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
 * Carries the tenant as a query param — <img> tags can't send headers. */
export function rawUrl(repo: string, ref: string, path: string): string {
  const tenant = activeTenant() ? `&tenant=${encodeURIComponent(activeTenant())}` : '';
  return `/api/repos/${repo}/raw/${path}?ref=${encodeURIComponent(ref)}${tenant}`;
}

/** binary-safe file save (excalidraw PNGs); same baseSha contract as PUT files */
export async function putRaw(repo: string, branch: string, path: string, body: Blob, baseSha: string): Promise<{ sha: string }> {
  const res = await fetch(`/api/repos/${repo}/raw/${path}?branch=${encodeURIComponent(branch)}&baseSha=${encodeURIComponent(baseSha)}`, {
    method: 'PUT',
    headers: { 'X-SpecQuill': '1', ...(activeTenant() ? { 'X-SpecQuill-Tenant': activeTenant() } : {}) },
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
  role?: 'viewer' | 'member' | 'admin'; // the caller's effective role on this repo
}
export interface Branch { name: string; head: string; isDefault: boolean; ahead: number; behind: number }
export interface FileResp { content: string; sha: string }
export interface SnapshotResp { ref: string; files: Record<string, string> }
export interface TreeEntry { path: string; size: number }
