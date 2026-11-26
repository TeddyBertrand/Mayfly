# Metrics

Mayfly exposes Prometheus metrics on each process's ops listener, which is never published (decisions 018 and 020). All names, units and buckets are defined in `internal/metrics`.

| Process | Endpoint | Local default | In Compose |
|---|---|---|---|
| mayfly (receiver, provisioner) | `GET /metrics` | `127.0.0.1:8081` (`MAYFLY_OPS_ADDR`) | `mayfly:8081` |
| reaper | `GET /metrics` | `127.0.0.1:8082` (`MAYFLY_REAPER_OPS_ADDR`) | `reaper:8082` |

```bash
curl -s 127.0.0.1:8081/metrics | grep ^mayfly_
```

## Prometheus and Grafana

The Compose deploy runs both next to mayfly and the reaper (decision 021). Their config lives in `infra/observability/`:

| File | What it does |
|---|---|
| `prometheus/prometheus.yml` | Scrapes `mayfly:8081` and `reaper:8082` every 15 s over the Compose network |
| `grafana/provisioning/datasources/prometheus.yml` | Adds Prometheus as the default data source, uid `prometheus` |
| `grafana/provisioning/dashboards/mayfly.yml` | Loads every JSON in `grafana/dashboards/` into the Mayfly folder |
| `grafana/dashboards/mayfly.json` | The Mayfly dashboard, also Grafana's home page |

```
mayfly:8081 ─┐
             ├──scrape 15s──► prometheus:9090 ◄──query── grafana:3000 ──► 127.0.0.1:3000 on the host
reaper:8082 ─┘                (not published)                            (SSH tunnel on the VM)
```

- **Open Grafana.** Locally: <http://127.0.0.1:3000>, or another loopback port set with `GRAFANA_PUBLISH` in `.env` when 3000 is taken. On the VM: `ssh -L 3000:localhost:3000 <host>`, then the same URL on your machine. Nothing is public.
- **Memory.** Idle, Grafana uses about 230 MB plus file cache and Prometheus about 40 MB; Compose caps them at 512 MB and 256 MB. On the 1 GB VM that leaves room for one worker (`workers = 1`); benchmarks use the 4 GB size.
- **Login.** None: anonymous users are Viewers and the login form is disabled, which is safe only because the port is on loopback.
- **Change a dashboard.** Set `GRAFANA_ROLE=Editor` in `.env` and run `docker compose up -d grafana`, edit in the UI, then export the dashboard JSON (keeping the data source uid `prometheus`, not an external-sharing placeholder) into `grafana/dashboards/`. Provisioned dashboards cannot be saved from the UI, so the repo stays the source of truth.
- **Check the scrape.** The dashboard's "Scrape targets up" panel shows `up` for mayfly and the reaper. Without a published port, Prometheus's own UI is reached with `docker compose exec prometheus wget -qO- 'localhost:9090/api/v1/targets'`.
- **Retention.** 15 days (`PROMETHEUS_RETENTION`) or 1 GB, whichever comes first. On Azure the data goes with the VM on `terraform destroy`, so capture screenshots and numbers first.

## Where the time goes

```
GitHub:  created_at ─────────────────────── started_at ───────────────── completed_at
                   │◄──── mayfly_job_queue_seconds ───►│◄─ mayfly_job_duration_seconds ─►│
mayfly:     queued event accepted ─────── in_progress event
   cold            │◄─ mayfly_worker_boot_seconds ─►│
   warm            │◄─ mayfly_pool_claim_seconds ──►│
```

- **Queue time** is what a developer feels: how long the job waited for a runner. It uses GitHub's timestamps, the same ones the benchmark harness reads through the API.
- **Claim time** is the warm path's counterpart of boot time: from a job reserving a warm runner to the `in_progress` event of a warm runner. Nothing boots, so it holds GitHub's assignment and the webhook delivery (decision 027).
- **Boot time**, for cold starts only, is the part of the queue time Mayfly controls: its own queue, the JIT registration, the container start and the runner connecting. It uses mayfly's clock, and ends when the `in_progress` webhook arrives, so it also holds that delivery delay and can come out slightly above queue time. In the first test run (10 jobs, 2 workers) mean boot time was 11.5 s against 11.4 s of queue time: nearly all of the wait was Mayfly's, not GitHub's. M4 (pre-baked image, warm pool) sets out to shrink boot time.
- **Job duration** is the job itself, from GitHub's timestamps. M5 (caches, mirrors, tmpfs) sets out to shrink it.

## mayfly

| Name | Type | Unit | Labels | Meaning |
|---|---|---|---|---|
| `mayfly_job_queue_seconds` | histogram | seconds | | Time a job waited for a runner, GitHub `created_at` to `started_at`. Recorded on `in_progress`. |
| `mayfly_worker_boot_seconds` | histogram | seconds | | Cold starts: time from mayfly accepting the `queued` event to the `in_progress` event of the runner it started. Only jobs this process tracks. |
| `mayfly_job_duration_seconds` | histogram | seconds | | Time a job ran, GitHub `started_at` to `completed_at`. Recorded on `completed` when a runner took the job. |
| `mayfly_jobs_total` | counter | jobs | `conclusion` | Completed jobs, by GitHub conclusion (`success`, `failure`, `cancelled`, ...). |
| `mayfly_provision_failures_total` | counter | jobs | `stage` | Jobs mayfly gave up provisioning: `jit_config` (runner registration) or `worker` (container). Such a job stays queued on GitHub and the reaper reports it as stuck. |
| `mayfly_retries_total` | counter | attempts | `op` | Retried attempts after a temporary failure: `jit_config` (GitHub) or `worker_start` (Docker). |
| `mayfly_workers_alive` | gauge | workers | | Workers this process started that are not gone yet. |
| `mayfly_jobs_waiting` | gauge | jobs | | Accepted jobs waiting for a free worker slot (`MAYFLY_WORKERS`). |

