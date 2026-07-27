---
type: Specification
title: Merging — landing workspace branches on the default branch
status: in_review
satisfies: [requirements/REQ-008.md]
updated: 2026-07-25
---

# Merging — landing workspace branches on the default branch

How [REQ-008](../requirements/REQ-008.md) is realized; it is the landing
counterpart to the branch mechanics in
[workspace-branches.md](workspace-branches.md).

## Direct merges (local-auth mode)

The default branch is never edited directly ([REQ-001](../requirements/REQ-001.md)),
so a merge commit from a workspace branch is the only way it moves. That
merge is **direct**: there is no in-app review step, no approval gate and no
PR object — an author with the `member` role previews what would land and
merges it.

Reviewed merges are not abandoned, they are **delegated**: push the branch and
open a merge request on the forge (GitLab, GitHub), where review already has
a mature home. SpecQuill deliberately does not reimplement that.

## Proposals (forge-PAT mode)

In forge-PAT deployments ([forge-auth.md](forge-auth.md),
[REQ-024](../requirements/REQ-024.md)) the delegation is total: the in-app
merge is disabled (403 `merge_via_forge`) and "landing" means **propose** —
push the workspace branch with the user's own token and open a merge
request / pull request via the forge API. The action is idempotent (an
open request for the branch is re-used; re-proposing pushes new commits
onto it), keeps the same preview and dirty-worktree refusal as the direct
merge, and the merged default branch returns via fetch. Everything below
this section applies to direct merges only.

## Preview

Before merging, the app reports exactly what would land: the diff between the
target and the source, whether the merge conflicts, and any uncommitted work
on the source. Uncommitted changes are **not** part of a merge, so the merge
is refused while the source worktree is dirty and the author is prompted to
commit first — a merge never silently lands less than the author sees.

## Merge safety

Merges use a write-tree merge that detects conflicts and refuses rather than
writing a conflicted tree. The protected reference advances with a
compare-and-swap update, so two merges racing onto the same branch cannot
clobber one another — the loser retries against the new tip. Either strategy
is available: a merge commit that keeps the branch history, or a squash.

## After the merge

A merged personal workspace resets onto the new default-branch head, so it
stays perpetually reusable rather than accumulating divergence.

## Identity

The merge commit records the merging user as **author and committer**; the
service identity and any collaborators from the room are added as
`Co-authored-by:` trailers.
