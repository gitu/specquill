// Guided authoring API: the four staged endpoints behind the wizard
// (related → interview → compose → section). All stream SSE — they run a tool
// loop against a thinking-class model — but their payload is a single
// structured result, not a token stream, so each call resolves with one
// object plus whatever tool activity happened on the way.
import { postStream, readSSE } from './sse';

export interface WizardMessage {
  role: 'user' | 'assistant';
  content: string;
}

export interface RubricItem {
  criterion: string;
  met: boolean;
}

/** One open question plus the concrete answers the author can pick. */
export interface InterviewQuestion {
  question: string;
  options?: string[];
}

export interface InterviewResult {
  reply: string;
  questions: InterviewQuestion[];
  rubric: RubricItem[];
  readyToDraft: boolean;
}

export interface RelatedMatch {
  path: string;
  title: string;
  relation: 'covers' | 'overlaps' | 'related';
  reason: string;
}

export interface RelatedResult {
  matches: RelatedMatch[];
  /** path of the document to extend, or 'new' */
  recommendation: string;
}

export interface DraftSection {
  name: string;
  content: string;
}

export interface ComposeResult {
  title: string;
  sections: DraftSection[];
}

export interface SectionResult {
  content: string;
  note: string;
}

/** Everything a stage needs to know about what is being authored. */
export interface WizardContext {
  branch?: string;
  intent: string;
  family: string;
  folder?: string;
  altitude?: '' | 'business' | 'technical';
}

type Frame<T> = { result?: T; note?: string; error?: string; done?: boolean };

async function stage<T>(
  repoId: string,
  name: 'related' | 'interview' | 'compose' | 'section',
  body: Record<string, unknown>,
  onNote?: (text: string) => void,
  signal?: AbortSignal,
): Promise<T> {
  const reader = await postStream(`/api/repos/${encodeURIComponent(repoId)}/speccy/${name}`, body, signal);
  let result: T | undefined;
  let failure: Error | undefined;
  const complete = await readSSE<Frame<T>>(reader, (payload) => {
    if (payload.error) {
      failure = new Error(payload.error);
      return true;
    }
    if (payload.note) onNote?.(payload.note);
    if (payload.result !== undefined) result = payload.result;
    return payload.done === true;
  });
  if (failure) throw failure;
  if (!complete || result === undefined) {
    // the same silent-truncation class the chat guards against: a stream that
    // ends without the terminal frame is a dropped connection, not an answer
    throw new Error(
      'connection lost before Speccy finished — check the network and the proxy’s SSE/idle timeout',
    );
  }
  return result;
}

/** Does the workspace already cover this intent? */
export function findRelated(repoId: string, ctx: WizardContext, onNote?: (text: string) => void, signal?: AbortSignal) {
  return stage<RelatedResult>(repoId, 'related', { ...ctx }, onNote, signal);
}

/** One interview turn: reply, open questions, and the readiness rubric. */
export function interview(
  repoId: string,
  ctx: WizardContext,
  messages: WizardMessage[],
  sections: string[],
  onNote?: (text: string) => void,
  signal?: AbortSignal,
) {
  return stage<InterviewResult>(repoId, 'interview', { ...ctx, messages, sections }, onNote, signal);
}

/** Write the draft into the section outline. Nothing is saved server-side. */
export function compose(
  repoId: string,
  ctx: WizardContext,
  messages: WizardMessage[],
  sections: string[],
  onNote?: (text: string) => void,
  signal?: AbortSignal,
) {
  return stage<ComposeResult>(repoId, 'compose', { ...ctx, messages, sections }, onNote, signal);
}

/** Revise one section in place. */
export function refineSection(
  repoId: string,
  ctx: WizardContext,
  messages: WizardMessage[],
  args: { title: string; section: string; sectionContent: string; instruction: string },
  onNote?: (text: string) => void,
  signal?: AbortSignal,
) {
  return stage<SectionResult>(repoId, 'section', { ...ctx, messages, ...args }, onNote, signal);
}
