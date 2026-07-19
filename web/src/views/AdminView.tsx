import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { api, ApiError, RepoInfo } from '../api/client';
import { useRepos, useTenant } from '../api/hooks';

interface ProjectRow {
  id: string;
  contentRoot?: string;
  defaultBranch: string;
  managedBy: 'config' | 'api';
}

// Tenant administration: projects (add/remove at runtime; config-managed
// rows come from specquill.yml and are read-only here). Requires the admin
// role — the API enforces it; this view just surfaces the errors.
export function AdminView() {
  const tenant = useTenant();
  const qc = useQueryClient();
  const projects = useQuery({ queryKey: ['t', tenant, 'projects'], queryFn: () => api<ProjectRow[]>('/api/projects') });
  const repos = useRepos();
  const sources = (repos.data || []).filter((r) => r.kind === 'source');
  const [form, setForm] = useState({ id: '', remote: '', contentRoot: '' });
  const [error, setError] = useState('');

  const create = useMutation({
    mutationFn: () => api<{ id: string }>('/api/projects', { method: 'POST', body: JSON.stringify(form) }),
    onSuccess: () => {
      setForm({ id: '', remote: '', contentRoot: '' });
      setError('');
      qc.invalidateQueries({ queryKey: ['t', tenant, 'projects'] });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'repos'] });
    },
    onError: (e) => setError(String((e as Error).message || e)),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/api/projects/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setError('');
      qc.invalidateQueries({ queryKey: ['t', tenant, 'projects'] });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'repos'] });
    },
    onError: (e) => setError(String((e as Error).message || e)),
  });

  const input = 'height:30px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px';
  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;padding:28px 34px')}>
      <div style={sx('max-width:760px;margin:0 auto')}>
        <h1 style={sx('font-size:20px;font-weight:700;letter-spacing:-.3px;margin:0 0 4px')}>Administration</h1>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-bottom:22px')}>
          Projects of this workspace. Entries from <code>specquill.yml</code> are config-managed;
          projects added here persist in the database.
        </div>
        {error && <div style={sx('padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);color:var(--reg);border-radius:8px;font-size:12.5px;margin-bottom:14px')}>{error}</div>}

        <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
          <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Projects</div>
          {(projects.data || []).map((p) => (
            <div key={p.id} style={sx('display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border)')}>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:600")}>{p.id}</span>
              {p.contentRoot && <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>/{p.contentRoot}</span>}
              <span style={sx('flex:1')} />
              <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;' + (p.managedBy === 'config' ? 'background:var(--surface-2);color:var(--text-3)' : 'background:var(--data-bg);color:var(--data)'))}>
                {p.managedBy === 'config' ? 'config' : 'in-app'}
              </span>
              {p.managedBy === 'api' && (
                <button onClick={() => remove.mutate(p.id)} disabled={remove.isPending}
                  style={sx('height:26px;padding:0 10px;border:1px solid var(--reg-line);border-radius:7px;background:var(--surface);color:var(--del);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
                  Remove
                </button>
              )}
            </div>
          ))}
          <form
            onSubmit={(e) => { e.preventDefault(); create.mutate(); }}
            style={sx('display:flex;gap:8px;align-items:center;padding:12px 14px;flex-wrap:wrap')}
          >
            <input required placeholder="id" value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} style={sx(input + ';width:130px')} />
            <input required placeholder="remote (git url or path)" value={form.remote} onChange={(e) => setForm({ ...form, remote: e.target.value })} style={sx(input + ';flex:1;min-width:200px')} />
            <input placeholder="content root (optional)" value={form.contentRoot} onChange={(e) => setForm({ ...form, contentRoot: e.target.value })} style={sx(input + ';width:180px')} />
            <button type="submit" disabled={create.isPending}
              style={sx('height:30px;padding:0 14px;border:none;border-radius:7px;background:var(--brand);color:var(--brand-fg);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
              {create.isPending ? 'Cloning…' : 'Add project'}
            </button>
          </form>
        </div>

        {sources.length > 0 && (
          <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface);margin-top:22px')}>
            <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Reference sources</div>
            {sources.map((sourceRow) => (
              <SourceRow key={sourceRow.id} source={sourceRow} onError={setError} />
            ))}
          </div>
        )}

        <GitHubReposPanel onError={setError} />
        <AccessPanel
          projects={(repos.data || []).filter((r) => r.kind === 'project' && r.role === 'admin').map((r) => r.id)}
          sources={sources.filter((s) => s.role === 'admin').map((s) => s.id)}
          onError={setError}
        />
      </div>
    </div>
  );
}

