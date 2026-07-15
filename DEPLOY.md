# Deploying SpecQuill

Two supported shapes — pick one:

- **[deploy/local.md](deploy/local.md)** — self-host: the single binary (or
  container image), your Postgres, one YAML file, a reverse proxy. Right for
  a team server, a homelab, or anything behind a VPN.
- **[deploy/cloud.md](deploy/cloud.md)** — the managed pipeline: GitHub
  Actions builds the image once, Cloud Build rolls it out to Cloud Run
  (staging on every push to `main`, production on `v*` tags), Neon Postgres,
  Secret Manager, keyless auth via Workload Identity Federation.

Both share the same config format and constraints: content lives in git,
state in Postgres, `data_dir` is a rebuildable cache, and exactly **one
instance** runs at a time (in-process collab hub + local worktrees).
