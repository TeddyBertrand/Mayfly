# Architecture, file by file

[components.md](components.md) gives the three-component overview. This guide goes down to the files: in what order to read the code, how one job travels through it, and what every Go file under `cmd/` and `internal/` is for.

## How to read the code

Read from the data that flows through the system toward the programs that assemble it. Each step only needs the ones before it.

| # | Package | Why here |
|---|---|---|
| 1 | `workflow` | The `Job` every other package passes around, and `MatchesLabels` |
| 2 | `config` | Every setting and where it comes from (environment variables, secret files) |
| 3 | `receiver` | The front door: signature, filtering, dedupe, handoff |
| 4 | `provisioner` | The job loop and the per-job state machine ([state-machine.md](state-machine.md)) |
| 5 | `worker`, then `worker/docker` | The `Backend` and `Inventory` interfaces, then their only implementation |
| 6 | `github`, `retry` | How Mayfly authenticates as an App and survives GitHub or Docker hiccups |
| 7 | `reaper` | The safety net, which reads the same Docker labels the backend wrote |
| 8 | `metrics`, `ops`, `httpserve`, `logkey` | Cross-cutting: metrics, health, servers, log keys |
| 9 | `cmd/mayfly`, `cmd/reaper` | The wiring: how the packages above become two processes |
| 10 | `bench`, `cmd/bench`, `cmd/setwebhook` | Tools around the product, not part of the runtime path |

## One job, from webhook to cleanup

```
GitHub                       mayfly process                                         Docker host
  |                                                                                     |
  | POST /webhook (workflow_job, queued)                                                |
  |--------------> receiver.handleWebhook                                               |
  |                  1. body > 1 MiB ................................... 413            |
  |                  2. VerifySignature (HMAC) fails ................... 401            |
  |                  3. dispatch: ping 200 | other event 202 | no delivery id / bad JSON 400
  |                  4. action not queued/in_progress/completed ........ 202 ignored    |
  |                  5. labels are not mayfly's ........................ 202 ignored    |
  |                  6. Deduper.Seen(delivery) already seen ............ 202 ignored    |
  |                  7. OnQueued = provisioner.Enqueue                                  |
  |                       tracker.add -> state WAITING, push on the bounded channel     |
  |                       channel full -> Forget(delivery), answer 503 --+              |
  |<-------------- 202 Accepted                                         |              |
  |                                                  (GitHub redelivers the 503 later)  |
  |                                                                                     |
  |                     provisioner.Run: MAYFLY_WORKERS goroutines take jobs            |
  |                     handle(job)                                                     |
  |                       tracker.begin    WAITING -> PROVISIONING                      |
  |                          (already DROPPED by a cancel while queued? skip the job)   |
  |<-- JWT, then installation token (cached) --                                         |
  |<-- POST generate-jitconfig (retry.Do, new runner name per attempt)                  |
  |        fails for good -> metric provision_failures{jit_config}, job abandoned       |
  |                       tracker.ready    PROVISIONING -> READY                        |
  |                          (dropped meanwhile? the registration is left for the reaper)
  |                       backend.Run(spec)                                             |
  |                          ensureImage, ensureNetwork, createContainer (labels)       |
  |                          startContainer ----------------------------------------->  |
  |                          container starts ./run.sh with the JIT config in its env   |
  |                                                                                     |
  |<-------------- the runner registers and takes ONE job                               |
  | POST /webhook (in_progress) --> Update --> tracker: READY -> RUNNING (boot metric)  |
  | ... the job runs ...                                                                |
  | POST /webhook (completed)   --> Update --> tracker: RUNNING -> DONE                 |
  |                                                                                     |
  |                          the runner exits after its job; AutoRemove deletes the container
  |                          waitRemoved returns, defer remove() is a no-op safety net  |
  |                       handle returns, tracker.remove(job)  --> nothing left behind  |
```

### Failure and cleanup paths

Everything above is the happy path. Each branch below has a different owner, which is the point of the design: the process that failed is never the only thing that can clean up.

