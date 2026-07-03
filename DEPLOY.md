# Deploying reqbase to Cloud Run

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
| **Staging** | push to `main` | `reqbase-staging` | always (`_VERSION_GATE=off`) |
| **Production** | push of a `v*` tag | `reqbase` | only if the tag is reachable from `main` **and** is the newest `v*` version (`_VERSION_GATE=on`) |

Release: merge to `main`, verify on staging, then
`git tag vX.Y.Z <main-commit> && git push origin vX.Y.Z`. Prod never moves
backwards (older / side-branch / re-pushed tags skip the rollout).

The GitHub `deploy` job is guarded by the `CLOUD_BUILD_REGION` repo variable —
until step 7 below sets it, pushes only build + push the image and skip the
deploy cleanly.

## reqbase-specific constraints (read first)

- **Config is baked into the image.** [`deploy/reqbase.cloud.yml`](deploy/reqbase.cloud.yml)
  becomes `/etc/reqbase/reqbase.yml`. It holds no secrets — credentials are
  referenced by env-var *name* (`token_env`, `client_secret_env`,
  `api_key_env`) and mounted from Secret Manager by the deploy step. To change
  config: edit, commit, push (staging updates on the next main build).
  **Before the first deploy** fill in the real `base_url`, the writable repo
  `remote`, and the OIDC issuer.
- **The store is Postgres — use Neon in production.** Users, sessions, PRs,
  review comments, approvals, workspace-branch claims and the collab update
  logs all live in the database referenced by the `REQBASE_DATABASE_URL`
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
- **This repo is private → the ghcr package is private**, so the AR remote
  proxy **needs upstream credentials** (step 2b is not optional, unlike a
  public repo).

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

3. **Create the runtime secrets** (mounted as env vars on the service; the
   names must match the `_*_SECRET` substitutions / the env names in
   `deploy/reqbase.cloud.yml`):

   ```bash
   echo -n 'ghp_…git-push-fetch-token…'   | gcloud secrets create REQBASE_TOKEN --data-file=-
   echo -n '…oidc-client-secret…'         | gcloud secrets create REQBASE_OIDC_SECRET --data-file=-
   echo -n 'AIza…copilot-api-key…'        | gcloud secrets create REQBASE_AI_KEY --data-file=-
   # Neon: project → connection string (pooled is fine; keep sslmode=require)
   echo -n 'postgres://…@…neon.tech/reqbase?sslmode=require' | \
     gcloud secrets create REQBASE_DATABASE_URL --data-file=-
   ```

   **Staging gets its own set** — at minimum a distinct database so staging
   never touches prod data (a [Neon branch](https://neon.com/docs/introduction/branching)
   of the prod database is the cheap way to get one):

   ```bash
   echo -n 'postgres://…staging-branch…?sslmode=require' | \
     gcloud secrets create REQBASE_DATABASE_URL_STAGING --data-file=-
   ```

   Point the staging trigger's `_DATABASE_URL_SECRET` (and `_TOKEN_SECRET`/…
   if staging writes a different specs repo) at the staging entries — omitted
   overrides fall back to the prod defaults in `cloudbuild.yaml`.

4. **Create the deploy service account** (Cloud Build triggers here must run
   as an explicit SA):

   ```bash
   gcloud iam service-accounts create reqbase-deployer \
     --display-name="reqbase Cloud Build deployer"
   DEPLOYER="reqbase-deployer@${PROJECT_ID}.iam.gserviceaccount.com"

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
   gcloud iam service-accounts create gh-deploy-reqbase --display-name="GitHub Actions deploy (reqbase)"
   DEPLOY_SA="gh-deploy-reqbase@${PROJECT_ID}.iam.gserviceaccount.com"

   gcloud projects add-iam-policy-binding "$PROJECT_ID" \
     --member="serviceAccount:${DEPLOY_SA}" --role="roles/cloudbuild.builds.editor"
   gcloud iam service-accounts add-iam-policy-binding "$DEPLOYER" \
     --member="serviceAccount:${DEPLOY_SA}" --role="roles/iam.serviceAccountUser"

   gcloud iam workload-identity-pools create github --location=global --display-name="GitHub" || true
   gcloud iam workload-identity-pools providers create-oidc github-reqbase \
     --location=global --workload-identity-pool=github --display-name="GitHub OIDC (reqbase)" \
     --issuer-uri="https://token.actions.githubusercontent.com" \
     --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
     --attribute-condition="assertion.repository=='gitu/reqbase' && (assertion.ref=='refs/heads/main' || assertion.ref.startsWith('refs/tags/'))"

   POOL_ID=$(gcloud iam workload-identity-pools describe github --location=global --format='value(name)')
   gcloud iam service-accounts add-iam-policy-binding "$DEPLOY_SA" \
     --role="roles/iam.workloadIdentityUser" \
     --member="principalSet://iam.googleapis.com/${POOL_ID}/attribute.repository/gitu/reqbase"

   # provider resource name → the GCP_WORKLOAD_IDENTITY_PROVIDER secret (step 7)
   gcloud iam workload-identity-pools providers describe github-reqbase \
     --location=global --workload-identity-pool=github --format='value(name)'
   ```

