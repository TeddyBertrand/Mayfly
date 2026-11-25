# Mayfly: issue drafts

Format: `## Milestone`, then one `###` per issue as `### [labels] Title`, followed by the body.
All issues are on GitHub. Finished ones are marked **done** with their issue number.

## M0 - Foundations

### **done** [ci] Initialise Go module and repo layout (#1)
Set up `go mod init`, a `cmd/` + `internal/` layout (receiver, provisioner, reaper as packages or binaries), `.gitignore`, LICENSE and a README stub.
- [x] `go build ./...` passes
- [x] Layout documented in the README stub

### **done** [ci] Add linting and formatting (#2)
`golangci-lint` config, `gofmt` check, and a Makefile (or Taskfile) with `build`, `test`, `lint`.
- [x] `make lint test build` runs locally
- [x] Config committed

### **done** [ci] CI skeleton on GitHub-hosted runners (#3)
Workflow that runs lint, test and build on push and PR. It stays on hosted runners until Mayfly can host its own CI (M6).
- [ ] Workflow green on `main`
- [x] Go version pinned

### **done** [security] Create the GitHub App with least-privilege permissions (#4)
Create the app and subscribe it to `workflow_job` events. Grant the minimum permissions needed to generate JIT configs (repository "Administration: write" or the org self-hosted runners permission). Generate the private key and webhook secret.
- [x] App created and installed on the demo repo only
- [x] Permissions and the reason for each documented in `docs/github-app.md`
- [x] Private key and secret kept out of git

### [infra] Cloud account, budget alerts and dev host
Create the Hetzner project, add a budget/cost alert, pick a small instance size for development, and set up an SSH key.
- [ ] Budget alert active
- [x] Dev host reachable (or a documented decision to develop locally first)

Decided to develop locally first (design decision 002). The Hetzner project, budget alert and SSH key move to the start of M2, before the first `terraform apply`.

### **done** [docs] Decide and record the Go conventions (#6)
Short `docs/design-decisions.md` start: config via env vars, structured logging (`log/slog`), HTTP stack (`net/http`), the GitHub client library choice (`go-github` or raw REST).
- [x] First entries written

## M1 - Core loop

### **done** [receiver] Webhook HTTP server skeleton (#7)
`net/http` server with `/healthz` and `/webhook`, graceful shutdown, config from env.
- [x] Starts, serves `/healthz`, shuts down cleanly on SIGTERM

### **done** [receiver][security] Verify webhook signatures (#8)
Validate `X-Hub-Signature-256` using HMAC-SHA256 with a constant-time compare. Reject missing or invalid signatures with 401 before parsing the body.
- [x] Table-driven tests: valid, invalid, missing, tampered body
- [x] Body size limit applied

### **done** [receiver] Parse `workflow_job` events and dedupe deliveries (#9)
Parse the event types and handle only `queued` (others logged). Deduplicate on `X-GitHub-Delivery` (in-memory first, interface for a store later) so retries do not start two workers.
- [x] Same delivery twice starts one worker
- [x] Unknown events return 2xx and are ignored

### **done** [provisioner] GitHub API client with JIT config generation (#10)
Authenticate as the GitHub App (JWT then installation token) and call `generate-jitconfig` with a unique runner name and the right labels.
- [x] Returns an encoded JIT config for a test repo
- [x] Token caching and expiry handled
- [x] Runner name includes the job ID for traceability

### **done** [worker] Ephemeral Docker worker running `actions/runner` (#11)
Container image with the official runner. The provisioner starts it with the JIT config, it runs one job, then exits. The provisioner removes the container (`--rm` plus explicit cleanup).
- [x] Container is gone after the job finishes
- [x] Failed container start is logged with the job ID

### **done** [provisioner] Wire receiver to provisioner end to end (#12)
On `queued`, call the provisioner and start the worker. Use a job queue (channel) between them so the webhook returns quickly (under 1s).
- [x] A `workflow_job` on the demo repo runs on a Mayfly worker
- [x] Webhook response does not wait on provisioning

