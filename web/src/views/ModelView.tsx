import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { api } from '../api/client';
import { useWorkspace } from '../hooks/useWorkspace';
import { statusMeta } from '../lib/derive';
import { workspaceConfig } from '../lib/config';
import { isReservedMd } from '../lib/model';
import { scaffoldConfigYml, scaffoldFor } from '../lib/scaffold';
import { yamlSpans } from '../components/ConfigDoc';
import { Loading } from './Dashboard';
import { IconArrowLR } from '../components/icons';

const TYPE_COLOR: Record<string, string> = {
  enum: 'var(--reg)', links: 'var(--prod)', percent: 'var(--data)', code: 'var(--text-2)',
  user: 'var(--text-2)', tag: 'var(--ai)', date: 'var(--text-3)', text: 'var(--text-2)',
};

// the model axes — entities carry `group:`; families without one land in Other
const GROUPS = [
  { key: 'why', label: 'WHY', hint: 'where work comes from — regulation, product, technical' },
  { key: 'what', label: 'WHAT', hint: 'what the product must do' },
  { key: 'how', label: 'HOW', hint: 'how it is realized' },
  { key: 'when', label: 'WHEN', hint: 'when it lands — planned work items' },
  { key: '', label: 'OTHER', hint: 'families without a model axis — set group: in config.yml' },
];

