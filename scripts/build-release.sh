#!/usr/bin/env bash
# Build release archives of mayfly for every target into OUT_DIR (default dist/). Used by the release workflow.
set -euo pipefail

cd "$(dirname "$0")/.."

out="${1:-dist}"
version="$(tr -d '[:space:]' < VERSION)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "build-release.sh: VERSION '$version' is not X.Y.Z" >&2; exit 1; }

targets=(linux/amd64 linux/arm64)
commit_time="$(git log -1 --format=%ct)"

rm -rf "$out"
mkdir -p "$out"
for target in "${targets[@]}"; do
	os="${target%/*}"
	arch="${target#*/}"
	name="mayfly_${version}_${os}_${arch}"
	stage="$(mktemp -d)"

	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X main.version=$version" -o "$stage/mayfly" ./cmd/mayfly
	cp LICENSE README.md "$stage/"

	# Fixed owner, order and mtime make the archive identical for the same commit.
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$commit_time" \
		-czf "$out/$name.tar.gz" -C "$stage" mayfly LICENSE README.md
	rm -rf "$stage"
	echo "built $out/$name.tar.gz"
done

(cd "$out" && sha256sum ./*.tar.gz | sed 's| \./| |' > SHA256SUMS)
echo "version=$version"