### **done** [docs] Local dev guide with a tunnel (#13)
Document how to run locally and receive webhooks (smee.io, cloudflared or ngrok).
- [x] Steps verified from a clean checkout

### **done** [infra] Set the App webhook URL automatically in local dev (#67)
A cloudflared quick tunnel gets a new random URL on every run, so the App webhook URL had to be pasted by hand each time. Use `PATCH /app/hook/config` with the App JWT to set it once the tunnel is up.
- [x] `make dev` sets the App webhook URL to the tunnel URL without manual steps
- [x] The previous URL is logged, and a failed update stops the script with a clear error
- [x] Covered by tests against a fake GitHub API

### **done** [ci] Release automatically on push to main from a VERSION file (#70)
Publish a GitHub Release whenever `main` is pushed with a version that has no tag yet. The version comes from a `VERSION` file bumped in the milestone PR (decision 010). M1 ships as v0.1.0.
- [x] Push to `main` with a new `VERSION` creates tag `v<VERSION>` and a GitHub Release with notes
- [x] Release has `mayfly` binaries for linux/amd64 and linux/arm64 plus `SHA256SUMS`
- [ ] Push to `main` without a version change publishes nothing
- [x] `mayfly --version` prints the release version
- [x] Dry run on a branch builds everything and publishes nothing

## M2 - Reliability and one-command deploy

### **done** [reaper] Reaper for orphaned workers (#14)
Periodic loop that lists workers (by label) and removes any that exceed a max age, or whose runner is offline on GitHub's side.
- [x] Killing the provisioner mid-job leaves no worker after one reaper cycle
- [x] Max age configurable

### **done** [reaper] Reaper for stuck jobs and stale runner registrations (#15)
Detect jobs stuck in `queued` with no worker, and delete stale runner registrations through the API.
- [x] Stale registrations are removed
- [x] Stuck job triggers a retry or is logged with a reason

### **done** [provisioner] Retries and backoff (#16)
Retry JIT config and container start with exponential backoff and jitter. Respect GitHub rate limits (`Retry-After`).
- [x] Injected failures in tests are retried and finally given up with a clear error

### **done** [provisioner] State handling for all `workflow_job` actions (#17)
Handle `in_progress`, `completed` and cancelled: mark worker state, clean up on `completed`, and drop the pending worker if the job is cancelled before it starts.
- [x] A worker state machine is documented and tested

### **done** [provisioner] Concurrency cap and queueing (#18)
Max concurrent workers from config. Excess jobs wait in a queue with a bounded size.
- [x] Cap enforced under a burst of 20 jobs

### **done** [infra] Docker Compose deploy (#80)
Package mayfly and the reaper as container images and run them with one `compose.yaml`, the single deploy unit used both locally and on the cloud host (decision 014). Secrets are Compose secrets mounted as files, matching the `*_FILE` settings. Both services reach the host Docker daemon through the socket, and workers attach to a dedicated `mayfly-workers` network. A `tunnel` profile adds cloudflared for local use. Images are published to GHCR by the release workflow.
- [x] `docker compose up` runs mayfly and the reaper, and a demo job runs on a worker
- [x] `docker compose down` leaves no worker containers or runner registrations behind
- [x] Secrets never appear in the image, the Compose file or `docker inspect` environment
- [x] Images are multi-stage, run as a non-root user, and have a `/healthz` healthcheck
- [x] A release pushes `ghcr.io/teddybertrand/mayfly:<version>` for amd64 and arm64
- [x] Local dev guide covers the Compose path next to `make dev`

### **done** [infra] Terraform: host, network and firewall (#19)
Provision the server, a firewall allowing only the webhook port and SSH from the user's IP, and a Docker network for workers with no route to internal resources.
- [x] `terraform apply` creates the host
- [x] Worker egress limited as documented

