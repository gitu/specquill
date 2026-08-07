// WizardView — guided document authoring: intent → related → interview →
// review, then hand off to the markdown editor.
//
// The Speccy panel is an open-ended chat: powerful, but it never tells you
// where you are or what is still missing. This view is the opposite trade —
// a fixed set of stages, a readiness rubric you can watch fill up, and a
// section outline the draft is written into. Nothing is written to the
// worktree until "Create document": an abandoned wizard leaves no debris.
import { useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useNav } from '../state/nav';
import { api } from '../api/client';
import { useWorkspace } from '../hooks/useWorkspace';
import { useSpeccyInfo } from '../api/speccy';
import {
  compose, findRelated, interview, refineSection,
  type DraftSection, type RelatedMatch, type RubricItem, type WizardContext,
} from '../api/wizard';
import { resetWizard, rubricProgress, updateWizard, useWizard, type WizardStage, type WizardState } from '../state/wizard';
import { sectionsFor } from '../lib/sections';
import { existingIds, generateId, idPattern, slugify, slugifyPath } from '../lib/ids';
import { isReservedMd } from '../lib/model';
import { draftDocument } from '../lib/wizarddoc';
import { IconSpark } from '../components/icons';

const STAGES: { key: WizardStage; label: string }[] = [
  { key: 'intent', label: 'Intent' },
  { key: 'related', label: 'Existing work' },
  { key: 'interview', label: 'Interview' },
  { key: 'review', label: 'Draft' },
];

const CARD = 'border:1px solid var(--border);border-radius:12px;background:var(--surface);padding:16px 18px';
const INPUT = 'width:100%;padding:8px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12.5px;box-sizing:border-box;outline:none';
const BTN = 'height:32px;padding:0 14px;border-radius:8px;font-family:inherit;font-size:12.5px;cursor:pointer;border:1px solid var(--border-2);background:var(--surface);color:var(--text)';
const PRIMARY = 'height:32px;padding:0 16px;border-radius:8px;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;border:none;background:var(--ai);color:#fff';

const label = (t: string) => (
  <div style={sx('font-size:11px;font-weight:700;letter-spacing:.4px;color:var(--text-3);text-transform:uppercase;margin:16px 0 7px')}>{t}</div>
);

