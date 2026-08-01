// history.ts — the git change feed read through the WORKSPACE MODEL: every
// path a commit touched is classified into its document family (the same
// rule buildModel uses), so the feed says "3 requirements, 1 spec" instead of
// listing file paths. Deleted and renamed-away paths no longer exist in the
// snapshot, so classification falls back to the folder rule — that is why
// model.ts exports the classifier at all.

import type { Commit, CommitFile } from '../api/hooks';
import { classifier } from './model';
import type { WorkspaceModel } from './model';
import type { EntityDef } from './config';

export interface HistoryFile extends CommitFile {
  kind: string;      // entity family key ('' when the path is not a document)
  label: string;     // family label, singular-ish, for badges
  icon: string;
  color: string;
  title: string;     // frontmatter title when the document still exists
  change: 'added' | 'modified' | 'deleted' | 'renamed';
}

export interface HistoryCommit extends Commit {
  files: HistoryFile[];
  /** "3 requirements · 1 spec" — what this commit did, in model terms */
  summary: string;
  day: string;       // YYYY-MM-DD, for the day grouping
}

export interface HistoryDay { day: string; commits: HistoryCommit[] }

export interface HistoryFilters {
  kind?: string;     // one family key, or '' / undefined for all
  change?: string;   // added | modified | deleted | renamed
  author?: string;
  /** only families on the WHAT axis — "what changed in the requirements" */
  whatOnly?: boolean;
}

const CHANGE_OF: Record<string, HistoryFile['change']> = {
  A: 'added', M: 'modified', D: 'deleted', R: 'renamed', T: 'modified',
};

/** Singular label for a family: "Requirements" → "requirement". */
export const singularLabel = (e: EntityDef) =>
  e.label.toLowerCase().replace(/ies$/, 'y').replace(/s$/, '');

/**
 * Classify a log into the workspace's families and apply the filters. The
 * day grouping is what the view renders; `counts` feeds the family chips and
 * is computed BEFORE filtering, so the chips never lie about what exists.
 */
export function buildHistory(commits: Commit[], model: WorkspaceModel, filters: HistoryFilters = {}) {
  const classify = classifier(model.entities);
  const titleOf: Record<string, string> = {};
  const typeOf: Record<string, string> = {};
  model.docs.forEach((d) => { titleOf[d.path] = d.title; typeOf[d.path] = d.kind; });
  const entByKind: Record<string, EntityDef> = {};
  model.entities.forEach((e) => { entByKind[e.kind] = e; });

  const all: HistoryCommit[] = commits.map((c) => {
    const files: HistoryFile[] = c.files.map((f) => {
      // a document that still exists carries its classified kind; otherwise
      // the folder rule decides (deleted/renamed-away paths)
      const ent = entByKind[typeOf[f.path]] || classify(f.path);
      return {
        ...f,
        kind: ent?.kind || '',
        label: ent ? singularLabel(ent) : '',
        icon: ent?.icon || '·',
        color: ent?.color || 'var(--text-3)',
        title: titleOf[f.path] || '',
        change: CHANGE_OF[f.status] || 'modified',
      };
    });
    return { ...c, files, summary: rollup(files), day: (c.date || '').slice(0, 10) };
  });

  const counts: Record<string, number> = { all: all.length };
  model.entities.forEach((e) => { counts[e.kind] = 0; });
  const authors = new Map<string, number>();
  all.forEach((c) => {
    authors.set(c.author, (authors.get(c.author) || 0) + 1);
    new Set(c.files.map((f) => f.kind)).forEach((k) => { if (k in counts) counts[k]++; });
  });

  const whatKinds = new Set(model.entities.filter((e) => e.group === 'what').map((e) => e.kind));
  const matches = (c: HistoryCommit) =>
    (!filters.author || c.author === filters.author) &&
    c.files.some((f) =>
      (!filters.kind || f.kind === filters.kind) &&
      (!filters.change || f.change === filters.change) &&
      (!filters.whatOnly || whatKinds.has(f.kind)));

  const items = all.filter(matches);
  const days: HistoryDay[] = [];
  items.forEach((c) => {
    const last = days[days.length - 1];
    if (last && last.day === c.day) last.commits.push(c);
    else days.push({ day: c.day, commits: [c] });
  });
  return {
    items, days, counts,
    authors: [...authors.entries()].sort((a, b) => b[1] - a[1]).map(([name, n]) => ({ name, n })),
  };
}

/** "3 requirements · 1 spec · 1 other" — plural-aware, in family order. */
export function rollup(files: HistoryFile[]): string {
  const byLabel = new Map<string, number>();
  files.forEach((f) => {
    const key = f.label || 'file';
    byLabel.set(key, (byLabel.get(key) || 0) + 1);
  });
  return [...byLabel.entries()]
    .map(([label, n]) => `${n} ${n === 1 ? label : pluralLabel(label)}`)
    .join(' · ');
}

/** "requirement" → "requirements", "family" → "families". */
export const pluralLabel = (s: string) => (s.endsWith('y') ? s.slice(0, -1) + 'ies' : s.endsWith('s') ? s : s + 's');

/** The window filter's `since` value (YYYY-MM-DD) for a day count. */
export function sinceDays(days: number, today = new Date()): string {
  const d = new Date(today.getTime() - days * 86400000);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
}
