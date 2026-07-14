# Deploying specquill to Cloud Run

Same pipeline as pert.li — **the image is built once, by GitHub, not by
Google Cloud.** The [`Docker` workflow](.github/workflows/docker.yml) builds
the multi-stage `Dockerfile` (SPA → embedded Go binary → alpine+git) and
pushes it to **ghcr.io** on every push to `main` and every tag. It then hands
off over Workload Identity Federation: it runs a **deploy-only** Cloud Build
trigger (`cloudbuild.yaml`) pinned to the commit, which pulls *that same
image* through an Artifact Registry **remote-repo proxy** of ghcr.io — no
rebuild on the Google side — applies the promotion gate, and rolls out to
Cloud Run.

```
push main / tag ─► GitHub Actions: build + push ghcr.io
                          │
                          └─ gcloud builds triggers run  (WIF auth, --sha=<commit>)
                                      │
                          Cloud Build (deploy-only): pull via AR proxy of ghcr.io
                          ──► version-gate ──► gcloud run deploy
```

| Environment | GitHub event | `_SERVICE` | Rolls out when |
| --- | --- | --- | --- |
| **Staging** | push to `main` | `specquill-staging` | always (`_VERSION_GATE=off`) |
| **Production** | push of a `v*` tag | `specquill` | only if the tag is reachable from `main` **and** is the newest `v*` version (`_VERSION_GATE=on`) |

Release: merge to `main`, verify on staging, then
`git tag vX.Y.Z <main-commit> && git push origin vX.Y.Z`. Prod never moves
backwards (older / side-branch / re-pushed tags skip the rollout).

The GitHub `deploy` job is guarded by the `CLOUD_BUILD_REGION` repo variable —
until step 7 below sets it, pushes only build + push the image and skip the
deploy cleanly.

## specquill-specific constraints (read first)

- **Config is baked into the image.** [`deploy/specquill.cloud.yml`](deploy/specquill.cloud.yml)
  becomes `/etc/specquill/specquill.yml`. It holds no secrets — credentials are
  referenced by env-var *name* (`token_env`, `client_secret_env`,
  `api_key_env`) and mounted from Secret Manager by the deploy step. To change
  config: edit, commit, push (staging updates on the next main build).
  **Before the first deploy** fill in the real `base_url`, the writable repo
  `remote`, the GitHub OAuth `client_id` + `allowed_users`, and
  `admin_emails`.
- **The store is Postgres — use Neon in production.** Users, sessions, PRs,
  review comments, approvals, workspace-branch claims and the collab update
  logs all live in the database referenced by the `SPECQUILL_DATABASE_URL`
  secret (a Neon connection string, `sslmode=require`). All of that survives
  instance replacement.
- **`--max-instances=1` is still a hard requirement**, already set in
  `cloudbuild.yaml`: the collab hub (Yjs relay rooms + websockets) is
  in-process and the git worktrees are on local disk. Do not raise it.
- **`data_dir` is ephemeral on Cloud Run — and that's now OK.** It holds only
  the bare clones and worktrees, re-cloned from the real remote on boot.
  Committed content is on the remote; roomed (co-editing) drafts replay from
  the collab log in Postgres. The only thing an instance replacement can drop
  is a plain uncommitted worktree draft (last autosave since the previous
  commit) on a branch with no live room. `_MIN_INSTANCES=1` (the prod default
  here) keeps the instance warm so that only happens on revision rollouts.
- **ghcr package visibility**: with a public package the AR remote proxy
  needs no upstream credentials (skip step 2b). If the package is private,
  step 2b is mandatory.

## One-time setup

1. **Enable APIs**:

   ```bash
   gcloud services enable \
     run.googleapis.com \
     cloudbuild.googleapis.com \
     artifactregistry.googleapis.com \
     secretmanager.googleapis.com \
     iamcredentials.googleapis.com \
     sts.googleapis.com
   ```

