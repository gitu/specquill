// Properties form: categorical frontmatter fields render as comboboxes with a
// real options popup (schema values ∪ config statuses ∪ corpus values), date
// fields are read-only with a validated manual override, and the add-property
// row appends known or custom keys — writing only on a value.
import { APIRequestContext, expect, test } from '@playwright/test';

const REPO = 'trading-specs';
const H = { 'X-SpecQuill': '1' };
// nested on purpose: frontmatter refs are workspace-root-relative, and only
// a doc inside a folder catches an opener that resolves against the doc dir
const DOC = `requirements/scratch-props-${Date.now().toString(36)}.md`;
const BODY = '---\ntitle: Props scratch\nstatus: draft\nupdated: 2026-05-30\nanchors: [props-a]\ndrivers:\n  - type: regulatory\n    ref: regulations/mifid-ii.md#rts-22-art-26\n---\n# Props scratch\n\n## Props anchor {#props-a}\n\n## Retention Rules\n\nbody\n';

async function wsBranch(request: APIRequestContext): Promise<string> {
  const res = await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

async function fileContent(request: APIRequestContext, branch: string): Promise<string> {
  const res = await request.get(`/api/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(branch)}`);
  return ((await res.json()) as { content: string }).content;
}

test('combobox popups offer options, dates are read-only, add/remove stays byte-identical', async ({ page, request }) => {
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

  // clicking the pre-filled status field must still offer every schema value
  // (the old datalist filtered against "draft" and showed nothing)
  const status = page.getByRole('combobox', { name: 'status' });
  await expect(status).toHaveValue('draft');
  await status.click();
  await expect(page.getByRole('option', { name: 'in_review' })).toBeVisible();
  await expect(page.getByRole('option', { name: 'approved' })).toBeVisible();
  await page.keyboard.press('Escape'); // closes without writing
  await expect(page.getByRole('listbox')).toHaveCount(0);

  // updated (date-typed) renders read-only — no input until overridden
  await expect(page.getByText('2026-05-30')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'updated' })).toHaveCount(0);

  // "+ add property" opens the key selector immediately; picking an enum key
  // mounts its value dropdown with the schema options already offered
  await page.getByRole('button', { name: '+ add property' }).click();
  await expect(page.getByRole('option', { name: 'business_value' })).toBeVisible();
  await page.getByRole('option', { name: 'business_value' }).click();
  await expect(page.getByRole('combobox', { name: 'value for business_value' })).toBeFocused();
  await expect(page.getByRole('option', { name: 'high' })).toBeVisible();
  await page.keyboard.press('Escape'); // cancel — writes nothing
  await expect(page.getByRole('button', { name: '+ add property' })).toBeVisible();

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

  // picking an option from the popup writes it
  await status.click();
  await page.getByRole('option', { name: 'in_review' }).click();
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 })
    .toContain('status: in_review');

  // the date override only ever commits a valid YYYY-MM-DD
  await page.getByRole('button', { name: 'override updated' }).click();
  const date = page.getByRole('textbox', { name: 'updated' });
  await date.fill('not-a-date');
  await page.keyboard.press('Enter'); // rejected — the editor stays open
  await expect(date).toBeVisible();
  await date.fill('2026-07-28');
  await page.keyboard.press('Enter');
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 })
    .toContain('updated: 2026-07-28');

  // the driver ref is a search-picker over workspace docs AND their anchors,
  // with folder facet chips and an anchors-only toggle
  const driverRef = page.getByRole('combobox', { name: 'driver ref' });
  await driverRef.click();
  await expect(page.getByRole('option', { name: /regulations\/gdpr\.md#art-17-erasure/ })).toBeVisible();
  await page.getByRole('button', { name: 'filter products' }).click();
  await expect(page.getByRole('option', { name: /products\/ops-t1-settlement-sla\.md/ })).toBeVisible();
  await expect(page.getByRole('option', { name: /regulations\/gdpr/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'filter regulations' }).click();
  await page.getByRole('button', { name: 'filter # anchors' }).click();
  const anchorOnly = await page.locator('[role=option]').allTextContents();
  expect(anchorOnly.length).toBeGreaterThan(0);
  for (const t of anchorOnly) expect(t).toContain('#');
  await page.getByRole('option', { name: /regulations\/gdpr\.md#art-17-erasure/ }).click();
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 })
    .toContain('ref: regulations/gdpr.md#art-17-erasure');

  // the anchors list offers the document's own headings as ids — minus the
  // ones already listed (props-a is present in the frontmatter)
  await page.getByRole('combobox', { name: 'add anchors' }).click();
  await expect(page.getByRole('option', { name: /retention-rules/ })).toBeVisible();
  await expect(page.getByRole('option', { name: /props-a/ })).toHaveCount(0);
  await page.getByRole('option', { name: /retention-rules/ }).click();
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 })
    .toContain('retention-rules');

  // opening a frontmatter ref must NOT resolve against the doc's folder
  // (this doc lives in requirements/ — a relative resolve would 404 on
  // requirements/regulations/gdpr.md)
  await page.getByTitle('open regulations/gdpr.md').click();
  await expect(page).toHaveURL(/\/editor\/regulations\/gdpr\.md$/);

  await request.delete(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H });
});

test('badge-typed schema fields (product repo) get the combobox too', async ({ page }) => {
  await page.goto('/p/specquill-docs/editor/requirements/REQ-001.md');
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });

  // status is typed "badge" with a values map in this repo's schema.json —
  // values-map detection must not depend on the literal type "enum"
  const status = page.getByRole('combobox', { name: 'status' });
  await expect(status).toHaveValue('approved');
  await status.click();
  await expect(page.getByRole('option', { name: 'in_review' })).toBeVisible();
  await page.keyboard.press('Escape');
});
