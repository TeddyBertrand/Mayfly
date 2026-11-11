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