Histogram buckets: queue and boot time use 1, 2, 5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 300 and 600 seconds; job duration uses 10 seconds up to 6 hours, GitHub's job limit.

Events for jobs mayfly no longer tracks still count towards queue time, duration and `jobs_total`: a `completed` event often arrives after the worker exited and mayfly stopped tracking it. After a restart, jobs in flight have no boot time.

## Warm pool

These exist for the repositories in the pool's configuration. Without a pool, the gauges and `mayfly_pool_requests_total` have no series and the claim histogram stays at 0.

| Name | Type | Unit | Labels | Meaning |
|---|---|---|---|---|
| `mayfly_pool_runners` | gauge | runners | `repo`, `state` | Warm runners by state: `pending` (being registered, or a slot held while it backs off after a failure), `idle`, `busy`, `retiring`. Read from the pool at each scrape. |
| `mayfly_pool_max_runners` | gauge | runners | `repo` | Cap on the warm runners alive (`MAYFLY_POOL_MAX`). |
| `mayfly_pool_requests_total` | counter | jobs | `repo`, `result` | Queued jobs of pooled repositories: `hit` (reserved a warm runner), `miss_empty` (none free, below the cap), `miss_max` (none free, at the cap), `miss_unresolved` (installation not known, yet or at all). A redelivered event for a job already reserved or tracked is not counted again. |
| `mayfly_pool_claim_seconds` | histogram | seconds | | Time from a job reserving a warm runner to the `in_progress` event of a warm runner. Buckets from 0.25 to 60 s. |

A hit is a reservation: GitHub chooses which runner gets the job, so another runner can still take it. A job that missed and was then taken by a warm runner has no claim time.

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

## The Mayfly dashboard

`infra/observability/grafana/dashboards/mayfly.json`, Grafana's home page. Each panel's description in Grafana repeats what it shows.

| Row | Panel | Query idea |
|---|---|---|
| Now | Workers alive, Jobs waiting | Current gauge values |
| | Jobs completed, Provision failures | `increase(...[$__range])`: counts over the selected time range, readable with few jobs |
| | Failure rate | `failure` + `timed_out` over completed jobs that were not `cancelled` or `skipped`; "no jobs" when there are none |
| | Scrape targets up | `up` for mayfly and the reaper |
| Waiting for a runner | Queue time, Boot time | p50, p95 and mean, over 5 minutes |
| Warm pool | Pool runners | Runners by state, stacked, with the cap as a dashed line |
| | Pool hit rate | Hits over all pool requests in the selected time range |
| | Pool misses | Misses per minute, stacked by reason |
| | Claim latency | p50, p95 and mean, over 5 minutes |
| Running | Job duration | p50, p95 and mean, over 5 minutes |
| | Jobs by conclusion | Completed jobs per minute, stacked by conclusion |
| | Workers and queue | Workers alive and jobs waiting over time |
| Health | Retries | Retried attempts per minute, by operation |
| | Reaper actions | Workers removed and stuck jobs by reason, stale registrations, failed sweeps, over the range |
| | Since last reaper sweep | `time() - mayfly_reaper_last_sweep_timestamp_seconds` |

Reading it:

- **Percentiles are estimates, the mean is exact.** `histogram_quantile` interpolates inside the bucket that holds the percentile, so with few jobs p50 and p95 jump between bucket edges (a 3 s job in the 0-10 s bucket of job duration reads as 5 s). The mean comes from `_sum / _count` and is exact. Benchmark numbers come from the GitHub API (#25), not from this dashboard.
- **Labelled counters start at 0.** Mayfly creates every known label value (conclusions, stages, operations, reasons, and pool results per pooled repository) at 0 on startup. Otherwise a series would first appear at 1, and `increase()`, which needs an earlier sample, would miss the first job of each kind.
- **Gaps are normal.** With no job in the last 5 minutes, the latency panels have nothing to compute and show a gap, not 0.

## Example queries

| Question | PromQL |
|---|---|
| Median queue time, last hour | `histogram_quantile(0.5, sum by (le) (rate(mayfly_job_queue_seconds_bucket[1h])))` |
| p95 boot time | `histogram_quantile(0.95, sum by (le) (rate(mayfly_worker_boot_seconds_bucket[1h])))` |
| Pool misses by reason, last day | `sum by (result) (increase(mayfly_pool_requests_total{result!="hit"}[1d]))` |
| Median claim time, last hour | `histogram_quantile(0.5, sum by (le) (rate(mayfly_pool_claim_seconds_bucket[1h])))` |
| Job failure rate | `sum(rate(mayfly_jobs_total{conclusion="failure"}[1h])) / sum(rate(mayfly_jobs_total[1h]))` |
| Orphans removed per day | `sum by (reason) (increase(mayfly_reaper_workers_removed_total[1d]))` |
