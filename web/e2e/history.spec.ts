// Change history (REQ-027): the commit feed reads the workspace's own git
// history, classifies every touched path through the current config, and
// explains a commit as a semantic delta rather than as diff lines. The
// fixture's last two commits on main are built for exactly this (see
// scripts/dev-fixture.sh): a status transition and a reworded statement.
import { expect, test } from '@playwright/test';

// commit rows carry data-sha — the detail pane repeats the subject as a
// heading, so list assertions always go through the row locator
const rows = (page: import('@playwright/test').Page) => page.locator('[data-sha]');
const row = (page: import('@playwright/test').Page, subject: string) =>
  rows(page).filter({ hasText: subject });

test('the feed classifies commits by family and explains one as a delta', async ({ page }) => {
  await page.goto('/p/trading-specs/history');
  // the expanded rail carries the same label — assert the view's own header
  await expect(page.locator('main').getByText('Change history', { exact: true })).toBeVisible();

  // commits roll up in model terms, not as file counts
  const bound = row(page, 'req: bound the fills array');
  await expect(bound).toBeVisible();
  await expect(bound).toContainText('2 requirements');
  // family chips come from the config; counts move as other tests commit, so
  // assert the chip exists rather than a number
  await expect(page.getByText(/requirement \d+/)).toBeVisible();

  await bound.click();
  // property move: the retention window was extended
  await expect(page.getByText('requirements/REQ-090.md')).toBeVisible();
  await expect(page.getByText('2026-09-10', { exact: true })).toBeVisible();
  await expect(page.getByText('2026-09-30', { exact: true })).toBeVisible();
  // statement reword: same id, before and after
  await expect(page.getByText('~ REQ-063.1')).toBeVisible();
  await expect(page.getByText(/at most 500 entries/)).toBeVisible();

  // the raw diff stays one click away
  await page.getByText('Show text diff').click();
  await expect(page.getByText('@@', { exact: false }).first()).toBeVisible();
});

test('family filters narrow the feed and survive a reload', async ({ page }) => {
  await page.goto('/p/trading-specs/history');
  await page.getByText(/spec \d+/).click();
  await expect(page).toHaveURL(/kind=spec/);
  // the import commit touched specs; the requirement-only commits drop out
  await expect(row(page, 'import demo content')).toBeVisible();
  await expect(row(page, 'partial-fill reporting moves to review')).toHaveCount(0);

  await page.reload();
  await expect(row(page, 'import demo content')).toBeVisible();
  await page.getByText('All families').click();
  await expect(row(page, 'partial-fill reporting moves to review')).toBeVisible();
});
