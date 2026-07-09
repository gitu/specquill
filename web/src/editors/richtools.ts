// Slash-command menu and selection toolbar for the Milkdown editor — headless
// kit providers (floating-ui positioning) with our own muted DOM.
import type { Ctx } from '@milkdown/kit/ctx';
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core';
import { slashFactory, SlashProvider } from '@milkdown/kit/plugin/slash';
import { tooltipFactory, TooltipProvider } from '@milkdown/kit/plugin/tooltip';
import { TextSelection } from '@milkdown/kit/prose/state';
import type { EditorView } from '@milkdown/kit/prose/view';
import {
  createCodeBlockCommand, insertHrCommand,
  toggleEmphasisCommand, toggleInlineCodeCommand, toggleStrongCommand,
  wrapInBlockquoteCommand, wrapInBulletListCommand, wrapInHeadingCommand, wrapInOrderedListCommand,
} from '@milkdown/kit/preset/commonmark';
import { insertTableCommand, toggleStrikethroughCommand } from '@milkdown/kit/preset/gfm';
import { linkTooltipAPI } from '@milkdown/kit/component/link-tooltip';

export const slash = slashFactory('specquill-slash');
export const selectionTooltip = tooltipFactory('specquill-fmt');

/** host-app hooks the menu items call back into */
export interface RichHooks {
  requestImage: () => void;
  requestSketch: () => void;
}

interface SlashItem {
  label: string;
  hint: string;
  keywords: string;
  run: (ctx: Ctx) => void;
}

const cmd = (ctx: Ctx, key: { key: never } | unknown, payload?: unknown) =>
  ctx.get(commandsCtx).call((key as { key: string }).key as never, payload as never);

function slashItems(hooks: () => RichHooks): SlashItem[] {
  return [
    { label: 'Heading 1', hint: '#', keywords: 'h1 title', run: (c) => cmd(c, wrapInHeadingCommand, 1) },
    { label: 'Heading 2', hint: '##', keywords: 'h2 section', run: (c) => cmd(c, wrapInHeadingCommand, 2) },
    { label: 'Heading 3', hint: '###', keywords: 'h3', run: (c) => cmd(c, wrapInHeadingCommand, 3) },
    { label: 'Bullet list', hint: '•', keywords: 'ul list', run: (c) => cmd(c, wrapInBulletListCommand) },
    { label: 'Numbered list', hint: '1.', keywords: 'ol ordered', run: (c) => cmd(c, wrapInOrderedListCommand) },
    {
      label: 'Task list', hint: '☐', keywords: 'todo checkbox check',
      run: (c) => {
        cmd(c, wrapInBulletListCommand);
        const view = c.get(editorViewCtx);
        const { state } = view;
        const { $from } = state.selection;
        for (let d = $from.depth; d > 0; d--) {
          const n = $from.node(d);
          if (n.type.name === 'list_item') {
            view.dispatch(state.tr.setNodeMarkup($from.before(d), undefined, { ...n.attrs, checked: false }));
            break;
          }
        }
      },
    },
    { label: 'Quote', hint: '❝', keywords: 'blockquote requirement shall', run: (c) => cmd(c, wrapInBlockquoteCommand) },
    { label: 'Table', hint: '▦', keywords: 'grid columns rows', run: (c) => cmd(c, insertTableCommand) },
    { label: 'Divider', hint: '—', keywords: 'hr rule separator', run: (c) => cmd(c, insertHrCommand) },
    { label: 'Code block', hint: '‹›', keywords: 'code fence', run: (c) => cmd(c, createCodeBlockCommand, '') },
    { label: 'Mermaid diagram', hint: '⌗', keywords: 'diagram flowchart chart', run: (c) => cmd(c, createCodeBlockCommand, 'mermaid') },
    { label: 'Image', hint: '🖼', keywords: 'picture upload photo', run: () => hooks().requestImage() },
    { label: 'Sketch', hint: '✎', keywords: 'excalidraw drawing', run: () => hooks().requestSketch() },
  ];
}

/**
 * Build the slash-menu plugin view. Wire into the editor with
 * `.config(ctx => ctx.set(slash.key, { view: () => slashView(ctx, hooks) }))`.
 */
