---
type: Specification
title: Tenants — GitHub App installations, derived roles (deprecated)
status: deprecated
satisfies: [requirements/REQ-019.md, requirements/REQ-020.md]
updated: 2026-07-24
---

# Tenants — GitHub App installations, derived roles (deprecated)

**Deprecated 2026-07-24** with the single-tenant deployment decision: a
deployment serves exactly one tenant (v1: one deployment per customer,
OIDC; v2: a developer's local machine, local auth), so GitHub-App
installation tenants, GitHub-derived roles and the tenant switcher were
removed. [REQ-019](../requirements/REQ-019.md) is deprecated with them.

What survives moved:

- **Per-repo user grants** ([REQ-020](../requirements/REQ-020.md)) — now
  deployment-scoped, email-matched invites only; see
  [authentication.md](authentication.md).
- **Deployment roles** (`viewer < member < admin`, `auth.default_role`,
  `auth.admin_emails` bootstrap) — see
  [authentication.md](authentication.md).

Historic design (installation lifecycle webhooks, per-repo permission
derivation, repo adoption picker, installation tokens) lives in this
file's git history.
