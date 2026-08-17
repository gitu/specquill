---
type: Specification
title: Webhooks — push-triggered repository sync (deprecated)
status: deprecated
satisfies: [requirements/REQ-018.md]
updated: 2026-07-24
---

# Webhooks — push-triggered repository sync (deprecated)

**Deprecated 2026-07-24** with the single-tenant deployment decision: the
GitHub integration (and with it the `/hooks/github` endpoint) was removed.
Repositories sync on their per-repo `sync_interval` (2-minute default for
projects) or on demand (`POST /api/sources/{name}/sync`, the Admin "Sync
now" button); [REQ-018](../requirements/REQ-018.md) is deprecated with it.

Historic design (HMAC-authenticated `POST /hooks/github`, remote matching,
default-branch fast-forward) lives in this file's git history.
