import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, Branch, FileResp, RepoInfo, SnapshotResp, TreeEntry } from './client';
import { ForgeKind } from '../lib/forge';

export interface StatusResp {
  branch: string;
  dirty: { path: string; state: string }[];
  ahead: number;
  behind: number;
  behindDefault: number;
}

export interface Me {
  id: number; name: string; email: string; provider: string; initials: string; role: string;
  mergeMode?: 'local' | 'forge';
}

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
// ref = the selected branch: references/warnings follow its .specquill/config.yml
// (the server falls back to the default branch for projects without that branch)
export function useProjects(ref?: string) {
  const url = ref ? `/api/projects?ref=${encodeURIComponent(ref)}` : '/api/projects';
  return useQuery({ queryKey: ['projects', ref ?? ''], queryFn: () => api<ProjectInfo[]>(url) });
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

// ---------------------------------------------------------------- diffs

export interface DiffLine { op: string; text: string }
export interface DiffHunk { header: string; lines: DiffLine[] }
export interface DiffFile {
  path: string; oldPath?: string; status: string;
  additions: number; deletions: number; binaryLike: boolean; hunks: DiffHunk[] | null;
}

// ---------------------------------------------------------------- merging

export interface MergePreview {
  source: string; target: string;
  dirty?: string[]; mergeable?: boolean; conflicts?: string[]; files: DiffFile[];
}

/** What merging `source` into `target` would land — diff, conflicts, dirty files. */
export function useMergePreview(repo: string | undefined, source: string | undefined, target: string | undefined) {
  return useQuery({
    queryKey: ['mergepreview', repo, source, target],
    queryFn: () => api<MergePreview>(
      `/api/repos/${repo}/merge?source=${encodeURIComponent(source!)}&target=${encodeURIComponent(target ?? '')}`),
    enabled: !!repo && !!source,
  });
}

/** Land a branch on the target. 409 `dirty` means commit first. */
export function useMerge(repo: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { source: string; target?: string; strategy?: string; message?: string }) =>
      api<{ mergedCommit: string }>(`/api/repos/${repo}/merge`, { method: 'POST', body: JSON.stringify(body) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mergepreview', repo] });
      qc.invalidateQueries({ queryKey: ['snapshot'] });
      qc.invalidateQueries({ queryKey: ['branches', repo] });
      qc.invalidateQueries({ queryKey: ['status'] });
      qc.invalidateQueries({ queryKey: ['history'] });
    },
  });
}


export interface ProposeResp {
  number: number; url: string; title: string; created: boolean;
  kind?: ForgeKind; // names the object the way the host does (MR vs PR)
}

/** Forge mode: push the branch and open (or re-use) a merge request there. */
export function usePropose(repo: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { source: string; title?: string; body?: string }) =>
      api<ProposeResp>(
        `/api/repos/${repo}/propose`, { method: 'POST', body: JSON.stringify(body) }),
    onSuccess: (_, { source }) => {
      qc.invalidateQueries({ queryKey: ['forge', repo, source] });
      qc.invalidateQueries({ queryKey: ['status', repo, source] });
    },
  });
}

// ---------------------------------------------------------------- forge

export interface ForgeComment {
  author: string; body: string; path?: string; line?: number; createdAt: string; url?: string;
}
export interface ForgeRequest {
  number: number; title: string; state: string; author: string; url: string; comments: ForgeComment[];
}
export interface ForgeResp {
  enabled: boolean;
  kind?: ForgeKind;                // gitlab | github — drives MR/PR wording
  request?: ForgeRequest | null;   // null = the branch has no open merge request
  error?: string;                  // forge unreachable/misconfigured — panel degrades
}

/** The open merge request for a branch on the configured forge, read-only. */
export function useForgeRequest(repo: string | undefined, branch: string | undefined) {
  return useQuery({
    queryKey: ['forge', repo, branch],
    queryFn: () => api<ForgeResp>(`/api/repos/${repo}/forge/request?branch=${encodeURIComponent(branch!)}`),
    enabled: !!repo && !!branch,
    staleTime: 60_000,
    retry: false,
  });
}

