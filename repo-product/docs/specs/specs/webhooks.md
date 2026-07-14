---
type: Specification
title: Webhooks — push-triggered repository sync
status: in_review
satisfies: [requirements/REQ-018.md]
updated: 2026-07-15
---

# Webhooks — push-triggered repository sync

How [REQ-018](../requirements/REQ-018.md) is realized.

## Endpoint

`POST /hooks/github` — sessionless and CSRF-exempt: the
`X-Hub-Signature-256` HMAC-SHA256 signature over the raw body, verified in
constant time against the secret named by `webhooks.github.secret_env`, is
the only authentication. `ping` events answer ok (GitHub's delivery test);
unknown event types are acknowledged and ignored.

## Matching

The `push` payload's `repository.full_name` is compared against every
registered repo's configured remote, normalized to `owner/repo` — https,
`ssh://` and scp-like `git@host:owner/repo` remotes all resolve; local-path
remotes never match. Matching is exact and case-insensitive, so a webhook
can only ever touch repos that genuinely point at the pushed repository.

## Effect

Each matched repo is fetched immediately. When the pushed ref is a writable
project's default branch, the local branch fast-forwards (the same
never-merge rule as a manual pull — a diverged branch is logged and left
alone) and its worktree updates, so readers see the pushed state on the next
request. `fetch`/`pull` events go out on the bus, updating connected
clients. Every failure is non-fatal: the per-repo `sync_interval` polling
(2-minute default for projects) remains the correctness backstop, making
webhook delivery purely a latency optimization.

## Operations

One repository webhook per repo that should sync instantly (the writable
specs repo first, read-only git sources optionally), pointed at
`<base_url>/hooks/github` with the shared secret. Registration is a single
`gh api repos/…/hooks` call — see DEPLOY.md.
