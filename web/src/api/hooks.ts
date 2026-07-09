import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, Branch, FileResp, RepoInfo, SnapshotResp, TreeEntry } from './client';

export interface StatusResp {
  branch: string;
  dirty: { path: string; state: string }[];
  ahead: number;
  behind: number;
  behindDefault: number;
}

export interface Me { id: number; name: string; email: string; provider: string; initials: string }

export function useMe() {
  return useQuery({ queryKey: ['me'], queryFn: () => api<Me>('/api/me'), staleTime: 60_000 });
}

export function useRepos() {
  return useQuery({ queryKey: ['repos'], queryFn: () => api<RepoInfo[]>('/api/repos') });
}

export interface ProjectRef { source: string; kind: string; okf?: boolean; grounding: boolean; paths?: string[] }
export interface ProjectInfo {
  id: string; contentRoot?: string; defaultBranch: string; managedBy: string;
  references: ProjectRef[]; warnings?: string[];
}
export function useProjects() {
  return useQuery({ queryKey: ['projects'], queryFn: () => api<ProjectInfo[]>('/api/projects') });
}

export interface PresencePeer { connId: number; userId: number; name: string }
export interface PresenceRoom { branch: string; path: string; users: PresencePeer[]; orphaned: boolean }

/** who is co-editing what (live rooms + orphaned unflushed sessions) */
export function usePresence(repo: string | undefined) {
  return useQuery({
    queryKey: ['presence', repo],
    queryFn: () => api<PresenceRoom[]>(`/api/repos/${repo}/presence`),
    enabled: !!repo,
    refetchInterval: 10_000,
  });
}

export function useSnapshot(repo: string | undefined, ref: string) {
  return useQuery({
    queryKey: ['snapshot', repo, ref],
    queryFn: () => api<SnapshotResp>(`/api/repos/${repo}/snapshot?ref=${encodeURIComponent(ref)}`),
    enabled: !!repo,
    staleTime: 5_000,
  });
}

export function useFileQuery(repo: string | undefined, ref: string, path: string | undefined) {
  return useQuery({
    queryKey: ['file', repo, ref, path],
    queryFn: () => api<FileResp>(`/api/repos/${repo}/files/${path}?ref=${encodeURIComponent(ref)}`),
    enabled: !!repo && !!path,
  });
}

export function useBranches(repo: string | undefined) {
  return useQuery({
    queryKey: ['branches', repo],
    queryFn: () => api<Branch[]>(`/api/repos/${repo}/branches`),
    enabled: !!repo,
  });
}

export function useTree(repo: string | undefined, ref: string) {
  return useQuery({
    queryKey: ['tree', repo, ref],
    queryFn: () => api<TreeEntry[]>(`/api/repos/${repo}/tree?ref=${encodeURIComponent(ref)}`),
    enabled: !!repo,
  });
}

export function useStatus(repo: string | undefined, branch: string) {
  return useQuery({
    queryKey: ['status', repo, branch],
    queryFn: () => api<StatusResp>(`/api/repos/${repo}/status?branch=${encodeURIComponent(branch)}`),
    enabled: !!repo,
    refetchInterval: 15_000,
  });
}

// ---------------------------------------------------------------- PRs

export interface PRUser { id: number; name: string; email: string }
export interface PRApproval { user: PRUser; commitSha: string; createdAt: number; current: boolean }
export interface PR {
  repo: string; number: number; title: string; body: string;
  source: string; target: string; author: PRUser; state: 'open' | 'merged' | 'closed';
  mergedCommit?: string; createdAt: number; mergedAt?: number;
  headSha: string; mergeable?: boolean; conflicts?: string[];
  approvals: PRApproval[]; commentCount: number;
}
export interface DiffLine { op: string; text: string }
export interface DiffHunk { header: string; lines: DiffLine[] }
export interface DiffFile {
  path: string; oldPath?: string; status: string;
  additions: number; deletions: number; binaryLike: boolean; hunks: DiffHunk[] | null;
}
export interface PRComment {
  id: number; author: PRUser; filePath?: string; line?: number;
  anchoredCommit?: string; body: string; resolved: boolean; createdAt: number; outdated: boolean;
}

export function usePRs(repo: string | undefined, state = 'open') {
  return useQuery({
    queryKey: ['prs', repo, state],
    queryFn: () => api<PR[]>(`/api/repos/${repo}/prs?state=${state}`),
    enabled: !!repo,
  });
}

export function usePR(repo: string | undefined, n: number | undefined) {
  return useQuery({
    queryKey: ['pr', repo, n],
    queryFn: () => api<PR>(`/api/repos/${repo}/prs/${n}`),
    enabled: !!repo && !!n,
  });
}

export function usePRDiff(repo: string | undefined, n: number | undefined) {
  return useQuery({
    queryKey: ['prdiff', repo, n],
    queryFn: () => api<{ files: DiffFile[] }>(`/api/repos/${repo}/prs/${n}/diff`),
    enabled: !!repo && !!n,
  });
}

