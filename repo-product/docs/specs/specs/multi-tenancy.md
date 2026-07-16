---
type: Specification
title: Multi-tenancy — architecture and boundaries
status: in_review
satisfies: [requirements/REQ-019.md, requirements/REQ-020.md]
updated: 2026-07-16
---

# Multi-tenancy design

Status: **phases A and B implemented** — the GitHub App edge (login,
installation sync, token provider, role sync, repo picker, push webhooks,
tenant switcher) is code-complete and integration-tested against a fake
GitHub; only the app *registration* itself is outstanding (operational
steps: the cloud deployment guide, "Multi-tenant hosting"). This document
is the full design reference — where the boundaries are and why;
[tenants.md](tenants.md) specifies the implemented behavior.

## Tenant model

**A tenant is a GitHub App installation** (an org or personal account). The
installation defines which repos a tenant may bring in; each chosen repo
becomes a workspace (writable) or a reference repo (readonly) — the same
shape as today's `repos:` config list, but per-tenant and dynamic.

Self-hosting stays first-class: without a GitHub App configured, the YAML
`repos:` list is synced into a single built-in **`default` tenant**
(provider `config`) at boot, and every authenticated user is auto-enrolled
as a member. Hosted multi-tenant and self-hosted single-tenant run the same
code path; the default tenant is not a special case anywhere below the boot
sync.

## Identity and keys

- **Users are global** (one GitHub/OIDC/local identity), membership is
  per-tenant (`tenant_members`, role `admin|member|viewer`).
- **The canonical repo key is `<tenant_slug>/<repo_id>`** — this string is
  what lands in every store row (`prs.repo`, `collab_rooms.repo`,
  `workspace_branches.repo`, …), in collab room keys, and on disk
  (`data_dir/tenants/<tenant>/<repo>/{git,worktrees}`). Two tenants can both
  have a repo called `specs` with zero collision anywhere.
- **API URLs keep the short id** (`/api/repos/{repo}/…`); the tenant comes
  from the request context (see Resolution). The SPA never sees qualified
  keys.

## Database

```
tenants            (id, slug UNIQUE, provider 'config'|'github', installation_id,
                    display_name, created_at)
tenant_repos       (tenant_id, repo_id, mode, remote, default_branch,
                    gh_full_name, created_at)          PK (tenant_id, repo_id)
tenant_members     (tenant_id, user_id, role, synced_at) PK (tenant_id, user_id)
repo_grants        (tenant_id, repo_id, user_id, role, granted_by, created_at)
                    PK (tenant_id, repo_id, user_id) — explicit per-repo access (REQ-020)
repo_grant_invites (id, tenant_id, repo_id, kind 'email'|'github', matcher,
                    role, granted_by, created_at) — pending, claimed on first login
```

Everything else keys by the qualified repo string and needs no tenant_id.
Isolation upgrades available without re-architecture, in order of demand:
Postgres RLS on the qualified-key prefix as defense-in-depth, then a Neon
project per enterprise tenant (`store.Open` takes a DSN; a per-tenant DSN
lookup is a small seam).

## Authorization: derived from GitHub, granted per repo

Derived by default, never duplicated — plus one explicit layer. A user's
rights in a GitHub tenant are their rights on the repo, synced from the
installation and cached with a TTL:

| GitHub permission | specquill role | can |
| --- | --- | --- |
| `admin`           | admin  | tenant settings, repo add/remove, grants + everything below |
| `push`            | member | edit, commit, open/approve/merge PRs |
| `pull`            | viewer | read, comment |

Derivation is **per repository** (`tenant:login:repo` cache); the
tenant-level role in `tenant_members` is the maximum across repos and gates
tenant management only. On top sit **explicit repo grants** (REQ-020,
`viewer|member`, admin-managed): effective role = max(derived, granted).
Grants cover what derivation can't — outsiders scoped to one repo, on-prem
users without git-host access, elevation past a read-only git permission
(the server pushes with the installation token, so the app gate decides).
Syncs never write grants; GitHub revocation drops the derived role, not
the grant. Enforcement is one resolver on every repo route: `viewer` to
read/comment (PR reads included), `member` for every mutation
(writes, commits, PR create/approve/merge/close, co-editing, copilot).

Revocation of derived access happens where admins already do it — on
GitHub. The `config` tenant enrolls every authenticated user per
`auth.default_role`: `member` (default), `viewer`, or `none` — with `none`
only explicit grants open repos (restricted on-prem deployments).

## Request resolution

`requireAuth` resolves the user; a tenancy layer then resolves the tenant:

1. `X-SpecQuill-Tenant: <slug>` header (or `?tenant=` for websockets) when the
   client targets one explicitly (membership checked, else 403);
2. otherwise the user's only tenant;
3. multiple memberships and no header → 400 `tenant_required` (the SPA gains
   a tenant switcher when 3b lands; it pins the header from then on).

`GET /api/me` lists memberships so the SPA knows what to offer.

## Git layer: disk is a cache

- Layout: `data_dir/tenants/<tenant>/<repo>/{git,worktrees}`, cloned lazily
  on first access (`gitx.Manager.AddRepo` at runtime; boot no longer clones
  everything eagerly in multi-tenant mode).
- **Credentials**: `Manager.TokenFor(repo)` hook — the single seam. The
  GitHub provider mints **installation tokens** per tenant (1h expiry,
  cached until ~5 min before expiry) and never crosses tenants. Config-
  tenant repos on github.com also ride the app when it is installed on them
  (the covering installation is resolved per repo and cached); `token_env`
  is the fallback for uncovered repos and non-GitHub hosts. Tokens reach
  git via child-process env only (existing `credentialArgsEnv` invariant).
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
  YAML repos into the `default` tenant; auto-membership; qualified repo keys
  end-to-end (store rows, room keys, disk layout); request-scoped tenant
  resolution; `Manager.AddRepo` + `TokenFor` seams.
- **B — GitHub App integration** (needs registration): login, installation
  sync → tenants, token provider, push webhooks, repo picker UI.
- **C — multi-tenant UX**: tenant switcher in the SPA, member/role views,
  per-tenant AI settings.
- **D — scale-out** (only when needed): advisory-lock room leadership,
  LISTEN/NOTIFY fan-out or tenant sharding, repo LRU eviction.
