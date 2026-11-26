# Components

Mayfly runs each GitHub Actions job on a throwaway worker. Three Go components make that happen, plus the worker itself.

```
GitHub --webhook--> RECEIVER --> PROVISIONER --> WORKER (1 job, then destroyed)
                    "a job is     "build a          runs actions/runner
                     waiting"      worker"                  ^
                                                            | cleans up leftovers
                                                         REAPER
```

The worker is a container or VM, not Go code in this repo, so it has no `cmd/` folder.

This page is the overview. [architecture.md](architecture.md) goes down to the files: a reading order, one job's full path with its failure and cleanup branches, and every Go file explained.

## Receiver

The front door. A small HTTP server that receives `workflow_job` webhooks and decides whether a worker is needed.

- **Verifies the signature.** GitHub signs the body with HMAC-SHA256 and a shared secret (`X-Hub-Signature-256`). Without this check, anyone with the URL could trigger servers and cost money. The compare is constant-time so timing does not leak the secret.
- **Filters events.** Only `queued` starts a worker. Other actions are logged and ignored.
- **Deduplicates.** The same delivery can arrive twice (for example a manual redelivery from the GitHub UI). The receiver remembers `X-GitHub-Delivery` IDs so a repeat does not start a second worker (idempotency).
- **Answers fast.** It returns 2xx quickly and leaves the real work to the provisioner.

## Provisioner

The builder. It turns "a job is queued" into "a worker is running and registered".

1. Authenticates as a GitHub App: a signed JWT, exchanged for an installation token.
2. Calls `generate-jitconfig`. A JIT (just-in-time) config is a one-time credential for a runner bound to exactly one job, so it cannot be reused for later jobs.
3. Starts the worker (Docker first) and passes it the config. The runner name includes the job ID for traceability.

Main failure modes: expired tokens, API rate limits, container start failures. All are logged with the job ID.

## Reaper

The safety net. It finds and destroys workers that outlived their job: crashed machines, hung runners, or a provisioner that died between "create" and "register". It compares what exists with what GitHub reports.

It is a separate binary on purpose: the thing that cleans up must not depend on the thing that may have failed. It backs the success metric "zero orphaned workers after a 24h soak test".

## How the components are packaged

The receiver and the provisioner run in one binary, `cmd/mayfly` ([decision 001](design-decisions.md)). The receiver puts each accepted job on a bounded Go channel and answers GitHub at once; a fixed pool of provisioner goroutines takes jobs from it. When the channel is full, the receiver answers 503 and forgets the delivery ID, so a redelivery from GitHub is processed.

```
webhook ─► receiver ─► channel (MAYFLY_QUEUE_SIZE) ─► MAYFLY_WORKERS goroutines ─► JIT config ─► container
```

The reaper stays a separate binary (`cmd/reaper`): the thing that cleans up must not depend on the thing that may have failed.

A JIT runner is not bound to the job that triggered it. GitHub gives it any queued job whose labels it matches, so with two jobs queued at once, worker A may run job B. The count stays one worker per job; the job ID in the runner name is the job that triggered the worker.

### Warm pool

For the repositories in `MAYFLY_POOL_REPOS`, the provisioner keeps `MAYFLY_POOL_SIZE` runners registered, started and idle before any job exists ([decision 025](design-decisions.md)). They are ordinary JIT runners named `mayfly-warm-<random>`, so each still takes one job and is removed.

```
queued ─► free warm runner for the repo? ─ yes ─► reserve it, refill in the background (GitHub hands the job to an idle runner)
                                          └ no ──► cold path above (pool miss)
```

GitHub, not Mayfly, picks which idle runner gets a job, so the pool only counts: a queued job reserves one free runner, and an `in_progress` or `completed` event for the job, or a 5-minute timeout, ends the reservation. A warm runner taking a job that also has a cold worker waiting drops that worker. Warm runners have their own goroutines, outside `MAYFLY_WORKERS`.

Webhooks can be late and out of order, and a short job can end before any of them arrives. So the pool remembers an exited warm runner for 5 minutes:

```
container exits ─► slot freed and refilled at once ─► name kept for 5 min
   event naming it arrives ──► it took a job (claim time recorded on in_progress)
   no event in the window ───► warning "warm runner exited without a job"
```

If `completed` arrives before `in_progress`, the reservation stops counting at once and only its start time is kept, so the later `in_progress` can still record the claim time. This memory is lost on restart.

The pool is bounded ([decision 026](design-decisions.md)): it refills only while fewer than `MAYFLY_POOL_MAX` warm runners are alive for the repository, idle or busy, so the host runs at most `MAYFLY_WORKERS` cold workers plus `MAYFLY_POOL_MAX` warm runners per pooled repository. A runner idle for `MAYFLY_POOL_IDLE_TIMEOUT` is retired and replaced:

```
idle past timeout ─► delete registration on GitHub ─ ok (or 404) ─► stop container ─► refill to MAYFLY_POOL_SIZE
                                                    └ fails ─────► keep it idle, retry next sweep
```

Deleting the registration first means GitHub cannot hand the runner a job while its container is being stopped; a runner that took a job in the meantime is left running.

## In one paragraph

GitHub tells the receiver a job is waiting. The receiver checks it is authentic and not a duplicate, then asks the provisioner for a worker. The provisioner gets a one-job credential from GitHub and starts a throwaway container with it. If a container is left behind, the reaper kills it. Each worker lives for exactly one job, hence the name Mayfly.
