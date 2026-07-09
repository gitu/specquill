import { useMemo, useState } from 'react';
import { sx } from '../lib/sx';
import { fmToJS, setFmValue } from '../lib/frontmatter';
import type { PropertySchema } from '../state/AppContext';

const PAL: Record<string, { fg: string; bg: string }> = {
  green: { fg: 'var(--data)', bg: 'var(--data-bg)' }, amber: { fg: 'var(--reg)', bg: 'var(--reg-bg)' },
  blue: { fg: 'var(--prod)', bg: 'var(--prod-bg)' }, violet: { fg: 'var(--ai)', bg: 'var(--ai-bg)' },
  slate: { fg: 'var(--text-2)', bg: 'var(--surface-2)' },
};

const INPUT = "height:26px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:11.5px;outline:none";

/**
 * Schema-driven frontmatter editor: each row edits one top-level key and
 * writes through setFmValue (comment/format-preserving). Complex values
 * (lists of maps like `drivers`) render read-only.
 */
export function PropertiesForm({ fm, schema, files, onChange, onOpenPath }: {
  fm: string;
  schema: PropertySchema | undefined;
  files: Record<string, string> | undefined;
  onChange: (nextFm: string) => void;
  onOpenPath: (path: string) => void;
}) {
  const values = useMemo(() => fmToJS(fm), [fm]);
  const order = schema?.order || [];
  const keys = [
    ...order.filter((k) => k in values),
    ...Object.keys(values).filter((k) => !order.includes(k)),
  ].filter((k) => k !== 'title');

  const set = (key: string, v: unknown) => onChange(setFmValue(fm, key, v));

  return (
    <>
      {keys.map((key) => {
        const def = schema?.fields?.[key] || {};
        const label = def.label || key.replace(/_/g, ' ');
        return (
          <div key={key} style={sx('display:flex;gap:14px;padding:7px 14px;border-top:1px solid var(--border);align-items:center')}>
            <span style={sx("width:132px;flex:none;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.3px")}>{label}</span>
            <div style={sx('flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0')}>
              <Field
                fieldKey={key}
                type={def.type || 'text'}
                enumValues={def.values}
                value={values[key]}
                files={files}
                onSet={(v) => set(key, v)}
                onOpenPath={onOpenPath}
              />
            </div>
          </div>
        );
      })}
    </>
  );
}

function Field({ fieldKey, type, enumValues, value, files, onSet, onOpenPath }: {
  fieldKey: string;
  type: string;
  enumValues?: Record<string, string>;
  value: unknown;
  files: Record<string, string> | undefined;
  onSet: (v: unknown) => void;
  onOpenPath: (path: string) => void;
}) {
  // complex structures (e.g. drivers: [{type, ref}]) stay read-only
  if (Array.isArray(value) && value.some((v) => v !== null && typeof v === 'object')) {
    return (
      <>
        {value.map((v, i) => (
          <span key={i} style={sx("display:inline-flex;align-items:center;padding:2px 9px;border-radius:6px;font-size:11.5px;font-family:'JetBrains Mono',monospace;background:var(--surface-2);color:var(--text-2)")}>
            {Object.values(v as Record<string, unknown>).join(' · ')}
          </span>
        ))}
        <span style={sx('font-size:10px;color:var(--text-3)')}>edit in Source</span>
      </>
    );
  }

  if (Array.isArray(value)) {
    return <ListField fieldKey={fieldKey} type={type} items={value.map(String)} files={files} onSet={onSet} onOpenPath={onOpenPath} />;
  }

  if (type === 'enum') {
    const current = String(value ?? '');
    const color = PAL[enumValues?.[current.toLowerCase()] || 'slate'] || PAL.slate;
    return (
      <select
        value={current}
        onChange={(e) => onSet(e.target.value)}
        style={{ ...sx(INPUT), background: color.bg, color: color.fg, fontWeight: 600, border: '1px solid transparent', borderRadius: 20, textTransform: 'capitalize' }}
      >
        {!(current.toLowerCase() in (enumValues || {})) && <option value={current}>{current}</option>}
        {Object.keys(enumValues || {}).map((v) => <option key={v} value={v}>{v.replace(/_/g, ' ')}</option>)}
      </select>
    );
  }
  if (type === 'percent') {
    const n = typeof value === 'number' ? value : parseFloat(String(value)) || 0;
    const pct = Math.round(n <= 1 ? n * 100 : n);
    const c = pct > 80 ? 'var(--data)' : pct > 60 ? 'var(--prod)' : 'var(--reg)';
    return (
      <span style={sx('display:inline-flex;align-items:center;gap:4px')}>
        <input
          type="number" min={0} max={100} defaultValue={pct}
          onBlur={(e) => { const v = Math.max(0, Math.min(100, Number(e.target.value))); if (v !== pct) onSet(Math.round(v) / 100); }}
          style={{ ...sx(INPUT), width: 64, color: c, fontWeight: 600 }}
        />
        <span style={sx('font-size:11px;color:var(--text-3)')}>%</span>
      </span>
    );
  }
  if (type === 'text' && String(value ?? '').length > 60) {
    return (
      <textarea
        defaultValue={String(value ?? '')}
        rows={2}
        onBlur={(e) => { if (e.target.value !== String(value ?? '')) onSet(e.target.value); }}
        style={sx('flex:1;min-width:260px;padding:6px 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;line-height:1.5;resize:vertical;outline:none')}
      />
    );
  }
  return (
    <input
      defaultValue={String(value ?? '')}
      onBlur={(e) => { if (e.target.value !== String(value ?? '')) onSet(e.target.value); }}
      style={{ ...sx(INPUT), minWidth: 180 }}
    />
  );
}

function ListField({ fieldKey, type, items, files, onSet, onOpenPath }: {
  fieldKey: string;
  type: string;
  items: string[];
  files: Record<string, string> | undefined;
  onSet: (v: unknown) => void;
  onOpenPath: (path: string) => void;
}) {
  const [adding, setAdding] = useState('');
  const isLink = (t: string) => /([\w-]+\/[\w.\/-]+\.(?:md|excalidraw|mermaid))/.test(t);
  const listId = 'paths-' + fieldKey;
  return (
    <>
      {items.map((it, i) => (
        <span key={i} style={sx("display:inline-flex;align-items:center;gap:5px;padding:2px 9px;border-radius:6px;font-size:11.5px;font-family:'JetBrains Mono',monospace;background:var(--surface-2);color:" + (isLink(it) ? 'var(--prod)' : 'var(--text-2)'))}>
          <span
            onClick={isLink(it) ? () => onOpenPath(it.split('#')[0]) : undefined}
            style={isLink(it) ? { cursor: 'pointer', textDecoration: 'underline', textDecorationColor: 'var(--prod-line)' } : undefined}
          >
            {it}
          </span>
          <span
            title="remove"
            onClick={() => onSet(items.filter((_, j) => j !== i))}
            style={sx('cursor:pointer;color:var(--text-3);font-size:12px;line-height:1')}
          >
            ×
          </span>
        </span>
      ))}
      <input
        placeholder="+ add"
        value={adding}
        list={type === 'links' ? listId : undefined}
        onChange={(e) => setAdding(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && adding.trim()) {
            onSet([...items, adding.trim()]);
            setAdding('');
          }
        }}
        style={{ ...sx(INPUT), width: 130, borderStyle: 'dashed' }}
      />
      {type === 'links' && (
        <datalist id={listId}>
          {Object.keys(files || {}).map((p) => <option key={p} value={p} />)}
        </datalist>
      )}
    </>
  );
}
