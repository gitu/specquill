import { useCallback } from 'react';
import { NavigateOptions, useLocation, useNavigate } from 'react-router-dom';
import { useApp } from './AppContext';

// Views that live outside any project scope — never prefixed with /p/<id>.
const GLOBAL = ['/admin', '/login'];

/** '/p/<id>[/b/<branch>]/editor/x' → '/editor/x' — the app-level path
 * without the project (and branch) prefix. */
export function appPath(pathname: string): string {
  const m = pathname.match(/^\/p\/[^/]+(?:\/b\/[^/]+)?(\/.*)?$/);
  return m ? m[1] || '/' : pathname;
}

/** The project id encoded in the URL, if any. */
export function urlProject(pathname: string): string | undefined {
  return pathname.match(/^\/p\/([^/]+)/)?.[1];
}

/** The branch encoded in the URL (/p/<id>/b/<branch>/…), if any. */
export function urlBranch(pathname: string): string | undefined {
  const b = pathname.match(/^\/p\/[^/]+\/b\/([^/]+)/)?.[1];
  return b ? decodeURIComponent(b) : undefined;
}

/** Prefix an app-level path with the project (and branch) scope:
 * '/editor/x' → '/p/<id>/b/<branch>/editor/x'. */
export function projectPath(projectId: string | undefined, to: string, branch?: string): string {
  if (!projectId || GLOBAL.some((g) => to === g || to.startsWith(g + '/'))) return to;
  const b = branch ? '/b/' + encodeURIComponent(branch) : '';
  return '/p/' + projectId + b + (to === '/' ? '' : to);
}

/** Drop-in useNavigate that keeps every URL scoped to the active project
 * and the active branch (shareable, reload-safe). */
export function useNav() {
  const navigate = useNavigate();
  const app = useApp();
  return useCallback(
    (to: string, opts?: NavigateOptions) => navigate(projectPath(app.repoId, to, app.branch), opts),
    [navigate, app.repoId, app.branch],
  );
}

/** Current pathname with the project prefix stripped — for active-view checks. */
export function useAppPath(): string {
  return appPath(useLocation().pathname);
}
