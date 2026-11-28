# Design decisions

Each entry records one decision, the options considered and the consequences. Entries are numbered in order. A replaced decision is marked `superseded by NNN`, not deleted.

## 001. Binary layout: one binary, separate packages
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Mayfly has three components (receiver, provisioner, reaper). The repo layout had to be fixed before any code exists. The brief targets a single-command deploy (`terraform apply`) on one small host, and M1 plans an in-process job queue (a channel) between receiver and provisioner.
- **Options considered:**
  - **A. One binary** (`cmd/mayfly`, logic in `internal/receiver`, `internal/provisioner`, `internal/reaper`): one file and one systemd service to deploy, and components talk through channels. Cannot scale or fail per component.
  - **B. Three binaries** (`cmd/receiver`, `cmd/provisioner`, `cmd/reaper`): independent scaling and failure isolation, but needs IPC or a broker between components and three services to install and supervise.
- **Decision:** A. One binary, with each component in its own `internal/` package behind clear boundaries.
- **Consequences:** Simpler deploy and a cheap, fast queue between receiver and provisioner. A crash in one component takes down the whole process, so shutdown and panic handling must be done carefully. Revisit if the components ever need to run on different hosts or scale independently; the package boundaries keep a later split cheap.

## 002. Develop locally first, cloud host later
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Issue #5 asks for a cloud account, a cost alert and a dev host. M1 (receiver, provisioner, Docker worker) only needs Docker and a way for GitHub to reach the webhook endpoint, which a tunnel provides (M1 "Local dev guide with a tunnel"). The real host is built in M2 by Terraform, and the brief flags cloud cost overruns as a risk.
- **Options considered:**
  - **A. Develop locally first:** run Mayfly and its Docker workers on the owner's machine, exposed through a tunnel. Costs nothing and keeps iteration fast, but the cloud environment isn't tested until M2.
  - **B. Hetzner dev host now:** a small cloud server set up by hand. Gives a real environment early, but costs a few euros a month and produces a hand-built server that the M2 Terraform work replaces.
- **Decision:** A. Develop locally until the M2 Terraform issues.
- **Consequences:** No cloud spend during M0 and M1. Code must not assume a cloud host (paths, public IP); configuration comes from environment variables. The Hetzner project, cost alert and SSH key are created at the start of M2, before the first `terraform apply`. Revisit if local Docker can't reproduce something M1 needs, for example the network or resource limits of the target host.

## 003. Configuration from environment variables
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Every component needs settings (listen address, GitHub App ID, secrets, limits). The same binary must run locally (decision 002) and on the Terraform-built host in M2 without code changes.
- **Options considered:**
  - **A. Environment variables, read with the standard library:** one small `config` package reads them once at startup and fails fast on missing or invalid values. No dependency, but parsing and validation are written by hand.
  - **B. A config library (`viper`, `envconfig`):** struct tags, files and flags for free. Adds a dependency and far more features than Mayfly needs.
- **Decision:** A. Environment variables, parsed by a `config` package in `internal/`, prefixed `MAYFLY_`.
- **Consequences:** Config follows the twelve-factor rule and maps directly to systemd `Environment=` lines and Terraform. Secrets are passed as file paths (e.g. `MAYFLY_APP_KEY_FILE`), not values, so they never appear in process listings or crash dumps. Revisit if config grows nested enough to need a file.

## 004. Structured logging with `log/slog`
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Debugging a job's lifecycle across receiver, provisioner and reaper requires logs that can be filtered by fields such as job ID and delivery ID, and later shipped to the observability stack.
- **Options considered:**
  - **A. `log/slog` (standard library, Go 1.21+) with the JSON handler:** structured key/value output, no dependency. Slightly slower than specialised libraries.
  - **B. `zap` or `zerolog`:** faster and allocation-free, at the cost of a dependency. The speed only matters at volumes far above Mayfly's.
- **Decision:** A. `log/slog`, JSON to stdout, one logger created in `main` and passed to components.
- **Consequences:** Logs are machine-readable and can be correlated by `job_id` and `delivery_id`. Secrets and tokens must never be logged as attributes. Revisit only if logging shows up in a profile.

## 005. HTTP server on `net/http`
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** The receiver exposes a few endpoints (webhook, health check, metrics). Webhook signature verification (HMAC-SHA256) needs the exact raw request body.
- **Options considered:**
  - **A. Standard library `net/http`:** Go 1.22+ routing supports methods and path patterns (`POST /webhook`). No dependency, full control over body reading, but middleware is written by hand.
  - **B. A router or framework (`chi`, `gin`):** route groups and middleware helpers. Adds a dependency, and frameworks that bind or parse bodies make raw-body access easier to get wrong.
- **Decision:** A. `net/http` with `http.ServeMux`, explicit server timeouts and a request body size limit.
- **Consequences:** No dependency and a clear path from request to signature check. Small middleware (logging, panic recovery) is written in-house. Revisit if the API grows beyond a handful of routes.

## 006. GitHub API through raw REST
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Mayfly calls about four GitHub endpoints: create an installation access token, `generate-jitconfig`, and list and delete self-hosted runners. It authenticates as a GitHub App: a short-lived JWT signed with the App's private key (RS256) is exchanged for an installation token that expires after one hour.
- **Options considered:**
  - **A. Raw REST on `net/http`:** a small client in `internal/`, JWT signed with `crypto/rsa`. No dependency and a fully explainable auth flow. Request/response types, token caching and refresh are written and tested by hand.
  - **B. `go-github` plus `ghinstallation`:** typed, maintained client with App auth and token refresh handled. Two dependencies, frequent major versions to track, and the auth flow is hidden.
- **Decision:** A. Raw REST with a hand-written App authentication.
- **Consequences:** The client stays small and every request is visible, which suits a project whose owner must explain it. Correctness of the JWT, token expiry and API error handling (rate limits, 4xx/5xx) is ours to test. Revisit if the number of endpoints grows well past a dozen.

