---
id: CHG-2026-06-partial-fills
title: Support partial-fill reporting
source: product
authority: Trading desk
reference: PM-2026-06
published: 2026-06-25
detected: 2026-06-25
status: in_progress
pr: 131
branch: feature/partial-fills
impacts:
  requirements: [REQ-063]
  specs: [specs/txn-report.md]
  mappings: [data-mappings/trade.md#quantity]
  fields: [trade.quantity]
ai_summary: >
  New product requirement REQ-063 adds a fills[] array to the trade model so each partial
  execution is reportable while rolling up to its parent order. Grounded on
  repo:trading-specs, it touches 1 requirement, 1 spec and 1 data field; no regulatory
  driver. A new spec section is proposed in PR #131.
---

# Support partial-fill reporting

## What changed
When an order executes across multiple partial fills, each fill should be reportable
individually while rolling up to the parent order.

## Impact (AI-assessed)
- **REQ-063** Partial-fill Reporting — new requirement
- **specs/txn-report.md** — add `fills[]` to the trade model
- **data-mappings/trade.md** — `trade.quantity` per-fill

## Proposed edit → PR #131

```diff
  ## Data mapping
+ | oms.fill_qty | trade.fills[].quantity | decimal(18,4) | ✓ ok |
```
