# Metrics

Mayfly exposes Prometheus metrics on each process's ops listener, which is never published (decisions 018 and 020). All names, units and buckets are defined in `internal/metrics`.

| Process | Endpoint | Local default | In Compose |
|---|---|---|---|
| mayfly (receiver, provisioner) | `GET /metrics` | `127.0.0.1:8081` (`MAYFLY_OPS_ADDR`) | `mayfly:8081` |
| reaper | `GET /metrics` | `127.0.0.1:8082` (`MAYFLY_REAPER_OPS_ADDR`) | `reaper:8082` |

```bash
curl -s 127.0.0.1:8081/metrics | grep ^mayfly_
```

## Where the time goes

```
GitHub:  created_at ─────────────────────── started_at ───────────────── completed_at
                   │◄──── mayfly_job_queue_seconds ───►│◄─ mayfly_job_duration_seconds ─►│
mayfly:     queued event accepted ─────── in_progress event
                   │◄─ mayfly_worker_boot_seconds ─►│
```

- **Queue time** is what a developer feels: how long the job waited for a runner. It uses GitHub's timestamps, the same ones the benchmark harness reads through the API.
- **Boot time** is the part of the queue time Mayfly controls: its own queue, the JIT registration, the container start and the runner connecting. It uses mayfly's clock. Queue time minus boot time is roughly webhook delivery plus GitHub's own dispatch. M4 (pre-baked image, warm pool) sets out to shrink boot time.
- **Job duration** is the job itself, from GitHub's timestamps. M5 (caches, mirrors, tmpfs) sets out to shrink it.

## mayfly

| Name | Type | Unit | Labels | Meaning |
|---|---|---|---|---|
| `mayfly_job_queue_seconds` | histogram | seconds | | Time a job waited for a runner, GitHub `created_at` to `started_at`. Recorded on `in_progress`. |
| `mayfly_worker_boot_seconds` | histogram | seconds | | Time from mayfly accepting the `queued` event to the `in_progress` event of the runner it started. Only jobs this process tracks. |
| `mayfly_job_duration_seconds` | histogram | seconds | | Time a job ran, GitHub `started_at` to `completed_at`. Recorded on `completed` when a runner took the job. |
| `mayfly_jobs_total` | counter | jobs | `conclusion` | Completed jobs, by GitHub conclusion (`success`, `failure`, `cancelled`, ...). |
| `mayfly_provision_failures_total` | counter | jobs | `stage` | Jobs mayfly gave up provisioning: `jit_config` (runner registration) or `worker` (container). Such a job stays queued on GitHub and the reaper reports it as stuck. |
| `mayfly_retries_total` | counter | attempts | `op` | Retried attempts after a temporary failure: `jit_config` (GitHub) or `worker_start` (Docker). |
| `mayfly_workers_alive` | gauge | workers | | Workers this process started that are not gone yet. |
| `mayfly_jobs_waiting` | gauge | jobs | | Accepted jobs waiting for a free worker slot (`MAYFLY_WORKERS`). |

Histogram buckets: queue and boot time use 1, 2, 5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 300 and 600 seconds; job duration uses 10 seconds up to 6 hours, GitHub's job limit.

Events for jobs mayfly no longer tracks still count towards queue time, duration and `jobs_total`: a `completed` event often arrives after the worker exited and mayfly stopped tracking it. After a restart, jobs in flight have no boot time.

## reaper

| Name | Type | Unit | Labels | Meaning |
|---|---|---|---|---|
| `mayfly_reaper_workers_removed_total` | counter | workers | `reason` | Orphaned workers removed: `max_age`, `owner_gone`, `owner_unreachable`, `runner_gone`, `runner_offline`. |
| `mayfly_reaper_registrations_removed_total` | counter | registrations | | Stale `mayfly-*` runner registrations deleted on GitHub. |
| `mayfly_reaper_stuck_jobs_total` | counter | jobs | `reason` | Jobs stuck in `queued`, each counted once: `not_picked_up` or `mayfly_unreachable` (decision 013). |
| `mayfly_reaper_sweep_failures_total` | counter | sweeps | | Sweeps that could not list workers and did nothing. |
| `mayfly_reaper_last_sweep_timestamp_seconds` | gauge | Unix seconds | | When the last sweep listed workers. `time() - mayfly_reaper_last_sweep_timestamp_seconds` growing past the interval means the reaper is stuck. |

## Runtime metrics

Both processes also expose the standard Go runtime (`go_*`) and process (`process_*`) metrics: goroutines, memory, CPU, open file descriptors.

## Example queries

| Question | PromQL |
|---|---|
| Median queue time, last hour | `histogram_quantile(0.5, sum by (le) (rate(mayfly_job_queue_seconds_bucket[1h])))` |
| p95 boot time | `histogram_quantile(0.95, sum by (le) (rate(mayfly_worker_boot_seconds_bucket[1h])))` |
| Job failure rate | `sum(rate(mayfly_jobs_total{conclusion="failure"}[1h])) / sum(rate(mayfly_jobs_total[1h]))` |
| Orphans removed per day | `sum by (reason) (increase(mayfly_reaper_workers_removed_total[1d]))` |
