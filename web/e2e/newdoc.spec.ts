// Guided document creation: family picker, subfolder, scheme-generated IDs
// with conflict detection (config `ids:` or built-ins like REQ-{seq:3}).
import { expect, test } from '@playwright/test';
import { API, APP, H } from './helpers';

test.describe.configure({ mode: 'serial' });

const REPO = 'trading-specs';

async function wsBranch(request: import('@playwright/test').APIRequestContext): Promise<string> {
  const res = await request.post(`${API}/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

// drop leftover created files from earlier runs (dialog docs are always 'A')
async function cleanWorkspace(request: import('@playwright/test').APIRequestContext) {
  const branch = await wsBranch(request);
  const st = (await (await request.get(`${API}/repos/${REPO}/status?branch=${encodeURIComponent(branch)}`)).json()) as
    { dirty: { path: string; state: string }[] };
  for (const f of st.dirty) {
    if (f.state !== 'A') continue;
    // untracked directories come collapsed ('requirements/e2e-sub-x/') —
    // expand them via the worktree tree so the subfolder test self-heals
    const paths = f.path.endsWith('/')
      ? ((await (await request.get(`${API}/repos/${REPO}/tree?ref=${encodeURIComponent(branch)}`)).json()) as
          { path: string }[]).map((e) => e.path).filter((p) => p.startsWith(f.path))
      : [f.path];
    for (const p of paths) {
      await request.delete(`${API}/repos/${REPO}/files/${p}?branch=${encodeURIComponent(branch)}`, { headers: H });
    }
  }
}

test.beforeEach(async ({ request }) => { await cleanWorkspace(request); });
test.afterEach(async ({ request }) => { await cleanWorkspace(request); });

test('guided creation generates the next sequential ID and typed frontmatter', async ({ page, request }) => {
  await page.goto(`${APP}/p/trading-specs/editor`);
  await page.getByTitle('New document').first().click();
  const dialog = page.getByTestId('newdoc-dialog');
  await expect(dialog).toBeVisible();

  // requirements family → built-in REQ-{seq:3} scheme, prefilled
  await dialog.getByRole('button', { name: /Requirements/ }).click();
  const id = await page.getByTestId('newdoc-id').inputValue();
  expect(id).toMatch(/^REQ-\d{3,}$/);

  await page.getByTestId('newdoc-title').fill('Guided creation e2e');
  await expect(dialog.getByText(`requirements/${id}.md`)).toBeVisible();

  await page.getByTestId('newdoc-create').click();
  await page.waitForURL(new RegExp(`editor/requirements/${id}\\.md`));

  const branch = await wsBranch(request);
  const file = (await (await request.get(`${API}/repos/${REPO}/files/requirements/${id}.md?ref=${encodeURIComponent(branch)}`)).json()) as { content: string };
  expect(file.content).toContain(`id: ${id}`);
  expect(file.content).toContain('type: Requirement');
  expect(file.content).toContain('title: Guided creation e2e');
});

test('conflicting IDs are detected and block creation', async ({ page }) => {
  await page.goto(`${APP}/p/trading-specs/editor`);
  await page.getByTitle('New document').first().click();
  const dialog = page.getByTestId('newdoc-dialog');
  await dialog.getByRole('button', { name: /Requirements/ }).click();

  await page.getByTestId('newdoc-id').fill('REQ-063'); // exists in the fixture
  await expect(dialog.getByText(/already taken/)).toBeVisible();
  await expect(page.getByTestId('newdoc-create')).toBeDisabled();

  // regenerate resolves the clash
  await page.getByTitle('Generate a new ID').click();
  await expect(dialog.getByText(/already taken/)).toBeHidden();
  await expect(page.getByTestId('newdoc-create')).toBeEnabled();
});

test('folder + preselects the family; slug IDs track the title until edited', async ({ page }) => {
  await page.goto(`${APP}/p/trading-specs/editor`);
  // the per-folder plus opens the dialog with that family selected
  await page.getByTitle('New document in specs/').click();
  const dialog = page.getByTestId('newdoc-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('{slug}')).toBeVisible(); // specs scheme

  // no title yet → memorable placeholder pair, not "untitled"
  const placeholder = await page.getByTestId('newdoc-id').inputValue();
  expect(placeholder).toMatch(/^[a-z]+-[a-z]+$/);
  expect(placeholder).not.toBe('untitled');

  // typing a title takes over the ID
  await page.getByTestId('newdoc-title').fill('Slug Follow Check');
  await expect(page.getByTestId('newdoc-id')).toHaveValue('slug-follow-check');
  await expect(dialog.getByText('specs/slug-follow-check.md')).toBeVisible();

  // clashing with an existing document (specs/venue.md) blocks creation
  await page.getByTestId('newdoc-title').fill('Venue');
  await expect(dialog.getByText(/already taken/)).toBeVisible();
  await expect(page.getByTestId('newdoc-create')).toBeDisabled();

  // a manual ID edit stops title tracking
  await page.getByTestId('newdoc-id').fill('venue-v2');
  await page.getByTestId('newdoc-title').fill('Renamed Again');
  await expect(page.getByTestId('newdoc-id')).toHaveValue('venue-v2');
  await expect(page.getByTestId('newdoc-create')).toBeEnabled();

  // Escape closes without creating anything
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

test('a new nested subfolder can be created inline', async ({ page, request }) => {
  const stamp = Date.now().toString(36);
  const sub = `e2e-sub-${stamp}/inner`;
  await page.goto(`${APP}/p/trading-specs/editor`);
  await page.getByTitle('New document').first().click();
  const dialog = page.getByTestId('newdoc-dialog');
  await dialog.getByRole('button', { name: /Requirements/ }).click();
  const id = await page.getByTestId('newdoc-id').inputValue();

  await dialog.locator('select').selectOption('+new');
  // slashes nest; segments are slugified independently ('Inner' → 'inner')
  await dialog.getByPlaceholder(/subfolder/).fill(`e2e-sub-${stamp}/Inner`);
  await expect(dialog.getByText(`requirements/${sub}/${id}.md`)).toBeVisible();

  await page.getByTestId('newdoc-create').click();
  await page.waitForURL(new RegExp(`editor/requirements/${sub}/${id}\\.md`));

  const branch = await wsBranch(request);
  const file = (await (await request.get(`${API}/repos/${REPO}/files/requirements/${sub}/${id}.md?ref=${encodeURIComponent(branch)}`)).json()) as { content: string };
  expect(file.content).toContain(`id: ${id}`);
});
