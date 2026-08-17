/** @vitest-environment jsdom */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { appendEntry, autoTitle, dismissChat, nameChatOnce, newChat, updateChat } from './chats';

// jsdom serves an opaque origin with no web storage — stub it (same pattern
// as client.test.ts); the module store caches per repo, so every test gets a
// fresh repo key
let n = 0;
let repo = '';
const backing = new Map<string, string>();
beforeEach(() => {
  backing.clear();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => backing.get(k) ?? null,
    setItem: (k: string, v: string) => void backing.set(k, v),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => backing.clear(),
  });
  repo = `test-repo-${n++}`;
});
afterEach(() => vi.unstubAllGlobals());

const read = () => JSON.parse(localStorage.getItem(`specquill-chats:${repo}`)!) as {
  chats: { id: string; title: string; entries: unknown[] }[];
  active: string;
};

describe('chats store', () => {
  it('creates chats, activates them, and persists to localStorage', () => {
    const a = newChat(repo);
    const b = newChat(repo);
    const s = read();
    expect(s.chats.map((c) => c.id)).toEqual([a, b]);
    expect(s.active).toBe(b);
  });

  it('dismissing the active chat activates the neighbor; others keep focus', () => {
    const a = newChat(repo);
    const b = newChat(repo);
    dismissChat(repo, b); // active
    expect(read().active).toBe(a);
    const c = newChat(repo);
    dismissChat(repo, a); // inactive
    expect(read().active).toBe(c);
    dismissChat(repo, c);
    expect(read().active).toBe('');
    expect(read().chats).toEqual([]);
  });

  it('appends entries and updates individual chats', () => {
    const a = newChat(repo);
    appendEntry(repo, a, { kind: 'msg', msg: { role: 'user', content: 'hi' } });
    updateChat(repo, a, (c) => ({ ...c, title: 'x' }));
    const s = read();
    expect(s.chats[0].entries).toHaveLength(1);
    expect(s.chats[0].title).toBe('x');
    // updates to a dismissed/unknown chat are a no-op, not an error
    appendEntry(repo, 'nope', { kind: 'msg', msg: { role: 'user', content: 'lost' } });
    expect(read().chats[0].entries).toHaveLength(1);
  });

  it('auto-naming sets once, upgrades only with overwrite', () => {
    const a = newChat(repo);
    nameChatOnce(repo, a, autoTitle('  How   should retention  work for STORK cases in the new spec?  '));
    const fallback = read().chats[0].title;
    expect(fallback.length).toBeLessThanOrEqual(34);
    expect(fallback.startsWith('How should retention work')).toBe(true);
    nameChatOnce(repo, a, 'ignored'); // already named
    expect(read().chats[0].title).toBe(fallback);
    nameChatOnce(repo, a, 'Retention rules', true); // quick-model upgrade
    expect(read().chats[0].title).toBe('Retention rules');
  });

  it('evicts oldest chats beyond the cap', () => {
    for (let i = 0; i < 15; i++) newChat(repo);
    expect(read().chats.length).toBe(12);
  });
});
