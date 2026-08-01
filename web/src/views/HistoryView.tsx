import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useCommitDetail, useCommitSummary, useLog } from '../api/hooks';
import type { DocDelta } from '../api/hooks';
import { buildHistory, sinceDays, singularLabel } from '../lib/history';
import type { HistoryCommit, HistoryFile } from '../lib/history';
import { daysAgo } from '../lib/derive';
import { DiffCard } from '../components/DiffCard';
import { HistoryDrawer } from '../components/HistoryDrawer';
import { IconSpark } from '../components/icons';
import { Loading } from './Dashboard';

const WINDOWS: { key: string; label: string; days: number }[] = [
  { key: '7', label: '7d', days: 7 },
  { key: '30', label: '30d', days: 30 },
  { key: '90', label: '90d', days: 90 },
  { key: 'all', label: 'All', days: 0 },
];

const CHANGE_COLOR: Record<string, string> = {
  added: 'var(--add)', deleted: 'var(--del)', renamed: 'var(--ai)', modified: 'var(--text-2)',
};

/**
 * Change history (REQ-027) — what actually changed in the workspace, read
 * from git and classified through the CURRENT workspace config, so the feed
 * speaks in requirements and specs rather than file paths. Selecting a commit
 * shows its semantic delta (properties that moved, normative statements added
 * or reworded) with the raw diff a click away.
 */
