import { useMemo, useState } from 'react';
import { useTenant } from '../api/hooks';
import { useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useNav } from '../state/nav';
import { api } from '../api/client';
import { useWorkspace } from '../hooks/useWorkspace';
import { newDocTemplate } from '../lib/newdoc';
import { existingIds, generateId, hasRandomToken, hasSlugToken, idPattern, slugify, slugifyPath } from '../lib/ids';

// sentinel for the "create a new subfolder" option — real subfolders come
// from existing paths and are slugified, so they can never collide with it
const NEW_SUB = '+new';

// Guided document creation: pick a family, a (sub)folder and a title; the ID
// comes from the family's ID scheme (config `ids:` or built-in) with live
// conflict detection against the workspace snapshot.
export function NewDocDialog({ initialKind, onClose }: { initialKind?: string; onClose: () => void }) {
  const tenant = useTenant();
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const { ensureWritableBranch } = useWorkspace();
  const entities = app.entities;
  const files = app.files || {};

  const initKind = initialKind || entities[0]?.kind || 'requirement';
  const [kind, setKind] = useState(initKind);
  const [title, setTitle] = useState('');
  const [sub, setSub] = useState('');           // '' = family root, NEW_SUB = typing a new one
  const [newSub, setNewSub] = useState('');
  const [id, setId] = useState(() => {
    const e = entities.find((x) => x.kind === initKind);
    return generateId(idPattern(initKind, app.configYml), existingIds(app.files || {}, e?.folder || 'requirements/')).id;
  });
  const [idTouched, setIdTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const ent = entities.find((e) => e.kind === kind) || entities[0];
  const folder = ent?.folder || 'requirements/';
  const pattern = idPattern(kind, app.configYml);
  const taken = useMemo(() => existingIds(files, folder), [files, folder]);

  // subfolders already present under the family folder (any depth)
  const subfolders = useMemo(() => {
    const set = new Set<string>();
    Object.keys(files).forEach((p) => {
      if (!p.startsWith(folder)) return;
      const rest = p.slice(folder.length);
      if (rest.includes('/')) set.add(rest.slice(0, rest.lastIndexOf('/')));
    });
    return [...set].sort();
  }, [files, folder]);

  const roll = (k = kind, t = title) => {
    const g = generateId(idPattern(k, app.configYml), existingIds(files, (entities.find((e) => e.kind === k) || ent).folder), t);
    setId(g.id);
    setIdTouched(false);
  };
  const pickKind = (k: string) => {
    setKind(k);
    setSub('');
    setNewSub('');
    roll(k, title);
  };
  const onTitle = (t: string) => {
    setTitle(t);
    // slug-based IDs follow the title until the user takes over the field
    if (!idTouched && hasSlugToken(pattern)) {
      const g = generateId(pattern, taken, t);
      setId(g.id);
    }
  };

  const subPath = sub === NEW_SUB ? slugifyPath(newSub) : sub;
  const fileName = (id || slugify(title) || 'untitled') + '.md';
  const path = folder + (subPath ? subPath + '/' : '') + fileName;
  const idConflict = !!id && taken.has(id.toLowerCase());
  const pathConflict = !!files[path];
  const canCreate = !!ent && !!id.trim() && !idConflict && !pathConflict && !busy && (sub !== NEW_SUB || !!slugifyPath(newSub));

  const create = async () => {
    if (!canCreate) return;
    setBusy(true);
    setError('');
    try {
      const branch = await ensureWritableBranch();
      await api<{ sha: string }>(`/api/repos/${app.repoId}/files/${path}?branch=${encodeURIComponent(branch)}`, {
        method: 'PUT',
        body: JSON.stringify({ content: newDocTemplate(path, entities, { id, title: title.trim() || id }), baseSha: '' }),
      });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'status', app.repoId] });
      qc.invalidateQueries({ queryKey: ['t', tenant, 'snapshot', app.repoId] });
      nav('/editor/' + path);
      onClose();
    } catch (e) {
      setError(String((e as Error).message || e));
      setBusy(false);
    }
  };

  const label = (t: string) => (
    <div style={sx('font-size:11px;font-weight:700;letter-spacing:.4px;color:var(--text-3);text-transform:uppercase;margin:14px 0 6px')}>{t}</div>
  );
  const input = 'width:100%;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:12.5px;box-sizing:border-box';

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div data-testid="newdoc-dialog" onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => { if (e.key === 'Enter' && canCreate) void create(); if (e.key === 'Escape') onClose(); }}
        style={sx('width:480px;max-height:85vh;overflow-y:auto;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>New document</div>

        {label('Type')}
        <div style={sx('display:flex;flex-wrap:wrap;gap:6px')}>
          {entities.map((e) => (
            <button key={e.kind} onClick={() => pickKind(e.kind)}
              style={sx('display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 11px;border-radius:8px;font-family:inherit;font-size:12px;cursor:pointer;' +
                (e.kind === kind
                  ? 'border:1px solid ' + e.color + ';background:color-mix(in srgb, ' + e.color + ' 10%, var(--surface));color:var(--text);font-weight:600'
                  : 'border:1px solid var(--border-2);background:var(--surface);color:var(--text-2)'))}>
              <span style={{ color: e.color }}>{e.icon}</span>{e.label}
            </button>
          ))}
        </div>
        {ent?.description && <div style={sx('margin-top:6px;font-size:11.5px;color:var(--text-3);line-height:1.45')}>{ent.description}</div>}

        {label('Title')}
        <input data-testid="newdoc-title" autoFocus value={title} onChange={(e) => onTitle(e.target.value)}
          placeholder="What is this document about?" style={sx(input)} />

        {label('Folder')}
        <div style={sx('display:flex;gap:6px')}>
          <select value={sub} onChange={(e) => setSub(e.target.value)} style={sx(input + ';flex:1;cursor:pointer')}>
            <option value="">{folder}</option>
            {subfolders.map((s) => <option key={s} value={s}>{folder + s + '/'}</option>)}
            <option value={NEW_SUB}>+ new subfolder…</option>
          </select>
          {sub === NEW_SUB && (
            <input autoFocus value={newSub} onChange={(e) => setNewSub(e.target.value)}
              placeholder={'subfolder, "a/b" nests'} style={sx(input + ';flex:1')} />
          )}
        </div>

        {label('ID')}
        <div style={sx('display:flex;gap:6px;align-items:center')}>
          <input data-testid="newdoc-id" value={id}
            onChange={(e) => { setId(e.target.value.trim()); setIdTouched(true); }}
            style={sx(input + ";flex:1;font-family:'JetBrains Mono',monospace;font-size:12px" + (idConflict ? ';border-color:var(--del)' : ''))} />
          {(hasRandomToken(pattern) || idTouched || (hasSlugToken(pattern) && !slugify(title))) && (
            <button title="Generate a new ID" onClick={() => roll()}
              style={sx('height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:13px;cursor:pointer')}>
              ⚄
            </button>
          )}
        </div>
        <div style={sx('margin-top:5px;font-size:11px;color:var(--text-3)')}>
          {idConflict
            ? <span style={sx('color:var(--del);font-weight:600')}>“{id}” is already taken in this workspace</span>
            : <>scheme <span style={sx("font-family:'JetBrains Mono',monospace")}>{pattern}</span></>}
        </div>

        <div style={sx("margin-top:14px;padding:8px 11px;border:1px solid var(--border);border-radius:8px;background:var(--surface-2);font-family:'JetBrains Mono',monospace;font-size:11.5px;color:" + (pathConflict ? 'var(--del)' : 'var(--text-2)'))}>
          {path}{pathConflict ? ' — already exists' : ''}
        </div>

        {error && <div style={sx('margin-top:8px;color:var(--del);font-size:12px')}>create failed: {error}</div>}
        <div style={sx('display:flex;gap:8px;justify-content:flex-end;margin-top:16px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>Cancel</button>
          <button data-testid="newdoc-create" onClick={() => void create()} disabled={!canCreate}
            style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (canCreate ? '' : 'opacity:.5;cursor:default'))}>
            {busy ? 'Creating…' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  );
}
