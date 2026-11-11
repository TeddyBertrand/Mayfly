.PHONY: build test fmt-check lint

build:
	go build ./...

test:
	go test ./...

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

lint: fmt-check
	golangci-lint run
