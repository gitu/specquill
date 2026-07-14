#!/usr/bin/env bash
# Build TWO extra sample spec repositories with a real multi-commit history
# under data/origin/ — for local testing of history-aware features (file
# history, log.md, blame-ish flows, PR diffs) beyond the single-commit demo
# fixtures. Complements scripts/dev-fixture.sh; never touches postgres.
#
#   sample-payments.git    — payments platform specs, 7 commits, 2 authors
#   sample-onboarding.git  — customer onboarding specs, 6 commits, 2 authors
#
# With the dev server running (-dev auto-auth) the repos are registered as
# projects automatically; otherwise the curl commands are printed.
set -euo pipefail
cd "$(dirname "$0")/.."

ORIGIN=data/origin
mkdir -p "$ORIGIN"

SERVER="${SPECQUILL_URL:-http://127.0.0.1:8643}"

alice=(-c user.name=Alice\ Sample -c user.email=alice@sample.local)
bob=(-c user.name=Bob\ Sample -c user.email=bob@sample.local)

# commit <workdir> <author-array-name> <days-ago> <message>
commit() {
  local dir="$1" who="$2" age="$3" msg="$4" date
  date="$(date -u -d "$age days ago" +%Y-%m-%dT12:00:00)"
  local -n id="$who"
  git "${id[@]}" -C "$dir" add -A
  GIT_AUTHOR_DATE="$date" GIT_COMMITTER_DATE="$date" \
    git "${id[@]}" -C "$dir" commit -q -m "$msg"
}

doc() { # doc <path> <type> <title> <status> — writes minimal OKF frontmatter, body on stdin
  mkdir -p "$(dirname "$1")"
  { printf -- '---\ntype: %s\ntitle: %s\nstatus: %s\n---\n\n' "$2" "$3" "$4"; cat; } > "$1"
}

publish() { # publish <name> <workdir>
  local bare="$ORIGIN/$1.git"
  rm -rf "$bare"
  git init -q --bare -b main "$bare"
  git -C "$2" push -q "$(pwd)/$bare" main
}

# ---------------------------------------------------------------- payments
tmp="$(mktemp -d)"
git -C "$tmp" init -q -b main
( cd "$tmp"
  printf -- '---\nokf_version: "0.1"\n---\n\n# Payments platform specs\n' > index.md
  doc requirements/REQ-001.md Requirement "Idempotent payment capture" draft <<'EOF'
# Idempotent payment capture

> **REQ-001.1 · MUST** — A capture request replayed with the same idempotency
> key SHALL return the original result and move no additional funds.
EOF
)
commit "$tmp" alice 30 "scaffold payments workspace"
( cd "$tmp"
  doc requirements/REQ-002.md Requirement "Settlement cut-off handling" draft <<'EOF'
# Settlement cut-off handling

> **REQ-002.1 · MUST** — Captures after the acquirer cut-off SHALL settle in
> the next window and be flagged `deferred` in the ledger.
EOF
)
commit "$tmp" alice 26 "add settlement cut-off requirement"
( cd "$tmp"
  doc specs/capture-flow.md Specification "Capture flow" draft <<'EOF'
# Capture flow

How [REQ-001](../requirements/REQ-001.md) is realized: capture requests are
keyed on `(merchant, idempotency_key)`; replays short-circuit to the stored
outcome.
EOF
)
commit "$tmp" bob 22 "spec: capture flow with idempotency store"
( cd "$tmp"
  sed -i 's/move no additional funds./move no additional funds within a 24h key-retention window./' requirements/REQ-001.md
)
commit "$tmp" bob 15 "tighten REQ-001: bound the idempotency window"
( cd "$tmp"
  doc specs/settlement.md Specification "Settlement windows" draft <<'EOF'
# Settlement windows

Realizes [REQ-002](../requirements/REQ-002.md). Cut-offs are configured per
acquirer; deferred captures land in the next window's batch.
EOF
)
commit "$tmp" alice 10 "spec: settlement windows"
( cd "$tmp"
  sed -i 's/^status: draft/status: approved/' requirements/REQ-001.md specs/capture-flow.md
)
commit "$tmp" alice 6 "approve capture flow + REQ-001"
( cd "$tmp"
  doc requirements/REQ-003.md Requirement "Refund traceability" draft <<'EOF'
# Refund traceability

> **REQ-003.1 · MUST** — Every refund SHALL reference its original capture in
> the [capture flow](../specs/capture-flow.md).
EOF
)
commit "$tmp" bob 2 "add refund traceability requirement"
publish sample-payments "$tmp"
rm -rf "$tmp"

