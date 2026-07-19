import { createContext, useContext, useEffect, useMemo, useRef, useState, ReactNode } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useRepos, useSnapshot } from '../api/hooks';
import { buildModel, WorkspaceModel } from '../lib/model';
import { EntityDef, parseEntities } from '../lib/entities';
import { flushAllDrafts } from '../lib/draftRegistry';

export interface PropertySchema {
  order?: string[];
  fields?: Record<string, { label?: string; type?: string; values?: Record<string, string> }>;
}

export const VIEWS = ['dashboard', 'editor', 'changes', 'graph', 'matrix', 'model', 'prs'] as const;
export type ViewName = (typeof VIEWS)[number];

/** every project-scoped view root a /p/<project> URL may continue with */
export const PROJECT_VIEWS = [...VIEWS, 'diff'] as const;

/** theme preference: follow the OS (default) or pin light/dark */
export type ThemeMode = 'system' | 'light' | 'dark';

interface AppState {
  repoId?: string;                // the active project
  projects: { id: string; contentRoot?: string }[];
  projectsLoaded: boolean;        // repos query resolved — redirects may fire
  switchProject: (id: string) => void;
  branch: string;
  setBranch: (b: string) => void;
  /** switch branches; flushes open drafts first unless carrying them along */
  switchBranch: (b: string, opts?: { carryDraft?: boolean }) => void;
  protectedBranches: string[];
  isProtectedBranch: boolean;
  /** the caller's effective role on the active project (REQ-020) */
  repoRole: 'viewer' | 'member' | 'admin';
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
  entities: EntityDef[];          // effective document families (builtin + config)
  files?: Record<string, string>; // snapshot content the model was built from
  schema?: PropertySchema;
  configYml?: string;
  snapshotError?: string;
}

const Ctx = createContext<AppState>(null as unknown as AppState);
export const useApp = () => useContext(Ctx);

export function AppProvider({ children }: { children: ReactNode }) {
  const repos = useRepos();
  const navigate = useNavigate();
  const { pathname, search } = useLocation();
  const projects = (repos.data || []).filter((r) => r.kind === 'project');
  const [projectId, setProjectId] = useState<string>(() => localStorage.getItem('specquill-project') || '');
  // the URL is the source of truth for the active project; localStorage only
  // seeds project-less entry points ("/", legacy deep links)
  const urlPid = pathname.match(/^\/p\/([^/]+)/)?.[1];
  const writable =
    projects.find((r) => r.id === urlPid) ||
    projects.find((r) => r.id === projectId) ||
    projects[0];
  const [branch, setBranch] = useState('');

  // remember the project the URL names (back/forward included)
  useEffect(() => {
    if (urlPid && projects.some((r) => r.id === urlPid) && urlPid !== projectId) {
      localStorage.setItem('specquill-project', urlPid);
      setProjectId(urlPid);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlPid, repos.data]);

  // a URL naming an unknown project falls back to the default entry point
  useEffect(() => {
    if (repos.data && urlPid && !projects.some((r) => r.id === urlPid)) navigate('/', { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlPid, repos.data]);

  // branch state never leaks across projects (URL-driven switches included)
  const prevProject = useRef(writable?.id);
  useEffect(() => {
    if (prevProject.current && writable?.id && prevProject.current !== writable.id) setBranch('');
    prevProject.current = writable?.id;
  }, [writable?.id]);
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

  // the URL path carries the branch: /p/<project>/b/<encoded-branch>/<view>
  // (shareable, reload-safe). Legacy ?branch= links still resolve and are
  // canonicalized into the path — except invite links (&invite=1), which the
  // editor handles itself. localStorage remains the fallback for URLs
  // naming no branch at all.
  const pathBranch = (pathname.match(/^\/p\/[^/]+\/b\/([^/]+)/) || [])[1];
  const urlParams = new URLSearchParams(search);
  const queryBranch = urlParams.has('invite') ? '' : urlParams.get('branch') || '';
  const urlBranch = pathBranch ? decodeURIComponent(pathBranch) : queryBranch;
  const storedBranch = (writable && localStorage.getItem('specquill-branch:' + writable.id)) || '';
  const effBranch = branch || urlBranch || storedBranch || writable?.defaultBranch || 'main';
  const snapshot = useSnapshot(writable?.id, effBranch);

  // back/forward (or a pasted URL) with a different branch wins over the
  // in-memory selection
  useEffect(() => {
    if (urlBranch && branch && urlBranch !== branch) setBranch(urlBranch);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlBranch]);

  // canonicalize the URL to /p/<id>/b/<branch>/… whenever the path names no
  // branch or a stale one — but only once the repos query resolved, or the
  // pre-resolution default ('main') would be stamped and outrank the
  // remembered per-project branch on reloads
  useEffect(() => {
    if (!repos.data || !writable?.id) return;
    const m = pathname.match(/^\/p\/([^/]+)(?:\/b\/([^/]+))?(\/.*)?$/);
    if (!m) return;
    const cur = m[2] ? decodeURIComponent(m[2]) : '';
    const sp = new URLSearchParams(search);
    const legacy = sp.has('branch') && !sp.has('invite'); // pre-path-form link
    if (cur === effBranch && !legacy) return;
    if (legacy) sp.delete('branch');
    const q = sp.toString();
    navigate('/p/' + m[1] + '/b/' + encodeURIComponent(effBranch) + (m[3] || '') + (q ? '?' + q : ''), { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effBranch, pathname, search, repos.data, writable?.id]);

  useEffect(() => {
    if (writable?.id && branch) localStorage.setItem('specquill-branch:' + writable.id, branch);
  }, [branch, writable?.id]);

  // a remembered branch that no longer exists (merged, deleted, fixture
  // reset) must not wedge the workspace — drop it and fall back to default
  useEffect(() => {
    if (snapshot.error && !branch && storedBranch && writable?.id) {
      localStorage.removeItem('specquill-branch:' + writable.id);
      setBranch(writable.defaultBranch || 'main');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [snapshot.error]);

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
      projects: projects.map((p) => ({ id: p.id, contentRoot: p.contentRoot })),
      projectsLoaded: !!repos.data,
      switchProject: (id: string) => {
        if (id === writable?.id) return;
        // same discipline as switchBranch: never lose an open draft
        void flushAllDrafts().finally(() => {
          localStorage.setItem('specquill-project', id);
          setProjectId(id);
          setBranch(''); // back to the new project's default branch
          // land on the same view in the new project; file paths and query
          // state don't carry across projects
          const view = pathname.replace(/^\/p\/[^/]+(?:\/b\/[^/]+)?/, '').split('/')[1];
          if ((PROJECT_VIEWS as readonly string[]).includes(view)) navigate(`/p/${id}/${view}`);
          else if (pathname.startsWith('/p/')) navigate(`/p/${id}`);
        });
      },
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
      // least privilege until the repo list answers — write chrome must not
      // flash for viewers; 'member' only as backcompat when role is absent
      repoRole: writable ? writable.role || 'member' : 'viewer',
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
      entities: parseEntities(configYml),
      files,
      schema,
      configYml: files?.['.specquill/config.yml'],
      snapshotError: snapshot.error ? String(snapshot.error) : undefined,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [writable?.id, repos.data, effBranch, theme, themeMode, copilotOpen, aiSuggestions, snapshot.data, snapshot.error, userDefaultView, pathname]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