export function HistoryView() {
  const nav = useNav();
  const app = useApp();
  const [params, setParams] = useSearchParams();
  const win = params.get('win') || '90';
  const kind = params.get('kind') || '';
  const since = WINDOWS.find((w) => w.key === win)?.days;
  const log = useLog(app.repoId, app.branch, since ? sinceDays(since) : undefined, 100);

  if (!app.model) return <Loading />;
  const commits = log.data || [];
  const { days, counts, items } = buildHistory(commits, app.model, { kind });
  const selSha = params.get('sha') || items[0]?.sha;
  const sel = items.find((c) => c.sha === selSha) || items[0] || null;

  const set = (patch: Record<string, string>) => {
    const next = new URLSearchParams(params);
    Object.entries(patch).forEach(([k, v]) => (v ? next.set(k, v) : next.delete(k)));
    setParams(next);
  };
  const chip = (on: boolean) =>
    'padding:3px 9px;border-radius:20px;font-size:11px;font-weight:600;cursor:pointer;border:1px solid ' +
    (on ? 'var(--prod);background:var(--prod-bg);color:var(--prod)' : 'var(--border);background:var(--surface);color:var(--text-2)');

  return (
    <div style={sx('flex:1;min-height:0;display:flex;background:var(--bg)')}>
      <div style={sx('width:360px;flex:none;border-right:1px solid var(--border);background:var(--panel);display:flex;flex-direction:column')}>
        <div style={sx('padding:12px 14px 10px;border-bottom:1px solid var(--border)')}>
          <div style={sx('display:flex;align-items:center;gap:8px')}>
            <span style={sx('font-weight:700;font-size:14px')}>Change history</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>{app.branch}</span>
            <div style={sx('flex:1')} />
            <span style={sx('font-size:11px;color:var(--text-3)')}>{items.length} commits</span>
          </div>
          <div style={sx('display:flex;gap:5px;margin-top:10px;flex-wrap:wrap')}>
            {WINDOWS.map((w) => (
              <span key={w.key} onClick={() => set({ win: w.key, sha: '' })} style={sx(chip(win === w.key))}>{w.label}</span>
            ))}
          </div>
          <div style={sx('display:flex;gap:5px;margin-top:7px;flex-wrap:wrap')}>
            <span onClick={() => set({ kind: '', sha: '' })} style={sx(chip(!kind))}>All families</span>
            {app.model.entities.filter((e) => counts[e.kind] > 0).map((e) => (
              <span key={e.kind} onClick={() => set({ kind: e.kind, sha: '' })} style={sx(chip(kind === e.kind))}>
                <span style={{ color: e.color }}>{e.icon}</span> {singularLabel(e)} {counts[e.kind]}
              </span>
            ))}
          </div>
        </div>
        <div style={sx('flex:1;overflow-y:auto')}>
          {log.isLoading && <div style={sx("padding:20px;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:11.5px")}>reading history…</div>}
          {!log.isLoading && !items.length && (
            <div style={sx("padding:20px;color:var(--text-3);font-size:12px")}>no commits in this window</div>
          )}
          {days.map((d) => (
            <div key={d.day}>
              <div style={sx("position:sticky;top:0;padding:6px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>
                {d.day} · {daysAgo(d.day)}
              </div>
              {d.commits.map((c) => (
                <CommitRow key={c.sha} commit={c} active={sel?.sha === c.sha} onClick={() => set({ sha: c.sha })} />
              ))}
            </div>
          ))}
        </div>
      </div>
      <div style={sx('flex:1;min-width:0;overflow-y:auto;background:var(--surface)')}>
        {sel && <CommitDetail key={sel.sha} commit={sel} onOpen={(p) => nav('/editor/' + p)} />}
      </div>
    </div>
  );
}

function CommitRow({ commit, active, onClick }: { commit: HistoryCommit; active: boolean; onClick: () => void }) {
  return (
    <div onClick={onClick} data-sha={commit.sha}
      style={sx('padding:12px 14px;border-bottom:1px solid var(--border);cursor:pointer;' + (active ? 'border-left:3px solid var(--prod);background:var(--surface)' : 'border-left:3px solid transparent'))}>
      <div style={sx('display:flex;align-items:center;gap:7px')}>
        <span style={sx('font-weight:600;font-size:12.5px;flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{commit.subject}</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>{commit.sha.slice(0, 7)}</span>
      </div>
      <div style={sx('display:flex;align-items:center;gap:6px;margin-top:6px;flex-wrap:wrap')}>
        {[...new Map(commit.files.map((f) => [f.kind + f.icon, f])).values()].slice(0, 5).map((f) => (
          <span key={f.kind + f.path} title={f.label} style={{ ...sx('font-size:12px'), color: f.color }}>{f.icon}</span>
        ))}
        <span style={sx('font-size:11px;color:var(--text-2)')}>{commit.summary}</span>
      </div>
      <div style={sx('font-size:10.5px;color:var(--text-3);margin-top:5px')}>{commit.author}</div>
    </div>
  );
}

function CommitDetail({ commit, onOpen }: { commit: HistoryCommit; onOpen: (path: string) => void }) {
  const app = useApp();
  const detail = useCommitDetail(app.repoId, commit.sha, commit.parent);
  const summary = useCommitSummary(app.repoId, commit.sha, commit.parent);
  const [raw, setRaw] = useState(false);
  const [histPath, setHistPath] = useState<string | null>(null);
  const deltas = detail.data?.deltas || [];
  const byPath: Record<string, DocDelta> = {};
  deltas.forEach((d) => { byPath[d.path] = d; });

  return (
    <div style={sx('max-width:760px;margin:0 auto;padding:26px 30px 60px')}>
      <div style={sx('display:flex;align-items:center;gap:9px;flex-wrap:wrap')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>{commit.sha.slice(0, 10)}</span>
        <span style={sx('font-size:11.5px;color:var(--text-2)')}>{commit.author} · {commit.date.slice(0, 10)}</span>
      </div>
      <h1 style={sx('margin:12px 0 0;font-size:21px;font-weight:700;letter-spacing:-.4px')}>{commit.subject}</h1>

      {/* the AI summary is best-effort: no model configured ⇒ no card */}
      {(summary.data || summary.isLoading) && (
        <div style={sx('margin-top:16px;border:1px solid var(--ai-line);border-radius:11px;overflow:hidden')}>
          <div style={sx('display:flex;align-items:center;gap:8px;padding:9px 14px;background:var(--ai-bg)')}>
            <IconSpark size={13} stroke="var(--ai)" width={1.9} />
            <span style={sx('font-size:12px;font-weight:600;color:var(--ai)')}>What this changed</span>
          </div>
          <div style={sx('padding:12px 14px;font-size:13px;line-height:1.65;color:var(--text)')}>
            {summary.isLoading ? <span style={sx('color:var(--text-3)')}>reading the delta…</span> : summary.data?.summary}
          </div>
        </div>
      )}

      <div style={sx('display:flex;align-items:center;gap:10px;margin:24px 0 10px')}>
        <h2 style={sx('margin:0;font-size:14px;font-weight:700;color:var(--text-2)')}>
          {commit.files.length} file{commit.files.length === 1 ? '' : 's'} · {commit.summary}
        </h2>
        <div style={sx('flex:1')} />
        <span onClick={() => setRaw((v) => !v)} style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>
          {raw ? 'Show what changed' : 'Show text diff'}
        </span>
      </div>

      {detail.isLoading && <div style={sx("color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:11.5px")}>loading commit…</div>}

      {raw ? (
        (detail.data?.files || []).map((f) => <DiffCard key={f.path} file={f} />)
      ) : (
        <div style={sx('display:flex;flex-direction:column;gap:12px')}>
          {commit.files.map((f) => (
            <FileDelta key={f.path} file={f} delta={byPath[f.path]} onOpen={onOpen} onHistory={() => setHistPath(f.path)} />
          ))}
        </div>
      )}
      {histPath && <HistoryDrawer path={histPath} onClose={() => setHistPath(null)} />}
    </div>
  );
}

/** One document's change, in model terms — with the text diff as the fallback. */
function FileDelta({ file, delta, onOpen, onHistory }: {
  file: HistoryFile; delta?: DocDelta; onOpen: (p: string) => void; onHistory: () => void;
}) {
  const props = delta?.props || [];
  const stmts = delta?.statements || [];
  const sections = delta?.sections || [];
  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;background:var(--panel);overflow:hidden')}>
      <div style={sx("display:flex;align-items:center;gap:8px;padding:10px 14px;border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:11.5px")}>
        <span style={{ color: file.color }}>{file.icon}</span>
        <span style={sx('flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{file.path}</span>
        {file.oldPath && <span style={sx('font-size:10.5px;color:var(--text-3)')}>← {file.oldPath}</span>}
        <span style={{ ...sx('font-size:10.5px;font-weight:700'), color: CHANGE_COLOR[file.change] }}>{file.change}</span>
      </div>
      {file.title && <div style={sx('padding:10px 14px 0;font-size:13px;font-weight:600')}>{file.title}</div>}
      <div style={sx('padding:10px 14px;display:flex;flex-direction:column;gap:9px')}>
        {props.map((p) => (
          <div key={p.key} style={sx('display:flex;align-items:baseline;gap:8px;flex-wrap:wrap;font-size:12.5px')}>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2);min-width:90px")}>{p.key}</span>
            {p.before && <span style={sx('padding:1px 7px;border-radius:5px;background:var(--del-bg);color:var(--del);text-decoration:line-through')}>{p.before}</span>}
            {p.before && p.after && <span style={sx('color:var(--text-3)')}>→</span>}
            {p.after && <span style={sx('padding:1px 7px;border-radius:5px;background:var(--add-bg);color:var(--add)')}>{p.after}</span>}
            {!p.after && <span style={sx('font-size:11px;color:var(--text-3)')}>removed</span>}
          </div>
        ))}
        {stmts.map((s) => (
          <div key={s.id + s.op} style={sx('font-size:12.5px;line-height:1.6')}>
            <span style={{ ...sx("font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:700;margin-right:7px"), color: CHANGE_COLOR[s.op === 'added' ? 'added' : s.op === 'removed' ? 'deleted' : 'renamed'] }}>
              {s.op === 'added' ? '+' : s.op === 'removed' ? '−' : '~'} {s.id}
            </span>
            {s.op === 'modified' ? (
              <>
                <span style={sx('color:var(--text-3);text-decoration:line-through')}>{s.before}</span>
                <span style={sx('display:block;margin-top:2px')}>{s.after}</span>
              </>
            ) : (
              <span>{s.after || s.before}</span>
            )}
          </div>
        ))}
        {sections.map((h) => (
          <div key={h} style={sx('font-size:12px;color:var(--text-2)')}>
            <span style={{ ...sx("font-family:'JetBrains Mono',monospace;font-weight:700;margin-right:7px"), color: h.startsWith('+') ? 'var(--add)' : 'var(--del)' }}>{h.slice(0, 1)}</span>
            section “{h.slice(2)}”
          </div>
        ))}
        {!props.length && !stmts.length && !sections.length && (
          <div style={sx('font-size:12px;color:var(--text-3)')}>
            {file.change === 'renamed' ? 'moved, content unchanged' : 'prose or binary change — open the text diff for detail'}
          </div>
        )}
      </div>
      <div style={sx('display:flex;gap:14px;justify-content:flex-end;padding:0 14px 11px')}>
        <span onClick={onHistory} style={sx('font-size:11.5px;color:var(--text-2);cursor:pointer;font-weight:600')}>Document history</span>
        <span onClick={() => onOpen(file.path)} style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>Open in editor →</span>
      </div>
    </div>
  );
}