export function slashView(ctx: Ctx, hooks: () => RichHooks) {
  const items = slashItems(hooks);
  let visible: SlashItem[] = items;
  let active = 0;
  let currentView: EditorView | null = null;

  const el = document.createElement('div');
  el.className = 'slash-menu';
  el.style.position = 'absolute';

  const provider = new SlashProvider({
    content: el,
    shouldShow(view) {
      const content = provider.getContent(view);
      if (content == null || !content.startsWith('/')) return false;
      const filter = content.slice(1).toLowerCase();
      visible = items.filter((i) => (i.label + ' ' + i.keywords).toLowerCase().includes(filter));
      if (active >= visible.length) active = 0;
      render();
      return visible.length > 0;
    },
    trigger: '/',
  });

  const runItem = (item: SlashItem) => {
    const view = currentView;
    if (!view) return;
    const content = provider.getContent(view) ?? '';
    const { state, dispatch } = view;
    dispatch(state.tr.delete(state.selection.from - content.length, state.selection.from));
    provider.hide();
    item.run(ctx);
    view.focus();
  };

  function render() {
    el.replaceChildren(
      ...visible.map((item, i) => {
        const row = document.createElement('div');
        row.className = 'slash-item' + (i === active ? ' active' : '');
        row.innerHTML = `<span class="slash-hint">${item.hint}</span><span>${item.label}</span>`;
        row.addEventListener('mousedown', (e) => {
          e.preventDefault();
          runItem(item);
        });
        row.addEventListener('mousemove', () => {
          if (active !== i) { active = i; render(); }
        });
        return row;
      }),
    );
  }

  // capture-phase keyboard nav while the menu is open (runs before PM keymaps)
  const onKey = (e: KeyboardEvent) => {
    if (el.dataset.show !== 'true') return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      e.stopPropagation();
      active = (active + (e.key === 'ArrowDown' ? 1 : visible.length - 1)) % Math.max(1, visible.length);
      render();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      e.stopPropagation();
      if (visible[active]) runItem(visible[active]);
    } else if (e.key === 'Escape') {
      e.stopPropagation();
      provider.hide();
    }
  };

  return {
    update: (view: EditorView, prevState?: import('@milkdown/kit/prose/state').EditorState) => {
      if (!currentView) view.dom.addEventListener('keydown', onKey, true);
      currentView = view;
      // no-op transactions (collab y-sync echoes) must not reach the provider:
      // its debounce keeps only the LAST call's args, and its isSame guard
      // would then discard the real edit that preceded them
      if (prevState && prevState.doc.eq(view.state.doc) && prevState.selection.eq(view.state.selection)) return;
      provider.update(view, prevState);
    },
    destroy: () => {
      currentView?.dom.removeEventListener('keydown', onKey, true);
      provider.destroy();
      el.remove();
    },
  };
}

const FMT_BUTTONS: [label: string, title: string, cls: string, run: (ctx: Ctx) => void][] = [
  ['B', 'Bold', 'fmt-b', (c) => cmd(c, toggleStrongCommand)],
  ['I', 'Italic', 'fmt-i', (c) => cmd(c, toggleEmphasisCommand)],
  ['S', 'Strikethrough', 'fmt-s', (c) => cmd(c, toggleStrikethroughCommand)],
  ['‹›', 'Inline code', 'fmt-c', (c) => cmd(c, toggleInlineCodeCommand)],
  ['🔗', 'Link (Ctrl+K)', 'fmt-l', (c) => addLinkOnSelection(c)],
];

/** open the link edit tooltip over the current selection */
export function addLinkOnSelection(ctx: Ctx) {
  const view = ctx.get(editorViewCtx);
  const { selection } = view.state;
  if (selection.empty) return;
  ctx.get(linkTooltipAPI.key).addLink(selection.from, selection.to);
}

/** floating format toolbar over non-empty text selections */
export function selectionTooltipView(ctx: Ctx) {
  const el = document.createElement('div');
  el.className = 'sel-toolbar';
  el.style.position = 'absolute';
  for (const [label, title, cls, run] of FMT_BUTTONS) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = cls;
    b.title = title;
    b.textContent = label;
    b.addEventListener('mousedown', (e) => {
      e.preventDefault();
      run(ctx);
    });
    el.appendChild(b);
  }

  const provider = new TooltipProvider({
    content: el,
    debounce: 120,
    shouldShow(view) {
      const { selection, doc } = view.state;
      if (selection.empty || !(selection instanceof TextSelection)) return false;
      if (!doc.textBetween(selection.from, selection.to).trim()) return false;
      return view.editable;
    },
  });

  return {
    update: (view: EditorView, prevState?: import('@milkdown/kit/prose/state').EditorState) => {
      // same no-op-transaction filter as the slash menu (collab echoes)
      if (prevState && prevState.doc.eq(view.state.doc) && prevState.selection.eq(view.state.selection)) return;
      provider.update(view, prevState);
    },
    destroy: () => {
      provider.destroy();
      el.remove();
    },
  };
}
