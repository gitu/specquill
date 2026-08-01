# specquill — project notes for Claude

Git-native requirements engineering: markdown + typed frontmatter links in git,
Go single binary (`server/`) + React SPA (`web/`). Read `README.md` first for
the architecture; this file is operational knowledge that is NOT derivable
from the code.

## Dev environment

- Server runs on **:8643** (8080 is a Java app, 8642 a dead socket); also
  reachable over the local tailnet on the same port.
- Start: `./server/specquill -config specquill.dev.yml -dev` — the `-dev` flag
  auto-authenticates every request as `auth.dev_user` ("Flo Dev", workspace
  branch `ws/dev`) and bypasses session TTLs.
- **Hot-reload loop: `make dev`** (`scripts/dev.sh`) — starts `air`
  (rebuilds/restarts the Go server on save; a bare `touch` does NOT trigger it,
  air ignores chmod-only events), and vite HMR on 127.0.0.1:5643 (strictPort —
  another project squats :5173 on this machine; proxies /api+/auth, ws
  included). In `-dev` mode the Go server reverse-proxies SPA routes to vite
  (`internal/api/devproxy.go`, override via `SPECQUILL_VITE_ADDR`), so browse
  **:8643** — HMR works there, tailnet included; it falls back to the embedded
  build when vite is down. E2E still needs the embedded build (`make build`).
  **Working in a git worktree**: the proxy finds ANY vite on :5643, including
  one started from another checkout — the server then serves that checkout's
  SPA and your routes/components silently do not exist. Symptom: a new route
  redirects to the default view. Run with `SPECQUILL_VITE_ADDR=127.0.0.1:5699`
  (any dead port) to force the embedded build.
- **Secondary forge-mode dev server: `make dev-forge`** — :8644,
  `specquill.dev-forge.yml`: real `auth.forge` GitHub PAT login (`repo` scope)
  against gitu/specquill, own store/clones under `data/runtime-forge/`. One
  configured anchor project (`trading-specs` = `repo/`, REQ-024 login/role
  gate) + **dynamic projects enabled** (`dynamic:` block, REQ-025): "＋ Open
  repository…" in the project switcher opens any repo your PAT reaches whose
  root `.specquill/config.yml` declares workspaces — e.g.
  `gitu/specquill#specquill-docs` via the committed root manifest. Same
  dialog = checkout overview + reclaim/close. The specquill repo itself is
  the read-only reference `specquill-src` (workspace configs' `sources:`;
  in-repo defs + manifest only go live once pushed to GitHub). Deliberately
  runs WITHOUT `-dev` (auto-auth would bypass the PAT login it exists to
  exercise) ⇒ no vite proxy, embedded SPA only — `make build` + restart to
  see SPA changes. Propose pushes real `ws/<user>` branches and opens real
  PRs on gitu/specquill; in forge mode every commit now auto-pushes its
  branch (REQ-025.10).
- **The SPA is embedded in the Go binary.** After `cd web && npm run build`
  you MUST `cd server && go build -o specquill ./cmd/specquill` and restart, or
  the browser silently serves the stale build.
- `pkill specquill` matches the wrapper shell (exit 143) — use `pkill -x specquill`.
- **The store is embedded SQLite** (users, sessions, workspace claims) at
  `<data_dir>/specquill.db` = `data/runtime/specquill.db` in dev — no service
  to start, nothing in docker compose. Go tests get a throwaway DB per test
  via `store.OpenTest` and never skip. WAL mode, so `specquill.db-wal` /
  `-shm` sidecars sit next to it; `PRAGMA foreign_keys=ON` and
  `_txlock=immediate` are set in `store.Open` and the grant cascades depend
  on the former.
- Repo clones/worktrees live under `data/runtime/repos/<repo>/`; the
  canonical repo key in DB rows and room keys is the plain repo id, e.g.
  `trading-specs` (single-tenant since July 2026).
- `make dev-samples` adds two EXTRA sample projects (`sample-payments`,
  `sample-onboarding`) with real multi-commit/multi-author history — for
  testing history-aware features; auto-registers via the management API when
  the dev server is up. Survives until the next store reset.
