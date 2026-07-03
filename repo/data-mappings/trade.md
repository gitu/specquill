---
id: MAP-trade
title: Trade reporting — field lineage
status: in_review
source_system: OMS v4
target_model: trade
verifies:
  - requirements/REQ-042.md
updated: 2026-06-28
---

# Trade — field lineage

Source system `OMS v4` → reporting model `trade`.

| # | Source (OMS v4) | Target                     | Transform             | Owner | Status  |
|---|-----------------|----------------------------|-----------------------|-------|---------|
| 1 | oms.exec_time   | trade.executionTimestamp   | to ISO-8601, μs prec. | data  | ⚠ drift |
| 2 | oms.venue_mic   | trade.venue                | operating → segment MIC | data | ✓ ok  |
| 3 | acct.lei_code   | account.lei                | passthrough           | ref   | ✓ ok    |
| 4 | oms.px          | trade.price                | decimal(18,8)         | data  | ✓ ok    |
| 5 | oms.qty         | trade.quantity             | decimal(18,4)         | data  | ✓ ok    |

## Drift — #1 `trade.executionTimestamp` {#executionTimestamp}
The RTS 22 amendment (2026-06) requires **microsecond** precision. The current transform
emits second precision → status `⚠ drift`. Fix tracked in
[changes/2026-06-mifid-rts22.md](../changes/2026-06-mifid-rts22.md) (PR #128).

## `trade.venue` {#venue}
Resolved per [specs/venue.md](../specs/venue.md).