2. **Create the Artifact Registry remote repo** proxying ghcr.io:

   ```bash
   gcloud artifacts repositories create ghcr-remote \
     --repository-format=docker \
     --mode=remote-repository \
     --remote-docker-repo=https://ghcr.io \
     --location=europe-west1
   ```

   2b. **Attach upstream credentials** (required — private package). Create a
   GitHub PAT with `read:packages`, then:

   ```bash
   PROJECT_ID=$(gcloud config get-value project)
   PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')

   echo -n '<github-pat-with-read:packages>' | \
     gcloud secrets create ghcr-pull-token --data-file=-
   gcloud secrets add-iam-policy-binding ghcr-pull-token \
     --member="serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-artifactregistry.iam.gserviceaccount.com" \
     --role="roles/secretmanager.secretAccessor"
   gcloud artifacts repositories update ghcr-remote --location=europe-west1 \
     --remote-username=gitu \
     --remote-password-secret-version=projects/${PROJECT_ID}/secrets/ghcr-pull-token/versions/latest
   ```

3. **Register the GitHub OAuth app** (app sign-in — users log in with their
   GitHub account). Under *GitHub → Settings → Developer settings → OAuth
   Apps → New OAuth App* (register it on the org if the workspace belongs to
   one):

   - Homepage URL: your `base_url` (the Cloud Run URL until a domain is mapped)
   - **Authorization callback URL: `<base_url>/auth/github/callback`**
   - Generate a client secret; the client id goes into
     `deploy/specquill.cloud.yml` (`auth.github.client_id`), the secret into
     Secret Manager below.

   Staging needs its own OAuth app (GitHub allows one callback URL per app) —
   or add the staging URL to a second app and point the staging trigger's
   `_GH_SECRET_NAME` at its secret.

   Then **create the runtime secrets** (mounted as env vars on the service;
   the names must match the `_*_SECRET` substitutions / the env names in
   `deploy/specquill.cloud.yml`):

   ```bash
   echo -n 'ghp_…git-push-fetch-token…'   | gcloud secrets create SPECQUILL_TOKEN --data-file=-
   echo -n '…github-oauth-client-secret…' | gcloud secrets create SPECQUILL_GH_CLIENT_SECRET --data-file=-
   echo -n 'AIza…copilot-api-key…'        | gcloud secrets create SPECQUILL_AI_KEY --data-file=-
   # push-webhook HMAC secret (used again when registering the webhook below)
   WEBHOOK_SECRET=$(openssl rand -hex 32)
   echo -n "$WEBHOOK_SECRET" | gcloud secrets create SPECQUILL_GH_WEBHOOK_SECRET --data-file=-
   # Neon: project → connection string (pooled is fine; keep sslmode=require)
   echo -n 'postgres://…@…neon.tech/specquill?sslmode=require' | \
     gcloud secrets create SPECQUILL_DATABASE_URL --data-file=-
   ```

   **Staging gets its own set** — at minimum a distinct database so staging
   never touches prod data (a [Neon branch](https://neon.com/docs/introduction/branching)
   of the prod database is the cheap way to get one):

   ```bash
   echo -n 'postgres://…staging-branch…?sslmode=require' | \
     gcloud secrets create SPECQUILL_DATABASE_URL_STAGING --data-file=-
   ```

   Point the staging trigger's `_DATABASE_URL_SECRET` (and `_TOKEN_SECRET`/…
   if staging writes a different specs repo) at the staging entries — omitted
   overrides fall back to the prod defaults in `cloudbuild.yaml`.

4. **Create the deploy service account** (Cloud Build triggers here must run
   as an explicit SA):

   ```bash
   gcloud iam service-accounts create specquill-deployer \
     --display-name="specquill Cloud Build deployer"
   DEPLOYER="specquill-deployer@${PROJECT_ID}.iam.gserviceaccount.com"

   for role in run.admin iam.serviceAccountUser secretmanager.secretAccessor artifactregistry.reader logging.logWriter; do
     gcloud projects add-iam-policy-binding "$PROJECT_ID" \
       --member="serviceAccount:${DEPLOYER}" --role="roles/${role}"
   done

   # the Cloud Run runtime service agent pulls the image on cold start
   gcloud projects add-iam-policy-binding "$PROJECT_ID" \
     --member="serviceAccount:service-${PROJECT_NUMBER}@serverless-robot-prod.iam.gserviceaccount.com" \
     --role="roles/artifactregistry.reader"
   ```