// AccessPanel — tenant members plus per-repo grants (REQ-020/REQ-021.4).
// The two sections gate independently: the Members list needs the tenant
// admin role (/api/members 403s otherwise), while Repository access renders
// for every repo the caller holds the repo admin role on — the props carry
// only those, so a repo admin without tenant admin still manages grants.
// Renders nothing when the caller administers neither.
function AccessPanel({ projects, sources, onError }: { projects: string[]; sources: string[]; onError: (m: string) => void }) {
  const tenant = useTenant();
  const qc = useQueryClient();
  interface Member { userId: number; role: string; name: string; email: string; login?: string; provider: string }
  interface Grant { repo: string; userId: number; role: string; name: string; email: string; login?: string }
  interface Invite { id: number; repo: string; kind: string; matcher: string; role: string }
  const repos = [...projects, ...sources];
  const [repo, setRepo] = useState('');
  const active = repo || repos[0] || '';
  const [form, setForm] = useState({ user: '', role: 'viewer' });
  const [notice, setNotice] = useState('');

  const members = useQuery({ queryKey: ['t', tenant, 'members'], queryFn: () => api<Member[]>('/api/members'), retry: false });
  const grants = useQuery({
    queryKey: ['t', tenant, 'grants', active],
    queryFn: () => api<{ grants: Grant[]; invites: Invite[] }>(`/api/repos/${active}/grants`),
    enabled: !!active,
    retry: false,
  });
  const invalidate = () => qc.invalidateQueries({ queryKey: ['t', tenant, 'grants', active] });
  const add = useMutation({
    mutationFn: () => api<{ status: string }>(`/api/repos/${active}/grants`, { method: 'POST', body: JSON.stringify(form) }),
    onSuccess: (r) => {
      onError('');
      setNotice(r.status === 'invited' ? `No account for “${form.user}” yet — access is granted on their first login.` : '');
      setForm({ user: '', role: 'viewer' });
      invalidate();
    },
    onError: (e) => onError(String((e as Error).message || e)),
  });
  const revoke = useMutation({
    mutationFn: (userId: number) => api(`/api/repos/${active}/grants/${userId}`, { method: 'DELETE' }),
    onSuccess: invalidate,
    onError: (e) => onError(String((e as Error).message || e)),
  });
  const withdraw = useMutation({
    mutationFn: (id: number) => api(`/api/repos/${active}/grants/invites/${id}`, { method: 'DELETE' }),
    onSuccess: invalidate,
    onError: (e) => onError(String((e as Error).message || e)),
  });
  if (members.error && !(members.error instanceof ApiError && members.error.status === 403)) {
    return (
      <div style={sx('margin-top:22px;padding:12px 14px;border:1px solid var(--border);border-radius:11px;background:var(--surface);font-size:12px;color:var(--del)')}>
        Failed to load members: {String((members.error as Error).message || members.error)}
      </div>
    );
  }
  // tenant admins see both sections; repo admins see Repository access for
  // their repos; everyone else sees nothing
  if (!members.data && repos.length === 0) return null;
  const roleChip = (role: string) =>
    'font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;' +
    (role === 'admin' ? 'background:var(--ai-bg);color:var(--ai)' : role === 'maintainer' ? 'background:var(--prod-bg);color:var(--prod)' : role === 'editor' ? 'background:var(--data-bg);color:var(--data)' : 'background:var(--surface-2);color:var(--text-2)');
  const input = 'height:30px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px';
  const btn = 'height:26px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer';
  return (
    <>
      {members.data && (
        <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface);margin-top:22px')}>
          <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Members</div>
          {members.data.map((m) => (
            <div key={m.userId} style={sx('display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border)')}>
              <span style={sx('font-size:12.5px;font-weight:600')}>{m.name}</span>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>{m.login ? '@' + m.login : m.email}</span>
              <span style={sx('flex:1')} />
              <span style={sx(roleChip(m.role))}>{m.role}</span>
            </div>
          ))}
          {members.data.length === 0 && (
            <div style={sx('padding:12px 14px;font-size:12px;color:var(--text-3)')}>No members yet.</div>
          )}
        </div>
      )}

      {repos.length > 0 && (
      <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface);margin-top:22px')}>
        <div style={sx("display:flex;align-items:center;gap:10px;padding:6px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)")}>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Repository access</span>
          <span style={sx('flex:1')} />
          <select value={active} onChange={(e) => setRepo(e.target.value)}
            style={sx("height:24px;padding:0 6px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:11px")}>
            {repos.map((r) => <option key={r} value={r}>{r}</option>)}
          </select>
        </div>
        {(grants.data?.grants || []).map((g) => (
          <div key={g.userId} style={sx('display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border)')}>
            <span style={sx('font-size:12.5px;font-weight:600')}>{g.name}</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>{g.login ? '@' + g.login : g.email}</span>
            <span style={sx('flex:1')} />
            <span style={sx(roleChip(g.role))}>{g.role}</span>
            <button disabled={revoke.isPending} onClick={() => revoke.mutate(g.userId)} style={sx(btn + ';color:var(--del);border-color:var(--reg-line)')}>Revoke</button>
          </div>
        ))}
        {(grants.data?.invites || []).map((v) => (
          <div key={v.id} style={sx('display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border)')}>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12px")}>{v.kind === 'github' ? '@' + v.matcher : v.matcher}</span>
            <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;background:var(--surface-2);color:var(--text-3)')}>invited</span>
            <span style={sx('flex:1')} />
            <span style={sx(roleChip(v.role))}>{v.role}</span>
            <button disabled={withdraw.isPending} onClick={() => withdraw.mutate(v.id)} style={sx(btn + ';color:var(--del);border-color:var(--reg-line)')}>Withdraw</button>
          </div>
        ))}
        {grants.data && grants.data.grants.length === 0 && grants.data.invites.length === 0 && (
          <div style={sx('padding:12px 14px;font-size:12px;color:var(--text-3);border-bottom:1px solid var(--border)')}>
            No explicit grants — access to {active || 'this repo'} follows tenant roles.
          </div>
        )}
        <form onSubmit={(e) => { e.preventDefault(); if (form.user.trim()) add.mutate(); }}
          style={sx('display:flex;gap:8px;align-items:center;padding:12px 14px;flex-wrap:wrap')}>
          <input required placeholder="email or @github-login" value={form.user}
            onChange={(e) => setForm({ ...form, user: e.target.value })} style={sx(input + ';flex:1;min-width:200px')} />
          <select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })} style={sx(input + ';width:110px')}>
            <option value="viewer">viewer</option>
            <option value="editor">editor</option>
            <option value="maintainer">maintainer</option>
            <option value="admin">admin</option>
          </select>
          <button type="submit" disabled={add.isPending || !active}
            style={sx('height:30px;padding:0 14px;border:none;border-radius:7px;background:var(--brand);color:var(--brand-fg);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            Grant access
          </button>
          {notice && <div style={sx('flex-basis:100%;font-size:11.5px;color:var(--text-2)')}>{notice}</div>}
        </form>
      </div>
      )}
    </>
  );
}