export function WizardView() {
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const { ensureWritableBranch } = useWorkspace();
  const repoKey = app.repoId || '';
  const w = useWizard(repoKey);
  const info = useSpeccyInfo(app.repoId, app.branch);
  const enabled = info.data?.enabled === true;

  const [busy, setBusy] = useState<'' | 'related' | 'interview' | 'compose' | 'create'>('');
  const [busySection, setBusySection] = useState('');
  const [error, setError] = useState('');
  const [activity, setActivity] = useState<string[]>([]);
  const abort = useRef<AbortController | null>(null);

  const entities = app.entities;
  // default to the family people actually author. entities[0] is `regulation`
  // — an external document you record, not one you draft — so it is a poor
  // landing state for a drafting wizard.
  const family = w.family || (entities.some((e) => e.kind === 'requirement') ? 'requirement' : entities[0]?.kind) || 'requirement';
  const ent = entities.find((e) => e.kind === family) || entities[0];
  const folderRoot = ent?.folder || 'requirements/';
  const outline = useMemo(() => sectionsFor(family, app.configYml), [family, app.configYml]);

  const ctx: WizardContext = {
    branch: app.branch,
    intent: w.intent,
    family,
    folder: folderRoot + (w.folder ? w.folder + '/' : ''),
    altitude: w.altitude,
  };

  const set = (patch: Partial<WizardState>) => updateWizard(repoKey, patch);
  const onNote = (text: string) => setActivity((prev) => [...prev.slice(-5), text]);

  /** Every stage call shares this: reset activity, surface failures, unbusy. */
  const run = async <T,>(kind: typeof busy, fn: (signal: AbortSignal) => Promise<T>): Promise<T | undefined> => {
    if (!app.repoId || busy) return;
    setError('');
    setActivity([]);
    setBusy(kind);
    abort.current = new AbortController();
    try {
      return await fn(abort.current.signal);
    } catch (e) {
      setError(String((e as Error).message || e));
      return undefined;
    } finally {
      setBusy('');
      abort.current = null;
    }
  };

  // --- stage transitions --------------------------------------------------

  const start = async () => {
    if (!w.intent.trim()) return;
    set({ family, related: [], recommendation: '', carriedLinks: [], transcript: [], rubric: [], questions: [], readyToDraft: false, sections: [], notes: {}, touched: [] });
    const res = await run('related', (signal) => findRelated(app.repoId!, ctx, onNote, signal));
    // dedup is best-effort — a failure or an empty result goes straight on to
    // the interview rather than blocking the author on a nicety
    if (res && res.matches.length) {
      set({ stage: 'related', related: res.matches, recommendation: res.recommendation });
      return;
    }
    await runInterview([]);
  };

  const runInterview = async (transcript: WizardState['transcript']) => {
    const res = await run('interview', (signal) => interview(app.repoId!, ctx, transcript, outline, onNote, signal));
    if (!res) {
      // keep whatever stage we were on so the author can retry or edit
      if (w.stage === 'intent') set({ stage: 'interview' });
      return;
    }
    set({
      stage: 'interview',
      transcript: [...transcript, { role: 'assistant', content: res.reply }],
      questions: res.questions || [],
      rubric: res.rubric || [],
      readyToDraft: res.readyToDraft,
    });
  };

  const answer = async (text: string) => {
    if (!text.trim() || busy) return;
    const next = [...w.transcript, { role: 'user' as const, content: text.trim() }];
    set({ transcript: next, questions: [] });
    await runInterview(next);
  };

  const draft = async () => {
    const res = await run('compose', (signal) => compose(app.repoId!, ctx, w.transcript, outline, onNote, signal));
    if (!res) return;
    set({
      stage: 'review',
      title: res.title || w.title,
      sections: res.sections.length ? res.sections : outline.map((name) => ({ name, content: '' })),
      notes: {},
      touched: [],
    });
  };

  const refine = async (name: string, instruction: string) => {
    if (busySection || !app.repoId) return;
    const section = w.sections.find((s) => s.name === name);
    if (!section) return;
    setBusySection(name);
    setError('');
    try {
      const res = await refineSection(app.repoId, ctx, w.transcript, {
        title: w.title, section: name, sectionContent: section.content, instruction,
      }, onNote);
      updateWizard(repoKey, (s) => ({
        sections: s.sections.map((x) => (x.name === name ? { ...x, content: res.content } : x)),
        notes: { ...s.notes, [name]: res.note },
        touched: s.touched.filter((t) => t !== name),
      }));
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusySection('');
    }
  };

  // --- the target path ----------------------------------------------------

  const files = app.files || {};
  const taken = useMemo(() => existingIds(files, folderRoot), [files, folderRoot]);
  const pattern = idPattern(family, app.configYml);
  const id = w.id || generateId(pattern, taken, w.title).id;
  const fileName = (id || slugify(w.title) || 'untitled') + '.md';
  const subPath = slugifyPath(w.folder);
  const path = folderRoot + (subPath ? subPath + '/' : '') + fileName;
  const pathTaken = !!files[path];
  const reserved = isReservedMd(fileName);

  const create = async () => {
    if (!app.repoId || pathTaken || reserved) return;
    await run('create', async () => {
      const branch = await ensureWritableBranch();
      const content = draftDocument(path, entities, { id, title: w.title || id }, w.sections, w.carriedLinks);
      await api<{ sha: string }>(`/api/repos/${app.repoId}/files/${path}?branch=${encodeURIComponent(branch)}`, {
        method: 'PUT',
        body: JSON.stringify({ content, baseSha: '' }),
      });
      for (const key of ['status', 'snapshot', 'worktreediff']) {
        qc.invalidateQueries({ queryKey: [key, app.repoId] });
      }
      resetWizard(repoKey);
      nav('/editor/' + path);
    });
  };

  // --- render -------------------------------------------------------------

  // the store is keyed by project, and app.repoId is undefined until the
  // repos query resolves — typing before that would persist under '' and
  // vanish the moment the project lands
  if (!app.repoId) {
    return (
      <Frame stage={w.stage}>
        <div style={sx(CARD + ';color:var(--text-3);font-size:12.5px')}>Loading the workspace…</div>
      </Frame>
    );
  }

  if (!enabled) {
    return (
      <Frame stage={w.stage}>
        <div style={sx(CARD + ';color:var(--text-2);font-size:12.5px;line-height:1.6')}>
          Guided authoring needs Speccy. Configure <code>ai:</code> in <code>specquill.yml</code> to enable it —
          you can still create documents by hand from the Specs view.
        </div>
      </Frame>
    );
  }

  return (
    <Frame stage={w.stage} onRestart={w.stage === 'intent' ? undefined : () => { resetWizard(repoKey); setError(''); }}>
      {w.stage === 'intent' && (
        <IntentStep
          state={w} entities={entities} family={family} folderRoot={folderRoot} outline={outline}
          busy={busy === 'related' || busy === 'interview'} onChange={set} onStart={() => void start()}
        />
      )}

      {w.stage === 'related' && (
        <RelatedStep
          matches={w.related} recommendation={w.recommendation} busy={busy === 'interview'}
          onExtend={(p) => { resetWizard(repoKey); nav('/editor/' + p); }}
          onNew={() => {
            set({ carriedLinks: w.related.map((m) => m.path) });
            void runInterview([]);
          }}
        />
      )}

      {w.stage === 'interview' && (
        <InterviewStep
          state={w} busy={busy === 'interview'} drafting={busy === 'compose'}
          onAnswer={(t) => void answer(t)} onDraft={() => void draft()}
        />
      )}

      {w.stage === 'review' && (
        <ReviewStep
          state={w} path={path} pathTaken={pathTaken} reserved={reserved} id={id}
          busy={busy === 'create'} busySection={busySection} canEdit={app.canEdit}
          onTitle={(t) => set({ title: t })}
          onId={(v) => set({ id: v })}
          onSection={(name, content) => updateWizard(repoKey, (s) => ({
            sections: s.sections.map((x) => (x.name === name ? { ...x, content } : x)),
            touched: s.touched.includes(name) ? s.touched : [...s.touched, name],
          }))}
          onRefine={(name, instruction) => void refine(name, instruction)}
          onBack={() => set({ stage: 'interview' })}
          onCreate={() => void create()}
        />
      )}

      {activity.length > 0 && (busy || busySection) && (
        <div style={sx("display:flex;flex-wrap:wrap;gap:6px;margin-top:12px;font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--ai)")}>
          {activity.map((t, i) => (
            <span key={i} style={sx('padding:2px 7px;border-radius:5px;background:var(--ai-bg);border:1px solid var(--ai-line)')}>
              {t.trim()}
            </span>
          ))}
        </div>
      )}

      {error && (
        <div style={sx('margin-top:14px;padding:10px 13px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:9px;color:var(--reg);font-size:12.5px')}>
          {error}
        </div>
      )}
    </Frame>
  );
}

