// Real-time co-editing: two browser contexts on the same branch + file.
// Dev auto-auth signs both in as the same dev user (separate identities need
// real sessions), which still exercises the full CRDT/relay path; co-author
// trailers are asserted API-level with a second local user in the Go suite.
import { Browser, BrowserContext, Page, expect, test } from '@playwright/test';

const REPO = 'trading-specs';
const H = { 'X-Reqbase': '1' };
const DOC = 'requirements/REQ-095.md';

async function openEditor(page: Page, branch: string) {
  await page.goto(`/#/editor/${DOC}`);
  await expect(page.getByText('ICT Incident Reporting').first()).toBeVisible();
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).click();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.locator('.milkdown-editable')).toBeVisible({ timeout: 15_000 });
}

test('two browsers co-edit the same document live', async ({ page, request, browser }) => {
  // workspace branch for the session
  const ws = ((await (await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} })).json()) as { branch: string }).branch;

  const ctxB: BrowserContext = await browser.newContext();
  const b: Page = await ctxB.newPage();
  try {
  await openEditor(page, ws);
  await openEditor(b, ws);

  // both show the live indicator and two participants
  await expect(page.getByText('live').or(page.getByText('Saved ✓')).first()).toBeVisible({ timeout: 15_000 });
  await expect(b.getByText(/co-editing live|live/).first()).toBeVisible({ timeout: 15_000 });

  // A types → B sees it
  const stamp = Date.now().toString(36);
  await page.locator('.milkdown-editable').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(` collab-${stamp}-alpha`);
  await expect(b.locator('.milkdown-editable')).toContainText(`collab-${stamp}-alpha`, { timeout: 10_000 });

  // B types → A sees it (click a paragraph start, away from A's cursor widget)
  await b.locator('.milkdown-editable p').first().click({ position: { x: 5, y: 5 } });
  await b.keyboard.press('Control+Home');
  await b.keyboard.type(`collab-${stamp}-beta `);
  await expect(b.locator('.milkdown-editable')).toContainText(`collab-${stamp}-beta`, { timeout: 5_000 });
  await expect(page.locator('.milkdown-editable')).toContainText(`collab-${stamp}-beta`, { timeout: 10_000 });

  // the leader flush persists the merged doc to the branch worktree
  await expect(page.getByText('Saved ✓')).toBeVisible({ timeout: 15_000 });
  await expect
    .poll(async () => {
      const f = (await (await request.get(`/api/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(ws)}`)).json()) as { content: string };
      return f.content.includes(`collab-${stamp}-alpha`) && f.content.includes(`collab-${stamp}-beta`);
    }, { timeout: 15_000 })
    .toBe(true);

  // direct PUTs are refused while the room is live
  const put = await request.put(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(ws)}`, {
    headers: H, data: { content: 'x', baseSha: '' },
  });
  expect(put.status()).toBe(409);
  expect(((await put.json()) as { code: string }).code).toBe('room_active');

  // presence reports the room
  const presence = (await (await request.get(`/api/repos/${REPO}/presence`)).json()) as { path: string; users: unknown[] }[];
  const room = presence.find((p) => p.path === DOC);
  expect(room).toBeTruthy();
  expect(room!.users.length).toBe(2);

  // restore the doc for other tests
  await b.goto('/#/dashboard');
  await page.goto('/#/dashboard');
  // live rooms only — orphaned rooms (unflushed logs) linger by design
  await expect
    .poll(async () => {
      const rooms = (await (await request.get(`/api/repos/${REPO}/presence`)).json()) as { users: unknown[] }[];
      return rooms.filter((r) => r.users.length > 0).length;
    }, { timeout: 20_000 })
    .toBe(0);
  const head = (await (await request.get(`/api/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(ws)}&at=head`)).json()) as { content: string };
  const cur = (await (await request.get(`/api/repos/${REPO}/files/${DOC}?ref=${encodeURIComponent(ws)}`)).json()) as { sha: string };
  await request.put(`/api/repos/${REPO}/files/${DOC}?branch=${encodeURIComponent(ws)}`, {
    headers: H, data: { content: head.content, baseSha: cur.sha },
  });
  } finally {
    await ctxB.close();
  }
});
