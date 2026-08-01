-- specquill auth/session metadata (SQLite, a file in the data dir). Content
-- lives in git; this DB holds only users, sessions and workspace claims —
-- never documents. Single-tenant: one deployment serves one workspace; the
-- canonical repo key in all other tables is the plain repo id.
--
-- Foreign keys are enforced (PRAGMA foreign_keys=ON in store.Open) — the
-- repo_grants / repo_grant_invites cascades depend on it.

CREATE TABLE IF NOT EXISTS users (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  provider   TEXT NOT NULL,             -- 'oidc' | 'local'
  subject    TEXT NOT NULL,             -- OIDC sub / local username
  name       TEXT NOT NULL,
  email      TEXT NOT NULL,
  role       TEXT NOT NULL DEFAULT '',  -- admin | member | viewer | '' (not enrolled)
  UNIQUE(provider, subject)
);

CREATE TABLE IF NOT EXISTS local_users (
  user_id     BIGINT PRIMARY KEY REFERENCES users(id),
  username    TEXT UNIQUE NOT NULL,
  argon2_hash TEXT NOT NULL             -- encoded: argon2id$v$m$t$p$salt$hash
);

CREATE TABLE IF NOT EXISTS sessions (
  id         TEXT PRIMARY KEY,          -- opaque 256-bit random hex
  user_id    BIGINT NOT NULL REFERENCES users(id),
  created_at BIGINT NOT NULL,           -- unix seconds
  expires_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);

-- the deployment's repo registry, mirroring the YAML list at boot
-- (managed_by='config'); rows added through the API persist across boots.
CREATE TABLE IF NOT EXISTS repos (
  repo_id        TEXT PRIMARY KEY,
  mode           TEXT NOT NULL,           -- writable | readonly
  remote         TEXT NOT NULL,
  default_branch TEXT NOT NULL DEFAULT 'main',
  managed_by     TEXT NOT NULL DEFAULT 'config',  -- config rows reconcile at boot
  created_at     BIGINT NOT NULL
);



-- personal workspace branch ownership (ws/<slug> claimed per user)
CREATE TABLE IF NOT EXISTS workspace_branches (
  repo    TEXT NOT NULL,
  user_id BIGINT NOT NULL REFERENCES users(id),
  branch  TEXT NOT NULL,
  PRIMARY KEY (repo, user_id),
  UNIQUE (repo, branch)
);

-- real-time co-editing was removed (July 2026); this schema runs on every
-- store.Open, so the drops clean up databases from older versions
DROP TABLE IF EXISTS collab_updates;
DROP TABLE IF EXISTS collab_contributors;
DROP TABLE IF EXISTS collab_rooms;


