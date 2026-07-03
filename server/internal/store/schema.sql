-- reqbase review/auth metadata. Content lives in git; this DB holds only
-- users, sessions, and PR review state.

CREATE TABLE IF NOT EXISTS users (
  id         INTEGER PRIMARY KEY,
  provider   TEXT NOT NULL,             -- 'oidc' | 'local'
  subject    TEXT NOT NULL,             -- OIDC sub / local username
  name       TEXT NOT NULL,
  email      TEXT NOT NULL,
  UNIQUE(provider, subject)
);

CREATE TABLE IF NOT EXISTS local_users (
  user_id     INTEGER PRIMARY KEY REFERENCES users(id),
  username    TEXT UNIQUE NOT NULL,
  argon2_hash TEXT NOT NULL             -- encoded: argon2id$v$m$t$p$salt$hash
);

CREATE TABLE IF NOT EXISTS sessions (
  id         TEXT PRIMARY KEY,          -- opaque 256-bit random hex
  user_id    INTEGER NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS prs (
  id            INTEGER PRIMARY KEY,
  repo          TEXT NOT NULL,
  number        INTEGER NOT NULL,
  title         TEXT NOT NULL,
  body          TEXT,
  source_branch TEXT NOT NULL,
  target_branch TEXT NOT NULL,
  author_id     INTEGER NOT NULL REFERENCES users(id),
  state         TEXT NOT NULL DEFAULT 'open',   -- open|merged|closed
  merged_commit TEXT,
  created_at    INTEGER NOT NULL,
  merged_at     INTEGER,
  UNIQUE(repo, number)
);

CREATE TABLE IF NOT EXISTS pr_comments (
  id              INTEGER PRIMARY KEY,
  pr_id           INTEGER NOT NULL REFERENCES prs(id),
  author_id       INTEGER NOT NULL REFERENCES users(id),
  file_path       TEXT,                 -- NULL = general comment
  line            INTEGER,
  anchored_commit TEXT,
  body            TEXT NOT NULL,
  resolved        INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL
);

-- personal workspace branch ownership (ws/<slug> claimed per user)
CREATE TABLE IF NOT EXISTS workspace_branches (
  repo    TEXT NOT NULL,
  user_id INTEGER NOT NULL REFERENCES users(id),
  branch  TEXT NOT NULL,
  PRIMARY KEY (repo, user_id),
  UNIQUE (repo, branch)
);

-- real-time co-editing: room state, opaque Yjs update log, contributor sets
CREATE TABLE IF NOT EXISTS collab_rooms (
  repo        TEXT NOT NULL,
  branch      TEXT NOT NULL,
  path        TEXT NOT NULL,
  last_seq    INTEGER NOT NULL DEFAULT 0,
  seed_seq    INTEGER NOT NULL DEFAULT 0,
  flushed_seq INTEGER NOT NULL DEFAULT 0,
  flushed_sha TEXT NOT NULL DEFAULT '',
  updated_at  INTEGER NOT NULL,
  PRIMARY KEY (repo, branch, path)
);
CREATE TABLE IF NOT EXISTS collab_updates (
  repo    TEXT NOT NULL,
  branch  TEXT NOT NULL,
  path    TEXT NOT NULL,
  seq     INTEGER NOT NULL,
  payload BLOB NOT NULL,
  PRIMARY KEY (repo, branch, path, seq)
);
CREATE TABLE IF NOT EXISTS collab_contributors (
  repo       TEXT NOT NULL,
  branch     TEXT NOT NULL,
  path       TEXT NOT NULL,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (repo, branch, path, user_id)
);

CREATE TABLE IF NOT EXISTS pr_approvals (
  pr_id      INTEGER NOT NULL REFERENCES prs(id),
  user_id    INTEGER NOT NULL REFERENCES users(id),
  commit_sha TEXT NOT NULL,             -- approval pinned to head commit
  created_at INTEGER NOT NULL,
  PRIMARY KEY (pr_id, user_id)
);