export function ModelView() {
  const nav = useNav();
  const app = useApp();
  const qc = useQueryClient();
  const { ensureWritableBranch } = useWorkspace();
  const [showSample, setShowSample] = useState(false);
  if (!app.model) return <Loading />;
  const wcfg = workspaceConfig(app.configYml || '');
  const files = app.files || {};

  // the config file is optional — a workspace without it runs on the built-in
  // WHY/WHAT/HOW/WHEN defaults; creating it imports the full sample setup
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
  // stand-alone schema.json keeps working until a properties: section exists;
  // AppContext resolved what actually applies (a broken schema.json falls
  // back to defaults, so file presence alone must not claim the label)
  const legacySchema = app.schemaSource === 'legacy';
  const docsIn = (folder: string) => Object.keys(files).filter((p) => p.startsWith(folder) && !isReservedMd(p)).sort();
  // every configured document family renders a card, empty ones included —
  // the card is where users learn what the family is for
  const entities = app.entities.map((e) => {
    const docs = docsIn(e.folder);
    return { ...e, count: docs.length, first: docs[0] || '' };
  });
  // families with no group, or one naming no axis, land in OTHER (key '')
  const axisOf = (e: { group?: string }) => (e.group && GROUPS.some((g) => g.key === e.group) ? e.group : '');
  const groups = GROUPS.map((g) => ({
    ...g,
    entities: entities.filter((e) => axisOf(e) === g.key),
  })).filter((g) => g.entities.length);
  const schema = app.schema || { fields: {}, order: [] };
  const schemaFields = (schema.order || []).filter((k) => (schema.fields || {})[k]).map((k) => {
    const f = schema.fields![k];
    return { key: k, label: f.label || k, type: f.type || 'text', values: f.values ? Object.keys(f.values).join(' · ') : '' };
  });
  const sample = scaffoldConfigYml(app.repoId || 'my-project');

  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:1000px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx('display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap')}>
          <div>
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>.specquill/config.yml</div>
            <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Model definitions</h1>
            <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:5px;max-width:560px;line-height:1.5')}>
              The workspace model — WHY drives WHAT, HOW realizes it, WHEN delivers it. Every section of the config is optional; what it leaves out runs on built-in defaults.
              {missingCfg && <> This workspace has no config yet — it runs entirely on the <b>built-in defaults</b>; view the sample to see or import the full setup.</>}
            </div>
          </div>
          <div style={sx('display:flex;gap:8px')}>
            <button onClick={() => setShowSample(true)} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>sample config</button>
            <button onClick={() => void openOrCreate('.specquill/config.yml')} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>{missingCfg ? '+ create config.yml' : 'config.yml'}</button>
          </div>
        </div>

        {groups.map((g) => (
          <div key={g.key || 'other'}>
            <div style={sx('display:flex;align-items:baseline;gap:10px;margin-top:24px')}>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.5px")}>{g.label}</span>
              <span style={sx('font-size:11.5px;color:var(--text-2)')}>{g.hint}</span>
            </div>
            <div style={sx('display:grid;grid-template-columns:repeat(auto-fill,minmax(190px,1fr));gap:12px;margin-top:10px')}>
              {g.entities.map((e) => (
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
          </div>
        ))}

        <div style={sx('display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:24px;align-items:start')}>
          <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:16px 18px')}>
            <div style={sx('font-weight:700;font-size:13.5px')}>Drivers</div>
            <div style={sx('font-size:11.5px;color:var(--text-2);margin-top:2px')}>WHY — what a requirement can be driven by. Regulatory is one of several.</div>
            <div style={sx('display:flex;flex-wrap:wrap;gap:8px;margin-top:13px')}>
              {wcfg.drivers.map((d) => (
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
              {wcfg.statuses.map((s) => {
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

        <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;padding:16px 18px')}>
          <div style={sx('display:flex;align-items:baseline;gap:8px')}>
            <span style={sx('font-weight:700;font-size:13.5px')}>Timed dependencies</span>
            <span style={sx('font-size:11.5px;color:var(--text-2)')}>Which frontmatter keys carry a validity window — any document with one joins the Timed view</span>
          </div>
          <div style={sx('display:grid;grid-template-columns:150px 1fr;gap:6px 14px;margin-top:12px;font-size:12.5px;align-items:baseline')}>
            <span style={sx('color:var(--text-2)')}>Start keys</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{wcfg.timed.start.join(', ')}</span>
            <span style={sx('color:var(--text-2)')}>End keys</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{wcfg.timed.end.join(', ')}</span>
            <span style={sx('color:var(--text-2)')}>Ready statuses</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{wcfg.timed.readyStatuses.join(', ')}</span>
            <span style={sx('color:var(--text-2)')}>Horizon</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{wcfg.timed.horizonDays} days</span>
            <span style={sx('color:var(--text-2)')}>Families</span>
            <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px")}>{wcfg.timed.kinds.length ? wcfg.timed.kinds.join(', ') : 'all'}</span>
          </div>
        </div>

        <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;overflow:hidden')}>
          <div style={sx('padding:14px 18px;border-bottom:1px solid var(--border)')}>
            <span style={sx('font-weight:700;font-size:13.5px')}>Link types</span>
            <span style={sx('font-size:11.5px;color:var(--text-2);margin-left:8px')}>The typed edges of the graph — stored on the lower level pointing up (WHY ← WHAT ← HOW ← WHEN)</span>
          </div>
          {wcfg.linkTypes.map((l) => (
            <div key={l.name} style={sx('display:flex;align-items:center;gap:12px;padding:11px 18px;border-top:1px solid var(--border)')}>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:600;color:var(--prod);width:110px;flex:none")}>{l.name}</span>
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;padding:2px 8px;border-radius:5px;background:var(--surface-2);color:var(--text-2)")}>{l.from}</span>
              <IconArrowLR />
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;padding:2px 8px;border-radius:5px;background:var(--surface-2);color:var(--text-2)")}>{l.to}</span>
              {l.inverse && (
                <span title={`how the relation reads from the target side`} style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>⇄ {l.inverse}</span>
              )}
            </div>
          ))}
        </div>

        <div style={sx('background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;overflow:hidden')}>
          <div style={sx('padding:14px 18px;border-bottom:1px solid var(--border);display:flex;align-items:center')}>
            <span style={sx('font-weight:700;font-size:13.5px')}>Property schema</span>
            <span style={sx('font-size:11.5px;color:var(--text-2);margin-left:8px')}>
              Frontmatter attributes — types &amp; enum values drive the Properties panel
              {legacySchema ? ' · from .specquill/schema.json (legacy)' : app.schemaSource === 'config' ? ' · from config.yml properties:' : ' · built-in defaults'}
            </span>
            <div style={sx('flex:1')} />
            <span onClick={() => void openOrCreate(legacySchema ? '.specquill/schema.json' : '.specquill/config.yml')} style={sx('font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600')}>{legacySchema ? 'Edit schema.json →' : missingCfg ? 'Create config.yml →' : 'Edit config.yml →'}</span>
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

      {showSample && (
        <div onClick={() => setShowSample(false)} style={sx('position:fixed;inset:0;background:rgba(15,18,22,.45);display:flex;align-items:center;justify-content:center;z-index:60;padding:28px')}>
          <div onClick={(e) => e.stopPropagation()} style={sx('background:var(--surface);border:1px solid var(--border-2);border-radius:14px;box-shadow:var(--shadow);width:min(860px,100%);max-height:100%;display:flex;flex-direction:column;overflow:hidden')} data-testid="sample-config">
            <div style={sx('display:flex;align-items:center;gap:10px;padding:14px 18px;border-bottom:1px solid var(--border)')}>
              <div>
                <div style={sx('font-weight:700;font-size:14px')}>Sample workspace config</div>
                <div style={sx('font-size:11.5px;color:var(--text-2);margin-top:2px')}>The full default setup — WHY / WHAT / HOW / WHEN with linkage and attributes. Importing it as-is changes nothing; edit from there.</div>
              </div>
              <div style={sx('flex:1')} />
              {missingCfg && (
                <button data-testid="import-sample" onClick={() => { setShowSample(false); void openOrCreate('.specquill/config.yml'); }} style={sx('height:30px;padding:0 13px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;flex:none')}>Import as .specquill/config.yml</button>
              )}
              <button onClick={() => { void navigator.clipboard?.writeText(sample); }} style={sx('height:30px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;flex:none')}>Copy</button>
              <button onClick={() => setShowSample(false)} aria-label="Close" style={sx('height:30px;width:30px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:14px;cursor:pointer;flex:none')}>×</button>
            </div>
            <div style={sx('overflow:auto;padding:12px 0')}>
              {sample.replace(/\n$/, '').split('\n').map((line, i) => (
                <div key={i} style={sx('display:flex;line-height:1.6')}>
                  <span style={sx("font-family:'JetBrains Mono',monospace;flex:none;width:44px;text-align:right;padding-right:14px;font-size:11px;color:var(--text-3);user-select:none")}>{i + 1}</span>
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12px;white-space:pre")}>
                    {yamlSpans(line).map((s, j) => <span key={j} style={sx(s.style)}>{s.text}</span>)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
