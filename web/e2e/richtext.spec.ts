// Rich editing: slash menu, selection toolbar, link dialog, outline.
import { APIRequestContext, expect, test } from '@playwright/test';

const REPO = 'trading-specs';
const H = { 'X-SpecQuill': '1' };
const DOC = `scratch-rich-${Date.now().toString(36)}.md`;
const BODY = '# Rich scratch\n\nintro paragraph words here\n\n## Section two\n\nmore text\n\n## Section three\n\ntail\n';

async function wsBranch(request: APIRequestContext): Promise<string> {
  const res = await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

test('slash menu, selection toolbar, link dialog and outline', async ({ page, request }) => {
  const branch = await wsBranch(request);
  await request.delete(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H }).catch(() => {});
  await request.put(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, {
    headers: H, data: { content: BODY, baseSha: '' },
  });

  await page.goto(`/#/editor/${DOC}`);
  // the scratch doc lives on the workspace branch only
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).click();
  // outline chip expands to the heading list in view mode; clicking jumps
  await expect(page.locator('[data-outline]')).toBeVisible({ timeout: 10_000 });
  await page.locator('[data-outline]').click();
  await expect(page.locator('[data-outline-list]').getByText('Section two')).toBeVisible();
  await page.locator('[data-outline-list]').getByText('Section three').click();
  await page.locator('[data-outline]').click();

  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });
  await expect(page.locator('[data-sync]')).toBeVisible({ timeout: 10_000 });

  // slash menu → insert a table (anchor the cursor in a concrete paragraph;
  // the settle waits let PM process the click before synthetic keys arrive)
  const tail = page.locator('.milkdown-editable p', { hasText: 'tail' }).first();
  await tail.click();
  await page.waitForTimeout(150);
  await page.keyboard.press('End');
  await page.keyboard.press('Enter');
  await page.keyboard.type('/tab');
  await expect(page.locator('.slash-menu .slash-item', { hasText: 'Table' })).toBeVisible({ timeout: 5_000 });
  await page.keyboard.press('Enter');
  await expect(page.locator('.milkdown-editable table').first()).toBeVisible({ timeout: 5_000 });

  // selection toolbar → bold
  const intro = page.locator('.milkdown-editable p', { hasText: 'intro paragraph' }).first();
  await intro.click();
  await page.waitForTimeout(150);
  await page.keyboard.press('Home');
  for (let i = 0; i < 'intro'.length; i++) await page.keyboard.press('Shift+ArrowRight');
  await expect(page.locator('.sel-toolbar')).toBeVisible({ timeout: 5_000 });
  await page.locator('.sel-toolbar .fmt-b').click();
  await expect(page.locator('.milkdown-editable strong', { hasText: 'intro' })).toBeVisible();

  // Ctrl+K → link edit input → confirm
  const words = page.locator('.milkdown-editable p', { hasText: 'paragraph words' }).first();
  await words.click();
  await page.waitForTimeout(150);
  await page.keyboard.press('End');
  for (let i = 0; i < 'here'.length; i++) await page.keyboard.press('Shift+ArrowLeft');
  // the selection toolbar appearing proves PM registered the selection
  await expect(page.locator('.sel-toolbar')).toBeVisible({ timeout: 5_000 });
  await page.keyboard.press('Control+k');
  const linkInput = page.locator('.milkdown-link-edit input');
  await expect(linkInput).toBeVisible({ timeout: 5_000 });
  await linkInput.fill('https://example.com');
  await linkInput.press('Enter');
  await expect(page.locator('.milkdown-editable a[href="https://example.com"]')).toBeVisible({ timeout: 5_000 });

  // outline chip is present in edit mode too (jump behavior covered above;
  // the table widget's floating handles can overlap the chip mid-doc)
  await expect(page.locator('[data-outline]')).toBeVisible();

  // cleanup: close the room, delete the scratch file
  await page.goto('/#/dashboard');
  await expect
    .poll(async () => {
      const rooms = (await (await request.get(`/api/repos/${REPO}/presence`)).json()) as { users: unknown[] }[];
      return rooms.filter((r) => r.users.length > 0).length;
    }, { timeout: 20_000 })
    .toBe(0);
  await request.delete(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H });
});
