// Token-scoped dynamic projects (REQ-025): feature probe, open, search and
// the per-user checkout overview. Everything runs through the caller's own
// forge token server-side.

import { useQuery } from '@tanstack/react-query';
import { api, authFetch } from './client';

export interface DynamicInfo {
  enabled: boolean;
  search?: boolean;
  host?: string;   // the deployment's forge host, e.g. github.com
  budget?: number; // per-user byte budget
}

export interface DynRepo {
  id: string;
  path: string;
  remote: string;
  defaultBranch: string;
  webUrl: string;
}

export interface OpenedProject {
  id: string;
  name: string;
  spelling: string;
  root: string;
  readonly: boolean;
  role: string;
}

export interface Checkout {
  repoId: string;
  kind: 'project' | 'source' | 'dynamic';
  spelling?: string;
  role?: string;
  bytes: number;
  lastUsed: number;
  unsynced: boolean;
  materialized: boolean;
}

export interface CheckoutsResp { checkouts: Checkout[]; budget: number; used: number }

export function useDynamicInfo() {
  return useQuery({ queryKey: ['dynamic'], queryFn: () => api<DynamicInfo>('/api/dynamic'), staleTime: 60_000 });
}

export function useCheckouts(enabled: boolean) {
  return useQuery({
    queryKey: ['checkouts'],
    queryFn: () => api<CheckoutsResp>('/api/dynamic/checkouts'),
    enabled,
  });
}

export const searchDynamic = (q: string) =>
  api<{ repos: DynRepo[] }>('/api/dynamic/search?q=' + encodeURIComponent(q));

/** open outcome: opened, a pick-one list (multi-workspace manifest), or an error */
export type OpenResult =
  | { kind: 'opened'; project: OpenedProject }
  | { kind: 'choose'; choices: { name: string; root: string }[] }
  | { kind: 'error'; message: string; code?: string };

export async function openDynamic(spec: string): Promise<OpenResult> {
  const res = await authFetch('/api/dynamic/open', {
    method: 'POST',
    headers: { 'X-SpecQuill': '1', 'Content-Type': 'application/json' },
    body: JSON.stringify({ spec }),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (res.ok) return { kind: 'opened', project: body as unknown as OpenedProject };
  if (body.code === 'choose_project') {
    return { kind: 'choose', choices: (body.choices as { name: string; root: string }[]) || [] };
  }
  return { kind: 'error', message: String(body.error || res.statusText), code: body.code as string | undefined };
}

export const reclaimCheckout = (id: string, opts?: { force?: boolean; close?: boolean }) =>
  api<{ ok: boolean }>('/api/dynamic/reclaim', {
    method: 'POST',
    body: JSON.stringify({ id, force: !!opts?.force, close: !!opts?.close }),
  });

export function fmtBytes(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(1) + ' GB';
  if (n >= 1e6) return (n / 1e6).toFixed(1) + ' MB';
  if (n >= 1e3) return (n / 1e3).toFixed(0) + ' kB';
  return n + ' B';
}
