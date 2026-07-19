-- specquill review/auth metadata (Postgres). Content lives in git; this DB
-- holds only users, sessions, PR review state, and the collab update log.

CREATE TABLE IF NOT EXISTS users (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  provider   TEXT NOT NULL,             -- 'oidc' | 'local'
  subject    TEXT NOT NULL,             -- OIDC sub / local username
  name       TEXT NOT NULL,
  email      TEXT NOT NULL,
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

-- tenancy (docs/multi-tenancy.md): tenant = GitHub App installation, or the
-- built-in 'default' tenant mirroring the YAML repos list (self-hosting).
-- The canonical repo key in all other tables is '<tenant_slug>/<repo_id>'.
CREATE TABLE IF NOT EXISTS tenants (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug            TEXT UNIQUE NOT NULL,
  provider        TEXT NOT NULL,          -- 'config' | 'github'
  installation_id BIGINT,                 -- GitHub App installation (NULL for config)
  display_name    TEXT NOT NULL DEFAULT '',
  created_at      BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS tenant_repos (
  tenant_id      BIGINT NOT NULL REFERENCES tenants(id),
  repo_id        TEXT NOT NULL,           -- short id, unique within the tenant
  mode           TEXT NOT NULL,           -- writable | readonly
  remote         TEXT NOT NULL,
  default_branch TEXT NOT NULL DEFAULT 'main',
  gh_full_name   TEXT NOT NULL DEFAULT '',
  managed_by     TEXT NOT NULL DEFAULT 'config',  -- config rows reconcile at boot
  created_at     BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, repo_id)
);
ALTER TABLE tenant_repos ADD COLUMN IF NOT EXISTS managed_by TEXT NOT NULL DEFAULT 'config';

CREATE TABLE IF NOT EXISTS tenant_members (
  tenant_id BIGINT NOT NULL REFERENCES tenants(id),
  user_id   BIGINT NOT NULL REFERENCES users(id),
  role      TEXT NOT NULL DEFAULT 'member',   -- admin | member | viewer
  synced_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, user_id)
);

CREATE TABLE IF NOT EXISTS prs (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  repo          TEXT NOT NULL,
  number        INTEGER NOT NULL,
  title         TEXT NOT NULL,
  body          TEXT,
  source_branch TEXT NOT NULL,
  target_branch TEXT NOT NULL,
  author_id     BIGINT NOT NULL REFERENCES users(id),
  state         TEXT NOT NULL DEFAULT 'open',   -- open|merged|closed
  merged_commit TEXT,
  created_at    BIGINT NOT NULL,
  merged_at     BIGINT,
  UNIQUE(repo, number)
);

CREATE TABLE IF NOT EXISTS pr_comments (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  pr_id           BIGINT NOT NULL REFERENCES prs(id),
  author_id       BIGINT NOT NULL REFERENCES users(id),
  file_path       TEXT,                 -- NULL = general comment
  line            INTEGER,
  anchored_commit TEXT,
  body            TEXT NOT NULL,
  resolved        BOOLEAN NOT NULL DEFAULT FALSE,
  created_at      BIGINT NOT NULL
);

-- personal workspace branch ownership (ws/<slug> claimed per user)
CREATE TABLE IF NOT EXISTS workspace_branches (
  repo    TEXT NOT NULL,
  user_id BIGINT NOT NULL REFERENCES users(id),
  branch  TEXT NOT NULL,
  PRIMARY KEY (repo, user_id),
  UNIQUE (repo, branch)
);

-- real-time co-editing: room state, opaque Yjs update log, contributor sets
CREATE TABLE IF NOT EXISTS collab_rooms (
  repo        TEXT NOT NULL,
  branch      TEXT NOT NULL,
  path        TEXT NOT NULL,
  last_seq    BIGINT NOT NULL DEFAULT 0,
  seed_seq    BIGINT NOT NULL DEFAULT 0,
  flushed_seq BIGINT NOT NULL DEFAULT 0,
  flushed_sha TEXT NOT NULL DEFAULT '',
  updated_at  BIGINT NOT NULL,
  PRIMARY KEY (repo, branch, path)
);
CREATE TABLE IF NOT EXISTS collab_updates (
  repo    TEXT NOT NULL,
  branch  TEXT NOT NULL,
  path    TEXT NOT NULL,
  seq     BIGINT NOT NULL,
  payload BYTEA NOT NULL,
  PRIMARY KEY (repo, branch, path, seq)
);
CREATE TABLE IF NOT EXISTS collab_contributors (
  repo       TEXT NOT NULL,
  branch     TEXT NOT NULL,
  path       TEXT NOT NULL,
  user_id    BIGINT NOT NULL REFERENCES users(id),
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (repo, branch, path, user_id)
);

CREATE TABLE IF NOT EXISTS pr_approvals (
  pr_id      BIGINT NOT NULL REFERENCES prs(id),
  user_id    BIGINT NOT NULL REFERENCES users(id),
  commit_sha TEXT NOT NULL,             -- approval pinned to head commit
  created_at BIGINT NOT NULL,
  PRIMARY KEY (pr_id, user_id)
);

-- projects & sources (config-split plan): a project is a writable workspace
-- (repo + content_root); a source is a catalog entry projects may reference.
-- managed_by: 'config' rows reconcile to the YAML at boot, 'api' rows persist.
CREATE TABLE IF NOT EXISTS tenant_projects (
  tenant_id    BIGINT NOT NULL REFERENCES tenants(id),
  project_id   TEXT NOT NULL,
  repo_id      TEXT NOT NULL,           -- tenant_repos.repo_id (same tenant)
  content_root TEXT NOT NULL DEFAULT '',
  managed_by   TEXT NOT NULL DEFAULT 'config',   -- config | api
  created_at   BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, project_id)
);

