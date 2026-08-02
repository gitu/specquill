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
    findings?: { fingerprint: string; kind: string; draftPath: string; remedyPath: string }[];
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

  // File a DOC-BACKED finding: this test asserts the work-items backlink
  // lands in that document's frontmatter, so it must not pick a finding that
  // has no document (a gap, or a project recipe's source-anchored kind —
  // reconciliation deliberately leaves other recipes' findings alone).
  await rows(page, 'outdated-requirement').first()
    .getByRole('button', { name: 'File issue' }).click();
  await expect(page.getByText(/work item filed/).first()).toBeVisible({ timeout: 15_000 });

  // the filing wrote a work-items backlink into the doc's frontmatter as an
  // uncommitted save on the workspace branch (main is protected in dev)
  const ws = { branch: await wsBranch(request) };
  const drift = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    findings: { status: string; docPath: string; workItemUrl: string }[];
  };
  const filed = drift.findings.find((f) => f.status === 'filed' && f.docPath !== '');
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
  await expect(page.getByText('gap', { exact: true }).first()).toBeVisible();
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

  // "+ Work item" drafts the WHEN document and opens it in the editor. Scope
  // to the drifted SPEC finding: the work item's `delivers:` link points at
  // the document the finding is about, so a source-anchored finding (a gap, or
  // a project recipe's own kind) would have nothing to link to.
  await rows(page, 'outdated-requirement').first()
    .getByRole('button', { name: '+ Work item' }).click();
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

// A run is a server-side worker: closing the page does not stop it, and a run
// that stops with units left can be picked up where it stopped.
test('a stopped run is picked up where it stopped', async ({ page, request }) => {
  await resetFindings(request);
  const branch = await wsBranch(request);
  const before = await runId(request, branch);

  // the same scope the other tests use — a wider one would leave findings
  // behind for documents no other test resets. Stopped at once: the mock
  // answers in milliseconds, so waiting for progress races the run to its end.
  const started = (await (await request.post(`/api/repos/${REPO}/drift/run?branch=${q(branch)}`, {
    headers: H, data: { mode: 'drift', paths: ['specs/'] },
  })).json()) as { runId: number; docsTotal: number };
  expect(started.docsTotal).toBeGreaterThan(1);
  await request.post(`/api/repos/${REPO}/drift/cancel?branch=${q(branch)}`, { headers: H, data: {} });
  await runFinished(request, branch, before);

  const stopped = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as {
    run: { id: number; status: string; docsDone: number; docsTotal: number; resumable: boolean };
  };
  expect(stopped.run.status).toBe('cancelled');
  expect(stopped.run.resumable).toBe(true);
  const left = stopped.run.docsTotal - stopped.run.docsDone;

  // the page offers to pick it up, naming what is left. The run was set up
  // through the API, so the branch has to be in the URL — the SPA only
  // switches by itself when a run is started from the card.
  await page.goto(`/p/${REPO}/b/${q(branch)}/alignment`);
  const banner = page.locator('[data-drift-resume]');
  await expect(banner).toContainText(`${stopped.run.docsDone} of ${stopped.run.docsTotal}`);
  await page.locator('[data-drift-resume-start]').click();

  // the resumed run covers ONLY what the stopped one never reached
  await expect.poll(async () => {
    const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as {
      run?: { id: number; docsTotal: number; resumedFrom: number };
    };
    return d.run && d.run.id > stopped.run.id ? `${d.run.docsTotal}/${d.run.resumedFrom}` : 'pending';
  }, { timeout: 30_000 }).toBe(`${left}/${stopped.run.id}`);
  await runFinished(request, branch, stopped.run.id);
  await expect(page.locator('[data-drift-resume]')).toHaveCount(0);
});

// The mock finishes a run in about a second, far too fast to catch the live
// card by navigating — hold the payload in `running` instead, which is what
// this assertion is actually about: what a running card TELLS the user.
test('a running check says it does not need the page open', async ({ page, request }) => {
  const branch = await wsBranch(request);
  await page.route(`**/api/repos/${REPO}/drift?*`, async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    body.run = { ...(body.run ?? {}), id: 1, mode: 'drift', status: 'running', error: '',
      scope: ['specs/a.md', 'specs/b.md'], docsTotal: 2, docsDone: 1, droppedUnverified: 0,
      headSha: '', activity: ['19:00:00 [1/2] specs/a.md'], reportPath: '', reportBranch: '',
      focus: '', resumedFrom: 0, resumable: false, startedAt: 0, finishedAt: 0 };
    await route.fulfill({ response: res, json: body });
  });
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByText(/Runs on the server/)).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText(/picks the run back up when you return/)).toBeVisible();
});

