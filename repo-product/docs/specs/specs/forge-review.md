---
type: Specification
title: Forge review — merge-request comments from the git host
status: in_review
satisfies: [requirements/REQ-008.md]
updated: 2026-07-25
---

# Forge review — merge-request comments from the git host

Merging in SpecQuill is direct and unreviewed ([merging.md](merging.md)); a
team that wants review opens a merge request on its git host instead. This
specification covers the return path: the review conversation is read back
and shown beside the branch it is about, so an author does not have to work
in two windows.

## Scope

**Read-only, and optional.** The app fetches the open merge request for the
current branch and its comments. It never creates, replies to, approves or
merges anything through the host's API — replying happens on the forge, which
the panel links to. A project opts in with `forge.kind`; with no `forge:`
block the feature does not exist for that project.

This is not a return of the removed GitHub integration (see
[REQ-017](../requirements/REQ-017.md)): there is no login, no app
installation, no webhook, and no bearing on authorization. It is one
authenticated GET against a host the deployment already pushes to.

## Hosts

| host | request lookup | comments |
|---|---|---|
| GitLab (incl. self-hosted) | merge requests filtered by `source_branch` | notes, excluding system-generated events |
| GitHub (incl. Enterprise) | pull requests filtered by owner-qualified head branch | review comments (file + line) and issue comments (general) |

The project path is normally derived from the git remote — `https://`,
`ssh://` and `git@host:owner/repo` forms all resolve. A remote that does not
name the project plainly (an ssh host alias, for instance) can state it
explicitly instead.

## Resilience

The panel decorates a page; it never blocks one. A host that is unreachable,
rate-limiting or rejecting the token yields an explanatory message in place
of the panel, and the surrounding view renders unaffected. A branch with no
open merge request renders nothing at all.

Answers are cached briefly per branch so that re-renders, and several people
working on one branch, do not each spend a request against the host's rate
limit.

## Credentials

The API token comes from the environment by variable name, defaulting to the
token the project already uses for push/fetch. As everywhere else, tokens are
never written to config files, never stored, and never echoed in error
messages.
