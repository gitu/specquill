# Deploying SpecQuill to Cloud Run

> **⚠️ This recipe needs a hosting decision before it works again.**
> The store is now an embedded SQLite file inside `data_dir`
> (2026-07-25), and **Cloud Run's filesystem is ephemeral** — every revision
> rollout would discard users, sessions, PRs, approvals and collab logs.
> Cloud Run offers no persistent block storage, and SQLite is not safe on
> its GCS-FUSE or NFS volume mounts (neither gives SQLite the file locking
> it needs). So pick one:
>
> - **host with a real disk** — a small GCE VM, Fly.io volume, Railway/Hetzner
>   container with a persistent volume — and follow [local.md](local.md),
>   which is the supported shape; or
> - **keep Cloud Run** and reintroduce a networked database (a Postgres/Neon
>   store is what this recipe used until 2026-07-25 — see git history).
>
> Everything below still describes the image-build and rollout machinery
> accurately; only the storage layer is unresolved.

Same pipeline as pert.li — **the image is built once, by GitHub, not by
Google Cloud.** The [`Docker` workflow](../.github/workflows/docker.yml) builds
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

- **Config is baked into the image.** [`deploy/specquill.cloud.yml`](specquill.cloud.yml)
  becomes `/etc/specquill/specquill.yml`. It holds no secrets — credentials are
  referenced by env-var *name* (`token_env`, `client_secret_env`,
  `api_key_env`) and mounted from Secret Manager by the deploy step. To change
  config: edit, commit, push (staging updates on the next main build).
  **Before the first deploy** fill in the real `base_url`, the writable repo
  `remote`, the OIDC issuer + `client_id`, and `admin_emails`.
- **The store is SQLite at `<data_dir>/specquill.db`.** Users, sessions, PRs,
  review comments, approvals, workspace-branch claims and the collab update
  logs all live in that one file. It is **not** rebuildable, so it must sit on
  storage that survives instance replacement — see the banner above.
- **`--max-instances=1` is still a hard requirement**, already set in
  `cloudbuild.yaml`: the collab hub (Yjs relay rooms + websockets) is
  in-process and the git worktrees are on local disk. Do not raise it.
- **`data_dir` must now be persistent.** It holds the bare clones and
  worktrees (rebuildable — re-cloned from the remote on boot) *and*
  `specquill.db` (not rebuildable). Committed content is always safe on the
  remote; what an ephemeral disk would additionally lose is every account,
  session and review artifact. `_MIN_INSTANCES=1` (the prod default here)
  keeps the instance warm, but that is a latency setting, not durability.
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

3. **Register the deployment with your IdP** (users sign in through the
   tenant's own OIDC provider):

   - **Redirect/callback URL: `<base_url>/auth/callback`**
   - The issuer and client id go into `deploy/specquill.cloud.yml`
     (`auth.oidc.issuer` / `client_id`), the client secret into Secret
     Manager below.

   Staging registers its own client (its `base_url` differs) and points the
   staging trigger's `_OIDC_SECRET_NAME` at that secret.

   Then **create the runtime secrets** (mounted as env vars on the service;
   the names must match the `_*_SECRET` substitutions / the env names in
   `deploy/specquill.cloud.yml`):

   ```bash
   # git push/fetch token for https remotes (ssh remotes use the agent instead)
   echo -n 'ghp_…git-push-fetch-token…' | gcloud secrets create SPECQUILL_TOKEN --data-file=-
   echo -n '…oidc-client-secret…'       | gcloud secrets create SPECQUILL_OIDC_SECRET --data-file=-
   echo -n 'AIza…copilot-api-key…'      | gcloud secrets create SPECQUILL_AI_KEY --data-file=-
   ```

   **Staging gets its own set** — at minimum its own storage and its own specs
   repo, so it never touches prod data. Point the staging trigger's
   `_TOKEN_SECRET`/… at the staging entries; omitted overrides fall back to
   the prod defaults in `cloudbuild.yaml`.

   > The store secret that used to live here (`SPECQUILL_DATABASE_URL`, a Neon
   > DSN) is gone with the Postgres removal — storage is now a file on the
   > volume, which is exactly the open question in the banner at the top.

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
     --substitutions=_SERVICE=specquill-staging,_VERSION_GATE=off,_MIN_INSTANCES=0,_GHCR_IMAGE=gitu/specquill,_TOKEN_SECRET=SPECQUILL_TOKEN_STAGING

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

## Authentication & tenant configuration

**Who can log in.** Whoever the configured OIDC issuer lets in — one
deployment serves one tenant, so the tenant's own IdP is the gate. Set
`auth.default_role: none` to admit users but grant repository access
explicitly (per-repo grants, REQ-020).

**Who administers.** Everyone who logs in is auto-enrolled with
`auth.default_role` (**member** by default: edit, commit, PRs).
`auth.admin_emails` promotes matching users (any provider, matched on email)
to **admin** on login — admins manage projects, sources and grants via the
Admin view / management API. Set at least your own email before the first
deploy, or the instance has no administrator.

**The workspace itself** comes from the YAML `projects:` / `sources:` lists,
which sync into the registry at boot (config-managed rows); admins can add
more at runtime through the management API (api-managed rows persist across
boots). Roles are deployment-wide — `viewer < member < admin` on the user
row — with per-repo grants layered on top.

## Local smoke test of the production image

```bash
docker build -t specquill:local .
docker run --rm -p 8080:8080 \
  -e SPECQUILL_TOKEN='ghp_…' \
  -e SPECQUILL_OIDC_SECRET='…' \
  -e SPECQUILL_AI_KEY='…' \
  -v specquill-data:/var/lib/specquill \
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
