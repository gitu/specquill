#!/usr/bin/env bash
# Build local bare "origin" repos under data/origin/ from the repo/ demo content:
#   trading-specs.git  — writable workspace (main + feature/mifid-update)
#   regulations.git    — read-only input repo (regulations/ only)
set -euo pipefail
cd "$(dirname "$0")/.."

ORIGIN=data/origin
rm -rf "$ORIGIN"
mkdir -p "$ORIGIN"

# drop the store alongside the fixtures so sessions don't
# reference vanished git state (-wal/-shm are WAL sidecars)
rm -f data/runtime/specquill.db data/runtime/specquill.db-wal data/runtime/specquill.db-shm

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

# specquill-docs: SpecQuill's own product specs — a MONOREPO example whose
# workspace lives in the docs/specs/ subfolder (content_root project)
make_bare specquill-docs repo-product
# a couple of real commits on main so the change history (REQ-027) has
# something to read: a status transition and a reworded normative statement
tmp="$(mktemp -d)"
git clone -q "$ORIGIN/trading-specs.git" "$tmp"
sed -i 's/^status: draft/status: in_review/' "$tmp/requirements/REQ-063.md"
git "${fixture_env[@]}" -C "$tmp" commit -qam "req: partial-fill reporting moves to review"
sed -i 's/SHALL carry a `fills\[\]` array; each fill/SHALL carry a `fills[]` array of at most 500 entries; each fill/' "$tmp/requirements/REQ-063.md"
sed -i 's/^ends: 2026-09-10/ends: 2026-09-30/' "$tmp/requirements/REQ-090.md"
git "${fixture_env[@]}" -C "$tmp" commit -qam "req: bound the fills array, extend the retention window"
git -C "$tmp" push -q origin main
rm -rf "$tmp"

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
