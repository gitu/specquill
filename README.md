# SpecQuill

**Requirements as readable, structured Markdown — what you end up with is an
[OKF bundle](repo-product/docs/specs/specs/okf.md).** A git-native requirements-engineering tool:
requirements, specs, regulations and data mappings live as plain markdown in
git; SpecQuill is the editing and review surface on top — traceability graph,
timed dependencies, a git-derived change history, rich editors, and an in-app
branch-based merge flow, every commit authored by the logged-in user.

The artifact SpecQuill produces is deliberately **not proprietary**: a
workspace is a conformant **Open Knowledge Format (v0.1) bundle** — typed
frontmatter, generated `index.md`/`log.md`, plain relative links — fully
readable by humans, agents and any OKF consumer straight from git, with or
without SpecQuill running. Hand the whole bundle to an LLM as one zip via an
unauthenticated [share link](repo-product/docs/specs/specs/share-links.md).

Originally implemented from the Claude Design project
[`SpecQuill.dc.html`](design/SpecQuill.dc.html) (the static prototype it grew from lives in
[`design/prototype/`](design/prototype/)).

## Screenshots

A tour of every surface lives in
[`docs/screenshots/`](docs/screenshots/README.md) — editor, speccy, source
alignment, impact graph, timed dependencies, change history, and more.
Regenerate the gallery with `make shots` (isolated server + demo fixtures +
mock LLM, so no keys are needed).

[![Editor](docs/screenshots/editor.png)](docs/screenshots/README.md)

## Architecture

```
server/           Go single binary (specquill)
  internal/gitx     the only git surface: bare clone + per-branch worktrees,
                    status/commit (user = author & committer, service identity
                    as Co-authored-by), structured diffs, merge-tree merges,
                    env-token push/fetch
  internal/auth     forge-PAT login (GitLab/GitHub personal access tokens,
                    RAM-only token vault) + local argon2id fallback,
                    opaque session cookies in the store
  internal/store    embedded SQLite (modernc, cgo-free) at <data_dir>/specquill.db:
                    users, sessions, per-repo grants, workspace claims —
                    content never leaves git
  internal/api      REST under /api + embedded SPA (embed.FS)
web/              React + Vite + TypeScript SPA
  src/lib/model.ts  frontmatter/link parsing → workspace model (all client-side)
  src/editors/      Milkdown WYSIWYG (mermaid click-to-edit node view,
                    excalidraw embeds), CodeMirror 6 source mode,
                    schema-driven PropertiesForm (yaml Document API),
                    @excalidraw/excalidraw modal
repo/             demo "trading-specs" workspace (fixture source)
```

Key properties:

- **The server never parses frontmatter** — it serves files + git operations; the model
  (graph, dashboards) is computed in the browser from a `/snapshot` of the branch.
- **The type model is configuration, not code.** Document families sit on a
  WHY → WHAT → HOW → WHEN axis: drivers (regulation, product, technical) explain WHY
  work exists, requirements say WHAT the product must do, specs say HOW it is
  realized, work items say WHEN it lands. The whole type system — entities, drivers,
  statuses, link types, ID schemes and the property schema — lives in ONE optional
  file, `.specquill/config.yml`; every section it omits runs on the built-in
  defaults, entities merge (override single fields, add families, or drop one with
  `hidden: true`), and the Model view shows a **sample config** spelling out the full
  default setup — importable in one click when no config exists. A stand-alone
  `.specquill/schema.json` keeps working as the legacy property-schema form.
- **Timed dependencies** ([REQ-026](repo-product/docs/specs/requirements/REQ-026.md)).
  A document whose frontmatter carries a validity window — `starts`/`ends`, or
  regulatory wording like `effective_from`, all configurable under `timed:` —
  lands on a timeline as pending / active / expiring / expired, together with the
  readiness of everything that links to it. A window that opens inside the horizon
  while its dependents are still unfinished is flagged **at risk** on the Overview
  and as a badge on the rail. No document *about* change is required: the dates
  live on the documents themselves.
