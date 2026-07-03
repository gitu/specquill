import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import mermaid from 'mermaid';
import { EXCALIDRAW_CMAP, excalidrawToSvg, resolvePath } from '../lib/model';
import { rawUrl } from '../api/client';
import { useApp } from '../state/AppContext';

let mermaidSeq = 0;

/**
 * Renders pre-built document HTML and hydrates it: mermaid blocks become SVG,
 * .excalidraw images render as themed sketches, and internal links navigate
 * within the app. Port of the prototype's hydrateDoc().
 */
export function DocBody({ html, docPath }: { html: string; docPath: string }) {
  const host = useRef<HTMLDivElement>(null);
  const nav = useNavigate();
  const app = useApp();
  const files = app.files;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    el.innerHTML = html;

    const dark = app.theme === 'dark';
    const codes = el.querySelectorAll('code.language-mermaid');
    if (codes.length) {
      try {
        mermaid.initialize({
          startOnLoad: false, theme: dark ? 'dark' : 'neutral', securityLevel: 'loose',
          fontFamily: "'IBM Plex Sans',sans-serif", themeVariables: { background: 'transparent' },
        });
      } catch { /* re-init noise */ }
      codes.forEach((c) => {
        const src = c.textContent || '';
        mermaid.render('mmd-' + ++mermaidSeq, src).then((r) => {
          const pre = c.closest('pre') || c;
          const div = document.createElement('div');
          div.style.cssText = 'text-align:center;padding:8px 0;margin:16px 0;background:transparent';
          div.innerHTML = r.svg;
          const svgEl = div.querySelector('svg');
          if (svgEl) svgEl.style.backgroundColor = 'transparent';
          pre.replaceWith(div);
        }).catch(() => { /* leave the fenced block visible */ });
      });
    }

    const dir = docPath.split('/').slice(0, -1).join('/');
    const drawExc = (raw: string | undefined, target: Element) => {
      if (!raw) return;
      try {
        const box = document.createElement('div');
        box.style.cssText = 'border:1px solid var(--border);border-radius:10px;background:var(--surface);padding:14px 12px;margin:16px 0';
        box.innerHTML = excalidrawToSvg(JSON.parse(raw), EXCALIDRAW_CMAP);
        target.replaceWith(box);
      } catch { /* malformed sketch: keep placeholder */ }
    };
    el.querySelectorAll('img').forEach((img) => {
      const src = img.getAttribute('src') || '';
      if (/\.excalidraw$/.test(src)) { drawExc(files?.[resolvePath(dir, src)], img); return; }
      if (/^(https?:|data:|blob:)/.test(src)) return;
      // repo-relative image: serve through the raw endpoint (reference-repo
      // docs carry a ~repo/ prefix and read at their default branch)
      const resolved = resolvePath(dir, src);
      const m = resolved.match(/^~([^/]+)\/(.*)$/);
      img.src = m ? rawUrl(m[1], '', m[2]) : rawUrl(app.repoId || '', app.branch, resolved);
      img.style.maxWidth = '100%';
    });
    const embed = el.querySelector('[data-excalidraw]');
    if (embed) drawExc(files?.[docPath], embed);

    el.querySelectorAll('a[href]').forEach((a) => {
      const href = a.getAttribute('href') || '';
      if (/^(https?:|#|mailto:)/.test(href) || !/\.(md|excalidraw|mermaid|ya?ml)(#|$)/.test(href)) return;
      (a as HTMLElement).style.cursor = 'pointer';
      a.addEventListener('click', (e) => {
        e.preventDefault();
        nav('/editor/' + resolvePath(dir, href.split('#')[0]));
      });
    });
  }, [html, docPath, app.theme, app.repoId, app.branch, files, nav]);

  return <div id="reqbase-doc" ref={host} />;
}
