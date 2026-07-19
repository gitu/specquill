---
type: Specification
title: Tenants — GitHub App installations, derived roles, repo grants
status: in_review
satisfies: [requirements/REQ-019.md, requirements/REQ-020.md, requirements/REQ-021.md]
updated: 2026-07-19
---

# Tenants — GitHub App installations, derived roles, repo grants

How [REQ-019](../requirements/REQ-019.md),
[REQ-020](../requirements/REQ-020.md) and
[REQ-021](../requirements/REQ-021.md) are realized; the full design
reference is [multi-tenancy.md](multi-tenancy.md).

## Lifecycle

The App's `installation` webhooks drive the tenant table: `created` /
`unsuspend` upserts a tenant (slug = the account login, provider `github`,
keyed by installation id); `deleted` / `suspend` revokes every membership
and deregisters the tenant's repos from the git manager — store rows
survive, so a re-install restores the tenant intact. The self-host config
tenant (provider `config`, declared by the YAML `tenant:` block and synced
from `projects:`/`sources:` at boot) runs through the same code paths and
stays first-class.

## Derived authorization

Authorization is derived from GitHub by default; explicit per-repo grants
(below) are the one escape hatch. On login (and on a 5-minute cache TTL),
the user's role in each github tenant is computed as the **maximum repo
permission** across the tenant's adopted repos, on the four-level ladder
([authentication.md](authentication.md)): `admin` → admin, `maintain` →
maintainer, `write`/`triage` → editor, `read` → viewer, none anywhere → no
membership. This tenant-level role gates tenant management; **repo access
is derived per repository** (same mapping, cached per `tenant:login:repo`)
— write on one repo no longer implies write on its siblings, and only a
`maintain`-or-better permission merges into a protected branch. A fresh
installation with no adopted repos falls back to the installation's
candidate repositories — that is how the org admin becomes tenant admin and
reaches the repo picker at all. Permission-lookup failures keep existing
memberships and fall back to the tenant role per repo (GitHub being down
must not lock users out); revocation happens where admins already do it —
on GitHub.

## Explicit repo grants (REQ-020)

`repo_grants` rows — (tenant, repo, user) → any ladder role — are the
app-side layer for users the git host doesn't know or under-privileges: an
external reviewer scoped to one repo, an on-prem user without git-host
access, a git-read-only user who edits through the app (the server pushes
with its own credentials, so this gate is the effective one). The effective
role on a repo is **max(derived, granted)**; role syncs never touch grants,
so a GitHub revocation cannot drop one. Grant-only users see the tenant and
exactly their granted repos, and stay out of tenant management. Route
enforcement follows the ladder: `viewer` reads and comments, `editor`
mutates, `maintainer` merges into protected branches, `admin` manages the
repo's grants and settings. Granting an unknown identity (email or GitHub
login) leaves a pending invite, converted to a grant on the invitee's
first login. Repo admins manage grants and invites in the Admin view's
access panel (`/api/t/{tenant}/repos/{repo}/grants`,
`/api/t/{tenant}/members`).

## Repo adoption

The Admin view's GitHub-repositories panel lists the installation's
candidates with their current state. Adopting one as a **workspace** creates
the project (writable repo + tenant registry rows); as a **reference**, a
tenant-scoped granted source. Removal deletes the rows and deregisters the
clone; repositories the installation stops granting are dropped
automatically by the `installation_repositories` webhook. Ids are the repo's
short name; candidates outside the installation are unreachable by
construction. Repos outside any installation connect with a tenant
credential instead ([credentials.md](credentials.md)).

## Credentials and isolation

Git authenticates through the manager's token hook — installation token,
else the repo's attached tenant credential, else `token_env`
([credentials.md](credentials.md)). Installation tokens are minted from an
RS256 app JWT (~1h, cached until shortly before expiry); all tokens reach
git via child-process environment only and never cross tenants. Every
store row, collab room and on-disk path is keyed `<tenant>/<repo>`, so two
tenants can hold same-named repos with zero collision. The active tenant
is named by the URL path — `/t/{tenant}` in the SPA, `/api/t/{tenant}` on
the API, websockets and raw URLs included ([urls.md](urls.md)) — and users
with several memberships switch tenants in the top bar without a reload.
