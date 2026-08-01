// Source-drift e2e — needs the dev server, scripts/mock-llm.py (:8991) AND
// scripts/mock-forge.py (:8992, plays the dev-board work-item target).
import { expect, test, APIRequestContext } from '@playwright/test';

const H = { 'X-SpecQuill': '1' };
const REPO = 'trading-specs';

// Runs write (their report, drafts, remedies, backlinks), so starting one on
// the protected main moves the user onto their workspace branch and keys the
// run, its findings and its report there. API assertions follow that branch.
async function wsBranch(request: APIRequestContext) {
  const ws = (await (await request.post(`/api/repos/${REPO}/workspace`, { headers: H, data: {} })).json()) as { branch: string };
  return ws.branch;
}
const q = (b: string) => encodeURIComponent(b);

test.beforeEach(async ({ request }) => {
  const info = await request.get('/api/speccy/info');
  const body = (await info.json()) as { enabled: boolean; model?: string };
  test.skip(!body.enabled || body.model !== 'mock-1', 'speccy not running against mock-llm');
});

// findings persist in the store across suite runs — reopen everything (which
// also clears stale draft pointers) and drop leftover reverse-engineered
// drafts so each test starts from a deterministic slate (self-heal)
async function resetFindings(request: APIRequestContext) {
  const branch = await wsBranch(request);
  const ws = { branch };
  const drift = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as {
    findings?: { fingerprint: string; draftPath: string; remedyPath: string }[];
  };
  for (const f of drift.findings ?? []) {
    for (const p of [f.draftPath, f.remedyPath]) {
      if (p) await request.delete(`/api/repos/${REPO}/files/${p}?branch=${q(ws.branch)}`, { headers: H });
    }
    await request.post(`/api/repos/${REPO}/drift/findings/${f.fingerprint}/dismiss?branch=${q(branch)}`, {
      headers: H, data: { reopen: true },
    });
  }
  // the mock's document paths are deterministic: delete them unconditionally,
  // since a pointer cleared without its file would otherwise 409 the next
  // draft/remedy ("already exists")
  for (const p of ['requirements/REQ-gdpr-retention.md', 'work-items/WI-timestamp-precision.md',
    'changes/2026-08-timestamp-precision.md']) {
    await request.delete(`/api/repos/${REPO}/files/${p}?branch=${q(branch)}`, { headers: H });
  }
  // a previous run's report (and any remedy link written into a doc) are
  // worktree saves on ws — drop them too
  await request.post(`/api/repos/${REPO}/discard?branch=${q(ws.branch)}`, {
    headers: H, data: { paths: ['reports/source-alignment.md', 'specs/txn-report.md', 'specs/venue.md'] },
  });
}

test('scoped drift run verifies findings, files a work item and backlinks the doc', async ({ page, request }) => {
  let forgeUp = false;
  try {
    forgeUp = (await request.get('http://127.0.0.1:8992/api/v4/projects/acme%2Fbackend/issues')).ok();
  } catch { /* not running */ }
  test.skip(!forgeUp, 'mock-forge not running (drift filing target)');
  await resetFindings(request);

  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  // scope the run to specs/ — index.md is generated and stays out — and pin
  // the report target explicitly (the picker defaults to the LAST run's
  // report, which another session may have pointed elsewhere)
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  await expect(page.getByText(/2 docs in scope/)).toBeVisible();
  await page.locator('input[list="drift-report-docs"]').fill('reports/source-alignment.md');
  await page.getByRole('button', { name: 'Check drift' }).click();

  // the mock reports one finding per doc, evidence quoting the regulations
  // source verbatim (unverifiable quotes would be dropped server-side)
  await expect(page.getByText(/timestamp precision drifted/).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText('vs ~regulations').first()).toBeVisible();

  // file the first finding on the dev-board target (mock forge)
  await page.getByRole('button', { name: 'File issue' }).first().click();
  await expect(page.getByText(/work item filed/).first()).toBeVisible({ timeout: 15_000 });

  // the filing wrote a work-items backlink into the doc's frontmatter as an
  // uncommitted save on the workspace branch (main is protected in dev)
  const ws = { branch: await wsBranch(request) };
  const drift = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    findings: { status: string; docPath: string; workItemUrl: string }[];
  };
  const filed = drift.findings.find((f) => f.status === 'filed');
  expect(filed).toBeTruthy();
  expect(filed!.workItemUrl).toContain('/issues/');

  const file = (await (await request.get(
    `/api/repos/${REPO}/files/${filed!.docPath}?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(file.content).toContain('work-items:');
  expect(file.content).toContain(filed!.workItemUrl);

  // the run maintained its git-native report on the workspace branch (main
  // is protected) and the card links to it. Findings can be visible (and
  // fileable) while the run is still going — await completion before
  // reading the report file.
  await expect.poll(async () => {
    const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as { run?: { status: string } };
    return d.run?.status;
  }, { timeout: 30_000 }).not.toBe('running');
  await expect(page.getByText('reports/source-alignment.md').first()).toBeVisible();
  const report = (await (await request.get(
    `/api/repos/${REPO}/files/reports/source-alignment.md?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(report.content).toContain('# Source Alignment');
  expect(report.content).toContain('<!-- specquill:alignment:begin');
  expect(report.content).toContain('## Run activity');
  expect(report.content).toContain('timestamp precision drifted');

  // self-heal: drop the uncommitted backlink + report saves
  await request.post(`/api/repos/${REPO}/discard?branch=${q(ws.branch)}`, {
    headers: H, data: { paths: [filed!.docPath, 'reports/source-alignment.md'] },
  });
});

test('dismissing a finding survives a re-run', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  await page.getByRole('button', { name: 'Check drift' }).click();
  await expect(page.getByText(/timestamp precision drifted/).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/timestamp precision drifted/)).toHaveCount(2);

  await page.getByRole('button', { name: 'Dismiss' }).first().click();
  await expect(page.getByText(/timestamp precision drifted/)).toHaveCount(1);

  // re-run the same scope: the fingerprint is anchor-based, so the identical
  // finding stays dismissed instead of resurrecting. Completion is awaited
  // via the API — the mock finishes runs faster than the UI poll interval.
  await page.getByRole('button', { name: 'Check drift' }).click();
  await expect.poll(async () => {
    const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as { run?: { status: string } };
    return d.run?.status;
  }, { timeout: 30_000 }).toBe('ok');
  await expect(page.getByText(/timestamp precision drifted/)).toHaveCount(1);
  await expect(page.getByText(/1 dismissed/)).toBeVisible();
});

