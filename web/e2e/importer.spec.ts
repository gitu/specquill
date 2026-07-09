// Importer sources (P5): a non-git source (the demo OpenAPI spec the server
// serves to itself) is mirrored into a read-only repo, triggered from Admin.
import { expect, test } from '@playwright/test';

const H = { 'X-SpecQuill': '1' };

test('openapi importer source syncs and becomes browsable', async ({ page, request }) => {
  await page.goto('/#/admin');

  // the openapi source appears in the reference-sources section with its kind
  const row = page.locator('div').filter({ hasText: /^platform-api/ }).first();
  await expect(row.getByText('platform-api', { exact: true })).toBeVisible();
  await expect(row.getByText('openapi', { exact: true })).toBeVisible();

  // trigger a manual re-import
  await row.getByRole('button', { name: 'Sync now' }).click();
  await expect(row.getByText('synced', { exact: true })).toBeVisible({ timeout: 15_000 });

  // the imported mirror is browsable via the normal read path
  const tree = (await (await request.get('/api/repos/platform-api/tree', { headers: H })).json()) as { path: string }[];
  const paths = tree.map((e) => e.path);
  expect(paths).toContain('index.md');
  expect(paths).toContain('openapi.yaml');

  // the generated index summarizes the API contract for the copilot
  const idx = (await (await request.get('/api/repos/platform-api/files/index.md', { headers: H })).json()) as { content: string };
  expect(idx.content).toContain('# Platform API');
  expect(idx.content).toContain('GET /reports/rts22');
});
