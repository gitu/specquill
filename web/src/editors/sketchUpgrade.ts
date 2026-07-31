// Re-render a speccy-drawn sketch PNG through the real excalidraw.
// draw_sketch rasterizes server-side (embedded Virgil, clean lines) so the
// file is immediately viewable everywhere — but the browser has the real
// renderer. After the chat turn we load the embedded scene, export it with
// excalidraw (roughness, exact text layout) and save the pixels back; the
// scene itself is unchanged. Best-effort: any failure leaves the
// server-rendered pixels, which are perfectly usable.
import { putRaw, rawUrl } from '../api/client';

export async function upgradeSketchPixels(repo: string, branch: string, path: string): Promise<boolean> {
  try {
    const res = await fetch(rawUrl(repo, branch, path), { headers: { 'X-SpecQuill': '1' } });
    if (!res.ok) return false;
    const sha = (res.headers.get('ETag') || '').replace(/"/g, '');
    const blob = await res.blob();
    const { loadFromBlob, exportToBlob } = await import('@excalidraw/excalidraw');
    const restored = await loadFromBlob(blob, null, null);
    const elements = (restored.elements as unknown[]) || [];
    if (!elements.length) return false; // exportToBlob refuses empty scenes
    // same export shape as the sketch editor's save: light theme, transparent
    // background (dark mode comes from the CSS invert filter)
    const out = await exportToBlob({
      elements: restored.elements as never,
      appState: { exportEmbedScene: true, exportBackground: false, exportWithDarkMode: false, theme: 'light' } as never,
      files: (restored.files || {}) as never,
      mimeType: 'image/png',
    });
    // baseSha guards the race with a concurrent editor save — theirs wins
    await putRaw(repo, branch, path, out, sha);
    return true;
  } catch (e) {
    console.warn('sketch upgrade skipped for', path, e);
    return false;
  }
}
