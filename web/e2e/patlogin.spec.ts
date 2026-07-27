// Forge-PAT mode: PAT login page → sign in → propose flow chrome.
//
// Self-skips unless the target server runs in forge-PAT mode (auth.forge in
// the config, pointed at scripts/mock-forge.py, WITHOUT -dev auto-auth) —
// the standard dev server is local-auth and skips this file.
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  const providers = await request.get('/auth/providers');
  const body = await providers.json().catch(() => ({}));
  test.skip(!body.forge, 'server is not in forge-PAT mode');
});

test('login page offers the PAT form with a token-creation link', async ({ page }) => {
  await page.goto('/#/login');
  await expect(page.getByText(/personal access token/i)).toBeVisible();
  await expect(page.getByRole('link', { name: /create one on/i })).toBeVisible();
  await expect(page.getByText('The token stays in this browser')).toBeVisible();
});

test('a mock token signs in, lands in the app, and shows Propose', async ({ page }) => {
  await page.goto('/#/login');
  await page.getByPlaceholder(/glpat|ghp/).fill('tok-dev');
  await page.getByRole('button', { name: /sign in with/i }).click();
  // authenticated app chrome: the forge merge mode renames Merge → Propose
  await expect(page.getByRole('button', { name: 'Propose changes' })).toBeVisible({ timeout: 20_000 });
  // the PAT is kept client-side for silent re-login
  expect(await page.evaluate(() => localStorage.getItem('specquill-pat'))).toBe('tok-dev');
});

test('a rejected token shows the forge error inline', async ({ page }) => {
  await page.goto('/#/login');
  await page.getByPlaceholder(/glpat|ghp/).fill('tok-nope');
  await page.getByRole('button', { name: /sign in with/i }).click();
  await expect(page.getByText(/token rejected/i)).toBeVisible();
});
