---
type: Specification
title: Multi-tenancy — architecture and boundaries
status: in_review
satisfies: [requirements/REQ-019.md, requirements/REQ-020.md, requirements/REQ-021.md, requirements/REQ-022.md, requirements/REQ-023.md]
updated: 2026-07-19
---

# Multi-tenancy design

Status: **phases A and B implemented** — the GitHub App edge (login,
installation sync, token provider, role sync, repo picker, push webhooks)
is code-complete and integration-tested against a fake GitHub; only the app
*registration* itself is outstanding (operational steps: the cloud
deployment guide, "Multi-tenant hosting"). **Phase C is the layered-auth
refactor** ([REQ-021](../requirements/REQ-021.md)–[REQ-023](../requirements/REQ-023.md)): the four-level role ladder, tenant-in-URL,
the first-class config tenant, and the encrypted credential store. This
document is the full design reference — where the boundaries are and why;
[tenants.md](tenants.md) specifies the implemented behavior.

## Tenant model

**A tenant is a GitHub App installation** (an org or personal account) —
or the deployment's own on-prem installation. The installation defines
which repos a tenant may bring in; each chosen repo becomes a workspace
(writable) or a reference source (readonly).

Self-hosting stays first-class and is **not a special case**: the config
tenant is declared in YAML —

```yaml
tenant:
  slug: acme              # omitted entirely → synthesized as "default"
  display_name: Acme
  default_role: editor    # none | viewer | editor | maintainer
  admin_emails: [ops@acme.example]
```

— and the `projects:`/`sources:` lists sync into it at boot. A hosted
deployment omits `tenant:` and boots with zero configured projects; every
tenant then arrives via the GitHub App. Both providers (`config`,
`github`) run the same code path below the boot sync; no code knows a
hardcoded `default` slug.

## Identity and keys

- **Users are global** (one GitHub/OIDC/local identity), membership is
  per-tenant (`tenant_members`, role `admin|maintainer|editor|viewer`).
- **The canonical repo key is `<tenant_slug>/<repo_id>`** — this string is
  what lands in every store row (`prs.repo`, `collab_rooms.repo`,
  `workspace_branches.repo`, …), in collab room keys, and on disk
  (`data_dir/tenants/<tenant>/<repo>/{git,worktrees}`). Two tenants can both
  have a repo called `specs` with zero collision anywhere.
- **The tenant is in the URL** ([urls.md](urls.md)): API routes live under
  `/api/t/{tenant}/…`, SPA routes under `/t/{tenant}/…`. The store key
  never appears as one string in a URL, but its two parts do — `{tenant}`
  and the short `{repo}` id are separate path segments.

## Database

```
tenants            (id, slug UNIQUE, provider 'config'|'github', installation_id,
                    display_name, created_at)
tenant_repos       (tenant_id, repo_id, mode, remote, default_branch,
                    gh_full_name, credential_id, created_at)  PK (tenant_id, repo_id)
tenant_members     (tenant_id, user_id, role, synced_at) PK (tenant_id, user_id)
repo_grants        (tenant_id, repo_id, user_id, role, granted_by, created_at)
                    PK (tenant_id, repo_id, user_id) — explicit per-repo access (REQ-020)
repo_grant_invites (id, tenant_id, repo_id, kind 'email'|'github', matcher,
                    role, granted_by, created_at) — pending, claimed on first login
credentials        (id, tenant_id, name, username, nonce, ciphertext, key_id,
                    created_by, created_at, updated_at) — sealed repo tokens (REQ-023,
                    see credentials.md); referenced by tenant_repos and sources
```

Roles everywhere are the four-level ladder `viewer < editor < maintainer < admin` ([authentication.md](authentication.md)). Everything else keys by
the qualified repo string and needs no tenant_id. Isolation upgrades
available without re-architecture, in order of demand: Postgres RLS on the
qualified-key prefix as defense-in-depth, then a Neon project per
enterprise tenant (`store.Open` takes a DSN; a per-tenant DSN lookup is a
small seam).

## Authorization: derived from GitHub, granted per repo

Derived by default, never duplicated — plus one explicit layer. A user's
rights in a GitHub tenant are their rights on the repo, synced from the
installation and cached with a TTL:

| GitHub permission | specquill role | can |
| --- | --- | --- |
| `admin`            | admin      | repo grants + settings, and everything below; tenant-level: tenant management |
| `maintain`         | maintainer | merge PRs into protected branches, share links + everything below |
| `push` / `triage`  | editor     | edit ws/ branches, commit, open/approve/close PRs, co-edit, copilot |
| `pull`             | viewer     | read, comment |

Derivation is **per repository** (`tenant:login:repo` cache); the
tenant-level role in `tenant_members` is the maximum across repos and gates
tenant management only. On top sit **explicit repo grants** (REQ-020, any
ladder role, managed by repo admins): effective role = max(derived,
granted). Grants cover what derivation can't — outsiders scoped to one
repo, on-prem users without git-host access, elevation past a read-only git
permission (the server pushes with its own token, so the app gate decides).
Syncs never write grants; GitHub revocation drops the derived role, not
the grant. Enforcement is one resolver on every repo route: `viewer` to
read/comment (PR reads included), `editor` for mutations (writes, commits,
PR create/approve/close, co-editing, copilot), `maintainer` to merge into a
protected branch or manage share links, `admin` for the repo's grants and
settings ([REQ-021](../requirements/REQ-021.md)).

