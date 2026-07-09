// Narrow-viewport (reading-focused) layout.
import { expect, test } from '@playwright/test';

test.use({ viewport: { width: 390, height: 844 } });

test('narrow viewport: doc reads full-width, tree opens as a drawer', async ({ page }) => {
  await page.goto('/#/editor/requirements/REQ-051.md');
  await expect(page.getByText('Exception Handling').first()).toBeVisible();

  // rail and inline tree are gone; the copilot panel is closed
  await expect(page.getByTitle('Overview')).toHaveCount(0);
  await expect(page.getByText('TRADING-SPECS', { exact: true })).toBeHidden();
  await expect(page.getByPlaceholder(/Ask about requirements/)).toHaveCount(0);

  // the doc uses (nearly) the full width
  const doc = await page.locator('#specquill-doc').boundingBox();
  expect(doc!.width).toBeGreaterThan(330);

  // hamburger opens the file tree as a drawer; tapping a file navigates + closes
  await page.getByTitle('Files').click();
  await expect(page.getByText('TRADING-SPECS', { exact: true })).toBeVisible();
  await page.getByText('REQ-063.md').click();
  await expect(page.getByText('TRADING-SPECS', { exact: true })).toBeHidden();
  await expect(page).toHaveURL(/REQ-063/);

  // view/edit/source segments remain reachable
  await expect(page.getByText('View', { exact: true })).toBeVisible();
});
