// Copilot API: SSE chat streaming + draft-edit application.
import { useQuery } from '@tanstack/react-query';
import { api, apiPath } from './client';
import { useTenant } from './hooks';

export interface ChatMessage { role: 'user' | 'assistant'; content: string }
export interface CopilotInfo { enabled: boolean; model?: string; groundedSources?: string[] }
export interface DraftResult { branch: string; summary: string; applied: string[]; failures: string[] }

// info is per-project: grounded sources depend on the active project's
// references. repoId scopes the probe; omit it to fall back to the sole project.
export function useCopilotInfo(repoId?: string) {
  const tenant = useTenant();
  const url = repoId ? `/api/copilot/info?repo=${encodeURIComponent(repoId)}` : '/api/copilot/info';
  return useQuery({ queryKey: ['t', tenant, 'copilot-info', repoId ?? ''], queryFn: () => api<CopilotInfo>(url), staleTime: 300_000 });
}

/**
 * POST the active project's copilot/chat and consume the SSE stream. onDelta
 * fires per chunk; resolves with the full reply text. repoId targets the active
 * project so grounding follows the project switcher (omit → sole-project alias).
 */
export async function streamChat(
  repoId: string | undefined,
  body: { messages: ChatMessage[]; focusPath?: string; branch?: string },
  onDelta: (text: string) => void,
  signal?: AbortSignal,
): Promise<string> {
  const res = await fetch(apiPath(`/api/repos/${encodeURIComponent(repoId!)}/copilot/chat`), {
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
  return api<DraftResult>(`/api/repos/${encodeURIComponent(repoId!)}/copilot/draft`, { method: 'POST', body: JSON.stringify(body) });
}
