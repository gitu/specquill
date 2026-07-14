---
id: PROD-partial-fills
title: PM request 2026-06 — partial fills
type: product_driver
status: active
owner: d.laurent
sponsor: Product
drives:
  - requirements/REQ-063.md
updated: 2026-06-05
---

# PM request 2026-06 — partial fills

Clients splitting large orders across venues see only the parent order in
reporting exports. Product asks for partial executions to be first-class:
each fill reportable on its own, rolling up to its parent order.

## Ask

- Every partial execution carries its own reportable record.
- Fills reference their parent order; exports can roll up or itemize.

## Drives

- [REQ-063 · Partial-Fill Reporting](../requirements/REQ-063.md).
