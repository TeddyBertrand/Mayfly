#!/usr/bin/env bash
# Start the Compose deploy with the cloudflared tunnel and point the App webhook at it. See docs/local-dev.md.
set -euo pipefail

cd "$(dirname "$0")/.."

# fail prints a message to stderr and exits.
fail() {
	echo "compose-dev.sh: $*" >&2
	exit 1
}

[[ -f .env ]] || fail ".env is missing; copy .env.example and fill it in"
docker compose --profile tunnel up -d --build --wait || fail "stack did not become healthy, see: docker compose logs"

host=""
for _ in $(seq 60); do
	host="$(curl -fsS -m 2 http://127.0.0.1:20241/quicktunnel 2>/dev/null | grep -o '[a-z0-9-]*\.trycloudflare\.com' || true)"
	[[ -n "$host" ]] && break
	sleep 0.5
done
[[ -n "$host" ]] || fail "no tunnel hostname after 30s, see: docker compose logs tunnel"
url="https://$host"

for _ in $(seq 60); do
	docker compose logs tunnel 2>/dev/null | grep -q "Registered tunnel connection" && break
	sleep 0.5
done

# DNS-over-HTTPS avoids a cached "no such domain" from an early local lookup (same as dev.sh).
for _ in $(seq 120); do
	curl -fsS -m 5 --doh-url https://1.1.1.1/dns-query "$url/healthz" >/dev/null 2>&1 && break
	sleep 1
done
curl -fsS -m 5 --doh-url https://1.1.1.1/dns-query "$url/healthz" >/dev/null 2>&1 ||
	fail "$url/healthz is not reachable, see: docker compose logs tunnel"

docker compose run --rm --no-deps --entrypoint /usr/local/bin/setwebhook mayfly "$url/webhook" ||
	fail "could not set the App webhook URL; set it by hand to $url/webhook"

cat <<EOF

  Mayfly runs in Compose and the App webhook URL is set to:

      $url/webhook

  Trigger a job:  make smoke
  Logs:           docker compose logs -f
  Stop:           make compose-down

EOF
