// ProseMirror node view for ```mermaid fenced blocks inside the Milkdown
// editor: renders the diagram inline, click-to-edit in a fullscreen overlay
// with a live preview, and supports deleting the whole block.
// Non-mermaid code blocks keep a plain editable pre/code view.
import type { Node as PMNode } from '@milkdown/kit/prose/model';
import type { EditorView as PMView, NodeView } from '@milkdown/kit/prose/view';
import mermaid from 'mermaid';

let seq = 0;

function initMermaid() {
  const dark = document.body.getAttribute('data-theme') === 'dark';
  mermaid.initialize({
    startOnLoad: false,
    theme: dark ? 'dark' : 'neutral',
    securityLevel: 'loose',
    fontFamily: "'IBM Plex Sans',sans-serif",
    themeVariables: { background: 'transparent' },
  });
}

async function renderInto(el: HTMLElement, code: string, errText = 'mermaid: syntax error — click to fix') {
  try {
    initMermaid();
    const { svg } = await mermaid.render('mmdv-' + ++seq, code);
    el.innerHTML = svg;
    const svgEl = el.querySelector('svg');
    if (svgEl) svgEl.style.backgroundColor = 'transparent';
    return true;
  } catch {
    el.innerHTML = `<div style="padding:12px;border:1px dashed var(--reg-line);border-radius:9px;color:var(--reg);font-size:12px;font-family:'IBM Plex Mono',monospace">${errText}</div>`;
    return false;
  }
}

interface Ctx {
  docPath: string;
}

export function mermaidBlockView(getCtx: () => Ctx) {
  return (node: PMNode, view: PMView, getPos: () => number | undefined): NodeView => {
    if (node.attrs.language !== 'mermaid') {
      const pre = document.createElement('pre');
      const code = document.createElement('code');
      pre.appendChild(code);
      return { dom: pre, contentDOM: code };
    }
    return new MermaidView(node, view, getPos);
  };
}

class MermaidView implements NodeView {
  dom: HTMLElement;
  private node: PMNode;
  private view: PMView;
  private getPos: () => number | undefined;
  private preview: HTMLElement;
  private overlay: HTMLElement | null = null;
  private themeObserver: MutationObserver;

  constructor(node: PMNode, view: PMView, getPos: () => number | undefined) {
    this.node = node;
    this.view = view;
    this.getPos = getPos;
    this.dom = document.createElement('div');
    this.dom.className = 'mermaid-block';
    this.dom.contentEditable = 'false';
    this.dom.style.cssText = 'margin:16px 0';
    this.preview = document.createElement('div');
    this.preview.style.cssText = 'text-align:center;padding:8px 0;cursor:pointer;border-radius:10px;background:transparent';
    this.preview.title = 'Click to edit the diagram';
    this.preview.addEventListener('click', () => this.openEditor());
    this.dom.appendChild(this.preview);
    void renderInto(this.preview, node.textContent);
    // follow theme switches: mermaid bakes colors into the SVG at render time
    this.themeObserver = new MutationObserver(() => void renderInto(this.preview, this.node.textContent));
    this.themeObserver.observe(document.body, { attributes: true, attributeFilter: ['data-theme'] });
  }

  private openEditor() {
    if (this.overlay) return;
    const overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(10,12,16,.55);z-index:60;display:flex;align-items:center;justify-content:center;padding:32px';
    const dialog = document.createElement('div');
    dialog.style.cssText = 'width:100%;height:100%;max-width:1200px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);display:flex;flex-direction:column;overflow:hidden';

    const header = document.createElement('div');
    header.style.cssText = 'height:46px;flex:none;display:flex;align-items:center;gap:10px;padding:0 16px;border-bottom:1px solid var(--border)';
    header.innerHTML = `<span style="display:inline-flex"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="1.8"><rect x="3.5" y="3.5" width="8" height="6" rx="1.4"/><rect x="12.5" y="14.5" width="8" height="6" rx="1.4"/><path d="M7.5 9.5v4.5a2 2 0 002 2h3"/></svg></span><span style="font-family:'IBM Plex Mono',monospace;font-size:12.5px;font-weight:600">mermaid diagram</span><div style="flex:1"></div>`;
    const btn = (label: string, css: string) => {
      const b = document.createElement('button');
      b.textContent = label;
      b.style.cssText = 'height:30px;padding:0 13px;border-radius:8px;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;' + css;
      return b;
    };
    const del = btn('Delete diagram', 'border:1px solid var(--reg-line);background:var(--surface);color:var(--del)');
    const cancel = btn('Cancel', 'border:1px solid var(--border-2);background:var(--surface);color:var(--text)');
    const done = btn('Done', 'border:none;background:var(--ai);color:#fff;padding:0 15px');
    header.append(del, cancel, done);

    const bodyEl = document.createElement('div');
    bodyEl.style.cssText = 'flex:1;min-height:0;display:grid;grid-template-columns:1fr 1.2fr;gap:14px;padding:14px';
    const ta = document.createElement('textarea');
    ta.value = this.node.textContent;
    ta.spellcheck = false;
    ta.style.cssText = "font-family:'IBM Plex Mono',monospace;font-size:13px;line-height:1.7;border:1px solid var(--border-2);border-radius:10px;background:var(--surface-2);color:var(--text);padding:12px 14px;resize:none;outline:none";
    const live = document.createElement('div');
    live.style.cssText = 'display:flex;align-items:center;justify-content:center;overflow:auto;background:var(--surface-2);border:1px solid var(--border);border-radius:10px;padding:12px';
    bodyEl.append(ta, live);
    dialog.append(header, bodyEl);
    overlay.appendChild(dialog);
    document.body.appendChild(overlay);
    this.overlay = overlay;

    const renderLive = () => void renderInto(live, ta.value, 'syntax error');
    renderLive();
    let t: ReturnType<typeof setTimeout>;
    ta.addEventListener('input', () => {
      clearTimeout(t);
      t = setTimeout(renderLive, 300);
    });
    const close = () => this.closeEditor();
    cancel.addEventListener('click', close);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
    done.addEventListener('click', () => {
      this.commit(ta.value);
      close();
    });
    del.addEventListener('click', () => {
      if (!window.confirm('Remove this diagram from the document?')) return;
      this.deleteBlock();
      close();
    });
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { close(); document.removeEventListener('keydown', onKey); } };
    document.addEventListener('keydown', onKey);
    ta.focus();
  }

  private closeEditor() {
    this.overlay?.remove();
    this.overlay = null;
  }

  // write the edited source back into the code_block's text content
  private commit(code: string) {
    const pos = this.getPos();
    if (pos === undefined) return;
    const { state } = this.view;
    const start = pos + 1;
    const end = pos + this.node.nodeSize - 1;
    const tr = code
      ? state.tr.replaceWith(start, end, state.schema.text(code))
      : state.tr.delete(start, end);
    this.view.dispatch(tr);
  }

  private deleteBlock() {
    const pos = this.getPos();
    if (pos === undefined) return;
    this.view.dispatch(this.view.state.tr.delete(pos, pos + this.node.nodeSize));
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type || node.attrs.language !== 'mermaid') return false;
    const changed = node.textContent !== this.node.textContent;
    this.node = node;
    if (changed) void renderInto(this.preview, node.textContent);
    return true;
  }

  stopEvent(): boolean { return this.overlay !== null; }
  ignoreMutation(): boolean { return true; }
  destroy() {
    this.closeEditor();
    this.themeObserver.disconnect();
  }
}