- **Change history from git** ([REQ-027](repo-product/docs/specs/requirements/REQ-027.md)).
  `/history` reads the workspace's commits (content-root scoped) and classifies every
  touched path through the current config, so the feed reads "3 requirements · 1 spec"
  rather than a file list. A selected commit is explained as a **semantic delta** —
  frontmatter properties that moved, normative statements added, dropped or reworded,
  sections that came and went — with the text diff one click away and, when an AI tier
  is configured, a cached one-sentence summary generated from that delta. `/changes` is
  the branch-scoped counterpart: uncommitted drafts, commits ahead of main, open MR.
- **Protected main, personal workspaces.** The default branch is never edited directly:
  the first edit transparently creates/switches to the user's `ws/<user>` branch
  (server-claimed, fast-forwarded onto main when safe). Direct API writes to protected
  branches 403. Drafts autosave to the branch worktree (debounced), survive branch
  switches and navigation (localStorage recovery + unload keepalive), and an explicit
  Commit turns them into history. Tree badges are real `git status`; merging
  prompts to commit pending changes first.
- **State lives in git; the database is bookkeeping.** Drafts are uncommitted
  changes on a per-branch worktree, history is git commits — SQLite holds
  identity, sessions, grants and workspace claims, never documents. Concurrent
  saves of the same file are guarded by a `baseSha` precondition: the later
  writer gets a 409 and a "file changed — reload" prompt instead of silently
  clobbering.
- **Two ways to land on main.** Local-auth deployments merge directly in-app: a
  workspace branch lands on the protected default branch through a previewed merge
  (diff + conflict check + dirty-worktree refusal); `git merge-tree` does the work
  as a merge commit or squash. Forge-PAT deployments instead **propose**: the branch
  is pushed with the user's own token and a merge request / pull request is opened
  via the forge API (idempotent — re-proposing pushes onto the open MR); review and
  the merge happen on the forge, and main comes back via fetch.
- **Forge-PAT auth (`auth.forge`).** Users sign in with a personal access token from
  the deployment's GitLab/GitHub; identity comes from the forge `/user` API and the
  deployment role from the user's actual permission on the main project. The token
  lives in the browser's localStorage and, per session, in a RAM-only server vault —
  never in the database. Every user gets **fully independent server-side clones**
  fetched with their own token, so nothing one token can reach ever leaks to another
  user. Reference sources are defined in-repo (`.specquill/config.yml` `sources:`) —
  listing one there grants nothing; the user's own forge permission is the gate.
- **Honest git identity.** The logged-in user is both **author and committer** on every
  commit and merge; the SpecQuill service identity is recorded as a `Co-authored-by:`
  trailer instead.
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
- **Responsive reading.** Under 900px the rail/tree/speccy collapse (tree becomes a
  hamburger drawer, speccy an overlay) and documents read full-width.
- **Read-only input repos** (e.g. a regulations repo) are fetched on an interval,
  browsable in the tree (🔒), and refuse writes server-side.
- **OKF bundles.** Workspaces conform to the
  [Open Knowledge Format](repo-product/docs/specs/specs/okf.md) (v0.1): every document carries a
  `type`, and opted-in bundles get `index.md` listings regenerated on every
  commit — readable by any OKF consumer or agent straight from git. The
  `log.md` change history is NOT materialized in the repo (git is the
  history): it is generated on the fly and injected only when the OKF bundle
  is exported through a share link. Untyped OKF body links show up as dashed
  reference edges in the traceability graph.
- **Workspace onboarding.** `specquill init <dir> [-types requirements,specs,changes,…]`
  scaffolds a new workspace repo: folder skeleton per chosen document family
  (requirements, specs, regulations, data-mappings, changes, work-items, decisions, glossary),
  the combined `.specquill/config.yml` (model + property schema), starter documents, a server-config
  stub — and the speccy's workspace-side brain: **authoring skills** under
  `.specquill/skills/`, an **instructions** starter (`.specquill/instructions.md`,
  with `speccy.instructions` in `config.yml` as the short inline form) and the
  **project memory** convention (`.specquill/memory/`, one decision per file).
  All of it is pinned into the system prompt, versioned in git, and reviewed
  like any other change.
- **Two model tiers.** `ai.model` is the main (thinking-class) tier for chat and
  draft edits; `ai.quick_model` is a fast one-shot tier for small tasks. Commit
  messages are auto-drafted from the uncommitted diff on the quick tier
  (`POST /commit-message`) and prefill the commit dialog — editable, regenerable,
  never overwriting what you typed. `<think>…</think>` reasoning tags are stripped.
