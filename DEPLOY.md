# Deploying SpecQuill

Two supported shapes — pick one:

- **[deploy/local.md](deploy/local.md)** — self-host: the single binary (or
  container image), one YAML file, a persistent directory, a reverse proxy.
  Right for a team server, a homelab, or anything behind a VPN. **This is the
  supported shape.**
- **[deploy/cloud.md](deploy/cloud.md)** — the managed pipeline: GitHub
  Actions builds the image once, Cloud Build rolls it out to Cloud Run
  (staging on every push to `main`, production on `v*` tags), Secret Manager,
  keyless auth via Workload Identity Federation. ⚠️ **Its storage story is
  currently unresolved** — the store is an embedded SQLite file and Cloud
  Run's disk is ephemeral; see the banner in that document.

Both share the same config format and constraints: content lives in git, the
server's own state is a SQLite file in `data_dir` (which therefore needs
persistent storage), and exactly **one instance** runs at a time (in-process
collab hub, local worktrees, single-writer SQLite).
