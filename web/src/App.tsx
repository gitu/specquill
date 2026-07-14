import { useEffect, useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { sx } from './lib/sx';
import { WorktreeChangesDrawer } from './components/WorktreeChangesDrawer';
import { AppProvider, useApp } from './state/AppContext';
import { appPath } from './state/nav';
import { ToastProvider } from './components/Toast';
import { TopBar } from './components/TopBar';
import { Rail } from './components/Rail';
import { Tree } from './components/Tree';
import { Copilot } from './components/Copilot';
import { SearchPalette } from './components/SearchPalette';
import { SyncBanner } from './components/SyncBanner';
import { useNarrow } from './hooks/useMediaQuery';

function Shell() {
  const app = useApp();
  const { pathname } = useLocation();
  const narrow = useNarrow();
  const view = appPath(pathname);
  const showTree = view.startsWith('/editor') || view.startsWith('/diff');
  const [changesOpen, setChangesOpen] = useState(false);
  const [treeOpen, setTreeOpen] = useState(false);
  useEffect(() => {
    const open = () => setChangesOpen(true);
    const tree = () => setTreeOpen((v) => !v);
    window.addEventListener('specquill:changes', open);
    window.addEventListener('specquill:tree', tree);
    return () => {
      window.removeEventListener('specquill:changes', open);
      window.removeEventListener('specquill:tree', tree);
    };
  }, []);
  // navigating (tapping a file) closes the drawer
  useEffect(() => { setTreeOpen(false); }, [pathname]);

  return (
    <div style={sx('height:100dvh;display:flex;flex-direction:column;overflow:hidden;font-size:13px;' + (narrow ? '' : 'min-height:720px'))}>
      <TopBar />
      <SyncBanner />
      <div style={sx('flex:1;display:flex;min-height:0')}>
        {!narrow && <Rail />}
        {showTree && !narrow && <Tree />}
        {showTree && narrow && treeOpen && (
          <div onClick={() => setTreeOpen(false)} style={sx('position:fixed;inset:0;top:46px;z-index:40;background:rgba(10,12,16,.35)')}>
            <div onClick={(e) => e.stopPropagation()} style={sx('position:absolute;left:0;top:0;bottom:0;display:flex;box-shadow:var(--shadow-lg)')}>
              <Tree />
            </div>
          </div>
        )}
        <main style={sx('flex:1;min-width:0;display:flex;flex-direction:column;background:var(--bg)')}>
          {app.snapshotError ? (
            <div style={sx('margin:40px auto;padding:16px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:10px;color:var(--reg);font-size:13px;max-width:560px')}>
              Couldn't load the workspace: {app.snapshotError}
            </div>
          ) : (
            <Outlet />
          )}
        </main>
        {app.copilotOpen && !narrow && <Copilot />}
        {app.copilotOpen && narrow && (
          <div onClick={app.toggleCopilot} style={sx('position:fixed;inset:0;top:46px;z-index:40;background:rgba(10,12,16,.35)')}>
            <div onClick={(e) => e.stopPropagation()} style={sx('position:absolute;right:0;top:0;bottom:0;display:flex;max-width:100vw;box-shadow:var(--shadow-lg)')}>
              <Copilot />
            </div>
          </div>
        )}
      </div>
      <SearchPalette />
      {changesOpen && <WorktreeChangesDrawer onClose={() => setChangesOpen(false)} />}
    </div>
  );
}

export default function App() {
  return (
    <AppProvider>
      <ToastProvider>
        <Shell />
      </ToastProvider>
    </AppProvider>
  );
}
