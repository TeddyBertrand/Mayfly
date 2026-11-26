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

| workflow | GitHub-hosted | Mayfly | Mayfly with the worker image |
|---|---|---|---|
| go-build | 46.5s | 85s | 49s |
| npm-test | 11s | 31s | 17.5s |

The last column is release 0.3.6 with Go and Node baked into the worker image, same host and harness ([details](docs/benchmarks.md#pre-baked-worker-image-of-2026-11-25-azure)); the first two are the baseline above, hosted numbers from run A.

**Status:** M1 core loop works: a `workflow_job` webhook starts a one-job Docker runner that is removed afterwards.
