// Sketches as *.excalidraw.png: PNG files with the scene embedded — render
// natively as images, still editable in the excalidraw modal.
import { APIRequestContext, expect, test } from '@playwright/test';
import { API, APP, H } from './helpers';

const REPO = 'trading-specs';
const DOC = `scratch-sketch-${Date.now().toString(36)}.md`;
const SLUG = `e2esketch-${Date.now().toString(36)}`;

async function wsBranch(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${API}/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

test('sketch png: draw, save, native render, reopen with scene', async ({ page, request }) => {
  const branch = await wsBranch(request);
  await request.put(`${API}/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, {
    headers: H, data: { content: '# Sketch scratch\n\nsome text here\n', baseSha: '' },
  });

  await page.goto(`${APP}/p/trading-specs/editor/${DOC}`);
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).first().click();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });
  await expect(page.locator('[data-sync]')).toBeVisible({ timeout: 10_000 });

  // Sketch button → name prompt → modal opens on a blank canvas
  page.once('dialog', (d) => void d.accept(SLUG));
  await page.getByRole('button', { name: 'Sketch' }).click();
  const canvas = page.locator('.excalidraw__canvas').first();
  await expect(canvas).toBeVisible({ timeout: 20_000 });

  // draw a rectangle
  await page.keyboard.press('r');
  const box = (await canvas.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2 - 60, box.y + box.height / 2 - 40);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 60, box.y + box.height / 2 + 40, { steps: 5 });
  await page.mouse.up();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  // the modal closes once the PNG is persisted
  await expect(page.locator('.excalidraw__canvas')).toHaveCount(0, { timeout: 15_000 });

  // the file is a real PNG on the branch
  const raw = await request.get(`${API}/repos/${REPO}/raw/diagrams/${SLUG}.excalidraw.png?ref=${encodeURIComponent(branch)}`);
  expect(raw.ok()).toBe(true);
  expect(raw.headers()['content-type']).toBe('image/png');
  expect((await raw.body()).subarray(0, 4).toString('hex')).toBe('89504e47');

  // …and renders natively as an image in the editor
  const img = page.locator(`.milkdown-editable img[src*="${SLUG}.excalidraw.png"]`).first();
  await expect(img).toBeVisible({ timeout: 10_000 });

  // clicking the image reopens the editor with the embedded scene (no error)
  await img.click();
  await expect(page.locator('.excalidraw__canvas').first()).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText(/load failed|Unsupported/)).toHaveCount(0);
  await page.getByRole('button', { name: 'Close', exact: true }).click();

  // view mode shows it as a plain image too
  await page.getByText('View', { exact: true }).click();
  await expect(page.locator(`#specquill-doc img[src*="${SLUG}.excalidraw.png"]`).first()).toBeVisible({ timeout: 10_000 });

  // cleanup
  await page.goto(`${APP}/p/trading-specs/dashboard`);
  await expect
    .poll(async () => {
      const rooms = (await (await request.get(`${API}/repos/${REPO}/presence`)).json()) as { users: unknown[] }[];
      return rooms.filter((r) => r.users.length > 0).length;
    }, { timeout: 20_000 })
    .toBe(0);
  await request.delete(`${API}/repos/${REPO}/files/diagrams/${SLUG}.excalidraw.png?branch=${encodeURIComponent(branch)}`, { headers: H });
  await request.delete(`${API}/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(branch)}`, { headers: H });
});
