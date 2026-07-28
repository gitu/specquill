import { useMemo, useRef, useState } from 'react';
import { sx } from '../lib/sx';
import { fmToJS, setFmValue } from '../lib/frontmatter';
import { PAL, collectFieldValues, collectRefTargets, daysAgo, docAnchorOptions, parseTaxonomy } from '../lib/derive';
import type { RefTarget } from '../lib/derive';
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

// strict YYYY-MM-DD backed by a real calendar date
const isValidDateStr = (s: string): boolean => {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
  if (!m) return false;
  const [y, mo, d] = [+m[1], +m[2], +m[3]];
  const dt = new Date(y, mo - 1, d);
  return dt.getFullYear() === y && dt.getMonth() === mo - 1 && dt.getDate() === d;
};

/**
 * Schema-driven frontmatter editor: each row edits one top-level key and
 * writes through setFmValue (comment/format-preserving). Categorical fields
 * (schema `values` map, config statuses, or values already used across the
 * workspace) render as comboboxes with free entry; date fields are read-only
 * with an explicit override control (valid YYYY-MM-DD only); the trailing row
 * adds a known or custom property. Complex values (lists of maps like
 * `drivers`) render read-only.
 */
export function PropertiesForm({ fm, body, schema, files, onChange, onOpenPath }: {
  fm: string;
  body?: string; // current editor body — anchor options come from its headings
  schema: PropertySchema | undefined;
  files: Record<string, string> | undefined;
  onChange: (nextFm: string) => void;
  onOpenPath: (path: string) => void;
}) {
  const app = useApp();
  const values = useMemo(() => fmToJS(fm), [fm]);
  const corpus = useMemo(() => collectFieldValues(files || {}), [files]);
  const statuses = useMemo(() => parseTaxonomy(app.configYml || '').statuses, [app.configYml]);
  const refTargets = useMemo(() => collectRefTargets(files || {}, app.model?.fields || []), [files, app.model]);
  const anchorOpts = useMemo(() => docAnchorOptions(body || ''), [body]);
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
                type={def.type || (key === 'updated' ? 'date' : 'text')}
                enumValues={def.values}
                options={optionsFor(key, def)}
                value={values[key]}
                refTargets={refTargets}
                anchorOpts={anchorOpts}
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
        refTargets={refTargets}
        anchorOpts={anchorOpts}
        onAdd={set}
      />
    </>
  );
}

/**
 * Combobox: free-entry input plus a real options popup. Native datalists
 * filter their suggestions against the input's current text, so a pre-filled
 * field ("draft") offers nothing when clicked — this popup always lists every
 * option on focus and only narrows once the user actually types. Controlled:
 * the caller owns the text; commits fire on blur, Enter, or an option pick.
 */
function Combobox({ text, onText, options, colorOf, style, placeholder, autoFocus, ariaLabel, onCommit, onEscape }: {
  text: string;
  onText: (t: string) => void;
  options: (string | RefTarget)[];
  colorOf?: (v: string) => { fg: string; bg: string };
  style: React.CSSProperties;
  placeholder?: string;
  autoFocus?: boolean;
  ariaLabel: string;
  onCommit: (v: string, via: 'blur' | 'enter' | 'pick') => void;
  onEscape?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState(false);
  const [hi, setHi] = useState(-1);
  const skipBlur = useRef(false);
  const norm: RefTarget[] = options.map((o) => (typeof o === 'string' ? { value: o } : o));
  // every whitespace-separated token must match somewhere in value or hint,
  // so "gdpr erasure" finds regulations/gdpr.md#art-17-erasure
  const toks = text.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const shown = typed && toks.length
    ? norm.filter((o) => {
        const hay = (o.value + ' ' + (o.hint || '')).toLowerCase();
        return toks.every((t) => hay.includes(t));
      })
    : norm;
  const listId = 'fmlist-' + ariaLabel.replace(/\W+/g, '-');

  const settle = () => { skipBlur.current = true; setOpen(false); setHi(-1); setTyped(false); };
  const pick = (o: string) => { settle(); onText(o); onCommit(o, 'pick'); };

  return (
    <span style={{ position: 'relative', display: 'inline-flex' }}>
      <input
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-label={ariaLabel}
        value={text}
        placeholder={placeholder}
        autoFocus={autoFocus}
        onChange={(e) => { onText(e.target.value); setTyped(true); setOpen(true); setHi(-1); }}
        onFocus={() => { setTyped(false); setOpen(true); }}
        onClick={() => setOpen(true)}
        onBlur={() => {
          if (skipBlur.current) { skipBlur.current = false; return; }
          setOpen(false); setHi(-1);
          onCommit(text, 'blur');
        }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && shown.length) { e.preventDefault(); setOpen(true); setHi((h) => (h + 1) % shown.length); }
          else if (e.key === 'ArrowUp' && shown.length) { e.preventDefault(); setOpen(true); setHi((h) => (h <= 0 ? shown.length - 1 : h - 1)); }
          else if (e.key === 'Enter') {
            const el = e.currentTarget;
            if (open && hi >= 0 && shown[hi] !== undefined) pick(shown[hi].value);
            else { settle(); onCommit(text, 'enter'); }
            el.blur();
          } else if (e.key === 'Escape') {
            const el = e.currentTarget;
            settle();
            onEscape?.();
            el.blur();
          }
        }}
        style={style}
      />
      {open && shown.length > 0 && (
        <div id={listId} role="listbox" style={sx('position:absolute;top:calc(100% + 4px);left:0;z-index:40;min-width:150px;max-width:520px;max-height:240px;overflow:auto;background:var(--surface);border:1px solid var(--border-2);border-radius:8px;box-shadow:var(--shadow);padding:4px')}>
          {shown.map((o, i) => (
            <div
              key={o.value}
              role="option"
              aria-selected={o.value === text}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => pick(o.value)}
              onMouseEnter={() => setHi(i)}
              style={{
                ...sx("display:flex;align-items:center;gap:7px;padding:4px 9px;border-radius:5px;font-family:'JetBrains Mono',monospace;font-size:11.5px;white-space:nowrap;cursor:pointer;color:var(--text)"),
                background: i === hi ? 'var(--surface-2)' : 'transparent',
                fontWeight: o.value === text ? 600 : 400,
              }}
            >
              {colorOf && <span style={{ width: 8, height: 8, borderRadius: 4, flex: 'none', background: colorOf(o.value).fg }} />}
              <span style={sx('flex:none')}>{o.value}</span>
              {o.hint && <span style={sx('font-size:10.5px;color:var(--text-3);overflow:hidden;text-overflow:ellipsis;max-width:240px')}>{o.hint}</span>}
            </div>
          ))}
        </div>
      )}
    </span>
  );
}

