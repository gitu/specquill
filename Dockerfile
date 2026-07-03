# syntax=docker/dockerfile:1.7
# Multi-stage build for reqbase. Produces a slim Alpine image with the single
# Go binary (SPA embedded) plus the `git` CLI it shells out to.
#
# Stage 1 (web)    — npm ci + vite build into the Go embed dir.
# Stage 2 (server) — static Go build with the SPA embedded.
# Stage 3 (runner) — alpine + git (reqbase requires git >= 2.38) + the binary.
#
# The runtime config is baked in from deploy/reqbase.cloud.yml — it contains
# no secrets (tokens/keys are referenced by env-var NAME and injected at
# deploy time, e.g. from Secret Manager — see cloudbuild.yaml).

ARG GO_VERSION=1.26
ARG NODE_VERSION=24

# ---------- web ----------
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src
# manifests first so `npm ci` caches across source-only commits
COPY web/package.json web/package-lock.json web/
RUN cd web && npm ci
COPY web/ web/
# vite outDir is ../server/internal/webui/dist (the Go embed dir)
RUN mkdir -p server/internal/webui/dist && cd web && npm run build

# ---------- server ----------
FROM golang:${GO_VERSION}-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/webui/dist internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /reqbased ./cmd/reqbased

# ---------- runner ----------
FROM alpine:3.22 AS runner
# reqbase shells out to git for every repo operation (needs >= 2.38)
RUN apk add --no-cache git ca-certificates tzdata \
 && adduser -D -H -u 10001 reqbase \
 && mkdir -p /var/lib/reqbase && chown reqbase /var/lib/reqbase
COPY --from=server /reqbased /usr/local/bin/reqbased
COPY deploy/reqbase.cloud.yml /etc/reqbase/reqbase.yml
USER reqbase
EXPOSE 8080
ENTRYPOINT ["reqbased"]
CMD ["-config", "/etc/reqbase/reqbase.yml"]
