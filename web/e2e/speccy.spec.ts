// Speccy e2e — needs the dev server AND scripts/mock-llm.py running.
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  const info = await request.get('/api/speccy/info');
  const body = (await info.json()) as { enabled: boolean; model?: string };
  // assertions expect the deterministic mock provider (scripts/mock-llm.py)
  test.skip(!body.enabled || body.model !== 'mock-1', 'speccy not running against mock-llm');
});

test('chat streams a grounded reply', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/specs/txn-report.md');
  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');
  await composer.fill('Which mapping drifted?');
  await composer.press('Enter');
  // .first(): the chat tab's fallback title repeats the question until the
  // quick-model title arrives
  await expect(page.getByText('Which mapping drifted?').first()).toBeVisible();
  await expect(page.getByText(/grounded on \d+ workspace files/)).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('trade.executionTimestamp').last()).toBeVisible();
  // the granted, selected reference source reached the system prompt (P4): the
  // mock echoes the ~source headings it saw back into the reply.
  await expect(page.getByText(/grounded sources: regulations/)).toBeVisible();
});

test('draft edits land on a speccy branch for review', async ({ page }) => {
  await page.goto('/p/trading-specs/dashboard');
  await page.getByRole('button', { name: /Draft edits & open as diff/ }).click();
  await expect(page.getByText('Edits drafted on')).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText('speccy/2026-06-mifid-rts22').first()).toBeVisible();
  await expect(page.getByText('data-mappings/trade.md').last()).toBeVisible();

  // review switches to the speccy branch; the tree shows uncommitted changes
  await page.getByRole('button', { name: /Review on speccy\// }).click();
  await expect(page).toHaveURL(/\/p\/[\w-]+(\/b\/[^/]+)?\/editor\//);
  await expect(page.getByRole('button', { name: 'Commit' })).toBeVisible({ timeout: 10_000 });
});

const REPO = 'trading-specs';
const H = { 'X-SpecQuill': '1' };

test('chat tools edit a file as an uncommitted draft on the workspace branch', async ({ page, request }) => {
  const ws = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} })).json()) as { branch: string };
  const stamp = Date.now().toString(36);
  const doc = `specs/scratch-speccy-${stamp}.md`;
  await request.put(`/api/repos/${REPO}/files/${doc}?branch=${encodeURIComponent(ws.branch)}`, {
    headers: H, data: { content: `---\ntitle: Speccy scratch\nstatus: draft\n---\n\n# Speccy scratch\n\nmagic-${stamp} value\n`, baseSha: '' },
  });

  await page.goto(`/p/trading-specs/editor/${doc}`);
  // edits need the workspace branch — on protected main the panel is read-only
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(ws.branch, { exact: true }).click();
  await expect(page.getByText('✎ can edit')).toBeVisible();

  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');
  await composer.fill(`EDIT ${doc} REPLACE "magic-${stamp} value" WITH "magic-${stamp} EDITED"`);
  await composer.press('Enter');

  // tool activity chip, then the mock's confirmation text
  await expect(page.getByText('edit file', { exact: true })).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText(/applied the edit as an uncommitted draft/)).toBeVisible({ timeout: 15_000 });

  // the save is a real uncommitted draft on the branch, with the date bumped
  await expect.poll(async () =>
    ((await (await request.get(`/api/repos/${REPO}/files/${doc}?ref=${encodeURIComponent(ws.branch)}`)).json()) as { content: string }).content,
  { timeout: 15_000 }).toContain(`magic-${stamp} EDITED`);
  const edited = ((await (await request.get(`/api/repos/${REPO}/files/${doc}?ref=${encodeURIComponent(ws.branch)}`)).json()) as { content: string }).content;
  expect(edited).toMatch(/updated: \d{4}-\d{2}-\d{2}/);

  // reject the speccy draft via the discard endpoint (removes the scratch file)
  await request.post(`/api/repos/${REPO}/discard?branch=${encodeURIComponent(ws.branch)}`, { headers: H, data: { paths: [doc] } });
});

test('chat pauses on a speccy question and resumes with the answer', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/specs/txn-report.md');
  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');
  await composer.fill('ASKME please');
  await composer.press('Enter');

  await expect(page.getByText('Speccy asks')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('Which option do you want?')).toBeVisible();
  await page.getByRole('button', { name: 'beta', exact: true }).click();

  // the answer resumes the stream; the card records what was chosen
  await expect(page.getByText(/noted: beta/)).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('↳ beta')).toBeVisible();
});

test('chats survive closing the panel, auto-name, and dismiss individually', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/specs/txn-report.md');
  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');
  await composer.fill('Which mapping drifted?');
  await composer.press('Enter');
  await expect(page.getByText(/grounded on \d+ workspace files/)).toBeVisible({ timeout: 15_000 });
  // the first exchange auto-names the chat (quick tier via the mock)
  await expect(page.getByText('Mock Chat Title')).toBeVisible({ timeout: 15_000 });

  // close the panel (unmount) and reopen from the rail — the transcript survives
  await page.getByText('⌵').click();
  await expect(composer).toHaveCount(0);
  await page.getByTitle('Speccy').click();
  await expect(page.getByText('Which mapping drifted?').first()).toBeVisible();
  await expect(page.getByText('Mock Chat Title')).toBeVisible();

  // a second chat starts empty next to the first
  await page.getByRole('button', { name: 'new chat' }).click();
  await expect(page.getByText('New chat', { exact: true })).toBeVisible();
  await expect(page.getByText('Which mapping drifted?')).toHaveCount(0);

  // dismissing it returns to the named chat; dismissing that clears the panel
  await page.getByLabel('dismiss chat').click();
  await expect(page.getByText('Which mapping drifted?').first()).toBeVisible();
  await page.getByLabel('dismiss Mock Chat Title').click();
  await expect(page.getByText('Which mapping drifted?')).toHaveCount(0);
});

test('a question after a read_file round renders, and again after an answer', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/specs/txn-report.md');
  const composer = page.getByPlaceholder('Ask about requirements, changes, mappings…');

  // read_file round first, then the question — the reported failure chain
  await composer.fill('READFIRST please');
  await composer.press('Enter');
  await expect(page.getByText('read file', { exact: true })).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('Follow-up question?')).toBeVisible({ timeout: 15_000 });
  await page.getByRole('button', { name: 'delta', exact: true }).click();
  await expect(page.getByText(/noted: delta/)).toBeVisible({ timeout: 15_000 });

  // answered question → model reads a file → asks AGAIN (chip between cards)
  await composer.fill('ASKME again');
  await composer.press('Enter');
  await expect(page.getByText('Which option do you want?')).toBeVisible({ timeout: 15_000 });
  const free = page.getByPlaceholder('or answer in your own words ⏎');
  await free.fill('READFIRST for details');
  await free.press('Enter');
  await expect(page.getByText('read file', { exact: true }).last()).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('Follow-up question?').last()).toBeVisible({ timeout: 15_000 });
  await page.getByRole('button', { name: 'gamma', exact: true }).last().click();
  await expect(page.getByText(/noted: gamma/)).toBeVisible({ timeout: 15_000 });
});
