# reqbase — project notes for Claude

Git-native requirements engineering: markdown + typed frontmatter links in git,
Go single binary (`server/`) + React SPA (`web/`). Read `README.md` first for
the architecture; this file is operational knowledge that is NOT derivable
from the code.

## Dev environment

- Server runs on **:8643** (8080 is a Java app, 8642 a dead socket); tailnet:
  `http://tt.warg-snares.ts.net:8643`.
- Start: `./server/reqbased -config reqbase.dev.yml -dev` — the `-dev` flag
  auto-authenticates every request as `auth.dev_user` ("Flo Dev", workspace
  branch `ws/dev`) and bypasses session TTLs.
- **The SPA is embedded in the Go binary.** After `cd web && npm run build`
  you MUST `cd server && go build -o reqbased ./cmd/reqbased` and restart, or
  the browser silently serves the stale build.
- `pkill reqbased` matches the wrapper shell (exit 143) — use `pkill -x reqbased`.
- Full state reset: `pkill -x reqbased; rm -rf data/runtime && ./scripts/dev-fixture.sh`.
  SQLite (sessions, PRs, collab room logs) lives in `data/runtime/reqbase.db`;
  bare fixture origins in `data/origin/`.
- Copilot in dev points at ollama `qwen2.5:7b` (`reqbase.dev.yml`);
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

- **Protected main**: the default branch is never edited; the first edit
  auto-creates/switches to the caller's `ws/<user>` branch (claimed in SQLite).
  Direct writes to protected branches 403 (`protected_branch`).
- **Worktree = draft store**: saves are uncommitted changes on a per-branch
  worktree; explicit Commit turns them into history.
- **Commit identity**: the logged-in user is **author AND committer**; the
  service identity (`git.committer_name/email`) is appended as a
  `Co-authored-by:` trailer, alongside trailers for collab contributors.
- **CRDT co-editing**: markdown files in edit mode join a Yjs room per
  (branch, path). The server is a dumb relay (`internal/collab`) — opaque
  update log in SQLite, replay to joiners, leader flushes serialized markdown
  to the worktree. While a room is live it OWNS the file: direct PUTs 409
  (`room_active`), pulls/workspace-ffs on that branch are withheld.
- **Byte fidelity**: untouched documents save byte-identical; only real user
  edits normalize markdown.
- **Sketches**: `*.excalidraw.png` — PNGs with the excalidraw scene embedded
  (export-embed-scene), natively viewable anywhere, editable in the modal via
  `loadFromBlob`/`exportToBlob`. Legacy `*.excalidraw` JSON still supported.
- **AI tiers**: `ai.model` (thinking-class: chat, draft edits) vs
  `ai.quick_model` (one-shot: commit messages). Both through any
  OpenAI-compatible endpoint. `.reqbase/skills/*.md` in the workspace are
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

## Deferred / planned

- GitHub App integration (login + installation repos as workspaces) — planned,
  blocked on an app registration (contents:rw, pull_requests:rw, metadata:r).
  `gitx.credentialArgsEnv` is the single credentials seam to extend.
