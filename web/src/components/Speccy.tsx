import { useEffect, useMemo, useRef, useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useAppPath, useNav } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import { buildTimed, daysLabel, todayISO } from '../lib/derive';
import { ChatMessage, DraftResult, PendingAsk, ToolEvent, draftEdits, nameChat, streamChat, useSpeccyInfo } from '../api/speccy';
import { useQueryClient } from '@tanstack/react-query';
import { IconSend, IconSpark } from './icons';
import { appendEntry, autoTitle, dismissChat, nameChatOnce, newChat, setActiveChat, updateChat, useChats } from '../state/chats';
import { upgradeSketchPixels } from '../editors/sketchUpgrade';

const SUGGESTIONS = ['Which teams should we notify about the RTS 22 change?', 'Compare our retention rules to the GDPR spec'];

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

// persisted drag-to-resize dimension: panel width / composer height survive
// reloads; parse failures and quota errors degrade to the default silently
function useStoredSize(key: string, fallback: number, lo: number, hi: number) {
  const [size, setSize] = useState(() => {
    try {
      const v = parseInt(localStorage.getItem(key) || '', 10);
      return Number.isFinite(v) ? clamp(v, lo, hi) : fallback;
    } catch {
      return fallback;
    }
  });
  useEffect(() => {
    try { localStorage.setItem(key, String(size)); } catch { /* quota */ }
  }, [key, size]);
  return [size, setSize] as const;
}

/** Pointer-drag helper: onPointerDown handler that streams deltas to onMove. */
function dragHandler(onMove: (dx: number, dy: number) => void) {
  return (e: React.PointerEvent) => {
    e.preventDefault();
    const startX = e.clientX, startY = e.clientY;
    const move = (ev: PointerEvent) => onMove(ev.clientX - startX, ev.clientY - startY);
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };
}

