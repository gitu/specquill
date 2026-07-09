// Multiple projects: the monorepo example project (SpecQuill's own specs,
// content_root docs/specs), the TopBar switcher, and isolation between
// projects. Requires the dev fixture with both projects.
import { expect, test } from '@playwright/test';

const H = { 'X-SpecQuill': '1' };

test('project switcher scopes the workspace to the selected project', async ({ page }) => {
  await page.goto('/#/editor');
  // default project is trading-specs
  await expect(page.locator('aside').getByText('TRADING-SPECS', { exact: true })).toBeVisible();

  // switch to the SpecQuill product specs (monorepo subfolder project)
  await page.getByTitle('Project').selectOption('specquill-docs');
  await expect(page.locator('aside').getByText('SPECQUILL-DOCS', { exact: true })).toBeVisible({ timeout: 10_000 });

  // paths are project-relative: the tree shows the families, never docs/specs/
  await expect(page.getByText('REQ-003.md').first()).toBeVisible();
  await expect(page.getByText('docs/specs')).toHaveCount(0);

  // the subfolder project's own requirement renders
  await page.getByText('REQ-003.md').first().click();
  await expect(page.getByText('Projects in repository subfolders').first()).toBeVisible();
  await expect(page.getByText('it is its own proof')).toBeVisible();

  // switch back — the first project is untouched
  await page.getByTitle('Project').selectOption('trading-specs');
  await expect(page.locator('aside').getByText('TRADING-SPECS', { exact: true })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('REQ-042.md').first()).toBeVisible();
});

test('monorepo project serves project-relative API paths only', async ({ request }) => {
  const tree = (await (await request.get('/api/repos/specquill-docs/tree', { headers: H })).json()) as { path: string }[];
  const paths = tree.map((e) => e.path);
  expect(paths).toContain('requirements/REQ-003.md');
  expect(paths.some((p) => p.startsWith('docs/'))).toBe(false);
  expect(paths.some((p) => p.includes('server.go'))).toBe(false);

  // sibling monorepo content is unreachable, even with traversal
  const res = await request.get('/api/repos/specquill-docs/files/..%2F..%2Fsrc%2Fserver.go.txt', { headers: H });
  expect(res.ok()).toBe(false);
});

test('management api: role-gated project lifecycle', async ({ request }) => {
  // the dev user is tenant admin; a bogus remote must fail cleanly
  const bad = await request.post('/api/projects', {
    headers: H,
    data: { id: 'nope', remote: '/does/not/exist.git' },
  });
  expect(bad.status()).toBe(502);

  // config-managed projects refuse deletion
  const del = await request.delete('/api/projects/trading-specs', { headers: H });
  expect(del.status()).toBe(409);
});

test('cross-repo reference renders as an external graph node', async ({ page }) => {
  await page.goto('/#/editor');
  await page.getByTitle('Project').selectOption('specquill-docs');
  await expect(page.locator('aside').getByText('SPECQUILL-DOCS', { exact: true })).toBeVisible({ timeout: 10_000 });

  await page.goto('/#/graph');
  // the ~regulations link in specs/references.md becomes an external node…
  await expect(page.getByText('~regulations')).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('mifid-ii').first()).toBeVisible();
  // …connected by a dashed edge
  await expect(page.locator('svg path[stroke-dasharray]').first()).toBeVisible();

  // back to the default project for the rest of the suite
  await page.goto('/#/editor');
  await page.getByTitle('Project').selectOption('trading-specs');
  await expect(page.locator('aside').getByText('TRADING-SPECS', { exact: true })).toBeVisible({ timeout: 10_000 });
});
