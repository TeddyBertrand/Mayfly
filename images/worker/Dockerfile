# Worker image: the official runner plus pinned toolchains in its tool cache (decision 022).
ARG RUNNER_VERSION=2.338.2
ARG GO_VERSION=1.27.4
ARG NODE_VERSION=24.22.3

FROM ghcr.io/actions/actions-runner:${RUNNER_VERSION} AS toolchains
ARG TARGETARCH
ARG TOOL_CACHE=/opt/hostedtoolcache
ARG GO_VERSION
ARG NODE_VERSION
ARG GO_SHA256_AMD64=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840
ARG GO_SHA256_ARM64=3392582988a273a87ab5043ee1b2c9c17a2c3c00580bf9326c47a2e0aaaeac92
ARG NODE_SHA256_AMD64=061ec3e3062115705b56b51fa9cc330ab50d34b6969387d4e4b1a50c6809acc2
ARG NODE_SHA256_ARM64=261af2af250dfd7e9f25cf151d1b2713a0adf8f10a9ad116c4947f68627affe8
USER root
WORKDIR /tmp/dl
RUN set -eu; \
	case "$TARGETARCH" in \
		amd64) cache_arch=x64; node_arch=x64; go_sha=$GO_SHA256_AMD64; node_sha=$NODE_SHA256_AMD64 ;; \
		arm64) cache_arch=arm64; node_arch=arm64; go_sha=$GO_SHA256_ARM64; node_sha=$NODE_SHA256_ARM64 ;; \
		*) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
	esac; \
	go_dir="$TOOL_CACHE/go/$GO_VERSION/$cache_arch"; \
	node_dir="$TOOL_CACHE/node/$NODE_VERSION/$cache_arch"; \
	mkdir -p "$go_dir" "$node_dir"; \
	curl -fsSL -o go.tar.gz "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
	echo "$go_sha  go.tar.gz" | sha256sum -c -; \
	tar -xzf go.tar.gz -C "$go_dir" --strip-components=1; \
	touch "$go_dir.complete"; \
	curl -fsSL -o node.tar.gz "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node_arch}.tar.gz"; \
	echo "$node_sha  node.tar.gz" | sha256sum -c -; \
	tar -xzf node.tar.gz -C "$node_dir" --strip-components=1; \
	touch "$node_dir.complete"

FROM ghcr.io/actions/actions-runner:${RUNNER_VERSION}
ARG RUNNER_VERSION
ARG GO_VERSION
ARG NODE_VERSION
ARG TOOL_CACHE=/opt/hostedtoolcache
LABEL io.mayfly.worker.runner-version=${RUNNER_VERSION} \
	io.mayfly.worker.go-version=${GO_VERSION} \
	io.mayfly.worker.node-version=${NODE_VERSION}
COPY --from=toolchains --chown=runner:runner ${TOOL_CACHE} ${TOOL_CACHE}
ENV RUNNER_TOOL_CACHE=${TOOL_CACHE} AGENT_TOOLSDIRECTORY=${TOOL_CACHE}