5. **Workload Identity Federation** so the GitHub workflow can run the
   trigger without a long-lived key (reuse the pool/provider from pert.li if
   deploying into the same project — then only add the attribute-condition for
   this repo and the SA binding):

   ```bash
   gcloud iam service-accounts create gh-deploy-specquill --display-name="GitHub Actions deploy (specquill)"
   DEPLOY_SA="gh-deploy-specquill@${PROJECT_ID}.iam.gserviceaccount.com"

   gcloud projects add-iam-policy-binding "$PROJECT_ID" \
     --member="serviceAccount:${DEPLOY_SA}" --role="roles/cloudbuild.builds.editor"
   gcloud iam service-accounts add-iam-policy-binding "$DEPLOYER" \
     --member="serviceAccount:${DEPLOY_SA}" --role="roles/iam.serviceAccountUser"

   gcloud iam workload-identity-pools create github --location=global --display-name="GitHub" || true
   gcloud iam workload-identity-pools providers create-oidc github-specquill \
     --location=global --workload-identity-pool=github --display-name="GitHub OIDC (specquill)" \
     --issuer-uri="https://token.actions.githubusercontent.com" \
     --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
     --attribute-condition="assertion.repository=='gitu/specquill' && (assertion.ref=='refs/heads/main' || assertion.ref.startsWith('refs/tags/'))"

   POOL_ID=$(gcloud iam workload-identity-pools describe github --location=global --format='value(name)')
   gcloud iam service-accounts add-iam-policy-binding "$DEPLOY_SA" \
     --role="roles/iam.workloadIdentityUser" \
     --member="principalSet://iam.googleapis.com/${POOL_ID}/attribute.repository/gitu/specquill"

   # provider resource name → the GCP_WORKLOAD_IDENTITY_PROVIDER secret (step 7)
   gcloud iam workload-identity-pools providers describe github-specquill \
     --location=global --workload-identity-pool=github --format='value(name)'
   ```

6. **Create the two manual deploy triggers** (2nd-gen connection shown;
   connect the repo first under Cloud Build → Repositories):

   ```bash
   REPO=projects/${PROJECT_ID}/locations/europe-west1/connections/<conn>/repositories/specquill
   DEPLOYER_RES=projects/${PROJECT_ID}/serviceAccounts/${DEPLOYER}

   # staging — run by GitHub on push to main (own database, scale to zero)
   gcloud builds triggers create manual \
     --name=specquill-deploy-staging --region=europe-west1 \
     --repository="$REPO" --branch=main --build-config=cloudbuild.yaml \
     --service-account="$DEPLOYER_RES" \
     --substitutions=_SERVICE=specquill-staging,_VERSION_GATE=off,_MIN_INSTANCES=0,_GHCR_IMAGE=gitu/specquill,_DATABASE_URL_SECRET=SPECQUILL_DATABASE_URL_STAGING

   # prod — run by GitHub on a v* tag (cloudbuild.yaml defaults are prod)
   gcloud builds triggers create manual \
     --name=specquill-deploy-prod --region=europe-west1 \
     --repository="$REPO" --branch=main --build-config=cloudbuild.yaml \
     --service-account="$DEPLOYER_RES" \
     --substitutions=_VERSION_GATE=on,_GHCR_IMAGE=gitu/specquill
   ```

