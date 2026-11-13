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
