// useDraft — the editor's durable buffer. Autosaves to the branch worktree
// (the worktree is the draft store), keeps a localStorage recovery copy for
// the sub-second window / crashes, survives branch switches, and exposes a
// conflict state instead of ever silently merging.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTenant } from '../api/hooks';
import { api } from '../api/client';
import { useQueryClient } from '@tanstack/react-query';
import { registerDraftFlush } from '../lib/draftRegistry';

export type SyncState = 'clean' | 'pending' | 'saving' | 'saved' | 'error' | 'conflict';

export interface Draft {
  path: string;
  raw: string;
  dirty: boolean;
  gen: number;
}

interface FileData {
  content: string;
  sha: string;
}

const DEBOUNCE_MS = 1500;
const MAX_WAIT_MS = 5000;
const LS_PREFIX = 'specquill-draft:';
const LS_MAX_AGE = 7 * 24 * 3600 * 1000;

// prune stale recovery entries once per session
let pruned = false;
function pruneOnce() {
  if (pruned) return;
  pruned = true;
  try {
    const now = Date.now();
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const k = localStorage.key(i);
      if (!k?.startsWith(LS_PREFIX)) continue;
      const at = JSON.parse(localStorage.getItem(k) || '{}').at || 0;
      if (now - at > LS_MAX_AGE) localStorage.removeItem(k);
    }
  } catch { /* quota/parse noise */ }
}

