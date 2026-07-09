---
type: Specification
title: References — sources, grants, grounding
status: draft
satisfies: [requirements/REQ-004.md]
updated: 2026-07-09
---

# References — sources, grants, grounding

How [REQ-004](../requirements/REQ-004.md) is realized.

## The chain

1. **Catalog** — named sources (`kind: git | url | openapi | confluence`)
   with remote + a credential *environment variable name*.
2. **Grants** — tenant admins attach catalog entries to the tenant.
3. **Selection** — the in-repo config lists references by source name, with
   optional path filters; effective references are the intersection of
   selection and grants, resolved from the default branch only.
4. **Roles** — viewer/member/admin gate reads, writes and administration.

## Grounding

Grounded references join the copilot context under `~<source>/<path>`
headings inside a budget; draft edits remain restricted to project files —
a reference path in a model reply is refused.
