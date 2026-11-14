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
go build -o "$work/setwebhook" ./cmd/setwebhook

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

for _ in $(seq 60); do
	grep -q "Registered tunnel connection" "$work/tunnel.log" && break
	kill -0 "$tunnel_pid" 2>/dev/null || fail "cloudflared exited, see $work/tunnel.log"
	sleep 0.5
done

# reachable checks the tunnel reaches mayfly: only mayfly answers GET /webhook with 405 (POST only);
# the tunnel answers 502 or 530 when it cannot (decision 018). DNS-over-HTTPS avoids a cached
# "no such domain" that an early lookup can leave in the local resolver for a while.
reachable() {
	[[ "$(curl -sS -o /dev/null -w '%{http_code}' -m 5 --doh-url https://1.1.1.1/dns-query "$url/webhook" 2>/dev/null)" == 405 ]]
}
for _ in $(seq 60); do
	reachable && break
	sleep 1
done
reachable || fail "$url/webhook is not reachable, see $work/tunnel.log"

"$work/setwebhook" "$url/webhook" || fail "could not set the App webhook URL; set it by hand to $url/webhook"

cat <<EOF

  Mayfly is reachable through the tunnel, and the App webhook URL is set to:

      $url/webhook

  Trigger a job:  gh workflow run mayfly-smoke -R TeddyBertrand/mayfly-demo
  Press Ctrl-C to stop.

EOF

# Stop as soon as either process exits.
wait -n "$mayfly_pid" "$tunnel_pid" || true