// --- chrome ---------------------------------------------------------------

function Frame({ stage, onRestart, children }: { stage: WizardStage; onRestart?: () => void; children: React.ReactNode }) {
  const at = STAGES.findIndex((s) => s.key === stage);
  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto')}>
      <div style={sx('max-width:820px;margin:0 auto;padding:26px 24px 60px')}>
        <div style={sx('display:flex;align-items:center;gap:10px;margin-bottom:6px')}>
          <IconSpark size={18} stroke="var(--ai)" />
          <h1 style={sx('font-size:17px;font-weight:700;margin:0')}>Draft a document with Speccy</h1>
          <div style={sx('flex:1')} />
          {onRestart && (
            <button onClick={onRestart} style={sx(BTN + ';height:27px;font-size:11.5px')}>Start over</button>
          )}
        </div>
        <div style={sx('font-size:12.5px;color:var(--text-2);line-height:1.6;margin-bottom:18px')}>
          Speccy reads the workspace, grills you on what the document still needs, then writes a first
          draft you finish in the editor.
        </div>

        <div style={sx('display:flex;align-items:center;gap:8px;margin-bottom:20px;flex-wrap:wrap')} data-testid="wizard-stepper">
          {STAGES.map((s, i) => (
            <span key={s.key} style={sx('display:inline-flex;align-items:center;gap:8px')}>
              <span
                aria-current={i === at ? 'step' : undefined}
                style={sx('display:inline-flex;align-items:center;gap:6px;padding:3px 11px;border-radius:20px;font-size:11.5px;border:1px solid ' +
                  (i === at ? 'var(--ai-line);background:var(--ai-bg);color:var(--ai);font-weight:600'
                    : i < at ? 'var(--border);background:var(--surface-2);color:var(--text-2)'
                    : 'var(--border);background:transparent;color:var(--text-3)'))}
              >
                <span>{i < at ? '✓' : i + 1}</span>{s.label}
              </span>
              {i < STAGES.length - 1 && <span style={sx('color:var(--text-3);font-size:10px')}>→</span>}
            </span>
          ))}
        </div>

        {children}
      </div>
    </div>
  );
}