// ---------------------------------------------------------------- source drift

export interface DriftEvidence { path: string; quote: string }
export type DriftMode = 'drift' | 'gaps' | 'extract';
export interface DriftFinding {
  fingerprint: string; docPath: string; anchor: string; source: string;
  // coverage gaps (docPath '') carry where the missing doc should live and
  // the reverse-engineered draft once one was created
  suggestedPath: string; draftPath: string;
  // the in-repo change/work-item document created to remedy it
  remedyPath: string; remedyKind: string;
  kind: string; severity: 'high' | 'medium' | 'low'; title: string; detail: string;
  evidence: DriftEvidence[]; status: 'open' | 'dismissed' | 'filed';
  workItemUrl: string; workItemTarget: string; updatedAt: number;
}
export interface DriftRun {
  id: number; mode: DriftMode; status: 'running' | 'ok' | 'error' | 'cancelled'; error: string;
  scope: string[]; docsTotal: number; docsDone: number; droppedUnverified: number;
  headSha: string; startedAt: number; finishedAt: number;
  activity: string[];             // live per-unit narration of the run
  reportPath: string;             // git-native run report doc ('' = none)
  reportBranch: string;
}
export interface DriftTarget { name: string; kind: string; project: string }
export interface DriftResp {
  enabled: boolean; run: DriftRun | null; findings: DriftFinding[]; targets: DriftTarget[];
  sources: string[]; // the references a gaps run would sweep
  reports: string[]; // existing report docs a run can continue (incl. the default)
  defaultReport: string; // the project's standing report (its drift.report:)
  // the analyzed application inventories persisted beside the report
  extractions: { source: string; path: string }[];
}

/** Latest source-drift run + live findings; polls while a run is in flight. */
export function useDrift(repo: string | undefined, branch: string) {
  return useQuery({
    queryKey: ['drift', repo, branch],
    queryFn: () => api<DriftResp>(`/api/repos/${repo}/drift?branch=${encodeURIComponent(branch)}`),
    enabled: !!repo,
    refetchInterval: (q) => (q.state.data?.run?.status === 'running' ? 2_500 : false),
  });
}

export function useRunDrift(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    // `branch` overrides the hook's branch: the caller may have just been
    // moved onto their workspace branch and the run must target THAT one
    mutationFn: ({ branch: on, ...body }: { mode?: DriftMode; paths?: string[]; report?: string; branch?: string; sources?: string[]; focus?: string }) =>
      api<{ runId: number; docsTotal: number; mode: DriftMode }>(
        `/api/repos/${repo}/drift/run?branch=${encodeURIComponent(on || branch)}`,
        { method: 'POST', body: JSON.stringify(body) }),
    // prefix key: a run started on a freshly switched branch must refresh too
    onSuccess: () => qc.invalidateQueries({ queryKey: ['drift', repo] }),
  });
}

/** Create the in-repo change record / work item that tracks fixing a finding. */
export function useRemedyFinding(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ fingerprint, kind }: { fingerprint: string; kind: 'change' | 'work_item' }) =>
      api<{ path: string; kind: string; branch: string; linked?: string; existing?: boolean }>(
        `/api/repos/${repo}/drift/findings/${fingerprint}/remedy?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: JSON.stringify({ kind }) }),
    onSuccess: (resp) => {
      qc.invalidateQueries({ queryKey: ['drift', repo, branch] });
      // the remedy doc (and possibly the affected doc) are worktree saves
      qc.invalidateQueries({ queryKey: ['file', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['status', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['tree', repo, resp.branch] });
    },
  });
}

export interface FocusArea { name: string; reason: string; sources: string[] }

/** Ask where to aim the next gap sweep (read-only: no run, no writes). */
export function useFocusAreas(repo: string | undefined, branch: string) {
  return useMutation({
    mutationFn: (sources?: string[]) =>
      api<{ areas: FocusArea[] }>(
        `/api/repos/${repo}/drift/focus?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: JSON.stringify({ sources: sources ?? [] }) }),
  });
}