-- projects & sources (config-split plan): a project is a writable workspace
-- (repo + content_root); a source is a catalog entry projects may reference.
-- managed_by: 'config' rows reconcile to the YAML at boot, 'api' rows persist.
CREATE TABLE IF NOT EXISTS projects (
  project_id   TEXT PRIMARY KEY,
  repo_id      TEXT NOT NULL,           -- repos.repo_id
  content_root TEXT NOT NULL DEFAULT '',
  managed_by   TEXT NOT NULL DEFAULT 'config',   -- config | api
  created_at   BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS sources (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  name           TEXT NOT NULL UNIQUE,
  kind           TEXT NOT NULL,                  -- git | url | openapi | confluence
  remote         TEXT NOT NULL,
  token_env      TEXT NOT NULL DEFAULT '',       -- env var NAME; never a secret value
  credential_ref TEXT NOT NULL DEFAULT '',       -- hosted future (Secret Manager path)
  default_branch TEXT NOT NULL DEFAULT 'main',
  sync_interval  BIGINT NOT NULL DEFAULT 300,    -- seconds
  managed_by     TEXT NOT NULL DEFAULT 'config',
  created_at     BIGINT NOT NULL
);

-- last-import status per non-git (importer) source, keyed by source name.
-- Populated by importer.Runner; surfaced in the sources list + sync API.
CREATE TABLE IF NOT EXISTS source_syncs (
  name       TEXT PRIMARY KEY,
  status     TEXT NOT NULL,                       -- ok | error
  error      TEXT NOT NULL DEFAULT '',
  file_count INT NOT NULL DEFAULT 0,
  head_sha   TEXT NOT NULL DEFAULT '',
  synced_at  BIGINT NOT NULL
);

-- source-drift runs: one row per AI drift check over a doc scope. Derived
-- state (like source_syncs) — the durable artifact of a filed finding is the
-- work-items frontmatter entry in the document itself.
CREATE TABLE IF NOT EXISTS drift_runs (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_key           TEXT NOT NULL,
  branch             TEXT NOT NULL,
  mode               TEXT NOT NULL DEFAULT 'drift', -- drift (per-doc verify) | gaps (per-source coverage)
  status             TEXT NOT NULL,               -- running | ok | error | cancelled
  error              TEXT NOT NULL DEFAULT '',
  scope_json         TEXT NOT NULL DEFAULT '[]',  -- frozen resolved doc list (gaps: source list)
  docs_total         INT NOT NULL DEFAULT 0,
  docs_done          INT NOT NULL DEFAULT 0,
  dropped_unverified INT NOT NULL DEFAULT 0,      -- findings whose evidence failed verification
  head_sha           TEXT NOT NULL DEFAULT '',
  started_at         BIGINT NOT NULL,
  finished_at        BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS drift_runs_repo ON drift_runs(repo_key, branch, id);

-- drift findings, keyed by the anchor-based fingerprint so dismissals stick
-- and re-runs upsert instead of duplicating. resolved_at != 0 = the last run
-- over the doc no longer reported it. Coverage gaps (mode 'gaps') have
-- doc_path = '' — no document covers them yet; suggested_path is where one
-- should live and draft_path the reverse-engineered draft once created.
CREATE TABLE IF NOT EXISTS drift_findings (
  repo_key         TEXT NOT NULL,
  branch           TEXT NOT NULL,
  fingerprint      TEXT NOT NULL,
  run_id           BIGINT NOT NULL,
  doc_path         TEXT NOT NULL,
  suggested_path   TEXT NOT NULL DEFAULT '',
  draft_path       TEXT NOT NULL DEFAULT '',
  anchor           TEXT NOT NULL DEFAULT '',
  source           TEXT NOT NULL DEFAULT '',
  kind             TEXT NOT NULL DEFAULT '',
  severity         TEXT NOT NULL DEFAULT 'medium',
  title            TEXT NOT NULL DEFAULT '',
  detail           TEXT NOT NULL DEFAULT '',
  evidence_json    TEXT NOT NULL DEFAULT '[]',
  status           TEXT NOT NULL DEFAULT 'open',  -- open | dismissed | filed
  work_item_url    TEXT NOT NULL DEFAULT '',
  work_item_target TEXT NOT NULL DEFAULT '',
  created_at       BIGINT NOT NULL,
  updated_at       BIGINT NOT NULL,
  resolved_at      BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (repo_key, branch, fingerprint)
);

-- unauthenticated OKF-bundle share links: the URL token is the only
-- credential (LLM copy-paste use case). One active link per project;
-- minting again rotates the token, deleting revokes access.
CREATE TABLE IF NOT EXISTS share_links (
  project_id TEXT PRIMARY KEY,
  token      TEXT NOT NULL UNIQUE,
  created_by BIGINT NOT NULL REFERENCES users(id),
  created_at BIGINT NOT NULL
);

-- token-scoped dynamic projects (REQ-025): PER-USER rows — one user's opened
-- repositories are their own state, never another's. project_id is the
-- derived stable id (dyn.<forge repo id>[.<name>]); role is the user's forge
-- permission on the repository, refreshed at every open (REQ-025.3).
CREATE TABLE IF NOT EXISTS user_projects (
  user_id        BIGINT NOT NULL REFERENCES users(id),
  project_id     TEXT NOT NULL,
  forge_repo_id  TEXT NOT NULL,
  name           TEXT NOT NULL DEFAULT '',      -- manifest subproject name ('' = repo root)
  spelling       TEXT NOT NULL,                 -- human form owner/repo[#name]
  remote         TEXT NOT NULL,
  content_root   TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL DEFAULT 'main',
  role           TEXT NOT NULL DEFAULT 'viewer',
  created_at     BIGINT NOT NULL,
  last_used      BIGINT NOT NULL,
  PRIMARY KEY (user_id, project_id)
);

-- last-use stamps per user clone (scope = 'u<id>'), fed by request
-- resolution and read by the reclamation janitor (REQ-025.6).
CREATE TABLE IF NOT EXISTS clone_uses (
  scope     TEXT NOT NULL,
  repo_id   TEXT NOT NULL,
  last_used BIGINT NOT NULL,
  PRIMARY KEY (scope, repo_id)
);

-- per-repo user grants (REQ-020): explicit access layered on the deployment
-- role; effective role = max(deployment role, granted).
CREATE TABLE IF NOT EXISTS repo_grants (
  repo_id    TEXT   NOT NULL REFERENCES repos(repo_id) ON DELETE CASCADE,
  user_id    BIGINT NOT NULL REFERENCES users(id),
  role       TEXT   NOT NULL DEFAULT 'viewer',   -- viewer | member (repo/project management is admin-scoped)
  granted_by BIGINT REFERENCES users(id),
  created_at BIGINT NOT NULL,
  PRIMARY KEY (repo_id, user_id)
);
CREATE INDEX IF NOT EXISTS repo_grants_user ON repo_grants(user_id);

-- pending grants for users who have not logged in yet; the matcher is a
-- lowercased email, claimed (converted to repo_grants rows) and deleted on
-- the invitee's first login.
CREATE TABLE IF NOT EXISTS repo_grant_invites (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_id    TEXT   NOT NULL REFERENCES repos(repo_id) ON DELETE CASCADE,
  matcher    TEXT   NOT NULL,                    -- lowercased email
  role       TEXT   NOT NULL DEFAULT 'viewer',
  granted_by BIGINT NOT NULL REFERENCES users(id),
  created_at BIGINT NOT NULL,
  UNIQUE (repo_id, matcher)
);