test('gap analysis reverse-engineers the missing requirement', async ({ page, request }) => {
  await resetFindings(request);
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  // switch to gap analysis: sweeps the selected references, not the docs
  await page.getByText('Gaps', { exact: true }).click();
  await expect(page.getByText(/sweeps 1 reference source/)).toBeVisible();
  await page.getByRole('button', { name: 'Find gaps' }).click();

  // the mock reports one uncovered capability from ~regulations
  await expect(page.getByText(/GDPR storage limitation has no requirement/).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText('gap', { exact: true })).toBeVisible();
  await expect(page.getByText(/suggests requirements\/REQ-gdpr-retention\.md/)).toBeVisible();

  // reverse-engineer the missing requirement — lands as an uncommitted draft
  // on the workspace branch and the UI opens it in the editor
  await page.getByRole('button', { name: 'Draft requirement' }).click();
  await expect(page).toHaveURL(/editor\/requirements\/REQ-gdpr-retention\.md/, { timeout: 20_000 });
  await expect(page.getByText('Report Data Retention').first()).toBeVisible({ timeout: 10_000 });

  // the draft exists on the workspace branch with proper frontmatter
  const ws = { branch: await wsBranch(request) };
  const file = (await (await request.get(
    `/api/repos/${REPO}/files/requirements/REQ-gdpr-retention.md?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(file.content).toContain('type: requirement');
  expect(file.content).toContain('status: draft');

  // self-heal: remove the draft (resetFindings clears the pointer next run)
  await request.delete(`/api/repos/${REPO}/files/requirements/REQ-gdpr-retention.md?branch=${q(ws.branch)}`, { headers: H });
});

test('linker proposes and applies a missing typed link', async ({ page, request }) => {
  // self-heal: a previous run's applied link lives on the ws worktree
  const ws = { branch: await wsBranch(request) };
  await request.post(`/api/repos/${REPO}/discard?branch=${q(ws.branch)}`, {
    headers: H, data: { paths: ['specs/venue.md'] },
  });

  await page.goto(`/p/${REPO}/links`);
  await expect(page.getByText('Link suggestions')).toBeVisible();
  await page.getByRole('button', { name: 'Suggest links' }).click();

  // the mock proposes venue.md implements REQ-063 (absent in the fixture)
  await expect(page.getByText('specs/venue.md').first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/does not declare REQ-063/)).toBeVisible();

  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByText(/linked \(uncommitted draft\)/)).toBeVisible({ timeout: 15_000 });

  // the from-doc's frontmatter carries the link on the workspace branch
  const file = (await (await request.get(
    `/api/repos/${REPO}/files/specs/venue.md?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(file.content).toContain('requirements/REQ-063.md');

  // self-heal for the next suite run
  await request.post(`/api/repos/${REPO}/discard?branch=${q(ws.branch)}`, {
    headers: H, data: { paths: ['specs/venue.md'] },
  });
});

test('a finding spawns a linked work item in the workspace', async ({ page, request }) => {
  await resetFindings(request);
  await page.goto(`/p/${REPO}/alignment`);
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  await page.getByRole('button', { name: 'Check drift' }).click();
  await expect(page.getByText(/timestamp precision drifted/).first()).toBeVisible({ timeout: 30_000 });

  // "+ Work item" drafts the WHEN document and opens it in the editor
  await page.getByRole('button', { name: '+ Work item' }).first().click();
  await expect(page).toHaveURL(/editor\/work-items\/WI-timestamp-precision\.md/, { timeout: 20_000 });
  await expect(page.getByText('Raise execution-timestamp precision').first()).toBeVisible({ timeout: 10_000 });

  // it carries the configured typed link (delivers) back to the drifted spec
  const ws = { branch: await wsBranch(request) };
  const file = (await (await request.get(
    `/api/repos/${REPO}/files/work-items/WI-timestamp-precision.md?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(file.content).toContain('delivers:');
  expect(file.content).toMatch(/specs\/(txn-report|venue)\.md/);

  // and the finding now carries the remedy instead of the create buttons
  const drift = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    findings: { remedyPath: string; remedyKind: string }[];
  };
  expect(drift.findings.some((f) => f.remedyPath === 'work-items/WI-timestamp-precision.md' && f.remedyKind === 'work_item')).toBe(true);

  await request.delete(`/api/repos/${REPO}/files/work-items/WI-timestamp-precision.md?branch=${q(ws.branch)}`, { headers: H });
});
