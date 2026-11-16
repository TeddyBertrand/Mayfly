#!/usr/bin/env bash
# Register and start, or stop and unregister, the stock self-hosted runner used as a benchmark baseline.
# See docs/benchmarks.md. Usage: scripts/classic-runner.sh up|down
set -euo pipefail

cd "$(dirname "$0")/.."

repo="${DEMO_REPO:-TeddyBertrand/mayfly-demo}"
name="classic-runner"
label="classic"
# Same network and bridge name as Mayfly's workers, so host firewall rules apply to both.
network="mayfly-workers"
compose=(docker compose --profile classic)
# Compose prefixes volumes with the project name, `name: mayfly` in compose.yaml.
volume="mayfly_classic_runner"

# fail prints a message to stderr and exits.
fail() {
	echo "classic-runner.sh: $*" >&2
	exit 1
}

# runner_token asks GitHub for a short-lived registration or removal token; it is never printed.
runner_token() {
	gh api -X POST "repos/$repo/actions/runners/$1-token" --jq .token
}

# registered reports whether the runner volume already holds a registration.
registered() {
	"${compose[@]}" run --rm --no-deps classic test -f .runner >/dev/null 2>&1
}

# up creates the network if needed, registers the runner once and starts it.
up() {
	docker network inspect "$network" >/dev/null 2>&1 ||
		docker network create --driver bridge -o "com.docker.network.bridge.name=$network" "$network" >/dev/null
	if ! registered; then
		RUNNER_TOKEN="$(runner_token registration)" || fail "could not get a registration token for $repo"
		export RUNNER_TOKEN
		# The token stays in the environment; bash -c expands it inside the container, not on this command line.
		"${compose[@]}" run --rm --no-deps -e RUNNER_TOKEN classic bash -c \
			"./config.sh --unattended --replace --disableupdate --url https://github.com/$repo --token \"\$RUNNER_TOKEN\" --name $name --labels $label" >/dev/null ||
			fail "runner registration failed"
		unset RUNNER_TOKEN
	fi
	"${compose[@]}" up -d classic
	echo "classic-runner.sh: $name is up on $repo with labels [self-hosted, $label]"
}

# down stops the runner, removes its registration from GitHub and deletes its volume.
down() {
	"${compose[@]}" rm --stop --force classic
	if registered; then
		RUNNER_TOKEN="$(runner_token remove)" || fail "could not get a removal token for $repo"
		export RUNNER_TOKEN
		"${compose[@]}" run --rm --no-deps -e RUNNER_TOKEN classic bash -c './config.sh remove --token "$RUNNER_TOKEN"' >/dev/null ||
			fail "runner removal failed; delete $name in the repo's runner settings"
		unset RUNNER_TOKEN
	fi
	docker volume rm "$volume" >/dev/null 2>&1 || true
	echo "classic-runner.sh: $name removed"
}

for cmd in docker gh; do
	command -v "$cmd" >/dev/null || fail "$cmd not found"
done
case "${1:-}" in
up) up ;;
down) down ;;
*) fail "usage: scripts/classic-runner.sh up|down" ;;
esac
