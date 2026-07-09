import { createContext, useContext, useEffect, useMemo, useState, ReactNode } from 'react';
import { useRepos, useSnapshot } from '../api/hooks';
import { buildModel, WorkspaceModel } from '../lib/model';
import { flushAllDrafts } from '../lib/draftRegistry';

export interface PropertySchema {
  order?: string[];
  fields?: Record<string, { label?: string; type?: string; values?: Record<string, string> }>;
}

export const VIEWS = ['dashboard', 'editor', 'changes', 'graph', 'matrix', 'model', 'prs'] as const;
export type ViewName = (typeof VIEWS)[number];

/** theme preference: follow the OS (default) or pin light/dark */
export type ThemeMode = 'system' | 'light' | 'dark';

interface AppState {
  repoId?: string;                // the writable workspace repo
  branch: string;
  setBranch: (b: string) => void;
  /** switch branches; flushes open drafts first unless carrying them along */
  switchBranch: (b: string, opts?: { carryDraft?: boolean }) => void;
  protectedBranches: string[];
  isProtectedBranch: boolean;
  theme: 'light' | 'dark';        // resolved — what actually renders
  themeMode: ThemeMode;           // the preference behind it
  systemTheme: 'light' | 'dark';  // what the OS currently prefers
  setThemeMode: (m: ThemeMode) => void;
  defaultView: ViewName;          // resolved: user pref > workspace config > editor
  userDefaultView: ViewName | null;
  workspaceDefaultView: ViewName | null;
  setDefaultView: (v: ViewName | null) => void; // null = follow workspace config
  copilotOpen: boolean;
  toggleCopilot: () => void;
  aiSuggestions: boolean;
  toggleAI: () => void;
  model?: WorkspaceModel;
  files?: Record<string, string>; // snapshot content the model was built from
  schema?: PropertySchema;
  configYml?: string;
  snapshotError?: string;
}

const Ctx = createContext<AppState>(null as unknown as AppState);
export const useApp = () => useContext(Ctx);

export function AppProvider({ children }: { children: ReactNode }) {
  const repos = useRepos();
  const writable = repos.data?.find((r) => r.kind === 'project'); // sole project until the switcher (P2)
  const [branch, setBranch] = useState('');
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => {
    const v = localStorage.getItem('specquill-theme');
    return v === 'light' || v === 'dark' ? v : 'system';
  });
  // live OS preference — system/inverse modes re-resolve when the OS flips
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches);
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const on = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  const systemTheme: 'light' | 'dark' = systemDark ? 'dark' : 'light';
  const theme: 'light' | 'dark' = themeMode === 'system' ? systemTheme : themeMode;
  // narrow screens never open the copilot by default (it overlays the doc)
  const [copilotOpen, setCopilotOpen] = useState(
    () => localStorage.getItem('specquill-copilot') !== '0' && !window.matchMedia('(max-width: 900px)').matches,
  );
  const [aiSuggestions, setAI] = useState(true);
  const [userDefaultView, setUserDefaultView] = useState<ViewName | null>(() => {
    const v = localStorage.getItem('specquill-default-view');
    return VIEWS.includes(v as ViewName) ? (v as ViewName) : null;
  });

  const effBranch = branch || writable?.defaultBranch || 'main';
  const snapshot = useSnapshot(writable?.id, effBranch);

  useEffect(() => {
    document.body.setAttribute('data-theme', theme);
  }, [theme]);
  useEffect(() => {
    localStorage.setItem('specquill-theme', themeMode);
  }, [themeMode]);
  useEffect(() => {
    localStorage.setItem('specquill-copilot', copilotOpen ? '1' : '0');
  }, [copilotOpen]);

  const value = useMemo<AppState>(() => {
    const files = snapshot.data?.files;
    let schema: PropertySchema | undefined;
    try { schema = files?.['.specquill/schema.json'] ? JSON.parse(files['.specquill/schema.json']) : undefined; } catch { schema = undefined; }
    const configYml = files?.['.specquill/config.yml'] || '';
    const wsView = (configYml.match(/^\s*default_view:\s*([\w-]+)/m) || [])[1];
    const workspaceDefaultView = VIEWS.includes(wsView as ViewName) ? (wsView as ViewName) : null;
    const protectedBranches = writable?.protectedBranches || [];
    return {
      repoId: writable?.id,
      branch: effBranch,
      setBranch,
      switchBranch: (b: string, opts?: { carryDraft?: boolean }) => {
        if (b === effBranch) return;
        if (opts?.carryDraft) {
          setBranch(b);
        } else {
          void flushAllDrafts().finally(() => setBranch(b));
        }
      },
      protectedBranches,
      isProtectedBranch: protectedBranches.includes(effBranch),
      theme,
      themeMode,
      systemTheme,
      setThemeMode,
      defaultView: userDefaultView || workspaceDefaultView || 'editor',
      userDefaultView,
      workspaceDefaultView,
      setDefaultView: (v) => {
        if (v) localStorage.setItem('specquill-default-view', v);
        else localStorage.removeItem('specquill-default-view');
        setUserDefaultView(v);
      },
      copilotOpen,
      toggleCopilot: () => setCopilotOpen((v) => !v),
      aiSuggestions,
      toggleAI: () => setAI((v) => !v),
      model: files ? buildModel(files) : undefined,
      files,
      schema,
      configYml: files?.['.specquill/config.yml'],
      snapshotError: snapshot.error ? String(snapshot.error) : undefined,
    };
  }, [writable?.id, effBranch, theme, themeMode, copilotOpen, aiSuggestions, snapshot.data, snapshot.error, userDefaultView]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