// GitHubReposPanel — GitHub-App tenants pick which installation repositories
// become workspaces or reference sources. Renders nothing for config tenants
// or when no GitHub App is configured (the list request 4xxes).
function GitHubReposPanel({ onError }: { onError: (m: string) => void }) {
  const tenant = useTenant();
  const qc = useQueryClient();
  interface GhRepo { fullName: string; private: boolean; description?: string; defaultBranch: string; state?: string; id?: string }
  const repos = useQuery({
    queryKey: ['t', tenant, 'github-repos'],
    queryFn: () => api<GhRepo[]>('/api/github/repos'),
    retry: false,
  });
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['t', tenant, 'github-repos'] });
    qc.invalidateQueries({ queryKey: ['t', tenant, 'repos'] });
    qc.invalidateQueries({ queryKey: ['t', tenant, 'projects'] });
  };
  const add = useMutation({
    mutationFn: (v: { fullName: string; mode: string }) =>
      api('/api/github/repos', { method: 'POST', body: JSON.stringify(v) }),
    onSuccess: invalidate,
    onError: (e) => onError(String((e as Error).message || e)),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api(`/api/github/repos/${id}`, { method: 'DELETE' }),
    onSuccess: invalidate,
    onError: (e) => onError(String((e as Error).message || e)),
  });
  if (!repos.data) return null; // not a github tenant / app not configured
  const btn = 'height:26px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer';
  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface);margin-top:22px')}>
      <div style={sx("padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
        GitHub repositories · this installation
      </div>
      {repos.data.map((r) => (
        <div key={r.fullName} style={sx('display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border)')}>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:600")}>{r.fullName}</span>
          {r.private && <span style={sx('font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:4px;padding:1px 5px')}>private</span>}
          <span style={sx('flex:1')} />
          {r.state ? (
            <>
              <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;' +
                (r.state === 'workspace' ? 'background:var(--data-bg);color:var(--data)' : 'background:var(--surface-2);color:var(--text-2)'))}>
                {r.state}
              </span>
              <button disabled={remove.isPending} onClick={() => remove.mutate(r.id!)}
                style={sx(btn + ';color:var(--del);border-color:var(--reg-line)')}>Remove</button>
            </>
          ) : (
            <>
              <button disabled={add.isPending} onClick={() => add.mutate({ fullName: r.fullName, mode: 'workspace' })} style={sx(btn)}>
                + workspace
              </button>
              <button disabled={add.isPending} onClick={() => add.mutate({ fullName: r.fullName, mode: 'reference' })} style={sx(btn)}>
                + reference
              </button>
            </>
          )}
        </div>
      ))}
      {repos.data.length === 0 && (
        <div style={sx('padding:12px 14px;font-size:12px;color:var(--text-3)')}>
          The installation grants no repositories — add some in GitHub's installation settings.
        </div>
      )}
    </div>
  );
}

