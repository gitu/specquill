import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
// since 0.18 the editor styles ship separately — without this import the
// canvas mounts but the whole UI (toolbar, selection, handles) is unusable
import '@excalidraw/excalidraw/index.css';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useDeleteFile, useFileQuery, useSaveFile } from '../api/hooks';
import { putRaw, rawUrl } from '../api/client';
import { IconPen } from '../components/icons';

// Excalidraw is heavy — load it only when a sketch is actually opened.
const Excalidraw = lazy(() =>
  import('@excalidraw/excalidraw').then((m) => ({ default: m.Excalidraw })),
);

interface ExcalidrawAPI {
  getSceneElements: () => readonly unknown[];
  getAppState: () => { viewBackgroundColor?: string };
  getFiles: () => Record<string, unknown>;
}

interface Scene {
  elements: unknown[];
  appState: Record<string, unknown>;
  files: Record<string, unknown>;
}

/** the modern sketch format: a PNG with the scene JSON embedded (tEXt chunk) */
export const isSketchPng = (p: string) => /\.excalidraw\.png$/i.test(p);

/**
 * Full-screen editor for sketches. Two on-disk formats:
 * - `*.excalidraw.png` (preferred): a PNG with the scene embedded via
 *   excalidraw's export-embed-scene — renders natively as an image anywhere
 *   (GitHub included) and stays editable here.
 * - `*.excalidraw` (legacy): plain scene JSON, rendered by our own svg shim.
 */
export function ExcalidrawModal({ path, onClose, onSaved }: { path: string; onClose: () => void; onSaved?: () => void }) {
  const app = useApp();
  const png = isSketchPng(path);
  // legacy JSON loads through the text file API; PNGs load raw bytes
  const file = useFileQuery(png ? undefined : app.repoId, app.branch, path);
  const save = useSaveFile(app.repoId, app.branch);
  const del = useDeleteFile(app.repoId, app.branch);
  const apiRef = useRef<ExcalidrawAPI | null>(null);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  // png loading state: scene + sha of the loaded blob ('' = new file)
  const [pngScene, setPngScene] = useState<{ scene: Scene; sha: string } | null>(null);

  useEffect(() => {
    if (!png || !app.repoId) return;
    let gone = false;
    (async () => {
      try {
        const res = await fetch(rawUrl(app.repoId!, app.branch, path), { headers: { 'X-SpecQuill': '1' } });
        if (res.status === 404) {
          // fresh sketch: file is created on first save
          if (!gone) setPngScene({ scene: { elements: [], appState: {}, files: {} }, sha: '' });
          return;
        }
        if (!res.ok) throw new Error(`load failed (${res.status})`);
        const sha = (res.headers.get('ETag') || '').replace(/"/g, '');
        const blob = await res.blob();
        const { loadFromBlob } = await import('@excalidraw/excalidraw');
        const restored = await loadFromBlob(blob, null, null);
        if (!gone) {
          setPngScene({
            scene: {
              elements: (restored.elements as unknown[]) || [],
              appState: { viewBackgroundColor: 'transparent' },
              files: (restored.files as Record<string, unknown>) || {},
            },
            sha,
          });
        }
      } catch (e) {
        if (!gone) setError(String((e as Error).message || e));
      }
    })();
    return () => { gone = true; };
  }, [png, app.repoId, app.branch, path]);

  const doDelete = async () => {
    if (!window.confirm(`Delete ${path} from the branch? Documents embedding it will show a placeholder.`)) return;
    setError('');
    try {
      await del.mutateAsync({ path });
      onSaved?.();
      onClose();
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  const initialData = useCallback(() => {
    if (png) return pngScene!.scene;
    try {
      const parsed = JSON.parse(file.data!.content);
      return { elements: parsed.elements || [], appState: { viewBackgroundColor: 'transparent' }, files: parsed.files || {} };
    } catch {
      return { elements: [], appState: {} };
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file.data, png, pngScene]);

  const doSave = async () => {
    const api = apiRef.current;
    if (!api) return;
    setError('');
    setSaving(true);
    try {
      if (png) {
        // PNG with the scene embedded: natively viewable, still editable
        const { exportToBlob } = await import('@excalidraw/excalidraw');
        const blob = await exportToBlob({
          elements: api.getSceneElements() as never,
          appState: { exportEmbedScene: true, exportBackground: false } as never,
          files: api.getFiles() as never,
          mimeType: 'image/png',
        });
        const res = await putRaw(app.repoId!, app.branch, path, blob, pngScene?.sha || '');
        setPngScene((p) => (p ? { ...p, sha: res.sha } : p));
      } else {
        const scene = {
          type: 'excalidraw',
          version: 2,
          source: 'specquill',
          elements: api.getSceneElements(),
          appState: { gridSize: null, viewBackgroundColor: '#ffffff' },
          files: api.getFiles(),
        };
        await save.mutateAsync({ path, content: JSON.stringify(scene, null, 2) + '\n', baseSha: file.data!.sha });
      }
      setDirty(false);
      onSaved?.();
      onClose();
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setSaving(false);
    }
  };

  const ready = png ? !!pngScene : !!file.data;
  return (
    <div style={sx('position:fixed;inset:0;background:rgba(10,12,16,.55);z-index:60;display:flex;align-items:center;justify-content:center;padding:32px')}>
      <div style={sx('width:100%;height:100%;max-width:1200px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);display:flex;flex-direction:column;overflow:hidden')}>
        <div style={sx('height:46px;flex:none;display:flex;align-items:center;gap:10px;padding:0 16px;border-bottom:1px solid var(--border)')}>
          <span style={sx('color:var(--text-3);display:inline-flex')}><IconPen size={13} /></span>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12.5px;font-weight:600")}>{path}</span>
          {dirty && <span style={sx('width:6px;height:6px;border-radius:50%;background:var(--reg)')} />}
          {error && <span style={sx('color:var(--del);font-size:12px')}>{error}</span>}
          <div style={sx('flex:1')} />
          <button onClick={doDelete} disabled={del.isPending}
            style={sx('height:30px;padding:0 12px;border:1px solid var(--reg-line);border-radius:8px;background:var(--surface);color:var(--del);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>
            {del.isPending ? 'Deleting…' : 'Delete file'}
          </button>
          <button onClick={onClose} style={sx('height:30px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer')}>
            Close
          </button>
          <button onClick={doSave} disabled={!dirty || saving || save.isPending}
            style={sx('height:30px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + (!dirty ? 'opacity:.5' : ''))}>
            {saving || save.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
        <div style={sx('flex:1;min-height:0')}>
          {ready ? (
            <Suspense fallback={<div style={sx("height:100%;display:flex;align-items:center;justify-content:center;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>loading editor…</div>}>
              <Excalidraw
                excalidrawAPI={(api: unknown) => { apiRef.current = api as ExcalidrawAPI; }}
                initialData={initialData() as never}
                onChange={() => setDirty(true)}
                theme={app.theme}
              />
            </Suspense>
          ) : (
            <div style={sx("height:100%;display:flex;align-items:center;justify-content:center;color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:12px")}>
              loading {path}…
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
