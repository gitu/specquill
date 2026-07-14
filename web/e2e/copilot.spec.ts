// Copilot e2e — needs the dev server AND scripts/mock-llm.py running.
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  const info = await request.get('/api/copilot/info');
  const body = (await info.json()) as { enabled: boolean; model?: string };
  // assertions expect the deterministic mock provider (scripts/mock-llm.py)
  test.skip(!body.enabled || body.model !== 'mock-1', 'copilot not running against mock-llm');
});

test('chat streams a grounded reply', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/specs/txn-report.md');
  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');
  await composer.fill('Which mapping drifted?');
  await composer.press('Enter');
  await expect(page.getByText('Which mapping drifted?')).toBeVisible();
  await expect(page.getByText(/grounded on \d+ workspace files/)).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('trade.executionTimestamp').last()).toBeVisible();
  // the granted, selected reference source reached the system prompt (P4): the
  // mock echoes the ~source headings it saw back into the reply.
  await expect(page.getByText(/grounded sources: regulations/)).toBeVisible();
});

test('draft edits land on a copilot branch for review', async ({ page }) => {
  await page.goto('/p/trading-specs/dashboard');
  await page.getByRole('button', { name: /Draft edits & open as diff/ }).click();
  await expect(page.getByText('Edits drafted on')).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText('copilot/2026-06-mifid-rts22').first()).toBeVisible();
  await expect(page.getByText('data-mappings/trade.md').last()).toBeVisible();

  // review switches to the copilot branch; the tree shows uncommitted changes
  await page.getByRole('button', { name: /Review on copilot\// }).click();
  await expect(page).toHaveURL(/\/p\/[\w-]+(\/b\/[^/]+)?\/editor\//);
  await expect(page.getByRole('button', { name: 'Commit' })).toBeVisible({ timeout: 10_000 });
});
