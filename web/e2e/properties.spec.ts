// Properties form: categorical frontmatter fields render as comboboxes backed
// by datalists (schema values ∪ config statuses ∪ corpus values), and the
// add-property row appends known or custom keys — writing only on a value.
import { APIRequestContext, expect, test } from '@playwright/test';

const REPO = 'trading-specs';
const H = { 'X-SpecQuill': '1' };
const DOC = `scratch-props-${Date.now().toString(36)}.md`;
const BODY = '---\ntitle: Props scratch\nstatus: draft\n---\n# Props scratch\n\nbody\n';

async function wsBranch(request: APIRequestContext): Promise<string> {
  const res = await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

async function fileContent(request: APIRequestContext, branch: string): Promise<string> {
  const res = await request.get(`/api/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(branch)}`);
  return ((await res.json()) as { content: string }).content;
}

test('categorical fields are comboboxes; add row writes, remove restores byte-identically', async ({ page, request }) => {
  const branch = await wsBranch(request);
  await request.delete(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H }).catch(() => {});
  await request.put(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, {
    headers: H, data: { content: BODY, baseSha: '' },
  });

  await page.goto(`/p/trading-specs/editor/${DOC}`);
  // the scratch doc lives on the workspace branch only
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).click();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });

  // status (schema enum) renders as a datalist-backed combobox with the
  // schema's values offered
  const status = page.locator('input[list="fmopt-status"]');
  await expect(status).toHaveValue('draft');
  const options = await page.locator('datalist#fmopt-status option')
    .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
  expect(options).toContain('in_review');
  expect(options).toContain('approved');

  // add a known property from the schema/corpus key pool, then provide a value
  await page.getByRole('button', { name: '+ add property' }).click();
  await page.keyboard.type('jurisdiction');
  await page.keyboard.press('Enter');
  await page.keyboard.type('EU');
  await page.keyboard.press('Enter');
  await expect(page.getByText('Jurisdiction', { exact: true })).toBeVisible();
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 })
    .toContain('jurisdiction: EU');

  // removing it restores the document byte-identically (nothing else was touched)
  await page.getByTitle('remove jurisdiction').click();
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 }).toBe(BODY);

  await request.delete(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H });
});

test('badge-typed schema fields (product repo) get the combobox too', async ({ page }) => {
  await page.goto('/p/specquill-docs/editor/requirements/REQ-001.md');
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });

  // status is typed "badge" with a values map in this repo's schema.json —
  // values-map detection must not depend on the literal type "enum"
  const status = page.locator('input[list="fmopt-status"]');
  await expect(status).toHaveValue('approved');
  const options = await page.locator('datalist#fmopt-status option')
    .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
  expect(options).toContain('in_review');
});
