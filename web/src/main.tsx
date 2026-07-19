import React from 'react';
import ReactDOM from 'react-dom/client';
import { createBrowserRouter, Navigate, RouterProvider, useLocation, useParams } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import App from './App';
import { Dashboard } from './views/Dashboard';
import { EditorView } from './views/EditorView';
import { ChangesView } from './views/ChangesView';
import { GraphView } from './views/GraphView';
import { MatrixView } from './views/MatrixView';
import { ModelView } from './views/ModelView';
import { DiffView } from './views/DiffView';
import { PRListView, PRView } from './views/PRView';
import { LoginView } from './views/LoginView';
import { AdminView } from './views/AdminView';
import { useApp } from './state/AppContext';
import { projectPath } from './state/nav';
import { Membership, useMe } from './api/hooks';
import { sx } from './lib/sx';
import './theme.css';

// tenant-less entry points resolve to a tenant (REQ-022.3): the last-used
// tenant if still a membership, else the sole membership, else the picker
function resolveTenant(tenants: Membership[]): string | undefined {
  const last = localStorage.getItem('specquill-last-tenant');
  if (last && tenants.some((m) => m.tenant.slug === last)) return last;
  if (tenants.length === 1) return tenants[0].tenant.slug;
  return undefined;
}

function TenantPicker({ tenants }: { tenants: Membership[] }) {
  if (!tenants.length) {
    return (
      <div style={sx('margin:80px auto;max-width:420px;padding:20px;border:1px solid var(--border);border-radius:11px;background:var(--surface);font-size:13px;color:var(--text-2)')}>
        Your account has no workspace yet — ask an administrator for access.
      </div>
    );
  }
  return (
    <div style={sx('margin:80px auto;max-width:420px;display:flex;flex-direction:column;gap:10px')}>
      <div style={sx('font-weight:700;font-size:15px;margin-bottom:4px')}>Choose a workspace</div>
      {tenants.map((m) => (
        <a key={m.tenant.slug} href={'/t/' + m.tenant.slug}
          style={sx('display:flex;align-items:center;gap:10px;padding:14px 16px;border:1px solid var(--border);border-radius:11px;background:var(--surface);text-decoration:none;color:var(--text)')}>
          <span style={sx('font-weight:600;font-size:13.5px')}>{m.tenant.displayName || m.tenant.slug}</span>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>/t/{m.tenant.slug}</span>
          <span style={sx('flex:1')} />
          <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;background:var(--surface-2);color:var(--text-2)')}>{m.role}</span>
        </a>
      ))}
    </div>
  );
}

// "/" → the resolved tenant's default view
function RootRedirect() {
  const me = useMe();
  if (!me.data) return null;
  const tenants = me.data.tenants || [];
  const slug = resolveTenant(tenants);
  if (slug) return <Navigate to={'/t/' + slug} replace />;
  return <TenantPicker tenants={tenants} />;
}

// legacy tenant-less deep links (/p/…, /admin) → the same location under the
// resolved tenant
function LegacyRedirect() {
  const me = useMe();
  const { pathname, search } = useLocation();
  if (!me.data) return null;
  const tenants = me.data.tenants || [];
  const slug = resolveTenant(tenants);
  if (slug) return <Navigate to={'/t/' + slug + pathname + search} replace />;
  return <TenantPicker tenants={tenants} />;
}

// tenant index (/t/<tenant>) lands on the configured default view (user pref
// > workspace config > editor) inside the active project
function DefaultRedirect() {
  const app = useApp();
  const { tenant } = useParams();
  if (!app.projectsLoaded) return null; // the project id is not known yet
  if (!app.projects.length) return <Navigate to={projectPath(tenant, undefined, '/admin')} replace />;
  return <Navigate to={projectPath(tenant, app.repoId, '/' + app.defaultView)} replace />;
}

// project index (/t/<tenant>/p/<project>) → its default view
function ProjectIndexRedirect() {
  const app = useApp();
  return <Navigate to={app.defaultView} replace />;
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

// The project-scoped views, mounted twice: with and without the /b/<branch>
// segment (fresh RouteObject arrays — react-router mutates route configs).
const projectViews = () => [
  { index: true, element: <ProjectIndexRedirect /> },
  { path: 'dashboard', element: <Dashboard /> },
  { path: 'editor/*', element: <EditorView /> },
  { path: 'changes', element: <ChangesView /> },
  { path: 'graph', element: <GraphView /> },
  { path: 'matrix', element: <MatrixView /> },
  { path: 'model', element: <ModelView /> },
  { path: 'diff', element: <DiffView /> },
  { path: 'prs', element: <PRListView /> },
  { path: 'prs/:n', element: <PRView /> },
];

// History routing: the Go spaHandler (and Vite in dev) serve index.html for
// any unknown path, so /t/<tenant>/p/<project>/editor/<path> deep links
// resolve directly. The tenant is a path segment, never client-pinned state.
const router = createBrowserRouter([
  { path: '/login', element: <LoginView /> },
  { path: '/', element: <RootRedirect /> },
  {
    path: '/t/:tenant',
    element: <App />,
    children: [
      { index: true, element: <DefaultRedirect /> },
      {
        path: 'p/:projectId',
        children: [
          // branch-scoped form: …/p/<project>/b/<encoded-branch>/<view>
          { path: 'b/:branch', children: projectViews() },
          // branch-less form — AppContext canonicalizes it to /b/<branch>
          ...projectViews(),
        ],
      },
      { path: 'admin', element: <AdminView /> },
      { path: '*', element: <Navigate to="." replace /> },
    ],
  },
  { path: '/p/*', element: <LegacyRedirect /> },
  { path: '/admin', element: <LegacyRedirect /> },
  { path: '*', element: <Navigate to="/" replace /> },
]);

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
