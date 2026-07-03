// linediff — tiny LCS-based line diff for the editor's changed-line gutter.
// Returns, for the NEW text, which line numbers (1-based) are added or
// changed, plus positions where deletions occurred.

export interface LineDiff {
  added: Set<number>;    // lines present only in next
  changed: Set<number>;  // lines replaced (paired add/remove)
  removedAt: Set<number>; // next-line numbers after which content was deleted
}

export function diffLinesLCS(prev: string, next: string): LineDiff {
  const a = prev.split('\n');
  const b = next.split('\n');
  const n = a.length, m = b.length;

  // LCS table (guard against pathological sizes — bail to "all changed")
  if (n * m > 4_000_000) {
    const all = new Set<number>();
    for (let i = 1; i <= m; i++) all.add(i);
    return { added: all, changed: new Set(), removedAt: new Set() };
  }
  const dp = new Uint32Array((n + 1) * (m + 1));
  const idx = (i: number, j: number) => i * (m + 1) + j;
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[idx(i, j)] = a[i] === b[j]
        ? dp[idx(i + 1, j + 1)] + 1
        : Math.max(dp[idx(i + 1, j)], dp[idx(i, j + 1)]);
    }
  }

  const added = new Set<number>();
  const changed = new Set<number>();
  const removedAt = new Set<number>();
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      i++; j++;
    } else if (dp[idx(i + 1, j)] >= dp[idx(i, j + 1)]) {
      // deletion from prev
      removedAt.add(j); // before next line j+1 (0 = top)
      i++;
    } else {
      // insertion into next; treat as "changed" when paired with a deletion here
      if (removedAt.has(j)) {
        changed.add(j + 1);
        removedAt.delete(j);
      } else {
        added.add(j + 1);
      }
      j++;
    }
  }
  while (j < m) { added.add(j + 1); j++; }
  if (i < n) removedAt.add(m);
  return { added, changed, removedAt };
}