export function usePRComments(repo: string | undefined, n: number | undefined) {
  return useQuery({
    queryKey: ['prcomments', repo, n],
    queryFn: () => api<PRComment[]>(`/api/repos/${repo}/prs/${n}/comments`),
    enabled: !!repo && !!n,
  });
}

export function useCreatePR(repo: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { title: string; body?: string; source: string; target?: string }) =>
      api<PR>(`/api/repos/${repo}/prs`, { method: 'POST', body: JSON.stringify(body) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['prs', repo] }),
  });
}

export function usePRAction(repo: string | undefined, n: number | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ action, payload }: { action: 'approve' | 'merge' | 'close' | 'comments'; payload?: unknown }) =>
      api(`/api/repos/${repo}/prs/${n}/${action}`, { method: 'POST', body: JSON.stringify(payload ?? {}) }),
    onSuccess: (_, { action }) => {
      qc.invalidateQueries({ queryKey: ['pr', repo, n] });
      qc.invalidateQueries({ queryKey: ['prs', repo] });
      qc.invalidateQueries({ queryKey: ['prcomments', repo, n] });
      if (action === 'merge') {
        qc.invalidateQueries({ queryKey: ['snapshot'] });
        qc.invalidateQueries({ queryKey: ['branches', repo] });
        qc.invalidateQueries({ queryKey: ['status'] });
      }
    },
  });
}

// ---------------------------------------------------------------- mutations

export function useWorktreeDiff(repo: string | undefined, branch: string, enabled: boolean) {
  return useQuery({
    queryKey: ['worktreediff', repo, branch],
    queryFn: () => api<{ files: DiffFile[] }>(`/api/repos/${repo}/diff/worktree?branch=${encodeURIComponent(branch)}`),
    enabled: !!repo && enabled,
    refetchInterval: enabled ? 5_000 : false,
  });
}

// committed baseline (object db), for the changed-line gutter in source mode
export function useFileAtHead(repo: string | undefined, branch: string, path: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: ['fileathead', repo, branch, path],
    queryFn: () => api<FileResp>(`/api/repos/${repo}/files/${path}?ref=${encodeURIComponent(branch)}&at=head`),
    enabled: !!repo && !!path && enabled,
    staleTime: 10_000,
  });
}

export function usePull(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      api<{ head: string; updated: boolean }>(`/api/repos/${repo}/pull?branch=${encodeURIComponent(branch)}`, { method: 'POST', body: '{}' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, branch] });
      qc.invalidateQueries({ queryKey: ['file', repo, branch] });
      qc.invalidateQueries({ queryKey: ['branches', repo] });
    },
  });
}

export function useUpdateWorkspace(repo: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      api<{ branch: string; state: string; heldByRoom?: boolean }>(`/api/repos/${repo}/workspace`, { method: 'POST', body: '{}' }),
    onSuccess: (_, __) => {
      qc.invalidateQueries(); // workspace ff moves the branch head — refresh broadly
    },
  });
}

export function useSaveFile(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ path, content, baseSha }: { path: string; content: string; baseSha: string }) =>
      api<{ sha: string }>(`/api/repos/${repo}/files/${path}?branch=${encodeURIComponent(branch)}`, {
        method: 'PUT',
        body: JSON.stringify({ content, baseSha }),
      }),
    onSuccess: (_, { path }) => {
      qc.invalidateQueries({ queryKey: ['file', repo, branch, path] });
      qc.invalidateQueries({ queryKey: ['status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, branch] });
    },
  });
}

export function useDeleteFile(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ path }: { path: string }) =>
      api<{ ok: boolean }>(`/api/repos/${repo}/files/${path}?branch=${encodeURIComponent(branch)}`, { method: 'DELETE' }),
    onSuccess: (_, { path }) => {
      qc.invalidateQueries({ queryKey: ['file', repo, branch, path] });
      qc.invalidateQueries({ queryKey: ['status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, branch] });
    },
  });
}

export function useCommit(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ message, paths }: { message: string; paths?: string[] }) =>
      api<{ commitSha: string }>(`/api/repos/${repo}/commit?branch=${encodeURIComponent(branch)}`, {
        method: 'POST',
        body: JSON.stringify({ message, paths }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['branches', repo] });
    },
  });
}

export function useCreateBranch(repo: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, from }: { name: string; from: string }) =>
      api<{ name: string }>(`/api/repos/${repo}/branches`, {
        method: 'POST',
        body: JSON.stringify({ name, from }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['branches', repo] }),
  });
}

export function useSync(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ push }: { push: boolean }) => {
      await api(`/api/repos/${repo}/fetch`, { method: 'POST', body: '{}' });
      if (push) await api(`/api/repos/${repo}/push?branch=${encodeURIComponent(branch)}`, { method: 'POST', body: '{}' });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['branches', repo] });
      qc.invalidateQueries({ queryKey: ['repos'] });
    },
  });
}
