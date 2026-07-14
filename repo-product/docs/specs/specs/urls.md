---
type: Specification
title: URLs — project-scoped deep links
status: in_review
satisfies: [requirements/REQ-011.md]
updated: 2026-07-13
---

# URLs — project-scoped deep links

How [REQ-011](../requirements/REQ-011.md) is realized.

## Scheme

Locations are plain URL paths (history routing, no `#` fragment). The server
serves the SPA shell for any path that is not an API route or a build asset,
so every deep link resolves directly. Project-scoped views live under a
`/p/<project>` prefix:

| URL | meaning |
|---|---|
| `/p/<project>/editor/<path>` | a document in a project (the canonical deep link) |
| `/p/<project>/editor/~<source>/<path>` | a read-only reference document, viewed in the project's context |
| `/p/<project>/dashboard` · `changes` · `graph` · `matrix` · `model` · `diff` · `prs` · `prs/<n>` | the other project views |
| `/p/<project>` | the project's default view |
| `/` | the remembered (else first) project's default view |
| `/admin`, `/login` | global — no project scope |

`<project>` is the project id from the catalog (e.g. `trading-specs`), not
the `<tenant>/<repo>` store key. Document paths are project-relative, exactly
as the API serves them (see [content roots](content-root.md)).

## What stays out of the path

The **branch** is deliberately not part of the path: a link points at
content, and each visitor lands on it in their own workspace branch — the
app remembers the last-used branch per project (browser-local) and falls
back to the default branch when the remembered one no longer exists.
Ephemeral state rides in query parameters instead — `?branch=` +
`&invite=1` (collab invites pin the room's branch), `?change=` (diff
scope), `?sel=` (list selection).

## Resolution rules

- The URL is the source of truth for the active project. The last-used
  project is remembered per browser only to resolve the project-less entry
  point `/`.
- Switching projects navigates to the same view under the new prefix; file
  paths and query state do not carry across projects.
- An unknown `<project>` redirects to `/`; with no projects at all, to
  `/admin`. Any unroutable path likewise falls back to `/`.
