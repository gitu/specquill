# SpecQuill

**Requirements as readable, structured Markdown.** A git-native
requirements-engineering tool: requirements, specs, regulations, data mappings
and change records live as plain markdown in git; SpecQuill is the editing
and review surface on top: traceability graph & matrix, change inbox, rich editors, and
an in-app branch-based PR flow — every commit authored by the logged-in user.

Originally implemented from the Claude Design project
[`SpecQuill.dc.html`](design/SpecQuill.dc.html) (the static prototype it grew from lives in
[`design/prototype/`](design/prototype/)).

## Architecture

```
server/           Go single binary (specquill)
  internal/gitx     the only git surface: bare clone + per-branch worktrees,
                    status/commit (user = author & committer, service identity
                    as Co-authored-by), structured diffs, merge-tree merges,
                    env-token push/fetch
  internal/auth     OIDC (code+PKCE, coreos/go-oidc) + local argon2id fallback,
                    opaque session cookies in Postgres
  internal/store    Postgres (pgx; Neon in prod): users, sessions, PRs,
                    comments, approvals, workspace claims, collab room
                    logs — content never leaves git
  internal/collab   real-time co-editing relay: the server is a dumb Yjs
                    update log (no server-side CRDT) — rooms per
                    (branch, path), seed handshake, replay to joiners,
                    leader-snapshot compaction, flush-to-worktree
  internal/api      REST under /api + embedded SPA (embed.FS)
web/              React + Vite + TypeScript SPA
  src/lib/model.ts  frontmatter/link parsing → workspace model (all client-side)
  src/editors/      Milkdown WYSIWYG (mermaid click-to-edit node view,
                    excalidraw embeds), CodeMirror 6 source mode,
                    schema-driven PropertiesForm (yaml Document API),
                    @excalidraw/excalidraw modal
  src/collab/       CollabSession (Y.Doc + awareness + websocket provider,
                    cached across remounts), Milkdown collab binding
repo/             demo "trading-specs" workspace (fixture source)
```

Key properties:

- **The server never parses frontmatter** — it serves files + git operations; the model
  (graph, matrix, dashboards) is computed in the browser from a `/snapshot` of the branch.
- **Protected main, personal workspaces.** The default branch is never edited directly:
  the first edit transparently creates/switches to the user's `ws/<user>` branch
  (server-claimed, fast-forwarded onto main when safe). Direct API writes to protected
  branches 403. Drafts autosave to the branch worktree (debounced), survive branch
  switches and navigation (localStorage recovery + unload keepalive), and an explicit
  Commit turns them into history. Tree badges are real `git status`; opening a PR
  prompts to commit pending changes.
- **Real-time co-editing (CRDT).** Editing a markdown file in WYSIWYG mode joins a Yjs
  room per (branch, file): live text sync, named cursors, presence dots in the tree,
  invite links ("Switch & join"). The room owns the file while live — direct PUTs 409,
  pulls/ffs are refused on roomed branches — and the leader client flushes the merged
  doc to the worktree. Commits run a flush barrier and append `Co-authored-by:`
  trailers for every room contributor. Unflushed sessions (crash) are surfaced as
  orphaned rooms and recovered on next open.
- **PRs are branches.** Review (diff, inline comments, approvals pinned to the head sha)
  lives in the app; merge uses `git merge-tree` (merge commit or squash) with conflicts
  detected and blocked. No forge API involved; `push`/`fetch` sync the plain remote with
  a token from the environment.
- **Honest git identity.** The logged-in user is both **author and committer** on every
  commit and merge; the SpecQuill service identity is recorded as a `Co-authored-by:`
  trailer instead, alongside trailers for live co-editing contributors.
- **Byte-fidelity editing.** Untouched documents save byte-identical; frontmatter edits
  go through the `yaml` Document API (comments/formatting preserved); WYSIWYG edits
  normalize markdown to house style (covered by a golden round-trip suite).
- **Rich WYSIWYG.** Slash-command menu (`/` inserts headings, lists, task lists,
  quotes, tables, dividers, code/mermaid blocks, images, sketches), floating selection
  toolbar (bold/italic/strike/code/link), link dialog (Ctrl+K, hover to preview/edit),
  table editing controls (add/remove/align/drag rows & columns), a collapsible outline
  panel with click-to-jump, markdown-aware clipboard, and inline formatting via
  fixed toolbar, ⌘B/⌘I, or markdown syntax. **Images**: paste, drag-drop, or upload —
  files land in `<docdir>/assets/` on the branch worktree (`POST /assets`, served raw
  via `GET /raw/{path}`), embedded as doc-relative markdown. In edit mode internal
  links follow on Ctrl/Cmd+click (plain click places the cursor).