// The Speccy panel: streaming chat grounded on the branch snapshot — with
// tools to edit workspace files (uncommitted drafts on the workspace branch)
// and ask clarifying questions — plus the "draft edits" flow for changes.
export function Speccy() {
  const nav = useNav();
  const app = useApp();
  const qc = useQueryClient();
  const pathname = useAppPath();
  const { ensureWritableBranch } = useWorkspace();
  const info = useSpeccyInfo(app.repoId, app.branch);
  // transcripts live in the chats store (survives closing the panel);
  // streaming/busy is per-render-session, tagged with the chat it feeds
  const repoKey = app.repoId || '';
  const { chats, active } = useChats(repoKey);
  const chat = chats.find((c) => c.id === active);
  const entries = chat?.entries ?? [];
  const streamChatId = useRef('');
  const [input, setInput] = useState('');
  const [streamText, setStreamText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const scroller = useRef<HTMLDivElement>(null);

  // both windows resize by dragging: the panel at its left edge, the
  // composer at the grip above it
  const [panelW, setPanelW] = useStoredSize('specquill-speccy-w', 340, 280, 720);
  // textarea height; 19px = one line at 12.5px/1.5
  const [composerH, setComposerH] = useStoredSize('specquill-speccy-input-h', 19, 19, 300);
  const panelW0 = useRef(0);
  const composerH0 = useRef(0);
  const dragPanel = dragHandler((dx) =>
    setPanelW(clamp(panelW0.current - dx, 280, Math.min(720, Math.floor(window.innerWidth * 0.85)))));
  const dragComposer = dragHandler((_dx, dy) => setComposerH(clamp(composerH0.current - dy, 19, 300)));

  const enabled = info.data?.enabled === true;
  // edits only on a writable workspace branch — the server refuses protected
  // branches anyway, the flag just keeps the tools (and UI promise) honest
  const allowEdits = app.canEdit && !app.isProtectedBranch;
  // the document the "draft edits" flow works from: the timed dependency
  // closest to missing its window with dependents still unfinished
  const atRisk = useMemo(() => (app.model ? buildTimed(app.model, todayISO()).atRisk[0] : undefined), [app.model]);
  const focusPath = pathname.startsWith('/editor/') ? decodeURI(pathname.slice('/editor/'.length)) : undefined;

  useEffect(() => {
    scroller.current?.scrollTo({ top: scroller.current.scrollHeight });
  }, [entries, streamText]);

  const runChat = async (chatId: string, messages: ChatMessage[]) => {
    setError('');
    setBusy(true);
    streamChatId.current = chatId;
    setStreamText('');
    let lastText = '';
    try {
      let sawTool = false;
      // a move/delete of the OPEN document must not leave the editor on a
      // vanished path — follow the move, or step off the deleted file
      let openMovedTo: string | null = null;
      let openDeleted = false;
      // sketch PNGs drawn this turn: re-exported through the real excalidraw
      // after the reply (the server pixels are a clean-line approximation)
      const drawnSketches = new Set<string>();
      const result = await streamChat(
        app.repoId,
        { messages, focusPath, branch: app.branch, allowEdits },
        (t) => { lastText = t; setStreamText(t); },
        (t) => {
          sawTool = true;
          if (t.status === 'ok' && focusPath) {
            if (t.name === 'move_file' && t.path?.startsWith(focusPath + ' → ')) openMovedTo = t.path.split(' → ')[1] || null;
            if (t.name === 'delete_file' && t.path === focusPath) openDeleted = true;
          }
          if (t.status === 'ok' && t.name === 'draw_sketch' && t.path?.endsWith('.excalidraw.png')) drawnSketches.add(t.path);
          appendEntry(repoKey, chatId, { kind: 'tool', tool: t });
        },
      );
      // the server errors on empty terminal replies, but never let a stream
      // end without SOME visible outcome (the "chat just stops" bug class)
      if (!result.text && !result.ask && !sawTool) {
        setError('Speccy returned an empty reply — please try again.');
      }
      if (result.ask) {
        // any pre-question text lives inside the resume transcript — showing
        // it via the card avoids doubling it in the replayed history
        appendEntry(repoKey, chatId, { kind: 'ask', ask: result.ask, preface: result.text || undefined });
      } else if (result.text) {
        appendEntry(repoKey, chatId, { kind: 'msg', msg: { role: 'assistant', content: result.text } });
      }
      if (result.edited) {
        // speccy saved uncommitted drafts — refresh editors, tree and badges
        qc.invalidateQueries({ queryKey: ['file', app.repoId, app.branch] });
        qc.invalidateQueries({ queryKey: ['status', app.repoId, app.branch] });
        qc.invalidateQueries({ queryKey: ['snapshot', app.repoId, app.branch] });
        qc.invalidateQueries({ queryKey: ['worktreediff', app.repoId, app.branch] });
      }
      if (drawnSketches.size) {
        app.bumpSketchGen(); // show the server-rendered pixels right away
        // then quietly re-export each sketch through the real excalidraw
        void (async () => {
          let upgraded = false;
          for (const p of drawnSketches) {
            if (await upgradeSketchPixels(app.repoId!, app.branch, p)) upgraded = true;
          }
          if (upgraded) {
            app.bumpSketchGen();
            void qc.invalidateQueries({ queryKey: ['worktreediff', app.repoId, app.branch] });
          }
        })();
      }
      if (openMovedTo) nav('/editor/' + openMovedTo);
      else if (openDeleted) nav('/editor');
    } catch (e) {
      console.error('speccy: chat turn failed', e);
      // keep whatever streamed before the failure — a half answer plus a
      // visible error beats losing both
      if (lastText) {
        appendEntry(repoKey, chatId, { kind: 'msg', msg: { role: 'assistant', content: lastText + '\n\n⚠ reply interrupted' } });
      }
      setError(String((e as Error).message || e));
    } finally {
      setStreamText(null);
      setBusy(false);
    }
  };

  // conversation replayed to the model on later turns: plain messages, plus
  // answered questions reconstructed as assistant-question/user-answer pairs —
  // without them every clarified decision would be forgotten a turn later
  const textHistory = (): ChatMessage[] =>
    entries.flatMap((e): ChatMessage[] => {
      if (e.kind === 'msg') return [e.msg];
      if (e.kind === 'ask' && e.answered) {
        return [
          { role: 'assistant', content: (e.preface ? e.preface + '\n\n' : '') + e.ask.question },
          { role: 'user', content: e.answered },
        ];
      }
      return [];
    });

  const ask = async (question: string) => {
    if (!question.trim() || busy || !enabled) return;
    setInput('');
    const id = chat?.id ?? newChat(repoKey);
    const history: ChatMessage[] = chat ? textHistory() : [];
    appendEntry(repoKey, id, { kind: 'msg', msg: { role: 'user', content: question } });
    if (!chat || chat.entries.length === 0) {
      // auto-name on the first message: deterministic fallback right away,
      // upgraded by the quick-model title when/if it arrives
      nameChatOnce(repoKey, id, autoTitle(question));
      if (app.repoId) void nameChat(app.repoId, question).then((r) => nameChatOnce(repoKey, id, r.title, true)).catch(() => { /* keep fallback */ });
    }
    await runChat(id, [...history, { role: 'user', content: question }]);
  };

  // answering a pending speccy question: replay the conversation + the tool
  // transcript the server handed back, plus the answer as the tool result
  const answerAsk = async (index: number, ask: PendingAsk, answer: string) => {
    if (busy || !answer.trim() || !chat) return;
    const history = textHistory();
    updateChat(repoKey, chat.id, (c) => ({
      ...c,
      entries: c.entries.map((e, j) => (j === index && e.kind === 'ask' ? { ...e, answered: answer } : e)),
    }));
    await runChat(chat.id, [...history, ...ask.resume, { role: 'tool', tool_call_id: ask.callId, content: answer }]);
  };

  const draft = async () => {
    if (!atRisk || !app.model || busy || !enabled) return;
    setError('');
    setBusy(true);
    try {
      // impacted = the documents that depend on it and are not ready yet
      const files = atRisk.deps.filter((d) => !d.ready).map((d) => d.path);
      const result = await draftEdits(app.repoId, { changePath: atRisk.path, files: [...new Set(files)] });
      appendEntry(repoKey, chat?.id ?? newChat(repoKey), { kind: 'draft', result });
      qc.invalidateQueries({ queryKey: ['branches'] });
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusy(false);
    }
  };

  const reviewDraft = (result: DraftResult) => {
    app.switchBranch(result.branch);
    if (result.applied.length) nav('/editor/' + result.applied[0]);
  };

  return (
    <aside style={{ ...sx('flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;flex-direction:column;position:relative;overflow:hidden'), width: panelW, maxWidth: '92vw' }}>
      {/* left-edge resize handle */}
      <div
        onPointerDown={(e) => { panelW0.current = panelW; dragPanel(e); }}
        title="drag to resize"
        style={sx('position:absolute;left:-3px;top:0;bottom:0;width:7px;cursor:ew-resize;z-index:6')}
      />
      <div style={sx('height:46px;flex:none;display:flex;align-items:center;gap:9px;padding:0 14px;border-bottom:1px solid var(--border)')}>
        <IconSpark size={16} stroke="var(--ai)" />
        <span style={sx('font-weight:700;font-size:13.5px')}>Speccy</span>
        <span style={sx('display:inline-flex;align-items:center;gap:4px;font-size:10px;color:var(--text-2);background:var(--surface-2);border:1px solid var(--border);padding:2px 7px;border-radius:20px')}>
          <span style={sx('width:5px;height:5px;border-radius:50%;background:' + (enabled ? 'var(--data)' : 'var(--text-3)') + ';animation:pulse 2s infinite')} />
          {enabled ? (info.data?.model || 'grounded on repo') : 'not configured'}
        </span>
        <div style={sx('flex:1')} />
        <span onClick={app.toggleSpeccy} style={sx('color:var(--text-3);cursor:pointer')}>⌵</span>
      </div>

      {/* chat tabs: auto-named, individually dismissable */}
      {enabled && (
        <div style={sx('flex:none;display:flex;gap:6px;align-items:center;padding:6px 10px;border-bottom:1px solid var(--border);overflow-x:auto')}>
          {chats.map((c) => (
            <span
              key={c.id}
              onClick={() => setActiveChat(repoKey, c.id)}
              style={sx('flex:none;display:inline-flex;align-items:center;gap:6px;padding:3px 9px;border-radius:14px;font-size:11px;cursor:pointer;max-width:160px;border:1px solid ' +
                (c.id === active ? 'var(--ai-line);background:var(--ai-bg);color:var(--ai);font-weight:600' : 'var(--border);background:var(--surface-2);color:var(--text-2)'))}
            >
              <span style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{c.title || 'New chat'}</span>
              <span
                title="dismiss chat"
                aria-label={'dismiss ' + (c.title || 'chat')}
                onClick={(e) => { e.stopPropagation(); dismissChat(repoKey, c.id); }}
                style={sx('flex:none;color:var(--text-3);line-height:1')}
              >
                ×
              </span>
            </span>
          ))}
          <button
            onClick={() => newChat(repoKey)}
            title="new chat"
            aria-label="new chat"
            style={sx('flex:none;width:22px;height:22px;border:1px dashed var(--border-2);border-radius:11px;background:transparent;color:var(--text-2);cursor:pointer;font-family:inherit;line-height:1')}
          >
            +
          </button>
        </div>
      )}

      {/* min-height:0 lets the flex child actually shrink — without it the
          transcript grows past the panel instead of scrolling */}
      <div ref={scroller} style={sx('flex:1;min-height:0;overflow-y:auto;padding:14px;display:flex;flex-direction:column;gap:14px')}>
        <div style={sx("display:flex;align-items:center;gap:6px;flex-wrap:wrap;font-family:'JetBrains Mono',monospace;font-size:10.5px")}>
          <span style={sx('color:var(--text-3)')}>Context</span>
          {focusPath && <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)')}>@{focusPath.split('/').pop()}</span>}
          <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)')}>repo:{app.repoId}</span>
          <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)')}>{app.branch}</span>
          {info.data?.groundedSources?.map((src) => (
            <span key={src} title="Granted reference source in Speccy's context" style={sx('padding:2px 7px;border-radius:5px;background:var(--reg-bg);border:1px solid var(--reg-line);color:var(--reg)')}>~{src}</span>
          ))}
          {enabled && (allowEdits ? (
            <span title="Speccy can edit files on this branch — changes land as uncommitted drafts" style={sx('padding:2px 7px;border-radius:5px;background:var(--ai-bg);border:1px solid var(--ai-line);color:var(--ai)')}>✎ can edit</span>
          ) : app.canEdit ? (
            <span onClick={() => void ensureWritableBranch()} title="Protected branch — switch to your workspace to let Speccy edit files"
              style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-3);cursor:pointer')}>read-only · switch to edit</span>
          ) : (
            <span title="Viewer role — Speccy answers questions but cannot edit" style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-3)')}>read-only</span>
          ))}
        </div>

        {atRisk && atRisk.deps.some((d) => !d.ready) && (
          <div style={sx('flex:none;border:1px solid var(--reg-line);border-radius:11px;overflow:hidden;background:var(--surface)')}>
            <div style={sx('display:flex;align-items:center;gap:8px;padding:9px 13px;background:var(--reg-bg)')}>
              <span style={sx('font-size:13px')}>⧗</span>
              <span style={sx('font-size:12px;font-weight:700;color:var(--reg)')}>Deadline at risk</span>
              <div style={sx('flex:1')} />
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--reg)")}>{atRisk.governing}</span>
            </div>
            <div style={sx('padding:11px 13px;font-size:12.5px;line-height:1.6;color:var(--text)')}>
              <b>{atRisk.title || atRisk.name}</b> {atRisk.state === 'pending' ? 'comes into force' : 'expires'} {daysLabel(atRisk.days)},
              with {atRisk.deps.length - atRisk.readyCount} dependent document{atRisk.deps.length - atRisk.readyCount === 1 ? '' : 's'} not ready.
              <div style={sx('margin-top:11px;display:flex;flex-direction:column;gap:6px')}>
                <button onClick={draft} disabled={busy || !enabled}
                  style={sx('display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--ai-line);border-radius:8px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;text-align:left;' + (busy || !enabled ? 'opacity:.5' : ''))}>
                  ✦ {busy ? 'Working…' : 'Draft the outstanding edits'}
                </button>
                <button onClick={() => nav('/timed?sel=' + encodeURIComponent(atRisk.path))} style={sx('display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;cursor:pointer;text-align:left')}>
                  Open the timeline
                </button>
              </div>
            </div>
          </div>
        )}

        {entries.map((e, i) =>
          e.kind === 'msg' ? (
            <MessageRow key={i} msg={e.msg} />
          ) : e.kind === 'tool' ? (
            <ToolChip key={i} tool={e.tool} onOpen={(p) => nav('/editor/' + p)} />
          ) : e.kind === 'ask' ? (
            <AskCard key={i} preface={e.preface} ask={e.ask} answered={e.answered} busy={busy} onAnswer={(a) => void answerAsk(i, e.ask, a)} />
          ) : (
            <DraftResultCard key={i} result={e.result} onReview={() => reviewDraft(e.result)} />
          ),
        )}
        {streamText !== null && streamChatId.current === chat?.id && (
          <MessageRow msg={{ role: 'assistant', content: streamText || '…' }} streaming />
        )}
        {error && <div style={sx('padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>{error}</div>}

        {entries.length === 0 && enabled && (
          <div style={sx('display:flex;flex-wrap:wrap;gap:6px')}>
            {SUGGESTIONS.map((sug) => (
              <span key={sug} onClick={() => ask(sug)} style={sx('padding:5px 10px;border:1px solid var(--border);border-radius:20px;font-size:11.5px;color:var(--text-2);cursor:pointer;background:var(--surface-2)')}>
                {sug}
              </span>
            ))}
          </div>
        )}
      </div>

      <div style={sx('flex:none;padding:12px 14px;border-top:1px solid var(--border);position:relative')}>
        {/* composer resize grip (drag up to grow the input) */}
        <div
          onPointerDown={(e) => { composerH0.current = composerH; dragComposer(e); }}
          title="drag to resize"
          style={sx('position:absolute;top:-4px;left:0;right:0;height:8px;cursor:ns-resize;display:flex;align-items:center;justify-content:center')}
        >
          <span style={sx('width:34px;height:3px;border-radius:2px;background:var(--border-2)')} />
        </div>
        <div style={sx('border:1px solid var(--border-2);border-radius:11px;background:var(--surface-2);padding:9px 11px')}>
          {focusPath && (
            <div style={sx("display:flex;align-items:center;gap:6px;margin-bottom:8px;font-family:'JetBrains Mono',monospace;font-size:10px")}>
              <span style={sx('padding:2px 6px;border-radius:5px;background:var(--surface);border:1px solid var(--border);color:var(--text-2)')}>@ {focusPath.split('/').pop()}</span>
            </div>
          )}
          <div style={sx('display:flex;align-items:flex-end;gap:8px')}>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              // cmd/ctrl+enter sends; plain enter is a normal newline
              onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); void ask(input); } }}
              title="⌘/Ctrl+Enter sends — Enter starts a new line"
              placeholder={enabled ? 'Ask about requirements, deadlines, mappings… (⌘⏎ to send)' : 'Configure ai: in specquill.yml to enable Speccy'}
              disabled={!enabled || busy}
              rows={1}
              style={{ ...sx('flex:1;border:none;background:transparent;color:var(--text);font-family:inherit;font-size:12.5px;resize:none;outline:none;line-height:1.5'), height: composerH, overflowY: 'auto' }}
            />
            <button onClick={() => void ask(input)} disabled={!enabled || busy || !input.trim()}
              style={sx('width:28px;height:28px;flex:none;border:none;border-radius:8px;background:var(--ai);color:#fff;display:flex;align-items:center;justify-content:center;cursor:pointer;' + (!enabled || busy || !input.trim() ? 'opacity:.5' : ''))}>
              <IconSend />
            </button>
          </div>
        </div>
      </div>
    </aside>
  );
}

