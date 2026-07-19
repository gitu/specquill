# Deploying SpecQuill locally (self-host)

One binary, one Postgres, one YAML file. The SPA is embedded — there is
nothing else to serve. For the managed pipeline (Cloud Run, staging/prod,
CI-driven rollouts) see [cloud.md](cloud.md); for the hot-reload *dev* loop
see the repo README (`make dev`).

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
  -e SPECQUILL_DATABASE_URL='postgres://…' \
  ghcr.io/gitu/specquill:latest
```

The runtime needs **git ≥ 2.38** on the PATH (the image ships it).

## 2. Postgres

Users, sessions, PRs, approvals, workspace claims and collab logs live in
Postgres; document content lives only in git. Any Postgres ≥ 14 works:

```bash
docker run -d --name specquill-pg -p 5432:5432 \
  -e POSTGRES_USER=specquill -e POSTGRES_PASSWORD=change-me -e POSTGRES_DB=specquill \
  postgres:16-alpine
```

The schema applies itself at boot (idempotent, additive migrations).

## 3. Config

`specquill.yml` — the local sibling of the cloud config
([specquill.cloud.yml](specquill.cloud.yml)). Secrets are referenced by env-var
*name*, never as values:

```yaml
listen: ":8643"
data_dir: /var/lib/specquill        # git clones/worktrees — a CACHE, rebuildable
base_url: https://specs.example.com # exactly what browsers use (auth callbacks!)

database:
  url_env: SPECQUILL_DATABASE_URL   # postgres://specquill:…@localhost:5432/specquill

projects:
  - id: specs
    remote: git@github.com:you/your-specs.git   # or https + token_env
    default_branch: main
    # token_env: SPECQUILL_TOKEN               # https remotes: push/fetch token
    # (with github_app: configured, github.com repos the app is installed on
    #  authenticate via installation tokens — token_env is only the fallback)

git:
  committer_name: specquill          # service identity → Co-authored-by trailer
  committer_email: specquill@example.com

auth:
  github:                            # sign in with GitHub (OAuth app)
    enabled: true
    client_id: Iv23li…
    client_secret_env: SPECQUILL_GH_CLIENT_SECRET
    allowed_users: [you]             # EMPTY LIST ADMITS ANY GITHUB ACCOUNT
  local:
    enabled: false                   # or true for password accounts instead

tenant:                              # the deployment's own tenant (/t/<slug>/…)
  slug: default
  admin_emails: [you@example.com]    # who administers (projects/sources/grants)

session:
  ttl: 12h
  cookie_secure: true                # requires https (see reverse proxy below)

# optional: instant sync on external pushes (else 2-minute polling)
# webhooks:
#   github: { enabled: true, secret_env: SPECQUILL_GH_WEBHOOK_SECRET }

# optional: copilot via any OpenAI-compatible endpoint (ollama works)
# ai:
#   enabled: true
#   base_url: http://localhost:11434/v1
#   model: qwen2.5:7b
#   quick_model: qwen2.5:7b
```

Notes:

- **`base_url` must match reality** — the GitHub OAuth callback is
  `<base_url>/auth/github/callback`, and cookies/redirects derive from it.
- **`data_dir` is disposable**: clones rebuild from the remotes, roomed
  co-editing drafts replay from Postgres. Back up Postgres, not `data_dir`.
- ssh remotes use the host's ssh agent/keys; https remotes take a token via
  `token_env`.
- `auth.local.enabled: true` + `specquill user …` provisioning works fully
  offline; GitHub login needs the OAuth app (register at
  github.com/settings/developers).

## 4. Run it

```bash
export SPECQUILL_DATABASE_URL='postgres://specquill:change-me@localhost:5432/specquill?sslmode=disable'
export SPECQUILL_GH_CLIENT_SECRET='…'
./specquill -config specquill.yml
```

As a service, the usual systemd shape:

```ini
[Unit]
Description=SpecQuill
After=network-online.target postgresql.service

[Service]
ExecStart=/usr/local/bin/specquill -config /etc/specquill/specquill.yml
EnvironmentFile=/etc/specquill/env      # the SPECQUILL_* secrets, mode 0600
# include SPECQUILL_SECRET_KEY (base64 of 32 random bytes, e.g.
# `head -c32 /dev/urandom | base64`) to enable the in-app credential store —
# tenant admins can then connect private repos with a PAT from the Admin UI
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

Run exactly **one instance**: the collab hub is in-process and worktrees are
local disk (same constraint as `--max-instances=1` in the cloud).

## 6. Day-2

- **Upgrades**: swap the binary/image and restart — schema migrations are
  additive and run at boot; drain is instant (drafts autosave, rooms replay).
- **Backup**: Postgres dump. Git content lives on your remotes.
- **Webhook** (optional, instant external sync): create a repository webhook
  on the specs repo → `<base_url>/hooks/github`, content type JSON, the
  `SPECQUILL_GH_WEBHOOK_SECRET` value as secret, push events.
- **Health**: `GET /api/repos` answering 200 with a session (or 401 without)
  means the server, store and clones are up.
