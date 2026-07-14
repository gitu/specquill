import type { ReactNode } from 'react';
import { sx } from '../lib/sx';
import { parseEntities } from '../lib/entities';
import { parseTaxonomy } from '../lib/derive';
import { idSchemes } from '../lib/ids';

// ConfigDoc — the view-mode renderer for workspace config files. The two
// .specquill/ files get a structured summary of what they configure; every
// yaml/json file gets syntax-highlighted source instead of a bare <pre>.

const MONO = "font-family:'JetBrains Mono',monospace;";

interface Span { text: string; style: string }

// line-based highlighting in the design's string-style idiom — comments,
// keys, and quoted strings are enough color for config-sized files
function yamlSpans(line: string): Span[] {
  if (/^\s*#/.test(line)) return [{ text: line, style: 'color:var(--text-3)' }];
  const m = line.match(/^(\s*(?:- )?)([\w.-]+):(.*)$/);
  const value = (v: string): Span[] =>
    v.split(/("(?:[^"\\]|\\.)*")/).filter((s) => s !== '').map((s) => ({
      text: s,
      style: s.startsWith('"') ? 'color:var(--data)' : /^\s*(\d+|true|false)\s*$/.test(s) ? 'color:var(--reg)' : 'color:var(--text-2)',
    }));
  if (!m) return value(line);
  return [
    { text: m[1], style: '' },
    { text: m[2] + ':', style: 'color:var(--prod)' },
    ...value(m[3]),
  ];
}

function jsonSpans(line: string): Span[] {
  return line.split(/("(?:[^"\\]|\\.)*"\s*:|"(?:[^"\\]|\\.)*")/).filter((s) => s !== '').map((s) => ({
    text: s,
    style: /^".*"\s*:$/.test(s) ? 'color:var(--prod)'
      : s.startsWith('"') ? 'color:var(--data)'
      : /\d|true|false/.test(s) ? 'color:var(--reg)'
      : 'color:var(--text-3)',
  }));
}

function Section({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <div style={sx('margin-top:14px')}>
      <div style={sx('display:flex;align-items:baseline;gap:8px')}>
        <span style={sx(MONO + 'font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.5px')}>{title}</span>
        {hint && <span style={sx('font-size:11px;color:var(--text-3)')}>{hint}</span>}
      </div>
      <div style={sx('display:flex;flex-wrap:wrap;gap:6px;margin-top:7px')}>{children}</div>
    </div>
  );
}

const chip = (extra = '') =>
  sx('display:inline-flex;align-items:center;gap:6px;padding:4px 11px;border:1px solid var(--border);border-radius:8px;background:var(--surface);font-size:12px;font-weight:600;' + extra);

function ConfigSummary({ raw }: { raw: string }) {
  const tax = parseTaxonomy(raw);
  const entities = parseEntities(raw).filter((e) => !e.builtin);
  const schemes = idSchemes(raw);
  const view = (raw.match(/^\s*default_view:\s*([\w-]+)/m) || [])[1];
  const refs = [...raw.matchAll(/-\s*source:\s*([\w-]+)/g)].map((m) => m[1]);
  return (
    <div style={sx('border:1px solid var(--border);border-radius:12px;background:var(--surface);box-shadow:var(--shadow);padding:16px 18px 18px;margin-bottom:22px')}>
      <div style={sx('font-weight:700;font-size:13.5px')}>What this file configures</div>
      {view && (
        <Section title="Default view">
          <span style={chip()}>{view}</span>
        </Section>
      )}
      {tax.statuses.length > 0 && (
        <Section title="Statuses" hint="document lifecycle states">
          {tax.statuses.map((s) => <span key={s} style={chip('text-transform:capitalize')}>{s.replace(/_/g, ' ')}</span>)}
        </Section>
      )}
      {schemes.length > 0 && (
        <Section title="ID schemes" hint="how new document IDs are generated">
          {schemes.map((s) => (
            <span key={s.kind} style={chip()}>
              <span style={sx('text-transform:capitalize;color:var(--text-2)')}>{s.kind.replace(/_/g, ' ')}</span>
              <span style={sx(MONO + 'font-size:11px;color:var(--prod)')}>{s.pattern}</span>
            </span>
          ))}
        </Section>
      )}
      {tax.drivers.length > 0 && (
        <Section title="Drivers" hint="what a requirement can be driven by">
          {tax.drivers.map((d) => (
            <span key={d.key} style={{ ...chip(), borderLeft: '3px solid ' + d.color }}>
              <span style={{ color: d.color }}>{d.icon}</span>{d.label}
            </span>
          ))}
        </Section>
      )}
      {entities.length > 0 && (
        <Section title="Custom entities" hint="document families beyond the built-ins">
          {entities.map((e) => (
            <span key={e.kind} style={chip()} title={e.description}>
              <span style={{ color: e.color }}>{e.icon}</span>{e.label}
              <span style={sx(MONO + 'font-size:10px;color:var(--text-3)')}>{e.folder}</span>
            </span>
          ))}
        </Section>
      )}
      <Section title="References" hint="read-only sources this project selects">
        {refs.length
          ? refs.map((r) => <span key={r} style={chip(MONO + 'font-size:11px')}>~{r}</span>)
          : <span style={sx('font-size:12px;color:var(--text-3)')}>none — this workspace is self-contained</span>}
      </Section>
    </div>
  );
}

