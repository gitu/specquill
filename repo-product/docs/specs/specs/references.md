---
type: Specification
title: References — sources, grants, grounding
status: draft
satisfies: [requirements/REQ-004.md]
updated: 2026-07-27
---

# References — sources, grants, grounding

How [REQ-004](../requirements/REQ-004.md) is realized. The chain depends on
the deployment's auth mode — the two modes answer "who may this config
grant access to?" differently.

## The chain (local-auth mode)

The server holds shared credentials, so definitions must stay server-side:

1. **Catalog** — named sources (`kind: git | url | openapi | confluence`)
   with remote + a credential *environment variable name*.
2. **Grants** — tenant admins attach catalog entries to the tenant.
3. **Selection** — the in-repo config lists references by source name, with
   optional path filters; effective references are the intersection of
   selection and grants, resolved from the default branch only.
4. **Roles** — the per-repo ladder `viewer < editor < maintainer < admin`
   gates reads, writes, protected merges and administration
   ([authentication.md](authentication.md)).

## The chain (forge-PAT mode)

There are no shared credentials to protect, so the definitions move into
the repo and the forge becomes the gate ([forge-auth.md](forge-auth.md)):

1. **Definition** — the in-repo config's `sources:` names the reference
   repos (git, https remotes only). Defining one grants nothing.
2. **The user's own token** — every clone/fetch of a source runs with the
   requesting user's PAT into that user's isolated storage; the forge's
   permission check is the authorization. A source the token cannot reach
   fails for that user and materializes nowhere.
3. **Selection** — `references:` selects among the repo's own definitions
   (paths, grounding), still from the default branch only, so a workspace
   edit cannot redirect references before it is reviewed on the forge.
4. **Roles** — same ladder; the deployment role itself derives from the
   forge ([authentication.md](authentication.md)).

Importer sources (`url | openapi | confluence`) remain a catalog feature of
local-auth deployments — their credentials are env-vars, which the
credential-free forge mode deliberately lacks.

## Grounding

Grounded references join the copilot context under `~<source>/<path>`
headings inside a budget; draft edits remain restricted to project files —
a reference path in a model reply is refused.

## Cross-repo traceability

A document may link a granted source's file with a `~<source>/<path>` link;
these render as external nodes in the traceability graph, so a spec's
dependency on an upstream regulation or API contract stays visible without
importing that content into the project.
