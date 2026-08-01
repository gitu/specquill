---
type: Specification
title: Timed dependencies — validity windows and readiness
status: draft
satisfies: [requirements/REQ-026.md]
updated: 2026-08-01
---

# Timed dependencies — validity windows and readiness

How [REQ-026](../requirements/REQ-026.md) is realized.

## Configuration

`.specquill/config.yml` gains an optional `timed:` section; every key has a
built-in default, so a workspace with no section still has a timeline:

```yaml
timed:
  start: [starts, effective_from, valid_from]
  end: [ends, effective_until, valid_until, due]
  ready_statuses: [approved, done]
  horizon_days: 90
  kinds: []          # empty = every family
```

The lists are ordered: the FIRST key a document carries wins, and which key
that was is kept (`startKey`/`endKey`) so the UI can name it. `due` sits on
the end side deliberately — a work item with a due date belongs on the same
timeline as a regulation that lapses.

## Derivation

Model building collects every classified document carrying a configured key
into `model.timed`; nothing else about the document changes, and a document
with no key simply never appears. The view model is computed against a
**date parameter** rather than the clock, which is what makes the buckets
testable:

| state | condition |
|---|---|
| pending | start is in the future |
| active | started (or no start), end absent or beyond the horizon |
| expiring | end falls within `horizon_days` |
| expired | end has passed |

Dependents come from the existing backlink index — the inbound typed links of
the entity model — and count as ready when their status is in
`ready_statuses`. **At risk** = the window opens or closes inside the horizon
while the document itself or any dependent is not ready.

## Surfaces

- `/timed` — the four states as filters, `?sel=<path>` for a shareable item.
- Overview — a pending-count tile, a "Coming up" card, and one review row per
  at-risk item.
- Rail — the at-risk count as a badge, so the timeline does not have to be
  opened to notice a deadline.
- Editor — a document carrying a window shows it inline with its dependent
  readiness.
- Speccy — the most at-risk item drives the "draft the outstanding edits"
  action, which works from the document and its unfinished dependents.

## What it replaces

The change-record inbox (`inbox: true` on an entity, the triage/closed status
lifecycle, the `changes/` built-in family). A workspace may still declare a
`change:` family in its config as an ordinary family; it gets no special UI.
Documents about change are optional — dates on the documents themselves and
git history ([REQ-027](../requirements/REQ-027.md)) are not.