7. **Wire the GitHub repo** (this arms the deploy job — set it last):

   ```bash
   gh secret   set GCP_WORKLOAD_IDENTITY_PROVIDER --body "<provider resource name from step 5>"
   gh secret   set GCP_DEPLOY_SERVICE_ACCOUNT     --body "gh-deploy-specquill@${PROJECT_ID}.iam.gserviceaccount.com"
   gh variable set CLOUD_BUILD_STAGING_TRIGGER    --body "specquill-deploy-staging"
   gh variable set CLOUD_BUILD_PROD_TRIGGER       --body "specquill-deploy-prod"
   gh variable set CLOUD_BUILD_REGION             --body "europe-west1"
   ```

   The first staging deploy prints the service URL; map your domain via Cloud
   Run domain mappings and set `base_url` in `deploy/specquill.cloud.yml`
   accordingly (OIDC redirect URLs + cookies depend on it).

## Push webhooks (instant sync)

Without a webhook the server polls: the writable project repo is fetched
every 2 minutes (`sync_interval`, per project), read-only sources on their
own interval — external pushes show up within that window. With
`webhooks.github` enabled (the cloud config default), a **repository
webhook on the specs repo** makes them land immediately:

```bash
gh api repos/OWNER/SPECS-REPO/hooks -f name=web -F active=true \
  -f 'events[]=push' \
  -f config.url="<base_url>/hooks/github" \
  -f config.content_type=json \
  -f config.secret="$WEBHOOK_SECRET"     # the SPECQUILL_GH_WEBHOOK_SECRET value
```

Register after the first deploy (the URL must exist). The endpoint is
sessionless — the HMAC-SHA256 signature is the authentication; a push to a
registered repo's remote triggers a targeted fetch and, for the default
branch, a fast-forward of the served state. Add the same webhook to any
read-only git source repos you want instant too. GitHub's *Recent
Deliveries* tab on the webhook shows the responses
(`{"ok":true,"matched":1}` on success); the polling interval remains the
backstop if a delivery is ever missed.

## Authentication & tenant configuration

**Who can log in.** `auth.github.allowed_users` is the gate: only the listed
GitHub handles may sign in (case-insensitive). An empty list admits **any
GitHub account** — never ship that on a public URL. Denied users land on the
login page with an explanatory error.

**Who administers.** Everyone who logs in is auto-enrolled into the built-in
`default` tenant as a **member** (edit, commit, PRs). `auth.admin_emails`
promotes matching users (any provider, matched on email) to **admin** on
login — admins manage projects, sources and grants via the Admin view /
management API. Set at least your own email before the first deploy, or the
instance has no administrator.

**The tenant itself** is implicit in self-host mode: the YAML `projects:` /
`sources:` / `grants:` lists sync into the single `default` tenant at boot
(config-managed rows), and admins can add more at runtime through the
management API (api-managed rows persist across boots). Roles are per-tenant:
`viewer < member < admin`; change them with
`store.SetMemberRole` semantics via the admin API. True multi-tenancy (one
tenant per GitHub App installation, roles derived from GitHub repo
permissions) is designed in [docs/multi-tenancy.md](docs/multi-tenancy.md)
and blocked only on a GitHub App registration — the OAuth login shipped here
is forward-compatible with it (`users.provider='github'`, subject = GitHub
user id).

## Local smoke test of the production image

```bash
docker build -t specquill:local .
docker run --rm -p 8080:8080 \
  -e SPECQUILL_TOKEN='ghp_…' \
  -e SPECQUILL_OIDC_SECRET='…' \
  -e SPECQUILL_AI_KEY='…' \
  -e SPECQUILL_DATABASE_URL='postgres://…?sslmode=require' \
  specquill:local
```

(With placeholder config values the server will fail on the unreachable
remote/issuer — override the config with
`-v $PWD/specquill.yml:/etc/specquill/specquill.yml:ro` to test against real ones.)

## Operational notes

- **Rollback**: `gcloud run services update-traffic <service> --region=europe-west1 --to-revisions=<prev>=100`.
- **Deployed version**: recorded as the `APP_VERSION` env var on the service
  (`gcloud run services describe specquill --region=europe-west1`).
- **Websockets** (collab rooms) ride the same HTTP port; `--timeout=3600` and
  `--no-cpu-throttling` are set so background flush/heartbeat work keeps
  running while rooms are open.
