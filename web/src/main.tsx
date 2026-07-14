import React from 'react';
import ReactDOM from 'react-dom/client';
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom';
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
import './theme.css';

// "/" lands on the configured default view (user pref > workspace config >
// editor) inside the active project: /p/<project>/<view>
function DefaultRedirect() {
  const app = useApp();
  if (!app.projectsLoaded) return null; // the project id is not known yet
  if (!app.projects.length) return <Navigate to="/admin" replace />;
  return <Navigate to={projectPath(app.repoId, '/' + app.defaultView)} replace />;
}

// project index (/p/<project>) → its default view
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
// any unknown path, so /p/<project>/editor/<path> deep links resolve directly.
const router = createBrowserRouter([
  { path: '/login', element: <LoginView /> },
  {
    path: '/',
    element: <App />,
    children: [
      { index: true, element: <DefaultRedirect /> },
      {
        path: 'p/:projectId',
        children: [
          // branch-scoped form: /p/<project>/b/<encoded-branch>/<view>
          { path: 'b/:branch', children: projectViews() },
          // branch-less form — AppContext canonicalizes it to /b/<branch>
          ...projectViews(),
        ],
      },
      { path: 'admin', element: <AdminView /> },
      { path: '*', element: <Navigate to="/" replace /> },
    ],
  },
]);

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
