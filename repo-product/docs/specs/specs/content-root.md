---
type: Specification
title: Content roots — subfolder projects
status: in_review
satisfies: [requirements/REQ-003.md, requirements/REQ-005.md]
updated: 2026-07-09
---

# Content roots — subfolder projects

How [REQ-003](../requirements/REQ-003.md) is realized.

## Mapping rules

The API serves *project-relative* paths. One wrapper — the project layer —
is the only place that maps them onto full repository paths:

| direction | rule |
|---|---|
| request → git | `MapIn`: traversal-guarded join under `content_root` |
| git → response | `MapOut`: strip the prefix; foreign paths are filtered out |

Store rows (collab rooms, workspace claims) and git operations always use
full repository paths; the wire format is always project-relative. A commit
without explicit paths stages only the `content_root` subtree.

## OKF generation

Bundle detection and `index.md`/`log.md` regeneration run per content root,
so a subfolder workspace stays a conformant bundle without touching sibling
code.
