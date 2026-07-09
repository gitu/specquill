// End-to-end flow against a running dev server (-dev auto-auth):
// navigate → edit → save → commit → branch → PR → approve → merge.
import { expect, test } from '@playwright/test';

const stamp = Date.now().toString(36);
const BRANCH = `feature/e2e-${stamp}`;

test.describe.configure({ mode: 'serial' });

test('dashboard shows live KPIs', async ({ page }) => {
  await page.goto('/#/dashboard');
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await expect(page.getByText('Trace coverage')).toBeVisible();
  await expect(page.getByText('Requirement changes')).toBeVisible();
});

test('matrix and graph render from the model', async ({ page }) => {
  await page.goto('/#/matrix');
  await expect(page.getByText('Traceability matrix')).toBeVisible();
  await expect(page.getByText(/requirements × \d+ artifacts/)).toBeVisible();
  await page.goto('/#/graph');
  await expect(page.getByText('Lineage · from links')).toBeVisible();
});

test('search palette finds a requirement', async ({ page }) => {
  await page.goto('/#/dashboard');
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await page.getByText('Search requirements, specs, fields, changes…').first().click();
  const input = page.getByPlaceholder('Search requirements, specs, fields, changes…');
  await input.fill('REQ-042');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/editor\/requirements\/REQ-042\.md/);
  await expect(page.getByText('Transaction Reporting').first()).toBeVisible();
});

