// Thin fetch wrapper for the reqbase API. Every non-GET carries the
// X-Reqbase header (CSRF guard, enforced server-side from M3 on).

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      'X-Reqbase': '1',
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

/** URL serving a repo file's raw bytes (images embedded in documents) */
export function rawUrl(repo: string, ref: string, path: string): string {
  return `/api/repos/${repo}/raw/${path}?ref=${encodeURIComponent(ref)}`;
}

/** binary-safe file save (excalidraw PNGs); same baseSha contract as PUT files */
export async function putRaw(repo: string, branch: string, path: string, body: Blob, baseSha: string): Promise<{ sha: string }> {
  const res = await fetch(`/api/repos/${repo}/raw/${path}?branch=${encodeURIComponent(branch)}&baseSha=${encodeURIComponent(baseSha)}`, {
    method: 'PUT',
    headers: { 'X-Reqbase': '1' },
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
  mode: 'writable' | 'readonly';
  defaultBranch: string;
  protectedBranches: string[];
  syncedAt?: string;
}
export interface Branch { name: string; head: string; isDefault: boolean; ahead: number; behind: number }
export interface FileResp { content: string; sha: string }
export interface SnapshotResp { ref: string; files: Record<string, string> }
export interface TreeEntry { path: string; size: number }
