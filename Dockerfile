# syntax=docker/dockerfile:1.7
# Multi-stage build for specquill. Produces a slim Alpine image with the single
# Go binary (SPA embedded) plus the `git` CLI it shells out to.
#
# Stage 1 (web)    — npm ci + vite build into the Go embed dir.
# Stage 2 (server) — static Go build with the SPA embedded.
# Stage 3 (runner) — alpine + git (specquill requires git >= 2.38) + the binary.
#
# The runtime config is baked in from deploy/specquill.cloud.yml — it contains
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
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /specquill ./cmd/specquill

# ---------- runner ----------
FROM alpine:3.22 AS runner
# specquill shells out to git for every repo operation (needs >= 2.38)
RUN apk add --no-cache git ca-certificates tzdata \
 && adduser -D -H -u 10001 specquill \
 && mkdir -p /var/lib/specquill && chown specquill /var/lib/specquill
COPY --from=server /specquill /usr/local/bin/specquill
COPY deploy/specquill.cloud.yml /etc/specquill/specquill.yml
USER specquill
EXPOSE 8080
ENTRYPOINT ["specquill"]
CMD ["-config", "/etc/specquill/specquill.yml"]
