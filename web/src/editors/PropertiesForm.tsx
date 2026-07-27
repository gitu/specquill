import { useMemo, useState } from 'react';
import { sx } from '../lib/sx';
import { fmToJS, setFmValue } from '../lib/frontmatter';
import { PAL, collectFieldValues, parseTaxonomy } from '../lib/derive';
import { useApp } from '../state/AppContext';
import type { PropertySchema } from '../state/AppContext';

type FieldDef = { label?: string; type?: string; values?: Record<string, string> };

const INPUT = "height:26px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:11.5px;outline:none";

const LABEL = "width:132px;flex:none;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.3px";

// YAML-safe top-level key (same shape parseProps recognizes)
const KEY_RE = /^[A-Za-z_][\w-]*$/;

const enterBlurs = (e: React.KeyboardEvent<HTMLInputElement>) => {
  if (e.key === 'Enter') e.currentTarget.blur();
};

/**
 * Schema-driven frontmatter editor: each row edits one top-level key and
 * writes through setFmValue (comment/format-preserving). Categorical fields
 * (schema `values` map, config statuses, or values already used across the
 * workspace) render as comboboxes with free entry; the trailing row adds a
 * known or custom property. Complex values (lists of maps like `drivers`)
 * render read-only.
 */
export function PropertiesForm({ fm, schema, files, onChange, onOpenPath }: {
  fm: string;
  schema: PropertySchema | undefined;
  files: Record<string, string> | undefined;
  onChange: (nextFm: string) => void;
  onOpenPath: (path: string) => void;
}) {
  const app = useApp();
  const values = useMemo(() => fmToJS(fm), [fm]);
  const corpus = useMemo(() => collectFieldValues(files || {}), [files]);
  const statuses = useMemo(() => parseTaxonomy(app.configYml || '').statuses, [app.configYml]);
  const order = schema?.order || [];
  const keys = [
    ...order.filter((k) => k in values),
    ...Object.keys(values).filter((k) => !order.includes(k)),
  ].filter((k) => k !== 'title');

  const set = (key: string, v: unknown) => onChange(setFmValue(fm, key, v));

  // combobox options: schema values first (statuses as the status fallback),
  // then whatever the rest of the workspace already uses for this key
  const optionsFor = (key: string, def: FieldDef): string[] => {
    const base = def.values ? Object.keys(def.values) : key === 'status' ? statuses : [];
    const extra = (corpus[key] || []).filter((v) => !base.some((b) => b.toLowerCase() === v.toLowerCase()));
    return [...base, ...extra];
  };

  return (
    <>
      {keys.map((key) => {
        const def: FieldDef = schema?.fields?.[key] || {};
        const label = def.label || key.replace(/_/g, ' ');
        return (
          <div key={key} style={sx('display:flex;gap:14px;padding:7px 14px;border-top:1px solid var(--border);align-items:center')}>
            <span style={sx(LABEL)}>{label}</span>
            <div style={sx('flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0')}>
              <Field
                fieldKey={key}
                type={def.type || 'text'}
                enumValues={def.values}
                options={optionsFor(key, def)}
                value={values[key]}
                files={files}
                onSet={(v) => set(key, v)}
                onOpenPath={onOpenPath}
              />
            </div>
            <button
              type="button"
              title={'remove ' + key}
              aria-label={'remove ' + key}
              onClick={() => set(key, undefined)}
              style={sx('flex:none;cursor:pointer;color:var(--text-3);font-size:13px;line-height:1;padding:0 2px;border:none;background:none;font-family:inherit')}
            >
              ×
            </button>
          </div>
        );
      })}
      <AddPropertyRow
        schema={schema}
        presentKeys={Object.keys(values)}
        corpusKeys={Object.keys(corpus)}
        optionsFor={optionsFor}
        onAdd={set}
      />
    </>
  );
}

