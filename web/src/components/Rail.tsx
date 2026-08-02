import { ReactNode, useMemo, useState } from 'react';
import { sx } from '../lib/sx';
import { useApp, ThemeMode } from '../state/AppContext';
import { useAppPath, useNav } from '../state/nav';
import { buildTimed, todayISO } from '../lib/derive';
import { useStatus } from '../api/hooks';
import { IconAlign, IconChanges, IconChevR, IconClock, IconDash, IconFolder, IconGear, IconHistory, IconLink, IconModel, IconSpark, IconTrace } from './icons';

// each theme state carries its own glyph — the collapsed rail button shows
// it and cycles through, the expanded row renders them as a segment control
const THEMES: { mode: ThemeMode; glyph: string; label: string }[] = [
  { mode: 'system', glyph: '◐', label: 'System' },
  { mode: 'light', glyph: '☀', label: 'Light' },
  { mode: 'dark', glyph: '☾', label: 'Dark' },
];

const A = 'background:var(--surface);box-shadow:var(--shadow);color:var(--text)';
const I = 'background:transparent;color:var(--text-2)';
const BTN = 'width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;flex:none;';
const ROW = 'width:100%;height:31px;padding:0 9px;border-radius:8px;border:none;display:flex;align-items:center;gap:9px;cursor:pointer;font-family:inherit;font-size:12px;font-weight:600;text-align:left;';

interface Item { path: string; label: string; icon: ReactNode; badge?: number; badgeBg?: string }