- Full state reset: `pkill -x specquill; rm -rf data/runtime && ./scripts/dev-fixture.sh`
  — with the store inside `data/runtime`, removing that directory now clears
  sessions/merge state too (the fixture script also deletes the DB, so
  fixtures and store can't drift apart).
- Speccy in dev points at ollama `qwen2.5:7b` (`specquill.dev.yml`);
  `scripts/mock-llm.py` (:8991) is the keyless provider the speccy e2e needs
  (it self-skips unless the configured model is `mock-1`).

## Testing

- Go: `cd server && go test ./...`
- Unit: `cd web && npx vitest run`
- E2E: `cd web && npx playwright test` — MUST run from `web/` (running from
  the repo root loads a second @playwright/test and fails weirdly). Requires a
  running dev server built from the current source (see embedded-SPA note).
- Screenshot specs are gated behind `SHOT=1`.
- E2E state discipline: tests self-heal or use unique per-run file names
  (`scratch-*-<stamp>.md`).

## Domain model / invariants

- **Projects vs sources** (config-split): a **project** is a writable workspace
  = git repo + optional `content_root` subfolder (monorepo). A **source** is a
  read-only catalog entry projects reference. `internal/project` is the ONLY
  place project-relative ↔ full repo paths are mapped (MapIn/MapOut); store rows
  and git ops use full paths, the wire format is project-relative.
- **3-stage authorization** (local-auth mode): (1) catalog sources+credentials in
  app YAML/admin, (2) in-repo `.specquill/config.yml` `references:` SELECT
  cataloged sources (read from the request's branch, worktree edits included,
  falling back to the default branch — safe because selection can never mint
  access beyond the catalog; changed 2026-07-29), (3) deployment roles
  viewer<member<admin (`users.role`) plus per-repo grants. In-repo config can
  only select cataloged sources — it can NEVER mint access.
  `EffectiveReferences` = selection ∩ catalog.
- **Forge-PAT mode flips stage 1** (`auth.forge`, July 2026): no server-side
  catalog or credentials — `.specquill/config.yml` gains `sources:` DEFINITIONS
  (git; https-only, no userinfo, host must be on the allowlist = forge ∪
  project hosts ∪ `auth.forge.allowed_source_hosts`) and
  `EffectiveReferencesInRepo` resolves references against them. The git
  credential helper is HOST-SCOPED (`SPECQUILL_GIT_HOST`): the token is only
  released to the repo's own remote host — redirects get nothing. Safe because every user gets their OWN clones under
  `data/runtime/repos/u<id>/`, fetched lazily with their own PAT
  (`gitx.Fleet`/`NewUserManager`; `api/pat.go` is the mode's plumbing) — a
  defined source a user's token cannot fetch 502s for them and nothing lands on
  disk. PAT lives in browser localStorage + a RAM-only session vault
  (`auth.TokenVault`); server restart ⇒ silent re-login from the SPA
  (`client.ts` retry-once). Deployment role = forge permission on
  `projects[0]`, refreshed each login. No boot clone, no sync loops; in-app
  merge 403s (`merge_via_forge`) — `POST /propose` pushes and opens the MR/PR.
- **Speccy grounding**: grounded reference sources join the system prompt under
  `## ~source/path` read-only headings (workspace keeps a 60% budget floor);
  draft edits refuse any `~`-prefixed path.
- **Extraction is the baseline** (mode `extract`), and it is DIVIDE AND
  CONQUER, not one pass: (1) `ai.SurveyPrompt` divides the app into
  capability areas with file hints (capped at `maxExtractAreas`), (2) each
  area is extracted on its OWN AI loop — a failed area is noted and skipped,
  never sinking the source, (3) `ai.MatchPrompt` then walks the extracted
  requirements in batches of `matchBatchSize` and matches each against the
  workspace docs (full/partial/none + the document + why). Extraction no
  longer guesses coverage inline — matching is its own phase, and a match
  naming a document that does not exist degrades to `none`. The result is a
  grouped inventory: capability areas, atomic RFC-2119 statements, verbatim
  evidence, coverage per requirement. It persists
  as its own living document BESIDE the alignment report
  (`<report folder>/extracted-<source>.md`, `specquill:extraction:begin/end`
  markers, `type: extraction`); the run records what it wrote in
  `drift_runs.extractions_json`, which is what GET /drift lists (the docs
  may live on the caller's ws branch, not the queried one). Later drift and
  gap runs feed that block into their prompts as the analyzed baseline
  (`extractionContext`, narrated as "using extracted requirements as the
  baseline"). Engine-marked docs — reports AND extractions — never enter a
  run scope.
- **Source alignment** (`api/drift.go`; its OWN page `/alignment` —
  `views/AlignmentView.tsx`, rail icon. `DriftControls` (run controls) and
  the last-run panel sit compact side by side; below them a TABBED
  FULL-WIDTH panel switches between `DriftFindings` and the run activity —
  findings need the width for their paths, evidence and actions, and the log
  needs it to stay unwrapped. The Overview keeps only the compact
  `AlignmentSummary` card + "Check drift" in the editor) has TWO run modes:
  **drift** —
  scoped per-document AI runs verify docs against the selected references —
  and **gaps** — per-source sweeps report capabilities no document covers.
  Any run may be RESTRICTED to a subset of the project's references
  (`sources:` on the run request, 422 when none match) and a gaps sweep may
  be AIMED at one area (`focus:`, a hard constraint in the prompt — out-of-
  area gaps are another sweep's job); `POST .../drift/focus` proposes where
  to aim next from the extracted inventories (read-only: no run, no writes),
  and the card offers those as clickable chips that set both the focus and
  its sources
  (kind `coverage-gap`, `doc_path=''`, fingerprint anchored on the SOURCE
  path). Drift ALSO proposes documents that don't exist yet: kind
  `new-requirement` (the source mandates something in the audited doc's
  area that no requirement states) carries a `suggestedPath` and is
  draftable exactly like a gap — `draftableKind()` gates the draft
  action, so never re-gate it on `doc_path == ""`. Runs narrate
  themselves per unit AND per model tool call (`· read ~src/path`,
  `search "…"` — `toolNote`), naming every finding kept and dropped;
  the feed persists live (each note) while the report is rewritten
  per unit, and is capped at 400 lines. Finding rows carry
  `data-drift-finding=<kind>` — the e2e must scope to them, since the
  activity feed repeats finding titles in the DOM. A gap's missing requirement can be **reverse-engineered**
  (`POST .../findings/{fp}/draft`, `ai.ReversePrompt`, one-shot Complete):
  the AI drafts the doc from the finding's evidence files, it lands as an
  uncommitted worktree save (ws-branch on protected mains) and
  `draft_path` links finding → doc. Reopening a finding
  (`dismiss {reopen:true}`) clears EVERY pointer to a document the finding
  produced (draft, remedy, the planned set) — the e2e self-heal depends on
  that, and a leftover pointer hides the create actions in the UI. A
  finding can also be PLANNED (`POST .../findings/{fp}/plan` → which
  documents to create, from the workspace's OWN families and link types;
  read-only) and the plan APPLIED (`.../create`) as a linked SET — e.g. one
  change with two requirements carrying `drivers:` up to it. `validatePlan`
  is the gate: unknown families and untitled entries are dropped, paths are
  forced into the family's folder, and only links `linkBetween` permits (on
  the document that may carry them) survive. On create the server also
  applies the family's `type:` label read from a sibling document
  (`familyType` — the label is workspace prose, observable but not
  derivable), so the model drafts content, never placement or
  classification. Every created document is recorded in
  `drift_findings.documents_json`. Every finding also spawns its own
  **remedy document** (`POST .../findings/{fp}/remedy {kind:
  change|work_item}`, `ai.RemedyPrompt`): the AI drafts the WHY/WHEN doc
  in the family folder, learning conventions from an EXISTING doc of that
  family (the fixture's `type: Change Record` is prose no default could
  guess), and the server — never the model — writes the typed link,
  choosing the direction from the workspace's own link_types via
  `linkBetween` (work_item carries `delivers:` → the spec; a change is
  instead pointed AT by the requirement's `drivers:`; change↔spec has no
  link type, so none is written). `workspaceModel`/`docKind` in
  `api/linker.go` are the ONE parse of entities+link_types (modelRules
  reuses them). Each drift check inlines the doc's LINKED
  documents (frontmatter link graph, both directions — `api/linker.go`
  buildLinkIndex) as context. Large scopes are NOT refused: the worker just
  loops (sequential, cancellable); an EXPLICIT `drift.max_docs` remains a
  hard ceiling. The **linker** (`POST .../linker/propose|apply`, "Link
  suggestions" card) AI-proposes missing typed links per the configured
  link_types (tool-loop over the workspace, validated server-side: known
  field, both docs exist, not already linked); apply appends to the
  from-doc's frontmatter as a worktree save; the linker + the on-demand
  LinkCheckCard live on their OWN page `/links` (`views/LinksView.tsx`,
  chain-link rail icon; the Overview keeps the `LinksSummary` pointer
  card). **Live feedback + git-native
  report**: every run narrates per-unit activity (`run.activity` in
  GET /drift, shown in the card while running) and continuously rewrites a
  report doc IN the repo. WHERE it lives belongs to the PROJECT, not the
  server: `drift.report:` in that project's own `.specquill/config.yml`
  (fallback `reports/alignment-{date}.md` — DATED, so a day's runs continue
  one report and the next day starts fresh; `{date}/{yyyy}/{mm}/{dd}` expand
  at run time and a path without them means one standing report). Every
  report/draft/remedy
  path is PROJECT-relative — `project.SaveFile` MapIns it, so a monorepo
  project's alignment docs land under its own `content_root` beside the
  config that names them, never at the repo root (see
  TestDriftReportStaysInsideTheProjectContentRoot). The run request's
  `report:` field still overrides per run — creating a new one or
  CONTINUING any existing one (the card has a picker). Because a run
  WRITES, starting one from a protected branch first moves the user onto
  their `ws/<user>` branch (`ensureWritableBranch`, same as any edit) and
  runs THERE — so the run, its findings (keyed repo+branch), its report
  and every draft/remedy belong to one branch and stay visible. The card
  re-asks 400ms after starting: the branch switch remounts its query, and
  that mount fetch can land before the run row exists, leaving a "no run"
  card that would never poll. Reports are LIVING documents: the engine owns
  only the `<!-- specquill:alignment:begin/end -->` block (run summary +
  findings table + activity + accumulated run log); everything outside it
  is the human's and survives every rewrite. Written on the caller's ws
  branch when the run branch is protected; any doc containing the begin
  marker is excluded from run scopes (plus the run's own report path
  pre-marker). Dismiss/file/draft refresh the last finished run's report
  too. Because `CREATE TABLE IF NOT EXISTS`
  never adds columns, `store.Open` drops drift tables whose shape is stale
  (`dropStaleDriftTables` — safe, they hold derived state only). E2e gotcha:
  findings from reopened state are visible/fileable while a run is still
  going — await run completion via the API before asserting on the report
  file. Findings are SQLite rows keyed by an ANCHOR-based
  fingerprint (docPath|source|kind|anchor — model titles are display-only, so
  dismissals stick across reruns) and evidence quotes are string-verified
  against the source snapshot (unverifiable findings are silently dropped,
  counted as `droppedUnverified`). Filing a finding creates a GitLab/GitHub
  issue (`forge.CreateIssue`, marker `<!-- specquill:drift:<fp> -->` +
  `specquill-drift` label = idempotency) or a Jira issue (`internal/tracker`,
  REST **v2** on purpose — plain-text description; `token_env` with ':' →
  Basic, bare → Bearer) and appends the URL to the doc's `work-items:`
  frontmatter (worktree save; on a protected branch it claims the caller's
  ws branch). Targets = server `work_item_targets:` catalog ∩ in-repo
  `drift.targets` + the project forge as implicit target; in dev,
  `mock-forge.py` plays the `dev-board` target (its /issues endpoints are
  deliberately unauthenticated, and it is a ThreadingHTTPServer — a
  single-threaded mock deadlocks 15s between playwright's keep-alive probe
  and the Go client). The drift e2e needs BOTH mocks (mock-llm + mock-forge).
- **Speccy chat tools** (`ai.StreamTools` + `api/speccytools.go`): read_file /
  list_files / search / ask_user always — the read tools span the workspace
  plus ALL selected references (`resolveSources`; `grounding: true` only
  picks which get prompt-stuffed); edit_file / create_file only when the client sends
  `allowEdits` AND the branch is unprotected (server-checked). Writes are
  uncommitted worktree saves; markdown must keep parseable frontmatter (else
  the tool errors back to the model) and `mdfm.Touch` maintains
  created/updated server-side. ask_user halts the loop — the SPA replays the
  `resume` messages plus a `role:tool` answer; the server stays stateless.
  Project memory = `.specquill/memory/*.md`, ONE decision per file (merge-
  friendly), written by the speccy via create_file and pinned into the prompt
  with skills/instructions (`ai.AuthoringRules`).
- **Non-git sources = importer mirror repos**: url/openapi/confluence sources are
  remote-less gitx repos (`Mirror: true`, `git init --bare`) that `internal/
  importer`'s Runner populates via `SnapshotMirror` (full-tree bare-repo commit,
  idempotent). Credentials are env-only via `token_env`; `email:token` → HTTP
  Basic (Atlassian Cloud), a bare token → Bearer. **Dev quirk**: the demo
  `platform-api` openapi source self-fetches `http://127.0.0.1:8643/demo-openapi.
  json`, so its boot import errors ("connection refused") before the listener is
  up — it goes green on the next interval or a manual `POST /api/sources/platform-api/sync`
  (or the Admin "Sync now" button).
- **Timed dependencies replaced the change inbox** (Aug 2026, REQ-026): the
  built-in `change` family and the whole `inbox:`/`closed_statuses:`/
  `attention_statuses:` machinery are gone. Any document carrying a configured
  validity-window key (`timed:` in the workspace config — `starts`/`ends`,
  `effective_from`/`effective_until`, `due`, first key present wins) lands on
  `/timed` as pending|active|expiring|expired with its dependents' readiness;
  `atRisk` = opens/closes inside `horizon_days` while something is not in
  `ready_statuses`. A `change:` family can still be declared in config as an
  ordinary family (the `changes/` scaffold starter + skill are kept) — it just
  gets no special UI.
- **Three change surfaces** (REQ-027): `/timed` (deadlines), `/changes`
  (pending: uncommitted + ahead-of-default + open MR, composed from existing
  endpoints), `/history` (committed history from git). `GET /log` is
  repo-wide and content-root scoped; the older `GET /history?path=` is
  per-document (`--follow`) — do not confuse them. `GET /commit?sha=&parent=`
  returns the two-dot diff (`gitx.DiffCommit`, NOT the three-dot `DiffRange`
  used for merge previews) plus `internal/delta` semantic deltas; the parent
  travels in the log payload because `ValidRef` rejects `sha^`. Commit
  summaries run on `ai.quick_model` over the delta and cache per `repo@sha`
  without a TTL (commits are immutable); 501 when no model is configured.
- **Protected main**: the default branch is never edited; the first edit
  auto-creates/switches to the caller's `ws/<user>` branch (claimed in the store).
  Direct writes to protected branches 403 (`protected_branch`). A **merge** from
  a workspace branch (`POST /api/repos/{repo}/merge`, member role) is the only
  thing that moves it — there is no in-app PR/review flow, that lives on the
  forge. Merges refuse a dirty source worktree (409 `dirty`) and conflicts
  (409 `conflicts`), and reset the merged workspace onto the new head.
- **Worktree = draft store**: saves are uncommitted changes on a per-branch
  worktree; explicit Commit turns them into history.
- **Commit identity**: the logged-in user is **author AND committer**; the
  service identity (`git.committer_name/email`) is appended as a
  `Co-authored-by:` trailer.
- **Single-editor saves**: every save carries a `baseSha` precondition
  (`SaveFile` → 409 `ErrStale` on mismatch); the conflict banner in the
  editor is the whole concurrency story. Real-time co-editing (Yjs rooms)
  was removed in July 2026.
- **Byte fidelity**: untouched documents save byte-identical; only real user
  edits normalize markdown.
- **Sketches**: `*.excalidraw.png` — PNGs with the excalidraw scene embedded
  (export-embed-scene), natively viewable anywhere, editable in the modal via
  `loadFromBlob`/`exportToBlob`. Legacy `*.excalidraw` JSON still supported.
- **Forge review (optional, read-only)**: `projects[].forge.kind: gitlab|github`
  turns on `GET /api/repos/{repo}/forge/request?branch=` — the branch's open
  MR/PR plus its comments, shown on the Overview. GitLab auth is the
  `PRIVATE-TOKEN` header, GitHub a bearer token; GitLab project paths are
  URL-encoded whole (nested groups), GitHub needs `owner:branch` in the
  `head=` filter or it is ignored. `forge.project` overrides path derivation
  when the remote is an ssh alias. Answers are cached 60s server-side; any
  failure degrades to an `error` field, never a broken page.
- **AI tiers**: `ai.model` (thinking-class: chat, draft edits) vs
  `ai.quick_model` (one-shot: commit messages). Both through any
  OpenAI-compatible endpoint. `.specquill/skills/*.md` in the workspace are
  pinned into the speccy system prompt as authoring rules.

## Hard-won gotchas (do not rediscover these)

- **Never replace ProseMirror node-view DOM** (e.g. `img.replaceWith(...)`):
  PM re-parses and deletes the node from the document. Mutate the existing
  element (swap `src`, add classes) instead.
- **Milkdown listener debounce**: even `listener.updated` is debounced; the
  undebounced truth for "user typed" is a DOM `input` listener on the
  contenteditable.
- **Toolbar flex**: every control cluster needs `flex:none`; otherwise the
  overflowing toolbar silently crushes the weakest item to ~2px. The path
  label is the designated shrink/ellipsis element.
- `sx()` converts inline-style strings to React style objects; components
  carry design styles as strings on purpose — keep that idiom.

## Deployment model

- Two supported deployments (July 2026 decision, revised 2026-07-27): **v1** —
  one deployment per tenant, a single writable repository, **forge-PAT login**
  (`auth.forge`: users bring a GitLab/GitHub personal access token; OIDC was
  removed with this revision); **v2** — a developer's local machine,
  `auth.local`/`-dev`. The GitHub integration (OAuth login, GitHub App
  tenants, webhooks) was removed earlier; git remotes authenticate via the
  per-user PAT (v1) or `token_env` (v2) — `gitx.credentialArgsEnv` is still
  the single credentials seam (manager PAT wins, env is the fallback).
  `scripts/mock-forge.py` (:8992) is the keyless GitLab mock for exercising
  PAT mode; `web/e2e/patlogin.spec.ts` self-skips unless the target server
  reports a `forge` provider.

## Runs outlive the page; one-shot actions do not

- A **run** is a server-side worker (goroutine + `drift_runs` row). Closing
  the tab does not stop it, findings and the report are written per unit, and
  the card picks it back up on return — the card SAYS so while running,
  because nothing else would tell the user.
- Boot closes what the previous process left behind: `MarkInterruptedDriftRuns`
  (called from `api/router.go`) turns every `running` row into status
  **`interrupted`** — its own status, not an error, since the units already
  checked stand.
- A run that stopped with units left (`interrupted`, `cancelled`, errored) is
  **resumable**: `POST .../drift/run {resume: <runId>}` inherits its mode,
  sources, focus and report and runs ONLY `scope[DocsDone:]`. The worker is
  sequential, so `DocsDone` is exactly how far it got — which is why it now
  persists REAL progress when it stops (it used to mark every unit done at
  the end, making a cancelled run look complete and killing the resume).
  `store.DriftRun.Resumable()` is the one gate; a run already picked up by
  another 409s, so the same units never run twice.
- The per-finding actions (draft, plan, remedy, create) and the linker are
  ONE request each — navigating away cancels them. Both surface a
  `data-keep-open` hint while in flight; do not promise resumability there.

## AI call resilience

A long run makes hundreds of model calls, so a single blip must not sink a
unit. Two independent layers, both silent on the happy path:

- **Transport** (`ai.Client.attempt`): 3 attempts with a doubling pause from
  1s for TRANSIENT failures only — 429, 408, any 5xx (the Azure/OpenAI
  "server_error" 502 is the common one) and network errors. A 400/401/404
  fails once. `Retry-After` wins when the provider sends a sane
  delta-seconds; a cancelled run never sleeps out the backoff. Streaming
  calls retry only before the first byte — a mid-stream break is already
  delivered content.
- **Parse** (`askJSON` / `completeJSON` in `api/drift.go`): when a reply is
  not JSON, the engine hands the model its own reply plus the parse error and
  asks ONCE more. Every drift/gaps/extract/match/plan/remedy/create/linker/
  speccy-draft JSON path goes through these two helpers — do not call
  `StreamTools`+`ExtractJSON` directly.
- `ai.ExtractJSON` decodes the first object that FITS the target's json tags,
  NOT first-`{`-to-last-`}` — models emit a trailing second object (which made
  the span invalid: "invalid character '{' after top-level value") or a
  preamble object (which decoded into an empty struct and silently turned a
  failed call into a zero-finding result).

## Server logs

Beyond the `METHOD /path duration` request line, the server narrates the work
itself — read `/tmp/…` or journald when a run "does nothing":

- **`ai: <model> [<label>] complete|tool loop in <dur> (rounds, tools, sizes)`**
  — every model call, with WHAT it was for. The label rides on the context
  (`ai.WithLabel`, `internal/ai/label.go`): `drift specs/x.md`,
  `extract survey ~src`, `extract area <name>`, `match 1-8`, `gaps ~src`,
  `plan <fp>`, `create <kind> <path>`, `remedy <kind>`, `linker propose`,
  `focus areas`, `speccy chat|draft`. Sizes only — prompts carry workspace
  content and never enter the log.
- **`drift [<repo>@<branch>]: run N …`** — start (mode, units, sources,
  report + branch, focus), one line per unit (findings, dropped, duration),
  and the finish (status, live findings, dropped, failed units). Plus every
  action: filed, drafted, remedy, created, extracted, planned, cancelled.
- **`linker [<repo>@<branch>]:`** — proposed/applied counts and validation drops.
