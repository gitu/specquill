import { useQueryClient } from '@tanstack/react-query';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { api } from '../api/client';
import { useWorkspace } from '../hooks/useWorkspace';
import { parseTaxonomy, statusMeta } from '../lib/derive';
import { isReservedMd } from '../lib/model';
import { scaffoldFor } from '../lib/scaffold';
import { Loading } from './Dashboard';
import { IconArrowLR } from '../components/icons';

const TYPE_COLOR: Record<string, string> = {
  enum: 'var(--reg)', links: 'var(--prod)', percent: 'var(--data)', code: 'var(--text-2)',
  user: 'var(--text-2)', tag: 'var(--ai)', date: 'var(--text-3)', text: 'var(--text-2)',
};

export function ModelView() {
  const nav = useNav();
  const app = useApp();
  const qc = useQueryClient();
  const { ensureWritableBranch } = useWorkspace();
  if (!app.model) return <Loading />;
  const tax = parseTaxonomy(app.configYml || '');
  const files = app.files || {};

  // the .specquill/ files are optional — a workspace without them runs on
  // built-in defaults, and the buttons scaffold them on first use
  const openOrCreate = async (path: string) => {
    if (files[path] === undefined) {
      const branch = await ensureWritableBranch();
      await api<{ sha: string }>(`/api/repos/${app.repoId}/files/${path}?branch=${encodeURIComponent(branch)}`, {
        method: 'PUT',
        body: JSON.stringify({ content: scaffoldFor(path, app.repoId || '') || '', baseSha: '' }),
      });
      qc.invalidateQueries({ queryKey: ['status', app.repoId] });
      qc.invalidateQueries({ queryKey: ['snapshot', app.repoId] });
    }
    nav('/editor/' + path);
  };
  const missingCfg = files['.specquill/config.yml'] === undefined;
  const missingSchema = files['.specquill/schema.json'] === undefined;
  const docsIn = (folder: string) => Object.keys(files).filter((p) => p.startsWith(folder) && !isReservedMd(p)).sort();
  // every configured document family renders a card, empty ones included —
  // the card is where users learn what the family is for
  const entities = app.entities.map((e) => {
    const docs = docsIn(e.folder);
    return { ...e, count: docs.length, first: docs[0] || '' };
  });
  const schema = app.schema || { fields: {}, order: [] };
  const schemaFields = (schema.order || []).filter((k) => (schema.fields || {})[k]).map((k) => {
    const f = schema.fields![k];
    return { key: k, label: f.label || k, type: f.type || 'text', values: f.values ? Object.keys(f.values).join(' · ') : '' };
  });

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1000px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx('display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap')}>
          <div>
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>.specquill/config.yml · .specquill/schema.json</div>
            <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Model definitions</h1>
            <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:5px;max-width:560px;line-height:1.5')}>
              The workspace taxonomy that everything else is computed from — edit the config files to change drivers, statuses, link types, or property schema.
              {(missingCfg || missingSchema) && <> This workspace runs on <b>built-in defaults</b> — create the files to customize them.</>}
            </div>
          </div>
          <div style={sx('display:flex;gap:8px')}>
            <button onClick={() => void openOrCreate('.specquill/config.yml')} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>{missingCfg ? '+ create config.yml' : 'config.yml'}</button>
            <button onClick={() => void openOrCreate('.specquill/schema.json')} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>{missingSchema ? '+ create schema.json' : 'schema.json'}</button>
          </div>
        </div>

        <div style={sx('display:flex;align-items:baseline;gap:10px;margin-top:24px')}>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.5px")}>Entities</span>
          <span style={sx('font-size:11.5px;color:var(--text-2)')}>
            The document families this workspace is made of — add your own under <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px")}>entities:</span> in config.yml
          </span>
        </div>
        <div style={sx('display:grid;grid-template-columns:repeat(auto-fill,minmax(190px,1fr));gap:12px;margin-top:10px')}>
          {entities.map((e) => (
            <div key={e.kind} onClick={() => e.first && nav('/editor/' + e.first)} style={sx('background:var(--surface);border:1px solid var(--border);border-radius:11px;padding:14px;box-shadow:var(--shadow);' + (e.first ? 'cursor:pointer' : 'opacity:.75'))}>
              <div style={sx('display:flex;align-items:center;gap:7px')}>
                <span style={sx(`width:9px;height:9px;border-radius:3px;background:${e.color}`)} />
                <span style={sx('font-size:23px;font-weight:700;letter-spacing:-.5px')}>{e.count}</span>
                {!e.builtin && <span style={sx("font-family:'JetBrains Mono',monospace;font-size:9px;font-weight:700;padding:1px 6px;border-radius:4px;background:var(--ai-bg);color:var(--ai);letter-spacing:.4px")}>CUSTOM</span>}
              </div>
              <div style={sx('font-size:12.5px;font-weight:600;margin-top:6px')}>{e.label}</div>
              <div style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>{e.kind} · {e.folder}</div>
              {e.description && <div style={sx('font-size:11px;color:var(--text-2);margin-top:7px;line-height:1.45')}>{e.description}</div>}
            </div>
          ))}
        </div>

        <div style={sx('display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:24px;align-items:start')}>
          <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:16px 18px')}>
            <div style={sx('font-weight:700;font-size:13.5px')}>Drivers</div>
            <div style={sx('font-size:11.5px;color:var(--text-2);margin-top:2px')}>What a requirement can be driven by. Regulatory is one of several.</div>
            <div style={sx('display:flex;flex-wrap:wrap;gap:8px;margin-top:13px')}>
              {tax.drivers.map((d) => (
                <span key={d.key} style={sx(`display:inline-flex;align-items:center;gap:6px;padding:6px 12px;border-radius:8px;border:1px solid var(--border);border-left:3px solid ${d.color};background:var(--surface);font-size:12.5px;font-weight:600`)}>
                  <span style={{ color: d.color }}>{d.icon}</span>{d.label}
                </span>
              ))}
            </div>
          </div>
          <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:16px 18px')}>
            <div style={sx('font-weight:700;font-size:13.5px')}>Statuses</div>
            <div style={sx('font-size:11.5px;color:var(--text-2);margin-top:2px')}>Lifecycle states for requirements and specs.</div>
            <div style={sx('display:flex;flex-wrap:wrap;gap:8px;margin-top:13px')}>
              {tax.statuses.map((s) => {
                const m = statusMeta(s);
                return (
                  <span key={s} style={sx(`display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:20px;font-size:11.5px;font-weight:600;background:var(--surface-2);color:${m.color}`)}>
                    <span style={sx(`width:6px;height:6px;border-radius:50%;background:${m.color}`)} />{s.replace(/_/g, ' ')}
                  </span>
                );
              })}
            </div>
          </div>
        </div>

        <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;overflow:hidden')}>
          <div style={sx('padding:14px 18px;border-bottom:1px solid var(--border)')}>
            <span style={sx('font-weight:700;font-size:13.5px')}>Link types</span>
            <span style={sx('font-size:11.5px;color:var(--text-2);margin-left:8px')}>The typed edges the graph &amp; matrix are computed from</span>
          </div>
          {tax.links.map((l) => (
            <div key={l.name} style={sx('display:flex;align-items:center;gap:12px;padding:11px 18px;border-top:1px solid var(--border)')}>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:600;color:var(--prod);width:110px;flex:none")}>{l.name}</span>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;padding:2px 8px;border-radius:5px;background:var(--surface-2);color:var(--text-2)")}>{l.from}</span>
              <IconArrowLR />
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;padding:2px 8px;border-radius:5px;background:var(--surface-2);color:var(--text-2)")}>{l.to}</span>
            </div>
          ))}
        </div>

        <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;overflow:hidden')}>
          <div style={sx('padding:14px 18px;border-bottom:1px solid var(--border);display:flex;align-items:center')}>
            <span style={sx('font-weight:700;font-size:13.5px')}>Property schema</span>
            <span style={sx('font-size:11.5px;color:var(--text-2);margin-left:8px')}>Frontmatter fields — types &amp; enum values drive the Properties panel</span>
            <div style={sx('flex:1')} />
            <span onClick={() => void openOrCreate('.specquill/schema.json')} style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>{missingSchema ? 'Create schema.json →' : 'Edit schema.json →'}</span>
          </div>
          <div style={sx("display:grid;grid-template-columns:160px 90px 1fr;padding:8px 18px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
            <span>Field</span><span>Type</span><span>Enum values</span>
          </div>
          {schemaFields.map((f) => (
            <div key={f.key} style={sx('display:grid;grid-template-columns:160px 90px 1fr;align-items:center;padding:9px 18px;border-top:1px solid var(--border)')}>
              <span style={sx('font-size:12.5px;font-weight:500')}>{f.label}</span>
              <span>
                <span style={sx(`font-family:'JetBrains Mono',monospace;font-size:10.5px;padding:1px 7px;border-radius:5px;background:var(--surface-2);color:${TYPE_COLOR[f.type] || 'var(--text-2)'}`)}>{f.type}</span>
              </span>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-2)")}>{f.values}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
