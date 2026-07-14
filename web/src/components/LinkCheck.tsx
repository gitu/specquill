import { useState } from 'react';
import { api } from '../api/client';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useNav } from '../state/nav';

interface LinkProblem { file: string; href: string; target?: string; kind: string; status: string; detail?: string }
interface LinkCounts { ok: number; broken: number; skipped?: number }
interface LinkReport { ref: string; counts: Record<string, LinkCounts>; problems: LinkProblem[] | null }

const KIND_COLOR: Record<string, string> = { internal: 'var(--prod)', source: 'var(--ai)', external: 'var(--text-2)' };

/**
 * On-demand link verification card: internal links against the branch,
 * ~source links against granted sources, external URLs probed server-side.
 */
export function LinkCheckCard() {
  const app = useApp();
  const nav = useNav();
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  const [report, setReport] = useState<LinkReport | null>(null);

  const run = async () => {
    setRunning(true);
    setError('');
    try {
      setReport(await api<LinkReport>(`/api/repos/${app.repoId}/linkcheck?ref=${encodeURIComponent(app.branch)}`));
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setRunning(false);
    }
  };

  const problems = report?.problems || [];
  return (
    <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden')}>
      <div style={sx('display:flex;align-items:center;gap:8px;padding:13px 16px;' + (report || error ? 'border-bottom:1px solid var(--border)' : ''))}>
        <span style={sx('font-weight:700;font-size:13.5px')}>Link health</span>
        <span style={sx('font-size:11px;color:var(--text-3)')}>internal · sources · external</span>
        <div style={sx('flex:1')} />
        <button onClick={run} disabled={running}
          style={sx('height:26px;padding:0 11px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer;' + (running ? 'opacity:.6' : ''))}>
          {running ? 'Checking…' : report ? 'Re-check' : 'Verify links'}
        </button>
      </div>
      {error && <div style={sx('padding:11px 16px;font-size:11.5px;color:var(--reg)')}>check failed: {error}</div>}
      {report && (
        <>
          <div style={sx('display:flex;gap:14px;padding:11px 16px;flex-wrap:wrap')}>
            {(['internal', 'source', 'external'] as const).map((k) => {
              const c = report.counts[k] || { ok: 0, broken: 0 };
              return (
                <span key={k} style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2)")}>
                  <span style={sx(`font-weight:700;color:${KIND_COLOR[k]}`)}>{k}</span>{' '}
                  {c.ok} ok{c.broken > 0 && <span style={sx('color:var(--reg);font-weight:700')}> · {c.broken} broken</span>}
                  {(c.skipped || 0) > 0 && <span style={sx('color:var(--text-3)')}> · {c.skipped} skipped</span>}
                </span>
              );
            })}
          </div>
          {problems.length === 0 ? (
            <div style={sx('padding:0 16px 12px;font-size:11.5px;color:var(--data);font-weight:600')}>✓ all links resolve</div>
          ) : (
            <div style={sx('max-height:260px;overflow-y:auto')}>
              {problems.map((p, i) => (
                <div key={i} onClick={() => nav('/editor/' + p.file)}
                  style={sx('display:flex;align-items:baseline;gap:8px;padding:8px 16px;border-top:1px solid var(--border);cursor:pointer;font-size:11.5px')}>
                  <span style={sx(`flex:none;font-family:'JetBrains Mono',monospace;font-size:9.5px;font-weight:700;color:${KIND_COLOR[p.kind] || 'var(--text-2)'}`)}>{p.kind}</span>
                  <div style={sx('min-width:0')}>
                    <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap")}>
                      {p.file} → <span style={sx('color:var(--reg)')}>{p.href}</span>
                    </div>
                    {p.detail && <div style={sx('font-size:10.5px;color:var(--text-3);margin-top:1px')}>{p.detail}</div>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
