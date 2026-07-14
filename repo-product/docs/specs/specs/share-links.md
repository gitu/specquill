---
type: Specification
title: Share links — unauthenticated OKF-bundle downloads
status: in_review
satisfies: [requirements/REQ-016.md]
updated: 2026-07-14
---

# Share links — unauthenticated OKF-bundle downloads

How [REQ-016](../requirements/REQ-016.md) is realized.

## Model

One `share_links` row per (tenant, project): a 48-hex-char random token,
who minted it, and when. Minting again upserts the row with a fresh token —
rotation and revocation are immediate because the public path resolves the
token on every request.

## API

| route | auth | effect |
|---|---|---|
| `GET /api/repos/<project>/share` | session | current link state (`url` or null) |
| `POST /api/repos/<project>/share` | session, member role | mint or rotate the token |
| `DELETE /api/repos/<project>/share` | session, member role | revoke |
| `GET /share/<token>/<name>.zip` | **none** — the token is the credential | stream the bundle |

The `<name>` segment is a cosmetic filename (`<project>-okf.zip`) and is not
validated. Downloads answer with `Content-Type: application/zip`,
`Content-Disposition: attachment` and `Cache-Control: no-store`.

## Bundle contents

The zip is produced by `git archive` against the project repo's default
branch — no worktree involved, binaries intact. For content-root projects
the archive is scoped to the subtree (`<ref>:<content_root>`), so entry
paths are project-relative and the bundle is a conformant, self-contained
OKF workspace: exactly what a recipient (human, LLM or agent) needs to read
the specs without SpecQuill.

## UI

The editor's Share button opens the share dialog: create, copy, rotate and
revoke, with the trust consequences stated inline (anyone holding the link
can download until revocation).
