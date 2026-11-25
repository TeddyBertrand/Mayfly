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