### **done** [ci] Release v0.2.0 (#83)
Publish v0.2.0 (decision 010) so the cloud host can pull a pinned image for #20. Contains the M2 work so far: reaper, retries, worker state machine, concurrency cap, Docker Compose deploy (#80) and the Azure host (#19). #20 follows as v0.2.1.
- [x] `VERSION` is `0.2.0` on `dev` and merged into `main`
- [x] GitHub Release `v0.2.0` with `mayfly` binaries for linux/amd64 and linux/arm64 plus `SHA256SUMS`
- [x] `ghcr.io/teddybertrand/mayfly:0.2.0` published for amd64 and arm64, and pullable by the host
- [x] `mayfly --version` in the image prints `0.2.0`

### **done** [infra] Terraform: bootstrap and secrets (#20)
Cloud-init installs Docker, applies the worker egress rules, and starts the Compose deploy with a pinned image version. Secrets (webhook secret, App key) are injected without being committed.
- [x] From a clean account, `terraform apply` yields a working system
- [x] `terraform destroy` removes everything

### **done** [ci] Release v0.2.1 (#87)
Publish v0.2.1 (decision 010) so `main` ships the one-command Azure deploy from #20 (Key Vault secrets, Caddy, cloud-init Compose deploy). Closes M2. Also marks the v0.2.0 release (#83) and the multi-arch image box of #80 done in the draft, and moves the Terraform default `mayfly_version` to `0.2.1`.
- [x] `VERSION` is `0.2.1` on `dev` and merged into `main`
- [x] GitHub Release `v0.2.1` with `mayfly` binaries for linux/amd64 and linux/arm64 plus `SHA256SUMS`
- [x] `ghcr.io/teddybertrand/mayfly:0.2.1` published for amd64 and arm64
- [x] Terraform default `mayfly_version` is `0.2.1`

## M2.5 - Refactor

Clean up package boundaries before M3 instruments them, so metrics code is written once against the final layout. No behaviour change unless stated: `go test ./...` passes without changing test assertions.

### **done** [refacto] Move `Job` and the `workflow_job` actions into a shared `job` package (#90)
`provisioner` and `reaper` import `receiver` only for `receiver.Job`, and the reaper builds a fake `receiver.Job` to call `MatchesLabels`. Move `Job` and `MatchesLabels` to `internal/workflow`, and make the actions (`queued`, `in_progress`, `completed`) typed constants instead of raw strings. The package is named `workflow`, not `job`, because a package named `job` is shadowed by the `job` variables that use it.
- [x] No package imports `receiver` except `cmd/mayfly`
- [x] No raw action strings outside `internal/workflow`

### **done** [refacto] Decouple the reaper from the Docker backend (#91)
`reaper.Workers` returns `docker.Worker`, so the reaper depends on one concrete backend. Move the worker inventory type and its list/remove interface into `internal/worker`, implemented by `worker/docker`, so a Firecracker backend (M7) plugs in without touching the reaper. The type is `worker.Info` and the interface `worker.Inventory`, next to `worker.Backend`.
- [x] `internal/reaper` does not import `internal/worker/docker`

### **done** [refacto] Split config into shared GitHub App and Docker settings (#92)
`ReaperConfig` embeds the whole `ProvisionerConfig`, so the reaper loads settings it never uses, and `config` imports `github` and `worker/docker` for defaults. Introduce shared `GitHubApp` and `Docker` sub-structs that each process composes, and give `provisioner.New` and `docker.New` option structs instead of long positional arguments. The shared defaults now live in `config`, and `setwebhook` loads only `LoadGitHubApp`.
- [x] The reaper only loads the settings it uses
- [x] `internal/config` imports no other internal package
- [x] No constructor takes two adjacent arguments of the same type