// The page shows ONE run — the newest by default, any earlier one on request.
// Picking a past run scopes the findings to what THAT run found.
test('past runs stay selectable and scope the findings', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  // run 1: a drift check over specs/
  await page.getByRole('button', { name: 'specs/', exact: true }).click();
  const before = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, before);
  const driftRun = await runId(request, ws.branch);

  // run 2: a gap sweep, which becomes the newest
  await page.getByText('Gaps', { exact: true }).click();
  await page.getByRole('button', { name: 'Find gaps' }).click();
  await runFinished(request, ws.branch, driftRun);
  const gapRun = await runId(request, ws.branch);
  expect(gapRun).toBeGreaterThan(driftRun);

  // the newest run is what the page defaults to, and both runs' findings show
  const picker = page.locator('[data-drift-run-picker]');
  await expect(picker).toHaveValue(String(gapRun), { timeout: 15_000 });
  await expect(picker.locator(`option[value="${driftRun}"]`)).toHaveCount(1);
  await expect(rows(page, 'coverage-gap').first()).toBeVisible();
  await expect(rows(page, 'outdated-requirement').first()).toBeVisible();

  // pick the older drift run: its findings only, and the panel says so
  await picker.selectOption(String(driftRun));
  await expect(page.locator('[data-drift-scoped]')).toBeVisible();
  await expect(rows(page, 'outdated-requirement').first()).toBeVisible();
  await expect(rows(page, 'coverage-gap')).toHaveCount(0);
  await expect(page.getByText('Run ' + driftRun, { exact: true })).toBeVisible();

  // back to the default view: every live finding again
  await page.getByRole('button', { name: 'Show all' }).click();
  await expect(page.locator('[data-drift-scoped]')).toHaveCount(0);
  await expect(rows(page, 'coverage-gap').first()).toBeVisible();
});

