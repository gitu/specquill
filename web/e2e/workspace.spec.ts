// Phase-1 collaboration model: protected main, auto personal workspaces,
// durable autosaved drafts, change visualization, sync banner.
import { expect, test } from '@playwright/test';

test.describe.configure({ mode: 'serial' });

const REPO = 'trading-specs';
const H = { 'X-SpecQuill': '1' };

async function wsBranch(request: import('@playwright/test').APIRequestContext): Promise<string> {
  const res = await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} });
  return ((await res.json()) as { branch: string }).branch;
}

async function cleanWorkspace(request: import('@playwright/test').APIRequestContext) {
  const branch = await wsBranch(request);
  const st = (await (await request.get(`/api/repos/${REPO}/status?branch=${encodeURIComponent(branch)}`)).json()) as
    { dirty: { path: string; state: string }[] };
  for (const f of st.dirty) {
    if (f.state === 'A') {
      // untracked directories come collapsed ('requirements/assets/') —
      // expand them via the worktree tree so leftovers from aborted runs
      // (e.g. a failed images test) actually get removed
      const paths = f.path.endsWith('/')
        ? ((await (await request.get(`/api/repos/${REPO}/tree?ref=${encodeURIComponent(branch)}`)).json()) as
            { path: string }[]).map((e) => e.path).filter((p) => p.startsWith(f.path))
        : [f.path];
      for (const p of paths) {
        await request.delete(`/api/repos/${REPO}/files/${p}?branch=${encodeURIComponent(branch)}`, { headers: H });
      }
    } else {
      const head = (await (await request.get(`/api/repos/${REPO}/files/${f.path}?ref=${encodeURIComponent(branch)}&at=head`)).json()) as { content: string };
      const cur = (await (await request.get(`/api/repos/${REPO}/files/${f.path}?ref=${encodeURIComponent(branch)}`)).json()) as { sha: string };
      await request.put(`/api/repos/${REPO}/files/${f.path}?branch=${encodeURIComponent(branch)}`, {
        headers: H, data: { content: head.content, baseSha: cur.sha },
      });
    }
  }
  return branch;
}

test.beforeEach(async ({ request }) => { await cleanWorkspace(request); });

