---
type: Specification
title: Workspace branches — protected main mechanics
status: approved
satisfies: [requirements/REQ-001.md, requirements/REQ-002.md]
updated: 2026-07-09
---

# Workspace branches — protected main mechanics

How [REQ-001](../requirements/REQ-001.md) is realized.

## Branch claim

The first edit on a protected branch resolves the caller's workspace branch
`ws/<slug>`; ownership is claimed in the database with a uniqueness
constraint, so a name collision falls back to a suffixed branch. The claim —
not the branch's existence — is authoritative.

## Draft store

Saves are uncommitted changes on a per-branch worktree; an explicit commit
turns them into history with the user as **author and committer** and the
service identity as a `Co-authored-by:` trailer. Untouched files round-trip
byte-identically (see [REQ-002](../requirements/REQ-002.md)).

## Merge path

Protected branches move only via merge-tree merges with conflicts detected
and blocked; refs advance through compare-and-swap updates, so concurrent
merges cannot clobber each other.
