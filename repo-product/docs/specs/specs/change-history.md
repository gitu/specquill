---
type: Specification
title: Change history — commits read as document changes
status: draft
satisfies: [requirements/REQ-027.md]
updated: 2026-08-01
---

# Change history — commits read as document changes

How [REQ-027](../requirements/REQ-027.md) is realized.

## Reading the log

`GET /api/repos/{repo}/log?ref=&since=&limit=` returns commits newest first
with the paths each touched. Notes on the git side:

- The repo-wide log cannot use `--follow` (single-path only), so renames come
  back as `R` entries carrying both sides instead.
- `--name-status -z` is parsed record-wise; each commit header is prefixed
  with a sentinel, so a path containing a newline cannot fake a boundary.
- `since` is validated as `YYYY-MM-DD` before it reaches argv — git's date
  parser otherwise accepts free-form input.
- The first parent travels in the payload (`%P`): ref validation rejects
  `sha^`, so a client cannot derive the diff baseline itself.
- The project's content root is the pathspec, and paths are mapped out to the
  project-relative wire form; a commit left with no files drops out.

## Reading one commit

`GET /api/repos/{repo}/commit?sha=&parent=` returns the file diffs and the
semantic deltas. The diff is two-dot (`parent..sha`) — deliberately not the
three-dot form used for merge previews, which answers a different question —
and a root commit diffs against the empty tree.

`internal/delta` is pure (two strings in, a struct out) and produces:

- **props** — top-level frontmatter keys whose rendered value changed, in
  document order, removed keys last;
- **statements** — normative statements matched on
  `> **<id> · MUST|SHALL|SHOULD|MAY** — …`, paired BY ID so a reworded
  statement reads as modified rather than as one removal plus one addition;
- **sections** — headings that came or went (fenced code stripped first).

A document that yields none of these is marked `plain`, and the client shows
the textual diff instead.

## Classification

The SPA classifies every changed path through the current workspace config
with the same classifier the model uses, exported for the purpose: a path
that still exists takes its document's kind, one that does not (deleted,
renamed away) falls back to the folder rule. Feed roll-ups, family chips and
filters are all derived from that.

## Summaries

`GET /api/repos/{repo}/commit/summary?sha=&parent=` builds a compact prompt
from the semantic delta — not the diff — and runs it on the quick model tier,
cached per `repo@sha` without a TTL (commits are immutable), bounded in size.
Without a configured model the endpoint answers 501 and the feed renders
without the card.

## Pending vs history

`/history` is committed history; `/changes` is what has not landed yet on
this branch: uncommitted worktree drafts (shared rendering with the header
drawer), commits ahead of the default branch with the merge or propose
action, and the open merge request via the read-only forge integration.
