---
type: Specification
title: Co-editing — collaborative rooms
status: approved
satisfies: [requirements/REQ-006.md]
updated: 2026-07-09
---

# Co-editing — collaborative rooms

How [REQ-006](../requirements/REQ-006.md) is realized.

## Rooms

A markdown file opened in edit mode joins a room keyed by (branch, path).
Clients exchange CRDT updates; the server appends each update to a durable
log and fans it out to the other members. Because updates are opaque, the
server never parses document content to merge — convergence is the CRDT's
property, not the server's.

## Ownership

While a room has members it owns the file:

| operation on that (branch, path) | behavior |
|---|---|
| direct write (PUT) | rejected — `room_active` |
| background branch update / fast-forward | withheld until the room drains |
| flush to the worktree | performed by the elected leader |

The leader serializes the shared document to markdown and writes it to the
per-branch worktree, so a committed room result is a normal file.

## Joining

A joiner replays the full update log before its local edits are transmitted.
Mutating the shared document before the connection is open and replay is
applied is forbidden: such edits reference update ranges peers never saw and
diverge silently.
