---
type: Specification
title: URLs — tenant- and project-scoped deep links
status: in_review
satisfies: [requirements/REQ-011.md, requirements/REQ-022.md]
updated: 2026-07-19
---

# URLs — tenant- and project-scoped deep links

How [REQ-011](../requirements/REQ-011.md) and the URL side of
[REQ-022](../requirements/REQ-022.md) are realized.

## Scheme

Locations are plain URL paths (history routing, no `#` fragment). The server
serves the SPA shell for any path that is not an API route or a build asset,
so every deep link resolves directly. **The tenant is part of the path** —
there is no out-of-band tenant header or pinned client state; a pasted link
carries everything needed to land in the right tenant, project, branch and
document. Tenant-scoped views live under `/t/<tenant>`, project-scoped
views under `/t/<tenant>/p/<project>/b/<branch>`:

| URL | meaning |
|---|---|
| `/t/<tenant>/p/<project>/b/<branch>/editor/<path>` | a document on a branch (the canonical deep link) |
| `/t/<tenant>/p/<project>/b/<branch>/editor/~<source>/<path>` | a read-only reference document, viewed in the project's context |
| `/t/<tenant>/p/<project>/b/<branch>/dashboard` · `changes` · `graph` · `matrix` · `model` · `diff` · `prs` · `prs/<n>` | the other project views |
| `/t/<tenant>/p/<project>` | the project's default view on the visitor's remembered branch |
| `/t/<tenant>` | the tenant's remembered (else first) project's default view |
| `/t/<tenant>/admin` | the tenant's administration view |
| `/` | redirect: last-used tenant, else the sole membership, else the tenant picker |
| `/share/<token>/<name>.zip` | unauthenticated OKF-bundle download (see [share links](share-links.md)) |
| `/login` | global — no tenant scope |

`<tenant>` is the tenant slug (installation account login, or the config
tenant's configured slug). `<project>` is the project id from the catalog
(e.g. `trading-specs`), not the `<tenant>/<repo>` store key — the store key
never appears in URLs, but the API path now composes it visibly:
`/api/t/<tenant>/repos/<project>/…`. `<branch>` is the branch name
URL-encoded as one path segment (`ws/dev` → `ws%2Fdev`). Document paths are
project-relative, exactly as the API serves them (see
[content roots](content-root.md)).

The API mirrors the scheme: every tenant-scoped route lives under
`/api/t/<tenant>/…` — including websockets and raw asset URLs, which
previously needed a `?tenant=` query. Global routes are exactly
`/api/me`, `/auth/*`, `/share/*` and `/hooks/*`.

## Branch resolution

The URL is authoritative for the branch it names: a pasted link lands every
recipient on the same tenant, project, branch, document and view. URLs
without a `/b/<branch>` segment still resolve — the app falls back to the
visitor's remembered per-project branch (browser-local, tenant-scoped),
then the default branch — and are immediately canonicalized to the
branch-scoped form. Legacy `?branch=` query links are honored and
canonicalized the same way.

Ephemeral state rides in query parameters — `?branch=…&invite=1` (collab
invites: handled by the editor as a join prompt, never auto-canonicalized),
`?change=` (diff scope), `?sel=` (list selection).

## Resolution rules

- The URL is the source of truth for the active tenant, project and branch.
  The last-used tenant/project/branch are remembered per browser only to
  resolve URLs that do not name them.
- Switching tenants navigates to `/t/<tenant>` — client-side navigation,
  no reload; project, branch and file paths do not carry across tenants.
- Switching projects navigates to the same view under the new prefix; file
  paths, branch and query state do not carry across projects.
- Switching branches rewrites the `/b/<branch>` segment in place, so
  browser history traverses branch switches too.
- An unknown or inaccessible `<tenant>` redirects to `/`. An unknown
  `<project>` redirects to `/t/<tenant>`; with no projects at all, to
  `/t/<tenant>/admin`. A remembered branch that no longer exists falls back
  to the default branch. Any unroutable path likewise falls back to `/`.
- Legacy tenant-less paths (`/p/…`, `/admin`) redirect to the same location
  under the resolved tenant.
