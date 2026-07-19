import { useCallback } from 'react';
import { NavigateOptions, useLocation, useNavigate, useParams } from 'react-router-dom';
import { useApp } from './AppContext';

// Every app URL is tenant-scoped (REQ-022): /t/<tenant>/p/<project>/…
// Only /login lives outside the tenant prefix.

/** '/t/<t>/p/<id>[/b/<branch>]/editor/x' → '/editor/x' — the app-level path
 * without the tenant, project (and branch) prefix. */
export function appPath(pathname: string): string {
  const m = pathname.match(/^\/t\/[^/]+\/p\/[^/]+(?:\/b\/[^/]+)?(\/.*)?$/);
  if (m) return m[1] || '/';
  const g = pathname.match(/^\/t\/[^/]+(\/.*)?$/);
  return g ? g[1] || '/' : pathname;
}

/** The tenant slug encoded in the URL, if any. */
export function urlTenant(pathname: string): string | undefined {
  return pathname.match(/^\/t\/([^/]+)/)?.[1];
}

/** The project id encoded in the URL, if any. */
export function urlProject(pathname: string): string | undefined {
  return pathname.match(/^\/t\/[^/]+\/p\/([^/]+)/)?.[1];
}

/** The branch encoded in the URL (/t/<t>/p/<id>/b/<branch>/…), if any. */
export function urlBranch(pathname: string): string | undefined {
  const b = pathname.match(/^\/t\/[^/]+\/p\/[^/]+\/b\/([^/]+)/)?.[1];
  return b ? decodeURIComponent(b) : undefined;
}

/** Prefix an app-level path with the tenant + project (and branch) scope:
 * '/editor/x' → '/t/<tenant>/p/<id>/b/<branch>/editor/x'. '/admin' scopes to
 * the tenant only. */
export function projectPath(tenant: string | undefined, projectId: string | undefined, to: string, branch?: string): string {
  if (to === '/login' || to.startsWith('/login/')) return to;
  const t = tenant ? '/t/' + tenant : '';
  if (to === '/admin' || to.startsWith('/admin/')) return t + to;
  if (!projectId) return t || '/';
  const b = branch ? '/b/' + encodeURIComponent(branch) : '';
  return t + '/p/' + projectId + b + (to === '/' ? '' : to);
}

/** Drop-in useNavigate that keeps every URL scoped to the active tenant,
 * project and branch (shareable, reload-safe). */
export function useNav() {
  const navigate = useNavigate();
  const app = useApp();
  const { tenant } = useParams();
  return useCallback(
    (to: string, opts?: NavigateOptions) => navigate(projectPath(tenant, app.repoId, to, app.branch), opts),
    [navigate, tenant, app.repoId, app.branch],
  );
}

/** Current pathname with the tenant/project prefix stripped — for
 * active-view checks. */
export function useAppPath(): string {
  return appPath(useLocation().pathname);
}