## 007. Filter `workflow_job` events by runner label in the receiver
- **Date:** 2026-11-12
- **Status:** accepted
- **Context:** GitHub sends a `workflow_job` event for every job in the repositories where the App is installed, including jobs for GitHub-hosted runners (`runs-on: ubuntu-latest`). Without a filter, Mayfly would request a worker for jobs it must never run, and those workers would register and then sit idle until reaped, wasting money.
- **Options considered:**
  - **A. Filter in the receiver (#9):** accept a queued job only if its `labels` contain every label in `MAYFLY_RUNNER_LABELS` (comma-separated, default `mayfly`), compared case-insensitively as GitHub does. Other jobs get a 2xx and a log line. Decides "is this for us?" at the entry point, before any state or API call, at the cost of a little more logic in #9.
  - **B. Defer to the provisioner (#10/#12):** the receiver treats every queued job as ours, and the provisioner filters when it sets runner labels. Keeps #9 minimal, but puts the decision after dedupe and queueing, and leaves a window where wrong jobs flow through the pipeline.
- **Decision:** A. The receiver filters on `MAYFLY_RUNNER_LABELS`; `self-hosted` is not required, so `runs-on: mayfly` and `runs-on: [self-hosted, mayfly]` both match.
- **Consequences:** Only jobs Mayfly should run reach the deduper and the provisioner. The JIT runners created in #10 must register with at least these labels, or GitHub will not assign them the job. A job that asks for an extra label Mayfly's runners lack (e.g. `gpu`) still passes the filter and would wait unassigned; revisit by also checking that job labels are a subset of the runner's labels if that case appears.

## 008. Docker through the raw Engine API
- **Date:** 2026-11-12
- **Status:** accepted
- **Context:** The provisioner must create, start, wait for and remove one container per job (#11). The Docker daemon exposes a REST API on a Unix socket (`/var/run/docker.sock`); the CLI and the SDK are both clients of it.
- **Options considered:**
  - **A. Raw Engine API with `net/http` over the Unix socket:** no dependency, native contexts and timeouts, HTTP status plus JSON errors. About 150 lines to write and test by hand.
  - **B. Shell out to the `docker` CLI:** least code, but needs the binary installed, errors are stderr text to parse, and cancellation means killing a process.
  - **C. Official Go SDK (`github.com/docker/docker/client`):** typed and maintained, but a large dependency tree for five endpoints, and the flow is hidden.
- **Decision:** A. A small engine client in `internal/worker/docker`, with the API version pinned to `v1.44`.
- **Consequences:** Same approach as decision 006, so both external systems are plain, visible HTTP. The wait is registered with `condition=removed` before start, so a container removed by `AutoRemove` cannot be missed. Error handling and the pull progress stream are ours to test. Revisit if Mayfly needs many more Docker features (networks, volumes, exec).

## 009. Official runner image, pinned
- **Date:** 2026-11-12
- **Status:** accepted
- **Context:** Each worker container needs the GitHub Actions runner. M4 will add a pre-baked image with toolchains, so M1 only needs a working, trustworthy base.
- **Options considered:**
  - **A. `ghcr.io/actions/actions-runner`, pinned to an exact release:** maintained by GitHub, nothing to build. Contains only the runner and minimal tools.
  - **B. Own Dockerfile from `ubuntu` with the runner tarball:** full control over contents, but we track runner releases and checksums ourselves.
- **Decision:** A. `ghcr.io/actions/actions-runner:2.338.2`, overridable for M4. The JIT config is passed as `ACTIONS_RUNNER_INPUT_JITCONFIG`, not `--jitconfig`, so it never appears in the host's process list; this was checked against the real image.
- **Consequences:** No image to build or publish in M1, and M4's pre-baked image builds `FROM` it. The pin must be bumped by hand; GitHub stops accepting very old runner versions, so a stale pin eventually breaks job pickup. The runner exits 0 even when it fails, so the container exit code is logged but not used as a job result.

## 010. Releases from a `VERSION` file on push to `main`
- **Date:** 2026-11-12
- **Status:** accepted
- **Context:** `main` only moves when a milestone is merged from `dev`, so each milestone is a natural release. A release needs a version number, and something has to decide it on each push. Versions follow semver below 1.0: one minor version per milestone (M1 is v0.1.0), patch versions for fixes between milestones.
- **Options considered:**
  - **A. `VERSION` file:** the version is a line in the repo, bumped in the milestone PR. On each push to `main`, a workflow releases `v<VERSION>` if that tag does not exist yet. Automatic, reviewable, idempotent, no dependency.
  - **B. Tag-driven:** the owner pushes `v0.1.0` by hand and the workflow releases on the tag. Simple, but one manual step per release and easy to forget.
  - **C. release-please:** computes the version from commit types and opens a release PR. Fully derived, but adds a third-party action and bot commits typed `chore`, which the commit rules do not allow, and does not know the `refacto` type.
- **Decision:** A. A `VERSION` file and a release workflow on push to `main`, with a manual dry run.
- **Consequences:** Releasing a milestone means bumping `VERSION` in the `dev` to `main` PR; forgetting it publishes nothing, which is safe. A push to `main` without a bump is a no-op. The version is stamped into the binary at build time. Release notes come from GitHub's generated list of merged PRs. Revisit if releases need signing or container images.

## 011. Worker state kept in memory
- **Date:** 2026-11-13
- **Status:** accepted
- **Context:** #17 tracks each job's worker through `waiting`, `provisioning`, `ready`, `running`, `done` and `dropped` (see `docs/state-machine.md`), so that `in_progress` and `completed` events can update it and a cancelled job can stop its worker. That state has to live somewhere.
- **Options considered:**
  - **A. In memory:** a map from job ID to state, guarded by a mutex, inside the provisioner. No dependency, simple and fast. Lost on restart.
  - **B. SQLite or a state file:** survives a restart. Adds a dependency and on-disk state to manage, and does not help much, since a restarted process cannot reattach to running containers anyway.
- **Decision:** A. In-memory state, matching the in-memory deduper.
- **Consequences:** A restart forgets every job. Containers left behind are found by their `mayfly.job_id` label and removed by the reaper (#14), and stale runner registrations are deleted by #15. Revisit if Mayfly runs as several replicas, which would need shared state.

## 012. Orphaned workers detected by owner instance ID
- **Date:** 2026-11-13
- **Status:** accepted
- **Context:** #14 must remove workers nobody manages any more, including after the provisioner is killed mid-job. In that case the container keeps running and its runner stays online and busy on GitHub, so neither a max age nor a GitHub status check notices it. The reaper is a separate binary and needs a way to tell whether the process that started a worker is still alive.
- **Options considered:**
  - **A. Owner instance ID:** mayfly picks a random ID at boot, labels each container with it (`mayfly.owner`) and reports it on `/healthz`. The reaper removes containers whose owner is not the live instance. A restart is caught in one cycle; an unreachable owner only counts after several missed probes in a row. No dependency.
  - **B. Let it finish:** treat a crash as harmless, since the container ends with its job and `AutoRemove` cleans it up. Kind to the job, but leaves unmanaged workers and fails the acceptance criterion.
  - **C. Startup sweep:** mayfly removes every labelled container when it starts. Simple, but does nothing if mayfly never comes back.
  - **D. Lease file:** mayfly touches a file every few seconds. Ties both processes to a shared path and cannot tell a restarted process from the old one.
- **Decision:** A, combined with a max age (`MAYFLY_WORKER_MAX_AGE`, must exceed the worker timeout) and a GitHub check (registration gone, or runner offline after a grace period).
- **Consequences:** A job running when mayfly restarts is killed and fails on GitHub, so it must be rerun; that is the price of never leaving unmanaged workers. The reaper only removes on positive evidence: a GitHub error or a single missed probe keeps the worker for the next cycle. The reaper needs the GitHub App key, with the same permissions as the provisioner. Containers from versions before this decision have no owner label and are removed as orphans.

## 013. Stuck jobs are logged, not retried
- **Date:** 2026-11-13
- **Status:** accepted
- **Context:** #15 must handle a mayfly job stuck in `queued` with no worker, for example one that was in mayfly's in-memory queue when it crashed. The reaper is a separate process: it can see the job on GitHub, but it cannot put it back in mayfly's queue.
- **Options considered:**
  - **A. Log with a reason:** log the job ID, URL, age and a reason (`mayfly unreachable`, or `not picked up` when mayfly is up), once per job. No new moving parts.
  - **B. Redeliver the webhook:** ask GitHub to resend the original `queued` delivery, which then goes through the normal signed path. Needs a search through recent deliveries for the job, and the receiver's dedupe drops it if mayfly still remembers that delivery ID, so it only helps after a restart.
  - **C. Internal enqueue endpoint:** the reaper calls a mayfly endpoint to re-enqueue the job. Most direct, but adds an attack surface and a shared secret between the two processes.
- **Decision:** A. Retrying can be added once real runs show how often jobs get stuck and why.
- **Consequences:** A stuck job needs a human to rerun or redeliver it. The log line gives everything needed to do that. Detecting stuck jobs costs one listing of queued runs, plus one call per queued run, per repository and sweep.

## 014. Azure for Students host, created on demand
- **Date:** 2026-11-13
- **Status:** accepted (replaces the Hetzner assumption in decision 002 and the brief)
- **Context:** M2 needs a real host built by `terraform apply`. The owner has an Azure for Students subscription: $100 of credit for 12 months, no card, and a hard spending limit that disables resources when the credit runs out. Hetzner would cost real money (about €5 per month for the server, about €5 more for an S3 bucket to hold remote state). The host is only needed for benchmarks and CI-heavy sprints.
- **Options considered:**
  - **A. Hetzner, local state:** simplest resource model (server, firewall, SSH key), but paid and state lives on one laptop.
  - **B. Azure, always on:** free to the owner, but a 4 GB VM (about $30 per month) uses up the yearly credit in about three months.
  - **C. Azure, on demand:** the host exists only while in use. `terraform destroy` after each session, and `apply` rebuilds it in minutes. Default size is the free-tier `Standard_B2ats_v2`, with a bigger size passed as a variable on benchmark days.
- **Decision:** C. Terraform state lives in Azure Blob Storage (`azurerm` backend, with locking) in a storage account created once by an idempotent `az` script (`infra/bootstrap/bootstrap.sh`), so the state has no state of its own to lose. Locally, Terraform authenticates through the Azure CLI login; no service principal until CI runs Terraform.
- **Consequences:** More resources to manage than Hetzner (resource group, VNet, subnet, NSG, public IP, NIC, VM). The deploy must be fully reproducible, because the host is rebuilt often. Docker Compose is the deploy unit used locally day to day; Terraform only builds the host, and cloud-init runs the same Compose file there. The webhook URL changes with each new public IP unless it gets a DNS name. The Azure metadata service at 169.254.169.254 must be blocked for workers, like every internal range. Firecracker (stretch goal) needs nested virtualization, which B-series sizes lack. Revisit if the credit runs out or the subscription type changes.

## 015. Container packaging for the Compose deploy
- **Date:** 2026-11-13
- **Status:** accepted
- **Context:** #80 runs mayfly and the reaper with Docker Compose, locally and on the cloud host. Both are static Go binaries that need the host Docker socket, and the images must run as non-root and be published from the release workflow.
- **Options considered:**
  - **Base image:** `distroless/static` (CA certs, tzdata, a nonroot user, no shell), `scratch` (empty, certs and user added by hand), or `alpine` (shell for debugging, more attack surface).
  - **Image count:** one image holding both binaries, or one image per binary.
  - **Socket access:** add the container user to the host's docker group by GID, run as root, or put a socket proxy in front of the daemon.
  - **Publishing:** Docker's official GitHub Actions, or plain `docker buildx` commands in `run:` steps.
- **Decision:** `distroless/static`, one image with both binaries (Compose picks the command), `group_add` with the host docker GID from a `DOCKER_GID` variable, and Docker's official actions to push multi-arch images to GHCR.
- **Consequences:** No shell in the image, so debugging uses `docker debug` or the logs. mayfly and the reaper always ship at the same version. `DOCKER_GID` must be set per host (`getent group docker`), and cloud-init computes it. Socket access is still root-equivalent on the host: non-root limits what a bug in mayfly can do outside the API, not through it. Revisit with a socket proxy if mayfly ever runs untrusted input closer to the daemon.

## 016. Cloud host region, webhook TLS and worker egress
- **Date:** 2026-11-13
- **Status:** accepted
- **Context:** #19 builds the Azure host. The subscription policy allows five regions, but only `denmarkeast` offers B-series sizes to this subscription, with quota for `Basv2` (10 vCPU) and `BS` (4 vCPU) under a 6 vCPU regional cap; D v5 sizes have 0 quota. GitHub must reach the webhook from the internet, and workers run untrusted job code next to the host and Azure's internal endpoints.
- **Options considered:**
  - **Region:** `denmarkeast`, or `belgiumcentral` (only D v4, no free tier).
  - **Webhook transport:** HTTPS through Caddy with an automatic Let's Encrypt certificate for the public IP's Azure DNS name (ports 80 and 443), plain HTTP on 8080, or a cloudflared tunnel with no inbound port.
  - **Egress enforcement:** host `iptables` rules on the worker bridge, installed by a minimal cloud-init in #19, or deferred to #20. An NSG cannot tell container traffic from host traffic and never sees traffic to 169.254.169.254 or 168.63.129.16.
  - **SSH source address:** a `ssh_allowed_cidr` variable, or auto-detection through an external "what is my IP" service.
- **Decision:** `denmarkeast`; HTTPS through Caddy (added to the Compose deploy in #20); `iptables` rules in the `DOCKER-USER` and `INPUT` chains for the `mayfly-workers` bridge, applied by a systemd unit from cloud-init in #19; SSH allowed only from `ssh_allowed_cidr`.
- **Consequences:** The NSG opens 22 (one address), 80 and 443. The HMAC signature still authenticates deliveries; TLS adds confidentiality. Workers can reach the internet but not the host, the VNet, private ranges, link-local addresses (instance metadata) or the Azure wire server (168.63.129.16); DNS keeps working because Docker's embedded resolver forwards from the host namespace. The DNS name stays the same across rebuilds, so the App webhook URL is set once. Firecracker needs a D v5 quota request first. The SSH rule must be updated when the owner's IP changes.

## 017. Deploy secrets in Key Vault, read by the VM's managed identity
- **Date:** 2026-11-14
- **Status:** accepted
- **Context:** #20 starts the Compose deploy from cloud-init, which needs the webhook secret and the GitHub App private key on the host. The host is destroyed and rebuilt often (decision 014), and the secrets must never be committed, stored in Terraform state, or need re-entering for each rebuild.
- **Options considered:**
  - **A. Key Vault and managed identity:** a vault created once by `bootstrap.sh` next to the state storage; the owner uploads the secrets with `az keyvault secret set`; Terraform gives the VM a system-assigned identity and the `Key Vault Secrets User` role; cloud-init reads them with a token from the instance metadata service.
  - **B. Inline in custom_data:** Terraform reads local files into the cloud-init payload. Simplest, but the secrets land in the state and in the VM model, readable by anyone with read access to the VM.
  - **C. SSH push after apply:** a second command copies the files and starts Compose. Nothing extra in Azure, but `apply` alone no longer yields a working system.
- **Decision:** A. The vault uses Azure RBAC, 7-day soft delete and no purge protection, and lives in `mayfly-tfstate-rg`, outside the Terraform state, so `destroy` never deletes it.
- **Consequences:** No secret value passes through Terraform, the state or custom_data. The identity and its role assignment die with the VM. Cloud-init retries for up to 10 minutes because a new role assignment takes minutes to apply. The metadata service hands out tokens for that identity, so blocking 169.254.0.0/16 for workers (decision 016) is now required, not just good practice. Rotating a secret means updating the vault and restarting `mayfly.service` or rebuilding the host. The vault costs a few cents a month.

## 018. Ops endpoints on a separate listener
- **Date:** 2026-11-14
- **Status:** accepted
- **Context:** #93. One listener on `:8080` serves `POST /webhook`, which must be reachable from the internet, and `GET /healthz`, which hands out the instance ID the reaper relies on (decision 012). M3 adds `/metrics`, which reveals queue sizes, failure counts and timings. On the cloud host `:8080` sits behind Caddy (decision 016), so anything on that listener is public. `/healthz` also serves as the external reachability check in `scripts/dev.sh`, `scripts/compose-dev.sh` and `infra/README.md`.
- **Options considered:**
  - **A. Full split:** the webhook listener serves only `POST /webhook`; an ops listener (`MAYFLY_OPS_ADDR`) serves `/healthz` and later `/metrics`, and is never published. External checks send `GET /webhook` and expect 405 from mayfly, since Caddy and cloudflared answer 502 or 530 when mayfly is unreachable. The 405 check is less obvious than a 200 and needs a comment.
  - **B. Ops listener plus a public ping:** the ops listener as in A, and a bare public `GET /healthz` answering `ok` for external checks. Intuitive checks, but the public listener serves more than the webhook, and two endpoints share one name.
  - **C. One listener, filtered by the proxies:** Caddy and the tunnel forward only `/webhook`. No new port, but `:8080` is also published on the host, so keeping `/metrics` private would rest on proxy config instead of on what listens.
- **Decision:** A. `MAYFLY_OPS_ADDR` defaults to `127.0.0.1:8081`; Compose sets `:8081` so the reaper can reach it, and never publishes it. Port 9090 is avoided because it is Prometheus's own port.
- **Consequences:** The internet-facing port does one job, and the instance ID is no longer public. The container healthcheck and the reaper's `MAYFLY_HEALTH_URL` move to port 8081. Prometheus scrapes 8081 over the Compose network in M3, with nothing new opened. Local runs outside Docker keep ops endpoints on loopback by default. External checks read "405 from `GET /webhook`" as "mayfly answered", which the scripts and docs must explain.

## 019. Unit tests beside the code, end-to-end tests in `tests/`
- **Date:** 2026-11-14
- **Status:** accepted
- **Context:** #95. The owner wanted tests out of the source folders, as in a C++ `tests/` tree, to make the code easier to read. In Go, visibility is per package and a package is a directory: only a `_test.go` file in the same directory can declare the same package and reach unexported code. All test files in the repo were white-box tests of unexported code (the tracker, the payload parser, the Docker engine client).
- **Options considered:**
  - **A. Tests beside the code, readability in the editor:** `foo_test.go` next to `foo.go`, as every Go project does; the editor hides or folds test files (Zed `hidden_files`, VS Code file nesting). No code change, and the tree only looks clean in a configured editor.
  - **B. A `tests/` folder for end-to-end tests only:** tests that drive the system from outside, through exported APIs or the built binary, live in `tests/` behind an `e2e` build tag. Fits new black-box tests, not the existing unit tests.
  - **C. Every test in `tests/`:** matches the C++ habit, but every white-box test would have to be deleted or the code would have to export internals just for tests, which makes it harder to read, not easier. It also breaks the convention Go reviewers expect.
- **Decision:** A and B together. Unit tests stay beside the code; end-to-end tests go in `tests/` with `//go:build e2e`.
- **Consequences:** `go test ./...` stays fast and never runs e2e tests; they run with `go test -tags e2e ./tests/...`. A `make e2e` target and the `tests/` folder arrive with the first e2e test, since `go test ./tests/...` fails while the folder does not exist. Tests that need a real external system but test internals (`internal/github/live_test.go`, `internal/worker/docker/docker_test.go`) stay beside their code and skip themselves unless enabled. `internal/provisioner/handoff_test.go`, which wires the real receiver to the provisioner, is the first candidate to move to `tests/`. Rules in `docs/go-conventions.md`.

## 020. Prometheus metrics with `client_golang`, one ops listener per process
- **Date:** 2026-11-15
- **Status:** accepted
- **Context:** #21. M3 needs queue time, boot time, job duration, workers alive, jobs by result, retries and reaped workers, scraped by Prometheus and graphed in Grafana. These numbers back the baseline benchmark. Until now `go.mod` had no third-party dependencies. Mayfly and the reaper are separate processes in separate containers (decision 001 and `docs/components.md`), and only mayfly has an ops listener (decision 018).
- **Options considered (library):**
  - **A. `prometheus/client_golang`:** the reference Go client. Correct histograms, label escaping and thread safety, plus Go runtime and process metrics for free. Brings about eight indirect dependencies (protobuf, procfs, ...).
  - **B. Hand-written text exposition format:** no dependency and teaches the format, but histograms, escaping and concurrency would have to be right by hand, which reinvents a solved problem.
  - **C. OpenTelemetry SDK with a Prometheus exporter:** vendor-neutral and ready for traces, but far heavier, with more concepts (meter providers, readers, exporters) than one host needs.
- **Options considered (reaper metrics):**
  - **A. Its own ops listener:** the reaper serves `/metrics` and `/healthz` on `MAYFLY_REAPER_OPS_ADDR`, scraped like mayfly.
  - **B. Pushgateway:** made for short batch jobs; for a long-running process it keeps stale values after a crash and adds a service.
  - **C. Report through mayfly:** breaks the rule that the cleaner must not depend on the process that may have failed.
- **Decision:** A and A. `client_golang`, and a separate ops listener for the reaper.
- **Consequences:**
  - Metric names, units and buckets live in one package, `internal/metrics`, so they are defined once and documented in one table (`docs/metrics.md`). Each process builds its own registry (no global `prometheus.DefaultRegisterer`) and hands the metrics to its components, so tests use a fresh registry.
  - Queue time and job duration come from GitHub's timestamps (`created_at`, `started_at`, `completed_at`), so they match what the benchmark harness reads through the API and do not depend on the host clock. Boot time is measured on mayfly's own clock, from accepting the `queued` event to the `in_progress` event of its runner: the part of queue time Mayfly controls, which M4 sets out to shrink.
  - The reaper's ops listener defaults to `127.0.0.1:8082`, because `make dev` and `make reaper` run both processes on one host and mayfly already holds 8081. Compose sets `:8082`, and neither port is published.

## 021. Prometheus and Grafana in the Compose deploy, config shipped by cloud-init, anonymous read-only Grafana
- **Date:** 2026-11-15
- **Status:** accepted
- **Context:** #22. Prometheus and Grafana must start with the system and load their dashboard from the repo. Terraform ships only `compose.yaml` to the VM, embedded in cloud-init, and Azure caps cloud-init data at 64 KB; a Grafana dashboard is often 20-50 KB of JSON. Grafana needs some login model, and the default VM has 1 GB of RAM (decision 014).
- **Options considered (config delivery):**
  - **A. cloud-init writes the files, gzipped:** each file under `infra/observability/` lands at the same path under `/opt/mayfly`, compressed with Terraform's `base64gzip()`. `compose.yaml` bind-mounts them with the same relative paths locally and on the VM.
  - **B. Custom images with the config baked in:** a simple VM, but two more images to build and publish in the release workflow.
  - **C. Compose `configs:` with inline content:** no Terraform change, but a dashboard embedded in YAML is unreadable and hard to review.
- **Options considered (Grafana login):**
  - **A. Anonymous read-only, loopback only:** Grafana is published on `127.0.0.1:3000` only and reached on the VM through an SSH tunnel. Anonymous users are Viewers; no password to manage. Locally `.env` can raise the role to Editor to build dashboards.
  - **B. Admin password as a secret:** conventional, but a new secret across `bootstrap.sh`, Key Vault, cloud-init and `.env`, for a page nobody outside the host can reach.
- **Options considered (memory):**
  - **A. Always on:** what "come up with the system" asks. Prometheus is capped by retention and a memory limit; benchmarks already use the 4 GB size.
  - **B. An `observability` profile:** can be left off on 1 GB, but one more switch to explain.
- **Decision:** A, A and A.
- **Consequences:** Prometheus is never published; it scrapes `mayfly:8081` and `reaper:8082` over the Compose network (decisions 018 and 020). The Grafana login form is disabled, so the dashboard is changed in the repo, not in the UI; anyone with shell or SSH access to the host can see it, which is the same trust boundary as `docker compose logs`. Metrics live in a named volume and are lost when the VM is destroyed (decision 014), so screenshots and benchmark numbers must be captured before `terraform destroy`. A changed file under `infra/observability/` reaches the VM only on a new `terraform apply` that recreates it, as `compose.yaml` already does.

## 022. Pre-baked worker image: toolchains in the runner tool cache
- **Date:** 2026-11-25
- **Status:** accepted
- **Context:** #27. Workers run the stock `actions-runner` image (decision 009, overridable with `MAYFLY_WORKER_IMAGE`) and start with an empty `_work/_tool`, so `setup-go` and `setup-node` download their toolchain on every job. The Azure baseline (`bench/results/2026-11-25-azure-*`) shows Mayfly's first step already near 10 s, but `go-build` totals 85 s against 46.5 s hosted, so the gap is in the steps and not in startup. The image must keep one job per worker with nothing shared between jobs, and must be reproducible with versioned tags.
- **Options considered:**
  - **A. Custom image `FROM` the runner image, toolchains and tools baked in:** pinned toolchains go into the tool cache and common tools are installed. Each worker gets a fresh copy of a read-only layer, so nothing is shared. A job that asks for a version that is not baked downloads it as before.
  - **B. Stock image plus a shared read-only tool-cache volume:** no image to build, but it is shared state between jobs and is populated outside the image, so it is less reproducible.
  - **C. Clone of the hosted `ubuntu-24.04` image:** any workflow works unchanged, but it is tens of GB, slow to build and pull, and far beyond what this project's jobs use.
- **Decision:** A, with a broad but bounded list chosen from the languages in the owner's repositories (TypeScript, Rust, Go, C++, C) and checked against GitHub's hosted-image software list. Starting list, to be adjusted: Go, Node (LTS and current), Rust stable with `clippy` and `rustfmt`, Python 3; `gcc`, `g++`, `clang`, `make`, `cmake`, `ninja`, `pkg-config`; `npm`, `pnpm`, `yarn`, `cargo`, `pip`; `git`, `curl`, `wget`, `jq`, `zip`, `unzip`, `tar`, `openssl`.
- **Consequences:**
  - Jobs that use a baked version skip the toolchain download. The image is a bet on common toolchains, not a promise that every job is faster; the long tail is left to the M5 cache and mirrors. Project packages (npm, Go modules, pip) and actions are not baked: they differ per repo and change with every lockfile.
  - Cost: the image is large (Rust and C++ toolchains), so size, build time and first-pull time are part of the result and are recorded in the benchmark next to the gain. The first pull on a fresh host is slower; the harness warm-up runs absorb it. Every baked toolchain must also be rebuilt and re-pinned when it ages.
  - The demo workloads only use Go and Node, so they cannot show a gain for Rust or C/C++. Proving that needs a Rust or C++ workload in `bench/demo`, which is a separate change.
  - Revisit if the image grows past what the host disk or pull time tolerates, or if the benchmark shows the download was not the main cost of the slow steps.

## 023. Worker image released with Mayfly, same version, contents in labels
- **Date:** 2026-11-25
- **Status:** accepted
- **Context:** #27. The worker image of decision 022 must be published with versioned tags. Decision 010 already releases `v<VERSION>` on push to `main` and publishes the Mayfly image once per tag. The worker image is much larger (about 2.8 GB) and its default name must not drift from what a release ships.
- **Options considered:**
  - **A. Same version as Mayfly, built by the release workflow:** `ghcr.io/teddybertrand/mayfly-worker:<VERSION>` and `:latest`, as a job next to the Mayfly image, under the same tag-exists check. One version to bump and one idempotent release. The tag does not show what is inside, and every release rebuilds and pushes the image.
  - **B. Its own tag from its contents** (for example `2.338.2-go1.27.4-node24.22.3`): self-describing, and it only changes with its contents. It needs a second release path and tag-exists check, a tag cannot capture the whole tool list without a revision suffix, and Mayfly still has to be told which tag to use.
  - **C. Immutable digest only:** exact and tamper-proof, but unreadable and copied into every config by hand or by tooling.
- **Decision:** A. The base runner version, the Go version and the Node version are recorded as image labels, so `docker inspect` shows what is inside without a second version scheme.
- **Consequences:**
  - The release workflow gets a worker image job for linux/amd64 and linux/arm64, with the same push condition as the Mayfly image (push to `main`, tag not yet existing), and Terraform's `mayfly_version` default keeps following `VERSION` as before. The default worker image can be derived from the binary's own version, as `main.version` is, so `DefaultWorkerImage` no longer needs a hand-synced tag; `MAYFLY_WORKER_IMAGE` still overrides it.
  - Costs: each release rebuilds and pushes about 2.8 GB (layer caching with `type=gha` softens it), and a toolchain bump needs a Mayfly release. The label values are build arguments, so they must stay equal to the pins in the Dockerfile.
  - Images are only pushed from `main`, so an Azure benchmark of the image alone has to wait for a release or for a manual publish path, such as a `workflow_dispatch` that pushes a pre-release tag. That is not decided here.
  - Revisit if releases become frequent enough that the image size slows them down, or if toolchain bumps need to ship between Mayfly releases.

## 024. Default worker image derived from the binary's version
- **Date:** 2026-11-25
- **Status:** accepted
- **Context:** #27. Decision 023 publishes `mayfly-worker:<VERSION>` with each release, but `DefaultWorkerImage` was a hard-coded constant, so a release would keep using the stock runner image unless someone remembered to change it. The binary already knows its release version (`main.version`, stamped at build time, `dev` otherwise), and the registry has no `mayfly-worker:dev`.
- **Options considered:**
  - **A. Derive from the version, `dev` falls back to the stock runner:** a release defaults to `ghcr.io/teddybertrand/mayfly-worker:<version>`, a development build keeps `actions-runner`. No tag to sync by hand, and `make dev` needs no image build. A local run differs from production unless `MAYFLY_WORKER_IMAGE` is set.
  - **B. Derive from the version, `dev` uses `mayfly-worker:dev`:** dev and release behave the same, but `make dev` needs a 2.8 GB local build first and fails slowly at the first job when it is missing.
  - **C. Hard-coded constant bumped each release:** the smallest code, but a pin to sync by hand, which decision 023 set out to remove.
  - **D. No default, `MAYFLY_WORKER_IMAGE` required:** nothing implicit, but it breaks `make dev` and moves the same sync problem into Compose and Terraform.
- **Decision:** A. `MAYFLY_WORKER_IMAGE` still overrides everything, and the chosen image is logged at startup.
- **Consequences:**
  - Terraform's `mayfly_version` picks the Mayfly image and, through its binary, the worker image, so the two cannot drift.
  - A local `make dev` runs the stock image, without the baked toolchains, unless `MAYFLY_WORKER_IMAGE=mayfly-worker:dev` is set after `docker build`; the startup log shows which one is in use.
  - The `classic` runner in `compose.yaml` still uses the stock image, so "same image as Mayfly's workers" in `docs/benchmarks.md` no longer holds; whether `classic` moves to the baked image is decided with the benchmark (#31).
  - A new ghcr.io package is private by default, so the deploy host may need access to pull `mayfly-worker` before the first benchmark.

## 025. Warm pool of fully started runners, per configured repository
- **Date:** 2026-11-26
- **Status:** accepted
- **Context:** #28. The image benchmark (#31) left startup unchanged: queue 6-7 s and first step 9-10 s, against 3 s and 5.5 s hosted. About 4 s of boot (JIT call, container start, runner start and connection to GitHub) runs after the `queued` webhook, on the critical path. JIT configs are repository-scoped (`/repos/{repo}/actions/runners/generate-jitconfig`), and the owner is a user account, which has no account-level runners. GitHub, not Mayfly, assigns a queued job to an idle runner with matching labels. Each JIT runner takes one job and is then removed, which must hold for warm workers too.
- **Options considered:**
  - **A. Fully warm runners per repository:** keep N containers per configured repository that are JIT-registered, started and connected, waiting for a job. Takes the whole boot off the critical path. Needs a repository list, and idle runners cost memory and hold a live registration while they wait.
  - **B. Pre-started containers that register on claim:** start containers with no credential and hand them a JIT config on `queued`. Works for any repository with no credential waiting, but still pays the JIT call and the runner's start and connection, and needs a new handoff mechanism in the worker image.
  - **C. Banked JIT configs only:** pre-register runners and start the container on `queued`. Smallest change and no idle memory, but saves only the API call, and the unconnected registrations show as offline runners.
- **Decision:** A. The pooled repositories come from configuration. On `queued`, a repository with an idle warm runner gets no cold worker and its pool is refilled in the background; a repository without one falls back to the cold path, counted as a pool miss.
- **Consequences:**
  - One job per worker still holds by construction: a warm runner is an ordinary JIT runner, so it runs one job and is deregistered.
  - Mayfly cannot claim a runner, only account for GitHub's choice. Refills must count the warm runners actually idle (matched by runner name on `in_progress`, as the tracker already does) rather than trust a counter decremented on `queued`.
  - Costs: a new setting listing repositories, idle memory per warm runner on a small host, and a registration that lives as long as the runner idles. The idle timeout of #29 is therefore a security limit as well as a cost one, and the reaper must treat an idle warm runner as expected rather than stale.
  - Warm runners run outside the `MAYFLY_WORKERS` goroutines, since an idle one holding a slot would starve cold jobs. The host can therefore run up to `MAYFLY_WORKERS` cold workers, plus the idle pool, plus warm runners busy with jobs; a cap on the total is settled in #29.
  - The worker timeout counts from container start, so a warm runner's idle time uses up its `MAYFLY_WORKER_TIMEOUT` until #29 adds an idle timeout.
  - Revisit if the pooled repositories grow past what the host can keep idle, or if the benchmark shows the runner's connection was not the main cost of boot.

## 026. Warm pool sized by a fixed minimum and a total cap, idle runners rotated
- **Date:** 2026-11-26
- **Status:** accepted
- **Context:** #29. The pool of decision 025 refills after every claim and does not count busy warm runners, so a burst of queued jobs on one repository starts about one warm container per job, with no upper bound, on a 4 GB host. Its idle runners also use up `MAYFLY_WORKER_TIMEOUT` and keep a live registration for as long as they wait. The brief asks for little wasted capacity and tracks cost per job.
- **Options considered:**
  - **A. Fixed minimum, total cap, idle runners rotated:** keep `min` runners idle per repository, refill after a claim only while fewer than `max` warm runners (idle and busy) are alive, and retire a runner idle past a timeout, replacing it only to stay at `min`. Smallest change to the pool's accounting; after a burst the pool falls straight back to `min`.
  - **B. Minimum plus a demand-driven burst:** as A, but each pool miss raises the repository's idle target by one up to `max`, and runners above `min` that idle past the timeout are retired, so the target decays; `min` can be 0. Adapts to real use, at the cost of more state per repository and a less predictable pool.
  - **C. One global cap for cold and warm workers:** tightest host protection, but couples both paths, needs a fairness rule between them and a rewrite of the cold path's fixed goroutines.
- **Decision:** A. `MAYFLY_POOL_SIZE` stays the minimum (default 1), `MAYFLY_POOL_MAX` caps warm runners alive per repository (default twice the minimum, never below it), and `MAYFLY_POOL_IDLE_TIMEOUT` (default 30 minutes) retires idle runners. A job that finds the cap reached takes the cold path. A runner is retired by deleting its registration on GitHub first and stopping its container only once that succeeded, so GitHub cannot hand it a job while it is being stopped; a failed delete leaves the runner running.
- **Consequences:**
  - The host runs at most `MAYFLY_WORKERS` cold workers plus `MAYFLY_POOL_MAX` warm runners per pooled repository.
  - Rotation bounds how long an idle registration lives and keeps idle time far below the worker timeout, at the cost of one registration and container start per idle slot per timeout.
  - A burst larger than `max`, such as a wide matrix, still runs partly cold, and the pool keeps no memory of it. The retire path relies on GitHub refusing to delete a busy runner, to be confirmed against the real API.
  - Revisit with option B if the #31 benchmark or the pool-miss metric (#30) shows repeated bursts above the minimum.

## 027. Pool metrics: claim time apart from boot time, size read at scrape
- **Date:** 2026-11-26
- **Status:** accepted
- **Context:** #30. The dashboard needs pool size, claim latency and pool misses, and decision 026 relies on the miss metric to judge the pool's sizing. Warm claims were recorded in `mayfly_worker_boot_seconds`, mixed with cold starts, and pool size existed only in the pool's memory.
- **Options considered:**
  - **Claim latency, A. Its own histogram, boot time kept for cold starts:** buckets can go below 1 s, and each name means one thing. Boot time changes meaning, so older data is not directly comparable.
  - **Claim latency, B. A `path=warm|cold` label on boot time:** easy to compare and existing queries keep working, but both paths share buckets starting at 1, 2 and 5 s, and nothing boots on the warm path.
  - **Pool size, A. A custom collector reading the pool at each scrape:** the gauges cannot drift from the pool's state, at the cost of a little more code and a short lock of the pool per scrape.
  - **Pool size, B. Gauges updated at each state change:** the more familiar pattern, but registration, claim, retirement and exit all change the state, and one missed update leaves a wrong value until restart.
- **Decision:** A for both. Misses are counted by reason (`miss_empty`, `miss_max`, `miss_unresolved`), in the same counter as hits so the hit rate is one division.
- **Consequences:**
  - From this version, `mayfly_worker_boot_seconds` covers cold starts only, so its values before and after are not comparable when a pool is configured.
  - A hit is a reservation, not proof that a warm runner ran the job: GitHub chooses the runner, and a reservation can expire.
  - A job that missed and was then taken by a warm runner has no claim time, since nothing recorded when it started waiting for one.
  - `miss_max` points at the cap (`MAYFLY_POOL_MAX`). `miss_empty` points at refills slower than the arrival of jobs, which a higher `MAYFLY_POOL_SIZE` would absorb.

## 028. Existing cache server for `actions/cache`, with a patched runner
- **Date:** 2026-11-27
- **Status:** accepted
- **Context:** #32. `actions/cache` v4 speaks the v2 cache protocol (Twirp over HTTP) and finds its server through `ACTIONS_RESULTS_URL`. The runner sets that variable from the job message and overwrites any value set on the worker, so no option works through configuration alone. The brief wants workflows to run unchanged and caches isolated per repo and branch (#33).
- **Options considered:**
  - **A. An existing self-hosted server (falcondev `github-actions-cache-server`) and a runner patch in the worker image:** mature, speaks v1 and v2, workflows unchanged, little code to own. Adds a non-Go service, depends on a binary patch of the runner, and its isolation model must be checked for #33.
  - **B. A minimal v2 server written in Go inside Mayfly, with the same patch:** full ownership and isolation built in, but the most work, on a protocol GitHub does not document.
  - **C. A drop-in replacement action backed by S3 or MinIO:** no patch, but users must edit their workflows, and the caching built into `setup-*` actions still goes to GitHub.
  - **D. A shared cache volume mounted on the workers:** simplest and fastest, but does not serve `actions/cache`, and a shared writable volume lets any job poison the cache for all others.
- **Decision:** A, for its maturity. The runner patch lives in the worker image, pinned to the runner version the image ships.
- **Consequences:**
  - Workflows keep using `actions/cache` and `setup-*` caching unchanged.
  - Each runner upgrade must re-check the patch. The image build fails if the patch no longer applies, instead of silently caching on GitHub.
  - A new service joins the compose stack, with its storage on the host disk.
  - #33 depends on what the server can scope by. If it cannot isolate by repo and branch, wrap it or revisit B.
  - B stays an option for a later Mayfly version if the server limits isolation or the protocol changes faster than it follows.

## 029. Worker scratch directories on tmpfs, under one memory budget
- **Date:** 2026-11-27
- **Status:** accepted
- **Context:** #36. A worker lives for one job, so nothing it writes needs to reach the disk, yet the checkout, `node_modules`, build outputs and restored caches all go through overlayfs to the VM's small managed disk. The host has 4 GB of RAM, about 1.3 GB of it already used by the other services, and tmpfs pages count as memory.
- **Options considered:**
  - **Directories, A. `_work` and `/tmp` only:** what the issue names, the smallest memory cost, but the Go and npm caches that `actions/cache` restores into stay on disk.
  - **Directories, B. A plus `~/.cache/go-build`, `~/go/pkg/mod` and `~/.npm`:** covers where the cache restore, the compile and `npm ci` write, so the larger expected gain, at more memory per job.
  - **Size, A. One budget split across the mounts:** a job can never hold more than the budget in tmpfs, whatever directory fills up.
  - **Size, B. One limit per mount:** simpler to reason about per directory, but the sum can exceed what the host can spare.
- **Decision:** B for the directories, for the larger expected gain, and A for the size. Off unless a budget is configured (`MAYFLY_WORKER_TMPFS_SIZE`). The caches are mounted at `/mayfly/go-build`, `/mayfly/go-mod` and `/mayfly/npm`, with `GOCACHE`, `GOMODCACHE` and `npm_config_cache` pointing at them: mounting under `$HOME` would make Docker create root-owned parents such as `~/.cache` and `~/go` that the runner could no longer write to. `_work` gets two shares of the budget and each other mount one. `/opt/hostedtoolcache` is never mounted over, since a mount hides the toolchains baked into the image (decision 022).
- **Consequences:**
  - Disk I/O leaves the job's hot path; the gain must be measured, since the page cache already absorbs many writes.
  - Each job can hold up to the budget in RAM. A job that fills a mount fails with "no space left on device" instead of pushing the host out of memory.
  - Splitting the budget caps each directory at its share, so one directory can fill up while others are empty. Revisit the split if a workload hits it.
  - Idle warm runners cost nothing extra, since an empty tmpfs uses no memory.

## 030. Athens and Verdaccio as package proxies for workers
- **Date:** 2026-11-28
- **Status:** accepted
- **Context:** #35. A job whose `actions/cache` entry misses (new branch, changed lockfile, evicted entry) downloads every Go module and npm package from the internet again. A proxy on the host keeps everything it has served, across repositories and branches. #34 (registry mirror) was closed as not planned at the same time: workers have no Docker daemon (decision 015), so jobs never pull images.
- **Options considered:**
  - **A. Athens for Go and Verdaccio for npm, two Compose services:** standard tools, reached through the variables Go and npm already read (`GOPROXY`, `npm_config_registry`). Two more services: Athens used 18 MB and Verdaccio 144 MB in a local test.
  - **B. Athens only:** one service where most of the download time is, but npm stays on the internet, against what #35 asks.
  - **C. A generic HTTP caching proxy (Squid, nginx):** one service, but registries use HTTPS, so caching means intercepting TLS with a CA every worker trusts.
- **Decision:** A, behind one switch, like the cache server (decision 028). Athens also proxies the Go checksum database, so module verification keeps working.
- **Consequences:**
  - Cache misses and uncached workflows fetch modules and packages from the host after their first download.
  - Two more services hold memory (about 160 MB) and disk on the 4 GB host, and must be kept up to date.
  - If a proxy is down, Go and npm fail instead of falling back to the internet, so jobs depend on it while the switch is on.
  - Workflows that set their own `GOPROXY` or registry bypass the proxies.
