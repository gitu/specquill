import { useEffect, useRef } from 'react';
import { Editor, rootCtx, defaultValueCtx, editorViewOptionsCtx } from '@milkdown/kit/core';
import { commonmark, codeBlockSchema, toggleStrongCommand, toggleEmphasisCommand, toggleInlineCodeCommand } from '@milkdown/kit/preset/commonmark';
import { gfm, toggleStrikethroughCommand } from '@milkdown/kit/preset/gfm';
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener';
import { history } from '@milkdown/kit/plugin/history';
import { $view, callCommand, getMarkdown, insert, replaceAll } from '@milkdown/kit/utils';
import { clipboard } from '@milkdown/kit/plugin/clipboard';
import { tableBlock, tableBlockConfig } from '@milkdown/kit/component/table-block';
import { linkTooltipPlugin, linkTooltipConfig, configureLinkTooltip } from '@milkdown/kit/component/link-tooltip';
import { collab as collabPlugin, collabServiceCtx } from '@milkdown/plugin-collab';
import { addLinkOnSelection, selectionTooltip, selectionTooltipView, slash, slashView } from './richtools';
import type { Doc } from 'yjs';
import type { Awareness } from 'y-protocols/awareness';
import type { EditorView as PMEditorView } from '@milkdown/kit/prose/view';
import { mermaidBlockView } from './MermaidBlock';
import { excalidrawImageView } from './ExcalidrawImage';
import type { OpenExcalidraw } from './ExcalidrawImage';

export interface CollabBinding {
  doc: Doc;
  awareness: Awareness;
  /** this client seeds the room's document from `body` */
  seedGranted: boolean;
}

/**
 * WYSIWYG markdown editor (Preview mode). Receives the body only — the
 * frontmatter never enters the ProseMirror doc. onChange fires with the
 * serialized markdown after real user edits, so untouched docs can be saved
 * byte-identical from the original raw text.
 */
export interface MilkdownApi {
  insert: (markdown: string) => void;
  /** replace the whole document (used by reference linking) */
  replaceAll: (markdown: string) => void;
  /**
   * Serialize the current doc, or null when the user never edited it.
   * Save flows call this to pick up changes still sitting in the listener's
   * debounce window (e.g. a diagram edit followed by an immediate save).
   */
  flush: () => string | null;
  /** unconditional serialization (collab flushes are room-driven) */
  serialize: () => string;
  /** toggle an inline mark on the selection (toolbar; ⌘B/⌘I also work) */
  format: (mark: 'strong' | 'em' | 'code' | 'strike') => void;
}

