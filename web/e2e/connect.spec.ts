import { expect, test } from '@playwright/test';
import { API, APP, H } from './helpers';

// REQ-023: a tenant admin connects a repo with a custom token at runtime.
// The token is sealed server-side, never echoed, rotatable, and deletable
// only once detached. Requires SPECQUILL_SECRET_KEY on the dev server.

test('connect a repo with a PAT — sealed, redacted, rotatable, deletable', async ({ page, request }) => {
  const probe = await request.post(`${API}/credentials`, { headers: H, data: { name: `probe-${Date.now()}`, token: 'x' } });
  test.skip(probe.status() === 501, 'SPECQUILL_SECRET_KEY not configured on the dev server');
  if (probe.ok()) {
    const { id } = (await probe.json()) as { id: number };
    await request.delete(`${API}/credentials/${id}`, { headers: H });
  }

  const stamp = Date.now();
  const id = `scratch-conn-${stamp}`;
  const secret = `dummy-pat-${stamp}-supersecret`;

  await page.goto(`${APP}/admin`);
  await page.getByPlaceholder('id').fill(id);
  await page.getByPlaceholder('remote (git url or path)').fill('./data/origin/regulations.git');
  await page.getByPlaceholder('access token (private remotes)').fill(secret);
  await page.getByRole('button', { name: 'Add project' }).click();
  await expect(page.locator(`span:text-is("${id}")`)).toBeVisible({ timeout: 15_000 });
  // the token field cleared on success; the credentials panel lists the row
  await expect(page.getByPlaceholder('access token (private remotes)')).toHaveValue('');
  await expect(page.locator(`span:text-is("${id} token")`)).toBeVisible();

  // the list is redacted — no token material anywhere in the response
  const list = await request.get(`${API}/credentials`, { headers: H });
  const body = await list.text();
  expect(body).toContain(`${id} token`);
  expect(body).not.toContain(secret);
  const cred = (JSON.parse(body) as { id: number; name: string; repoCount: number }[]).find((c) => c.name === `${id} token`)!;
  expect(cred.repoCount).toBe(1);

  // rotation replaces without display; deletion refuses while attached
  expect((await request.put(`${API}/credentials/${cred.id}`, { headers: H, data: { token: `rotated-${stamp}` } })).ok()).toBeTruthy();
  expect((await request.delete(`${API}/credentials/${cred.id}`, { headers: H })).status()).toBe(409);

  // detach, then credential and project clean up
  expect((await request.put(`${API}/repos/${id}/settings/credential`, { headers: H, data: { credentialId: 0 } })).ok()).toBeTruthy();
  expect((await request.delete(`${API}/credentials/${cred.id}`, { headers: H })).ok()).toBeTruthy();
  expect((await request.delete(`${API}/projects/${id}`, { headers: H })).ok()).toBeTruthy();
});