// ComboInput adapts the controlled Combobox to a Field row: local draft text,
// commit only on change, Escape reverts. Remounted via key={current} when the
// document value changes underneath.
function ComboInput({ fieldKey, current, options, pill, enumValues, onSet }: {
  fieldKey: string;
  current: string;
  options: string[];
  pill?: boolean;
  enumValues?: Record<string, string>;
  onSet: (v: unknown) => void;
}) {
  const [text, setText] = useState(current);
  const colorOf = enumValues ? (v: string) => PAL[enumValues[v.trim().toLowerCase()] || 'slate'] || PAL.slate : undefined;
  const style = pill && colorOf
    ? { ...sx(INPUT), width: Math.max(110, text.length * 8 + 36), background: colorOf(text).bg, color: colorOf(text).fg, fontWeight: 600, border: '1px solid transparent', borderRadius: 20, textTransform: 'capitalize' as const }
    : { ...sx(INPUT), minWidth: 180 };
  return (
    <Combobox
      text={text}
      onText={setText}
      options={options}
      colorOf={colorOf}
      ariaLabel={fieldKey}
      style={style}
      onCommit={(v) => { if (v !== current) onSet(v); }}
      onEscape={() => setText(current)}
    />
  );
}

// DateField renders read-only by default — dates like `updated` are
// maintenance metadata, not something to retype casually. The override
// control opens a draft input that only ever commits a valid YYYY-MM-DD.
function DateField({ fieldKey, value, onSet }: {
  fieldKey: string;
  value: string;
  onSet: (v: unknown) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  if (!editing) {
    return (
      <>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-2)")}>{value || '—'}</span>
        {value !== '' && <span style={sx('font-size:10.5px;color:var(--text-3)')}>({daysAgo(value)})</span>}
        <button
          type="button"
          title={'override ' + fieldKey}
          aria-label={'override ' + fieldKey}
          onClick={() => { setDraft(value); setEditing(true); }}
          style={sx('flex:none;cursor:pointer;color:var(--text-3);font-size:11px;line-height:1;padding:0 2px;border:none;background:none;font-family:inherit')}
        >
          ✎
        </button>
      </>
    );
  }
  const t = draft.trim();
  const ok = isValidDateStr(t);
  return (
    <input
      autoFocus
      value={draft}
      placeholder="YYYY-MM-DD"
      aria-label={fieldKey}
      onChange={(e) => setDraft(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && ok) { if (t !== value) onSet(t); setEditing(false); }
        if (e.key === 'Escape') setEditing(false);
      }}
      onBlur={() => { if (ok && t !== value) onSet(t); setEditing(false); }}
      style={{ ...sx(INPUT), width: 120, borderColor: ok ? undefined : 'var(--reg)' }}
    />
  );
}

