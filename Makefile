.PHONY: build test fmt-check lint dev reaper smoke long workers runners check-clean crash

# Same defaults as scripts/dev.sh; set the variables in the environment to override them.
MAYFLY_APP_ID ?= 5714458
MAYFLY_APP_KEY_FILE ?= $(HOME)/.config/mayfly/app.pem
DEMO_REPO ?= TeddyBertrand/mayfly-demo

build:
	go build ./...

test:
	go test ./...

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

lint: fmt-check
	golangci-lint run

dev:
	./scripts/dev.sh

reaper:
	MAYFLY_APP_ID=$(MAYFLY_APP_ID) MAYFLY_APP_KEY_FILE=$(MAYFLY_APP_KEY_FILE) go run ./cmd/reaper

# smoke runs the short demo job and follows it to the end.
smoke:
	gh workflow run mayfly-smoke -R $(DEMO_REPO)
	@sleep 3
	gh run watch -R $(DEMO_REPO) --exit-status $$(gh run list -R $(DEMO_REPO) --workflow mayfly-smoke --limit 1 --json databaseId --jq '.[0].databaseId')

# long starts the 10-minute demo job, used to crash mayfly mid-job.
long:
	gh workflow run mayfly-long -R $(DEMO_REPO)

# workers lists Mayfly containers with the labels the reaper reads.
workers:
	@docker ps -a --filter label=mayfly.job_id \
		--format 'table {{.ID}}\t{{.Status}}\t{{.Label "mayfly.runner_name"}}\t{{.Label "mayfly.owner"}}'

# runners lists the runner registrations GitHub holds for the demo repo.
runners:
	@gh api repos/$(DEMO_REPO)/actions/runners --jq '.runners[] | [.id, .name, .status, (.busy|tostring)] | @tsv'

# check-clean fails if any worker container or runner registration is left behind.
check-clean:
	@ids=$$(docker ps -aq --filter label=mayfly.job_id) || { echo "cannot reach Docker"; exit 1; }; \
	n=$$(printf '%s' "$$ids" | grep -c . || true); \
	r=$$(gh api repos/$(DEMO_REPO)/actions/runners --jq .total_count) || { echo "cannot reach GitHub"; exit 1; }; \
	echo "worker containers: $$n, runner registrations: $$r"; \
	[ "$$n" -eq 0 ] && [ "$$r" -eq 0 ]

# crash kills the mayfly started by make dev with SIGKILL, so it cannot clean up: its workers become orphans.
crash:
	pkill -9 -f 'mayfly-dev\.[A-Za-z0-9]+/mayfly$$'