| What goes wrong | Who notices | What happens |
|---|---|---|
| Bad signature, oversized body | receiver | 401 or 413, nothing tracked |
| Same delivery twice | `Deduper` | 202, ignored (idempotency) |
| Queue full | `Enqueue` | delivery forgotten, 503, GitHub redelivers later |
| GitHub API 5xx, rate limit, lost connection | `retry.Do` | up to 5 attempts with exponential backoff and jitter, honouring `Retry-After` |
| JIT config fails for good | `handle` | failure metric and error log; no worker was created, so nothing to clean up |
| Docker create or start fails | `Backend.start` | removes the half-made container, `retry.Do` tries again, then `handle` logs a failure |
| Job cancelled while still queued | `Update` -> `Dropped` | the worker context is cancelled, `backend.Run` stops, the container is removed |
| Job runs past `MAYFLY_WORKER_TIMEOUT` | `Backend.Run` | context deadline, error "worker timed out", container force-removed |
| Mayfly stops (SIGTERM) | `provisioner.Run` | running workers are cancelled, queued jobs are dropped and logged |
| Mayfly crashes or restarts | reaper | its containers carry the old instance ID, so they are `owner instance gone`, and they are removed |
| Container left over for any reason | reaper `Sweep` | removed for age, owner gone or unreachable, or runner gone or offline on GitHub |
| Runner registration left on GitHub | reaper `sweepRegistrations` | an offline `mayfly-` runner with no container for `StaleAfter` is deleted |
| Job queued but nobody picks it up | reaper `sweepStuck` | logged once with a reason (decision 013); never retried automatically |

```
            mayfly process                              reaper process (separate binary)
   receiver -> provisioner -> docker backend      every interval, one Sweep:
        |                          |                1. list containers by label mayfly.job_id
        | labels written           |                2. ask mayfly /healthz for its instance ID
        | at create time           v                3. remove each container whose reason is not ""
        +------------------> containers  <----------4. delete stale offline registrations on GitHub
                                                    5. log jobs stuck in queued
```

## Package by package

Each file lists what it is for, its key symbols, who calls it, what it depends on and how it fails. "Failure" is what the caller sees or what the operator sees in the logs, not every possible `error`.

### `internal/workflow`

**`internal/workflow/job.go`**
- Purpose: the shared vocabulary of a workflow job, so the receiver, provisioner and reaper do not import each other.
- Key: `Job`, `Action` (`queued`, `in_progress`, `completed`), `ConclusionCancelled`, `MatchesLabels`.
- Called by: `receiver` (builds `Job`), `provisioner`, `reaper` (label filter).
- Depends on: the standard library only.
- Failure: none; `MatchesLabels` is a pure function and compares labels case-insensitively as GitHub does.

### `internal/config`

All settings come from `MAYFLY_*` environment variables (decision 003). Secrets are never read from variables: a variable names a file, and the file holds the secret. Every loader takes `getenv` and `readFile` as arguments so tests need no real environment.

