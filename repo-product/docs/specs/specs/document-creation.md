---
type: Specification
title: Document creation — guided flow and ID schemes
status: in_review
satisfies: [requirements/REQ-015.md]
updated: 2026-07-14
---

# Document creation — guided flow and ID schemes

How [REQ-015](../requirements/REQ-015.md) is realized.

## Guided flow

The New Document dialog (tree header, per-family "+", dashboard) collects:

- **Family** — a chip per effective entity (built-ins plus the config's
  `entities:` block), showing icon, color and description. The chosen family
  fixes the target folder and the frontmatter `type:`.
- **Folder** — the family root, any existing subfolder (any depth), or a new
  subfolder typed inline; `a/b` nests, and every segment is slugified.
- **Title** — becomes frontmatter `title:` and the `#` heading.
- **ID** — prefilled from the family's scheme, editable, regenerable, with a
  live path preview. The file is created as `<folder>/[<sub>/]<ID>.md` with
  `id`/`type`/`title`/`status: draft` frontmatter, saved as a draft on the
  author's workspace branch and opened in the editor.

## ID schemes

Per-family patterns come from the workspace config:

```yaml
ids:
  requirement: { pattern: "REQ-{seq:3}" }
  decision:    { pattern: "ADR-{yyyy}-{seq:2}" }
  spec:        { pattern: "{adj}-{word}" }        # memorable word pairs
```

Tokens: `{seq}`/`{seq:N}` (next sequential number, zero-padded), `{rand:N}`
digits, `{hex:N}`, `{adj}`/`{word}` (curated word lists — memorable pairs
like `brisk-heron`), `{yy}`/`{yyyy}`, `{slug}` (kebab-cased title).
Families without a configured scheme use built-ins
(`REQ-`/`REG-`/`CHG-`/`MAP-`/`ADR-{seq:3}`); everything else names files
after the title (`{slug}`).

## Conflict detection

The taken set is the union of file-name stems in the family folder and every
frontmatter `id:` in the workspace, compared case-insensitively. `{seq}`
anchors the pattern's literal parts, so unrelated files cannot poison the
counter and the next number continues after the highest match. Random and
word tokens re-roll (bounded) around collisions; a pattern with no free
candidates, or a hand-typed duplicate, disables creation with the clash
shown inline. A `{slug}` with no title degrades to a conflict-checked
memorable word pair rather than `untitled`.