function SchemaSummary({ raw }: { raw: string }) {
  let schema: { order?: string[]; fields?: Record<string, { label?: string; type?: string; values?: Record<string, string> }> };
  try { schema = JSON.parse(raw); } catch { return null; }
  const keys = (schema.order || Object.keys(schema.fields || {})).filter((k) => (schema.fields || {})[k]);
  if (!keys.length) return null;
  const TYPE_COLOR: Record<string, string> = {
    enum: 'var(--reg)', links: 'var(--prod)', percent: 'var(--data)', code: 'var(--text-2)',
    user: 'var(--text-2)', tag: 'var(--ai)', date: 'var(--text-3)', text: 'var(--text-2)',
  };
  return (
    <div style={sx('border:1px solid var(--border);border-radius:12px;background:var(--surface);box-shadow:var(--shadow);overflow:hidden;margin-bottom:22px')}>
      <div style={sx('padding:13px 18px;border-bottom:1px solid var(--border);font-weight:700;font-size:13.5px')}>
        Property schema
        <span style={sx('font-size:11.5px;font-weight:400;color:var(--text-2);margin-left:8px')}>drives the Properties panel on every document</span>
      </div>
      <div style={sx('display:grid;grid-template-columns:160px 90px 1fr;padding:8px 18px;background:var(--surface-2);border-bottom:1px solid var(--border);' + MONO + 'font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px')}>
        <span>Field</span><span>Type</span><span>Enum values</span>
      </div>
      {keys.map((k) => {
        const f = schema.fields![k];
        return (
          <div key={k} style={sx('display:grid;grid-template-columns:160px 90px 1fr;align-items:center;padding:8px 18px;border-top:1px solid var(--border)')}>
            <span style={sx('font-size:12.5px;font-weight:500')}>{f.label || k}</span>
            <span>
              <span style={sx(MONO + `font-size:10.5px;padding:1px 7px;border-radius:5px;background:var(--surface-2);color:${TYPE_COLOR[f.type || 'text'] || 'var(--text-2)'}`)}>{f.type || 'text'}</span>
            </span>
            <span style={sx(MONO + 'font-size:11px;color:var(--text-2)')}>{f.values ? Object.keys(f.values).join(' · ') : ''}</span>
          </div>
        );
      })}
    </div>
  );
}

export function ConfigDoc({ path, raw }: { path: string; raw: string }) {
  const isJson = path.endsWith('.json');
  const spansOf = isJson ? jsonSpans : yamlSpans;
  return (
    <div>
      {path.endsWith('.specquill/config.yml') && <ConfigSummary raw={raw} />}
      {path.endsWith('.specquill/schema.json') && <SchemaSummary raw={raw} />}
      <div style={sx('border:1px solid var(--border);border-radius:12px;background:var(--surface);box-shadow:var(--shadow);overflow:hidden')}>
        <div style={sx('display:flex;align-items:center;padding:9px 14px;border-bottom:1px solid var(--border);background:var(--surface-2)')}>
          <span style={sx(MONO + 'font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px')}>Source</span>
          <span style={sx(MONO + 'font-size:10.5px;color:var(--text-3);margin-left:8px')}>{path.split('/').pop()}</span>
        </div>
        <div style={sx('overflow-x:auto;padding:12px 0')}>
          {raw.replace(/\n$/, '').split('\n').map((line, i) => (
            <div key={i} style={sx('display:flex;line-height:1.65')}>
              <span style={sx(MONO + 'flex:none;width:44px;text-align:right;padding-right:14px;font-size:11px;color:var(--text-3);user-select:none')}>{i + 1}</span>
              <span style={sx(MONO + 'font-size:12px;white-space:pre')}>
                {spansOf(line).map((s, j) => <span key={j} style={sx(s.style)}>{s.text}</span>)}
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
