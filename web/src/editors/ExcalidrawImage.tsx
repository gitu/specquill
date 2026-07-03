// ProseMirror node view for image nodes whose src points at an .excalidraw
// file: renders the themed SVG preview; click opens the Excalidraw editor.
import { imageSchema } from '@milkdown/kit/preset/commonmark';
import { $view } from '@milkdown/kit/utils';
import type { Node as PMNode } from '@milkdown/kit/prose/model';
import type { NodeView } from '@milkdown/kit/prose/view';
import { EXCALIDRAW_CMAP, excalidrawToSvg, resolvePath } from '../lib/model';

export type OpenExcalidraw = (path: string) => void;

interface Ctx {
  docPath: string;
  files: Record<string, string> | undefined;
  onOpenExcalidraw: OpenExcalidraw;
  resolveAsset?: (src: string) => string;
}

export function excalidrawImageView(getCtx: () => Ctx) {
  return $view(imageSchema.node, () => (node: PMNode, view, getPos): NodeView => {
    const src = String(node.attrs.src || '');
    if (!/\.excalidraw$/.test(src)) {
      // plain raster image: doc-relative srcs load through the raw endpoint
      const img = document.createElement('img');
      const resolve = getCtx().resolveAsset;
      img.src = resolve ? resolve(src) : src;
      img.style.maxWidth = '100%';
      if (node.attrs.alt) img.alt = node.attrs.alt;
      // sketch PNGs (scene embedded in the file) open the sketch editor
      if (/\.excalidraw\.png$/i.test(src)) {
        const ctx = getCtx();
        const dir = ctx.docPath.split('/').slice(0, -1).join('/');
        const target = resolvePath(dir, src);
        img.style.cursor = 'pointer';
        img.title = 'Click to edit the sketch';
        img.addEventListener('click', (e) => {
          e.preventDefault();
          getCtx().onOpenExcalidraw(target);
        });
        img.addEventListener('error', () => {
          // not saved yet (fresh sketch): swap in a placeholder IMAGE — the
          // <img> element is ProseMirror-owned dom; replacing it would make
          // PM re-parse and drop the image node from the document
          if (img.dataset.ph) return;
          img.dataset.ph = '1';
          const label = target.split('/').pop() + ' — click to draw';
          img.src = 'data:image/svg+xml,' + encodeURIComponent(
            `<svg xmlns="http://www.w3.org/2000/svg" width="420" height="72">` +
            `<rect x="1" y="1" width="418" height="70" rx="10" fill="none" stroke="#b9bec7" stroke-dasharray="5 4"/>` +
            `<text x="20" y="42" font-family="sans-serif" font-size="14" fill="#8a8f98">✎ ${label}</text></svg>`);
        });
      }
      return { dom: img };
    }
    const ctx = getCtx();
    const dir = ctx.docPath.split('/').slice(0, -1).join('/');
    const target = resolvePath(dir, src);
    const box = document.createElement('div');
    box.className = 'excalidraw-embed';
    box.contentEditable = 'false';
    box.style.cssText = 'position:relative;border:1px solid var(--border);border-radius:10px;background:var(--surface);padding:14px 12px;margin:16px 0;cursor:pointer';
    box.title = 'Click to edit the sketch';
    const raw = getCtx().files?.[target];
    if (raw) {
      try {
        box.innerHTML = excalidrawToSvg(JSON.parse(raw), EXCALIDRAW_CMAP);
      } catch {
        box.textContent = '⚠ malformed .excalidraw file: ' + target;
      }
    } else {
      box.textContent = '✎ ' + target;
    }
    // hover ×: removes the embed from the document (the file itself stays;
    // delete it from the sketch editor if it is truly unwanted)
    const remove = document.createElement('button');
    remove.textContent = '×';
    remove.title = 'Remove this sketch from the document';
    remove.className = 'embed-remove';
    remove.style.cssText = 'position:absolute;top:8px;right:8px;width:24px;height:24px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text-2);font-size:15px;line-height:1;cursor:pointer;display:none';
    box.appendChild(remove);
    box.addEventListener('mouseenter', () => { remove.style.display = 'block'; });
    box.addEventListener('mouseleave', () => { remove.style.display = 'none'; });
    remove.addEventListener('click', (e) => {
      e.stopPropagation();
      if (!window.confirm('Remove this sketch embed from the document?')) return;
      const pos = getPos();
      if (pos !== undefined) view.dispatch(view.state.tr.delete(pos, pos + node.nodeSize));
    });
    box.addEventListener('click', () => getCtx().onOpenExcalidraw(target));
    return {
      dom: box,
      update: (n: PMNode) => n.type === node.type && n.attrs.src === src,
      ignoreMutation: () => true,
    };
  });
}
