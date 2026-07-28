// Editor ↔ Impact Graph roundtrip keeps the document in the URL, and the
// graph can focus on that document's chain (drivers up and down) with a
// toggle back to the full graph (?full=1).
import { expect, test } from '@playwright/test';

test('doc URL survives the editor↔graph roundtrip; focus filters the chain', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/requirements/REQ-042.md');
  await expect(page.locator('main').getByText('requirements/REQ-042.md').first()).toBeVisible();

  // → Impact Graph: the URL carries the doc and the graph opens focused
  await page.getByText('Impact Graph', { exact: true }).click();
  await expect(page).toHaveURL(/\/graph\/requirements\/REQ-042\.md$/);
  await expect(page.getByText('⚖ mifid-ii', { exact: true })).toBeVisible();
  // gdpr drives REQ-090, a different chain — filtered out
  await expect(page.getByText('⚖ gdpr', { exact: true })).toHaveCount(0);

  // the focus chip toggles to the full graph (URL: ?full=1)
  await page.getByText('◎ REQ-042.md', { exact: true }).click();
  await expect(page).toHaveURL(/\/graph\/requirements\/REQ-042\.md\?full=1$/);
  await expect(page.getByText('⚖ gdpr', { exact: true })).toBeVisible();

  // ← back to the editor: same document, proper URL
  await page.getByText('REQ-042.md', { exact: true }).first().click();
  await expect(page).toHaveURL(/\/editor\/requirements\/REQ-042\.md$/);
  await expect(page.locator('main').getByText('requirements/REQ-042.md').first()).toBeVisible();
});
