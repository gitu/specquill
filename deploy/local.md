# Deploying SpecQuill locally (self-host)

One binary and one YAML file — the store is an embedded SQLite database and
the SPA is compiled in, so there is no service to run beside it. This is the
only supported deployment shape; for the hot-reload *dev* loop see the repo
README (`make dev`).

## 1. Get the binary

Either grab a release tarball (built on every `v*` tag —
`specquill_<version>_<os>_<arch>.tar.gz` plus `SHA256SUMS`), or build it:

```bash
make build          # SPA → embed → single binary at server/specquill
```

Or run the container image instead of a binary:

```bash
docker run -p 8643:8643 \
  -v $PWD/specquill.yml:/etc/specquill/specquill.yml:ro \
  -v specquill-data:/var/lib/specquill \
  ghcr.io/gitu/specquill:latest
```

The runtime needs **git ≥ 2.38** on the PATH (the image ships it).

## 2. Storage

Everything the server owns lives under `data_dir`:

| path | contents | rebuildable? |
|---|---|---|
| `<data_dir>/repos/` | bare clones + worktrees | yes — re-cloned from the remotes |
| `<data_dir>/specquill.db` | users, sessions, PRs, approvals, workspace claims, collab logs | **no** |

So `data_dir` needs **persistent storage** (a real directory or a docker
volume, as above), and it is what you back up. Document content itself lives
in git, on your remotes. The schema applies itself at boot — idempotent, no
migration step.

## 3. Config

`specquill.yml` — the same shape as the config baked into the container image
([specquill.docker.yml](specquill.docker.yml)). Secrets are referenced by
env-var *name*, never as values:

```yaml
listen: ":8643"
data_dir: /var/lib/specquill        # clones AND specquill.db — persist this
base_url: https://specs.example.com # exactly what browsers use (auth callbacks!)

# store: <data_dir>/specquill.db by default; set database.path to move it

projects:
  - id: specs
    remote: git@github.com:you/your-specs.git   # or https + token_env
    default_branch: main
    # token_env: SPECQUILL_TOKEN               # https remotes: push/fetch token

git:
  committer_name: specquill          # service identity → Co-authored-by trailer
  committer_email: specquill@example.com

auth:
  oidc:                              # any discovery-capable IdP
    enabled: true
    issuer: https://login.example.com
    client_id: specquill
    client_secret_env: SPECQUILL_OIDC_SECRET
    scopes: [openid, profile, email]
  local:
    enabled: false                   # or true for password accounts instead
  admin_emails: [you@example.com]    # who administers (projects/sources/grants)
  # default_role: member             # or viewer / none (grants-only access)

session:
  ttl: 12h
  cookie_secure: true                # requires https (see reverse proxy below)

# optional: copilot via any OpenAI-compatible endpoint (ollama works)
# ai:
#   enabled: true
#   base_url: http://localhost:11434/v1
#   model: qwen2.5:7b
#   quick_model: qwen2.5:7b
```

Notes:

- **`base_url` must match reality** — the OIDC callback is
  `<base_url>/auth/callback`, and cookies/redirects derive from it.
- **Back up `data_dir`** (or at least `specquill.db`): the clones rebuild
  themselves, the database does not.
- ssh remotes use the host's ssh agent/keys; https remotes take a token via
  `token_env`.
- `auth.local.enabled: true` + `specquill user add …` works fully offline —
  that is the developer-local shape, no IdP required.

## 4. Run it

```bash
export SPECQUILL_OIDC_SECRET='…'
./specquill -config specquill.yml
```

As a service, the usual systemd shape:

```ini
[Unit]
Description=SpecQuill
After=network-online.target

[Service]
ExecStart=/usr/local/bin/specquill -config /etc/specquill/specquill.yml
EnvironmentFile=/etc/specquill/env      # the SPECQUILL_* secrets, mode 0600
User=specquill
StateDirectory=specquill                # /var/lib/specquill
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## 5. Reverse proxy / TLS

Terminate TLS in front (caddy/nginx/traefik — or simply a Tailscale/HTTPS
tunnel) and forward to `listen`. **Websockets must be proxied** (collab
rooms ride the same port). Caddy example:

```
specs.example.com {
    reverse_proxy 127.0.0.1:8643
}
```

Run exactly **one instance**: the collab hub is in-process, the worktrees are
on local disk, and SQLite wants a single writer.

## 6. Day-2

- **Upgrades**: swap the binary/image and restart — schema changes are
  additive and run at boot; drain is instant (drafts autosave, rooms replay).
- **Backup**: `data_dir`. Point-in-time copies of a live SQLite file need
  `sqlite3 specquill.db ".backup /path/out.db"` (or a filesystem snapshot) —
  copying the file while the server is running can catch a torn WAL. Git
  content lives on your remotes.
- **Health**: `GET /api/repos` answering 200 with a session (or 401 without)
  means the server, store and clones are up.
