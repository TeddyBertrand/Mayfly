# Image holding mayfly, the reaper and setwebhook (decision 015). Compose picks the command.
# Go cross-compiles on the build platform, so multi-arch builds need no CPU emulation.
FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN for cmd in mayfly reaper setwebhook; do \
		CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
			-ldflags "-s -w -X main.version=$VERSION" -o /out/$cmd ./cmd/$cmd; \
	done

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mayfly"]