// --- stage 1: intent ------------------------------------------------------

function IntentStep(props: {
  state: WizardState;
  entities: { kind: string; label: string; icon: string; color: string; description: string; folder: string }[];
  family: string;
  folderRoot: string;
  outline: string[];
  busy: boolean;
  onChange: (patch: Partial<WizardState>) => void;
  onStart: () => void;
}) {
  const { state, entities, family, folderRoot, outline, busy, onChange, onStart } = props;
  const ent = entities.find((e) => e.kind === family);
  return (
    <div style={sx(CARD)}>
      {label('What do you want to specify?')}
      <textarea
        data-testid="wizard-intent"
        autoFocus
        value={state.intent}
        onChange={(e) => onChange({ intent: e.target.value })}
        onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) onStart(); }}
        placeholder="A rough idea is enough — Speccy will grill you on the rest. e.g. “records must be kept for seven years, not five”"
        rows={4}
        style={sx(INPUT + ';resize:vertical;line-height:1.6')}
      />

      {label('Document type')}
      <div style={sx('display:flex;flex-wrap:wrap;gap:6px')}>
        {entities.map((e) => (
          <button
            key={e.kind}
            data-testid={'wizard-family-' + e.kind}
            onClick={() => onChange({ family: e.kind, folder: '' })}
            style={sx('display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 11px;border-radius:8px;font-family:inherit;font-size:12px;cursor:pointer;' +
              (e.kind === family
                ? 'border:1px solid ' + e.color + ';background:color-mix(in srgb, ' + e.color + ' 10%, var(--surface));color:var(--text);font-weight:600'
                : 'border:1px solid var(--border-2);background:var(--surface);color:var(--text-2)'))}
          >
            <span style={{ color: e.color }}>{e.icon}</span>{e.label}
          </button>
        ))}
      </div>
      {ent?.description && <div style={sx('margin-top:6px;font-size:11.5px;color:var(--text-3);line-height:1.45')}>{ent.description}</div>}

      {label('Altitude')}
      <div style={sx('display:flex;gap:6px')}>
        {([['', 'Follow the workspace'], ['business', 'Business'], ['technical', 'Technical']] as const).map(([v, l]) => (
          <button
            key={v || 'default'}
            onClick={() => onChange({ altitude: v })}
            style={sx('height:28px;padding:0 12px;border-radius:8px;font-family:inherit;font-size:12px;cursor:pointer;border:1px solid ' +
              (state.altitude === v ? 'var(--ai-line);background:var(--ai-bg);color:var(--ai);font-weight:600' : 'var(--border-2);background:var(--surface);color:var(--text-2)'))}
          >
            {l}
          </button>
        ))}
      </div>

      {label('Subfolder (optional)')}
      <input
        value={state.folder}
        onChange={(e) => onChange({ folder: e.target.value })}
        placeholder={folderRoot + '…  ("a/b" nests)'}
        style={sx(INPUT)}
      />

      <div style={sx('margin-top:16px;padding:9px 12px;border:1px solid var(--border);border-radius:8px;background:var(--surface-2);font-size:11.5px;color:var(--text-2);line-height:1.6')}>
        Draft outline: {outline.join(' · ')}
        <div style={sx('color:var(--text-3);margin-top:3px')}>
          Configure per family with a <code>sections:</code> block in <code>.specquill/config.yml</code>.
        </div>
      </div>

      <div style={sx('display:flex;justify-content:flex-end;margin-top:16px')}>
        <button data-testid="wizard-start" onClick={onStart} disabled={busy || !state.intent.trim()}
          style={sx(PRIMARY + (busy || !state.intent.trim() ? ';opacity:.5;cursor:default' : ''))}>
          {busy ? 'Reading the workspace…' : 'Start →'}
        </button>
      </div>
    </div>
  );
}