**`internal/config/config.go`**
- Purpose: the receiver's settings.
- Key: `ReceiverConfig`, `LoadReceiver`, `loadRunnerLabels`/`parseLabels` (the labels a job must request to be Mayfly's), `loadSecretFile` (reads and trims a secret file).
- Called by: `cmd/mayfly`, `cmd/reaper` (labels).
- Depends on: `shared.go`.
- Failure: a missing or invalid value returns an error at startup, so the process refuses to start. Errors never include secret contents.

**`internal/config/provisioner.go`**
- Purpose: the provisioner's settings.
- Key: `ProvisionerConfig`, `LoadProvisioner`, helpers `parseDuration` and `parsePositiveInt` (reject zero and negatives).
- Called by: `cmd/mayfly`.
- Failure: startup error on a bad value.

**`internal/config/reaper.go`**
- Purpose: the reaper's settings: interval, max age, grace, missed probes, stale and stuck windows, health URL.
- Key: `ReaperConfig`, `LoadReaper`. It enforces that `MAYFLY_WORKER_MAX_AGE` exceeds `MAYFLY_WORKER_TIMEOUT`.
- Called by: `cmd/reaper`.
- Failure: startup error. The max-age check exists because otherwise the reaper would kill jobs the backend still allows to run.

**`internal/config/shared.go`**
- Purpose: settings used by more than one process, and their defaults.
- Key: `GitHubApp` (App ID, PEM key bytes, API URL), `Docker` (socket), `LoadGitHubApp`, `LoadOpsAddr`, `loadWorkerTimeout`.
- Called by: the three loaders above and `cmd/setwebhook`.
- Depends on: nothing internal, on purpose, so every other package may import `config`.
- Failure: startup error.

### `internal/receiver`

**`internal/receiver/receiver.go`**
- Purpose: starts the webhook listener.
- Key: `Handoff` (two callbacks: `OnQueued` returns an error, `OnUpdate` does not), `Run`, `dedupeTTL` (72h, GitHub's redelivery window).
- Called by: `cmd/mayfly`.
- Depends on: `config`, `httpserve`, `workflow`.
- Failure: `Run` returns an error if it cannot listen or the server dies; a cancelled context is a clean stop with graceful drain.

**`internal/receiver/server.go`**
- Purpose: the whole webhook decision tree, in one request handler.
- Key: `Webhook`, `NewHandler` (the only route is `POST /webhook`), `handleWebhook` (size cap, signature), `dispatch` (event, parse, action, labels, dedupe, handoff).
- Called by: `receiver.Run`.
- Depends on: `signature.go`, `event.go`, `dedupe.go`, `workflow`, `logkey`.
- Failure: 413 too big, 401 bad signature, 400 malformed, 202 for anything valid but not for Mayfly, 503 when `OnQueued` fails. A 2xx is returned for ignored events so GitHub does not mark the delivery failed.

**`internal/receiver/signature.go`**
- Purpose: authenticate the sender.
- Key: `VerifySignature(secret, body, header)`: HMAC-SHA256 of the body, compared with `hmac.Equal`, which is constant-time so timing does not leak how many bytes matched.
- Failure: returns false on a missing `sha256=` prefix, bad hex or a mismatch. The caller answers 401.

**`internal/receiver/event.go`**
- Purpose: turn GitHub's JSON into a `workflow.Job`.
- Key: `workflowJobEvent` (only the fields Mayfly reads), `parseWorkflowJob`.
- Failure: error when the JSON is invalid or the action or job ID is missing. A null timestamp stays zero.

**`internal/receiver/dedupe.go`**
- Purpose: idempotency. The same delivery ID is processed once.
- Key: `Deduper` interface (`Seen` records and tests in one atomic step, `Forget` undoes it), `MemoryDeduper` with a TTL and a periodic sweep so memory stays bounded.
- Failure: in memory only, so a restart forgets everything. That is acceptable because the provisioner's tracker also refuses a job ID it already tracks.

### `internal/provisioner`

**`internal/provisioner/provisioner.go`**
- Purpose: the job loop: bounded queue in, fixed pool of workers out.
- Key: `Provisioner`, `New`, `Enqueue` (non-blocking, `ErrQueueFull`), `Update` (applies in_progress and completed events), `Run` (starts `MAYFLY_WORKERS` goroutines, drains on shutdown), `handle` (JIT config, then `backend.Run`), `dropped`, `runnerLabels`.
- Called by: `cmd/mayfly` (as the receiver's `Handoff`).
- Depends on: `JITIssuer` (implemented by `github.Client`), `worker.Backend`, `retry`, `metrics`, `state.go`.
- Failure: JIT registration is retried, then abandoned with a metric. A cancelled job (`dropped`) is logged as info, not as an error. On shutdown, running workers are cancelled and queued jobs are dropped with a warning.

**`internal/provisioner/state.go`**
- Purpose: the per-job state machine, held in memory (decision 011).
- Key: `State` (`Waiting`, `Provisioning`, `Ready`, `Running`, `Done`, `Dropped`), `transitions`, `target` (which state an event asks for), `tracker` with `add`, `begin`, `ready`, `update`, `find`, `move`, `remove`.
- Called by: `provisioner.go` only.
- Depends on: `workflow`.
- Failure: an illegal move returns an error and leaves the state alone. `update` finds a job by runner name first, then by ID, because a runner may take a different job than the one it was created for. Dropping a job calls its stored `cancel`, which stops only its worker.

**`internal/provisioner/observe.go`**
- Purpose: job metrics derived from GitHub's own timestamps.
- Key: `observe` (queue time on in_progress; job count and duration on completed), `observeSpan` (skips a missing or reversed timestamp).
- Called by: `Update`, for tracked and untracked jobs alike, since a completed event often arrives after the entry is gone.

### `internal/worker`

**`internal/worker/worker.go`**
- Purpose: the contract between the provisioner or reaper and a machine, independent of Docker, so a Firecracker backend could plug in later.
- Key: `Backend` (`Run` must leave nothing behind), `Inventory` (`List`, `Remove`), `Spec`, `Info`, `ShortID`.
- Called by: `provisioner`, `reaper`, `worker/docker`.
- Failure: `Spec` hides its JIT config in `LogValue` and `String`, so it cannot leak through logs or `fmt`.

**`internal/worker/docker/backend.go`**
- Purpose: runs a worker as one container.
- Key: `Backend`, `New`, `Run`, `start` (one retryable attempt that cleans up after itself), `containerSpec`, `ensureImage`, `ensureNetwork`, `remove`, the `mayfly.*` label names.
- Called by: `provisioner.handle`.
- Depends on: `engine.go`, `retry`, `metrics`.
- Failure: start errors are retried; a deadline gives "worker timed out"; the container is always force-removed with its own 30s deadline, because the job context may already be done. If removal fails it is logged and left for the reaper. The JIT config goes in an environment variable, not argv, so it stays out of the host process list.

**`internal/worker/docker/engine.go`**
- Purpose: a minimal Docker Engine API client over the Unix socket (decision 008).
- Key: `engine`, `containerSpec`, `createContainer`, `startContainer`, `waitRemoved`, `removeContainer`, `imageExists`/`pullImage`, `networkExists`/`createNetwork`, `call`/`send`.
- Failure: `engineError` (a non-2xx answer; 5xx counts as temporary) and `connError` (no answer, always temporary). Both feed `retry.Temporary`. A missing container on removal is not an error.

**`internal/worker/docker/inventory.go`**
- Purpose: lets the reaper see workers without knowing how they were started.
- Key: `Inventory`, `NewInventory`, `List` (every container with the `mayfly.job_id` label, running or not, rebuilt from its labels), `Remove`, `parseLabelInt`.
- Failure: a missing or malformed numeric label becomes zero, which the reaper treats as "cannot look this up on GitHub".

### `internal/github`

**`internal/github/client.go`**
- Purpose: the REST client that authenticates as the GitHub App.
- Key: `Client`, `NewClient`, `InstallationToken` (App JWT exchanged for an installation token, cached until near expiry; the lock is held during refresh so concurrent callers share one request), `do`, `APIError`, `netError`, `rateLimit`.
- Failure: `APIError` is temporary for 5xx and rate limits and carries `Retry-After`; `netError` is always temporary. Errors never contain credentials.

**`internal/github/jwt.go`**
- Purpose: the App's identity proof.
- Key: `ParsePrivateKey` (PKCS#1 or PKCS#8 PEM), `appJWT` (RS256, short-lived).
- Failure: errors never include key material.

**`internal/github/jit.go`**
- Purpose: register a one-job runner and delete registrations.
- Key: `GenerateJITConfig`, `DeleteRunner`, `RunnerName` (e.g. `mayfly-123-a3f9c1`), `RunnerNamePrefix`, `JITConfig` (hides its encoded credential), `repoPattern`.
- Called by: `provisioner` (generate), `reaper` (delete).
- Failure: `repoPattern` rejects a repo name that could rewrite the request path. The `mayfly-` prefix is how the reaper recognises its own runners.

**`internal/github/list.go`**
- Purpose: paginated reads the reaper needs.
- Key: generic `list`, `Repos`, `Runners`, `QueuedJobs`, `Repo`, `RunnerRegistration`, `QueuedJob`.
- Failure: a page cap stops a misbehaving API from looping forever.

**`internal/github/runner.go`**
- Purpose: look up one runner registration.
- Key: `Runner`, `Client.Runner`, the online and offline status constants.
- Failure: a missing registration is `Found: false`, not an error. The reaper depends on that difference to say "runner registration gone".

**`internal/github/hook.go`**
- Purpose: read and set the App's webhook URL.
- Key: `AppWebhookURL`, `SetAppWebhookURL` (https only; sends only the URL so the secret is untouched).
- Called by: `cmd/setwebhook`.

**`internal/github/actions.go`**
- Purpose: benchmark-side calls made with a user token, not the App.
- Key: `TokenClient`, `DispatchWorkflow`, `Run`, `CancelRun`, `RunJobs`, `WorkflowRun`, `Job`, `Step`.
- Called by: `internal/bench`.
- Failure: shares `Client`'s error and rate-limit handling; `workflowFilePattern` validates the workflow file name.

### `internal/retry`

**`internal/retry/retry.go`**
- Purpose: try again after temporary failures, without hammering a struggling service.
- Key: `Policy`, `Default` (5 attempts, 1s base, 30s cap), `Do`, `backoff` ("full jitter": a random wait up to `Base*2^(n-1)`), `Temporary`, `After`.
- Called by: `provisioner.handle` (JIT config), `docker.Backend.Run` (worker start).
- Depends on: nothing internal. It asks the error itself, through a `Temporary() bool` method, so it knows nothing about GitHub or Docker.
- Failure: stops on a permanent error, on the last attempt, when a server asks to wait longer than the cap, or when the context ends. The returned error wraps the last cause and says why it stopped.

### `internal/reaper`

**`internal/reaper/reaper.go`**
- Purpose: the core of the safety net.
- Key: `Reaper`, `New`, `Run` (sweep at start, then every interval), `Sweep` (containers first, then registrations, then stuck jobs), `GitHub` and `Owner` interfaces, `Options`, `Report`.
- Called by: `cmd/reaper`.
- Failure: a container-list failure aborts the sweep. GitHub failures only skip their pass. The reaper acts only on positive evidence: when unsure it waits for the next cycle.

**`internal/reaper/reason.go`**
- Purpose: why a worker should be removed.
- Key: the `Reason*` constants, `reason` (max age, owner gone, owner unreachable after `MissedProbes` cycles, runner gone, runner offline), `reasonLabels`, `presetReasons`.
- Failure: a GitHub lookup error keeps the worker. Young workers (inside `Grace`) skip the GitHub check because their runner may not be online yet.

**`internal/reaper/registrations.go`**
- Purpose: delete runner registrations that outlived their container.
- Key: `sweepRegistrations`. It only touches names starting with `mayfly-`, offline, with no container claiming them, for `StaleAfter`.
- Failure: a failed listing keeps that repo's timers instead of resetting them. The timers are lost on restart, which only delays deletion.

**`internal/reaper/stuck.go`**
- Purpose: report jobs that waited too long (decision 013: logged, not retried).
- Key: `sweepStuck`. Each stuck job is logged once, with the reason "mayfly unreachable" or "not picked up".

**`internal/reaper/owner.go`**
- Purpose: ask the live mayfly process who it is.
- Key: `HealthOwner`, which reads the instance ID from `/healthz`.
- Failure: a non-200 answer, bad JSON or an empty instance is an error, which counts as a missed probe.

### `internal/metrics`, `ops`, `httpserve`, `logkey`

**`internal/metrics/metrics.go`**
- Purpose: every Prometheus metric in one place (decision 020); [metrics.md](metrics.md) lists them.
- Key: `Mayfly`, `Reaper`, `NewMayfly`, `NewReaper`, `NewRegistry`, `Handler`, `Preset` (starts a labelled series at 0 so `increase()` counts the first event), `OpJITConfig`, `OpWorkerStart`, `StageJITConfig`, `StageWorker`.
- Failure: a nil registerer leaves metrics unregistered, for tests.

**`internal/ops/ops.go`**
- Purpose: the ops listener, never exposed to the internet (decision 018).
- Key: `NewHandler` (`GET /healthz`, `GET /metrics`), `Run`, `Health`.
- Failure: `/healthz` depends on no external service, so it answers even when GitHub or Docker is down.

**`internal/httpserve/httpserve.go`**
- Purpose: one way to run an HTTP server safely.
- Key: `New` (explicit read, write and idle timeouts against slow clients), `Serve` (graceful shutdown on a fresh context, since the parent is already cancelled).
- Called by: `receiver.Run`, `ops.Run`.

**`internal/logkey/logkey.go`**
- Purpose: names for structured log keys, so a typo is a compile error instead of a broken log search.
- Key: constants only.

### `internal/bench` and its commands

The benchmark tools measure Mayfly against other runner types ([benchmarks.md](benchmarks.md)). They are not on the runtime path.

**`internal/bench/harness.go`**
- Purpose: run workflows one at a time and record each job's timing.
- Key: `Harness`, `Config`, `API` interface, `Run`, `runOne`, `waitRun`, `waitJob`, `poll`, `SleepContext`.
- Failure: a run that times out is cancelled and recorded as `timed_out`. Several in a row, or any other error, stop the benchmark.

**`internal/bench/schedule.go`**
- Purpose: the order of runs. `Schedule` cycles the runner types innermost so a slow period on GitHub's side spreads across all of them.

**`internal/bench/timing.go`**
- Purpose: derive durations from a completed job's timestamps. Key: `Timing`, `Measure`, `firstWorkflowStep`. Fails if a timestamp is missing.

**`internal/bench/record.go`**
- Purpose: one measured job per CSV row. Key: `Record`, `Writer` (flushes each row so an interrupted run keeps its data), `ReadRecords`, `parseRecord`.

**`internal/bench/stats.go`**
- Purpose: statistics. Key: `Summary`, `Summarize` (median, nearest-rank p90, min, max), `RelDiff`.

**`internal/bench/report.go`**
- Purpose: Markdown reports and repeatability checks. Key: `Summaries`, `WriteSummary`, `Compare`, `WriteComparison`. A cell missing on one side fails the comparison.

**`cmd/bench/main.go`**
- Purpose: the `bench` command with its `run`, `summary` and `compare` subcommands. Key: `run`, `runBench`, `runSummary`, `runCompare`, `readFile`, `splitList`. Failure: a non-zero exit when a run fails or a comparison is out of tolerance.

**`cmd/setwebhook/main.go`**
- Purpose: a local-dev tool that points the App's webhook at a new tunnel URL. Key: `main`, `run`. Failure: exits non-zero if either GitHub call fails.

### `cmd/mayfly`

**`cmd/mayfly/main.go`**
- Purpose: wiring for the main process and its start and stop logic.
- Key: `main` (handles `--version` and `--healthcheck`, then runs), `run` (loads config, builds the GitHub client, Docker backend and provisioner, then starts three things: the provisioner loop, the ops server, and the receiver).
- Failure: if the receiver or the ops server dies on its own, `stop()` cancels the others, and the errors are joined and returned. SIGTERM and SIGINT cancel the context for a graceful shutdown.

**`cmd/mayfly/healthcheck.go`**
- Purpose: the container healthcheck, built into the binary because the distroless image has no curl.
- Key: `runHealthcheck`, `healthURL` (dials loopback when the address is a wildcard), `healthcheck`, `healthcheckTimeout` (3s, under Compose's 5s probe).
- Failure: prints to stderr and exits 1 unless `/healthz` answers 200.

**`cmd/mayfly/instance.go`**
- Purpose: `newInstanceID`, a random ID per process.
- Why it matters: a restart gets a new ID, and that change is how the reaper spots the old process's containers as orphans (decision 012).

**`cmd/mayfly/version.go`**
- Purpose: `version` (set at build time with `-ldflags "-X main.version=..."`) and `printVersion` for `--version`.

### `cmd/reaper`

**`cmd/reaper/main.go`**
- Purpose: wiring for the reaper process, which is a separate binary so that the cleaner does not depend on what it cleans.
- Key: `main`, `run` (loads config, builds the Docker inventory, GitHub client and `HealthOwner`, serves its own ops endpoints, runs the sweep loop).
- Failure: same pattern as `cmd/mayfly`: if the ops server dies, `stop()` ends the sweeps.

## Coverage check

Every non-test Go file under `cmd/` and `internal/` appears in this guide as a bold entry with its full repo path. This loop prints each file that does not, and prints nothing when the guide is complete. Run it from the repo root after adding, renaming or removing a file.

```bash
for f in $(find cmd internal -name '*.go' ! -name '*_test.go' | sort); do grep -qF "**\`$f\`**" docs/architecture.md || echo "missing: $f"; done
```
