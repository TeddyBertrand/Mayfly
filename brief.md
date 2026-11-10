# Project Brief: Mayfly

> Ephemeral GitHub Actions runners that live for exactly one job.

## Summary

Mayfly is a self-hosted, autoscaling GitHub Actions runner system that runs each job in a clean, short-lived worker and is measurably faster and cheaper than GitHub-hosted runners. Built as a DevOps portfolio project: reproducible infrastructure, event-driven design, security thinking, observability, and benchmarks that back up every claim.

## Goals

- Run each GitHub Actions job on a fresh ephemeral worker (one job, then destroyed).
- Reduce job time versus GitHub-hosted runners (target: 30-50% faster on a real repo).
- Reduce queue and startup time (target: first step starts in under 10s).
- Deploy everything from one command (`terraform apply`).
- Publish reproducible benchmarks and a clear security model.

## Non-goals

- Not a commercial multi-tenant product (no billing, no user accounts).
- Not macOS or Windows runners.
- Not a replacement for ARC or existing managed offerings. This is a focused, well-documented implementation.

## Architecture

1. **Webhook receiver** (Go or Python): listens for `workflow_job` events, verifies the signature, handles duplicates idempotently.
2. **Provisioner**: on `queued`, requests a JIT runner config from the GitHub REST API (`generate-jitconfig`) and starts a worker.
3. **Worker**: clean VM or container that runs the official `actions/runner` once, then is destroyed.
4. **Reaper**: cleans up orphaned workers and stuck jobs.
5. **Cache and mirror layer**: local cache server and registry mirror to speed up dependency and image downloads.
6. **Observability**: Prometheus metrics and a Grafana dashboard.

```
GitHub --webhook--> Receiver --> Provisioner --> Worker (JIT runner, 1 job)
   ^                                  |                  |
   |                                  v                  v
   +------- job logs/status <---- Reaper          Cache / Registry mirror
                                      |
                                Prometheus / Grafana
```

## Tech stack (proposed)

- Language: Go (or Python)
- Infra as code: Terraform
- Compute: small bare-metal or cloud server (e.g. Hetzner), Docker first, Firecracker microVMs as a stretch goal
- Observability: Prometheus + Grafana
- CI for the project itself: GitHub Actions running on its own runner

## Optimization roadmap (each step benchmarked)

1. Baseline: stock GitHub-hosted runner timings on a demo repo.
2. Ephemeral worker with JIT config (no optimization).
3. Pre-baked image with toolchains and common Docker images.
4. Warm pool of pre-started workers.
5. Local cache server and registry/package mirror.
6. Fast disk tweaks (tmpfs for hot directories).
7. Stretch: Firecracker microVMs with snapshot/restore.

## Security considerations

- Ephemeral workers only; nothing persists between jobs.
- Workers are network-isolated from internal resources.
- Only private repos or trusted PRs (never untrusted fork PRs on persistent infrastructure).
- Webhook signature verification; least-privilege GitHub App permissions.
- Cache isolation per repo/branch to prevent cache poisoning.
- A short threat-model document in the repo.

## Deliverables

- Working system deployable with one command.
- README with architecture diagram, quick start, and benchmark table with methodology.
- Grafana dashboard (queue time, boot time, job duration, cost per job).
- Threat model and design-decisions document.
- Demo repo with visible workflow runs on the runner.
- 2-minute demo GIF or video.

## Success metrics

- Median queue-to-first-step time.
- Median total job time versus GitHub-hosted on the same workflow.
- Cost per job (infrastructure cost / jobs run).
- Zero orphaned workers after a 24h soak test.

## Timeline estimate

Assumes about 8-10 hours per week (side project). Full-time, divide by roughly 3-4.

| Phase | Scope | Estimate |
|---|---|---|
| 1 | Core loop: webhook, JIT config, ephemeral Docker worker | 1-2 weeks |

| 2 | Terraform deployment, reaper, retries, signature checks | 1-2 weeks |
| 3 | Observability and baseline benchmarks | 1 week |
| 4 | Optimizations: pre-baked image, warm pool, cache and mirror | 2-3 weeks |
| 5 | Security write-up, docs, demo repo, GIF | 1 week |
| 6 | Stretch: Firecracker microVMs | 2-4 weeks (optional) |

**Total: about 6-9 weeks for the solid version (phases 1-5), 8-13 weeks with the Firecracker stretch goal.**

**With AI assistance** (boilerplate, Terraform, docs): roughly 4-6 weeks for phases 1-5 and 6-9 weeks with the Firecracker stretch goal. Benchmarks and real-infrastructure debugging don't shrink much.

## Risks

- GitHub changes self-hosted runner pricing or APIs (the platform fee was announced then suspended; check current status).
- Scope creep: finish phases 1-3 before optimizing.
- Cache protocol changes in the runner/actions ecosystem.
- Cloud cost overruns: set budget alerts and use small instances during development.