// --- stage 2: existing work ----------------------------------------------

const RELATION_STYLE: Record<RelatedMatch['relation'], string> = {
  covers: 'var(--reg)',
  overlaps: 'var(--prod)',
  related: 'var(--text-2)',
};

function RelatedStep(props: {
  matches: RelatedMatch[];
  recommendation: string;
  busy: boolean;
  onExtend: (path: string) => void;
  onNew: () => void;
}) {
  const { matches, recommendation, busy, onExtend, onNew } = props;
  return (
    <div>
      <div style={sx('font-size:12.5px;color:var(--text-2);line-height:1.6;margin-bottom:12px')}>
        The workspace already has {matches.length === 1 ? 'a document' : 'documents'} in this area. Extending one keeps
        the traceability intact — a near-duplicate splits it.
      </div>
      <div style={sx('display:flex;flex-direction:column;gap:10px')} data-testid="wizard-related">
        {matches.map((m) => {
          const recommended = m.path === recommendation;
          return (
            <div key={m.path} style={sx(CARD + ';padding:13px 15px' + (recommended ? ';border-color:var(--ai-line);background:var(--ai-bg)' : ''))}>
              <div style={sx('display:flex;align-items:center;gap:9px;flex-wrap:wrap')}>
                <span style={sx('font-weight:600;font-size:13px')}>{m.title || m.path}</span>
                <span style={sx('padding:1px 8px;border-radius:20px;font-size:10.5px;font-weight:600;border:1px solid ' + RELATION_STYLE[m.relation] + ';color:' + RELATION_STYLE[m.relation])}>
                  {m.relation}
                </span>
                {recommended && <span style={sx('font-size:10.5px;font-weight:700;color:var(--ai)')}>RECOMMENDED</span>}
              </div>
              <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3);margin-top:3px")}>{m.path}</div>
              <div style={sx('font-size:12.5px;color:var(--text-2);line-height:1.6;margin-top:7px')}>{m.reason}</div>
              <button onClick={() => onExtend(m.path)} disabled={busy}
                style={sx('margin-top:10px;' + BTN + (busy ? ';opacity:.5;cursor:default' : ''))}>
                Extend this document →
              </button>
            </div>
          );
        })}
      </div>
      <div style={sx('display:flex;align-items:center;gap:12px;margin-top:16px')}>
        <button data-testid="wizard-create-new" onClick={onNew} disabled={busy} style={sx(PRIMARY + (busy ? ';opacity:.5;cursor:default' : ''))}>
          {busy ? 'Starting the interview…' : 'Create a new document anyway →'}
        </button>
        <span style={sx('font-size:11.5px;color:var(--text-3)')}>
          The documents above are recorded as <code>related:</code> in the new document's frontmatter.
        </span>
      </div>
    </div>
  );
}

// --- stage 3: interview ---------------------------------------------------

