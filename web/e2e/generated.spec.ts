// OKF derived reserved files (index.md, log.md) are generated at commit time
// — the UI marks them as such: grayed tree rows with a GEN tag, a view-only
// editor without Edit/Move, and creation/rename refusing the reserved names.
import { expect, test } from '@playwright/test';

test.describe.configure({ mode: 'serial' });

test('tree marks index.md rows as generated', async ({ page }) => {
  await page.goto('/p/trading-specs/editor');
  // every index.md row (one per fixture folder) carries the GEN tag
  const tags = page.locator('aside [title="generated automatically at commit time"]');
  await expect(tags.first()).toBeVisible();
  const rows = page.locator('aside div').filter({ hasText: /index\.md/ });
  expect(await tags.count()).toBeGreaterThanOrEqual(1);
  await expect(rows.last()).toBeVisible();
});

test('generated files open view-only: no Edit, no Move, generated chip', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/requirements/index.md');
  await expect(page.getByText('⟳ generated')).toBeVisible();
  // the mode toggle offers View/Source/History but no Edit for generated files
  await expect(page.getByText('Source', { exact: true })).toBeVisible();
  await expect(page.getByText('Edit', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Move' })).toHaveCount(0);
});

test('new-document dialog refuses reserved names', async ({ page }) => {
  await page.goto('/p/trading-specs/editor');
  await page.getByTitle('New document').first().click();
  const dialog = page.getByTestId('newdoc-dialog');
  await expect(dialog).toBeVisible();

  await page.getByTestId('newdoc-id').fill('index');
  await expect(dialog.getByText(/reserved — these files are generated/)).toBeVisible();
  await expect(page.getByTestId('newdoc-create')).toBeDisabled();

  // a regular ID is accepted again
  await page.getByTestId('newdoc-id').fill('REQ-900');
  await expect(page.getByTestId('newdoc-create')).toBeEnabled();
});
