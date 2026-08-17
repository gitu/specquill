// Model view: the WHY/WHAT/HOW/WHEN model renders from built-in defaults +
// config overrides, the sample config dialog shows the full default setup,
// and the property schema reports its source (combined config.yml properties:
// in trading-specs, legacy stand-alone schema.json in specquill-docs).
import { expect, test } from '@playwright/test';

test('model view groups entities on the WHY/WHAT/HOW/WHEN axes with defaulted taxonomy', async ({ page }) => {
  await page.goto('/p/trading-specs/model');
  await expect(page.getByRole('heading', { name: 'Model definitions' })).toBeVisible();

  // axis headers, in order
  for (const axis of ['WHY', 'WHAT', 'HOW', 'WHEN']) {
    await expect(page.getByText(axis, { exact: true })).toBeVisible();
  }
  // built-in families render even when empty (work-items has no docs in the
  // fixture), and the config's custom family joins its configured axis
  await expect(page.getByText('Work items', { exact: true })).toBeVisible();
  await expect(page.getByText('product_driver · products/')).toBeVisible();

  // drivers/statuses/link types come from the built-in defaults — the fixture
  // config intentionally does not restate them (chip text = icon + label)
  await expect(page.getByText('⚖Regulatory', { exact: true })).toBeVisible();
  await expect(page.getByText('in review', { exact: true })).toBeVisible();
  await expect(page.getByText('delivers', { exact: true })).toBeVisible();

  // property schema comes from the combined config's properties: section
  await expect(page.getByText('· from config.yml properties:')).toBeVisible();
  await expect(page.getByText('Authority', { exact: true })).toBeVisible();
});

test('sample config dialog spells out the full default setup', async ({ page }) => {
  await page.goto('/p/trading-specs/model');
  await page.getByRole('button', { name: 'sample config' }).click();

  const dialog = page.getByTestId('sample-config');
  await expect(dialog).toBeVisible();
  // the full model is in the sample: WHEN family, linkage, attributes
  await expect(dialog.getByText('work_item:', { exact: true }).first()).toBeVisible();
  await expect(dialog.getByText(/\{ from: work_item/).first()).toBeVisible();
  await expect(dialog.getByText('properties:', { exact: true }).first()).toBeVisible();
  // a config exists in this fixture — no import button, only copy
  await expect(page.getByTestId('import-sample')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Copy' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Close' }).click();
  await expect(dialog).toHaveCount(0);
});

test('legacy stand-alone schema.json is still honored and labeled', async ({ page }) => {
  await page.goto('/p/specquill-docs/model');
  await expect(page.getByText('· from .specquill/schema.json (legacy)')).toBeVisible();
  // its fields drive the table
  await expect(page.getByText('Verified by', { exact: true })).toBeVisible();
});
