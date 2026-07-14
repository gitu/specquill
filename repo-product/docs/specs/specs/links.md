---
type: Specification
title: Links — resolution and verification
status: in_review
satisfies: [requirements/REQ-005.md, requirements/REQ-013.md]
updated: 2026-07-13
---

# Links — resolution and verification

How [REQ-013](../requirements/REQ-013.md) (and the link-style half of
[REQ-005](../requirements/REQ-005.md)) is realized.

## Resolution rules

One tolerant resolver is shared by the SPA, the CLI document model and the
link checker:

| href | resolves to |
|---|---|
| `~source/path.md` | the file in the granted reference source (passes through) |
| `/path/doc.md` | bundle-root-relative (the form OKF recommends) |
| everything else | standard markdown, relative to the linking document |

**Written** links default to the relative form (REQ-005.3) — it renders on
any forge; the `/`-absolute form stays readable.

## Verification

`GET /api/repos/{repo}/linkcheck?ref=…` scans every markdown file of the
branch snapshot — reserved `index.md`/`log.md` included, their listings must
stay navigable — and checks each class differently:

- **internal** — the resolved target must exist on the branch (tree +
  snapshot, so binary assets count).
- **source** — the `~source` must be granted (and selected, when the in-repo
  config selects); the file must exist at the source's default branch.
  Selection can never widen access (REQ-004).
- **external** — http(s) URLs are probed (HEAD, GET fallback) with bounded
  count, parallelism and timeout; private/loopback addresses are refused
  (the checker must not become an internal-network probe). `?external=0`
  skips probing entirely.

The report separates ok/broken/skipped per class and lists each problem with
its linking document. The dashboard's **Link health** card runs it on demand.