CREATE TABLE IF NOT EXISTS sources (
  id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id      BIGINT REFERENCES tenants(id),  -- NULL = global (app YAML / platform)
  name           TEXT NOT NULL,
  kind           TEXT NOT NULL,                  -- git | url | openapi | confluence
  remote         TEXT NOT NULL,
  token_env      TEXT NOT NULL DEFAULT '',       -- env var NAME; never a secret value
  credential_ref TEXT NOT NULL DEFAULT '',       -- hosted future (Secret Manager path)
  default_branch TEXT NOT NULL DEFAULT 'main',
  sync_interval  BIGINT NOT NULL DEFAULT 300,    -- seconds
  managed_by     TEXT NOT NULL DEFAULT 'config',
  created_at     BIGINT NOT NULL,
  UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS source_grants (
  tenant_id  BIGINT NOT NULL REFERENCES tenants(id),
  source_id  BIGINT NOT NULL REFERENCES sources(id),
  granted_by BIGINT REFERENCES users(id),        -- NULL = boot sync
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, source_id)
);

-- last-import status per non-git (importer) source, keyed by tenant + source
-- name. Populated by importer.Runner; surfaced in the sources list + sync API.
CREATE TABLE IF NOT EXISTS source_syncs (
  tenant_id  BIGINT NOT NULL REFERENCES tenants(id),
  name       TEXT NOT NULL,
  status     TEXT NOT NULL,                       -- ok | error
  error      TEXT NOT NULL DEFAULT '',
  file_count INT NOT NULL DEFAULT 0,
  head_sha   TEXT NOT NULL DEFAULT '',
  synced_at  BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, name)
);

-- the GitHub @handle behind a github-provider user — what permission
-- lookups and allow-lists match on (subjects are the immutable numeric id)
ALTER TABLE users ADD COLUMN IF NOT EXISTS login TEXT NOT NULL DEFAULT '';

-- unauthenticated OKF-bundle share links: the URL token is the only
-- credential (LLM copy-paste use case). One active link per project;
-- minting again rotates the token, deleting revokes access.
CREATE TABLE IF NOT EXISTS share_links (
  tenant_id  BIGINT NOT NULL REFERENCES tenants(id),
  project_id TEXT NOT NULL,
  token      TEXT NOT NULL UNIQUE,
  created_by BIGINT NOT NULL REFERENCES users(id),
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, project_id)
);

-- per-repo user grants (REQ-020): explicit access layered on derived roles;
-- effective role = max(derived, granted). Role sync never touches these —
-- that is the point: a GitHub revocation must not drop an explicit grant.
CREATE TABLE IF NOT EXISTS repo_grants (
  tenant_id  BIGINT NOT NULL REFERENCES tenants(id),
  repo_id    TEXT   NOT NULL,
  user_id    BIGINT NOT NULL REFERENCES users(id),
  role       TEXT   NOT NULL DEFAULT 'viewer',   -- viewer | member (repo/project management is tenant-scoped)
  granted_by BIGINT REFERENCES users(id),
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, repo_id, user_id),
  FOREIGN KEY (tenant_id, repo_id) REFERENCES tenant_repos(tenant_id, repo_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS repo_grants_user ON repo_grants(user_id);

-- pending grants for users who have not logged in yet; the matcher is a
-- lowercased email or GitHub login, claimed (converted to repo_grants rows)
-- and deleted on the invitee's first login.
CREATE TABLE IF NOT EXISTS repo_grant_invites (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  BIGINT NOT NULL REFERENCES tenants(id),
  repo_id    TEXT   NOT NULL,
  kind       TEXT   NOT NULL,                    -- 'email' | 'github'
  matcher    TEXT   NOT NULL,                    -- lowercased
  role       TEXT   NOT NULL DEFAULT 'viewer',
  granted_by BIGINT NOT NULL REFERENCES users(id),
  created_at BIGINT NOT NULL,
  UNIQUE (tenant_id, repo_id, kind, matcher),
  FOREIGN KEY (tenant_id, repo_id) REFERENCES tenant_repos(tenant_id, repo_id) ON DELETE CASCADE
);

-- per-tenant credentials encrypted at rest (secrets.Sealer). The secret VALUE
-- is never stored in plaintext and never returned by any read API; only the
-- ciphertext (nonce||AES-GCM) and non-secret metadata live here. AAD binds the
-- ciphertext to (tenant_id, kind, ref) so a row cannot be decrypted in another
-- tenant's context. Deleting a tenant cascades its credentials away.
CREATE TABLE IF NOT EXISTS tenant_credentials (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id    BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind         TEXT   NOT NULL,                 -- git_pat | git_basic | importer_token | oidc_secret
  ref          TEXT   NOT NULL DEFAULT '',      -- slot name (e.g. repo_id / source name); '' = tenant default
  username     TEXT   NOT NULL DEFAULT '',      -- non-secret (git_basic user; else x-access-token)
  secret_blob  BYTEA  NOT NULL,                 -- nonce || AES-256-GCM ciphertext
  key_version  INT    NOT NULL DEFAULT 1,
  created_by   BIGINT REFERENCES users(id),
  created_at   BIGINT NOT NULL,
  rotated_at   BIGINT NOT NULL DEFAULT 0,
  last_used_at BIGINT NOT NULL DEFAULT 0,
  UNIQUE (tenant_id, kind, ref)
);
CREATE INDEX IF NOT EXISTS tenant_credentials_lookup ON tenant_credentials (tenant_id, kind, ref);
