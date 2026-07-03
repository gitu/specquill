---
id: SPEC-venue
title: Venue Identification — Technical Spec
status: approved
implements:
  - requirements/REQ-051.md
  - requirements/REQ-070.md
maps_to:
  - data-mappings/trade.md#venue
updated: 2026-05-22
---

# Venue Identification — Technical Spec

Every reportable trade SHALL carry the Market Identifier Code (MIC) of the execution
venue, resolved to its **segment MIC** where one exists.

## Resolution
1. Read `oms.venue_mic`.
2. Resolve operating MIC → segment MIC via the venue reference table.
3. Fall back to `XOFF` for off-venue execution.

Maps to `trade.venue` — see [data-mappings/trade.md](../data-mappings/trade.md).
