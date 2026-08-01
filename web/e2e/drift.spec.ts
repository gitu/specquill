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

// finding rows carry data-drift-finding=<kind>; scope to them, since the run
// activity feed repeats titles and would inflate page-wide text matches
const rows = (page: import('@playwright/test').Page, kind?: string) =>
  page.locator(kind ? `[data-drift-finding="${kind}"]` : '[data-drift-finding]');

// the id of the latest run, captured BEFORE starting a new one
async function runId(request: APIRequestContext, branch: string) {
  const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as { run?: { id: number } };
  return d.run?.id ?? 0;
}

// findings appear per document while a run is going, and a PREVIOUS run may
// still be the latest one — wait for a run newer than `after` to finish
async function runFinished(request: APIRequestContext, branch: string, after = 0) {
  await expect.poll(async () => {
    const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as { run?: { id: number; status: string } };
    if (!d.run || d.run.id <= after) return 'pending';
    return d.run.status;
  }, { timeout: 30_000 }).toMatch(/^(ok|error|cancelled)$/);
}

// the standing report is the PROJECT's (drift.report:) — ask, never assume
async function standingReport(request: APIRequestContext, branch: string) {
  const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as { defaultReport: string };
  return d.defaultReport;
}

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
    'changes/2026-08-timestamp-precision.md', 'reports/extracted-regulations.md',
    'changes/2026-08-rts22-precision.md', 'requirements/REQ-exec-precision.md',
    'requirements/REQ-timestamp-validation.md', await standingReport(request, branch)]) {
    await request.delete(`/api/repos/${REPO}/files/${p}?branch=${q(branch)}`, { headers: H });
  }
  // a previous run's report (and any remedy link written into a doc) are
  // worktree saves on ws — drop them too
  await request.post(`/api/repos/${REPO}/discard?branch=${q(branch)}`, {
    headers: H, data: { paths: [await standingReport(request, branch), 'specs/txn-report.md', 'specs/venue.md'] },
  });
}

