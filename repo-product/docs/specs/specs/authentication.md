---
type: Specification
title: Authentication — providers, deployment roles
status: in_review
satisfies: [requirements/REQ-017.md, requirements/REQ-020.md]
updated: 2026-07-24
---

# Authentication — providers, deployment roles

How [REQ-017](../requirements/REQ-017.md) (deprecated GitHub clauses
removed) and the access side of [REQ-020](../requirements/REQ-020.md) are
realized.

## Providers

Two login providers, both optional, offered side by side — matching the two
deployment shapes (v1: per-tenant OIDC deployment; v2: developer-local):

| provider | flow | config |
|---|---|---|
| **OIDC** | authorization-code + PKCE against any discovery-capable IdP | `auth.oidc:` |
| **Local** | username/password (dev, air-gapped setups; `specquill user add`) | `auth.local:` |

`GET /auth/providers` reports what is enabled; the login page renders
exactly that. `GET /auth/login` short-circuits: OIDC redirects straight to
the IdP.

Identity is global (`users` row per provider subject). The OIDC client
secret is read from the env var named in `client_secret_env` — config files
carry no secrets.

## Deployment roles and the admin bootstrap

Every authenticated user is auto-enrolled with the deployment role from
**`auth.default_role`**: `member` (the default — self-host semantics),
`viewer`, or `none`. With `none`, users have no access until an admin
grants them a repository ([REQ-020](../requirements/REQ-020.md)) — the
restricted mode for deployments where, say, a spec repo is opened to a user
without any git-host account. Roles are `viewer < member < admin`, stored
on the user (`users.role`), with per-repo grants layered on top; the
management API (projects, sources, grants, members) requires admin.
Enrollment is sticky: a later `default_role` change never downgrades an
already-enrolled user.

`auth.admin_emails` is the bootstrap: users whose email matches
(case-insensitive, any provider) are promoted to admin on login — including
under `default_role: none`, so a fresh restricted deployment still has a
reachable management API.

On every successful login (any provider) pending grant invites matching the
identity's email are claimed: converted into repo grants and deleted, so an
invited external reviewer has access the moment they first sign in.
