# Benchmarks

How Mayfly is compared with other runners: what is measured, how, and how much the numbers can be trusted. The harness is [`cmd/bench`](../cmd/bench/main.go), with its logic in [`internal/bench`](../internal/bench/). The workloads are the demo repo's `go-build` and `npm-test` ([demo-repo.md](demo-repo.md)).

## Runner types

Each demo workflow takes a `runner` input, so the same job runs on any of three runner types:

| `runner` | Labels | What it is |
|---|---|---|
| `hosted` | `ubuntu-24.04` | GitHub-hosted runner: a fresh VM per job, 2 vCPU and 7 GB for a private repo |
| `mayfly` | `[self-hosted, mayfly]` | A Mayfly worker: a fresh container per job, started from a JIT config |
| `classic` | `[self-hosted, classic]` | A stock self-hosted runner: one long-lived container, registered once, taking job after job |

`classic` is how most teams self-host before building anything like Mayfly. It runs the same `actions-runner` image on the same host and the same `mayfly-workers` network as Mayfly's workers, so only the lifecycle differs:

```
              isolation per job   startup cost         state between jobs
hosted        fresh VM            GitHub's queue       none
classic       none                ~0 (already up)      everything persists (_work, tool cache)
mayfly        fresh container     container boot       none
```

`classic` keeps `_work/_tool` in its volume, so after the first job `setup-go` and `setup-node` find their toolchain already there. That is not a flaw in the comparison: it is the speed a shared, persistent runner buys, at the cost of one job seeing what the previous one left behind. Mayfly gives that up for a clean environment per job, and M4 sets out to win the speed back without sharing state.