function InterviewStep(props: {
  state: WizardState;
  busy: boolean;
  drafting: boolean;
  onAnswer: (text: string) => void;
  onDraft: () => void;
}) {
  const { state, busy, drafting, onAnswer, onDraft } = props;
  const [text, setText] = useState('');
  const { met, total } = rubricProgress(state.rubric);
  const send = () => {
    if (!text.trim()) return;
    onAnswer(text);
    setText('');
  };

  return (
    <div style={sx('display:flex;gap:16px;align-items:flex-start')}>
      <div style={sx('flex:1;min-width:0')}>
        <div style={sx('display:flex;flex-direction:column;gap:12px')} data-testid="wizard-transcript">
          {state.transcript.map((m, i) =>
            m.role === 'user' ? (
              <div key={i} style={sx('align-self:flex-end;max-width:85%;padding:8px 12px;border-radius:11px 11px 3px 11px;background:var(--prod-bg);font-size:12.5px;line-height:1.55;white-space:pre-wrap')}>
                {m.content}
              </div>
            ) : (
              <div key={i} style={sx('display:flex;gap:10px')}>
                <div style={sx('width:24px;height:24px;flex:none;border-radius:7px;background:var(--ai-bg);display:flex;align-items:center;justify-content:center')}>
                  <IconSpark size={13} stroke="var(--ai)" width={1.9} />
                </div>
                <div style={sx('flex:1;font-size:12.5px;line-height:1.62;white-space:pre-wrap;min-width:0')}>{m.content}</div>
              </div>
            ),
          )}
          {busy && (
            <div style={sx('display:flex;gap:10px;align-items:center;font-size:12px;color:var(--text-3)')}>
              <IconSpark size={13} stroke="var(--ai)" width={1.9} /> reading the workspace…
            </div>
          )}
        </div>

        {state.questions.length > 0 && !busy && (
          <div style={sx('margin-top:14px;display:flex;flex-direction:column;gap:6px')} data-testid="wizard-questions">
            {state.questions.map((q) => (
              <div key={q} style={sx('display:flex;gap:8px;padding:8px 12px;border:1px solid var(--ai-line);border-radius:9px;background:var(--surface);font-size:12.5px;line-height:1.5')}>
                <span style={sx('flex:none;color:var(--ai)')}>?</span>
                <span>{q}</span>
              </div>
            ))}
          </div>
        )}

        <div style={sx('margin-top:14px;border:1px solid var(--border-2);border-radius:11px;background:var(--surface-2);padding:9px 11px')}>
          <textarea
            data-testid="wizard-answer"
            value={text}
            disabled={busy || drafting}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } }}
            placeholder="Answer the open questions (⏎ to send, ⇧⏎ for a new line)…"
            rows={2}
            style={sx('width:100%;border:none;background:transparent;color:var(--text);font-family:inherit;font-size:12.5px;resize:vertical;outline:none;line-height:1.55;box-sizing:border-box')}
          />
        </div>

        <div style={sx('display:flex;align-items:center;gap:8px;margin-top:12px;flex-wrap:wrap')}>
          <button onClick={send} disabled={busy || drafting || !text.trim()}
            style={sx(BTN + (busy || drafting || !text.trim() ? ';opacity:.5;cursor:default' : ''))}>
            Send answer
          </button>
          <button onClick={() => onAnswer('Just draft it — fill the gaps with reasonable assumptions and flag them.')}
            disabled={busy || drafting} title="Stop the interview; Speccy fills the gaps and flags its assumptions"
            style={sx(BTN + (busy || drafting ? ';opacity:.5;cursor:default' : ''))}>
            Skip — just draft it
          </button>
          <div style={sx('flex:1')} />
          <button data-testid="wizard-draft" onClick={onDraft} disabled={busy || drafting}
            style={sx((state.readyToDraft ? PRIMARY : BTN) + (busy || drafting ? ';opacity:.5;cursor:default' : ''))}>
            {drafting ? 'Writing the draft…' : 'Write the draft →'}
          </button>
        </div>
      </div>

      <div style={sx('width:250px;flex:none;' + CARD + ';padding:14px 15px;position:sticky;top:0')} data-testid="wizard-rubric">
        <div style={sx('display:flex;align-items:baseline;gap:7px')}>
          <span style={sx('font-size:12px;font-weight:700')}>Readiness</span>
          <span style={sx('font-size:11px;color:var(--text-3)')}>{total ? `${met}/${total}` : '—'}</span>
        </div>
        <div style={sx('height:5px;border-radius:3px;background:var(--surface-2);margin:9px 0 12px;overflow:hidden')}>
          <div style={{ ...sx('height:100%;background:var(--ai);transition:width .3s'), width: total ? `${(met / total) * 100}%` : '0%' }} />
        </div>
        {state.rubric.length === 0 ? (
          <div style={sx('font-size:11.5px;color:var(--text-3);line-height:1.55')}>
            Speccy builds this checklist as it interviews you.
          </div>
        ) : (
          <div style={sx('display:flex;flex-direction:column;gap:7px')}>
            {state.rubric.map((r: RubricItem) => (
              <div key={r.criterion} style={sx('display:flex;gap:7px;font-size:11.5px;line-height:1.45;color:' + (r.met ? 'var(--text-2)' : 'var(--text)'))}>
                <span style={sx('flex:none;color:' + (r.met ? 'var(--add)' : 'var(--text-3)'))}>{r.met ? '✓' : '○'}</span>
                <span>{r.criterion}</span>
              </div>
            ))}
          </div>
        )}
        {state.readyToDraft && (
          <div style={sx('margin-top:12px;padding:7px 10px;border-radius:7px;background:var(--ai-bg);color:var(--ai);font-size:11.5px;font-weight:600')}>
            Ready to draft
          </div>
        )}
      </div>
    </div>
  );
}

