// Timed dependencies (REQ-026): documents carrying a validity window bucket
// into pending/active/expiring/expired against today, with the readiness of
// everything that links to them. The fixture dates are fixed, so the buckets
// are asserted by document, never by count.
import { expect, test } from '@playwright/test';

test('the timeline buckets the fixture windows and deep-links a document', async ({ page }) => {
  await page.goto('/p/trading-specs/timed');
  // scoped to main: the expanded rail carries the same label
  await expect(page.locator('main').getByText('Timed dependencies', { exact: true }).first()).toBeVisible();

  // REQ-042 starts 2026-09-01 and its specs are not approved → pending, at risk
  const req042 = page.locator('[data-timed="requirements/REQ-042.md"]');
  await expect(req042).toContainText('Pending');
  await expect(req042).toContainText('at risk');

  // MiFID II is in force (effective_from, no end) → active
  await expect(page.locator('[data-timed="regulations/mifid-ii.md"]')).toContainText('Active');
  // REQ-070 ended 2026-07-20 → expired, and the filter narrows to it
  await page.getByText('Expired', { exact: true }).first().click();
  await expect(page.locator('[data-timed="requirements/REQ-070.md"]')).toBeVisible();
  await expect(page.locator('[data-timed="requirements/REQ-042.md"]')).toHaveCount(0);

  // a per-document URL is shareable and opens on the detail pane
  await page.goto('/p/trading-specs/timed?sel=' + encodeURIComponent('requirements/REQ-042.md'));
  await expect(page.getByRole('heading', { name: 'Transaction Reporting' })).toBeVisible();
  await expect(page.getByText('Comes into force', { exact: true })).toBeVisible();
  await expect(page.getByText('Dependents ready', { exact: true })).toBeVisible();
  await expect(page.getByText('0/4', { exact: true })).toBeVisible();  // 4 documents link to it, none ready
  // the detail lists what depends on it and opens the document
  await page.getByRole('button', { name: 'Open document' }).click();
  await expect(page).toHaveURL(/editor\/requirements\/REQ-042\.md/);
});

test('the overview surfaces at-risk windows without opening the timeline', async ({ page }) => {
  await page.goto('/p/trading-specs/dashboard');
  await expect(page.getByText('Pending dependencies')).toBeVisible();
  await expect(page.getByText('Coming up')).toBeVisible();
  // the review card links into the timeline for the at-risk document
  const row = page.getByText('Transaction Reporting').first();
  await expect(row).toBeVisible();
  await row.click();
  await expect(page).toHaveURL(/timed\?sel=/);
});
