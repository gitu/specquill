---
type: Specification
title: Pull requests — reviewed merges
status: in_review
satisfies: [requirements/REQ-008.md, requirements/REQ-021.md]
updated: 2026-07-19
---

# Pull requests — reviewed merges

How [REQ-008](../requirements/REQ-008.md) is realized; it is the reviewed
counterpart to the branch mechanics in
[workspace-branches.md](workspace-branches.md).

## Lifecycle

A workspace branch opens a pull request against the protected default branch.
Reviewers approve; the PR merges only when it is approved and conflict-free.

## Approval pinning

An approval records the branch's head commit at approval time. A new commit
moves the head, so prior approvals no longer match and the PR needs
re-approval — an approval always refers to the exact tree that was reviewed.

## Who may do what

Roles follow the per-repo ladder ([REQ-021](../requirements/REQ-021.md)):
a `viewer` reads PRs and comments; an `editor` opens, approves and closes
them; **merging into a protected branch requires `maintainer`** — approval
and merge remain distinct capabilities, so a team can review broadly while
keeping the protected branch behind a smaller group. A PR targeting an
unprotected branch merges at `editor`.

## Merge safety

Merges use a write-tree merge that detects conflicts and refuses rather than
writing a conflicted tree. The protected reference advances with a
compare-and-swap update, so two merges racing onto the same branch cannot
clobber one another — the loser retries against the new tip.

## Identity

The merge commit records the branch author as **author and committer**; the
service identity and any collaborators from the room are added as
`Co-authored-by:` trailers.