// ToolChip: one executed tool call — edit/create/move/delete/read activity.
function ToolChip({ tool, onOpen }: { tool: ToolEvent; onOpen: (path: string) => void }) {
  const err = tool.status === 'error';
  const icon = tool.name === 'read_file' ? '👁' : tool.name === 'create_file' ? '+'
    : tool.name === 'move_file' ? '⇢' : tool.name === 'delete_file' ? '✕' : '✎';
  // move_file's path reads "from → to" — clicking opens the destination; a
  // deleted path no longer exists, so it never opens
  const openTarget = tool.name === 'move_file' ? (tool.path || '').split(' → ')[1] || '' : tool.path || '';
  const openable = !!openTarget && !err && tool.name !== 'read_file' && tool.name !== 'delete_file' && !openTarget.startsWith('~');
  return (
    <div style={sx("display:flex;align-items:center;gap:7px;padding:5px 10px;border:1px solid " + (err ? 'var(--reg-line)' : 'var(--ai-line)') + ";border-radius:8px;background:" + (err ? 'var(--reg-bg)' : 'var(--ai-bg)') + ";font-family:'JetBrains Mono',monospace;font-size:11px;color:" + (err ? 'var(--reg)' : 'var(--ai)'))}>
      <span>{icon}</span>
      <span style={sx('font-weight:600')}>{tool.name.replace(/_/g, ' ')}</span>
      {tool.path && (
        <span
          onClick={openable ? () => onOpen(openTarget) : undefined}
          style={openable ? { cursor: 'pointer', textDecoration: 'underline' } : undefined}
        >
          {tool.path}
        </span>
      )}
      {err && <span title={tool.detail} style={sx('overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:160px')}>{tool.detail}</span>}
    </div>
  );
}

