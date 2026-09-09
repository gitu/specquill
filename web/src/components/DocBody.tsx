import { useEffect, useRef } from 'react';
import { useNav } from '../state/nav';
import mermaid from 'mermaid';
import { EXCALIDRAW_CMAP, excalidrawToSvg, resolveDocHref } from '../lib/model';
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
  const nav = useNav();
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
          fontFamily: "'Instrument Sans',sans-serif", themeVariables: { background: 'transparent' },
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
      if (/\.excalidraw$/.test(src)) { drawExc(files?.[resolveDocHref(dir, src)], img); return; }
      if (/^(https?:|data:|blob:)/.test(src)) return;
      // repo-relative image: serve through the raw endpoint (reference-repo
      // docs carry a ~repo/ prefix and read at their default branch).
      // Sketch PNGs get the sketchGen buster — their bytes change under a
      // stable path (editor save, speccy draw/upgrade) and the viewed doc
      // must show the new pixels, not the browser-cached ones.
      const resolved = resolveDocHref(dir, src);
      const m = resolved.match(/^~([^/]+)\/(.*)$/);
      const bust = /\.excalidraw\.png$/i.test(resolved) ? '&v=' + app.sketchGen : '';
      img.src = m ? rawUrl(m[1], '', m[2]) : rawUrl(app.repoId || '', app.branch, resolved) + bust;
      img.style.maxWidth = '100%';
    });
    const embed = el.querySelector('[data-excalidraw]');
    if (embed) drawExc(files?.[docPath], embed);

    el.querySelectorAll('a[href]').forEach((a) => {
      const href = a.getAttribute('href') || '';
      if (/^(https?:|#|mailto:)/.test(href)) return;
      // an archived HTML mock-up (chat attachment): show it inline in a
      // sandboxed frame — raw serving already pins the sandbox CSP — and let
      // the link itself open the original in a new tab
      if (/\.html?$/i.test(href) && !a.parentElement?.classList.contains('specquill-embed')) {
        const resolved = resolveDocHref(dir, href);
        const m = resolved.match(/^~([^/]+)\/(.*)$/);
        const src = m ? rawUrl(m[1], '', m[2]) : rawUrl(app.repoId || '', app.branch, resolved);
        const box = document.createElement('div');
        box.className = 'specquill-embed';
        box.style.cssText = 'margin:12px 0;';
        const frame = document.createElement('iframe');
        frame.src = src;
        frame.title = a.textContent || 'mock-up';
        frame.setAttribute('sandbox', 'allow-scripts allow-forms allow-popups');
        frame.setAttribute('loading', 'lazy');
        frame.style.cssText = 'display:block;width:100%;height:520px;border:1px solid var(--border);border-radius:8px;background:#fff';
        a.replaceWith(box);
        box.appendChild(frame);
        box.appendChild(a);
        (a as HTMLAnchorElement).href = src;
        (a as HTMLAnchorElement).target = '_blank';
        (a as HTMLAnchorElement).rel = 'noreferrer';
        a.textContent = (a.textContent || 'mock-up') + ' ↗';
        (a as HTMLElement).style.cssText = 'display:inline-block;margin-top:4px;font-size:11px';
        return;
      }
      if (!/\.(md|adoc|excalidraw|mermaid|ya?ml)(#|$)/.test(href)) return;
      (a as HTMLElement).style.cursor = 'pointer';
      a.addEventListener('click', (e) => {
        e.preventDefault();
        nav('/editor/' + resolveDocHref(dir, href));
      });
    });
  }, [html, docPath, app.theme, app.repoId, app.branch, app.sketchGen, files, nav]);

  return <div id="specquill-doc" ref={host} />;
}
