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
// local YYYY-MM-DD, same as the SPA's todayStr (property edits auto-bump `updated`)
const TODAY = (() => { const d = new Date(), p = (n: number) => String(n).padStart(2, '0'); return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`; })();

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

  // removing it restores the document byte-identically — except `updated`,
  // which the property edits auto-bumped to today
  await page.getByTitle('remove jurisdiction').click();
  await expect.poll(() => fileContent(request, branch), { timeout: 15_000 })
    .toBe(BODY.replace('updated: 2026-05-30', `updated: ${TODAY}`));

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

  // drivers edit as a flat path list: the add-picker searches workspace docs
  // AND their anchors, with folder facet chips and an anchors-only toggle.
  // Adding an entry rewrites the legacy {type, ref} block to the flat form.
  const addDriver = page.getByRole('combobox', { name: 'add drivers' });
  await addDriver.click();
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
    .toContain('- regulations/gdpr.md#art-17-erasure');

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
  // .first(): the collapsed link-debug panel carries the same open-title
  await page.getByTitle('open regulations/gdpr.md').first().click();
  await expect(page).toHaveURL(/\/editor\/regulations\/gdpr\.md$/);

  await request.delete(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H });
});

test('documents show computed backlinks: drivers, typed relations, text mentions', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/regulations/gdpr.md');
  // the panel sits outside the Properties box and is marked computed
  await expect(page.getByText('· computed from links to this document — not stored in it')).toBeVisible();
  await page.getByText('REQ-090', { exact: true }).first().click();
  await expect(page).toHaveURL(/editor\/requirements\/REQ-090\.md/);

  // the chain reads upward now: a REQUIREMENT collects implements-backlinks
  // from the specs that realize it — shown with the target-side inverse name
  await page.goto('/p/trading-specs/editor/requirements/REQ-042.md');
  await expect(page.getByText('· computed from links to this document — not stored in it')).toBeVisible();
  await expect(page.getByText('implemented by', { exact: true }).first()).toBeVisible();

  // view-mode driver chips render the FLAT driver list as linked doc chips
  // with the derived type (not raw path text)
  await expect(page.getByTitle('open regulations/mifid-ii.md').first()).toBeVisible();
  await expect(page.getByTitle('open products/ops-t1-settlement-sla.md').first()).toBeVisible();
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
