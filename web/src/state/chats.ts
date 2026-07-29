// chats.ts — Speccy conversation store. The panel unmounts when closed, so
// transcripts live here: a module store (useSyncExternalStore) persisted to
// localStorage per repo. Multiple chats, individually dismissable, capped.
import { useSyncExternalStore } from 'react';
import type { ChatMessage, DraftResult, PendingAsk, ToolEvent } from '../api/speccy';

export type ChatEntry =
  | { kind: 'msg'; msg: ChatMessage }
  | { kind: 'tool'; tool: ToolEvent }
  | { kind: 'ask'; ask: PendingAsk; answered?: string; preface?: string }
  | { kind: 'draft'; result: DraftResult };

export interface Chat {
  id: string;
  title: string; // '' until auto-named
  entries: ChatEntry[];
  at: number; // created (ms) — display/eviction order
}

interface RepoChats {
  chats: Chat[];
  active: string;
}

const MAX_CHATS = 12; // oldest evicted beyond this
const MAX_ENTRIES = 200; // per chat; oldest entries dropped

const key = (repo: string) => `specquill-chats:${repo}`;
const store = new Map<string, RepoChats>();
const subs = new Set<() => void>();

function load(repo: string): RepoChats {
  const cached = store.get(repo);
  if (cached) return cached;
  let s: RepoChats = { chats: [], active: '' };
  try {
    const raw = JSON.parse(localStorage.getItem(key(repo)) || 'null') as RepoChats | null;
    if (raw && Array.isArray(raw.chats)) s = { chats: raw.chats, active: raw.active || '' };
  } catch {
    /* corrupted/absent — start fresh */
  }
  store.set(repo, s);
  return s;
}

function mutate(repo: string, fn: (s: RepoChats) => RepoChats) {
  const next = fn(load(repo));
  store.set(repo, next);
  try {
    localStorage.setItem(key(repo), JSON.stringify(next));
  } catch {
    /* quota — chats survive in memory for the session */
  }
  subs.forEach((f) => f());
}

const newId = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 7);

/** Create a chat, make it active, return its id (synchronous). */
export function newChat(repo: string): string {
  const id = newId();
  mutate(repo, (s) => ({
    active: id,
    chats: [...s.chats, { id, title: '', entries: [], at: Date.now() }].slice(-MAX_CHATS),
  }));
  return id;
}

export function setActiveChat(repo: string, id: string) {
  mutate(repo, (s) => ({ ...s, active: id }));
}

/** Dismiss one chat; the neighbor (or none) becomes active. */
export function dismissChat(repo: string, id: string) {
  mutate(repo, (s) => {
    const chats = s.chats.filter((c) => c.id !== id);
    const active = s.active === id ? (chats[chats.length - 1]?.id ?? '') : s.active;
    return { chats, active };
  });
}

export function updateChat(repo: string, id: string, fn: (c: Chat) => Chat) {
  mutate(repo, (s) => ({
    ...s,
    chats: s.chats.map((c) => {
      if (c.id !== id) return c;
      const next = fn(c);
      return next.entries.length > MAX_ENTRIES ? { ...next, entries: next.entries.slice(-MAX_ENTRIES) } : next;
    }),
  }));
}

export function appendEntry(repo: string, id: string, entry: ChatEntry) {
  updateChat(repo, id, (c) => ({ ...c, entries: [...c.entries, entry] }));
}

/** Set the title unless a (better) one already stuck. */
export function nameChatOnce(repo: string, id: string, title: string, overwrite = false) {
  const t = title.trim();
  if (!t) return;
  updateChat(repo, id, (c) => (c.title && !overwrite ? c : { ...c, title: t.slice(0, 60) }));
}

/** Deterministic fallback title from the first user message. */
export function autoTitle(text: string): string {
  const t = text.replace(/\s+/g, ' ').trim();
  return t.length > 34 ? t.slice(0, 33).trimEnd() + '…' : t;
}

export function useChats(repo: string): RepoChats {
  return useSyncExternalStore(
    (cb) => {
      subs.add(cb);
      return () => subs.delete(cb);
    },
    () => load(repo),
  );
}
