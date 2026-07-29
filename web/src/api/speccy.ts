// Speccy API: SSE chat streaming (with tool activity) + draft-edit application.
import { useQuery } from '@tanstack/react-query';
import { api } from './client';

// Chat wire messages round-trip through the server untouched; tool_calls /
// tool_call_id only appear on the resume path of a pending ask_user question.
export interface ChatMessage {
  role: 'user' | 'assistant' | 'tool';
  content: string;
  tool_calls?: unknown[];
  tool_call_id?: string;
}
export interface SpeccyInfo { enabled: boolean; model?: string; groundedSources?: string[] }
export interface DraftResult { branch: string; summary: string; applied: string[]; failures: string[] }

/** One executed tool call, streamed for display. */
export interface ToolEvent { name: string; path?: string; status: 'ok' | 'error'; detail?: string }

/** A pending ask_user question: answer it by replaying resume + a tool message. */
export interface PendingAsk { callId: string; question: string; options?: string[]; resume: ChatMessage[] }

export interface ChatResult { text: string; ask?: PendingAsk; edited: boolean }

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
 * fires per chunk with the accumulated text, onTool per executed tool call.
 * Resolves with the reply text plus a pending ask_user question, if the model
 * paused for one. repoId targets the active project so grounding follows the
 * project switcher (omit → sole-project alias).
 */
export async function streamChat(
  repoId: string | undefined,
  body: { messages: ChatMessage[]; focusPath?: string; branch?: string; allowEdits?: boolean },
  onDelta: (text: string) => void,
  onTool?: (t: ToolEvent) => void,
  signal?: AbortSignal,
): Promise<ChatResult> {
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
  const result: ChatResult = { text: '', edited: false };
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
      const payload = JSON.parse(line.slice(5).trim()) as {
        delta?: string; error?: string; done?: boolean;
        tool?: ToolEvent;
        ask?: { callId: string; question: string; options?: string[] };
        resume?: ChatMessage[];
      };
      if (payload.error) throw new Error(payload.error);
      if (payload.delta) {
        result.text += payload.delta;
        onDelta(result.text);
      }
      if (payload.tool) {
        if (payload.tool.status === 'ok' && (payload.tool.name === 'edit_file' || payload.tool.name === 'create_file')) {
          result.edited = true;
        }
        onTool?.(payload.tool);
      }
      if (payload.ask) {
        result.ask = { ...payload.ask, resume: payload.resume || [] };
      }
      if (payload.done) return result;
    }
  }
  // the stream closed without the server's terminal {done} event — a dropped
  // connection (proxy idle timeout, network) that would otherwise pass for a
  // finished reply. A pending ask is still usable; anything else is an error.
  if (result.ask) return result;
  throw new Error(
    result.text
      ? `connection lost mid-reply (after ${result.text.length} characters) — check the proxy's SSE/idle timeout`
      : 'connection lost before Speccy answered — check the network and the proxy\'s SSE/idle timeout',
  );
}

/** Quick-tier chat naming; the caller keeps its fallback title on failure. */
export function nameChat(repoId: string, text: string): Promise<{ title: string }> {
  return api<{ title: string }>(`/api/repos/${encodeURIComponent(repoId)}/speccy/title`, {
    method: 'POST',
    body: JSON.stringify({ text }),
  });
}

export function draftEdits(repoId: string | undefined, body: { changePath: string; files: string[]; branch?: string }): Promise<DraftResult> {
  const url = repoId ? `/api/repos/${encodeURIComponent(repoId)}/speccy/draft` : '/api/speccy/draft';
  return api<DraftResult>(url, { method: 'POST', body: JSON.stringify(body) });
}
