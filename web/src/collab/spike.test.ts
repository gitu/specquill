// @vitest-environment jsdom
// M0 spike (kept as a regression test): two Milkdown editors bound to two
// Y.Docs, connected by a loopback relay — validates @milkdown/plugin-collab
// against our kit version: seeding via applyTemplate, convergence both ways,
// serialization from either side.
import { afterEach, describe, expect, it } from 'vitest';
import { Editor, editorViewCtx, rootCtx } from '@milkdown/kit/core';
import { commonmark } from '@milkdown/kit/preset/commonmark';
import { gfm } from '@milkdown/kit/preset/gfm';
import { getMarkdown } from '@milkdown/kit/utils';
import { collab, collabServiceCtx } from '@milkdown/plugin-collab';
import { Doc, applyUpdate } from 'yjs';
import { Awareness } from 'y-protocols/awareness';

const editors: Editor[] = [];
afterEach(async () => {
  for (const e of editors.splice(0)) await e.destroy();
});

async function makePeer(doc: Doc, awareness: Awareness) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const editor = await Editor.make()
    .config((ctx) => ctx.set(rootCtx, host))
    .use(commonmark)
    .use(gfm)
    .use(collab)
    .create();
  editors.push(editor);
  let service!: ReturnType<typeof Object.assign>;
  editor.action((ctx) => {
    const s = ctx.get(collabServiceCtx);
    s.bindDoc(doc).setAwareness(awareness);
    service = s as never;
  });
  return { editor, service, host };
}

function connect(a: Doc, b: Doc) {
  a.on('update', (u: Uint8Array, origin: unknown) => { if (origin !== 'loop') applyUpdate(b, u, 'loop'); });
  b.on('update', (u: Uint8Array, origin: unknown) => { if (origin !== 'loop') applyUpdate(a, u, 'loop'); });
}

describe('milkdown collab spike', () => {
  it('seeds via template and converges across two editors', async () => {
    const docA = new Doc();
    const docB = new Doc();
    connect(docA, docB);
    const awA = new Awareness(docA);
    const awB = new Awareness(docB);

    const a = await makePeer(docA, awA);
    const b = await makePeer(docB, awB);

    // peer A is the seeder; B joins without applying the template
    (a.service as { applyTemplate: (t: string, c?: () => boolean) => unknown; connect: () => void })
      .applyTemplate('# Seeded\n\nHello collab.', () => true);
    (a.service as { connect: () => void }).connect();
    (b.service as { applyTemplate: (t: string, c?: () => boolean) => unknown; connect: () => void })
      .applyTemplate('# Seeded\n\nHello collab.', () => false);
    (b.service as { connect: () => void }).connect();
    await new Promise((r) => setTimeout(r, 50));

    const mdB = b.editor.action(getMarkdown());
    expect(mdB).toContain('# Seeded');
    expect(mdB).toContain('Hello collab.');

    // edit on B → visible on A
    b.editor.action((ctx) => {
      const view = ctx.get(editorViewCtx);
      view.dispatch(view.state.tr.insertText('B-EDIT ', 1));
    });
    await new Promise((r) => setTimeout(r, 50));
    expect(a.editor.action(getMarkdown())).toContain('B-EDIT');

    // both serialize identically
    expect(a.editor.action(getMarkdown())).toBe(b.editor.action(getMarkdown()));
  });
});
