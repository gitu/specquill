// draftRegistry — module-level registry of pending draft flushers so branch
// switches, commits, PR creation and navigation can force-persist every open
// editor buffer before proceeding.

const flushers = new Map<string, () => Promise<void>>();

export function registerDraftFlush(key: string, fn: () => Promise<void>): () => void {
  flushers.set(key, fn);
  return () => {
    if (flushers.get(key) === fn) flushers.delete(key);
  };
}

export async function flushAllDrafts(): Promise<void> {
  await Promise.allSettled([...flushers.values()].map((fn) => fn()));
}
