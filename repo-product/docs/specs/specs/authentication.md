---
type: Specification
title: Authentication — providers, deployment roles
status: in_review
satisfies: [requirements/REQ-024.md, requirements/REQ-020.md]
updated: 2026-07-27
---

# Authentication — providers, deployment roles

How [REQ-024](../requirements/REQ-024.md) and the access side of
[REQ-020](../requirements/REQ-020.md) are realized.

## Providers

Two login providers, both optional, offered side by side — matching the two
deployment shapes (v1: per-tenant forge-PAT deployment; v2:
developer-local). OIDC was removed 2026-07-27 in favor of forge tokens —
the "accounts teams already have" driver is served by the git host itself,
which additionally already knows who may touch which repository.

| provider | flow | config |
|---|---|---|
| **Forge PAT** | a personal access token from the deployment's GitLab/GitHub, verified against the forge identity API ([forge-auth.md](forge-auth.md)) | `auth.forge:` |
| **Local** | username/password (dev, air-gapped setups; `specquill user add`) | `auth.local:` |

`GET /auth/providers` reports what is enabled — in forge mode including the
kind, required scopes and a prefilled token-creation deep link
(REQ-024.6); the login page renders exactly that.

Identity is global (`users` row per provider subject; forge logins key on
the forge's stable numeric user id). Config files carry no secrets — the
forge mode has no server-side credentials at all, local-mode git remotes
use `token_env` environment variables.

## Deployment roles and the admin bootstrap

**Forge mode**: the deployment role is derived from the user's permission
on the main project and refreshed at every login (REQ-024.2) —
`auth.default_role` acts only as the floor for enrollment.

**Local mode**: every authenticated user is auto-enrolled with the
deployment role from **`auth.default_role`**: `editor` (the default —
self-host semantics), `viewer`, or `none`. With `none`, users have no
access until an admin grants them a repository
([REQ-020](../requirements/REQ-020.md)) — the restricted mode for
deployments where, say, a spec repo is opened to a user without any
git-host account. Enrollment is sticky: a later `default_role` change
never downgrades an already-enrolled user.

Roles are `viewer < editor < maintainer < admin`, stored on the user
(`users.role`), with per-repo grants layered on top; the management API
(projects, sources, grants, members) requires admin.

`auth.admin_emails` is the bootstrap: users whose email matches
(case-insensitive, any provider) are promoted to admin on login — including
under `default_role: none`, so a fresh restricted deployment still has a
reachable management API.

On every successful login (any provider) pending grant invites matching the
identity's email are claimed: converted into repo grants and deleted, so an
invited external reviewer has access the moment they first sign in.