test('editing on protected main auto-switches to the personal workspace', async ({ page }) => {
  await page.goto('/p/trading-specs/editor/specs/venue.md');
  await expect(page.getByText('Venue Identification').first()).toBeVisible();
  // main shows the protection lock
  await expect(page.locator('header').getByTitle(/protected/)).toBeVisible();

  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByText(/you're now on your workspace ws\//)).toBeVisible();
  await expect(page.locator('header').getByText(/^ws\//)).toBeVisible();

  // type → autosave lands on the workspace worktree
  await page.locator('.milkdown-editable').click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('Workspace autosave check.');
  await expect(page.locator('[data-sync="saved"]')).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText('1 change', { exact: false }).first()).toBeVisible();
});

test('direct API writes to main are rejected', async ({ request }) => {
  const res = await request.put(`/api/repos/${REPO}/files/specs/venue.md?branch=main`, {
    headers: H, data: { content: 'nope', baseSha: '' },
  });
  expect(res.status()).toBe(403);
  expect(((await res.json()) as { code: string }).code).toBe('protected_branch');
});

test('drafts survive navigation away and back', async ({ page, request }) => {
  const branch = await wsBranch(request);
  await page.goto('/p/trading-specs/editor/requirements/REQ-063.md');
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.locator('.milkdown-editable').click();
  await page.keyboard.press('Control+End');
  const marker = `durable-${Date.now().toString(36)}`;
  await page.keyboard.type(marker);

  // navigate away immediately (before the 1.5s debounce) — the blocker flushes
  await page.getByTitle('Overview').click();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await page.goto('/p/trading-specs/editor/requirements/REQ-063.md');
  await expect(page.getByText(marker)).toBeVisible({ timeout: 10_000 });

  // and it is on the workspace worktree server-side
  const file = (await (await request.get(`/api/repos/${REPO}/files/requirements/REQ-063.md?ref=${encodeURIComponent(branch)}`)).json()) as { content: string };
  expect(file.content).toContain(marker);
});

test('changes drawer shows the uncommitted diff', async ({ page, request }) => {
  const branch = await wsBranch(request);
  const cur = (await (await request.get(`/api/repos/${REPO}/files/specs/venue.md?ref=${encodeURIComponent(branch)}`)).json()) as { content: string; sha: string };
  await request.put(`/api/repos/${REPO}/files/specs/venue.md?branch=${encodeURIComponent(branch)}`, {
    headers: H, data: { content: cur.content + '\nDrawer marker line.\n', baseSha: cur.sha },
  });

  await page.goto('/p/trading-specs/editor/specs/venue.md');
  // switch onto the workspace to see its status
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).click();
  await page.getByText('1 change', { exact: false }).first().click();
  await expect(page.getByText('Uncommitted changes')).toBeVisible();
  await expect(page.getByText('Drawer marker line.')).toBeVisible();
});

test('sync banner offers a workspace update after main moves', async ({ page, request }) => {
  const branch = await wsBranch(request);
  const stamp = Date.now().toString(36);

  // the workspace may carry real commits (diverged) — land them through the
  // normal PR flow first so the update below can fast-forward
  const st = (await (await request.get(`/api/repos/${REPO}/status?branch=${encodeURIComponent(branch)}`)).json()) as { ahead: number };
  const ws0 = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} })).json()) as { state: string };
  if (ws0.state === 'diverged' || ws0.state === 'ahead') {
    const pr0 = (await (await request.post(`/api/repos/${REPO}/prs`, {
      headers: H, data: { title: `land workspace ${stamp}`, source: branch },
    })).json()) as { number: number };
    const merged = await request.post(`/api/repos/${REPO}/prs/${pr0.number}/merge`, { headers: H, data: {} });
    test.skip(!merged.ok(), 'workspace commits conflict with main — cannot set up an ff-able workspace');
  }
  void st;

  // move main via a feature branch + PR merge (main itself is protected)
  const fb = `feature/banner-${stamp}`;
  await request.post(`/api/repos/${REPO}/branches`, { headers: H, data: { name: fb, from: 'main' } });
  const f = (await (await request.get(`/api/repos/${REPO}/files/notes-banner-${stamp}.md?ref=${fb}`)).json()) as { error?: string };
  void f;
  await request.put(`/api/repos/${REPO}/files/notes-banner-${stamp}.md?branch=${fb}`, {
    headers: H, data: { content: `# banner ${stamp}\n`, baseSha: '' },
  });
  await request.post(`/api/repos/${REPO}/commit?branch=${fb}`, { headers: H, data: { message: `banner ${stamp}` } });
  const pr = (await (await request.post(`/api/repos/${REPO}/prs`, { headers: H, data: { title: `banner ${stamp}`, source: fb } })).json()) as { number: number };
  await request.post(`/api/repos/${REPO}/prs/${pr.number}/merge`, { headers: H, data: {} });

  // sit on the (now stale) workspace → banner appears → update clears it
  await page.goto('/p/trading-specs/editor/specs/venue.md');
  await page.locator('header').getByText('main', { exact: true }).first().click();
  await page.getByText(branch, { exact: true }).click();
  await expect(page.locator('[data-banner]')).toBeVisible({ timeout: 20_000 });
  // the ff is withheld while a co-editing room is still live (5s grace after
  // the previous test) — wait for it to close
  await expect
    .poll(async () => {
      const rooms = (await (await request.get(`/api/repos/${REPO}/presence`)).json()) as { users: unknown[] }[];
      return rooms.filter((r) => r.users.length > 0).length;
    }, { timeout: 20_000 })
    .toBe(0);
  await page.getByRole('button', { name: 'Update workspace' }).click();
  await expect(page.locator('[data-banner]')).toBeHidden({ timeout: 10_000 });
});
