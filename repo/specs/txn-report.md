---
id: SPEC-txn-report
title: Transaction Reporting — Technical Spec
status: in_review
implements:
  - requirements/REQ-042.md
  - requirements/REQ-051.md
  - requirements/REQ-063.md
maps_to:
  - data-mappings/trade.md
diagrams:
  - diagrams/reporting.mermaid
  - diagrams/data-flow.excalidraw
updated: 2026-06-28
---

# Transaction Reporting — Technical Spec

## Reporting flow

```mermaid
flowchart LR
  A[Trade Capture] --> B[Enrichment]
  B --> C{Valid?}
  C -->|yes| D[ARM Submission]
  C -->|no| E[Exceptions queue]
  E -.-> F[[REQ-051 · Exception Handling]]
```

## Timing & precision
- Execution timestamp recorded to the **microsecond** (RTS 22 §2, 2026-06).
- Applies to mapping `trade.executionTimestamp`.

## Data mapping

| Source field  | Target field             | Transform   | Status  |
|---------------|--------------------------|-------------|---------|
| oms.exec_time | trade.executionTimestamp | ISO-8601 μs | ⚠ drift |
| oms.venue_mic | trade.venue              | MIC lookup  | ✓ ok    |
| acct.lei_code | account.lei              | passthrough | ✓ ok    |

Full lineage → [data-mappings/trade.md](../data-mappings/trade.md).

## Source-to-target sketch

<!-- embed: excalidraw -->
![data flow](../diagrams/data-flow.excalidraw)
