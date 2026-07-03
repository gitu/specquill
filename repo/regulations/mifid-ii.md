---
id: REG-mifid-ii
title: MiFID II
type: regulation
authority: ESMA
jurisdiction: EU
status: active
anchors: [rts-22-art-26]
drives:
  - requirements/REQ-042.md
  - requirements/REQ-051.md
amendments:
  - id: "2026-06"
    ref: RTS 22 §2
    summary: Execution timestamp precision → microseconds
    change: changes/2026-06-mifid-rts22.md
updated: 2026-06-27
---

# MiFID II

Markets in Financial Instruments Directive II — transaction reporting obligations.

## RTS 22 Art. 26 — Transaction reporting {#rts-22-art-26}

Investment firms which execute transactions in financial instruments shall report
complete and accurate details of those transactions to the competent authority as
quickly as possible, and **no later than the close of the following working day**.

> Drives: [REQ-042 · Transaction Reporting](../requirements/REQ-042.md),
> [REQ-051 · Exception Handling](../requirements/REQ-051.md)

## Amendments

### 2026-06 — Timestamp precision
Execution timestamps must be captured and reported to **microsecond** precision
(previously: second). See change record → [changes/2026-06-mifid-rts22.md](../changes/2026-06-mifid-rts22.md).
