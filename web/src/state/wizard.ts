// wizard.ts — state for the guided authoring flow. Same shape as chats.ts: a
// module store (useSyncExternalStore) persisted to localStorage per repo, so
// the server stays stateless and an interrupted wizard survives a reload or a
// detour into the editor.
//
// Nothing here is written to the worktree. The document is created once,
// through the normal file endpoint, when the author accepts the draft — an
// abandoned wizard leaves no debris in the changes drawer.
import { useSyncExternalStore } from 'react';
import type { DraftSection, InterviewQuestion, RelatedMatch, RubricItem, WizardMessage } from '../api/wizard';

export type WizardStage = 'intent' | 'related' | 'interview' | 'review';

export interface WizardState {
  stage: WizardStage;
  /** what the author typed on the intent step */
  intent: string;
  family: string;
  folder: string;            // subfolder under the family folder ('' = root)
  altitude: '' | 'business' | 'technical';
  id: string;
  title: string;

  /** dedup step */
  related: RelatedMatch[];
  recommendation: string;
  /** matches the author kept as typed links when creating a new document */
  carriedLinks: string[];

  /** interview */
  transcript: WizardMessage[];
  questions: InterviewQuestion[];
  rubric: RubricItem[];
  readyToDraft: boolean;

  /** review */
  sections: DraftSection[];
  /** section name → one-line note from the last refinement */
  notes: Record<string, string>;
  /** sections the author edited by hand — the UI stops calling them AI drafts */
  touched: string[];
}

export const EMPTY: WizardState = {
  stage: 'intent',
  intent: '',
  family: '',
  folder: '',
  altitude: '',
  id: '',
  title: '',
  related: [],
  recommendation: '',
  carriedLinks: [],
  transcript: [],
  questions: [],
  rubric: [],
  readyToDraft: false,
  sections: [],
  notes: {},
  touched: [],
};

/** Questions were plain strings before options existed; a wizard left open
 * across that change would otherwise render `undefined` per card. */
function coerceQuestions(raw: unknown): InterviewQuestion[] {
  if (!Array.isArray(raw)) return [];
  return raw
    .map((q) => (typeof q === 'string' ? { question: q } : (q as InterviewQuestion)))
    .filter((q) => q && typeof q.question === 'string' && q.question.trim() !== '');
}

const key = (repo: string) => `specquill-wizard:${repo}`;
const store = new Map<string, WizardState>();
const subs = new Set<() => void>();

function load(repo: string): WizardState {
  const cached = store.get(repo);
  if (cached) return cached;
  let s = EMPTY;
  try {
    const raw = JSON.parse(localStorage.getItem(key(repo)) || 'null') as Partial<WizardState> | null;
    // merge onto EMPTY: a persisted state from an older shape must not leave
    // fields undefined for components that index into them
    if (raw && typeof raw === 'object') s = { ...EMPTY, ...raw, questions: coerceQuestions(raw.questions) };
  } catch {
    /* corrupted/absent — start fresh */
  }
  store.set(repo, s);
  return s;
}

function persist(repo: string, next: WizardState) {
  store.set(repo, next);
  try {
    localStorage.setItem(key(repo), JSON.stringify(next));
  } catch {
    /* quota — the wizard survives in memory for the session */
  }
  subs.forEach((f) => f());
}

/** Patch the wizard state for a repo. */
export function updateWizard(repo: string, patch: Partial<WizardState> | ((s: WizardState) => Partial<WizardState>)) {
  const cur = load(repo);
  const delta = typeof patch === 'function' ? patch(cur) : patch;
  persist(repo, { ...cur, ...delta });
}

/** Back to a blank intent step (finished, or explicitly discarded). */
export function resetWizard(repo: string) {
  persist(repo, EMPTY);
}

export function useWizard(repo: string): WizardState {
  return useSyncExternalStore(
    (cb) => {
      subs.add(cb);
      return () => subs.delete(cb);
    },
    () => load(repo),
  );
}

/** How much of the rubric is satisfied — the "where am I" signal. */
export function rubricProgress(rubric: RubricItem[]): { met: number; total: number } {
  return { met: rubric.filter((r) => r.met).length, total: rubric.length };
}