// Every mode can be aimed the same three ways; a drift check narrows to
// single documents and to the sources it verifies against.
test('a drift check can be narrowed to one document and one source', async ({ page, request }) => {
  await resetFindings(request);
  const ws = { branch: await wsBranch(request) };
  await page.goto(`/p/${REPO}/alignment`);
  await expect(page.getByRole('heading', { name: 'Source alignment' })).toBeVisible();

  // pick ONE document instead of a whole folder
  await page.getByText(/pick individual documents/).click();
  const picker = page.locator('[data-drift-doc-picker]');
  await picker.getByText('specs/venue.md').click();
  await expect(page.getByText(/1 doc in scope/)).toBeVisible();

  // …and restrict which reference it is verified against
  await page.getByRole('button', { name: '~regulations' }).click();
  await expect(page.getByText(/1 doc in scope · against 1 source/)).toBeVisible();

  const before = await runId(request, ws.branch);
  await page.getByRole('button', { name: 'Check drift' }).click();
  await runFinished(request, ws.branch, before);

  const d = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(ws.branch)}`, { headers: H })).json()) as {
    run: { scope: string[] };
  };
  expect(d.run.scope).toEqual(['specs/venue.md']);
});

// A project's OWN pipeline, end to end: the recipe committed in the fixture
// (repo/.specquill/alignment/deadline-audit.md) is picked from the SPA, dry
// run, started, and produces findings carrying its own declared kind.
test('a project recipe is listed, dry run and executed like a built-in', async ({ page, request }) => {
  await resetFindings(request);
  const branch = await wsBranch(request);

  // the recipe ships with the fixture and loads on this branch
  const listed = (await (await request.get(
    `/api/repos/${REPO}/alignment/recipes?branch=${q(branch)}`, { headers: H })).json()) as {
      recipes: { slug: string; name: string; builtin: boolean; findings: { kind: string }[] }[];
      errors: Record<string, string>;
      models: string[]; maxCallsPerRun: number;
    };
  expect(Object.keys(listed.errors)).toHaveLength(0); // nothing broken in the fixture
  for (const slug of ['drift', 'gaps', 'extract']) {
    expect(listed.recipes.find((r) => r.slug === slug)?.builtin).toBe(true);
  }
  const mine = listed.recipes.find((r) => r.slug === 'deadline-audit');
  expect(mine, 'the fixture recipe should be listed').toBeTruthy();
  expect(mine!.builtin).toBe(false);
  expect(mine!.findings.map((f) => f.kind)).toContain('unstated-deadline');
  expect(listed.maxCallsPerRun).toBeGreaterThan(0);

  // the dry run projects the work WITHOUT doing any of it
  const before = await runId(request, branch);
  const check = (await (await request.post(
    `/api/repos/${REPO}/alignment/recipes/validate?branch=${q(branch)}`,
    { headers: H, data: { recipe: 'deadline-audit' } })).json()) as {
      ok: boolean; units: number; unitKind: string; estimatedCalls: number;
      stages: { id: string; calls: number; files?: Record<string, number> }[];
    };
  expect(check.ok).toBe(true);
  expect(check.unitKind).toBe('sources');
  expect(check.units).toBeGreaterThan(0);
  expect(check.estimatedCalls).toBeGreaterThan(0);
  // the recipe filters the source to regulations/** — the projection says so
  const deadlines = check.stages.find((s) => s.id === 'deadlines');
  expect(Object.values(deadlines?.files ?? {}).some((n) => n > 0)).toBe(true);
  expect(await runId(request, branch), 'a dry run must not start a run').toBe(before);

  // …and running it goes through the same engine as any built-in
  await request.post(`/api/repos/${REPO}/drift/run?branch=${q(branch)}`, {
    headers: H, data: { recipe: 'deadline-audit' },
  });
  await runFinished(request, branch, before);

  const drift = (await (await request.get(`/api/repos/${REPO}/drift?branch=${q(branch)}`, { headers: H })).json()) as {
    run: { mode: string; recipeName: string; aiCalls: number; kinds: { kind: string; draftable: boolean }[]; activity: string[] };
    findings: { kind: string; suggestedPath: string }[];
  };
  expect(drift.run.mode).toBe('deadline-audit');
  expect(drift.run.recipeName).toBe('Deadline audit');
  expect(drift.run.aiCalls).toBeGreaterThan(0);
  // the run carries its recipe's kinds, which is how the page labels them
  expect(drift.run.kinds.find((k) => k.kind === 'unstated-deadline')?.draftable).toBe(true);
  // the recipe's own narration, not the engine's
  expect(drift.run.activity.join('\n')).toMatch(/found \d+ deadline/);
  const mineFound = drift.findings.filter((f) => f.kind === 'unstated-deadline');
  expect(mineFound.length).toBeGreaterThan(0);
  expect(mineFound[0].suggestedPath).toBeTruthy();

  // the page renders the custom kind and offers the recipe in its picker
  await page.goto(`/p/${REPO}/alignment?branch=${q(branch)}`);
  await expect(rows(page, 'unstated-deadline').first()).toBeVisible();
  await expect(page.locator('[data-drift-recipe]')).toContainText('Deadline audit');
});

// A recipe that is there but does not parse must SAY so — otherwise it is
// simply absent from the picker and nobody can tell why.
test('a broken recipe is reported, not silently dropped', async ({ page, request }) => {
  const branch = await wsBranch(request);
  const path = '.specquill/alignment/scratch-broken.md';
  await request.put(`/api/repos/${REPO}/files/${path}?branch=${q(branch)}`, {
    headers: H, data: { content: '---\nname: Broken\nunits: nope\n---\n' },
  });
  try {
    const listed = (await (await request.get(
      `/api/repos/${REPO}/alignment/recipes?branch=${q(branch)}`, { headers: H })).json()) as {
        errors: Record<string, string>;
      };
    expect(listed.errors['scratch-broken']).toMatch(/units must be/);

    await page.goto(`/p/${REPO}/alignment?branch=${q(branch)}`);
    await expect(page.getByText(/scratch-broken\.md/)).toBeVisible();
  } finally {
    await request.delete(`/api/repos/${REPO}/files/${path}?branch=${q(branch)}`, { headers: H });
  }
});
