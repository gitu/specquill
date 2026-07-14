---
id: PROD-ops-t1-sla
title: Ops T+1 settlement SLA
type: product_driver
status: active
owner: k.osei
sponsor: Operations
drives:
  - requirements/REQ-042.md
updated: 2026-04-02
---

# Ops T+1 settlement SLA

Operations commits to settling all reportable transactions by close of the
following working day (**T+1**). The SLA is contractual with two prime-broker
clients from Q3 2026 and is blocked on transaction reporting landing at the
ARM within the same window.

## Ask

- Reporting completes no later than T+1 close for every reportable execution.
- Late or failed submissions surface to the ops dashboard within 15 minutes.

## Drives

- [REQ-042 · Transaction Reporting](../requirements/REQ-042.md) — the T+1
  deadline is jointly driven by this SLA and MiFID II RTS 22.
