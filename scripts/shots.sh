#!/usr/bin/env bash
# Regenerate the docs/screenshots gallery. Boots an ISOLATED specquill on
# :8663 (own data dir, embedded SPA, mock-llm as mock-1) against the demo
# fixtures and runs the SHOT=1 playwright sweep in web/e2e/screenshot.spec.ts.
# Never touches the :8643 dev server or its store. Usage: make shots
set -euo pipefail
cd "$(dirname "$0")/.."

PORT=8663
# config paths resolve relative to the config file — keep it at the repo root
CFG=specquill.shots.yml
DATA=data/runtime-shots

[ -x server/specquill ] || { echo "server/specquill missing — run: make build" >&2; exit 1; }
[ -d data/origin ] || scripts/dev-fixture.sh

# isolated config: own port + data dir, AI pointed at the deterministic mock
# (the openapi demo source self-fetches, so its port moves along)
sed -e "s|8643|$PORT|g" \
    -e "s|\./data/runtime|./$DATA|" \
    -e "s|http://127.0.0.1:11434/v1|http://127.0.0.1:8991/v1|" \
    -e "s|qwen2.5:7b|mock-1|g" \
    specquill.dev.yml > "$CFG"

rm -rf "$DATA"

SERVER_PID=""
MOCK_PID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null || true
}
trap cleanup EXIT

# deterministic keyless LLM — the speccy and alignment shots need it
if ! curl -s -o /dev/null http://127.0.0.1:8991/; then
  python3 scripts/mock-llm.py >/dev/null 2>&1 &
  MOCK_PID=$!
fi

# embedded build only: a vite from any checkout on :5643 would hijack the SPA
SPECQUILL_VITE_ADDR=127.0.0.1:1 ./server/specquill -config "$CFG" -dev \
  >/tmp/specquill-shots.log 2>&1 &
SERVER_PID=$!

# boot race: /api/repos answering 200 does NOT mean the clones are ready —
# wait until each project's snapshot lists real files
for repo in trading-specs specquill-docs; do
  ready=""
  for _ in $(seq 1 60); do
    if curl -sf "http://127.0.0.1:$PORT/api/repos/$repo/snapshot?ref=main" 2>/dev/null | grep -q '"files":{"'; then
      ready=1; break
    fi
    sleep 1
  done
  [ -n "$ready" ] || { echo "$repo never became ready (see /tmp/specquill-shots.log)" >&2; exit 1; }
done

mkdir -p docs/screenshots
(cd web && SHOT=1 SPECQUILL_URL="http://127.0.0.1:$PORT" npx playwright test e2e/screenshot.spec.ts)

echo "gallery updated: docs/screenshots/"