Revocation of derived access happens where admins already do it — on
GitHub. The `config` tenant enrolls every authenticated user per
`tenant.default_role`: `editor` (default), `viewer`, `maintainer`, or
`none` — with `none` only explicit grants open repos (restricted on-prem
deployments).

## Request resolution

`requireAuth` resolves the user; the tenant comes **from the URL path** —
`/api/t/{tenant}/…` — checked against the caller's visibility (a
membership, or at least one repo grant in the tenant): unknown slug → 404,
no visibility → 403. There is no header or query fallback; a request
either names its tenant or targets a global route (`/api/me`, `/auth/*`,
`/share/*`, `/hooks/*`). `GET /api/me` lists memberships so the SPA can
offer the switcher and resolve `/` to a tenant.

## Git layer: disk is a cache

- Layout: `data_dir/tenants/<tenant>/<repo>/{git,worktrees}`, cloned lazily
  on first access (`gitx.Manager.AddRepo` at runtime; boot no longer clones
  everything eagerly in multi-tenant mode).
- **Credentials**: `Manager.TokenFor(repo)` hook — the single seam, in
  precedence order: (1) the tenant's **installation token** (GitHub App:
  1h expiry, cached until ~5 min before expiry, never crossing tenants),
  (2) the repo's attached **tenant credential** (AES-GCM-sealed PAT,
  [credentials.md](credentials.md)), (3) the configured **`token_env`**
  fallback. Config-tenant repos on github.com also ride the app when it is
  installed on them (the covering installation is resolved per repo and
  cached). Tokens reach git via child-process env only (existing
  `credentialArgsEnv` invariant).
- Everything on disk is reconstructable: clones from the remote, roomed
  drafts replay from the collab log in Postgres. Plain uncommitted worktree
  drafts are the one loss case on eviction — mitigation (later): auto-commit
  stale drafts to the owner's ws branch before evicting.
- Per-tenant caps (later): repo size (partial clones `--filter=blob:none`),
  worktree count, room count, LRU eviction of idle repos.

## Collab + scaling

Single instance (`max-instances=1`) carries the first many tenants: the hub
relays opaque Yjs frames; hundreds of concurrent rooms fit in one Go
process. The scale-out path is prepared, not built:

1. Room leadership → a **Postgres advisory lock per room** (instance holding
   it accepts writes + flushes to its worktree).
2. Cross-instance fan-out → Postgres `LISTEN/NOTIFY` on the update log
   (joiners already replay from the log).
3. Or tenant-sticky sharding at a thin proxy / per-large-tenant Cloud Run
   services ("cellular") — worktrees being caches makes shard moves cheap.

Do none of this until a real load wall; keep the interfaces compatible.

## GitHub App edge (blocked on registration)

Registration needs: **contents:rw, pull_requests:rw, metadata:r**, webhook
events `push` + `installation_repositories`, and a user-auth (OAuth) flow.
Config gains:

```yaml
github_app:
  app_id: 12345
  private_key_path: /etc/specquill/github-app.pem   # or private_key_env
  client_id: Iv1.…
  client_secret_env: SPECQUILL_GH_CLIENT_SECRET
  webhook_secret_env: SPECQUILL_GH_WEBHOOK_SECRET
```

Components (all behind `github_app:` being present):
- **Login**: `github` provider beside OIDC/local (`users.provider='github'`,
  subject = GitHub user id).
- **Installation sync**: `installation` / `installation_repositories`
  webhooks upsert tenants + candidate repos; an admin picks which become
  workspace/readonly (`tenant_repos`).
- **Token provider**: app JWT (RS256) → `POST /app/installations/{id}/access_tokens`,
  cached; plugged into `Manager.TokenFor`.
- **Push webhook**: targeted `Fetch()` on the affected tenant repo,
  replacing interval polling for github tenants.
- **Identity on commits**: unchanged — the logged-in user is author AND
  committer, service identity + collab contributors as `Co-authored-by`
  trailers; installation tokens only authenticate the *push*.

## Everything else

- **AI**: per-tenant copilot config — BYO key (Secret Manager entry per
  tenant) or platform key + per-tenant metering. `.specquill/skills/` lives in
  the tenant's repo, so authoring rules are already per-tenant.
- **Billing** (hosted): seats = distinct active members/month (derivable
  from sessions), quotas on repos/rooms/AI calls hang off `tenants`.

## Sequencing

- **A — tenant-ready core** (done): schema + store methods; boot sync of
  YAML repos into the config tenant; auto-membership; qualified repo keys
  end-to-end (store rows, room keys, disk layout); request-scoped tenant
  resolution; `Manager.AddRepo` + `TokenFor` seams.
- **B — GitHub App integration** (needs registration): login, installation
  sync → tenants, token provider, push webhooks, repo picker UI.
- **C — layered auth** (this refactor): four-level role ladder with the
  maintainer merge gate (REQ-021), tenant-in-URL for API and SPA with a
  reload-free switcher (REQ-022), first-class config tenant (`tenant:`
  block), encrypted tenant credentials + connect-repo-with-token (REQ-023).
- **D — scale-out** (only when needed): advisory-lock room leadership,
  LISTEN/NOTIFY fan-out or tenant sharding, repo LRU eviction.