/** Reverse-engineer the missing requirement doc from a coverage-gap finding. */
export function useDraftRequirement(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ fingerprint }: { fingerprint: string }) =>
      api<{ path: string; branch: string }>(
        `/api/repos/${repo}/drift/findings/${fingerprint}/draft?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: '{}' }),
    onSuccess: (resp) => {
      qc.invalidateQueries({ queryKey: ['drift', repo, branch] });
      // the draft is an uncommitted save on resp.branch
      qc.invalidateQueries({ queryKey: ['status', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['tree', repo, resp.branch] });
    },
  });
}

export function useCancelDrift(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      api<{ ok: boolean }>(`/api/repos/${repo}/drift/cancel?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: '{}' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['drift', repo, branch] }),
  });
}

export function useDismissFinding(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ fingerprint, reopen }: { fingerprint: string; reopen?: boolean }) =>
      api<{ status: string }>(
        `/api/repos/${repo}/drift/findings/${fingerprint}/dismiss?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: JSON.stringify({ reopen: !!reopen }) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['drift', repo, branch] }),
  });
}

export interface FileFindingResp {
  url: string; created: boolean; target: string;
  backlinked: boolean; backlinkBranch?: string; backlinkError?: string;
}

/** File a finding as a work item; the backlink save may touch an open doc. */
export function useFileFinding(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ fingerprint, target }: { fingerprint: string; target: string; docPath: string }) =>
      api<FileFindingResp>(
        `/api/repos/${repo}/drift/findings/${fingerprint}/file?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: JSON.stringify({ target }) }),
    onSuccess: (resp, { docPath }) => {
      qc.invalidateQueries({ queryKey: ['drift', repo, branch] });
      // the backlink is a worktree save — refresh an open editor instead of
      // letting it stale into a baseSha conflict
      const wb = resp.backlinkBranch ?? branch;
      qc.invalidateQueries({ queryKey: ['file', repo, wb, docPath] });
      qc.invalidateQueries({ queryKey: ['status', repo, wb] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, wb] });
    },
  });
}

// ---------------------------------------------------------------- linker

export interface LinkProposal { from: string; field: string; to: string; reason?: string }

/** Ask the AI for missing typed links between documents (nothing is written). */
export function useProposeLinks(repo: string | undefined, branch: string) {
  return useMutation({
    mutationFn: () =>
      api<{ proposals: LinkProposal[]; dropped: number }>(
        `/api/repos/${repo}/linker/propose?branch=${encodeURIComponent(branch)}`,
        { method: 'POST', body: '{}' }),
  });
}

/** Write accepted link proposals into the from-docs' frontmatter (drafts). */
export function useApplyLinks(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ links, branch: on }: { links: LinkProposal[]; branch?: string }) =>
      api<{ applied: LinkProposal[]; failures: string[]; branch: string }>(
        `/api/repos/${repo}/linker/apply?branch=${encodeURIComponent(on || branch)}`,
        { method: 'POST', body: JSON.stringify({ links }) }),
    onSuccess: (resp) => {
      qc.invalidateQueries({ queryKey: ['file', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['status', repo, resp.branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, resp.branch] });
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

// reject pending (uncommitted) worktree changes; no paths = everything
export function useDiscard(repo: string | undefined, branch: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ paths }: { paths?: string[] }) =>
      api<{ ok: boolean }>(`/api/repos/${repo}/discard?branch=${encodeURIComponent(branch)}`, {
        method: 'POST',
        body: JSON.stringify({ paths: paths || [] }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['file', repo, branch] });
      qc.invalidateQueries({ queryKey: ['fileathead', repo, branch] });
      qc.invalidateQueries({ queryKey: ['status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo, branch] });
      qc.invalidateQueries({ queryKey: ['worktreediff', repo, branch] });
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
