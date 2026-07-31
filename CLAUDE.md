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
