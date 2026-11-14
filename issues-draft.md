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
- [ ] A release pushes `ghcr.io/teddybertrand/mayfly:<version>` for amd64 and arm64
- [x] Local dev guide covers the Compose path next to `make dev`

### **done** [infra] Terraform: host, network and firewall (#19)
Provision the server, a firewall allowing only the webhook port and SSH from the user's IP, and a Docker network for workers with no route to internal resources.
- [x] `terraform apply` creates the host
- [x] Worker egress limited as documented

### [ci] Release v0.2.0 (#83)
Publish v0.2.0 (decision 010) so the cloud host can pull a pinned image for #20. Contains the M2 work so far: reaper, retries, worker state machine, concurrency cap, Docker Compose deploy (#80) and the Azure host (#19). #20 follows as v0.2.1.
- [ ] `VERSION` is `0.2.0` on `dev` and merged into `main`
- [ ] GitHub Release `v0.2.0` with `mayfly` binaries for linux/amd64 and linux/arm64 plus `SHA256SUMS`
- [ ] `ghcr.io/teddybertrand/mayfly:0.2.0` published for amd64 and arm64, and pullable by the host
- [ ] `mayfly --version` in the image prints `0.2.0`

### **done** [infra] Terraform: bootstrap and secrets (#20)
Cloud-init installs Docker, applies the worker egress rules, and starts the Compose deploy with a pinned image version. Secrets (webhook secret, App key) are injected without being committed.
- [x] From a clean account, `terraform apply` yields a working system
- [x] `terraform destroy` removes everything

## M3 - Observability and baseline

### [observability] Prometheus metrics in the receiver, provisioner and reaper
Expose `/metrics`: queue time, boot time, job duration, workers alive, jobs by result, retries, reaped workers.
- [ ] Metrics documented with names and units

### [observability] Prometheus and Grafana deployment
Run both with Docker Compose or Terraform-managed config on the host.
- [ ] Both come up with the system
- [ ] Dashboard JSON provisioned from the repo

### [observability] Grafana dashboard
Panels: queue time, boot time, job duration, workers alive, failure rate. A cost-per-job panel is added in M6.
- [ ] Dashboard shows live data from a test run

### [bench] Demo repo with benchmark workflows
Create a demo repo with realistic workflows (e.g. Go build and test, a Docker build, an npm install and test), all runnable on either hosted runners or Mayfly with one label change.
- [ ] Workflows run on both runner types

### [bench] Benchmark harness and methodology
Script that triggers N runs, collects timings through the API (queue time, first-step time, total time), and outputs CSV plus a summary. Document warm-up, run count, and how medians are computed.
- [ ] Methodology in `docs/benchmarks.md`
- [ ] Re-running gives results within a stated variance

### [bench] Baseline results
Run the harness on GitHub-hosted runners and on unoptimized Mayfly. Record both in the benchmark table.
- [ ] Table committed with date, host size and commit SHA

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
