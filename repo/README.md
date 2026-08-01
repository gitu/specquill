---
type: Guide
title: trading-specs
---

# trading-specs

Requirements-engineering workspace, stored as plain markdown in git. `specquill` (the UI)
is a **view over these files** — every rendered object on screen maps to a file below.

```
trading-specs/
├─ .specquill/config.yml        workspace taxonomy: drivers, statuses, link types
├─ regulations/               external drivers (regulatory sources)
│  ├─ mifid-ii.md
│  ├─ gdpr.md
│  └─ dora.md
├─ requirements/              REQ-### — the actual requirements
│  ├─ REQ-042.md
│  ├─ REQ-051.md
│  └─ REQ-063.md
├─ specs/                     technical specs (markdown + mermaid + excalidraw)
│  ├─ txn-report.md
│  └─ venue.md
├─ data-mappings/             field-level lineage (source → target)
│  ├─ trade.md
│  └─ customer.md
├─ diagrams/                  standalone diagram sources
│  ├─ reporting.mermaid
│  └─ data-flow.excalidraw
```

## The model
- **Requirements** are driven by one or more **drivers** — `regulatory`, `product`, or
  `technical`. Regulatory change is only one input.
- Requirements **implement** into **specs**, which **map_to** **data fields** and are
  **verified** by tests. These typed links (in frontmatter) are what the traceability
  graph and matrix are computed from.
- **Diagrams** live inline in specs as fenced ` ```mermaid ` blocks or as embedded
  `.excalidraw` files.
- Documents that only apply for a period carry a **validity window** in their
  frontmatter (`starts`/`ends`, or regulatory wording like `effective_from`) — those
  are the workspace's timed dependencies. What CHANGED is read from git history, not
  from documents about changes.

## How the UI maps to files
| UI object | File |
|---|---|
| Spec editor document | `specs/txn-report.md` |
| Requirement callout | `requirements/REQ-042.md` |
| Inline mermaid block | fenced block in `specs/txn-report.md` + `diagrams/reporting.mermaid` |
| Inline excalidraw block | `diagrams/data-flow.excalidraw` |
| Data-mapping table | `data-mappings/trade.md` |
| Diff / PR #128 | git diff of the above on `feature/mifid-update` |
| Traceability graph | computed from `drivers` / `implements` / `maps_to` / `verifies` links |