Start and stop it with `make classic-up` and `make classic-down` ([makefile.md](makefile.md#bench-targets)). It is named `classic-runner`, outside Mayfly's `mayfly-` prefix, so the reaper never touches it.

## What is measured

Every number comes from the jobs API (`GET /repos/{repo}/actions/runs/{id}/jobs`): GitHub's clock, the same for every runner type, independent of the host clock (as for the metrics, decision 020).

| Metric | From | To | What it shows |
|---|---|---|---|
| queue | job `created_at` | job `started_at` | How long until a runner picked the job up |
| first step | job `created_at` | `started_at` of the first step after "Set up job" | When the workflow's own work begins: the brief's "first step under 10 s" target |
| total | job `created_at` | job `completed_at` | What a developer waits for |

```
created_at ──queue──► started_at ── "Set up job" ──► first step ──── steps ────► completed_at
    │                                                     │                           │
    └───────────────── first step ───────────────────────┘                           │
    └──────────────────────────────────────── total ─────────────────────────────────┘
```

GitHub's timestamps have **1-second resolution**, so every value is in whole seconds and ±1 s is noise. That matters most for short jobs: on a 17 s `npm-test`, one second is 6%.

The time between the dispatch call and the job's `created_at` (one or two seconds of GitHub-internal work) is the same for every runner type and is not counted.

## How a benchmark runs

`bench run` dispatches each run with `return_run_details`, which makes GitHub return the new run's ID, so no search by name or time is needed. It then polls the run every 5 s until it completes, reads its one job, computes the three durations and appends a CSV row.

- **Sequential.** One job at a time. Mayfly and `classic` share the host, and parallel jobs would compete for its CPUs, so a run is only dispatched once the previous one has completed.
- **Round-robin.** The order is: for each iteration, for each workflow, every runner type in turn (`hosted`, `mayfly`, `classic`, `hosted`...). A slow period on GitHub's side then spreads over all runner types instead of landing on one.
- **Warm-up.** The first 2 iterations of every cell (one workflow on one runner type) are recorded with `warmup=true` and left out of the summary. They pay one-off costs: the first image pull, the first toolchain download on `classic`, cold DNS and disk caches.
- **Run count.** 10 measured iterations per cell by default, so 72 runs for the default 2 workflows, 3 runner types and 2 + 10 iterations: about 45 minutes.
- **Failures.** A run that does not succeed is recorded with its conclusion and no timings, counted in the summary's `failed` column and left out of the statistics.
- **Timeouts.** A run still not completed after 10 minutes is recorded as `timed_out` (a failure) and cancelled, and the benchmark goes on. GitHub occasionally accepts a dispatch and never creates the job: the run stays `queued`, with no jobs and an unevaluated `run-name`. Nothing reaches Mayfly then, and GitHub refuses to cancel such a run (`409`); it expires on its own. Two timeouts in a row stop the benchmark, since that points to a broken setup (Mayfly down, dead tunnel) rather than a glitch. A GitHub error on dispatch also stops it. Either way the CSV keeps every row written so far.
- **Settling.** The runs and jobs endpoints are updated separately: a run can read `completed` while its job still reads `in_progress` for a few seconds. The harness waits until both agree before measuring.

## How the summary is computed

For each cell, over the measured runs that succeeded:

- **Median.** The middle value after sorting; for an even count, the mean of the two middle values (so it can end in .5 s). The median is the headline number because CI timings have a long tail: one slow image pull or GitHub hiccup moves a mean, not a median.
- **p90.** Nearest-rank: the ⌈0.9·n⌉-th sorted value, always a value that was really measured. With n = 10 it is the 9th fastest run: "9 runs out of 10 were at most this".
- **Min and max**, for the spread.

## Repeatability

The claim: **re-running the benchmark gives a median total time within ±10% per cell**. ±10% is tight enough to back up the 30-50% speed-up the brief aims for, and loose enough for the noise of shared hosted runners.

`bench compare a.csv b.csv` checks it: for every cell it computes |median_b − median_a| / median_a and fails (exit status 2) if any cell is above `-tolerance` (default 0.10), or is missing or has no successful runs on one side.

### Check of 2026-11-16 (local)

Mayfly at commit `01303f9` on the owner's machine (WSL2, Docker Desktop, 20 CPUs), `classic` and the Mayfly workers on the same Docker engine, hosted runners from GitHub. Two full runs one after the other with the defaults: [a](../bench/results/2026-11-16-local-a.md) ([CSV](../bench/results/2026-11-16-local-a.csv)) and [b](../bench/results/2026-11-16-local-b.md) ([CSV](../bench/results/2026-11-16-local-b.csv)).

Median total time per cell, with the change between the two runs (`bench compare`, exit status 0):

| workflow | runner | median total A | median total B | change | within ±10% |
|---|---|---|---|---|---|
| go-build | hosted | 52s | 49s | 5.8% | yes |
| go-build | mayfly | 55.5s | 57s | 2.7% | yes |
| go-build | classic | 14s | 14s | 0.0% | yes |
| npm-test | hosted | 18s | 16.5s | 8.3% | yes |
| npm-test | mayfly | 33s | 34s | 3.0% | yes |
| npm-test | classic | 14s | 13.5s | 3.6% | yes |

All six cells are within ±10%, so the claim holds on this check. The worst cell, `npm-test` on `hosted`, moved 8.3%: that is 1.5 s on a 16-18 s job, close to the 1 s timestamp resolution. Margins are thin on the short workflow, so the tolerance should not be tightened.

Five runs ended `timed_out` with no job and no runner (one of them a warm-up run): GitHub accepted the dispatch and never created the job, as described under Timeouts. They hit all three runner types, so Mayfly is not the cause. They are in the CSVs and counted in `failed`, and the medians come from the remaining runs (9 or 10 per cell).

`mayfly` on `go-build` in run B has a 108 s outlier (the median stays 57 s), which is why the median, not the mean, is the headline number.

These local numbers check the harness, not Mayfly: the baseline on the 2 vCPU Azure host is #26.

## Running it

The harness dispatches workflows, which needs a token with Actions write on the demo repo. Your `gh` login has it; the GitHub App deliberately does not (read only), so the harness never uses App credentials.

```bash
make compose-dev      # Mayfly, behind the tunnel, webhook pointed at it
make classic-up       # the stock runner
GH_TOKEN=$(gh auth token) go run ./cmd/bench run -out bench/results/$(date -u +%F)-a.csv
GH_TOKEN=$(gh auth token) go run ./cmd/bench run -out bench/results/$(date -u +%F)-b.csv
go run ./cmd/bench compare bench/results/$(date -u +%F)-a.csv bench/results/$(date -u +%F)-b.csv
```

| Flag | Default | Meaning |
|---|---|---|
| `-repo` | `TeddyBertrand/mayfly-demo` | Repository with the workflows |
| `-workflows` | `go-build,npm-test` | Workflow file names without `.yml` |
| `-runners` | `hosted,mayfly,classic` | Values of the `runner` input |
| `-warmup` | `2` | Warm-up iterations per cell |
| `-runs` | `10` | Measured iterations per cell |
| `-poll` | `5s` | Status check interval |
| `-run-timeout` | `10m` | Give up on one run, record it as `timed_out` |
| `-out` | (required) | CSV file; an existing file is never overwritten |

`bench summary file.csv` reprints the table of an earlier run. The summary goes to stdout and progress logs to stderr, so `> results.md` keeps only the table.

Hosted runs on a private repo use GitHub Actions minutes, billed per job rounded up to the minute: about 24 per default benchmark.

## CSV format

One row per job, in the order run:

`dispatched_at, workflow, runner, iteration, warmup, run_id, job_id, runner_name, conclusion, queue_s, first_step_s, total_s`

`runner_name` is what GitHub reports (`GitHub Actions …`, `mayfly-<job id>-…`, `classic-runner`), so every row can be checked against the runner that really ran it, and `run_id` links it to the run on GitHub.
