---
type: Specification
title: Document lifecycle — moves and history
status: in_review
satisfies: [requirements/REQ-014.md]
updated: 2026-07-13
---

# Document lifecycle — moves and history

How [REQ-014](../requirements/REQ-014.md) is realized.

## Moving

`POST /api/repos/{repo}/move {from, to}` renames inside the branch worktree:
tracked files via `git mv` (the rename is staged, so the eventual commit
carries it), untracked drafts via a plain rename. Protection and live-room
ownership apply exactly like writes: protected branches 403, a co-edited
file 409s (`room_active`), and the destination must be free.

The Move dialog (editor header) detects referencing documents with the same
resolver the model uses — body links in any style, image embeds, typed
frontmatter lists — and, when confirmed, rewrites each one as an ordinary
worktree save: body links become **relative** links to the new location
(REQ-005.3), frontmatter entries get the new root-relative path. The whole
operation is uncommitted worktree state until an explicit commit, like every
other edit.

## History

`GET /api/repos/{repo}/history?path=…&ref=…` returns the commits touching a
file, newest first, renames followed (`git log --follow`). The editor's
History tab lists them; selecting a commit shows the document as committed
then (object database read, never the worktree). Uncommitted drafts have no
history yet — by design, the worktree is the draft store
([workspace branches](workspace-branches.md)).
