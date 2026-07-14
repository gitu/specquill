import { useEffect, useRef, useState } from 'react';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useAppPath, useNav } from '../state/nav';
import { reqByName } from '../lib/derive';
import { ChatMessage, DraftResult, draftEdits, streamChat, useCopilotInfo } from '../api/copilot';
import { useQueryClient } from '@tanstack/react-query';
import { IconSend, IconSpark } from './icons';

interface DraftCard { kind: 'draft'; result: DraftResult }
type Entry = { kind: 'msg'; msg: ChatMessage } | DraftCard;

const SUGGESTIONS = ['Which teams should we notify about the RTS 22 change?', 'Compare our retention rules to the GDPR spec'];

// The Copilot panel: streaming chat grounded on the branch snapshot, plus the
// "draft edits" flow that applies model-proposed edits to a copilot branch.
export function Copilot() {
  const nav = useNav();
  const app = useApp();
  const qc = useQueryClient();
  const pathname = useAppPath();
  const info = useCopilotInfo(app.repoId);
  const [entries, setEntries] = useState<Entry[]>([]);
  const [input, setInput] = useState('');
  const [streamText, setStreamText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const scroller = useRef<HTMLDivElement>(null);

  const enabled = info.data?.enabled === true;
  const change = app.model?.changes.find((c) => c.status === 'triage') || app.model?.changes[0];
  const focusPath = pathname.startsWith('/editor/') ? decodeURI(pathname.slice('/editor/'.length)) : undefined;

  useEffect(() => {
    scroller.current?.scrollTo({ top: scroller.current.scrollHeight });
  }, [entries, streamText]);

  const ask = async (question: string) => {
    if (!question.trim() || busy || !enabled) return;
    setError('');
    setInput('');
    const history = entries.filter((e): e is { kind: 'msg'; msg: ChatMessage } => e.kind === 'msg').map((e) => e.msg);
    const messages: ChatMessage[] = [...history, { role: 'user', content: question }];
    setEntries((es) => [...es, { kind: 'msg', msg: { role: 'user', content: question } }]);
    setBusy(true);
    setStreamText('');
    try {
      const full = await streamChat(app.repoId, { messages, focusPath, branch: app.branch }, setStreamText);
      setEntries((es) => [...es, { kind: 'msg', msg: { role: 'assistant', content: full } }]);
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setStreamText(null);
      setBusy(false);
    }
  };

  const draft = async () => {
    if (!change || !app.model || busy || !enabled) return;
    setError('');
    setBusy(true);
    try {
      const files = [
        ...change.impSpecs,
        ...change.impMaps.map((m) => m.split('#')[0]),
        ...change.impReqs.map((r) => reqByName(app.model!, r)?.path).filter((p): p is string => !!p),
      ];
      const result = await draftEdits(app.repoId, { changePath: change.path, files: [...new Set(files)] });
      setEntries((es) => [...es, { kind: 'draft', result }]);
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
    <aside style={sx('width:340px;flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;flex-direction:column')}>
      <div style={sx('height:46px;flex:none;display:flex;align-items:center;gap:9px;padding:0 14px;border-bottom:1px solid var(--border)')}>
        <IconSpark size={16} stroke="var(--ai)" />
        <span style={sx('font-weight:700;font-size:13.5px')}>Copilot</span>
        <span style={sx('display:inline-flex;align-items:center;gap:4px;font-size:10px;color:var(--text-2);background:var(--surface-2);border:1px solid var(--border);padding:2px 7px;border-radius:20px')}>
          <span style={sx('width:5px;height:5px;border-radius:50%;background:' + (enabled ? 'var(--data)' : 'var(--text-3)') + ';animation:pulse 2s infinite')} />
          {enabled ? (info.data?.model || 'grounded on repo') : 'not configured'}
        </span>
        <div style={sx('flex:1')} />
        <span onClick={app.toggleCopilot} style={sx('color:var(--text-3);cursor:pointer')}>⌵</span>
      </div>

      <div ref={scroller} style={sx('flex:1;overflow-y:auto;padding:14px;display:flex;flex-direction:column;gap:14px')}>
        <div style={sx("display:flex;align-items:center;gap:6px;flex-wrap:wrap;font-family:'JetBrains Mono',monospace;font-size:10.5px")}>
          <span style={sx('color:var(--text-3)')}>Context</span>
          {focusPath && <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)')}>@{focusPath.split('/').pop()}</span>}
          <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)')}>repo:{app.repoId}</span>
          <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)')}>{app.branch}</span>
          {info.data?.groundedSources?.map((src) => (
            <span key={src} title="Granted reference source in the copilot context" style={sx('padding:2px 7px;border-radius:5px;background:var(--reg-bg);border:1px solid var(--reg-line);color:var(--reg)')}>~{src}</span>
          ))}
        </div>

        {change && (
          <div style={sx('border:1px solid var(--reg-line);border-radius:11px;overflow:hidden;background:var(--surface)')}>
            <div style={sx('display:flex;align-items:center;gap:8px;padding:9px 13px;background:var(--reg-bg)')}>
              <span style={sx('font-size:13px')}>⚖</span>
              <span style={sx('font-size:12px;font-weight:700;color:var(--reg)')}>Regulatory change detected</span>
              <div style={sx('flex:1')} />
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--reg)")}>{change.published}</span>
            </div>
            <div style={sx('padding:11px 13px;font-size:12.5px;line-height:1.6;color:var(--text)')}>
              {change.summary}
              <div style={sx('margin-top:11px;display:flex;flex-direction:column;gap:6px')}>
                <button onClick={draft} disabled={busy || !enabled}
                  style={sx('display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--ai-line);border-radius:8px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;text-align:left;' + (busy || !enabled ? 'opacity:.5' : ''))}>
                  ✦ {busy ? 'Working…' : 'Draft edits & open as diff'}
                </button>
                <button onClick={() => nav('/graph')} style={sx('display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;cursor:pointer;text-align:left')}>
                  Open impact graph
                </button>
              </div>
            </div>
          </div>
        )}

        {entries.map((e, i) =>
          e.kind === 'msg' ? (
            <MessageRow key={i} msg={e.msg} />
          ) : (
            <DraftResultCard key={i} result={e.result} onReview={() => reviewDraft(e.result)} />
          ),
        )}
        {streamText !== null && <MessageRow msg={{ role: 'assistant', content: streamText || '…' }} streaming />}
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

      <div style={sx('flex:none;padding:12px 14px;border-top:1px solid var(--border)')}>
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
              onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void ask(input); } }}
              placeholder={enabled ? 'Ask about requirements, changes, mappings…' : 'Configure ai: in specquill.yml to enable the copilot'}
              disabled={!enabled || busy}
              rows={1}
              style={sx('flex:1;border:none;background:transparent;color:var(--text);font-family:inherit;font-size:12.5px;resize:none;outline:none;line-height:1.5')}
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
    <div style={sx('border:1px solid var(--ai-line);border-radius:11px;overflow:hidden;background:var(--surface)')}>
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
            Review on {result.branch} → commit → PR
          </button>
        )}
      </div>
    </div>
  );
}
