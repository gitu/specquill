---
type: Specification
title: Authentication — providers, access gate, tenant roles
status: in_review
satisfies: [requirements/REQ-017.md, requirements/REQ-020.md]
updated: 2026-07-16
---

# Authentication — providers, access gate, tenant roles

How [REQ-017](../requirements/REQ-017.md) is realized.

## Providers

Three login providers, all optional, offered side by side:

| provider | flow | config |
|---|---|---|
| **GitHub** | OAuth2 authorization-code against github.com (GitHub is not an OIDC issuer for user login); identity from `/user`, email from `/user/emails` when the profile hides it | `auth.github:` — `client_id`, `client_secret_env`, `allowed_users`, `web_base`/`api_base` (GHE) |
| **OIDC** | authorization-code + PKCE against any discovery-capable IdP | `auth.oidc:` |
| **Local** | username/password (dev, air-gapped setups) | `auth.local:` |

`GET /auth/providers` reports what is enabled; the login page renders
exactly that. `GET /auth/login` short-circuits: OIDC redirects straight to
the IdP; a GitHub-only setup redirects straight to GitHub.

Identity is global (`users` row per provider subject; GitHub subject =
`github:<numeric id>`, so handle renames don't fork accounts). The client
secret is read from the env var named in `client_secret_env` — config files
carry no secrets.

## Access gate

`auth.github.allowed_users` lists the GitHub handles admitted
(case-insensitive). Anyone else completes the OAuth dance but gets no
session — they land on the login page with an explanatory error. An empty
list admits any GitHub account: acceptable behind a VPN, an operator
decision on a public URL.

## Tenant roles and the admin bootstrap

Every authenticated user is auto-enrolled into the built-in `default`
tenant with the role from **`auth.default_role`**: `member` (the default —
self-host semantics), `viewer`, or `none`. With `none`, users have no
tenant until an admin grants them a repository ([REQ-020](
../requirements/REQ-020.md)) — the restricted mode for on-prem deployments
where, say, a GitLab-hosted spec repo is opened to a user without any
git-host account. Roles are per-tenant (`viewer < member < admin`) with
per-repo grants layered on top; the management API (projects, sources,
grants) requires admin.

`auth.admin_emails` is the bootstrap: users whose email matches
(case-insensitive, any provider) are promoted to admin in config-provider
tenants on login — including under `default_role: none`, so a fresh
restricted deployment still has a reachable management API.

On every successful login (any provider) pending grant invites matching the
identity's email — or GitHub login, for github-kind invites — are claimed:
converted into repo grants and deleted, so an invited external reviewer has
access the moment they first sign in.

The GitHub App edge (one tenant per installation, roles derived from GitHub
repo permissions) replaces this static mapping for github-provider tenants;
the config tenant keeps it.
