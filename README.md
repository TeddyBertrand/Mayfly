# Mayfly

Ephemeral GitHub Actions runners that live for exactly one job.

Mayfly is a self-hosted, autoscaling runner system: each job runs on a fresh, short-lived worker that is destroyed afterwards. The goal is faster and cheaper CI than GitHub-hosted runners, with reproducible infrastructure and published benchmarks.

See [brief.md](brief.md) for goals, architecture and scope.

## Repository layout

```
cmd/
  mayfly/          entrypoint: receiver + provisioner in one process
  reaper/          entrypoint: cleans up leaked or stuck workers
internal/
  config/          settings from MAYFLY_* environment variables
  receiver/        webhook handling, signature verification, dedupe
  provisioner/     job queue and worker pool
  github/          GitHub App client (JWT, installation tokens, JIT config)
  worker/          worker interface
  worker/docker/   Docker backend over the Engine API
  reaper/          orphan detection and cleanup
```

The receiver and provisioner share one binary so they can pass jobs through an in-memory queue ([decision 001](docs/design-decisions.md)). Each component keeps its own `internal/` package, so splitting them later stays cheap. `internal/` keeps the code private to this module.

Build everything with:

```bash
go build ./...
```

To run Mayfly locally and send it real jobs from GitHub, follow [docs/local-dev.md](docs/local-dev.md).

**Status:** M1 core loop works: a `workflow_job` webhook starts a one-job Docker runner that is removed afterwards.
