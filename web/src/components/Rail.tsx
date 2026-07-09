import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp, VIEWS, ViewName, ThemeMode } from '../state/AppContext';
import { buildDashboard } from '../lib/derive';
import { IconChanges, IconDash, IconFolder, IconGear, IconMatrix, IconModel, IconSpark, IconTrace } from './icons';

const VIEW_LABEL: Record<ViewName, string> = {
  dashboard: 'Overview', editor: 'Specs', changes: 'Changes', graph: 'Graph',
  matrix: 'Matrix', model: 'Model definitions', prs: 'Pull requests',
};

const A = 'background:var(--surface);box-shadow:var(--shadow);color:var(--text)';
const I = 'background:transparent;color:var(--text-2)';
const BTN = 'width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;';

export function Rail() {
  const nav = useNavigate();
  const { pathname } = useLocation();
  const app = useApp();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const openCount = app.model ? buildDashboard(app.model).openCount : 0;
  const is = (p: string) => pathname === p || pathname.startsWith(p + '/');

  return (
    <nav style={sx('width:52px;flex:none;background:var(--rail);border-right:1px solid var(--border);display:flex;flex-direction:column;align-items:center;padding:8px 0;gap:3px')}>
      <button title="Overview" onClick={() => nav('/dashboard')} style={sx(BTN + (is('/dashboard') ? A : I))}><IconDash /></button>
      <button title="Specs" onClick={() => nav('/editor')} style={sx(BTN + (is('/editor') || is('/diff') ? A : I))}><IconFolder /></button>
      <button title="Changes" onClick={() => nav('/changes')} style={{ ...sx(BTN + (is('/changes') ? A : I)), position: 'relative' }}>
        <IconChanges />
        {openCount > 0 && (
          <span style={sx('position:absolute;top:5px;right:6px;min-width:15px;height:15px;padding:0 3px;border-radius:8px;background:var(--reg);color:#fff;font-size:9.5px;font-weight:700;display:flex;align-items:center;justify-content:center')}>
            {openCount}
          </span>
        )}
      </button>
      <button title="Traceability" onClick={() => nav('/graph')} style={sx(BTN + (is('/graph') ? A : I))}><IconTrace /></button>
      <button title="Traceability matrix" onClick={() => nav('/matrix')} style={sx(BTN + (is('/matrix') ? A : I))}><IconMatrix /></button>
      <button title="Model definitions" onClick={() => nav('/model')} style={sx(BTN + (is('/model') ? A : I))}><IconModel /></button>
      <div style={sx('flex:1')} />
      <button title="Copilot" onClick={app.toggleCopilot} style={sx(BTN + (app.copilotOpen ? 'background:var(--ai-bg);' : 'background:transparent;') + 'color:var(--ai)')}>
        <IconSpark />
      </button>
      <div style={sx('position:relative')}>
        <button title="Settings" onClick={() => setSettingsOpen((v) => !v)} style={sx(BTN + (settingsOpen ? A : 'background:transparent;color:var(--text-2)'))}>
          <IconGear />
        </button>
        {settingsOpen && (
          <div style={sx('position:absolute;left:48px;bottom:0;width:230px;background:var(--surface);border:1px solid var(--border);border-radius:11px;box-shadow:var(--shadow-lg);padding:12px 14px;z-index:30')}>
            <div style={sx('font-weight:700;font-size:12.5px;margin-bottom:10px')}>Settings</div>
            <div style={sx('display:flex;align-items:center;gap:8px;margin-bottom:11px')}>
              <span style={sx('font-size:11.5px;color:var(--text-2);flex:1')}>Theme</span>
              <select
                value={app.themeMode}
                onChange={(e) => app.setThemeMode(e.target.value as ThemeMode)}
                style={sx('height:24px;padding:0 6px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer')}
              >
                <option value="system">System ({app.systemTheme})</option>
                {/* pins: the one opposing the OS first — it's the likely reach */}
                {app.systemTheme === 'dark'
                  ? [<option key="l" value="light">Light</option>, <option key="d" value="dark">Dark</option>]
                  : [<option key="d" value="dark">Dark</option>, <option key="l" value="light">Light</option>]}
              </select>
            </div>
            <div style={sx('display:flex;align-items:center;gap:8px')}>
              <span style={sx('font-size:11.5px;color:var(--text-2);flex:1')}>Default view</span>
              <select
                value={app.userDefaultView ?? ''}
                onChange={(e) => { app.setDefaultView((e.target.value || null) as ViewName | null); setSettingsOpen(false); }}
                style={sx('height:24px;padding:0 5px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:11px')}
              >
                <option value="">
                  workspace ({VIEW_LABEL[app.workspaceDefaultView || 'editor']})
                </option>
                {VIEWS.map((v) => <option key={v} value={v}>{VIEW_LABEL[v]}</option>)}
              </select>
            </div>
          </div>
        )}
      </div>
    </nav>
  );
}