function Field({ fieldKey, type, enumValues, options, value, files, onSet, onOpenPath }: {
  fieldKey: string;
  type: string;
  enumValues?: Record<string, string>;
  options: string[];
  value: unknown;
  files: Record<string, string> | undefined;
  onSet: (v: unknown) => void;
  onOpenPath: (path: string) => void;
}) {
  // drivers ([{type, ref}]) get a dedicated editor
  if (fieldKey === 'drivers' && Array.isArray(value) && value.every((v) => v !== null && typeof v === 'object')) {
    return (
      <DriversField
        items={value as { type?: string; ref?: string }[]}
        files={files}
        onSet={onSet}
        onOpenPath={onOpenPath}
      />
    );
  }

  // other complex structures stay read-only
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

  const current = String(value ?? '');
  const commit = (el: HTMLInputElement) => { if (el.value !== current) onSet(el.value); };

  // categorical (schema values map): colored-pill combobox, free entry allowed
  if (enumValues) {
    const color = PAL[enumValues[current.toLowerCase()] || 'slate'] || PAL.slate;
    const listId = 'fmopt-' + fieldKey;
    return (
      <>
        <input
          key={current}
          defaultValue={current}
          list={listId}
          onBlur={(e) => commit(e.target)}
          onKeyDown={enterBlurs}
          style={{ ...sx(INPUT), width: Math.max(110, current.length * 8 + 36), background: color.bg, color: color.fg, fontWeight: 600, border: '1px solid transparent', borderRadius: 20, textTransform: 'capitalize' }}
        />
        <datalist id={listId}>
          {options.map((v) => <option key={v} value={v} />)}
        </datalist>
      </>
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
  if (type === 'text' && current.length > 60) {
    return (
      <textarea
        defaultValue={current}
        rows={2}
        onBlur={(e) => { if (e.target.value !== current) onSet(e.target.value); }}
        style={sx('flex:1;min-width:260px;padding:6px 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;line-height:1.5;resize:vertical;outline:none')}
      />
    );
  }
  // plain scalar — categorical in practice when the workspace already uses
  // values for this key (owner, type, jurisdiction, …): same input, plus
  // datalist suggestions
  const listId = options.length ? 'fmopt-' + fieldKey : undefined;
  return (
    <>
      <input
        key={current}
        defaultValue={current}
        list={listId}
        onBlur={(e) => commit(e.target)}
        onKeyDown={enterBlurs}
        style={{ ...sx(INPUT), minWidth: 180 }}
      />
      {listId && (
        <datalist id={listId}>
          {options.map((v) => <option key={v} value={v} />)}
        </datalist>
      )}
    </>
  );
}

// AddPropertyRow appends a frontmatter key: pick a known key (schema ∪ corpus)
// or type a custom one, then provide the value — nothing is written until a
// non-empty value is committed, so cancel paths never touch the document.
function AddPropertyRow({ schema, presentKeys, corpusKeys, optionsFor, onAdd }: {
  schema: PropertySchema | undefined;
  presentKeys: string[];
  corpusKeys: string[];
  optionsFor: (key: string, def: FieldDef) => string[];
  onAdd: (key: string, value: unknown) => void;
}) {
  const [stage, setStage] = useState<'idle' | 'key' | 'value'>('idle');
  const [keyDraft, setKeyDraft] = useState('');
  const [key, setKey] = useState('');
  const [val, setVal] = useState('');

  const knownKeys = [...new Set([...(schema?.order || []), ...Object.keys(schema?.fields || {}), ...corpusKeys])]
    .filter((k) => k !== 'title' && !presentKeys.includes(k)).sort();
  const keyOk = (k: string) => KEY_RE.test(k) && !presentKeys.includes(k);
  const def: FieldDef = schema?.fields?.[key] || {};

  const reset = () => { setStage('idle'); setKeyDraft(''); setKey(''); setVal(''); };
  const commitKey = () => {
    const k = keyDraft.trim();
    if (keyOk(k)) { setKey(k); setStage('value'); }
  };
  const commitValue = () => {
    const t = val.trim();
    if (!t) { reset(); return; }
    let v: unknown = t;
    if (def.type === 'percent') { const n = Math.max(0, Math.min(100, parseFloat(t) || 0)); v = Math.round(n <= 1 ? n * 100 : n) / 100; }
    else if (def.type === 'links' || def.type === 'anchors') v = [t];
    onAdd(key, v);
    reset();
  };

  if (stage === 'idle') {
    return (
      <div style={sx('padding:7px 14px;border-top:1px solid var(--border)')}>
        <button onClick={() => setStage('key')} style={{ ...sx(INPUT), borderStyle: 'dashed', cursor: 'pointer' }}>
          + add property
        </button>
      </div>
    );
  }

  if (stage === 'key') {
    const draftBad = keyDraft.trim() !== '' && !keyOk(keyDraft.trim());
    return (
      <div style={sx('display:flex;gap:14px;padding:7px 14px;border-top:1px solid var(--border);align-items:center')}>
        <span style={sx(LABEL)}>new property</span>
        <input
          autoFocus
          placeholder="property name ⏎"
          value={keyDraft}
          list="fmkey-new"
          onChange={(e) => setKeyDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') commitKey();
            if (e.key === 'Escape') reset();
          }}
          onBlur={reset}
          style={{ ...sx(INPUT), minWidth: 180, borderColor: draftBad ? 'var(--reg)' : undefined }}
        />
        <datalist id="fmkey-new">
          {knownKeys.map((k) => <option key={k} value={k} />)}
        </datalist>
      </div>
    );
  }

  const color = def.values ? PAL[def.values[val.trim().toLowerCase()] || 'slate'] || PAL.slate : undefined;
  return (
    <div style={sx('display:flex;gap:14px;padding:7px 14px;border-top:1px solid var(--border);align-items:center')}>
      <span style={sx(LABEL)}>{def.label || key.replace(/_/g, ' ')}</span>
      <input
        autoFocus
        placeholder="value ⏎"
        value={val}
        list="fmval-new"
        onChange={(e) => setVal(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commitValue();
          if (e.key === 'Escape') reset();
        }}
        onBlur={commitValue}
        style={color
          ? { ...sx(INPUT), minWidth: 140, background: color.bg, color: color.fg, fontWeight: 600, border: '1px solid transparent', borderRadius: 20 }
          : { ...sx(INPUT), minWidth: 180 }}
      />
      <datalist id="fmval-new">
        {optionsFor(key, def).map((v) => <option key={v} value={v} />)}
      </datalist>
    </div>
  );
}

// DriversField edits `drivers: [{type, ref}]` in place: the type comes from
// the workspace driver taxonomy, the ref is free text or a document path.
function DriversField({ items, files, onSet, onOpenPath }: {
  items: { type?: string; ref?: string }[];
  files: Record<string, string> | undefined;
  onSet: (v: unknown) => void;
  onOpenPath: (path: string) => void;
}) {
  const app = useApp();
  const types = parseTaxonomy(app.configYml || '').drivers;
  const effTypes = types.length ? types : [
    { key: 'regulatory', label: 'Regulatory', icon: '⚖', color: 'var(--reg)' },
    { key: 'product', label: 'Product', icon: '◆', color: 'var(--prod)' },
    { key: 'technical', label: 'Technical', icon: '⚙', color: 'var(--text-2)' },
  ];
  const isLink = (t: string) => /([\w-]+\/[\w.\/-]+\.md)/.test(t);
  const update = (i: number, patch: { type?: string; ref?: string }) =>
    onSet(items.map((d, j) => (j === i ? { ...d, ...patch } : d)));
  return (
    <>
      {items.map((d, i) => {
        const meta = effTypes.find((t) => t.key === d.type);
        const ref = String(d.ref ?? '');
        return (
          <span key={i + ':' + (d.type || '') + ':' + ref}
            style={sx('display:inline-flex;align-items:center;gap:5px;padding:3px 6px;border:1px solid var(--border);border-left:3px solid ' + (meta?.color || 'var(--border-2)') + ';border-radius:7px;background:var(--surface-2)')}>
            <select
              value={d.type || ''}
              onChange={(e) => update(i, { type: e.target.value })}
              style={{ ...sx(INPUT), height: 22, padding: '0 4px', fontWeight: 600, color: meta?.color || 'var(--text-2)', border: 'none', background: 'transparent' }}
            >
              {!effTypes.some((t) => t.key === d.type) && <option value={d.type || ''}>{d.type || '?'}</option>}
              {effTypes.map((t) => <option key={t.key} value={t.key}>{t.icon} {t.label}</option>)}
            </select>
            <input
              defaultValue={ref}
              placeholder="doc path or free text"
              list="paths-drivers"
              onBlur={(e) => { if (e.target.value !== ref) update(i, { ref: e.target.value }); }}
              style={{ ...sx(INPUT), height: 22, width: Math.max(180, ref.length * 6.6), border: 'none', background: 'transparent', color: isLink(ref) ? 'var(--prod)' : 'var(--text)' }}
            />
            {isLink(ref) && (
              <span title={'open ' + ref.split('#')[0]} onClick={() => onOpenPath(ref.split('#')[0])}
                style={sx('cursor:pointer;color:var(--prod);font-size:11px')}>↗</span>
            )}
            <span title="remove driver" onClick={() => onSet(items.filter((_, j) => j !== i))}
              style={sx('cursor:pointer;color:var(--text-3);font-size:12px;line-height:1')}>×</span>
          </span>
        );
      })}
      <button
        onClick={() => onSet([...items, { type: effTypes[0].key, ref: '' }])}
        style={{ ...sx(INPUT), borderStyle: 'dashed', cursor: 'pointer' }}
      >
        + add driver
      </button>
      <datalist id="paths-drivers">
        {Object.keys(files || {}).filter((p) => p.endsWith('.md')).map((p) => <option key={p} value={p} />)}
      </datalist>
    </>
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
