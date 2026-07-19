// Not a test — `npx playwright test screenshot.helper` grabs a demo shot of
// the copilot mid-conversation for docs/review.
import { test } from '@playwright/test';
import { API, APP, H } from './helpers';

test('capture excalidraw modal screenshot', async ({ page }) => {
  test.skip(!process.env.SHOT, 'set SHOT=1 to capture');
  await page.goto(`${APP}/p/trading-specs/editor/diagrams/data-flow.excalidraw`);
  await page.getByTitle('Click to edit the sketch').click();
  await page.locator('.excalidraw [title*="Rectangle"]').waitFor({ timeout: 20_000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: '/tmp/rq-excalidraw.png' });
});

test('capture copilot demo screenshot', async ({ page, request }) => {
  test.skip(!process.env.SHOT, 'set SHOT=1 to capture');
  const info = (await (await request.get(`${API}/copilot/info`)).json()) as { model?: string };
  test.skip(info.model !== 'mock-1', 'needs the deterministic mock provider');
  await page.goto(`${APP}/p/trading-specs/editor/specs/txn-report.md`);
  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');
  await composer.fill('Which mapping drifted?');
  await composer.press('Enter');
  await page.getByText(/grounded on \d+ workspace files/).waitFor({ timeout: 15_000 });
  await page.getByRole('button', { name: /Draft edits & open as diff/ }).click();
  await page.getByText('Edits drafted on').waitFor({ timeout: 20_000 });
  await page.screenshot({ path: '/tmp/rq-copilot.png' });
});