export function Rail() {
  const nav = useNav();
  const pathname = useAppPath();
  const app = useApp();
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem('specquill-rail') === '1');
  const toggle = () => setCollapsed((c) => { localStorage.setItem('specquill-rail', c ? '0' : '1'); return !c; });
  const themeIdx = Math.max(0, THEMES.findIndex((t) => t.mode === app.themeMode));
  const themeCur = THEMES[themeIdx];
  const cycleTheme = () => app.setThemeMode(THEMES[(themeIdx + 1) % THEMES.length].mode);
  // the rail badges count what needs a human: uncommitted drafts, and timed
  // windows opening (or closing) with work still unfinished behind them
  const atRisk = useMemo(() => (app.model ? buildTimed(app.model, todayISO()).atRisk.length : 0), [app.model]);
  // the same status query the TopBar polls, so the badge costs no extra requests
  const status = useStatus(app.repoId, app.branch);
  const dirty = status.data?.dirty.length ?? 0;
  const is = (p: string) => pathname === p || pathname.startsWith(p + '/');

  // the menu states the ontology: the content, how it changes (in flight →
  // coming due → landed), and how it hangs together
  const groups: { caption: string; items: Item[] }[] = [
    {
      caption: 'Workspace',
      items: [
        { path: '/dashboard', label: 'Overview', icon: <IconDash /> },
        { path: '/editor', label: 'Specs', icon: <IconFolder /> },
        { path: '/timed', label: 'Timed dependencies', icon: <IconClock />, badge: atRisk, badgeBg: 'var(--reg)' },
        { path: '/graph', label: 'Impact graph', icon: <IconTrace /> },
      ],
    },
    {
      caption: 'Changes',
      items: [
        { path: '/changes', label: 'Pending changes', icon: <IconChanges />, badge: dirty, badgeBg: 'var(--prod)' },
        { path: '/history', label: 'Change history', icon: <IconHistory /> },
      ],
    },
    {
      caption: 'Trace',
      items: [
        { path: '/links', label: 'Links', icon: <IconLink size={18} /> },
        { path: '/alignment', label: 'Source alignment', icon: <IconAlign /> },
      ],
    },
  ];

  const badge = (n: Item) => !!n.badge && (
    <span style={sx((collapsed
      ? 'position:absolute;top:5px;right:6px;'
      : 'margin-left:auto;') +
      `min-width:15px;height:15px;padding:0 3px;border-radius:8px;background:${n.badgeBg};color:#fff;font-size:9.5px;font-weight:700;display:flex;align-items:center;justify-content:center;flex:none`)}>
      {n.badge}
    </span>
  );
  const item = (n: Item) => (
    <button key={n.path} title={n.label} onClick={() => nav(n.path)}
      style={{ ...sx((collapsed ? BTN : ROW) + (is(n.path) ? A : I)), position: 'relative' }}>
      <span style={sx('flex:none;display:flex;align-items:center;justify-content:center')}>{n.icon}</span>
      {!collapsed && <span style={sx('flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{n.label}</span>}
      {badge(n)}
    </button>
  );

  return (
    <nav style={sx(`width:${collapsed ? 52 : 176}px;flex:none;background:var(--rail);border-right:1px solid var(--border);display:flex;flex-direction:column;align-items:${collapsed ? 'center' : 'stretch'};padding:8px ${collapsed ? 0 : 7}px;gap:2px;transition:width .15s`)}>
      {groups.map((grp, gi) => (
        <div key={grp.caption} style={sx(`display:flex;flex-direction:column;gap:2px;align-items:${collapsed ? 'center' : 'stretch'}`)}>
          {collapsed
            ? gi > 0 && <div style={sx('width:24px;height:1px;background:var(--border);margin:5px auto 5px')} />
            : <div style={sx(`padding:${gi === 0 ? 4 : 12}px 10px 3px;font-family:'JetBrains Mono',monospace;font-size:9px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.5px`)}>{grp.caption}</div>}
          {grp.items.map(item)}
        </div>
      ))}
      <div style={sx('flex:1')} />
      <button title="Speccy" onClick={app.toggleSpeccy}
        style={sx((collapsed ? BTN : ROW) + (app.speccyOpen ? 'background:var(--ai-bg);' : 'background:transparent;') + 'color:var(--ai)')}>
        <span style={sx('flex:none;display:flex;align-items:center;justify-content:center')}><IconSpark /></span>
        {!collapsed && <span>Speccy</span>}
      </button>
      {/* settings as its own group: pages first, then the inline preferences —
          collapsed, only what remains navigable/clickable stays (theme cycles) */}
      <div style={sx(`display:flex;flex-direction:column;gap:2px;align-items:${collapsed ? 'center' : 'stretch'}`)}>
        {collapsed
          ? <div style={sx('width:24px;height:1px;background:var(--border);margin:5px auto 5px')} />
          : <div style={sx("padding:12px 10px 3px;font-family:'JetBrains Mono',monospace;font-size:9px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.5px")}>Settings</div>}
        {item({ path: '/admin', label: 'Projects & sources', icon: <IconGear /> })}
        {item({ path: '/model', label: 'Model definitions', icon: <IconModel /> })}
        {collapsed ? (
          <button title={`Theme: ${themeCur.label}${themeCur.mode === 'system' ? ` (${app.systemTheme})` : ''} — click to cycle`}
            onClick={cycleTheme} style={sx(BTN + I + ';font-size:14px')}>{themeCur.glyph}</button>
        ) : (
          <div style={sx(ROW.replace('cursor:pointer;', '') + I)}>
            <span style={sx('flex:none;width:19px;text-align:center;font-size:13px')}>{themeCur.glyph}</span>
            <span style={sx('flex:1')}>Theme</span>
            <span style={sx('display:inline-flex;border:1px solid var(--border-2);border-radius:6px;overflow:hidden')}>
              {THEMES.map((t) => (
                <button key={t.mode} type="button" onClick={() => app.setThemeMode(t.mode)}
                  aria-pressed={t.mode === app.themeMode}
                  title={t.mode === 'system' ? `System (${app.systemTheme})` : t.label}
                  style={sx('width:24px;height:20px;padding:0;border:none;display:flex;align-items:center;justify-content:center;font-family:inherit;font-size:11.5px;cursor:pointer;user-select:none;' +
                    (t.mode === app.themeMode ? 'background:var(--surface-2);color:var(--text)' : 'background:transparent;color:var(--text-3)'))}>
                  {t.glyph}
                </button>
              ))}
            </span>
          </div>
        )}
      </div>
      <button title={collapsed ? 'Expand menu' : 'Collapse menu'} onClick={toggle}
        style={sx((collapsed ? BTN.replace('height:40px', 'height:30px') : ROW.replace('height:31px', 'height:28px')) + 'background:transparent;color:var(--text-3)')}>
        <span style={{ ...sx('flex:none;display:flex;align-items:center;justify-content:center'), transform: collapsed ? 'none' : 'rotate(180deg)' }}>
          <IconChevR size={13} />
        </span>
        {!collapsed && <span style={sx('font-size:11px;font-weight:600')}>Collapse</span>}
      </button>
    </nav>
  );
}
