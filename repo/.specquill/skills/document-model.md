---
type: Skill
title: Document model — create, ensure, migrate
---

# Skill: the document model (WHY ← WHAT ← HOW ← WHEN)

The lower level always carries the frontmatter link UP: requirements cite
their WHY docs in `drivers:`, specs cite requirements in `implements:`,
work items cite specs (or requirements) in `delivers:`. All link values are
plain root-relative path lists — a driver's type (regulatory/product/…) is
derived from the referenced document, never written on the link.

## Create
Place the document in its family folder, seed the family's attributes, and
set the upward link to a REAL upper-level document — list_files/search first
to find it, ask_user when ambiguous. Never invent target paths.

## Ensure ("audit the model")
Walk the workspace and report per level: documents missing their upward link,
links whose target is the wrong kind, and legacy shapes (`{type, ref}` driver
maps, `satisfies:`, `implements:` on requirements). Report first; fix only on
request.

## Migrate
Per file, as uncommitted drafts the user reviews: flatten driver maps to path
lists (drop the type — it derives from the target); move `implements:` values
found on requirements onto the referenced spec's `implements:` (merge, dedupe)
and delete the requirement-side field; rename `satisfies:` to `implements:`;
move_file stray documents into their family's folder (the folder is the
default location; the frontmatter type decides the family). ask_user before
any delete_file. Work in small batches and summarize what changed.