// AskCard: a pending speccy question — option chips plus a free-text answer.
function AskCard({ preface, ask, answered, busy, onAnswer }: {
  preface?: string;
  ask: PendingAsk;
  answered?: string;
  busy: boolean;
  onAnswer: (answer: string) => void;
}) {
  const [text, setText] = useState('');
  return (
    <div style={sx('flex:none;border:1px solid var(--ai-line);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:8px;padding:9px 13px;background:var(--ai-bg)')}>
        <IconSpark size={13} stroke="var(--ai)" width={1.9} />
        <span style={sx('font-size:12px;font-weight:600;color:var(--ai)')}>Speccy asks</span>
      </div>
      <div style={sx('padding:11px 13px;font-size:12.5px;line-height:1.6;color:var(--text)')}>
        {preface && <div style={sx('margin-bottom:8px;white-space:pre-wrap;color:var(--text-2)')}>{preface}</div>}
        <div style={sx('font-weight:600')}>{ask.question}</div>
        {answered ? (
          <div style={sx('margin-top:8px;font-size:12px;color:var(--text-2)')}>↳ {answered}</div>
        ) : (
          <>
            <div style={sx('margin-top:9px;display:flex;flex-wrap:wrap;gap:6px')}>
              {(ask.options || []).map((o) => (
                <button key={o} disabled={busy} onClick={() => onAnswer(o)}
                  style={sx('padding:5px 11px;border:1px solid var(--ai-line);border-radius:20px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:11.5px;cursor:pointer;' + (busy ? 'opacity:.5' : ''))}>
                  {o}
                </button>
              ))}
            </div>
            <input
              value={text}
              placeholder="or answer in your own words ⏎"
              disabled={busy}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && text.trim()) onAnswer(text); }}
              style={sx('margin-top:8px;width:100%;box-sizing:border-box;padding:6px 10px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12px;outline:none')}
            />
          </>
        )}
      </div>
    </div>
  );
}

