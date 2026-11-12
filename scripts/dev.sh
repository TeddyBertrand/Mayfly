#!/usr/bin/env bash
# Run Mayfly locally behind a cloudflared quick tunnel. See docs/local-dev.md.
set -euo pipefail

cd "$(dirname "$0")/.."

export MAYFLY_APP_ID="${MAYFLY_APP_ID:-5714458}"
export MAYFLY_APP_KEY_FILE="${MAYFLY_APP_KEY_FILE:-$HOME/.config/mayfly/app.pem}"
export MAYFLY_WEBHOOK_SECRET_FILE="${MAYFLY_WEBHOOK_SECRET_FILE:-$HOME/.config/mayfly/webhook-secret}"
export MAYFLY_LISTEN_ADDR="${MAYFLY_LISTEN_ADDR:-127.0.0.1:8080}"

# fail prints a message to stderr and exits.
fail() {
	echo "dev.sh: $*" >&2
	exit 1
}

for cmd in go docker cloudflared curl; do
	command -v "$cmd" >/dev/null || fail "$cmd not found (see docs/local-dev.md, Prerequisites)"
done
docker info >/dev/null 2>&1 || fail "cannot reach the Docker daemon"
for f in "$MAYFLY_APP_KEY_FILE" "$MAYFLY_WEBHOOK_SECRET_FILE"; do
	[[ -r "$f" ]] || fail "$f is missing or unreadable (see docs/local-dev.md, One-time setup)"
done

work="$(mktemp -d -t mayfly-dev.XXXXXX)"
target="http://${MAYFLY_LISTEN_ADDR/#:/127.0.0.1:}"

echo "dev.sh: building into $work"
go build -o "$work/mayfly" ./cmd/mayfly

mayfly_pid=""
tunnel_pid=""

# cleanup stops both processes and waits for Mayfly to remove its workers.
cleanup() {
	trap - EXIT INT TERM
	echo
	echo "dev.sh: stopping (logs kept in $work)"
	[[ -n "$tunnel_pid" ]] && kill -TERM "$tunnel_pid" 2>/dev/null || true
	[[ -n "$mayfly_pid" ]] && kill -TERM "$mayfly_pid" 2>/dev/null || true
	wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

"$work/mayfly" > >(tee "$work/mayfly.log") 2>&1 &
mayfly_pid=$!

cloudflared tunnel --no-autoupdate --url "$target" >"$work/tunnel.log" 2>&1 &
tunnel_pid=$!

url=""
for _ in $(seq 60); do
	url="$(grep -o 'https://[a-z0-9-]*\.trycloudflare\.com' "$work/tunnel.log" | head -n1 || true)"
	[[ -n "$url" ]] && break
	kill -0 "$tunnel_pid" 2>/dev/null || fail "cloudflared exited, see $work/tunnel.log"
	sleep 0.5
done
[[ -n "$url" ]] || fail "no tunnel URL after 30s, see $work/tunnel.log"

# A new trycloudflare hostname can take a few seconds to resolve.
for _ in $(seq 30); do
	curl -fsS -m 5 "$url/healthz" >/dev/null 2>&1 && break
	sleep 1
done
curl -fsS -m 5 "$url/healthz" >/dev/null 2>&1 || fail "$url/healthz is not reachable, see $work/tunnel.log"

cat <<EOF

  Mayfly is reachable through the tunnel.
  Set the App webhook URL (GitHub App settings, General, Webhook URL) to:

      $url/webhook

  Then trigger a job:  gh workflow run mayfly-smoke -R TeddyBertrand/mayfly-demo
  Press Ctrl-C to stop.

EOF

# Stop as soon as either process exits.
wait -n "$mayfly_pid" "$tunnel_pid" || true
