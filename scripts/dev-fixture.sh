#!/usr/bin/env bash
# Build local bare "origin" repos under data/origin/ from the repo/ demo content:
#   trading-specs.git  — writable workspace (main + feature/mifid-update)
#   regulations.git    — read-only input repo (regulations/ only)
set -euo pipefail
cd "$(dirname "$0")/.."

ORIGIN=data/origin
rm -rf "$ORIGIN"
mkdir -p "$ORIGIN"

# the store lives in the compose postgres — reset it alongside the fixtures
# so sessions/PRs/collab logs don't reference vanished git state
docker compose -f docker-compose.dev.yml up -d --wait postgres
docker compose -f docker-compose.dev.yml exec -T postgres \
  psql -q -U specquill -d specquill -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'

fixture_env=(-c user.name=specquill-fixture -c user.email=fixture@specquill.local)

make_bare() { # $1=name  $2=src-dir
  local bare="$ORIGIN/$1.git" tmp
  tmp="$(mktemp -d)"
  git "${fixture_env[@]}" -C "$tmp" init -q -b main
  cp -r "$2"/. "$tmp"/
  git "${fixture_env[@]}" -C "$tmp" add -A
  git "${fixture_env[@]}" -C "$tmp" commit -q -m "import demo content"
  git init -q --bare -b main "$bare"
  git -C "$tmp" push -q "$(pwd)/$bare" main
  rm -rf "$tmp"
}

# trading-specs: full workspace
make_bare trading-specs repo
# feature branch so the branch switcher has something to show
tmp="$(mktemp -d)"
git clone -q "$ORIGIN/trading-specs.git" "$tmp"
git "${fixture_env[@]}" -C "$tmp" switch -q -c feature/mifid-update
sed -i 's/recorded to the \*\*microsecond\*\*/recorded to the **microsecond** (amended)/' "$tmp/specs/txn-report.md" || true
git "${fixture_env[@]}" -C "$tmp" commit -qam "spec: note RTS 22 amendment" || true
git -C "$tmp" push -q origin feature/mifid-update
rm -rf "$tmp"

# regulations: read-only input repo (only the regulations/ folder)
tmp="$(mktemp -d)"
mkdir -p "$tmp/regulations"
cp repo/regulations/*.md "$tmp/regulations/"
make_bare regulations "$tmp"
rm -rf "$tmp"

echo "fixture origins ready:"
git -C "$ORIGIN/trading-specs.git" for-each-ref --format='  trading-specs %(refname:short) %(objectname:short)' refs/heads
git -C "$ORIGIN/regulations.git" for-each-ref --format='  regulations   %(refname:short) %(objectname:short)' refs/heads
