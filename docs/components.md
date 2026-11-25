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

## In one paragraph

GitHub tells the receiver a job is waiting. The receiver checks it is authentic and not a duplicate, then asks the provisioner for a worker. The provisioner gets a one-job credential from GitHub and starts a throwaway container with it. If a container is left behind, the reaper kills it. Each worker lives for exactly one job, hence the name Mayfly.
