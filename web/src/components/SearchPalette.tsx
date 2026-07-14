import { useEffect, useMemo, useRef, useState } from 'react';
import { useNav } from '../state/nav';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { srcMeta } from '../lib/derive';

interface Hit {
  key: string;
  icon: string;
  color: string;
  label: string;
  sub: string;
  to: string;
}

// ⌘K command palette over the workspace model: requirements, specs,
// regulations, mappings, data fields and changes.
export function SearchPalette() {
  const nav = useNav();
  const app = useApp();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen((v) => !v);
        setQ('');
        setSel(0);
      }
      if (e.key === 'Escape') setOpen(false);
    };
    const onOpen = () => { setOpen(true); setQ(''); setSel(0); };
    window.addEventListener('keydown', onKey);
    window.addEventListener('specquill:search', onOpen);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('specquill:search', onOpen);
    };
  }, []);

  useEffect(() => { if (open) inputRef.current?.focus(); }, [open]);

  const all = useMemo<Hit[]>(() => {
    const M = app.model;
    if (!M) return [];
    return [
      ...M.requirements.map((r) => ({ key: 'req' + r.path, icon: '▤', color: 'var(--prod)', label: `${r.id} ${r.title}`, sub: r.path, to: '/editor/' + r.path })),
      ...M.specs.map((s) => ({ key: 'spec' + s.path, icon: '◈', color: 'var(--text-2)', label: s.title || s.name, sub: s.path, to: '/editor/' + s.path })),
      ...M.regs.map((r) => ({ key: 'reg' + r.path, icon: '◈', color: 'var(--reg)', label: r.title, sub: r.path, to: '/editor/' + r.path })),
      ...M.maps.map((m) => ({ key: 'map' + m.path, icon: '⇄', color: 'var(--data)', label: m.name, sub: m.path, to: '/editor/' + m.path })),
      ...M.fields.map((f) => ({ key: 'field' + f.name, icon: '⊞', color: 'var(--data)', label: f.name, sub: `${f.source} → ${f.name}${f.drift ? ' · ⚠ drift' : ''}`, to: '/editor/' + f.map })),
      ...M.changes.map((c) => ({ key: 'chg' + c.path, icon: srcMeta(c.source).icon, color: srcMeta(c.source).fg, label: c.title, sub: c.path, to: '/changes?sel=' + encodeURIComponent(c.path) })),
    ];
  }, [app.model]);

  const hits = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return all.slice(0, 12);
    return all.filter((h) => (h.label + ' ' + h.sub).toLowerCase().includes(needle)).slice(0, 12);
  }, [all, q]);

  if (!open) return null;

  const go = (h: Hit) => {
    setOpen(false);
    nav(h.to);
  };

  return (
    <div onClick={() => setOpen(false)} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:70;display:flex;align-items:flex-start;justify-content:center;padding-top:12vh')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:560px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);overflow:hidden')}>
        <input
          ref={inputRef}
          value={q}
          onChange={(e) => { setQ(e.target.value); setSel(0); }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') { e.preventDefault(); setSel((s) => Math.min(s + 1, hits.length - 1)); }
            if (e.key === 'ArrowUp') { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
            if (e.key === 'Enter' && hits[sel]) go(hits[sel]);
          }}
          placeholder="Search requirements, specs, fields, changes…"
          style={sx('width:100%;height:46px;padding:0 16px;border:none;border-bottom:1px solid var(--border);background:var(--surface);color:var(--text);font-family:inherit;font-size:14px;outline:none')}
        />
        <div style={sx('max-height:380px;overflow-y:auto;padding:6px')}>
          {hits.map((h, i) => (
            <div
              key={h.key}
              onClick={() => go(h)}
              onMouseEnter={() => setSel(i)}
              style={sx('display:flex;align-items:center;gap:10px;padding:8px 11px;border-radius:8px;cursor:pointer;' + (i === sel ? 'background:var(--surface-2)' : ''))}
            >
              <span style={{ color: h.color, flex: 'none', width: 16, textAlign: 'center' }}>{h.icon}</span>
              <span style={sx('font-size:13px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis')}>{h.label}</span>
              <div style={sx('flex:1')} />
              <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);white-space:nowrap")}>{h.sub}</span>
            </div>
          ))}
          {hits.length === 0 && <div style={sx("padding:18px;text-align:center;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>no matches</div>}
        </div>
      </div>
    </div>
  );
}
