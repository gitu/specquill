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
  air ignores chmod-only events), and vite HMR on :5173 (proxies /api+/auth,
  ws included). In this mode browse the **vite port** — :8643 still serves
  whatever SPA was last embedded. E2E still needs the embedded build
  (`make build`).
- **The SPA is embedded in the Go binary.** After `cd web && npm run build`
  you MUST `cd server && go build -o specquill ./cmd/specquill` and restart, or
  the browser silently serves the stale build.
- `pkill specquill` matches the wrapper shell (exit 143) — use `pkill -x specquill`.
- **The store is embedded SQLite** (users, sessions, workspace claims, collab room logs) at
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
  sessions/merge state/collab logs too (the fixture script also deletes the DB, so
  fixtures and store can't drift apart).
- Copilot in dev points at ollama `qwen2.5:7b` (`specquill.dev.yml`);
  `scripts/mock-llm.py` (:8991) is the keyless provider the copilot e2e needs
  (it self-skips unless the configured model is `mock-1`).

## Testing

- Go: `cd server && go test ./...`
- Unit: `cd web && npx vitest run`
- E2E: `cd web && npx playwright test` — MUST run from `web/` (running from
  the repo root loads a second @playwright/test and fails weirdly). Requires a
  running dev server built from the current source (see embedded-SPA note).
- Screenshot specs are gated behind `SHOT=1`.
- E2E state discipline: tests self-heal or use unique per-run file names
  (`scratch-*-<stamp>.md`). Failed collab runs can leave orphaned room logs —
  presence polls in cleanups must count rooms with `users.length > 0` only.

## Domain model / invariants

- **Projects vs sources** (config-split): a **project** is a writable workspace
  = git repo + optional `content_root` subfolder (monorepo). A **source** is a
  read-only catalog entry projects reference. `internal/project` is the ONLY
  place project-relative ↔ full repo paths are mapped (MapIn/MapOut); store rows
  and git ops use full paths, the wire format is project-relative.
- **3-stage authorization** (single-tenant): (1) catalog sources+credentials in
  app YAML/admin, (2) in-repo `.specquill/config.yml` `references:` SELECT
  cataloged sources (read from the DEFAULT branch only), (3) deployment roles
  viewer<member<admin (`users.role`) plus per-repo grants. In-repo config can
  only select cataloged sources — it can NEVER mint access.
  `EffectiveReferences` = selection ∩ catalog.
- **Copilot grounding**: grounded reference sources join the system prompt under
  `## ~source/path` read-only headings (workspace keeps a 60% budget floor);
  draft edits refuse any `~`-prefixed path.
- **Non-git sources = importer mirror repos**: url/openapi/confluence sources are
  remote-less gitx repos (`Mirror: true`, `git init --bare`) that `internal/
  importer`'s Runner populates via `SnapshotMirror` (full-tree bare-repo commit,
  idempotent). Credentials are env-only via `token_env`; `email:token` → HTTP
  Basic (Atlassian Cloud), a bare token → Bearer. **Dev quirk**: the demo
  `platform-api` openapi source self-fetches `http://127.0.0.1:8643/demo-openapi.
  json`, so its boot import errors ("connection refused") before the listener is
  up — it goes green on the next interval or a manual `POST /api/sources/platform-api/sync`
  (or the Admin "Sync now" button).
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
  `Co-authored-by:` trailer, alongside trailers for collab contributors.
- **CRDT co-editing**: markdown files in edit mode join a Yjs room per
  (branch, path). The server is a dumb relay (`internal/collab`) — opaque
  update log in the store, replay to joiners, leader flushes serialized markdown
  to the worktree. While a room is live it OWNS the file: direct PUTs 409
  (`room_active`), pulls/workspace-ffs on that branch are withheld.
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
  pinned into the copilot system prompt as authoring rules.

## Hard-won gotchas (do not rediscover these)

- **Yjs pre-open mutation trap**: never mutate a Y.Doc before its websocket
  is open (and, for joiners, before replay applied). Pre-open local items
  never transmit; every later edit references clock ranges peers never got —
  held as `pendingStructs` forever, silent one-way divergence. The seeder
  initializes shared metadata in its seed-grant handler and pushes
  `encodeStateAsUpdate` afterwards.
- **Never replace ProseMirror node-view DOM** (e.g. `img.replaceWith(...)`):
  PM re-parses and deletes the node from the document. Mutate the existing
  element (swap `src`, add classes) instead.
- **Milkdown listener debounce**: even `listener.updated` is debounced; the
  undebounced truth for "user typed" is a DOM `input` listener on the
  contenteditable.
- **Slash/tooltip providers + collab**: y-sync echoes no-op transactions after
  every keystroke; the providers' lodash debounce keeps only the LAST call's
  args and their `isSame` guard then discards the real edit. Filter no-op
  transactions before calling `provider.update` (see `richtools.ts`).
- **Session acquisition must live in an effect** (`useCollabSession`):
  acquiring during render leaks a refcount on aborted renders → websocket and
  server-side room stay alive forever.
- **Toolbar flex**: every control cluster needs `flex:none`; otherwise the
  overflowing toolbar silently crushes the weakest item to ~2px. The path
  label is the designated shrink/ellipsis element.
- `sx()` converts inline-style strings to React style objects; components
  carry design styles as strings on purpose — keep that idiom.

## Deployment model

- Two supported deployments (July 2026 decision): **v1** — one deployment per
  tenant for BAs/non-technicals, a single writable repository, any-OIDC login;
  **v2** — a developer's local machine, `auth.local`/`-dev`, no OIDC. The
  GitHub integration (OAuth login, GitHub App tenants, webhooks) was removed
  with this decision; GitHub-hosted repos are plain git remotes via
  `token_env` — `gitx.credentialArgsEnv` is the single credentials seam.
