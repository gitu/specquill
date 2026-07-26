# Deploying SpecQuill

**[deploy/local.md](deploy/local.md)** is the deployment guide — the single
binary (or the container image), one YAML file, a persistent directory, a
reverse proxy. That covers both target shapes: a per-tenant instance you or a
customer runs, and a developer running it on their own machine.

Constraints, wherever it runs:

- **Content lives in git.** Documents are only ever markdown in the
  configured remotes.
- **The server's own state is a SQLite file in `data_dir`** (users, sessions,
  review state, collab logs), so `data_dir` needs persistent storage and is
  what you back up.
- **Exactly one instance** runs at a time: the collab hub is in-process, the
  worktrees are on local disk, and SQLite takes a single writer.

## Removed: the Cloud Run pipeline (2026-07-25)

SpecQuill previously shipped a managed pipeline — GitHub Actions built the
image, a deploy-only Cloud Build trigger rolled it out to Cloud Run (staging
on `main`, production on `v*` tags) with Neon Postgres, Secret Manager and
Workload Identity Federation. It was dropped along with Postgres.

The reasoning, so it does not get rediscovered: the store is now an embedded
SQLite file, and Cloud Run has no persistent disk (SQLite is unsafe on its
GCS-FUSE and NFS volume mounts, neither of which gives it real file locking).
Keeping Cloud Run would have meant carrying a second storage backend
indefinitely — and since the collab hub already forces `--max-instances=1`
with a warm minimum instance, that deployment was paying Cloud Run's
statelessness constraint without getting its elasticity in return. Any host
with a real disk (a small VM, or a container platform with volumes) runs the
same image with one backend and no database to operate.

`.github/workflows/docker.yml` still builds and pushes the image to ghcr.io;
only the deploy handoff is gone. The old pipeline is in git history if it is
ever needed.
