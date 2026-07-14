---
type: Specification
title: URLs — project-scoped deep links
status: in_review
satisfies: [requirements/REQ-011.md]
updated: 2026-07-14
---

# URLs — project-scoped deep links

How [REQ-011](../requirements/REQ-011.md) is realized.

## Scheme

Locations are plain URL paths (history routing, no `#` fragment). The server
serves the SPA shell for any path that is not an API route or a build asset,
so every deep link resolves directly. Project-scoped views live under a
`/p/<project>/b/<branch>` prefix:

| URL | meaning |
|---|---|
| `/p/<project>/b/<branch>/editor/<path>` | a document on a branch (the canonical deep link) |
| `/p/<project>/b/<branch>/editor/~<source>/<path>` | a read-only reference document, viewed in the project's context |
| `/p/<project>/b/<branch>/dashboard` · `changes` · `graph` · `matrix` · `model` · `diff` · `prs` · `prs/<n>` | the other project views |
| `/p/<project>` | the project's default view on the visitor's remembered branch |
| `/` | the remembered (else first) project's default view |
| `/share/<token>/<name>.zip` | unauthenticated OKF-bundle download (see [share links](share-links.md)) |
| `/admin`, `/login` | global — no project scope |

`<project>` is the project id from the catalog (e.g. `trading-specs`), not
the `<tenant>/<repo>` store key. `<branch>` is the branch name URL-encoded
as one path segment (`ws/dev` → `ws%2Fdev`). Document paths are
project-relative, exactly as the API serves them (see
[content roots](content-root.md)).

## Branch resolution

The URL is authoritative for the branch it names: a pasted link lands every
recipient on the same project, branch, document and view. URLs without a
`/b/<branch>` segment still resolve — the app falls back to the visitor's
remembered per-project branch (browser-local), then the default branch —
and are immediately canonicalized to the branch-scoped form. Legacy
`?branch=` query links are honored and canonicalized the same way.

Ephemeral state rides in query parameters — `?branch=…&invite=1` (collab
invites: handled by the editor as a join prompt, never auto-canonicalized),
`?change=` (diff scope), `?sel=` (list selection).

## Resolution rules

- The URL is the source of truth for the active project and branch. The
  last-used project/branch are remembered per browser only to resolve URLs
  that do not name them.
- Switching projects navigates to the same view under the new prefix; file
  paths, branch and query state do not carry across projects.
- Switching branches rewrites the `/b/<branch>` segment in place, so
  browser history traverses branch switches too.
- An unknown `<project>` redirects to `/`; with no projects at all, to
  `/admin`. A remembered branch that no longer exists falls back to the
  default branch. Any unroutable path likewise falls back to `/`.