- **Speccy** (`ai:` config) talks to any **OpenAI-compatible** chat endpoint —
  OpenAI, Gemini (`…/v1beta/openai`), Azure, Ollama — with the branch snapshot as
  grounding (no index; the workspace is prompt-sized). Chat streams over SSE;
  "Draft edits & open as diff" asks the model for surgical search/replace edits,
  validates them (impacted files only, unique match), and applies them as
  **uncommitted saves on a `speccy/<doc>` branch** — the human reviews via the
  normal status → commit → merge flow. `scripts/mock-llm.py` is a keyless dev provider.
- **Chat tools.** On a writable workspace branch the chat can act directly:
  `read_file`/`list_files`/`search` (full files, listings and text search over
  the workspace AND **every selected reference source** — `grounding: true`
  only decides which sources are additionally excerpted into the prompt, so
  large implementation repos stay explorable without prompt-stuffing),
  `edit_file`/`create_file` (unique search/replace or new documents —
  always **uncommitted drafts** on the current branch, never on protected ones;
  frontmatter must still parse and `created:`/`updated:` are maintained
  server-side), and `ask_user` (a clarifying question with option chips that
  pauses the conversation). Tool descriptions carry the workspace's own
  vocabulary — statuses, schema enums, family folders, ID patterns. Extra
  authoring rules live in `.specquill/instructions.md` and/or
  `speccy.instructions` in `.specquill/config.yml`, pinned into every prompt
  next to the skills. Speccy interviews rather than assumes: undefined
  behavior becomes pointed `ask_user` questions grounded in what the
  referenced repositories already do, and durable answers are persisted as
  **project memory** — one decision per file under `.specquill/memory/`
  (merge-friendly by construction), pinned above the specs in every
  conversation and reviewed/committed like any other workspace change.

## Run (dev)

```sh
make dev-fixture        # local bare origins under data/origin/ from repo/
                        # (also drops the store so it can't outlive the fixtures)
make web server         # build SPA into the embed dir + build specquill
python3 scripts/mock-llm.py &          # keyless speccy provider for dev
./server/specquill -config specquill.dev.yml -dev
# → http://localhost:8643  (dev flag auto-authenticates as auth.dev_user)
```

Frontend dev loop with HMR: `cd web && npm run dev` (Vite on 127.0.0.1:5643, proxying /api). A server started with `-dev` reverse-proxies the SPA routes on :8643 to vite while it runs — :8643 never serves a stale build in dev — and falls back to the embedded build when vite is down.

## Run (production-ish)

```sh
make build && ./server/specquill setup     # interactive wizard writes specquill.yml
# (or: cp specquill.example.yml specquill.yml and edit — running the server
#  without any config offers the wizard too)
./server/specquill -config specquill.yml
# forge-PAT mode needs no server-side credentials at all — users bring their own
# tokens. Local-auth mode instead: export the token_env vars and add users with
./server/specquill -config specquill.yml user add flo 'Flo' flo@example.com
```

Requirements: `git` ≥ 2.38 on the server (checked at startup). Exactly one `writable`
repo plus any number of `readonly` ones. The forge identity's `name`/`email` become
the git author on every commit.

## Configuration — what lives where

Two auth modes, two splits. The rule of thumb: **credentials and identity follow the
mode; content-shaped settings live in the repo.**

**Forge-PAT mode (`auth.forge`, the v1 deployment)** — the server config is minimal
and credential-free; access rides each user's own token:

| lives in server YAML | lives in `.specquill/config.yml` (in the repo) | lives with the user |
|---|---|---|
| forge kind + base URL (`auth.forge`) | reference **source definitions** (`sources:` — name, https remote on an allowlisted host, branch) | the PAT (browser localStorage + RAM-only session vault) |
| the workspace repo (`projects:` — remote, default branch, content root) | reference **selection** (`references:` — paths filter, `grounding:` = prompt excerpting; every selected source is chat-tool-explorable) | identity + git author (forge `/user`) |
| optional: scopes / token-creation link overrides, `admin_emails`, `default_role` floor | taxonomy, entities, views, schema — and the speccy's brain: skills, `speccy.instructions`, `instructions.md`, `memory/` | deployment role (forge permission on the main project, refreshed each login) |
| **no tokens, no source catalog** (a top-level `sources:` block is rejected) | | per-user clones under `data/…/repos/u<id>/` |

