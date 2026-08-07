// Guided authoring e2e — needs the dev server AND scripts/mock-llm.py running.
import { expect, test, type Page } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  const info = await request.get('/api/speccy/info');
  const body = (await info.json()) as { enabled: boolean; model?: string };
  // assertions expect the deterministic mock provider (scripts/mock-llm.py)
  test.skip(!body.enabled || body.model !== 'mock-1', 'speccy not running against mock-llm');
});

const REPO = 'trading-specs';

/** intent → related → interview, the shared prefix of every case below. */
async function toInterview(page: Page, intent: string, family = 'spec') {
  await page.goto(`/p/${REPO}/wizard`);
  await page.getByTestId('wizard-intent').fill(intent);
  await page.getByTestId('wizard-family-' + family).click();
  await page.getByTestId('wizard-start').click();

  // dedup step: the mock always finds one overlapping document
  await expect(page.getByTestId('wizard-related')).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText('specs/txn-report.md')).toBeVisible();
  await expect(page.getByText('overlaps')).toBeVisible();
  await page.getByTestId('wizard-create-new').click();

  await expect(page.getByTestId('wizard-transcript')).toBeVisible({ timeout: 20_000 });
}

test('the wizard walks intent → interview → draft → editor', async ({ page, request }) => {
  const stamp = Date.now().toString(36);
  await toInterview(page, 'records must be kept for seven years, not five');

  // the interview turn arrives as structure, not prose: open questions and a
  // rubric that shows how far from draftable the document is
  await expect(page.getByTestId('wizard-questions')).toBeVisible();
  await expect(page.getByText('Does this replace the existing window?')).toBeVisible();
  const rubric = page.getByTestId('wizard-rubric');
  await expect(rubric.getByText('0/2')).toBeVisible();
  await expect(rubric.getByText('Retention period stated')).toBeVisible();

  // answering fills the rubric and unlocks the draft
  await page.getByTestId('wizard-answer').fill('Yes, seven years replaces the five-year window, for all trade records.');
  await page.getByTestId('wizard-answer').press('Enter');
  await expect(rubric.getByText('2/2')).toBeVisible({ timeout: 20_000 });
  await expect(rubric.getByText('Ready to draft')).toBeVisible();

  // the draft is written into the family's section outline
  await page.getByTestId('wizard-draft').click();
  await expect(page.getByTestId('wizard-sections')).toBeVisible({ timeout: 20_000 });
  const sections = page.getByTestId('wizard-sections');
  await expect(sections.getByRole('button', { name: /^▾?\s*Overview$/ })).toBeVisible();
  await expect(sections.getByRole('button', { name: /Open questions$/ })).toBeVisible();
  await expect(sections.getByText('ai draft').first()).toBeVisible();

  // the title drives the path preview (the spec family's IDs are title slugs)
  const title = `Wizard scratch ${stamp}`;
  await page.getByTestId('wizard-title').fill(title);
  const path = `specs/wizard-scratch-${stamp}.md`;
  await expect(page.getByTestId('wizard-path')).toContainText(path);

  // create → uncommitted draft on the workspace branch, open in the editor
  await page.getByTestId('wizard-create').click();
  await expect(page).toHaveURL(new RegExp(`/editor/${path.replace(/[.]/g, '\\.')}`), { timeout: 20_000 });

  // the invariant, asserted against the branch rather than the editor chrome:
  // a real uncommitted draft carrying the frontmatter and the drafted outline
  const ws = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: { 'X-SpecQuill': '1' }, data: {} })).json()) as { branch: string };
  const doc = (await (await request.get(`/api/repos/${REPO}/files/${path}?ref=${encodeURIComponent(ws.branch)}`)).json()) as { content: string };
  expect(doc.content).toContain(`title: ${title}`);
  expect(doc.content).toContain('## Overview');
  expect(doc.content).toContain('## Open questions');
});

test('nothing is written to the worktree until the author accepts', async ({ page, request }) => {
  await toInterview(page, 'a document I will abandon halfway through');
  await page.getByTestId('wizard-answer').fill('just draft it');
  await page.getByTestId('wizard-answer').press('Enter');
  await page.getByTestId('wizard-draft').click();
  await expect(page.getByTestId('wizard-sections')).toBeVisible({ timeout: 20_000 });

  // a full draft exists in the UI — and no file exists anywhere for it
  const ws = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: { 'X-SpecQuill': '1' }, data: {} })).json()) as { branch: string };
  const status = await (await request.get(`/api/repos/${REPO}/status?branch=${encodeURIComponent(ws.branch)}`)).json();
  const changed = JSON.stringify(status);
  expect(changed).not.toContain('mock-drafted-specification');

  // abandoning it leaves nothing behind
  await page.getByRole('button', { name: 'Start over' }).click();
  await expect(page.getByTestId('wizard-intent')).toHaveValue('');
});

test('an interrupted wizard survives a reload', async ({ page }) => {
  await toInterview(page, 'a document I will come back to');
  await expect(page.getByTestId('wizard-questions')).toBeVisible();

  await page.reload();
  // same stage, same transcript, same rubric — the wizard is client state,
  // the server holds nothing
  await expect(page.getByTestId('wizard-transcript')).toBeVisible();
  await expect(page.getByText('Does this replace the existing window?')).toBeVisible();
  await expect(page.getByTestId('wizard-rubric').getByText('0/2')).toBeVisible();
});

test('a section can be redrafted on its own', async ({ page }) => {
  await toInterview(page, 'a spec whose overview I want rewritten');
  await page.getByTestId('wizard-answer').fill('just draft it');
  await page.getByTestId('wizard-answer').press('Enter');
  await page.getByTestId('wizard-draft').click();
  await expect(page.getByTestId('wizard-sections')).toBeVisible({ timeout: 20_000 });

  await page.getByRole('button', { name: 'tighten' }).first().click();
  await expect(page.getByText('✓ rewrote it')).toBeVisible({ timeout: 20_000 });
  await expect(page.getByTestId('wizard-sections').getByText('(mock) rewritten section body.')).toBeVisible();
});
