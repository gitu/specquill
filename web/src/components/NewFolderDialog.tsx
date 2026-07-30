import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useNav } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import { useToasts } from './Toast';

/**
 * Create a folder in the workspace. Git cannot track an empty directory, so
 * the folder materializes through its first document — a README guide, the
 * same pattern `specquill init` seeds for entity folders.
 */
export function NewFolderDialog({ onClose }: { onClose: () => void }) {
  const app = useApp();
  const nav = useNav();
  const qc = useQueryClient();
  const toasts = useToasts();
  const { ensureWritableBranch } = useWorkspace();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const target = name.trim().replace(/^\/+|\/+$/g, '');
  const wellFormed = /^[\w.-]+(\/[\w.-]+)*$/.test(target) && !target.startsWith('.');
  const exists = target && app.files?.[target + '/README.md'] !== undefined;
  const valid = !!target && wellFormed && !exists;

  const submit = async () => {
    if (!valid || busy) return;
    setBusy(true);
    setError('');
    try {
      const readme = target + '/README.md';
      const title = target.split('/').pop()!;
      const branch = await ensureWritableBranch();
      await api(`/api/repos/${app.repoId}/files/${readme}?branch=${encodeURIComponent(branch)}`, {
        method: 'PUT',
        body: JSON.stringify({ content: `---\ntype: Guide\ntitle: ${title}\n---\n\n# ${title}\n\nDocuments in \`${target}/\` live here.\n`, baseSha: '' }),
      });
      qc.invalidateQueries();
      toasts.push({ text: `Created ${target}/ with a README guide`, kind: 'success' });
      onClose();
      nav('/editor/' + readme);
    } catch (e) {
      setError(String((e as Error).message || e));
      setBusy(false);
    }
  };

  return (
    <div onClick={onClose} style={sx('position:fixed;inset:0;background:rgba(10,12,16,.45);z-index:50;display:flex;align-items:center;justify-content:center')}>
      <div onClick={(e) => e.stopPropagation()} style={sx('width:440px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:20px 22px')}>
        <div style={sx('font-weight:700;font-size:15px')}>New folder</div>
        <div style={sx('font-size:12px;color:var(--text-3);margin-top:4px')}>
          Created with a README guide inside — git only tracks folders through their files.
        </div>

        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin:14px 0 5px')}>Folder name</label>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="notes or archive/notes"
          autoFocus
          onKeyDown={(e) => { if (e.key === 'Enter') void submit(); if (e.key === 'Escape') onClose(); }}
          style={sx("width:100%;box-sizing:border-box;height:32px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:12px")}
        />
        {target && !wellFormed && (
          <div style={sx('margin-top:8px;font-size:12px;color:var(--del)')}>
            Folder names use letters, digits, <code>- _ .</code> and <code>/</code> for nesting — no leading dot.
          </div>
        )}
        {exists && (
          <div style={sx('margin-top:8px;font-size:12px;color:var(--text-2)')}>
            <code>{target}/</code> already exists — creating opens its README instead.
          </div>
        )}

        {error && (
          <div style={sx('margin-top:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            {error}
          </div>
        )}

        <div style={sx('display:flex;justify-content:flex-end;gap:8px;margin-top:18px')}>
          <button onClick={onClose} style={sx('height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            Cancel
          </button>
          {exists ? (
            <button onClick={() => { onClose(); nav('/editor/' + target + '/README.md'); }}
              style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
              Open folder
            </button>
          ) : (
            <button onClick={() => void submit()} disabled={!valid || busy}
              style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--prod);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (!valid || busy ? 'opacity:.5' : ''))}>
              {busy ? 'Creating…' : 'Create folder'}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
