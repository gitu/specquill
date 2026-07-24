---
type: Specification
title: Multi-tenancy — architecture and boundaries (deprecated)
status: deprecated
satisfies: [requirements/REQ-019.md, requirements/REQ-020.md]
updated: 2026-07-24
---

# Multi-tenancy — architecture and boundaries (deprecated)

**Deprecated 2026-07-24.** The product now targets two single-tenant
deployment shapes:

- **v1 — per-tenant deployment**: one deployment per customer/tenant,
  serving a single writable repository (plus read-only reference sources),
  login through any OIDC provider.
- **v2 — developer local**: the binary on a developer's machine, local
  auth (`auth.local` / `-dev`), no OIDC.

Isolation between tenants is therefore *between deployments* (separate
processes, databases and data directories), not inside one process. The
in-process tenancy foundation this document designed (tenant tables,
`<tenant>/<repo>` keys, the `X-SpecQuill-Tenant` header, GitHub App
installations as tenants) was removed; the canonical repo key is the plain
repo id and repos live under `data/runtime/repos/<repo>/`.

The full phase A/B design lives in this file's git history.