function MessageRow({ msg, streaming }: { msg: ChatMessage; streaming?: boolean }) {
  if (msg.role === 'user') {
    return (
      <div style={sx('align-self:flex-end;max-width:85%;padding:8px 12px;border-radius:11px 11px 3px 11px;background:var(--prod-bg);color:var(--text);font-size:12.5px;line-height:1.55;white-space:pre-wrap')}>
        {msg.content}
      </div>
    );
  }
  return (
    <div style={sx('display:flex;gap:10px')}>
      <div style={sx('width:24px;height:24px;flex:none;border-radius:7px;background:var(--ai-bg);display:flex;align-items:center;justify-content:center')}>
        <IconSpark size={13} stroke="var(--ai)" width={1.9} />
      </div>
      <div style={sx('flex:1;font-size:12.5px;line-height:1.62;color:var(--text);white-space:pre-wrap;min-width:0')}>
        {msg.content}
        {streaming && <span style={sx('display:inline-block;width:7px;height:13px;background:var(--ai);margin-left:2px;animation:blink 1s infinite;vertical-align:text-bottom')} />}
      </div>
    </div>
  );
}

function DraftResultCard({ result, onReview }: { result: DraftResult; onReview: () => void }) {
  return (
    <div style={sx('flex:none;border:1px solid var(--ai-line);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:8px;padding:9px 13px;background:var(--ai-bg)')}>
        <IconSpark size={13} stroke="var(--ai)" width={1.9} />
        <span style={sx('font-size:12px;font-weight:600;color:var(--ai)')}>Edits drafted on</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--ai)")}>{result.branch}</span>
      </div>
      <div style={sx('padding:11px 13px;font-size:12.5px;line-height:1.6;color:var(--text)')}>
        {result.summary}
        <div style={sx('margin-top:9px;display:flex;flex-direction:column;gap:4px')}>
          {result.applied.map((p) => (
            <div key={p} style={sx("display:flex;gap:7px;font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2)")}>
              <span style={sx('color:var(--add)')}>✎</span>{p}
            </div>
          ))}
          {result.failures.map((f) => (
            <div key={f} style={sx("display:flex;gap:7px;font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--reg)")}>
              <span>⚠</span>{f}
            </div>
          ))}
        </div>
        {result.applied.length > 0 && (
          <button onClick={onReview} style={sx('margin-top:11px;display:flex;align-items:center;gap:8px;padding:8px 11px;border:none;border-radius:8px;background:var(--ai);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
            Review on {result.branch} → commit → merge
          </button>
        )}
      </div>
    </div>
  );
}