# -------------------------------------------------------------- onboarding
tmp="$(mktemp -d)"
git -C "$tmp" init -q -b main
( cd "$tmp"
  printf -- '---\nokf_version: "0.1"\n---\n\n# Customer onboarding specs\n' > index.md
  doc requirements/REQ-001.md Requirement "KYC before first transaction" draft <<'EOF'
# KYC before first transaction

> **REQ-001.1 · MUST** — An account SHALL NOT transact before identity
> verification completes.
EOF
)
commit "$tmp" bob 21 "scaffold onboarding workspace"
( cd "$tmp"
  doc glossary/glossary.md Glossary "Glossary" draft <<'EOF'
# Glossary

**KYC** — know your customer: the identity verification a regulated account
must pass before activation.
EOF
)
commit "$tmp" bob 18 "start a glossary"
( cd "$tmp"
  doc specs/kyc-gate.md Specification "KYC activation gate" draft <<'EOF'
# KYC activation gate

Realizes [REQ-001](../requirements/REQ-001.md): accounts are created
`pending`, and the transaction service refuses `pending` accounts.
EOF
)
commit "$tmp" alice 14 "spec: KYC activation gate"
( cd "$tmp"
  doc requirements/REQ-002.md Requirement "Progressive profile capture" draft <<'EOF'
# Progressive profile capture

> **REQ-002.1 · SHOULD** — Only the fields required for the next step SHALL be
> requested, per the [KYC gate](../specs/kyc-gate.md).
EOF
)
commit "$tmp" alice 9 "add progressive profiling requirement"
( cd "$tmp"
  sed -i 's/refuses `pending` accounts./refuses `pending` accounts with error code `account_pending`./' specs/kyc-gate.md
)
commit "$tmp" bob 5 "kyc-gate: pin the refusal error code"
( cd "$tmp"
  sed -i 's/^status: draft/status: in_review/' specs/kyc-gate.md requirements/REQ-001.md
)
commit "$tmp" bob 1 "move KYC gate + REQ-001 to review"
publish sample-onboarding "$tmp"
rm -rf "$tmp"

echo "sample origins ready:"
for r in sample-payments sample-onboarding; do
  echo "  $r: $(git -C "$ORIGIN/$r.git" rev-list --count main) commits on main"
done

# register as projects when the dev server is up (auto-auth in -dev mode)
register() {
  curl -sf -X POST "$SERVER/api/projects" -H 'X-SpecQuill: 1' -H 'Content-Type: application/json' \
    -d "{\"id\":\"$1\",\"remote\":\"$(pwd)/$ORIGIN/$1.git\"}"
}
if curl -sf -o /dev/null --max-time 2 "$SERVER/api/repos" -H 'X-SpecQuill: 1'; then
  for r in sample-payments sample-onboarding; do
    if out="$(register "$r" 2>&1)"; then
      echo "registered project $r"
    else
      echo "project $r not registered (already exists?)"
    fi
  done
else
  echo "dev server not reachable at $SERVER — register manually once it runs:"
  echo "  curl -X POST $SERVER/api/projects -H 'X-SpecQuill: 1' -H 'Content-Type: application/json' \\"
  echo "       -d '{\"id\":\"sample-payments\",\"remote\":\"$(pwd)/$ORIGIN/sample-payments.git\"}'"
fi
