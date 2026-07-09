---
type: Specification
title: Copilot grounding — context and limits
status: in_review
satisfies: [requirements/REQ-007.md]
updated: 2026-07-09
---

# Copilot grounding — context and limits

How [REQ-007](../requirements/REQ-007.md) is realized; the reference chain it
draws on is specified in [references.md](references.md).

## Context assembly

The system prompt is built from the current branch snapshot plus the
project's effective, grounded references:

- **Workspace files** head the prompt and keep a **60% floor** of the byte
  budget, so references can never starve the project itself.
- **Grounded references** share the remainder, proportional to size with a
  small per-source floor, under `## ~<source>/<path>` headings marked
  read-only.
- Authoring skills (`.specquill/skills/*`) are pinned ahead of file content.

Reference snapshots are cached by (source, head commit), so a busy edit room
never re-reads an unchanged source.

## Write boundary

The copilot has two write surfaces, both bounded:

| surface | limit |
|---|---|
| chat | read-only; grounds an answer, edits nothing |
| draft | surgical search/replace edits, applied as uncommitted saves on a `copilot/*` branch for review |

A draft edit targeting a `~`-prefixed reference path is refused: reference
sources are read-only. Nothing the copilot does reaches a protected branch
without a human merge.