test('scoped drift run verifies findings, files a work item and backlinks the doc', async ({ page, request }) => {
  let forgeUp = false;
  try {
    forgeUp = (await request.get('http://127.0.0.1:8992/api/v4/projects/acme%2Fbackend/issues')).ok();
  } catch { /* not running */ }
  test.skip(!forgeUp, 'mock-forge not running (drift filing target)');
  await resetFindings(request);

  const ws0 = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  // scope the run to specs/ — index.md is generated and stays out — and pin
  // the report target explicitly (the picker defaults to the LAST run's
  // report, which another session may have pointed elsewhere)
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  await expect(page.getByText(/2 docs in scope/)).toBeVisible();
  const report = await standingReport(request, ws0.branch);
  await page.locator('input[list="drift-report-docs"]').fill(report);
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
  await expect(page.getByText(report).first()).toBeVisible();
  const reportDoc = (await (await request.get(
    `/api/repos/${REPO}/files/${report}?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(reportDoc.content).toContain('<!-- specquill:alignment:begin');
  expect(reportDoc.content).toContain('## Run activity');
  expect(reportDoc.content).toContain('timestamp precision drifted');

  // self-heal: drop the uncommitted backlink + report saves
  await request.post(`/api/repos/${REPO}/discard?branch=${q(ws.branch)}`, {
    headers: H, data: { paths: [filed!.docPath, report] },
  });
});

test('dismissing a finding survives a re-run', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  const prev = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, prev);
  await expect(rows(page).first()).toBeVisible({ timeout: 30_000 });
  const before = await rows(page).count();
  expect(before).toBeGreaterThan(1);

  await page.getByRole('button', { name: 'Dismiss' }).first().click();
  await expect(rows(page)).toHaveCount(before - 1);

  // re-run the same scope: the fingerprint is anchor-based, so the identical
  // finding stays dismissed instead of resurrecting. Completion is awaited
  // via the API — the mock finishes runs faster than the UI poll interval.
  const prev2 = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, prev2);
  await expect(rows(page)).toHaveCount(before - 1);
  await expect(page.getByText(/1 dismissed/)).toBeVisible();
});

test('gap analysis reverse-engineers the missing requirement', async ({ page, request }) => {
  await resetFindings(request);
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  // switch to gap analysis: sweeps the selected references, not the docs
  await page.getByText('Gaps', { exact: true }).click();
  await expect(page.getByRole('button', { name: '~regulations' })).toBeVisible();
  await expect(page.getByText(/1 source · uncovered capabilities/)).toBeVisible();
  await page.getByRole('button', { name: 'Find gaps' }).click();

  // the mock reports one uncovered capability from ~regulations
  await expect(page.getByText(/GDPR storage limitation has no requirement/).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText('gap', { exact: true })).toBeVisible();
  await expect(rows(page, 'coverage-gap').getByText(/suggests requirements\/REQ-gdpr-retention\.md/)).toBeVisible();

  // reverse-engineer the missing requirement — lands as an uncommitted draft
  // on the workspace branch and the UI opens it in the editor
  await rows(page, 'coverage-gap').getByRole('button', { name: 'Draft requirement' }).click();
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

test('the run narrates its work and proposes new requirements', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  const prev = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, prev);

  // the activity feed shows what the run actually did — including the tool
  // calls the model made against the reference source
  const feed = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    run: { activity: string[] }; findings: { kind: string; suggestedPath: string }[];
  };
  const log = feed.run.activity.join('\n');
  expect(log).toContain('drift check of 2 documents against ~regulations');
  expect(log).toContain('[1/2] specs/');
  expect(log).toMatch(/· read ~regulations\//);        // tool use, live
  expect(log).toMatch(/⚠ (high|medium) [\w-]+ @ /);      // each finding named
  expect(log).toMatch(/▪ ok — \d+ findings? live/);     // closing summary
  // the full log lives on its own full-width tab
  await page.getByRole('button', { name: /^Run activity/ }).click();
  await expect(page.getByText(/· read ~regulations\//).first()).toBeVisible();
  await page.getByRole('button', { name: /^Findings/ }).click();
  await expect(rows(page).first()).toBeVisible();

  // drift also detects requirements that do not exist yet: the source
  // mandates something no document states, with a path proposed for it
  const fresh = feed.findings.find((f) => f.kind === 'new-requirement');
  expect(fresh).toBeTruthy();
  expect(fresh!.suggestedPath).toBe('requirements/REQ-amendment-tracking.md');
  const row = rows(page, 'new-requirement').first();
  await expect(row.getByText('new', { exact: true })).toBeVisible();
  await expect(row.getByText(/suggests requirements\/REQ-amendment-tracking\.md/)).toBeVisible();
  // and such a finding is draftable, exactly like a coverage gap
  await expect(row.getByRole('button', { name: 'Draft requirement' })).toBeVisible();
});

test('extract analyzes the app into a persisted requirement inventory', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  await page.getByText('Extract', { exact: true }).click();
  await expect(page.getByText(/1 source → grouped requirement inventory/)).toBeVisible();
  const prev = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Analyze app' }).click();
  await runFinished(request, ws.branch, prev);

  // the inventory is a real document in the repo, grouped by capability,
  // with verbatim evidence and coverage mapping
  const doc = (await (await request.get(
    `/api/repos/${REPO}/files/reports/extracted-regulations.md?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string };
  expect(doc.content).toContain('type: extraction');
  expect(doc.content).toContain('<!-- specquill:extraction:begin');
  expect(doc.content).toContain('## Transaction reporting');   // divided by area
  expect(doc.content).toContain('## Data protection');          // …both of them
  expect(doc.content).toContain('SHALL be captured to microsecond precision');
  expect(doc.content).toContain('✓ full');                      // walked and matched
  expect(doc.content).toContain('requirements/REQ-042.md');
  expect(doc.content).toContain('◐ partial');
  expect(doc.content).toContain('— *not covered*');
  expect(doc.content).not.toContain('Hallucinated');            // evidence verified

  // the card links it, and the run says what it did
  await expect(page.getByText('reports/extracted-regulations.md').first()).toBeVisible();
  const feed = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    run: { activity: string[] }; extractions: { source: string; path: string }[];
  };
  const log = feed.run.activity.join('\n');
  expect(log).toContain('divided ~regulations into 2 areas');            // divide
  expect(log).toMatch(/area 1\/2: /);                                    // conquer
  expect(log).toContain('matching 1-3 of 3 against the specs');          // walk
  expect(log).toContain('matched 2 of 3 requirements to documents');     // match
  expect(log).toMatch(/✓ 3 requirements in 2 groups → reports\/extracted-regulations\.md/);
  expect(feed.extractions).toContainEqual({ source: 'regulations', path: 'reports/extracted-regulations.md' });

  // a following drift run starts FROM the extraction
  await page.getByText('Drift', { exact: true }).click();
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  const prev2 = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, prev2);
  const after = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    run: { activity: string[] };
  };
  expect(after.run.activity.join('\n')).toContain('using extracted requirements as the baseline');

  await request.delete(`/api/repos/${REPO}/files/reports/extracted-regulations.md?branch=${q(ws.branch)}`, { headers: H });
});

