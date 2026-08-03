// Not tests — the docs screenshot gallery. `make shots` boots an ISOLATED
// server (embedded SPA, fresh fixture state, mock-llm as mock-1) and runs
// this file with SHOT=1; images land in docs/screenshots/ where its
// README.md embeds them. Ordered on purpose: the AI steps (speccy draft,
// drift run) come first so the later changes/history shots have real
// pending work to show.
import { test, expect, APIRequestContext, Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

const OUT = path.resolve('../docs/screenshots');
const H = { 'X-SpecQuill': '1' };
const REPO = 'trading-specs';

test.beforeAll(() => fs.mkdirSync(OUT, { recursive: true }));
test.beforeEach(() => test.skip(!process.env.SHOT, 'set SHOT=1 to capture'));

const shot = async (page: Page, name: string) => {
  await page.waitForTimeout(400); // let fonts/transitions settle
  await page.screenshot({ path: path.join(OUT, `${name}.png`) });
};

// AI-backed shots need the deterministic keyless provider
async function needsMock(request: APIRequestContext) {
  const info = (await (await request.get('/api/speccy/info')).json()) as { model?: string };
  test.skip(info.model !== 'mock-1', 'needs the deterministic mock provider (make shots)');
}

test('speccy chat + draft edits', async ({ page, request }) => {
  await needsMock(request);
  await page.goto(`/p/${REPO}/editor/specs/txn-report.md`);
  const composer = page.getByPlaceholder('Ask about requirements, deadlines, mappings…');
  await composer.fill('Which mapping drifted?');
  await composer.press('ControlOrMeta+Enter');
  await page.getByText(/grounded on \d+ workspace files/).waitFor({ timeout: 15_000 });
  // the at-risk timed-dependency card offers the draft-edits flow (REQ-026)
  await page.getByRole('button', { name: /Draft the outstanding edits/ }).click();
  await page.getByText('Edits drafted on').waitFor({ timeout: 20_000 });
  await shot(page, 'speccy');
});

test('source alignment with findings', async ({ page, request }) => {
  await needsMock(request);
  // run drift over the specs against the demo sources; findings and the
  // report land on the dev user's ws branch (protected main), so view THAT
  const ws = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} })).json()) as { branch: string };
  const q = encodeURIComponent(ws.branch);
  await request.post(`/api/repos/${REPO}/drift/run?branch=${q}`, {
    headers: H,
    data: { mode: 'drift', paths: ['specs/'] },
  });
  await expect
    .poll(
      async () => {
        const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q}`, { headers: H })).json()) as {
          run?: { status: string };
        };
        return d.run?.status ?? 'pending';
      },
      { timeout: 60_000 },
    )
    .toMatch(/^(ok|error|cancelled)$/);
  await page.goto(`/p/${REPO}/b/${q}/alignment`);
  await page.locator('[data-drift-finding]').first().waitFor({ timeout: 15_000 });
  await shot(page, 'alignment');
});

test('overview dashboard', async ({ page }) => {
  await page.goto(`/p/${REPO}/dashboard`);
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await shot(page, 'dashboard');
});

test('editor', async ({ page }) => {
  await page.goto(`/p/${REPO}/editor/specs/txn-report.md`);
  await expect(page.locator('main').getByText('specs/txn-report.md').first()).toBeVisible();
  await shot(page, 'editor');
});

test('sketch modal', async ({ page }) => {
  await page.goto(`/p/${REPO}/editor/diagrams/data-flow.excalidraw`);
  await page.getByTitle('Click to edit the sketch').click();
  await page.locator('.excalidraw [title*="Rectangle"]').waitFor({ timeout: 20_000 });
  await page.waitForTimeout(600);
  await shot(page, 'sketch');
});

test('model definitions', async ({ page }) => {
  await page.goto(`/p/${REPO}/model`);
  await expect(page.getByRole('heading', { name: 'Model definitions' })).toBeVisible();
  await shot(page, 'model');
});

test('impact graph', async ({ page }) => {
  await page.goto(`/p/${REPO}/graph/requirements/REQ-042.md`);
  await expect(page.getByText('⚖ mifid-ii', { exact: true })).toBeVisible();
  await page.waitForTimeout(1200); // force layout settles
  await shot(page, 'graph');
});

test('links', async ({ page }) => {
  await page.goto(`/p/${REPO}/links`);
  await expect(page.getByRole('heading', { name: 'Links' })).toBeVisible();
  await shot(page, 'links');
});

test('timed dependencies', async ({ page }) => {
  await page.goto(`/p/${REPO}/timed?sel=` + encodeURIComponent('requirements/REQ-042.md'));
  await expect(page.locator('main').getByText('Timed dependencies', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Comes into force', { exact: true })).toBeVisible();
  await shot(page, 'timed');
});

test('pending changes', async ({ page, request }) => {
  // the speccy drafts and the drift report sit on the ws branch — show THAT
  const ws = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} })).json()) as { branch: string };
  await page.goto(`/p/${REPO}/b/${encodeURIComponent(ws.branch)}/changes`);
  await expect(page.getByRole('heading', { name: 'Pending changes' })).toBeVisible();
  await shot(page, 'changes');
});

test('change history', async ({ page }) => {
  await page.goto(`/p/${REPO}/history`);
  await expect(page.locator('main').getByText('Change history', { exact: true })).toBeVisible();
  await shot(page, 'history');
});

test('administration', async ({ page }) => {
  await page.goto('/admin');
  await expect(page.getByRole('heading', { name: 'Administration' })).toBeVisible();
  await shot(page, 'admin');
});
