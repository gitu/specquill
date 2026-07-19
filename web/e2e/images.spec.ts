// Image upload/paste + inline formatting.
import { APIRequestContext, expect, test } from '@playwright/test';
import { API, APP, H } from './helpers';

const REPO = 'trading-specs';
const DOC = 'requirements/REQ-051.md';
// 1x1 red PNG
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

async function wsBranch(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${API}/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

test('asset upload round-trips through the raw endpoint', async ({ request }) => {
  const branch = await wsBranch(request);
  const up = await request.post(`${API}/repos/${REPO}/assets?branch=${encodeURIComponent(branch)}&dir=requirements/assets`, {
    headers: H,
    multipart: { file: { name: 'e2e dot.png', mimeType: 'image/png', buffer: PNG } },
  });
  expect(up.ok()).toBe(true);
  const { path } = (await up.json()) as { path: string };
  expect(path).toMatch(/^requirements\/assets\/e2e-dot(-\d+)?\.png$/);

  const raw = await request.get(`${API}/repos/${REPO}/raw/${path}?ref=${encodeURIComponent(branch)}`);
  expect(raw.ok()).toBe(true);
  expect(raw.headers()['content-type']).toBe('image/png');
  expect(Buffer.compare(await raw.body(), PNG)).toBe(0);

  // uploads to the protected default branch are refused
  const denied = await request.post(`${API}/repos/${REPO}/assets?branch=main&dir=assets`, {
    headers: H,
    multipart: { file: { name: 'nope.png', mimeType: 'image/png', buffer: PNG } },
  });
  expect(denied.status()).toBe(403);
  // non-image types are refused
  const badType = await request.post(`${API}/repos/${REPO}/assets?branch=${encodeURIComponent(branch)}&dir=assets`, {
    headers: H,
    multipart: { file: { name: 'evil.html', mimeType: 'text/html', buffer: Buffer.from('<script/>') } },
  });
  expect(badType.status()).toBe(400);

  await request.delete(`${API}/repos/${REPO}/files/${path}?branch=${encodeURIComponent(branch)}`, { headers: H });
});

async function restoreDoc(request: APIRequestContext, branch: string) {
  // wait for live rooms only — orphaned rooms (unflushed logs) linger by design
  await expect
    .poll(async () => {
      const rooms = (await (await request.get(`${API}/repos/${REPO}/presence`)).json()) as { users: unknown[] }[];
      return rooms.filter((r) => r.users.length > 0).length;
    }, { timeout: 20_000 })
    .toBe(0);
  const head = (await (await request.get(`${API}/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(branch)}&at=head`)).json()) as { content: string };
  const cur = (await (await request.get(`${API}/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(branch)}`)).json()) as { sha: string };
  await request.put(`${API}/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, {
    headers: H, data: { content: head.content, baseSha: cur.sha },
  });
  const tree = (await (await request.get(`${API}/repos/${REPO}/tree?ref=${encodeURIComponent(branch)}`)).json()) as { path: string }[];
  for (const e of tree.filter((t) => /^requirements\/assets\/shot.*\.png$/.test(t.path))) {
    await request.delete(`${API}/repos/${REPO}/files/${e.path}?branch=${encodeURIComponent(branch)}`, { headers: H });
  }
}

test('editor: upload button embeds an image; bold toolbar formats text', async ({ page, request }) => {
  const branch = await wsBranch(request);
  await restoreDoc(request, branch); // clean slate (prior runs may have left embeds)
  await page.goto(`${APP}/p/trading-specs/editor/${DOC}`);
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });

  // upload via the toolbar picker → editor shows the raw-endpoint image
  await page.locator('input[type="file"]').setInputFiles({ name: 'shot.png', mimeType: 'image/png', buffer: PNG });
  const img = page.locator(`.milkdown-editable img[src*="/raw/requirements/assets/shot"]`).first();
  await expect(img).toBeVisible({ timeout: 10_000 });

  // bold: type, select the word, hit the B button (wait for the session
  // toolbar cluster to render first — it shifts the buttons once)
  await expect(page.locator('[data-sync]')).toBeVisible({ timeout: 10_000 });
  await page.locator('.milkdown-editable').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(' boldcheck');
  for (let i = 0; i < 'boldcheck'.length; i++) await page.keyboard.press('Shift+ArrowLeft');
  await page.getByTitle('Bold (Ctrl+B)').click();
  await expect(page.locator('.milkdown-editable strong', { hasText: 'boldcheck' })).toBeVisible();

  // view mode renders the embedded image through the raw endpoint too
  await page.getByText('View', { exact: true }).click();
  await expect(page.locator('#specquill-doc img[src*="/raw/requirements/assets/shot"]').first()).toBeVisible({ timeout: 10_000 });

  // restore: close the room, put the committed content back, drop the asset
  await page.goto(`${APP}/p/trading-specs/dashboard`);
  await restoreDoc(request, branch);
});
