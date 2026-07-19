#!/usr/bin/env bash
# Register the SpecQuill GitHub App via the app-manifest flow.
#
# GitHub App creation cannot be done headlessly — but the manifest flow gets
# it down to ONE browser click: this script serves a pre-filled manifest
# form, you press "Create GitHub App" while signed in to github.com, GitHub
# redirects back here with a one-hour code, and `gh api` converts that code
# into the full credential set (app id, client id+secret, webhook secret,
# private-key PEM).
#
# Requirements: gh (authenticated: `gh auth status`), python3.
#
# Usage:
#   deploy/gh-app-setup.sh --url https://specquill.example.com [--name SpecQuill] [--org my-org] [--private]
#
#   --url     the deployment's base URL (auth callback + webhook derive from it)
#   --name    app name, globally unique on GitHub, max 34 chars (default: SpecQuill)
#   --org     register under an organization instead of your user account
#   --private only the owning account can install (default: public, so any
#             org/account can install — one tenant per installation)
set -euo pipefail

NAME="SpecQuill" URL="" ORG="" PUBLIC=true PORT=8977
while [ $# -gt 0 ]; do
  case "$1" in
    --name) NAME="$2"; shift 2 ;;
    --url) URL="${2%/}"; shift 2 ;;
    --org) ORG="$2"; shift 2 ;;
    --private) PUBLIC=false; shift ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$URL" ] || { echo "required: --url https://your-specquill-host" >&2; exit 2; }
gh auth status >/dev/null || { echo "gh is not authenticated — run: gh auth login" >&2; exit 2; }

FORM_URL="https://github.com/settings/apps/new"
[ -n "$ORG" ] && FORM_URL="https://github.com/organizations/$ORG/settings/apps/new"

echo "→ open http://127.0.0.1:$PORT and click 'Create GitHub App' (waiting) …"
( command -v xdg-open >/dev/null && xdg-open "http://127.0.0.1:$PORT" >/dev/null 2>&1 || true ) &

# One tiny local server, two jobs: GET / serves the auto-submitting manifest
# form; GET /callback receives GitHub's redirect and prints the temp code.
CODE=$(NAME="$NAME" URL="$URL" FORM_URL="$FORM_URL" PUBLIC="$PUBLIC" PORT="$PORT" python3 - <<'PY'
import html, json, os, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

port = int(os.environ["PORT"])
manifest = json.dumps({
    "name": os.environ["NAME"],
    "url": os.environ["URL"],
    "hook_attributes": {"url": os.environ["URL"] + "/hooks/github", "active": True},
    "redirect_url": f"http://127.0.0.1:{port}/callback",
    "callback_urls": [os.environ["URL"] + "/auth/github/callback"],
    "public": os.environ["PUBLIC"] == "true",
    # the app edge needs exactly this (repo-product/docs/specs/specs/multi-tenancy.md):
    # contents:rw (clone/push), pull_requests:rw, metadata:r. `installation`
    # and `installation_repositories` webhooks are delivered to apps
    # automatically; only `push` must be subscribed.
    "default_permissions": {"contents": "write", "pull_requests": "write", "metadata": "read"},
    "default_events": ["push"],
})
form = f"""<!doctype html><meta charset="utf-8"><title>Register SpecQuill</title>
<body style="font-family:system-ui;margin:3em">
<h3>Register the SpecQuill GitHub App</h3>
<p>This submits the app manifest to GitHub — review and press <b>Create GitHub App</b> there.</p>
<form action="{html.escape(os.environ["FORM_URL"])}" method="post">
<input type="hidden" name="manifest" value="{html.escape(manifest)}">
<button type="submit" style="font-size:1.1em;padding:.5em 1.2em">Continue to GitHub</button>
</form>"""

class H(BaseHTTPRequestHandler):
    code = None
    def do_GET(self):
        u = urlparse(self.path)
        if u.path == "/callback":
            H.code = parse_qs(u.query).get("code", [""])[0]
            body = "<body style='font-family:system-ui;margin:3em'>✓ App created — return to the terminal."
        else:
            body = form
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.end_headers()
        self.wfile.write(body.encode())
    def log_message(self, *a): pass

srv = HTTPServer(("127.0.0.1", port), H)
while H.code is None:
    srv.handle_request()
print(H.code)
PY
)
[ -n "$CODE" ] || { echo "no code received — the manifest was not accepted" >&2; exit 1; }

# The temp code (valid 1h) converts into the app's full credentials.
OUT=$(gh api --method POST "/app-manifests/$CODE/conversions")
APP_ID=$(echo "$OUT"    | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
SLUG=$(echo "$OUT"      | python3 -c 'import json,sys; print(json.load(sys.stdin)["slug"])')
CLIENT_ID=$(echo "$OUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["client_id"])')
CLIENT_SECRET=$(echo "$OUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["client_secret"])')
WEBHOOK_SECRET=$(echo "$OUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["webhook_secret"])')
PEM_FILE="specquill-gh-app.$APP_ID.private-key.pem"
echo "$OUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["pem"], end="")' > "$PEM_FILE"
chmod 600 "$PEM_FILE"

cat <<EOF

✓ GitHub App created: https://github.com/apps/$SLUG   (app id $APP_ID)
✓ private key written to ./$PEM_FILE — move it somewhere safe, it is shown once

# --- specquill.yml -----------------------------------------------------------
auth:
  github:
    enabled: true
    client_id: $CLIENT_ID
    client_secret_env: SPECQUILL_GH_CLIENT_SECRET
github_app:
  app_id: $APP_ID
  private_key_env: SPECQUILL_GH_APP_KEY        # or private_key_path: /etc/specquill/github-app.pem
  webhook_secret_env: SPECQUILL_GH_APP_WEBHOOK_SECRET

# --- environment (self-host / systemd / compose) -----------------------------
export SPECQUILL_GH_CLIENT_SECRET='$CLIENT_SECRET'
export SPECQUILL_GH_APP_WEBHOOK_SECRET='$WEBHOOK_SECRET'
export SPECQUILL_GH_APP_KEY="\$(cat $PEM_FILE)"

# --- or Google Secret Manager (deploy/cloud.md) -------------------------------
gcloud secrets create SPECQUILL_GH_APP_KEY --data-file=$PEM_FILE
printf %s '$WEBHOOK_SECRET' | gcloud secrets create SPECQUILL_GH_APP_WEBHOOK_SECRET --data-file=-
printf %s '$CLIENT_SECRET'  | gcloud secrets create SPECQUILL_GH_CLIENT_SECRET --data-file=-

# --- finally: install the app (creates the first tenant via webhook) ----------
open https://github.com/apps/$SLUG/installations/new
EOF
