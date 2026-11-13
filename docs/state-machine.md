# Worker state machine

The provisioner tracks every job it handles, from the `queued` webhook until its worker is gone. The code is in [`internal/provisioner/state.go`](../internal/provisioner/state.go) and the tests in `state_test.go`.

## States

```
                 queued              picked from queue         runner registered        in_progress
  (webhook) ───────────▶ waiting ─────────────────▶ provisioning ─────────────▶ ready ───────────────▶ running
                            │                            │                     │  │                     │
                            │ completed (cancelled)      │ completed           │  │ completed           │ completed
                            ▼                            ▼ (cancelled)         │  ▼ (not cancelled)     ▼ (any conclusion)
                         dropped ◀───────────────────────┴─────────────────────┘ done ◀─────────────────────┘
                                          completed (cancelled)
```

| State | Meaning | What stops it |
|---|---|---|
| `waiting` | In the provisioner queue, no worker yet. | A pool goroutine picks it up, or it is cancelled. |
| `provisioning` | Requesting the JIT runner config from GitHub. | Registration ends, or it is cancelled. |
| `ready` | Runner registered, container starting or waiting for a job. | GitHub assigns a job (`in_progress`), or it is cancelled. |
| `running` | The runner is executing a job. | The job completes. |
| `done` | The job ended. The runner exits by itself (it is ephemeral). | Final. |
| `dropped` | Cancelled before the runner took a job. The worker is not started, or is stopped. | Final. |

The entry is removed from the table when the worker's goroutine returns, whatever the final state.

## Rules

- **Allowed moves only.** The table in `transitions` lists every legal move. Anything else is rejected and logged, never forced. This protects against GitHub's webhooks arriving out of order: a late `in_progress` after `completed` would otherwise move a finished job back to `running`.
- **A running job is never killed.** A cancelled job that has started ends as `done`, not `dropped`: GitHub stops the job and the ephemeral runner exits by itself.
- **Cancelling stops one worker.** Each worker runs under its own context. Dropping a job in `provisioning` or `ready` cancels that context, which aborts the JIT request or stops the container.
- **Matching by runner name first.** A JIT runner can pick up any queued job with matching labels in its repository, not only the one it was created for. `in_progress` and `completed` events carry `runner_name`, so the provisioner looks up the entry by runner name and falls back to the job ID.
- **Duplicate `queued` events are ignored** while the job ID is tracked, so a job never gets two workers.
- **State lives in memory** (design decision 011). After a restart the table is empty. Containers left behind are found by their `mayfly.job_id` label by the reaper.

## Known limits

- A job cancelled in `ready` whose runner registration was already created leaves that registration on GitHub until the reaper removes stale runners.
- If a runner picks up a different job than its own, a later `completed (cancelled)` for its own job ID may arrive while the runner is `running` another job. The entry then moves to `done`, which is harmless: the worker is not stopped, and it is removed when it exits.