### **done** [refacto] Serve `/healthz` from a separate ops server (#93)
`/healthz` shares the public webhook router, and `receiver.Handoff` and `receiver.Webhook` repeat the same fields. Move `/healthz` to a small ops server on its own listener, which M3 reuses for `/metrics`. Behaviour change: new port, Compose healthcheck and reaper `HealthURL` updated. Record the choice in `docs/design-decisions.md`. Done as decision 018: `MAYFLY_OPS_ADDR` (default `127.0.0.1:8081`), and external checks expect 405 from `GET /webhook`.
- [x] Decision recorded
- [x] The webhook listener serves only `/webhook`
- [ ] Compose stack and Terraform deploy healthy with the new port (Compose checked locally with throwaway credentials; Terraform not run)

### **done** [refacto] No magic values, enforced by linters (#94)
Every number or string with a meaning gets a name, defined once: defaults, timeouts, limits, the `mayfly-` runner prefix, runner statuses, the GitHub API version, and duplicated helpers such as `shortID`. Go `const` covers scalars only, so fixed slices and maps stay unexported `var`s or functions returning copies. Enable `mnd` and `goconst` in `.golangci.yml`. Literals that stay: `0`, `1`, `""` in their usual roles, stdlib idioms such as `strconv.ParseInt(v, 10, 64)`, log and error messages, struct tags, test fixtures. Whether log attribute keys become constants is decided first. Log keys became constants in `internal/logkey`; the rules live in the new `docs/go-conventions.md`.
- [x] Convention documented in the Go conventions doc
- [x] `mnd` and `goconst` enabled, `golangci-lint run` clean

### **done** [docs] Test layout convention (#95)
Go visibility is per package and a package is a directory, so unit tests stay next to the code they test (`foo_test.go` beside `foo.go`) to reach unexported code. End-to-end tests that drive the system from outside, through its exported API or binary, go in a top-level `tests/` directory behind an `e2e` build tag so `go test ./...` stays fast. Readability of the source tree is handled in the editor (e.g. Zed `hidden_files`), not by moving tests. Recorded as decision 019; `tests/` and a `make e2e` target arrive with the first e2e test.
- [x] Decision recorded in `docs/design-decisions.md`
- [x] Convention documented in the Go conventions doc, including the build tag and how to run e2e tests

### **done** [docs] Link PRs to their milestone and issue (#98)
Every PR should carry the milestone of the issue it implements and show under that issue's "Development". Closing keywords only fill "Development" for PRs into the default branch (`main`), and GitHub has no API to link an existing PR, so PRs into `dev` stay unlinked. Checked on #99: a branch from `gh issue develop` is listed under "Development" but is not promoted to a PR link, and the sidebar does not offer PRs into `dev` either. So create each branch from its issue with `gh issue develop` so the issue lists the branch, set the milestone when opening the PR, and let the PR point to the issue through `Closes #<number>`. `dev` stays a non-default branch so the repo's front page shows the released `main`. Document all of it in `CLAUDE.md` sections 6 and 7.
- [x] `CLAUDE.md` section 6 creates branches with `gh issue develop`
- [x] `CLAUDE.md` section 7 requires the milestone, explains what "Development" shows, and lists the checks to run after opening
- [x] The PR for this issue shows the milestone, and the issue lists its branch under "Development"

