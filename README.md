# Mayfly

Ephemeral GitHub Actions runners that live for exactly one job.

Mayfly is a self-hosted, autoscaling runner system: each job runs on a fresh, short-lived worker that is destroyed afterwards. The goal is faster and cheaper CI than GitHub-hosted runners, with reproducible infrastructure and published benchmarks.

See [brief.md](brief.md) for goals, architecture and scope.

## Repository layout

```
cmd/
  mayfly/          entrypoint: receiver + provisioner in one process
  reaper/          entrypoint: cleans up leaked or stuck workers
  bench/           benchmark harness: runs the demo workflows, summarizes timings
internal/
  config/          settings from MAYFLY_* environment variables
  receiver/        webhook handling, signature verification, dedupe
  provisioner/     job queue and worker pool
  github/          GitHub App client (JWT, installation tokens, JIT config)
  worker/          worker interface
  worker/docker/   Docker backend over the Engine API
  reaper/          orphan detection and cleanup
  bench/           benchmark schedule, timings, statistics and reports
bench/
  demo/            canonical copy of the demo repo the benchmarks run on
  results/         benchmark CSVs and summaries
```

The receiver and provisioner share one binary so they can pass jobs through an in-memory queue ([decision 001](docs/design-decisions.md)). Each component keeps its own `internal/` package, so splitting them later stays cheap. `internal/` keeps the code private to this module.

Build everything with:

```bash
go build ./...
```

To run Mayfly locally and send it real jobs from GitHub, follow [docs/local-dev.md](docs/local-dev.md). How Mayfly is measured against GitHub-hosted and stock self-hosted runners is in [docs/benchmarks.md](docs/benchmarks.md).

Baseline of 2026-11-25, unoptimized Mayfly 0.2.6 on an Azure `Standard_B2als_v2` (2 vCPU), median total time over 10 runs ([details](docs/benchmarks.md#baseline-of-2026-11-25-azure-unoptimized-mayfly)):

| workflow | GitHub-hosted | Mayfly | with the worker image | with the warm pool | with both |
|---|---|---|---|---|---|
| go-build | 46.5s | 85s | 49s | 83s | 45s |
| npm-test | 11s | 31s | 17.5s | 28s | 13s |

All numbers are from run A of each setup, on the same host size and harness settings. The hosted and plain Mayfly columns are the baseline above. The worker image bakes in Go and Node (release 0.3.6, [details](docs/benchmarks.md#pre-baked-worker-image-of-2026-11-25-azure)). The warm pool is release 0.3.9 ([details](docs/benchmarks.md#warm-pool-of-2026-11-26-azure-alone-and-with-the-worker-image)): it halves the time to a job's first step, from about 10s to 5s. In the same runs as "with both", hosted runners took 48s and 13s, so Mayfly is on par with them on these two workflows, run one job at a time.

With caching (M5, 2026-11-27, [details](docs/benchmarks.md#local-cache-server-of-2026-11-27-azure)), on the cached variants of the same workflows:

| workflow | GitHub-hosted | Mayfly, GitHub cache | with the local cache | with the local cache and warm pool |
|---|---|---|---|---|
| go-build-cached | 17s | 20s | 18s | 13s |
| npm-test-cached | 13.5s | 19s | 17s | 12s |

The local cache server ships in release 0.4.1. With it and the warm pool, Mayfly finished both workflows faster than hosted runners, and its times varied by 1s against 15s on hosted.

**Status:** M1 core loop works: a `workflow_job` webhook starts a one-job Docker runner that is removed afterwards.