// --- stage 4: review ------------------------------------------------------

function ReviewStep(props: {
  state: WizardState;
  path: string;
  pathTaken: boolean;
  reserved: boolean;
  id: string;
  busy: boolean;
  busySection: string;
  canEdit: boolean;
  onTitle: (t: string) => void;
  onId: (v: string) => void;
  onSection: (name: string, content: string) => void;
  onRefine: (name: string, instruction: string) => void;
  onBack: () => void;
  onCreate: () => void;
}) {
  const { state, path, pathTaken, reserved, id, busy, busySection, canEdit } = props;
  const filled = state.sections.filter((s) => s.content.trim()).length;
  const blocked = pathTaken || reserved || !canEdit;

  return (
    <div>
      <div style={sx(CARD)}>
        {label('Title')}
        <input data-testid="wizard-title" value={state.title} onChange={(e) => props.onTitle(e.target.value)} style={sx(INPUT)} />
        {label('ID')}
        <input value={id} onChange={(e) => props.onId(e.target.value.trim())}
          style={sx(INPUT + ";font-family:'JetBrains Mono',monospace;font-size:12px")} />
        <div style={sx("margin-top:12px;padding:8px 11px;border:1px solid var(--border);border-radius:8px;background:var(--surface-2);font-family:'JetBrains Mono',monospace;font-size:11.5px;color:" + (blocked ? 'var(--del)' : 'var(--text-2)'))}
          data-testid="wizard-path">
          {path}
          {pathTaken && ' — already exists'}
          {reserved && ' — “index” and “log” are reserved'}
        </div>
        {!canEdit && (
          <div style={sx('margin-top:8px;font-size:11.5px;color:var(--del)')}>
            Your role on this project is read-only — you can copy the draft, but not create the document.
          </div>
        )}
      </div>

      <div style={sx('display:flex;align-items:center;gap:9px;margin:18px 0 10px')}>
        <span style={sx('font-size:12px;font-weight:700')}>Draft</span>
        <span style={sx('font-size:11px;color:var(--text-3)')}>{filled}/{state.sections.length} sections written</span>
      </div>

      <div style={sx('display:flex;flex-direction:column;gap:10px')} data-testid="wizard-sections">
        {state.sections.map((s) => (
          <SectionCard
            key={s.name} section={s} note={state.notes[s.name]}
            edited={state.touched.includes(s.name)} busy={busySection === s.name}
            onChange={(c) => props.onSection(s.name, c)}
            onRefine={(i) => props.onRefine(s.name, i)}
          />
        ))}
      </div>

      <div style={sx('display:flex;align-items:center;gap:9px;margin-top:18px')}>
        <button onClick={props.onBack} disabled={busy} style={sx(BTN)}>← Back to the interview</button>
        <div style={sx('flex:1')} />
        <button data-testid="wizard-create" onClick={props.onCreate} disabled={busy || blocked}
          style={sx(PRIMARY + (busy || blocked ? ';opacity:.5;cursor:default' : ''))}>
          {busy ? 'Creating…' : 'Create document & open in the editor →'}
        </button>
      </div>
      <div style={sx('margin-top:8px;font-size:11.5px;color:var(--text-3);text-align:right')}>
        Lands as an uncommitted draft on your workspace branch — review, commit, then merge or propose.
      </div>
    </div>
  );
}

