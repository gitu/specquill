---
type: Specification
title: Forge-PAT auth — tokens, per-user clones, in-repo sources
status: in_review
satisfies: [requirements/REQ-024.md, requirements/REQ-004.md]
updated: 2026-07-27
---

# Forge-PAT auth — tokens, per-user clones, in-repo sources

How [REQ-024](../requirements/REQ-024.md) (and the forge-mode side of
[REQ-004](../requirements/REQ-004.md)) is realized. Enabled by `auth.forge:`
in the server config; [authentication.md](authentication.md) covers the
provider surface, this spec the mechanics behind it.

## What is configured where

The defining property of this mode is a **minimal, credential-free server
config**: the installation says *which forge* and *which workspace repo*;
everything else — access, identity, reference sources — travels with tokens
and the repo itself.

| concern | server YAML (`specquill.yml`) | in-repo (`.specquill/config.yml`) | per user |
|---|---|---|---|
| forge kind + base URL | `auth.forge: {kind, base_url}` | — | — |
| token scopes / creation link | optional overrides (`scopes`, `token_create_url`) | — | — |
| workspace repo (remote, default branch, content root) | `projects:` (exactly one) | — | — |
| credentials | **none** | **never** | the PAT (browser localStorage + RAM-only session vault) |
| identity, git author | — | — | forge account (`/user`) |
| deployment role | optional floor/override (`auth.default_role`, `auth.admin_emails`) | — | forge permission on the main project, refreshed each login |
| reference sources (definitions) | — (a top-level `sources:` block is rejected) | `sources:` — name + **https** remote + branch | cloned lazily with the user's own token |
| reference selection / grounding | — | `references:` (paths, grounding), default branch only | — |
| merges to main | — | — | forge MR/PR via `POST /propose` |

Local-auth deployments (v2) keep the inverse split: catalog + `token_env`
credentials in the server YAML, in-repo config selects only
([references.md](references.md)).

## Login

`POST /auth/pat/login {token}` verifies the token against the forge's
identity endpoint (GitLab `PRIVATE-TOKEN`, GitHub bearer). The forge's
stable numeric user id keys the `users` row; name/email become the git
author. GitHub identities with a hidden email fall back to the verified
primary from `/user/emails`, then to the documented noreply address —
email never blocks login.

The deployment role is read from the token's permission on the main
project (GitHub `permissions` booleans; GitLab access level: 30 developer →
editor, 40 maintainer → maintainer, 50 owner → admin; below → viewer) and
written to `users.role` on every login, so permission changes on the forge
propagate at the next sign-in. A token that cannot read the main project is
rejected with a pointer to request access. `auth.admin_emails` still floors
matching users to admin.

## Token lifetime

The token's persistent home is the **browser** (localStorage). Server-side
it exists only in a RAM vault keyed by session id: written at login, read
per request, deleted at logout, gone on restart. Entries are also dropped
when a request arrives on a session that no longer resolves, and swept when
untouched for longer than the session's idle lifetime — a browser that never
comes back cannot pin a token forever. A session whose vault entry is
missing answers 401, and the SPA silently re-plays login with the stored
token and retries — restarts and session expiry are invisible while the
token remains valid. The browser only discards its token when the forge
itself rejects it (401/403); a 5xx or a dead network keeps it, so an outage
never forces everyone to mint new tokens. Nothing token-shaped ever reaches
SQLite or disk.

From the vault the token travels **per operation**: every git call takes it
as an argument rather than reading it from shared state, so two sessions of
the same user holding different tokens can run concurrently without one
borrowing the other's credential.

## Per-user clones

Every user works on their own bare clones and worktrees
(`<data_dir>/repos/u<id>/…`), created lazily on first access and fetched
exclusively with that user's token. There is no boot clone and there are no
background sync loops — the client fetches when a project is opened. The
isolation is the security argument for in-repo source definitions
(REQ-004): a definition grants nothing, because materializing it runs as
*you*, with *your* token, into *your* storage. A source your token cannot
fetch is a 502 for you and nothing lands on disk.

### Source remotes are hostile input

The in-repo config is ordinary repo content — anyone with push access can
edit it — so `sources:` remotes are validated like untrusted input:

- **http(s) only** — a filesystem path could read arbitrary local repos on
  the server.
- **no embedded credentials** (`https://user:pass@…` is rejected).
- **host allowlist** — the remote's hostname must be the forge itself, one
  of the configured project remotes' hosts, or an entry in
  `auth.forge.allowed_source_hosts`. Without this fence a definition could
  point at an attacker's server (which would then be *offered users'
  tokens* by git's credential machinery) or probe internal network
  services. Rejected definitions never register and surface as project
  warnings naming the reason.
- **host-scoped credentials** — independent of the allowlist, the git
  credential helper only releases the token to the exact `host[:port]` of
  the repo's own configured remote. A same-host remote that redirects
  elsewhere gets a credential-less request: the redirect target sees no
  token, the clone fails.

## Proposing instead of merging

The in-app merge answers 403 (`merge_via_forge`). `POST /propose` refuses a
dirty worktree (same commit-first contract as the merge dialog), pushes the
branch with the user's token, and opens a merge request / pull request via
the forge API — idempotently: an open request for the branch is re-used, so
re-proposing just pushes new commits onto it. Review, approval and the
merge itself happen on the forge ([merging.md](merging.md)); the moved
default branch arrives back via fetch, and the
[forge-review panel](forge-review.md) shows the request's thread — with its
60-second cache keyed **per user**, since visibility now follows each
user's own token. Both surfaces name the object the way the host does
(GitLab *merge request* `!12`, GitHub *pull request* `#12`), driven by the
`kind` the API reports alongside each request.

## Share links in this mode

A [share link](share-links.md) is downloaded without a session, so nothing
can clone at download time. Minting is therefore the moment the clone is
materialized — with the creator's token, the last point one is available —
and the download serves from that user's storage. A link whose clone has
since gone answers 404 rather than surfacing a git error.