- **Sketches are PNGs.** New excalidraw sketches save as `*.excalidraw.png` — a real
  PNG with the scene JSON embedded (excalidraw's export-embed-scene), so they render
  natively anywhere git renders images (GitHub included) and stay fully editable in
  the built-in sketch editor. Legacy `*.excalidraw` JSON files keep working.
- **Sessions idle out after 10 minutes** without a request (sliding expiry server-side;
  `session.ttl` in config). The cookie is a browser-session cookie — activity keeps you
  signed in indefinitely.
- **Responsive reading.** Under 900px the rail/tree/copilot collapse (tree becomes a
  hamburger drawer, copilot an overlay) and documents read full-width.
- **Read-only input repos** (e.g. a regulations repo) are fetched on an interval,
  browsable in the tree (🔒), and refuse writes server-side.
- **OKF bundles.** Workspaces conform to the
  [Open Knowledge Format](docs/okf.md) (v0.1): every document carries a
  `type`, and opted-in bundles get `index.md` listings + a `log.md` change
  history regenerated on every commit — readable by any OKF consumer or
  agent straight from git. Untyped OKF body links show up as dashed
  reference edges in the traceability graph.
- **Workspace onboarding.** `specquill init <dir> [-types requirements,specs,changes,…]`
  scaffolds a new workspace repo: folder skeleton per chosen document family
  (requirements, specs, regulations, data-mappings, changes, decisions, glossary),
  the `.specquill/schema.json` property schema, starter documents, a server-config
  stub — and **AI authoring skills** under `.specquill/skills/` that the copilot pins
  into its system prompt, so it drafts requirements/specs following your house rules.
- **Two model tiers.** `ai.model` is the main (thinking-class) tier for chat and
  draft edits; `ai.quick_model` is a fast one-shot tier for small tasks. Commit
  messages are auto-drafted from the uncommitted diff on the quick tier
  (`POST /commit-message`) and prefill the commit dialog — editable, regenerable,
  never overwriting what you typed. `<think>…</think>` reasoning tags are stripped.
- **Copilot** (`ai:` config) talks to any **OpenAI-compatible** chat endpoint —
  OpenAI, Gemini (`…/v1beta/openai`), Azure, Ollama — with the branch snapshot as
  grounding (no index; the workspace is prompt-sized). Chat streams over SSE;
  "Draft edits & open as diff" asks the model for surgical search/replace edits,
  validates them (impacted files only, unique match), and applies them as
  **uncommitted saves on a `copilot/<change>` branch** — the human reviews via the
  normal status → commit → PR flow. `scripts/mock-llm.py` is a keyless dev provider.

## Run (dev)

```sh
make dev-fixture        # local bare origins under data/origin/ from repo/
                        # (also starts + resets the compose postgres, the store)
make web server         # build SPA into the embed dir + build specquill
python3 scripts/mock-llm.py &          # keyless copilot provider for dev
./server/specquill -config specquill.dev.yml -dev
# → http://localhost:8643  (dev flag auto-authenticates as auth.dev_user)
```

Frontend dev loop with HMR: `cd web && npm run dev` (Vite on :5173, proxying /api).

## Run (production-ish)

```sh
cp specquill.example.yml specquill.yml     # point at your remotes, OIDC issuer, data dir
export SPECQUILL_DATABASE_URL=…          # Postgres DSN (e.g. Neon), env only
export SPECQUILL_TOKEN_TRADING=…         # git token, env only
export SPECQUILL_OIDC_SECRET=…
make build && ./server/specquill -config specquill.yml
./server/specquill -config specquill.yml user add flo 'Flo' flo@example.com   # local fallback user
```

Requirements: `git` ≥ 2.38 on the server (checked at startup). Exactly one `writable`
repo plus any number of `readonly` ones. The user's OIDC `name`/`email` claims become
the git author on every commit and merge.

## Verify

```sh
make test               # Go: gitx/auth/API suites · web: model, frontmatter, Milkdown round-trip
make e2e                # Playwright against a running dev server: edit → commit → PR → merge
python3 scripts/verify-write-path.py   # API-level write/commit/push/409 checks
python3 scripts/verify-pr-flow.py      # API-level PR lifecycle incl. conflict blocking
docker compose -f docker-compose.dev.yml up -d   # dex IdP for exercising real OIDC
```

## Deploy

`Dockerfile` builds the whole thing into one alpine+git image;
[`DEPLOY.md`](DEPLOY.md) documents the Cloud Run pipeline (GitHub Actions →
ghcr.io → deploy-only Cloud Build trigger → Cloud Run, staging on `main`,
prod on `v*` tags).

## Notes & future work

- **GitHub App integration is planned** (next round; needs an app registration): GitHub
  login beside OIDC, installation repos as workspace/reference repos, installation
  tokens through the existing `credentialArgsEnv` seam. Co-author trailers already give
  correct multi-avatar attribution on GitHub.
- Collab protocol notes: never mutate a Y.Doc before its socket is open (pre-sync local
  items never transmit and every later edit references clock ranges peers never
  received); the seeder initializes shared metadata after the seed grant and pushes
  full state.
- Copilot grounding is whole-snapshot prompting — fine at workspace scale; a retrieval
  index would be needed for large corpora or multi-repo grounding.
- Read-only repos are browse-only inputs; federating them into the traceability model
  (cross-repo `drives` links) is future work.
- Conflicting PRs are blocked with the conflicted paths listed; materializing the
  conflict into the source worktree for in-app resolution is future work.