function Field({ fieldKey, type, enumValues, options, value, refTargets, anchorOpts, onSet, onOpenPath }: {
  fieldKey: string;
  type: string;
  enumValues?: Record<string, string>;
  options: string[];
  value: unknown;
  refTargets: RefTarget[];
  anchorOpts: RefTarget[];
  onSet: (v: unknown) => void;
  onOpenPath: (path: string) => void;
}) {
  // drivers ([{type, ref}]) get a dedicated editor
  if (fieldKey === 'drivers' && Array.isArray(value) && value.every((v) => v !== null && typeof v === 'object')) {
    return (
      <DriversField
        items={value as { type?: string; ref?: string }[]}
        refTargets={refTargets}
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
    const listOptions = (fieldKey === 'anchors' || type === 'anchors') ? anchorOpts : type === 'links' ? refTargets : [];
    return <ListField fieldKey={fieldKey} items={value.map(String)} options={listOptions} onSet={onSet} onOpenPath={onOpenPath} />;
  }

  const current = String(value ?? '');

  if (type === 'date') {
    return <DateField fieldKey={fieldKey} value={current} onSet={onSet} />;
  }

  // categorical (schema values map): colored-pill combobox, free entry allowed
  if (enumValues) {
    return <ComboInput key={current} fieldKey={fieldKey} current={current} options={options} pill enumValues={enumValues} onSet={onSet} />;
  }
  if (type === 'percent') {
    const n = typeof value === 'number' ? value : parseFloat(String(value)) || 0;
    const pct = Math.round(n <= 1 ? n * 100 : n);
    const c = pct > 80 ? 'var(--data)' : pct > 60 ? 'var(--prod)' : 'var(--reg)';
    return (
      <span style={sx('display:inline-flex;align-items:center;gap:4px')}>
        <input
          type="number" min={0} max={100} defaultValue={pct}
          aria-label={fieldKey}
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
        aria-label={fieldKey}
        onBlur={(e) => { if (e.target.value !== current) onSet(e.target.value); }}
        style={sx('flex:1;min-width:260px;padding:6px 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;line-height:1.5;resize:vertical;outline:none')}
      />
    );
  }
  // plain scalar — categorical in practice when the workspace already uses
  // values for this key (owner, type, jurisdiction, …): same input, plus a
  // real options popup
  if (options.length) {
    return <ComboInput key={current} fieldKey={fieldKey} current={current} options={options} onSet={onSet} />;
  }
  return (
    <input
      key={current}
      defaultValue={current}
      aria-label={fieldKey}
      onBlur={(e) => { if (e.target.value !== current) onSet(e.target.value); }}
      onKeyDown={enterBlurs}
      style={{ ...sx(INPUT), minWidth: 180 }}
    />
  );
}

// AddPropertyRow appends a frontmatter key. Clicking "+ add property"
// immediately opens the key selector in the label column — the row already
// looks like the property it will become — and picking a key mounts the
// type-proper value editor on the right (enum pills, validated date, percent
// number, path options for links). Nothing is written until a non-empty
// value is committed, so cancel paths never touch the document.
function AddPropertyRow({ schema, presentKeys, corpusKeys, optionsFor, refTargets, anchorOpts, onAdd }: {
  schema: PropertySchema | undefined;
  presentKeys: string[];
  corpusKeys: string[];
  optionsFor: (key: string, def: FieldDef) => string[];
  refTargets: RefTarget[];
  anchorOpts: RefTarget[];
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
  const commitKey = (raw: string) => {
    const k = raw.trim();
    if (keyOk(k)) { setKeyDraft(k); setKey(k); setStage('value'); }
  };
  const commitValue = (raw: string) => {
    const t = raw.trim();
    if (!t) { reset(); return; }
    if (def.type === 'date' && !isValidDateStr(t)) return; // invalid dates never commit
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
        {/* key selector sits in the label column; autoFocus opens its popup
            right away (per-stage keys force a remount so that fires) */}
        <span style={sx('width:132px;flex:none;display:flex')}>
          <Combobox
            key="new-key"
            text={keyDraft}
            onText={setKeyDraft}
            options={knownKeys}
            placeholder="property…"
            autoFocus
            ariaLabel="new property name"
            style={{ ...sx(INPUT), width: 132, boxSizing: 'border-box', borderColor: draftBad ? 'var(--reg)' : undefined }}
            onCommit={(v, via) => { if (via === 'blur') reset(); else commitKey(v); }}
            onEscape={reset}
          />
        </span>
        <span style={sx('font-size:11px;color:var(--text-3)')}>pick or type a property name ⏎</span>
      </div>
    );
  }

  const colorOf = def.values ? (v: string) => PAL[def.values![v.trim().toLowerCase()] || 'slate'] || PAL.slate : undefined;
  const dateBad = def.type === 'date' && val.trim() !== '' && !isValidDateStr(val.trim());
  const valueOptions: (string | RefTarget)[] = def.type === 'links' ? refTargets
    : (key === 'anchors' || def.type === 'anchors') ? anchorOpts
    : optionsFor(key, def);
  return (
    <div style={sx('display:flex;gap:14px;padding:7px 14px;border-top:1px solid var(--border);align-items:center')}>
      <span style={sx(LABEL)}>{def.label || key.replace(/_/g, ' ')}</span>
      <div style={sx('flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0')}>
        {def.type === 'percent' ? (
          <span style={sx('display:inline-flex;align-items:center;gap:4px')}>
            <input
              type="number" min={0} max={100} autoFocus value={val} placeholder="0–100"
              aria-label={'value for ' + key}
              onChange={(e) => setVal(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') commitValue(val);
                if (e.key === 'Escape') reset();
              }}
              onBlur={() => commitValue(val)}
              style={{ ...sx(INPUT), width: 72 }}
            />
            <span style={sx('font-size:11px;color:var(--text-3)')}>%</span>
          </span>
        ) : (
          <Combobox
            key="new-value"
            text={val}
            onText={setVal}
            options={valueOptions}
            placeholder={def.type === 'date' ? 'YYYY-MM-DD ⏎' : 'value ⏎'}
            autoFocus
            ariaLabel={'value for ' + key}
            colorOf={colorOf}
            style={colorOf
              ? { ...sx(INPUT), minWidth: 140, background: colorOf(val).bg, color: colorOf(val).fg, fontWeight: 600, border: '1px solid transparent', borderRadius: 20 }
              : { ...sx(INPUT), minWidth: 180, borderColor: dateBad ? 'var(--reg)' : undefined }}
            onCommit={(v) => commitValue(v)}
            onEscape={reset}
          />
        )}
      </div>
    </div>
  );
}

// DriversField edits `drivers: [{type, ref}]` in place: the type comes from
// the workspace driver taxonomy, the ref is a searchable path#anchor target
// or free text.
function DriversField({ items, refTargets, onSet, onOpenPath }: {
  items: { type?: string; ref?: string }[];
  refTargets: RefTarget[];
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
            <DriverRef current={ref} options={refTargets} onCommit={(v) => update(i, { ref: v })} />
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
    </>
  );
}

// DriverRef: the searchable half of a driver chip — workspace docs and their
// anchors as options, free text (prose drivers) still allowed.
function DriverRef({ current, options, onCommit }: {
  current: string;
  options: RefTarget[];
  onCommit: (v: string) => void;
}) {
  const [text, setText] = useState(current);
  const isLink = (t: string) => /([\w-]+\/[\w.\/-]+\.md)/.test(t);
  return (
    <Combobox
      text={text}
      onText={setText}
      options={options}
      placeholder="doc path or free text"
      ariaLabel="driver ref"
      style={{ ...sx(INPUT), height: 22, width: Math.max(180, text.length * 6.6), border: 'none', background: 'transparent', color: isLink(text) ? 'var(--prod)' : 'var(--text)' }}
      onCommit={(v) => { if (v !== current) onCommit(v); }}
      onEscape={() => setText(current)}
    />
  );
}

function ListField({ fieldKey, items, options, onSet, onOpenPath }: {
  fieldKey: string;
  items: string[];
  options: RefTarget[];
  onSet: (v: unknown) => void;
  onOpenPath: (path: string) => void;
}) {
  const [adding, setAdding] = useState('');
  const isLink = (t: string) => /([\w-]+\/[\w.\/-]+\.(?:md|excalidraw|mermaid))/.test(t);
  const addable = options.filter((o) => !items.includes(o.value));
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
      {addable.length ? (
        // append via search-picker; blur keeps the draft so clicking away
        // never accidentally writes a half-typed entry
        <Combobox
          text={adding}
          onText={setAdding}
          options={addable}
          placeholder="+ add"
          ariaLabel={'add ' + fieldKey}
          style={{ ...sx(INPUT), width: 160, borderStyle: 'dashed' }}
          onCommit={(v, via) => {
            if (via === 'blur') return;
            const t = v.trim();
            if (t) { onSet([...items, t]); setAdding(''); }
          }}
          onEscape={() => setAdding('')}
        />
      ) : (
        <input
          placeholder="+ add"
          aria-label={'add ' + fieldKey}
          value={adding}
          onChange={(e) => setAdding(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && adding.trim()) {
              onSet([...items, adding.trim()]);
              setAdding('');
            }
          }}
          style={{ ...sx(INPUT), width: 130, borderStyle: 'dashed' }}
        />
      )}
    </>
  );
}
