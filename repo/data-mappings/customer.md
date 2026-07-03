---
id: MAP-customer
title: Customer / account — field lineage
status: approved
source_system: CRM
target_model: account
updated: 2026-04-09
---

# Customer / account — field lineage

Source system `CRM` → reporting model `account`.

| # | Source (CRM)  | Target        | Transform          | Owner | Status |
|---|---------------|---------------|--------------------|-------|--------|
| 1 | crm.lei       | account.lei   | validate ISO 17442 | ref   | ✓ ok   |
| 2 | crm.legal_nm  | account.name  | trim / normalise   | ref   | ✓ ok   |
| 3 | crm.acct_type | account.type  | enum map           | ref   | ✓ ok   |
