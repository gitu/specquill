---
type: Specification
title: Tenants — GitHub App installations, derived roles
status: in_review
satisfies: [requirements/REQ-019.md]
updated: 2026-07-15
---

# Tenants — GitHub App installations, derived roles

How [REQ-019](../requirements/REQ-019.md) is realized.

## Lifecycle

The App's `installation` webhooks drive the tenant table: `created` /
`unsuspend` upserts a tenant (slug = the account login, provider `github`,
keyed by installation id); `deleted` / `suspend` revokes every membership
and deregisters the tenant's repos from the git manager — store rows
survive, so a re-install restores the tenant intact. The self-host `default`
tenant (provider `config`, synced from YAML at boot) runs through the same
code paths and stays first-class.

## Derived authorization

No SpecQuill ACLs. On login (and on a 5-minute cache TTL), the user's role
in each github tenant is computed as the **maximum repo permission** across
the tenant's adopted repos: `admin` → admin, `write`/`maintain` → member,
`read`/`triage` → viewer, none anywhere → no membership. A fresh
installation with no adopted repos falls back to the installation's
candidate repositories — that is how the org admin becomes tenant admin and
reaches the repo picker at all. Permission-lookup failures keep existing
memberships (GitHub being down must not lock users out); revocation happens
where admins already do it — on GitHub.

## Repo adoption

The Admin view's GitHub-repositories panel lists the installation's
candidates with their current state. Adopting one as a **workspace** creates
the project (writable repo + tenant registry rows); as a **reference**, a
tenant-scoped granted source. Removal deletes the rows and deregisters the
clone; repositories the installation stops granting are dropped
automatically by the `installation_repositories` webhook. Ids are the repo's
short name; candidates outside the installation are unreachable by
construction.

## Credentials and isolation

Git authenticates through the manager's token hook: an RS256 app JWT mints
per-installation access tokens (~1h, cached until shortly before expiry) —
tokens reach git via child-process environment only and never cross
tenants. Every store row, collab room and on-disk path is keyed
`<tenant>/<repo>`, so two tenants can hold same-named repos with zero
collision. The client pins its active tenant (`X-SpecQuill-Tenant` header;
`?tenant=` for websockets and raw URLs), and users with several memberships
switch tenants in the top bar.
