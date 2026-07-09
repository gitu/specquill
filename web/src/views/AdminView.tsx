import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { api, RepoInfo } from '../api/client';
import { useRepos } from '../api/hooks';

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
  const qc = useQueryClient();
  const projects = useQuery({ queryKey: ['projects'], queryFn: () => api<ProjectRow[]>('/api/projects') });
  const repos = useRepos();
  const sources = (repos.data || []).filter((r) => r.kind === 'source');
  const [form, setForm] = useState({ id: '', remote: '', contentRoot: '' });
  const [error, setError] = useState('');

  const create = useMutation({
    mutationFn: () => api<{ id: string }>('/api/projects', { method: 'POST', body: JSON.stringify(form) }),
    onSuccess: () => {
      setForm({ id: '', remote: '', contentRoot: '' });
      setError('');
      qc.invalidateQueries({ queryKey: ['projects'] });
      qc.invalidateQueries({ queryKey: ['repos'] });
    },
    onError: (e) => setError(String((e as Error).message || e)),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/api/projects/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setError('');
      qc.invalidateQueries({ queryKey: ['projects'] });
      qc.invalidateQueries({ queryKey: ['repos'] });
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
      </div>
    </div>
  );
}

// SourceRow shows one granted reference source. Importer-backed sources
// (url/openapi/confluence) carry their last-sync status and a manual re-import
// button; plain git sources show only their name.
function SourceRow({ source, onError }: { source: RepoInfo; onError: (m: string) => void }) {
  const qc = useQueryClient();
  const sync = useMutation({
    mutationFn: () => api<{ status: string; fileCount: number }>(`/api/sources/${source.id}/sync`, { method: 'POST' }),
    onSuccess: () => {
      onError('');
      qc.invalidateQueries({ queryKey: ['repos'] });
      qc.invalidateQueries({ queryKey: ['tree'] });
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