export function MilkdownEditor({ body, docPath, files, collab, onChange, onDirty, onOpenPath, onOpenExcalidraw, onReady, onCollabTeardown, resolveAsset, onUploadImage, onRequestImage, onRequestSketch }: {
  body: string;
  docPath: string;
  files: Record<string, string> | undefined;
  collab?: CollabBinding; // co-editing: bind to the room's Y.Doc instead of defaultValue
  onChange: (markdown: string) => void;
  onDirty?: () => void; // fires immediately on the first doc change (undebounced)
  onOpenPath: (path: string) => void;
  onOpenExcalidraw: OpenExcalidraw;
  onReady?: (api: MilkdownApi | null) => void;
  /** collab: final serialization at unmount, before pending flushes lose the editor */
  onCollabTeardown?: (markdown: string) => void;
  /** doc-relative image src → displayable URL (raw endpoint) */
  resolveAsset?: (src: string) => string;
  /** upload a pasted/dropped image; resolves to the doc-relative src to embed */
  onUploadImage?: (file: File) => Promise<string | null>;
  /** slash-menu hooks: open the image picker / the sketch flow */
  onRequestImage?: () => void;
  onRequestSketch?: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const cbRef = useRef({ onChange, onDirty, onOpenPath, onOpenExcalidraw, onCollabTeardown, resolveAsset, onUploadImage, onRequestImage, onRequestSketch, files, docPath });
  cbRef.current = { onChange, onDirty, onOpenPath, onOpenExcalidraw, onCollabTeardown, resolveAsset, onUploadImage, onRequestImage, onRequestSketch, files, docPath };
  const onReadyRef = useRef(onReady);
  onReadyRef.current = onReady;
  const editedRef = useRef(false);
  // captured once per mount — a different room remounts via the React key
  const collabRef = useRef(collab);
  collabRef.current = collab;

  useEffect(() => {
    if (!host.current) return;
    let editor: Editor | undefined;
    let destroyed = false;

    // Milkdown's listener plugin debounces even its `updated` hook — a fast
    // type-then-navigate would never register as dirty. DOM input events on
    // the contenteditable are the undebounced truth for user typing.
    const hostEl = host.current;
    const onDomInput = () => {
      editedRef.current = true;
      cbRef.current.onDirty?.();
    };
    hostEl.addEventListener('input', onDomInput);

    const collabBinding = collabRef.current;
    // pasted/dropped images upload to the branch worktree, then embed
    const insertImages = (view: PMEditorView, imgs: File[], pos?: number) => {
      for (const file of imgs) {
        void cbRef.current.onUploadImage?.(file).then((src) => {
          if (!src) return;
          const node = view.state.schema.nodes.image?.createAndFill({ src, alt: file.name || '' });
          if (!node) return;
          view.dispatch(pos !== undefined
            ? view.state.tr.insert(Math.min(pos, view.state.doc.content.size), node)
            : view.state.tr.replaceSelectionWith(node));
          editedRef.current = true;
          cbRef.current.onDirty?.();
        });
      }
    };
    const imageFiles = (list: FileList | undefined | null) =>
      Array.from(list ?? []).filter((f) => f.type.startsWith('image/'));

    const builder = Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, host.current!);
        if (!collabBinding) ctx.set(defaultValueCtx, body); // collab seeds via applyTemplate
        ctx.update(editorViewOptionsCtx, (prev) => ({
          ...prev,
          attributes: { class: 'doc-typo milkdown-editable', spellcheck: 'false' },
          // while editing, a plain click places the cursor — internal links
          // navigate in-app on Ctrl/Cmd+click only (accidental navigation
          // mid-edit loses the user's place)
          handleClickOn: (_view, _pos, _node, _nodePos, event) => {
            if (!event.ctrlKey && !event.metaKey) return false;
            const a = (event.target as HTMLElement).closest('a[href]');
            if (!a) return false;
            const href = a.getAttribute('href') || '';
            if (/^(https?:|mailto:)/.test(href)) return false;
            if (/\.(md|excalidraw|mermaid|ya?ml|json)(#|$)/.test(href)) {
              event.preventDefault();
              cbRef.current.onOpenPath(href.split('#')[0]);
              return true;
            }
            return false;
          },
          handleKeyDown: (_view, event) => {
            if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
              event.preventDefault();
              addLinkOnSelection(ctx);
              return true;
            }
            return false;
          },
          handlePaste: (view, event) => {
            const imgs = imageFiles(event.clipboardData?.files);
            if (!imgs.length || !cbRef.current.onUploadImage) return false;
            event.preventDefault();
            insertImages(view, imgs);
            return true;
          },
          handleDrop: (view, event) => {
            const imgs = imageFiles((event as DragEvent).dataTransfer?.files);
            if (!imgs.length || !cbRef.current.onUploadImage) return false;
            event.preventDefault();
            const coords = view.posAtCoords({ left: (event as DragEvent).clientX, top: (event as DragEvent).clientY });
            insertImages(view, imgs, coords?.pos);
            return true;
          },
        }));
        // rich tooling: slash menu, selection toolbar, link tooltip, tables
        ctx.set(slash.key, { view: () => slashView(ctx, () => ({
          requestImage: () => cbRef.current.onRequestImage?.(),
          requestSketch: () => cbRef.current.onRequestSketch?.(),
        })) });
        ctx.set(selectionTooltip.key, { view: () => selectionTooltipView(ctx) });
        configureLinkTooltip(ctx);
        ctx.update(linkTooltipConfig.key, (prev) => ({
          ...prev,
          inputPlaceholder: 'Paste or type a link…',
        }));
        const tblBtn = (svg: string, label: string) => svg + (label ? `<span>${label}</span>` : '');
        const plusSvg = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>';
        const xSvg = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 6l12 12M18 6L6 18"/></svg>';
        const gripSvg = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01"/></svg>';
        ctx.update(tableBlockConfig.key, (prev) => ({
          ...prev,
          renderButton: (t: string) =>
            t === 'add_row' || t === 'add_col' ? tblBtn(plusSvg, '')
            : t === 'delete_row' || t === 'delete_col' ? tblBtn(xSvg, '')
            : t === 'align_col_left' ? '⇤' : t === 'align_col_center' ? '⇹' : t === 'align_col_right' ? '⇥'
            : gripSvg,
        }));
        const l = ctx.get(listenerCtx);
        // undebounced: mark dirty the moment the doc changes
        l.updated(() => {
          editedRef.current = true;
          cbRef.current.onDirty?.();
        });
        l.markdownUpdated((_ctx, markdown, prevMarkdown) => {
          if (prevMarkdown !== null && markdown !== prevMarkdown) {
            editedRef.current = true;
            cbRef.current.onChange(markdown);
          }
        });
      })
      .use(commonmark)
      .use(gfm)
      .use(listener)
      .use($view(codeBlockSchema.node, () => mermaidBlockView(() => cbRef.current)))
      .use(excalidrawImageView(() => cbRef.current))
      .use(clipboard)
      .use(slash)
      .use(selectionTooltip)
      .use(tableBlock)
      .use(linkTooltipPlugin);
    // collab mode: y-undo replaces the history plugin (undo stays local-only)
    if (collabBinding) builder.use(collabPlugin);
    else builder.use(history);

    builder
      .create()
      .catch((err) => { console.error('[mde] create failed', err); throw err; })
      .then((e) => {
        if (destroyed) {
          e.destroy();
          return;
        }
        editor = e;
        if (collabBinding) {
          e.action((ctx) => {
            const service = ctx.get(collabServiceCtx);
            service.bindDoc(collabBinding.doc).setAwareness(collabBinding.awareness);
            // emptiness guard: a rebind within the session's teardown grace
            // (theme switch, navigate away & back) must not re-apply the
            // stale template over the live doc
            service.applyTemplate(body, () =>
              collabBinding.seedGranted && collabBinding.doc.getXmlFragment('prosemirror').length === 0);
            service.connect();
          });
        }
        onReadyRef.current?.({
          insert: (md) => {
            e.action(insert(md));
            editedRef.current = true;
            cbRef.current.onDirty?.();
          },
          replaceAll: (md) => {
            editedRef.current = true;
            e.action(replaceAll(md, true));
            cbRef.current.onDirty?.();
          },
          // byte-fidelity guard: only serialize when the user actually edited
          flush: () => (editedRef.current ? e.action(getMarkdown()) : null),
          serialize: () => e.action(getMarkdown()),
          format: (mark) => {
            const cmd = { strong: toggleStrongCommand, em: toggleEmphasisCommand, code: toggleInlineCodeCommand, strike: toggleStrikethroughCommand }[mark];
            e.action(callCommand(cmd.key));
            editedRef.current = true;
            cbRef.current.onDirty?.();
          },
        });
      });

    return () => {
      destroyed = true;
      hostEl.removeEventListener('input', onDomInput);
      // give pending room flushes their content while the editor still exists
      if (collabBinding && editor) {
        try { cbRef.current.onCollabTeardown?.(editor.action(getMarkdown())); } catch { /* torn-down editor */ }
      }
      onReadyRef.current?.(null);
      editor?.destroy();
    };
    // recreate per document; body prop is the initial value only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docPath]);

  return <div ref={host} className="milkdown-host" />;
}