6. **Create the two manual deploy triggers** (2nd-gen connection shown;
   connect the repo first under Cloud Build → Repositories):

   ```bash
   REPO=projects/${PROJECT_ID}/locations/europe-west1/connections/<conn>/repositories/reqbase
   DEPLOYER_RES=projects/${PROJECT_ID}/serviceAccounts/${DEPLOYER}

   # staging — run by GitHub on push to main (own database, scale to zero)
   gcloud builds triggers create manual \
     --name=reqbase-deploy-staging --region=europe-west1 \
     --repository="$REPO" --branch=main --build-config=cloudbuild.yaml \
     --service-account="$DEPLOYER_RES" \
     --substitutions=_SERVICE=reqbase-staging,_VERSION_GATE=off,_MIN_INSTANCES=0,_GHCR_IMAGE=gitu/reqbase,_DATABASE_URL_SECRET=REQBASE_DATABASE_URL_STAGING

   # prod — run by GitHub on a v* tag (cloudbuild.yaml defaults are prod)
   gcloud builds triggers create manual \
     --name=reqbase-deploy-prod --region=europe-west1 \
     --repository="$REPO" --branch=main --build-config=cloudbuild.yaml \
     --service-account="$DEPLOYER_RES" \
     --substitutions=_VERSION_GATE=on,_GHCR_IMAGE=gitu/reqbase
   ```

7. **Wire the GitHub repo** (this arms the deploy job — set it last):

   ```bash
   gh secret   set GCP_WORKLOAD_IDENTITY_PROVIDER --body "<provider resource name from step 5>"
   gh secret   set GCP_DEPLOY_SERVICE_ACCOUNT     --body "gh-deploy-reqbase@${PROJECT_ID}.iam.gserviceaccount.com"
   gh variable set CLOUD_BUILD_STAGING_TRIGGER    --body "reqbase-deploy-staging"
   gh variable set CLOUD_BUILD_PROD_TRIGGER       --body "reqbase-deploy-prod"
   gh variable set CLOUD_BUILD_REGION             --body "europe-west1"
   ```

   The first staging deploy prints the service URL; map your domain via Cloud
   Run domain mappings and set `base_url` in `deploy/reqbase.cloud.yml`
   accordingly (OIDC redirect URLs + cookies depend on it).

## Local smoke test of the production image

```bash
docker build -t reqbase:local .
docker run --rm -p 8080:8080 \
  -e REQBASE_TOKEN='ghp_…' \
  -e REQBASE_OIDC_SECRET='…' \
  -e REQBASE_AI_KEY='…' \
  -e REQBASE_DATABASE_URL='postgres://…?sslmode=require' \
  reqbase:local
```

(With placeholder config values the server will fail on the unreachable
remote/issuer — override the config with
`-v $PWD/reqbase.yml:/etc/reqbase/reqbase.yml:ro` to test against real ones.)

## Operational notes

- **Rollback**: `gcloud run services update-traffic <service> --region=europe-west1 --to-revisions=<prev>=100`.
- **Deployed version**: recorded as the `APP_VERSION` env var on the service
  (`gcloud run services describe reqbase --region=europe-west1`).
- **Websockets** (collab rooms) ride the same HTTP port; `--timeout=3600` and
  `--no-cpu-throttling` are set so background flush/heartbeat work keeps
  running while rooms are open.