function SectionCard(props: {
  section: DraftSection;
  note?: string;
  edited: boolean;
  busy: boolean;
  onChange: (content: string) => void;
  onRefine: (instruction: string) => void;
}) {
  const { section, note, edited, busy } = props;
  const [instruction, setInstruction] = useState('');
  const [open, setOpen] = useState(true);
  const empty = !section.content.trim();
  const send = () => {
    if (!instruction.trim()) return;
    props.onRefine(instruction.trim());
    setInstruction('');
  };

  return (
    <div style={sx(CARD + ';padding:0;overflow:hidden')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;border-bottom:' + (open ? '1px solid var(--border)' : 'none'))}>
        <button onClick={() => setOpen(!open)} aria-expanded={open}
          style={sx('display:flex;align-items:center;gap:7px;border:none;background:transparent;color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;padding:0')}>
          <span style={sx('color:var(--text-3);font-size:10px')}>{open ? '▾' : '▸'}</span>
          {section.name}
        </button>
        <span style={sx('padding:1px 8px;border-radius:20px;font-size:10px;font-weight:600;' +
          (empty ? 'background:var(--surface-2);color:var(--text-3);border:1px solid var(--border)'
            : edited ? 'background:var(--prod-bg);color:var(--prod);border:1px solid var(--prod)'
            : 'background:var(--ai-bg);color:var(--ai);border:1px solid var(--ai-line)'))}>
          {empty ? 'empty' : edited ? 'you edited' : 'ai draft'}
        </span>
        <div style={sx('flex:1')} />
        {open && (
          <>
            <button disabled={busy} onClick={() => props.onRefine('redraft this section')} style={sx(BTN + ';height:24px;padding:0 9px;font-size:11px' + (busy ? ';opacity:.5' : ''))}>redraft</button>
            <button disabled={busy} onClick={() => props.onRefine('tighten this section; cut anything non-essential')} style={sx(BTN + ';height:24px;padding:0 9px;font-size:11px' + (busy ? ';opacity:.5' : ''))}>tighten</button>
          </>
        )}
        {busy && <span style={sx('font-size:11px;color:var(--ai)')}>working…</span>}
      </div>

      {open && (
        <div style={sx('padding:11px 14px 13px')}>
          <textarea
            value={section.content}
            onChange={(e) => props.onChange(e.target.value)}
            placeholder="Empty — write it here, or ask Speccy below."
            rows={Math.min(18, Math.max(4, section.content.split('\n').length + 1))}
            style={sx(INPUT + ";font-family:'JetBrains Mono',monospace;font-size:11.5px;line-height:1.6;resize:vertical")}
          />
          {note && <div style={sx('margin-top:6px;font-size:11.5px;color:var(--add)')}>✓ {note}</div>}
          <div style={sx('display:flex;gap:6px;margin-top:9px')}>
            <input
              value={instruction} disabled={busy}
              onChange={(e) => setInstruction(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') send(); }}
              placeholder="Tell Speccy what to do with this section…"
              style={sx(INPUT + ';flex:1;font-size:12px')}
            />
            <button onClick={send} disabled={busy || !instruction.trim()}
              style={sx(BTN + (busy || !instruction.trim() ? ';opacity:.5;cursor:default' : ''))}>ask</button>
          </div>
        </div>
      )}
    </div>
  );
}