### **done** [ci] Release v0.2.5 (#105)
Publish v0.2.5 (decision 010) so `main` ships the M2.5 refactor: shared `workflow` package, reaper decoupled from Docker, config split, the ops listener on port 8081 (decision 018), no magic values, and the test layout (decision 019). Moves the Terraform default `mayfly_version` to `0.2.5` in the same change, because Terraform deploys the working-tree `compose.yaml`, which now points the reaper at port 8081, a port only the 0.2.5 image opens. Also marks the v0.2.1 release (#87) done in the draft, checked against the published release, the image manifest and `mayfly --version`.
- [x] `VERSION` is `0.2.5` on `dev` and merged into `main`
- [x] GitHub Release `v0.2.5` with `mayfly` binaries for linux/amd64 and linux/arm64 plus `SHA256SUMS`
- [x] `ghcr.io/teddybertrand/mayfly:0.2.5` published for amd64 and arm64
- [x] `mayfly --version` in the image prints `0.2.5`
- [x] Terraform default `mayfly_version` is `0.2.5`

## M3 - Observability and baseline

### **done** [observability] Prometheus metrics in the receiver, provisioner and reaper (#21)
Expose `/metrics`: queue time, boot time, job duration, workers alive, jobs by result, retries, reaped workers.
- [x] Metrics documented with names and units

### **done** [observability] Prometheus and Grafana deployment (#22)
Run both with Docker Compose or Terraform-managed config on the host.
- [x] Both come up with the system
- [x] Dashboard JSON provisioned from the repo

### **done** [observability] Grafana dashboard (#23)
Panels: queue time, boot time, job duration, workers alive, failure rate. A cost-per-job panel is added in M6.
- [x] Dashboard shows live data from a test run

### **done** [ci] Release v0.2.6 (#111)
Publish v0.2.6 (decision 010) so `main` ships the observability half of M3: Prometheus metrics in mayfly and the reaper (#21, decision 020), Prometheus and Grafana in the Compose deploy (#22, decision 021) and the Mayfly dashboard (#23). Moves the Terraform default `mayfly_version` to `0.2.6` in the same change, because Terraform deploys the working-tree `compose.yaml`, which now runs Prometheus against `/metrics` endpoints only the 0.2.6 image serves. Also marks the v0.2.5 release (#105) done in the draft, checked against the published release, the image manifest and `mayfly --version`.
- [ ] `VERSION` is `0.2.6` on `dev` and merged into `main`
- [ ] GitHub Release `v0.2.6` with `mayfly` binaries for linux/amd64 and linux/arm64 plus `SHA256SUMS`
- [ ] `ghcr.io/teddybertrand/mayfly:0.2.6` published for amd64 and arm64
- [ ] `mayfly --version` in the image prints `0.2.6`
- [x] Terraform default `mayfly_version` is `0.2.6`

### **done** [bench] Demo repo with benchmark workflows (#24)
Create a demo repo with realistic workflows (e.g. Go build and test, a Docker build, an npm install and test), all runnable on either hosted runners or Mayfly with one label change.
- [x] Workflows run on both runner types

`go-build` and `npm-test` ship; the canonical copy is in `bench/demo/` (`docs/demo-repo.md`). The Docker build is deferred: workers have no Docker daemon (decision 015), so it would only pass on hosted runners.

### **done** [bench] Benchmark harness and methodology (#25)
Script that triggers N runs, collects timings through the API (queue time, first-step time, total time), and outputs CSV plus a summary. Document warm-up, run count, and how medians are computed.
- [x] Methodology in `docs/benchmarks.md`
- [x] Re-running gives results within a stated variance

### [bench] Baseline results (#26, closed, superseded by #116)
Run the harness on GitHub-hosted runners and on unoptimized Mayfly. Record both in the benchmark table.
The first attempt on the Azure host was stopped after 25 of 48 jobs, too little to publish; the full run moved to #116.
- [ ] Table committed with date, host size and commit SHA

### **done** [bench] Azure baseline on the 2 vCPU host (#116)
Two full runs (`azure-a`, `azure-b`) of `hosted,mayfly` on `Standard_B2als_v2`, about 2 hours on the VM. Compare the runs, check the VM's CPU credits, explain the slow `go-build` on Azure, then record the baseline table.
- [x] Two full runs committed in `bench/results/`
- [x] `bench compare` result between the two runs recorded
- [x] CPU credit metric of the VM checked during the runs and the result noted
- [x] Cause of the slow `go-build` explained, or listed as open
- [x] Baseline table in `docs/benchmarks.md` and the README with date, host size, Mayfly image version and commit SHA

## M4 - Optimization: startup

### [worker] Pre-baked worker image
Image with common toolchains (Go, Node, Python) and frequently used Docker images already pulled. Reproducible build, versioned tags.
- [ ] Boot-to-first-step time measured before and after

### [worker] Warm pool of pre-started workers
Keep N idle workers registered and waiting, claim one per job, and refill in the background. Must respect the one-job-per-worker rule and security isolation.
- [ ] Pool size configurable
- [ ] A claimed worker is never reused

### [provisioner] Pool sizing and scale-down
Simple policy for pool size (fixed minimum plus burst) and idle timeout so the host is not wasting resources.
- [ ] Idle workers are retired after the timeout

### [observability] Pool metrics
Add pool size, claim latency and pool-miss metrics to the dashboard.
- [ ] Panels added

### [bench] Benchmark: image and warm pool
Run the harness for each step on its own and combined, and add rows to the table.
- [ ] First step starts in under 10s (or the gap is explained)

## M5 - Optimization: caching and disk

### [infra] Local cache server for `actions/cache`
Deploy a cache backend reachable only from workers. Account for the cache protocol version used by the current runner.
- [ ] Cache hit demonstrated on the demo repo

### [security] Cache isolation per repo and branch
Scope cache keys by repo and branch (with a restricted fallback to the default branch) to prevent poisoning between repos or from PR branches.
- [ ] A test shows a PR branch cannot write to the default-branch cache

### [infra] Registry mirror
Pull-through mirror for Docker Hub (and ghcr.io if useful) configured on the worker Docker daemon.
- [ ] Image pull time measured before and after

### [infra] Package mirror or proxy
Go module proxy and npm/pip mirror (e.g. Athens or Verdaccio, or a caching proxy).
- [ ] Dependency install time measured before and after

### [worker] tmpfs for hot directories
Mount `_work`, the Go build cache and `/tmp` on tmpfs, with a memory limit.
- [ ] Measured gain and memory cost recorded

### [bench] Benchmark: cache, mirror and tmpfs
Update the table and compare against the 30-50% target versus hosted runners.
- [ ] Final numbers for each optimization step

## M6 - Hardening, docs and demo

### [security] Threat model document
`docs/threat-model.md`: assets, trust boundaries, threats (fork PRs, runner escape, cache poisoning, secrets exposure, webhook spoofing) and mitigations.
- [ ] Each threat maps to a mitigation or an accepted risk

### [security] Fork PR protection
Enforce private repos or trusted PRs only. Ignore or refuse jobs from untrusted forks.
- [ ] Test with a fork PR confirms no worker starts

### [docs] Design decisions document
Why Go, why JIT, why Docker first, why this cache design, and what was left out.
- [ ] Reviewed for accuracy against the code

### [observability] Cost-per-job panel
Compute infrastructure cost divided by jobs run and show it on the dashboard.
- [ ] Hourly price configurable

### [bench] 24h soak test
Run a steady stream of jobs for 24h and check for orphans, leaks and memory growth.
- [ ] Zero orphaned workers at the end

### [ci] Run this project's own CI on Mayfly
Switch the project workflows to the Mayfly runner (keeping a hosted-runner fallback for the deploy job).
- [ ] Recent runs visible on Mayfly runners

### [docs] README with diagram, quick start and benchmark table
- [ ] Quick start verified on a clean machine
- [ ] Benchmark table with methodology link

### [docs] Demo GIF or video (2 minutes)
- [ ] Embedded in the README

## M7 - Stretch: Firecracker

### [stretch] Spike: run a job inside a Firecracker microVM
Feasibility check on the host (KVM available, kernel, rootfs).
- [ ] Go/no-go decision recorded

### [stretch] Firecracker worker backend behind the worker interface
- [ ] Backend selectable by config; jobs run in microVMs

### [stretch] Snapshot and restore for fast boot
- [ ] Restore time measured

### [stretch] Benchmark against the Docker backend
- [ ] Results added to the table and the design-decisions doc