**Local-auth mode (`auth.local`, the v2 developer setup)** — the server owns shared
credentials, so source definitions must stay server-side: the YAML carries the source
**catalog** (git + url/openapi/confluence importers) with `token_env` env-var
credentials, and the in-repo config only **selects** cataloged sources by name
(selection ∩ catalog — in-repo config can never mint access). In-app merges,
boot clones and background sync loops exist only in this mode.

The authoritative version of this table is
[`specs/forge-auth.md`](repo-product/docs/specs/specs/forge-auth.md); the
authorization reasoning is [`REQ-004`](repo-product/docs/specs/requirements/REQ-004.md)
and [`REQ-024`](repo-product/docs/specs/requirements/REQ-024.md).

### Example: forge-PAT deployment with reference sources

A specs workspace grounded on regulatory texts, with the implementation repo
that is *built from* these specs selected as a read-only source — so the speccy
can check the code against the requirements (drift detection).

Server YAML — minimal and credential-free; the top-level `sources:` block must
stay empty in this mode:

```yaml
listen: ":8080"
data_dir: /var/lib/specquill
base_url: https://specquill.acme.com

projects:
  - id: trading-specs
    remote: https://gitlab.acme.com/trading/trading-specs.git
    default_branch: main

auth:
  forge:
    kind: gitlab
    base_url: https://gitlab.acme.com
    # in-repo source remotes may only name the forge/project hosts;
    # extra hosts (e.g. a public mirror) must be listed here
    allowed_source_hosts: [gitlab.esma-mirror.org]
  admin_emails: [ops@acme.com]

ai:
  enabled: true
  base_url: https://api.openai.com/v1
  model: gpt-4o
  quick_model: gpt-4o-mini
  api_key_env: SPECQUILL_AI_KEY
```

`.specquill/config.yml` in the workspace repo — sources are **defined** here
(git repos, https only, no credentials; each user fetches them with their own
PAT, so a definition never mints access) and **selected** under `references:`:

```yaml
version: 2
project: trading-specs
default_branch: main

sources:
  - name: regulations            # regulatory texts the requirements derive from
    remote: https://gitlab.acme.com/compliance/regulations.git
  - name: esma-rts               # public mirror — needs the allowed_source_hosts entry
    remote: https://gitlab.esma-mirror.org/esma/rts-texts.git
    default_branch: master
  - name: trading-platform       # the implementation built from these specs
    remote: https://gitlab.acme.com/trading/trading-platform.git

references:
  # small, load-bearing texts: pin into the speccy system prompt
  - source: regulations
    grounding: true
  - source: esma-rts
    grounding: true
    paths: [rts22/]              # grounding-only prefix filter
  # the implementation is too big to prompt-stuff: no grounding — the speccy
  # still reads it on demand via its list_files/search/read_file tools
  - source: trading-platform
```

`grounding: true` only decides what gets excerpted into the prompt; every
selected source is fully explorable through the chat tools regardless.

## Verify

```sh
make test               # Go: gitx/auth/API suites · web: model, frontmatter, Milkdown round-trip
make e2e                # Playwright against a running dev server: edit → commit → merge
python3 scripts/verify-write-path.py   # API-level write/commit/push/409 checks
python3 scripts/mock-forge.py &        # mock GitLab for exercising forge-PAT auth
```

## Deploy

`Dockerfile` builds the whole thing into one alpine+git image (pushed to
ghcr.io on every push to `main` and every tag); [`DEPLOY.md`](DEPLOY.md)
documents self-hosting it — one binary or container, one YAML file, a
persistent directory, a reverse proxy.

## Notes & future work

- Speccy grounding is whole-snapshot prompting — fine at workspace scale; a retrieval
  index would be needed for large corpora or multi-repo grounding.
- Read-only repos are browse-only inputs; federating them into the traceability model
  (cross-repo `drives` links) is future work.
- Conflicting PRs are blocked with the conflicted paths listed; materializing the
  conflict into the source worktree for in-app resolution is future work.
