// Move/rename with reference rewriting (git mv server-side) + git file history.
import { APIRequestContext, expect, test } from '@playwright/test';

const REPO = 'trading-specs';
const H = { 'X-SpecQuill': '1' };

async function wsBranch(request: APIRequestContext): Promise<string> {
  const res = await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

async function put(request: APIRequestContext, branch: string, path: string, content: string) {
  const res = await request.put(`/api/repos/${REPO}/files/${path}?branch=${encodeURIComponent(branch)}`, {
    headers: H, data: { content, baseSha: '' },
  });
  if (!res.ok()) throw new Error(`put ${path}: ${res.status()}`);
}

test('move rewrites referencing documents and lands on the new path', async ({ page, request }) => {
  const stamp = Date.now().toString(36);
  const TARGET = `specs/scratch-mv-${stamp}.md`;
  const MOVED = `specs/scratch-mv-${stamp}-renamed.md`;
  const REF = `requirements/scratch-mvref-${stamp}.md`;
  const branch = await wsBranch(request);
  await put(request, branch, TARGET, `---\ntype: Specification\ntitle: Move me\n---\n\n# Move me\n\nbody\n`);
  await put(request, branch, REF, `---\ntype: Requirement\ntitle: Refers\nimplements: [${TARGET}]\n---\n\n# Refers\n\nSee [the spec](../${TARGET}).\n`);

  // land on the workspace branch (remembered per project after switching once)
  await page.goto(`/p/${REPO}/dashboard`);
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).click();
  await page.goto(`/p/${REPO}/editor/${TARGET}`);
  await expect(page.getByText('Move me').first()).toBeVisible();

  // move it, updating the one referencing document
  await page.getByRole('button', { name: 'Move', exact: true }).click();
  await expect(page.getByText('1 referencing document')).toBeVisible();
  const input = page.locator('input[type="text"], input:not([type])').last();
  await input.fill(MOVED);
  await page.getByRole('button', { name: 'Move', exact: true }).last().click();

  await expect(page).toHaveURL(new RegExp(MOVED.replace(/[.\\]/g, '\\$&')));
  await expect(page.getByText('Move me').first()).toBeVisible();

  // the reference now points at the new location (body link relative + frontmatter)
  const ref = (await (await request.get(`/api/repos/${REPO}/files/${REF}?ref=${encodeURIComponent(branch)}`)).json()) as { content: string };
  expect(ref.content).toContain(`implements: [${MOVED}]`);
  expect(ref.content).toContain(`(../${MOVED})`);
  expect(ref.content).not.toContain(`(../${TARGET})`);

  // the old path is gone from the branch
  const gone = await request.get(`/api/repos/${REPO}/files/${TARGET}?ref=${encodeURIComponent(branch)}`, { headers: H });
  expect(gone.ok()).toBe(false);

  // self-heal: drop the scratch files
  for (const p of [MOVED, REF]) {
    await request.delete(`/api/repos/${REPO}/files/${p}?branch=${encodeURIComponent(branch)}`, { headers: H });
  }
});

test('history drawer lists commits and previews a version', async ({ page }) => {
  await page.goto(`/p/${REPO}/editor/specs/venue.md`);
  await expect(page.getByText('Venue Identification').first()).toBeVisible();
  await page.getByText('History', { exact: true }).click();
  // fixture history: the import commit is always there
  await expect(page.getByText('import demo content').first()).toBeVisible({ timeout: 10_000 });
  await page.getByText('import demo content').first().click();
  await expect(page.getByText(/specs\/venue\.md @ [0-9a-f]{7}/)).toBeVisible();
  await expect(page.locator('pre').getByText('Venue Identification')).toBeVisible();
});

test('linkcheck endpoint reports fixture links healthy', async ({ request }) => {
  // internal + source links in both fixture projects resolve; externals skipped
  for (const repo of ['trading-specs', 'specquill-docs']) {
    const out = (await (await request.get(`/api/repos/${repo}/linkcheck?external=0`, { headers: H })).json()) as {
      counts: Record<string, { ok: number; broken: number }>;
      problems: { file: string; href: string; detail?: string }[] | null;
    };
    const internalProblems = (out.problems || []).filter((p) => !p.href.startsWith('~'));
    expect(internalProblems, `${repo}: ${JSON.stringify(out.problems)}`).toEqual([]);
    expect(out.counts.internal.ok).toBeGreaterThan(0);
  }
});
