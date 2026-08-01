import { useSearchParams } from 'react-router-dom';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { TIMED_STATES, buildTimed, daysLabel, statusMeta, todayISO } from '../lib/derive';
import type { TimedItem } from '../lib/derive';
import { Loading } from './Dashboard';

// state → chip color/label. Pending is the bucket the view exists for: a
// window that has not opened yet, with work possibly still behind it.
const STATE_META: Record<string, { label: string; fg: string; bg: string }> = {
  pending: { label: 'Pending', fg: 'var(--prod)', bg: 'var(--prod-bg)' },
  active: { label: 'Active', fg: 'var(--data)', bg: 'var(--data-bg)' },
  expiring: { label: 'Expiring', fg: 'var(--reg)', bg: 'var(--reg-bg)' },
  expired: { label: 'Expired', fg: 'var(--text-3)', bg: 'var(--surface-2)' },
};

/**
 * Timed dependencies — every document with a validity window, bucketed
 * against today and joined with what depends on it. Which frontmatter keys
 * carry the window is workspace config (`timed:` in .specquill/config.yml).
 */
export function TimedView() {
  const nav = useNav();
  const app = useApp();
  const [params, setParams] = useSearchParams();
  if (!app.model) return <Loading />;

  const filter = params.get('filter') || 'all';
  const { items, sel, counts } = buildTimed(app.model, todayISO(), filter, params.get('sel'));
  const def = app.model.timedDef;
  const setFilter = (f: string) => setParams(f === 'all' ? {} : { filter: f });
  const select = (path: string) => setParams(filter === 'all' ? { sel: path } : { filter, sel: path });
  const fSeg = (on: boolean) =>
    'flex:1;text-align:center;padding:4px 0;border-radius:6px;font-size:11.5px;cursor:pointer;' +
    (on ? 'font-weight:600;background:var(--surface);box-shadow:var(--shadow)' : 'color:var(--text-3)');

  if (!app.model.timed.length) return <EmptyTimed startKeys={def.start} endKeys={def.end} />;

  return (
    <div style={sx('flex:1;min-height:0;display:flex;background:var(--bg)')}>
      <div style={sx('width:328px;flex:none;border-right:1px solid var(--border);background:var(--panel);display:flex;flex-direction:column')}>
        <div style={sx('padding:12px 14px 10px;border-bottom:1px solid var(--border)')}>
          <div style={sx('display:flex;align-items:center;gap:8px')}>
            <span style={sx('font-weight:700;font-size:14px')}>Timed dependencies</span>
            <span style={sx('font-size:11px;color:var(--text-3)')}>{counts.pending} pending</span>
          </div>
          <div style={sx('display:flex;gap:4px;margin-top:10px;background:var(--surface-2);border:1px solid var(--border);border-radius:8px;padding:3px')}>
            <span onClick={() => setFilter('all')} style={sx(fSeg(filter === 'all'))}>All</span>
            {TIMED_STATES.map((s) => (
              <span key={s} onClick={() => setFilter(s)} title={`${STATE_META[s].label} — ${counts[s]}`} style={sx(fSeg(filter === s))}>
                {STATE_META[s].label}
              </span>
            ))}
          </div>
        </div>
        <div style={sx('flex:1;overflow-y:auto')}>
          {items.map((t) => {
            const m = STATE_META[t.state];
            const active = sel && sel.path === t.path;
            return (
              <div key={t.path} onClick={() => select(t.path)} data-timed={t.path}
                style={sx('padding:13px 14px;border-bottom:1px solid var(--border);cursor:pointer;' + (active ? `border-left:3px solid ${m.fg};background:var(--surface)` : 'border-left:3px solid transparent'))}>
                <div style={sx('display:flex;align-items:center;gap:7px')}>
                  <span style={sx(`display:inline-flex;align-items:center;gap:3px;padding:2px 7px;border-radius:5px;font-size:10px;font-weight:600;background:${m.bg};color:${m.fg}`)}>{m.label}</span>
                  {t.atRisk && <span style={sx('font-size:10px;font-weight:700;color:var(--reg)')}>⚠ at risk</span>}
                  <div style={sx('flex:1')} />
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>{daysLabel(t.days)}</span>
                </div>
                <div style={sx('font-weight:600;font-size:12.5px;margin-top:7px')}>{t.title || t.name}</div>
                <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);margin-top:3px")}>{windowText(t)}</div>
                <div style={sx('display:flex;align-items:center;gap:6px;margin-top:8px')}>
                  <span style={sx(`width:6px;height:6px;border-radius:50%;background:${statusMeta(t.status).color}`)} />
                  <span style={sx('font-size:10.5px;color:var(--text-2)')}>
                    {statusMeta(t.status).label || 'no status'}
                    {t.deps.length ? ` · ${t.readyCount}/${t.deps.length} dependents ready` : ' · no dependents'}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      </div>
      <div style={sx('flex:1;min-width:0;overflow-y:auto;background:var(--surface)')}>
        {sel && <TimedDetail item={sel} readyStatuses={def.readyStatuses} onOpen={(p) => nav('/editor/' + p)} onGraph={(p) => nav('/graph/' + p)} />}
      </div>
    </div>
  );
}

const windowText = (t: TimedItem) =>
  t.start && t.end ? `${t.start} → ${t.end}` : t.start ? `from ${t.start}` : t.end ? `until ${t.end}` : '';

function TimedDetail({ item, readyStatuses, onOpen, onGraph }: {
  item: TimedItem; readyStatuses: string[];
  onOpen: (path: string) => void; onGraph: (path: string) => void;
}) {
  const m = STATE_META[item.state];
  const blocked = item.deps.filter((d) => !d.ready);
  return (
    <div style={sx('max-width:680px;margin:0 auto;padding:26px 30px 60px')}>
      <div style={sx('display:flex;align-items:center;gap:9px;flex-wrap:wrap')}>
        <span style={sx(`display:inline-flex;align-items:center;gap:4px;padding:3px 9px;border-radius:6px;font-size:11px;font-weight:600;background:${m.bg};color:${m.fg}`)}>{m.label}</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>{item.id || item.name}</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>· {windowText(item)}</span>
      </div>
      <h1 style={sx('margin:14px 0 0;font-size:23px;font-weight:700;letter-spacing:-.4px')}>{item.title || item.name}</h1>

      <div style={sx('display:flex;gap:10px;margin-top:16px;flex-wrap:wrap')}>
        <Fact label={item.state === 'pending' ? 'Comes into force' : item.state === 'expired' ? 'Ended' : item.end ? 'Ends' : 'Started'}
          value={daysLabel(item.days)} sub={item.governing} accent={item.atRisk ? 'var(--reg)' : undefined} />
        <Fact label={`Own status (${item.startKey || item.endKey})`} value={statusMeta(item.status).label || '—'} sub={readyStatuses.join(' / ') + ' = ready'} />
        <Fact label="Dependents ready" value={`${item.readyCount}/${item.deps.length}`} sub={blocked.length ? `${blocked.length} outstanding` : 'nothing outstanding'} accent={blocked.length ? 'var(--reg)' : undefined} />
      </div>

      {item.atRisk && (
        <div style={sx('margin-top:16px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:11px;padding:12px 14px;font-size:12.5px;line-height:1.6;color:var(--text)')}>
          <b>At risk</b> — this window {item.state === 'pending' ? 'opens' : 'closes'} {daysLabel(item.days)} while
          {blocked.length ? ` ${blocked.length} dependent document${blocked.length === 1 ? '' : 's'} ` : ' the document itself '}
          {blocked.length ? 'still are not ' : 'is not '}in {readyStatuses.join(' / ')}.
        </div>
      )}

      <h2 style={sx('margin:24px 0 10px;font-size:14px;font-weight:700;color:var(--text-2)')}>
        Depends on this {item.deps.length ? '' : '— nothing yet'}
      </h2>
      <div style={sx('display:flex;flex-direction:column;gap:8px')}>
        {item.deps.map((d) => (
          <div key={d.path} onClick={() => onOpen(d.path)}
            style={{ ...sx('display:flex;align-items:center;gap:10px;padding:11px 14px;border-radius:9px;' + (d.ready ? 'border:1px solid var(--border)' : 'border:1px solid var(--reg-line);background:var(--reg-bg)')), cursor: 'pointer' }}>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--prod);background:var(--prod-bg);padding:2px 7px;border-radius:5px")}>{d.kind || '—'}</span>
            <span style={sx('font-size:12.5px;font-weight:500')}>{d.title}</span>
            <div style={sx('flex:1')} />
            <span style={{ ...sx('font-size:11px;font-weight:600'), color: d.ready ? 'var(--data)' : 'var(--reg)' }}>
              {d.ready ? '✓ ' : ''}{statusMeta(d.status).label || 'no status'}
            </span>
          </div>
        ))}
      </div>

      <div style={sx('display:flex;gap:8px;margin-top:22px;flex-wrap:wrap')}>
        <button onClick={() => onOpen(item.path)} style={sx('height:34px;padding:0 15px;border:none;border-radius:8px;background:var(--text);color:var(--bg);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>Open document</button>
        <button onClick={() => onGraph(item.path)} style={sx('height:34px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>Open impact graph</button>
      </div>
    </div>
  );
}

function Fact({ label, value, sub, accent }: { label: string; value: string; sub?: string; accent?: string }) {
  return (
    <div style={sx('flex:1;min-width:150px;background:var(--panel);border:1px solid var(--border);border-radius:11px;padding:12px 14px')}>
      <div style={sx('font-size:11px;color:var(--text-2)')}>{label}</div>
      <div style={{ ...sx('font-size:19px;font-weight:700;letter-spacing:-.3px;margin-top:5px'), color: accent || 'var(--text)' }}>{value}</div>
      {sub && <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);margin-top:4px")}>{sub}</div>}
    </div>
  );
}

/** No document carries a window yet — say which keys would light this up. */
function EmptyTimed({ startKeys, endKeys }: { startKeys: string[]; endKeys: string[] }) {
  return (
    <div style={sx('flex:1;display:flex;align-items:center;justify-content:center;background:var(--bg);padding:40px')}>
      <div style={sx('max-width:460px;text-align:center')}>
        <div style={sx('font-size:15px;font-weight:700')}>No timed dependencies yet</div>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:9px;line-height:1.6')}>
          A document joins this timeline as soon as its frontmatter carries a validity window. This workspace reads{' '}
          <code style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{startKeys.join(' / ')}</code> as the start and{' '}
          <code style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{endKeys.join(' / ')}</code> as the end —
          change that in <code style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>.specquill/config.yml</code> under{' '}
          <code style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>timed:</code>.
        </div>
      </div>
    </div>
  );
}
