---
type: Specification
title: Authentication — providers, access gate, tenant roles
status: in_review
satisfies: [requirements/REQ-017.md]
updated: 2026-07-14
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
tenant as **member** (self-host semantics). Roles are per-tenant:
`viewer < member < admin`; the management API (projects, sources, grants)
requires admin.

`auth.admin_emails` is the bootstrap: users whose email matches
(case-insensitive, any provider) are promoted to admin in config-provider
tenants on login. Without it a fresh deployment would consist of members
only, with no path to the management API.

The future GitHub App edge (one tenant per installation, roles derived from
GitHub repo permissions) replaces this static mapping for github-provider
tenants; the config tenant keeps it.
