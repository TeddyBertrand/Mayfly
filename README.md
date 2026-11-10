# Mayfly

Ephemeral GitHub Actions runners that live for exactly one job.

Mayfly is a self-hosted, autoscaling runner system: each job runs on a fresh, short-lived worker that is destroyed afterwards. The goal is faster and cheaper CI than GitHub-hosted runners, with reproducible infrastructure and published benchmarks.

See [brief.md](brief.md) for goals, architecture and scope.

## Repository layout

```
cmd/
  receiver/      entrypoint: webhook HTTP server
  provisioner/   entrypoint: creates workers and JIT runner configs
  reaper/        entrypoint: cleans up leaked or stuck workers
internal/
  receiver/      webhook handling and signature verification
  provisioner/   GitHub API client and worker lifecycle
  reaper/        orphan detection and cleanup
```

Each component is its own binary (`cmd/<name>`) with its logic in `internal/<name>`, so they can be deployed and scaled independently. `internal/` keeps the code private to this module.

Build everything with:

```bash
go build ./...
```

**Status:** early planning.
