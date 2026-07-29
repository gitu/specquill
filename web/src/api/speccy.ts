// Speccy API: SSE chat streaming + draft-edit application.
import { useQuery } from '@tanstack/react-query';
import { api } from './client';

export interface ChatMessage { role: 'user' | 'assistant'; content: string }
export interface SpeccyInfo { enabled: boolean; model?: string; groundedSources?: string[] }
export interface DraftResult { branch: string; summary: string; applied: string[]; failures: string[] }

// info is per-project: grounded sources depend on the active project's
// references, read from the selected branch. repoId scopes the probe (omit it
// to fall back to the sole project); branch picks the config.yml revision.
export function useSpeccyInfo(repoId?: string, branch?: string) {
  const params = new URLSearchParams();
  if (repoId) params.set('repo', repoId);
  if (branch) params.set('branch', branch);
  const q = params.toString();
  const url = q ? `/api/speccy/info?${q}` : '/api/speccy/info';
  return useQuery({ queryKey: ['speccy-info', repoId ?? '', branch ?? ''], queryFn: () => api<SpeccyInfo>(url), staleTime: 300_000 });
}

/**
 * POST the active project's speccy/chat and consume the SSE stream. onDelta
 * fires per chunk; resolves with the full reply text. repoId targets the active
 * project so grounding follows the project switcher (omit → sole-project alias).
 */
export async function streamChat(
  repoId: string | undefined,
  body: { messages: ChatMessage[]; focusPath?: string; branch?: string },
  onDelta: (text: string) => void,
  signal?: AbortSignal,
): Promise<string> {
  const res = await fetch(repoId ? `/api/repos/${encodeURIComponent(repoId)}/speccy/chat` : '/api/speccy/chat', {
    method: 'POST',
    headers: { 'X-SpecQuill': '1', 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok || !res.body) {
    let msg = res.statusText;
    try { msg = ((await res.json()) as { error?: string }).error || msg; } catch { /* keep */ }
    throw new Error(msg);
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let full = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buffer.indexOf('\n\n')) >= 0) {
      const frame = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      const line = frame.trim();
      if (!line.startsWith('data:')) continue;
      const payload = JSON.parse(line.slice(5).trim()) as { delta?: string; error?: string; done?: boolean };
      if (payload.error) throw new Error(payload.error);
      if (payload.delta) {
        full += payload.delta;
        onDelta(full);
      }
      if (payload.done) return full;
    }
  }
  return full;
}

export function draftEdits(repoId: string | undefined, body: { changePath: string; files: string[]; branch?: string }): Promise<DraftResult> {
  const url = repoId ? `/api/repos/${encodeURIComponent(repoId)}/speccy/draft` : '/api/speccy/draft';
  return api<DraftResult>(url, { method: 'POST', body: JSON.stringify(body) });
}
