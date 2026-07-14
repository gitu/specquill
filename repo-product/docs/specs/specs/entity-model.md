---
type: Specification
title: Entity model — document families
status: in_review
satisfies: [requirements/REQ-012.md]
updated: 2026-07-13
---

# Entity model — document families

How [REQ-012](../requirements/REQ-012.md) is realized.

## Built-in families

Six families ship with descriptions, icons and colors: regulations,
requirements, specs, data mappings, diagrams, changes. Their descriptions
render on the Model view's entity cards and as tooltips on the tree's folder
headers — the card is where a newcomer learns what a family is *for*.

## Custom families

The in-repo config extends or overrides the set (inline-map style, like
`drivers:`):

```yaml
entities:
  decision: { folder: "decisions/", label: "Decisions", icon: "◆",
              color: "#7c5cd6", description: "The WHY behind the system's shape." }
```

- Omitted fields default sensibly (`folder` = `<kind>s/`, generic icon/color).
- Overriding a built-in changes only the fields provided.
- The tree lists entity folders first (config order), then any other folder —
  unknown folders render with generic styling, never hidden.
- "New file" derives the frontmatter `type` from the family (custom kinds are
  title-cased: `decision` → `Decision`).

This workspace dogfoods the mechanism: `decisions/` and `glossary/` are
custom entities in its own `.specquill/config.yml`.
