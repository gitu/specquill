// @vitest-environment jsdom
// Golden round-trip suite: parse every repo markdown body through the same
// Milkdown pipeline the editor uses, serialize, and assert the result is
// stable (serialize∘parse is idempotent) and content-preserving.
import { afterAll, describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { Editor, rootCtx, defaultValueCtx } from '@milkdown/kit/core';
import { commonmark } from '@milkdown/kit/preset/commonmark';
import { gfm } from '@milkdown/kit/preset/gfm';
import { getMarkdown } from '@milkdown/kit/utils';
import { stripFrontmatter } from '../lib/model';

// vitest runs with cwd = web/; jsdom rewrites import.meta.url, so use cwd
const REPO = join(process.cwd(), '../repo');

const editors: Editor[] = [];
afterAll(async () => { for (const e of editors) await e.destroy(); });

async function serialize(md: string): Promise<string> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const editor = await Editor.make()
    .config((ctx) => {
      ctx.set(rootCtx, host);
      ctx.set(defaultValueCtx, md);
    })
    .use(commonmark)
    .use(gfm)
    .create();
  editors.push(editor);
  return editor.action(getMarkdown());
}

const files: [string, string][] = [];
for (const folder of ['regulations', 'requirements', 'specs', 'data-mappings', 'changes']) {
  for (const name of readdirSync(join(REPO, folder))) {
    if (name.endsWith('.md')) files.push([`${folder}/${name}`, readFileSync(join(REPO, folder, name), 'utf8')]);
  }
}

describe('milkdown round-trip over every repo markdown body', () => {
  for (const [path, raw] of files) {
    it(path, async () => {
      const { body } = stripFrontmatter(raw);
      const once = await serialize(body);
      const twice = await serialize(once);
      // normalization must be stable after one pass
      expect(twice).toBe(once);
      // headings survive (incl. {#anchor} suffixes kept as literal text)
      for (const m of body.matchAll(/^#{1,6}\s+(.+)$/gm)) {
        expect(once).toContain(m[1].trim());
      }
      // mermaid fences survive
      if (body.includes('```mermaid')) expect(once).toContain('```mermaid');
      // task list states survive
      if (body.includes('- [x]')) expect(once).toContain('[x]');
    });
  }
});
