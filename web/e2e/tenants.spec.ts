import { expect, test } from '@playwright/test';
import { API, APP, H, TENANT } from './helpers';

// REQ-022: tenants are completely separate — named in every URL, switched by
// plain navigation (no reload), with per-tenant caches. The dev server seeds
// a second empty config tenant "acme" (dev-mode fixture) for these tests.

test('tenant switcher navigates without a reload and scopes the workspace', async ({ page }) => {
  await page.goto(`${APP}/p/trading-specs/dashboard`);
  await expect(page).toHaveURL(new RegExp(`/t/${TENANT}/p/trading-specs`));
  // mark the JS context — a full reload would wipe it
  await page.evaluate(() => { (window as unknown as { __alive?: number }).__alive = 1; });

  const switcher = page.getByTitle('Tenant');
  await expect(switcher).toBeVisible();
  await switcher.selectOption('acme');
  // acme has no projects — its index falls back to the admin view
  await expect(page).toHaveURL(/\/t\/acme\/admin/);
  expect(await page.evaluate(() => (window as unknown as { __alive?: number }).__alive)).toBe(1);

  // back traverses the tenant switch; the dev tenant's data is intact
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`/t/${TENANT}/`));
  await expect(page.getByTitle('Project')).toBeVisible();
});

test('API requests are tenant-scoped by path', async ({ request }) => {
  const dev = await request.get(`${API}/repos`, { headers: H });
  expect(dev.ok()).toBeTruthy();
  expect((await dev.json()).map((r: { id: string }) => r.id)).toContain('trading-specs');

  // the fixture tenant is visible but empty
  const acme = await request.get('/api/t/acme/repos', { headers: H });
  expect(acme.ok()).toBeTruthy();
  expect(await acme.json()).toEqual([]);

  // an unknown tenant slug is refused
  const nope = await request.get('/api/t/nosuch/repos', { headers: H });
  expect(nope.status()).toBe(403);

  // tenant-less API paths no longer exist
  const bare = await request.get('/api/repos', { headers: H });
  expect(bare.status()).toBe(404);
});
