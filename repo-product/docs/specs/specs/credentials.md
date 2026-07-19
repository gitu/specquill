---
type: Specification
title: Credentials — tenant-owned encrypted repo tokens
status: in_review
satisfies: [requirements/REQ-023.md]
updated: 2026-07-19
---

# Credentials — tenant-owned encrypted repo tokens

How [REQ-023](../requirements/REQ-023.md) is realized. GitHub-App tenants
authenticate git with installation tokens ([tenants.md](tenants.md)); this
spec covers everything else — private remotes on other hosts, config-tenant
repos without an installation, reference sources — connected with a token a
tenant admin enters at runtime.

## Model

A **credential** is a named, tenant-owned secret: a label (`"deploy PAT"`),
an optional basic-auth username (empty → `x-access-token`), and a token.
Credentials are entities, not per-repo fields — one PAT can back several
repos, and rotating it is one operation. A repo references at most one
credential (`tenant_repos.credential_id`; likewise
`sources.credential_id`); deleting a referenced credential is refused
until it is detached.

## Encryption at rest

Tokens are sealed with **AES-256-GCM** before they touch Postgres:

- Master key: env var `SPECQUILL_SECRET_KEY` (base64, 32 bytes). Config
  files never carry key material.
- Each row stores a random 12-byte nonce, the ciphertext, and a `key_id`
  (`v1`) — the rotation seam: a future key becomes `v2`, rows re-seal on
  their next update.
- The GCM additional authenticated data binds the ciphertext to the owning
  tenant, so a row copied across tenants fails to decrypt.

Without `SPECQUILL_SECRET_KEY` the server boots normally (installation
tokens and `token_env` still work); only the credential endpoints answer
`501 secrets_unconfigured`.

## Never echoed

A token is write-only past the moment it is entered:

- List/read endpoints return id, name, username, dates and reference count
  — never token material, ciphertext or nonce.
- Rotation replaces the token without ever displaying the old one.
- Logs and error messages carry credential ids and labels only; decrypt
  failures log without plaintext and fall through to the next resolution
  step.

## Resolution order

The git layer resolves credentials through one seam (`Manager.TokenFor`),
in precedence order:

1. **Installation token** — github-provider tenant with a covering
   installation ([tenants.md](tenants.md)).
2. **Tenant credential** — the repo's attached credential row, unsealed
   with the master key.
3. **Environment fallback** — the repo/source's configured `token_env`
   (self-host, operator-managed).

Whatever wins, tokens reach git via child-process environment only —
never argv, never config files — preserving the existing
`credentialArgsEnv` invariant.

## API

All under the tenant prefix ([urls.md](urls.md)), tenant-admin gated:

| route | effect |
|---|---|
| `GET /api/t/{tenant}/credentials` | list (redacted) |
| `POST /api/t/{tenant}/credentials` | create `{name, username?, token}` |
| `PUT /api/t/{tenant}/credentials/{id}` | rename / rotate (token present → re-seal) |
| `DELETE /api/t/{tenant}/credentials/{id}` | delete; `409` while referenced |
| `PUT /api/t/{tenant}/repos/{repo}/settings/credential` | attach/detach (repo admin) |

Connecting a repo accepts an inline shortcut —
`POST …/projects {…, credential: {token}}` creates the credential and
attaches it in one step. The clone runs **after** the rows exist, so the
first fetch already authenticates with the new credential.

## UI

The Admin view gains a credentials panel (list, per-row rotate, delete)
and the connect-repo form gains an optional token field (password input,
cleared on success) beside the workspace/reference mode choice.