export function useDraft({ repo, branch, path, file, enabled, onRecovered, beforePersist }: {
  repo: string | undefined;
  branch: string;
  path: string;
  file: { data?: FileData };
  enabled: boolean;
  onRecovered?: () => void;
  /** called right before a PUT to pull content still sitting in editor debounce windows */
  beforePersist?: () => string | null;
}) {
  const qc = useQueryClient();
  const tenant = useTenant();
  const [draft, setDraft] = useState<Draft>({ path: '', raw: '', dirty: false, gen: 0 });
  const [syncState, setSyncState] = useState<SyncState>('clean');
  const lsKey = `${LS_PREFIX}${repo}:${branch}:${path}`;

  const state = useRef({
    baseSha: '',
    seedKey: '',            // `${branch}:${path}` the current baseSha belongs to
    raw: '',
    dirty: false,
    inFlight: false,
    conflicted: false,
    timer: 0 as ReturnType<typeof setTimeout> | 0,
    firstChangeAt: 0,
    lsTimer: 0 as ReturnType<typeof setTimeout> | 0,
    url: '',
  });
  state.current.url = `/api/repos/${repo}/files/${path}?branch=${encodeURIComponent(branch)}`;
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;
  const beforePersistRef = useRef(beforePersist);
  beforePersistRef.current = beforePersist;

  // ---- seeding from the server file (and recovery) --------------------
  useEffect(() => {
    pruneOnce();
    const content = file.data?.content;
    if (content === undefined) return;
    const seedKey = `${branch}:${path}`;

    setDraft((d) => {
      if (d.path === path && d.dirty) {
        // keep user edits; if the branch changed under a dirty draft (the
        // workspace carry), adopt the new branch's sha so autosave targets it
        if (state.current.seedKey !== seedKey) {
          state.current.baseSha = file.data!.sha;
          state.current.seedKey = seedKey;
          scheduleAutosave(0);
        }
        return d;
      }
      state.current.baseSha = file.data!.sha;
      state.current.seedKey = seedKey;
      // crash/offline recovery: a stored draft that differs from the server
      let raw = content;
      let dirty = false;
      try {
        const stored = enabledRef.current ? JSON.parse(localStorage.getItem(lsKey) || 'null') : null;
        if (stored && typeof stored.raw === 'string' && stored.raw !== content) {
          raw = stored.raw;
          dirty = true;
          onRecovered?.();
        }
      } catch { /* ignore */ }
      state.current.raw = raw;
      state.current.dirty = dirty;
      if (dirty) scheduleAutosave(0);
      if (d.path === path && d.raw === raw) return d;
      return { path, raw, dirty, gen: d.path === path ? d.gen + 1 : 0 };
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file.data, path, branch]);

  // ---- autosave engine -------------------------------------------------
  const persist = useCallback(async () => {
    const s = state.current;
    if (!enabledRef.current || s.inFlight || s.conflicted || !s.dirty) return;
    // pull whatever still sits in the editor's own debounce window
    const fresh = beforePersistRef.current?.();
    if (fresh != null && fresh !== s.raw) {
      s.raw = fresh;
      setDraft((d) => ({ ...d, raw: fresh, dirty: true }));
    }
    s.inFlight = true;
    setSyncState('saving');
    const raw = s.raw;
    try {
      const res = await api<{ sha: string }>(s.url, {
        method: 'PUT',
        body: JSON.stringify({ content: raw, baseSha: s.baseSha }),
      });
      s.baseSha = res.sha;
      if (s.raw === raw) {
        s.dirty = false;
        setDraft((d) => ({ ...d, dirty: false }));
        setSyncState('saved');
        try { localStorage.removeItem(lsKey); } catch { /* ignore */ }
      } else {
        setSyncState('pending'); // typed while saving — go again
        scheduleAutosave(DEBOUNCE_MS);
      }
      s.firstChangeAt = 0;
      qc.invalidateQueries({ queryKey: ['t', tenant, 'status', repo, branch] });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'snapshot', repo, branch] });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'file', repo, branch, path] });
    } catch (e) {
      const status = (e as { status?: number }).status;
      if (status === 409) {
        s.conflicted = true;
        setSyncState('conflict');
      } else if (status === 403) {
        s.conflicted = true; // protected/read-only: stop hammering
        setSyncState('error');
      } else {
        setSyncState('error');
        scheduleAutosave(4000); // transient (network) — retry
      }
    } finally {
      s.inFlight = false;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lsKey, repo, branch]);

  const persistRef = useRef(persist);
  persistRef.current = persist;

  function scheduleAutosave(delay: number) {
    const s = state.current;
    if (s.timer) clearTimeout(s.timer);
    const now = Date.now();
    if (!s.firstChangeAt) s.firstChangeAt = now;
    const cap = Math.max(0, s.firstChangeAt + MAX_WAIT_MS - now);
    s.timer = setTimeout(() => void persistRef.current(), Math.min(delay, cap));
  }

  // ---- public mutators -------------------------------------------------
  const setRaw = useCallback((raw: string) => {
    const s = state.current;
    if (s.raw === raw) return;
    s.raw = raw;
    s.dirty = true;
    setDraft((d) => ({ ...d, raw, dirty: true }));
    setSyncState((st) => (st === 'conflict' || st === 'error' ? st : 'pending'));
    if (!s.conflicted) scheduleAutosave(DEBOUNCE_MS);
    if (s.lsTimer) clearTimeout(s.lsTimer);
    s.lsTimer = setTimeout(() => {
      try { localStorage.setItem(lsKey, JSON.stringify({ raw: state.current.raw, at: Date.now() })); } catch { /* quota */ }
    }, 250);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lsKey]);

  const markDirty = useCallback(() => {
    const s = state.current;
    if (s.dirty) return;
    s.dirty = true;
    setDraft((d) => (d.dirty ? d : { ...d, dirty: true }));
    setSyncState('pending');
    if (!s.conflicted) scheduleAutosave(DEBOUNCE_MS);
  }, []);

  const flush = useCallback(async () => {
    const s = state.current;
    if (s.timer) clearTimeout(s.timer);
    await persistRef.current();
  }, []);

  const resolveConflict = useCallback(async (mode: 'theirs' | 'mine') => {
    const s = state.current;
    s.conflicted = false;
    if (mode === 'theirs') {
      s.dirty = false;
      setDraft({ path: '', raw: '', dirty: false, gen: 0 }); // reseed from refetch
      try { localStorage.removeItem(lsKey); } catch { /* ignore */ }
      setSyncState('clean');
      await qc.invalidateQueries({ queryKey: ['t', tenant, 'file', repo, branch, path] });
    } else {
      // adopt the server's current sha, then overwrite with mine
      const fresh = await api<FileData>(`/api/repos/${repo}/files/${path}?ref=${encodeURIComponent(branch)}`);
      s.baseSha = fresh.sha;
      setSyncState('pending');
      await persistRef.current();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repo, branch, path, lsKey]);

  // ---- durability hooks --------------------------------------------------
  useEffect(() => {
    if (!enabled || !repo) return;
    const key = `${repo}:${branch}:${path}`;
    const unregister = registerDraftFlush(key, flush);

    const onHide = () => {
      const s = state.current;
      if (!s.dirty || s.conflicted) return;
      // fire-and-forget; the localStorage copy covers failure
      void fetch(s.url, {
        method: 'PUT',
        headers: { 'X-SpecQuill': '1', 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: s.raw, baseSha: s.baseSha }),
        keepalive: true,
      });
    };
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      const s = state.current;
      if (s.dirty || s.inFlight) {
        onHide();
        e.preventDefault();
      }
    };
    const onVisibility = () => { if (document.visibilityState === 'hidden') onHide(); };
    window.addEventListener('beforeunload', onBeforeUnload);
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      unregister();
      window.removeEventListener('beforeunload', onBeforeUnload);
      document.removeEventListener('visibilitychange', onVisibility);
      onHide(); // unmount (e.g. navigating views) flushes too
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, repo, branch, path, flush]);

  return useMemo(() => ({ draft, setDraft, setRaw, markDirty, syncState, flush, resolveConflict }),
    [draft, setRaw, markDirty, syncState, flush, resolveConflict]);
}