// SourceRow shows one granted reference source. Importer-backed sources
// (url/openapi/confluence) carry their last-sync status and a manual re-import
// button; plain git sources show only their name.
function SourceRow({ source, onError }: { source: RepoInfo; onError: (m: string) => void }) {
  const tenant = useTenant();
  const qc = useQueryClient();
  const sync = useMutation({
    mutationFn: () => api<{ status: string; fileCount: number }>(`/api/sources/${source.id}/sync`, { method: 'POST' }),
    onSuccess: () => {
      onError('');
      qc.invalidateQueries({ queryKey: ['t', tenant, 'repos'] });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'tree'] });
    },
    onError: (e) => onError(String((e as Error).message || e)),
  });
  const ok = source.syncStatus !== 'error';
  return (
    <div style={sx('display:flex;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border)')}>
      <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:600")}>{source.id}</span>
      {source.importer && (
        <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;background:var(--surface-2);color:var(--text-3)')}>{source.importer}</span>
      )}
      {source.okf && <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;background:var(--data-bg);color:var(--data)')}>OKF</span>}
      {source.syncStatus && (
        <span title={source.syncError} style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;' + (ok ? 'background:var(--data-bg);color:var(--data)' : 'background:var(--reg-bg);color:var(--reg)'))}>
          {ok ? 'synced' : 'error'}
        </span>
      )}
      <span style={sx('flex:1')} />
      {source.importer && (
        <button onClick={() => sync.mutate()} disabled={sync.isPending}
          style={sx('height:26px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
          {sync.isPending ? 'Importing…' : 'Sync now'}
        </button>
      )}
    </div>
  );
}
