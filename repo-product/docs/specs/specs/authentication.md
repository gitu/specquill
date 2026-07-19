---
type: Specification
title: Authentication — layers, providers, roles
status: in_review
satisfies: [requirements/REQ-017.md, requirements/REQ-020.md, requirements/REQ-021.md, requirements/REQ-022.md]
updated: 2026-07-19
---

# Authentication — layers, providers, roles

How [REQ-017](../requirements/REQ-017.md),
[REQ-021](../requirements/REQ-021.md) and the identity side of
[REQ-022](../requirements/REQ-022.md) are realized.

## The four layers

Access is decided in four independent layers, each with its own
configuration surface and its own failure mode:

1. **Identity** — *who is the user.* Resolved by a login provider (GitHub
   OAuth, OIDC, local). Users are global; one `users` row per provider
   subject, valid across every tenant.
2. **Tenancy & repo connection** — *which installation, which repos.* A
   tenant is a GitHub App installation or the deployment's configured
   on-prem tenant. Repos connect through installation grants or through
   tenant-owned credentials ([credentials.md](credentials.md)); connecting
   a repo is a tenant-admin action, never a side effect of logging in.
3. **Repo permissions** — *what may this user do here.* A four-level
   per-repository ladder (below), derived from the git host and/or granted
   explicitly ([tenants.md](tenants.md)).
4. **Project configuration** — *what does this workspace see.* The in-repo
   `.specquill/config.yml` selects granted reference sources and carries
   taxonomy/UI config ([references.md](references.md)). It can narrow,
   never widen, access.

A request is evaluated strictly top-down: no session → no tenant; no
tenant visibility → no repo; no repo role → no route; and the in-repo
config only ever intersects with what the layers above granted.

## Providers (layer 1)

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
carry no secrets. Auth endpoints stay tenant-free: `/auth/*` and
`/api/me` are the only authenticated routes outside the `/api/t/{tenant}`
prefix ([urls.md](urls.md)).

## Access gate

`auth.github.allowed_users` lists the GitHub handles admitted
(case-insensitive). Anyone else completes the OAuth dance but gets no
session — they land on the login page with an explanatory error. An empty
list admits any GitHub account: acceptable behind a VPN, an operator
decision on a public URL.

## Repo roles: the four-level ladder (layer 3)

Every repository route is gated by one effective role per (user, repo),
`viewer < editor < maintainer < admin`:

| role | may |
|---|---|
| `viewer` | read everything, comment on PRs |
| `editor` | + write workspace (`ws/`) branches, commit, open/approve/close PRs, co-edit, use the copilot |
| `maintainer` | + merge PRs into protected branches, mint/revoke share links |
| `admin` | + manage the repo's grants and settings (credential attachment) |

Derived roles come from the git host (GitHub permission mapping:
`pull` → viewer, `triage`/`push` → editor, `maintain` → maintainer,
`admin` → admin) or, for config-provider tenants, from the tenant's default role; explicit per-repo
grants ([REQ-020](../requirements/REQ-020.md)) layer on top. The
effective role is **max(derived, granted)**.

Repo `admin` is distinct from tenant admin: the tenant-level role gates
tenant management (projects, sources, members, credentials), and a tenant
admin derives repo admin on every repo — but a user can also hold `admin`
on a single repository (GitHub repo admin, or an explicit grant) without
any tenant management rights.

## Tenant enrollment and the admin bootstrap (layer 2)

The on-prem tenant is declared in config
([multi-tenancy.md](multi-tenancy.md)):

```yaml
tenant:
  slug: acme            # defaults to "default" when omitted
  display_name: Acme
  default_role: editor  # none | viewer | editor | maintainer
  admin_emails: [ops@acme.example]
```

Every authenticated user is auto-enrolled into this tenant with
`default_role`: `editor` (the default — self-host semantics), `viewer`,
`maintainer`, or `none`. With `none`, users have no tenant until an admin
grants them a repository ([REQ-020](../requirements/REQ-020.md)) — the
restricted mode for on-prem deployments where, say, a GitLab-hosted spec
repo is opened to a user without any git-host account.

`tenant.admin_emails` is the bootstrap: users whose email matches
(case-insensitive, any provider) are promoted to admin in config-provider
tenants on login — including under `default_role: none`, so a fresh
restricted deployment still has a reachable management API.

On every successful login (any provider) pending grant invites matching the
identity's email — or GitHub login, for github-kind invites — are claimed:
converted into repo grants and deleted, so an invited external reviewer has
access the moment they first sign in.

The GitHub App edge (one tenant per installation, roles derived from GitHub
repo permissions) replaces this static enrollment for github-provider
tenants; the config tenant keeps it.
