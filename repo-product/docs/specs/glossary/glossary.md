---
type: Glossary
title: Glossary
updated: 2026-07-09
---

# Glossary

**Project** — a writable workspace: a git repository plus an optional
`content_root` subfolder.

**Source** — a named, read-only external input from the catalog (git repo,
URL list, OpenAPI spec, Confluence export).

**Reference** — a project's selection of a granted source, optionally
path-filtered, optionally AI-grounded.

**Workspace branch** — the personal `ws/<user>` branch edits land on when
the default branch is protected.

**Mirror source** — a non-git source (`url`, `openapi`, `confluence`)
materialized into a remote-less, read-only repository by an importer.

**Grounding** — including the workspace and its grounded references in the
copilot's context so answers cite real content; references stay read-only.

**Room** — the per-(branch, path) collaborative session markdown editors join;
the server relays its CRDT updates and owns the file while it is live.

**Pull request** — the reviewed, conflict-checked path a workspace branch
takes to reach the protected default branch.
