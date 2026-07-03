import React from 'react';
import ReactDOM from 'react-dom/client';
import { createHashRouter, Navigate, RouterProvider } from 'react-router-dom';
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
import { useApp } from './state/AppContext';
import './theme.css';

// "/" lands on the configured default view (user pref > workspace config > editor)
function DefaultRedirect() {
  const app = useApp();
  return <Navigate to={'/' + app.defaultView} replace />;
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

// Hash routing keeps the prototype's #/editor/<path> deep links working and
// never disagrees with the embed.FS static file handler.
const router = createHashRouter([
  { path: '/login', element: <LoginView /> },
  {
    path: '/',
    element: <App />,
    children: [
      { index: true, element: <DefaultRedirect /> },
      { path: 'dashboard', element: <Dashboard /> },
      { path: 'editor/*', element: <EditorView /> },
      { path: 'changes', element: <ChangesView /> },
      { path: 'graph', element: <GraphView /> },
      { path: 'matrix', element: <MatrixView /> },
      { path: 'model', element: <ModelView /> },
      { path: 'diff', element: <DiffView /> },
      { path: 'prs', element: <PRListView /> },
      { path: 'prs/:n', element: <PRView /> },
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
