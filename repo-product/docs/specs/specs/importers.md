---
type: Specification
title: Importers — mirroring non-git sources
status: in_review
satisfies: [requirements/REQ-009.md]
updated: 2026-07-09
---

# Importers — mirroring non-git sources

How [REQ-009](../requirements/REQ-009.md) is realized. Importer sources are
catalog entries like any other (see [references.md](references.md)); the only
difference is how their content is obtained.

## Mirror repositories

A non-git source is a remote-less repository, inited empty. An importer
fetches the upstream content and writes a full snapshot straight into the
bare repository through a throwaway index — no working copy. The snapshot is
idempotent: identical content yields the same tree and no new commit.

## Importers

| kind | fetches | produces |
|---|---|---|
| `url` | a list of pages | one file per page (HTML reduced to text) + an index |
| `openapi` | an API document | the raw spec + a readable endpoint/schema index |
| `confluence` | a space's pages via REST | one file per page (storage format → text) + an index |

## Credentials

Secrets are read only from the environment, by the variable name in the
catalog entry — never from a config file, the database, or a command line. A
`user:token` secret becomes HTTP Basic auth; a bare token becomes a Bearer
header.

## Refresh

Imports run on the source's interval and on demand (an administrator action).
Each run records its outcome — status, file count, and any error — for the
administration view.
