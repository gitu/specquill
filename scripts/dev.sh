#!/usr/bin/env bash
# Full dev loop: postgres + Go hot-rebuild (air) + vite HMR.
# Frontend: http://localhost:5173 (HMR, proxies /api + /auth to :8643)
# API:      http://localhost:8643 (serves the last *embedded* SPA — stale in dev, use :5173)
set -euo pipefail
cd "$(dirname "$0")/.."

command -v air >/dev/null || { echo "air not found — install: go install github.com/air-verse/air@latest" >&2; exit 1; }

docker compose -f docker-compose.dev.yml up -d postgres

# First run only; for a full reset use `make dev-fixture` (also recreates the pg schema).
[ -d data/runtime ] || ./scripts/dev-fixture.sh

# Both run as background jobs so a signal interrupts `wait` and cleanup runs
# (a foreground child would defer the trap until it exits). Cleanup is
# PID-based with SIGKILL escalation — air can wedge on TERM/INT, and its
# child specquill outlives a hard-killed air.
cleanup() {
  trap - EXIT INT TERM
  kill "$VITE_PID" "$AIR_PID" 2>/dev/null || true
  for _ in 1 2 3 4 5; do kill -0 "$AIR_PID" 2>/dev/null || break; sleep 1; done
  kill -9 "$AIR_PID" 2>/dev/null || true
  pkill -x specquill 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM
(cd web && npm run dev) & VITE_PID=$!
air & AIR_PID=$!
wait