test('gap sweeps can be aimed: suggested areas, a focus and chosen sources', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await page.getByText('Gaps', { exact: true }).click();

  // ask where to look — the proposals name an area and why it pays off
  await page.getByRole('button', { name: 'Suggest areas' }).click();
  const area = page.getByText('Data retention', { exact: true });
  await expect(area).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/retention rules have no requirement document/)).toBeVisible();

  // picking one aims the sweep (and selects the sources it lives in)
  await area.click();
  await expect(page.locator('input[placeholder*="name an area"]')).toHaveValue('Data retention');
  await expect(page.getByText(/1 source · focused on “Data retention”/)).toBeVisible();

  const prev = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Find gaps' }).click();
  await runFinished(request, ws.branch, prev);

  // the run records the restriction and the focus, and says so
  const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    run: { scope: string[]; activity: string[] };
  };
  expect(d.run.scope).toEqual(['regulations']);
  expect(d.run.activity.join('\n')).toContain('focus: Data retention');
});

test('a finding plans a linked SET of documents from the configured families', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  const prev = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, prev);

  // the plan proposes documents of the workspace's OWN families, wired the
  // way its link types prescribe
  await page.getByRole('button', { name: 'Plan documents' }).first().click();
  const plan = page.locator('[data-drift-plan]').first();
  await expect(plan).toBeVisible({ timeout: 30_000 });
  await expect(plan.getByText(/change realized by two requirements/)).toBeVisible();
  await expect(plan.getByText('changes/2026-08-rts22-precision.md').first()).toBeVisible();
  await expect(plan.getByText(/drivers → changes\/2026-08-rts22-precision\.md/).first()).toBeVisible();

  await plan.getByRole('button', { name: /^Create 3 document/ }).click();
  await expect(page.getByText(/✓ change: 2026-08-rts22-precision\.md/)).toBeVisible({ timeout: 30_000 });

  // one driver, two requirements — each carrying its family's type and the
  // typed link up to the change
  const read = async (p: string) => ((await (await request.get(
    `/api/repos/${REPO}/files/${p}?ref=${q(ws.branch)}`, { headers: H })).json()) as { content: string }).content;
  expect(await read('changes/2026-08-rts22-precision.md')).toContain('type: Change Record');
  for (const p of ['requirements/REQ-exec-precision.md', 'requirements/REQ-timestamp-validation.md']) {
    const doc = await read(p);
    expect(doc).toContain('type: Requirement');
    expect(doc).toContain('drivers:');
    expect(doc).toContain('changes/2026-08-rts22-precision.md');
  }
  // the finding records the whole set
  const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    findings: { documents: { kind: string; path: string }[] }[];
  };
  expect(d.findings.some((f) => f.documents?.length === 3)).toBe(true);
});