test('branch → edit → save → commit → PR → approve → merge', async ({ page }) => {
  await page.goto('/#/editor/specs/venue.md');
  await expect(page.getByText('Venue Identification', { exact: false }).first()).toBeVisible();

  // create a branch via the switcher (prompt dialog)
  page.on('dialog', (d) => d.accept(BRANCH));
  await page.getByText('main', { exact: true }).first().click();
  await page.getByText(/New branch from/).click();
  await expect(page.getByText(BRANCH).first()).toBeVisible();

  // edit in source mode — autosave persists it (no Save button anymore)
  await page.getByText('Source', { exact: true }).click();
  const editor = page.locator('.cm-content');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(`\nE2E marker ${stamp}.\n`);
  await expect(page.locator('[data-sync="saved"]')).toBeVisible({ timeout: 10_000 });

  // tree shows the dirty state; commit it
  await expect(page.getByText('1 change', { exact: false }).first()).toBeVisible();
  await page.getByRole('button', { name: 'Commit' }).click();
  await page.getByPlaceholder('Commit message…').fill(`e2e: venue marker ${stamp}`);
  await page.locator('button', { hasText: 'Commit' }).last().click();
  await expect(page.getByText('clean').first()).toBeVisible();

  // open a PR
  await page.getByRole('button', { name: 'Open PR' }).click();
  await page.locator('input').first().fill(`E2E venue update ${stamp}`);
  await page.getByRole('button', { name: 'Create PR' }).click();
  await expect(page).toHaveURL(/#\/prs\/\d+/);
  await expect(page.getByText(`E2E venue update ${stamp}`)).toBeVisible();
  await expect(page.getByText(`E2E marker ${stamp}.`)).toBeVisible(); // diff line

  // approve + merge
  await page.getByRole('button', { name: /Approve/ }).click();
  await expect(page.getByText(/approved by/)).toBeVisible();
  await page.getByRole('button', { name: 'Merge', exact: true }).click();
  await expect(page.getByText('merged', { exact: true })).toBeVisible();

  // main now carries the change in the editor
  await page.goto('/#/editor/specs/venue.md');
  await expect(page.getByText(`E2E marker ${stamp}.`)).toBeVisible();
});

// regression: switching between already-visited (query-cached) files used to
// leave the editor blank because the draft reset raced the populate effect
test('rapid switching between cached files always renders', async ({ page }) => {
  await page.goto('/#/editor/requirements/REQ-051.md');
  await expect(page.getByText('REQ-051.1', { exact: false })).toBeVisible();
  await page.goto('/#/editor/requirements/REQ-063.md');
  await expect(page.getByText('REQ-063.1', { exact: false })).toBeVisible();
  for (let i = 0; i < 3; i++) {
    await page.getByText('REQ-051.md').first().click(); // tree entry (cached now)
    await expect(page.getByText('REQ-051.1', { exact: false })).toBeVisible();
    await page.getByText('REQ-063.md').first().click();
    await expect(page.getByText('REQ-063.1', { exact: false })).toBeVisible();
  }
});

test('default view setting controls the root redirect', async ({ page }) => {
  await page.goto('/#/dashboard');
  await page.getByTitle('Settings').click();
  await page.getByRole('combobox').last().selectOption('matrix');
  await page.goto('/#/');
  await expect(page).toHaveURL(/#\/matrix/);
  await expect(page.getByText('Traceability matrix')).toBeVisible();
  // back to workspace default
  await page.getByTitle('Settings').click();
  await page.getByRole('combobox').last().selectOption('');
  await page.goto('/#/');
  await expect(page).toHaveURL(/#\/editor/);
});

test('excalidraw modal opens with a usable editor UI', async ({ page }) => {
  await page.goto('/#/editor/diagrams/data-flow.excalidraw');
  await expect(page.getByText('data-flow.excalidraw').first()).toBeVisible();
  await page.getByTitle('Click to edit the sketch').click();
  // toolbar present = styles loaded and the canvas is interactive
  await expect(page.locator('.excalidraw')).toBeVisible({ timeout: 20_000 });
  await expect(page.locator('.excalidraw [title*="Rectangle"]')).toBeVisible();
  await page.getByRole('button', { name: 'Close' }).click();
});

test('insert buttons add a mermaid block and an embedded sketch', async ({ page, request }) => {
  const stamp = Date.now().toString(36);
  // self-heal: restore REQ-090 on the workspace (prior failed runs leave the
  // inserted mermaid in the doc, and clicking it opens the overlay)
  {
    const wsRes = await request.post('/api/repos/trading-specs/workspace', { headers: { 'X-SpecQuill': '1' }, data: {} });
    const wsb = ((await wsRes.json()) as { branch: string }).branch;
    const head = (await (await request.get(`/api/repos/trading-specs/files/requirements/REQ-090.md?ref=${encodeURIComponent(wsb)}&at=head`)).json()) as { content: string };
    const cur = (await (await request.get(`/api/repos/trading-specs/files/requirements/REQ-090.md?ref=${encodeURIComponent(wsb)}`)).json()) as { sha: string };
    if (head.content !== undefined && cur.sha) {
      await request.put(`/api/repos/trading-specs/files/requirements/REQ-090.md?branch=${encodeURIComponent(wsb)}`, {
        headers: { 'X-SpecQuill': '1' }, data: { content: head.content, baseSha: cur.sha },
      });
    }
  }
  await page.goto('/#/editor/requirements/REQ-090.md');
  // main is protected: entering Edit auto-switches to the personal workspace
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByText(/you're now on your workspace ws\//)).toBeVisible();
  await page.locator('.milkdown-editable').click();

  // mermaid template lands in the doc and renders as a diagram
  await page.getByRole('button', { name: 'Diagram', exact: true }).click();
  await expect(page.locator('.mermaid-block svg').first()).toBeVisible({ timeout: 15_000 });

  // sketch: creates the file, embeds it, opens the excalidraw editor
  page.on('dialog', (d) => d.accept(`e2e-${stamp}`));
  await page.getByRole('button', { name: 'Sketch', exact: true }).click();
  await expect(page.locator('.excalidraw')).toBeVisible({ timeout: 20_000 });
  await page.getByRole('button', { name: 'Close' }).click();
  // the embed made the doc dirty; autosave persists it to the workspace
  await expect(page.locator('[data-sync="saved"]')).toBeVisible({ timeout: 10_000 });

  // cleanup: sketches are now *.excalidraw.png created on first SAVE — closing
  // without drawing leaves no file, so the delete may 404 (that's fine)
  const wsRes = await request.post('/api/repos/trading-specs/workspace', { headers: { 'X-SpecQuill': '1' }, data: {} });
  const ws = (await wsRes.json()) as { branch: string };
  await request.delete(
    `/api/repos/trading-specs/files/diagrams/e2e-${stamp}.excalidraw.png?branch=${encodeURIComponent(ws.branch)}`,
    { headers: { 'X-SpecQuill': '1' } },
  ).catch(() => {});
  // restore the autosaved doc to its committed content
  const head = await (await request.get(`/api/repos/trading-specs/files/requirements/REQ-090.md?ref=${encodeURIComponent(ws.branch)}&at=head`)).json() as { content: string };
  const cur = await (await request.get(`/api/repos/trading-specs/files/requirements/REQ-090.md?ref=${encodeURIComponent(ws.branch)}`)).json() as { sha: string };
  await request.put(`/api/repos/trading-specs/files/requirements/REQ-090.md?branch=${encodeURIComponent(ws.branch)}`, {
    headers: { 'X-SpecQuill': '1' },
    data: { content: head.content, baseSha: cur.sha },
  });
});

test('read-only input repo refuses editing', async ({ page }) => {
  await page.goto('/#/editor/~regulations/regulations/dora.md');
  await expect(page.getByText('read-only · regulations')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save' })).toBeHidden();
});
