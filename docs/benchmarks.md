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

These local numbers check the harness, not Mayfly: the baseline on the 2 vCPU Azure host is below.

## Baseline of 2026-11-25 (Azure, unoptimized Mayfly)

The starting point every M4 and M5 optimization is measured against.

| | |
|---|---|
| Host | Azure `Standard_B2als_v2` (2 vCPU AMD, 4 GB), Denmark East, one Docker host (`MAYFLY_WORKERS=1`) |
| Mayfly | image `0.2.6`, no optimization: fresh worker per job, nothing cached, `setup-go` and `setup-node` download their toolchain every run |
| Harness | commit `436bba6`, `hosted,mayfly`, 2 warm-up + 10 measured iterations per cell |
| Runs | two full runs one after the other: [a](../bench/results/2026-11-25-azure-a.md) ([CSV](../bench/results/2026-11-25-azure-a.csv)) and [b](../bench/results/2026-11-25-azure-b.md) ([CSV](../bench/results/2026-11-25-azure-b.csv)); 0 failed jobs out of 96 |

Median total time (queue + boot + steps), run A and run B:

| workflow | runner | A | B | change |
|---|---|---|---|---|
| go-build | hosted | 46.5s | 47s | 1.1% |
| go-build | mayfly | 85s | 85.5s | 0.6% |
| npm-test | hosted | 11s | 13s | 18.2% |
| npm-test | mayfly | 31s | 32s | 3.2% |

First step starts after (median, A / B): hosted 5-5.5s, mayfly 9.5-10s. Queue time: hosted 3s, mayfly 7s.

Mayfly is about 1.8x slower than hosted on `go-build` and about 2.8x on `npm-test`. That is the gap M4 and M5 have to close.

### Repeatability

`bench compare` exits with status 2: three cells are within ±10% (0.6% to 3.2%), but hosted `npm-test` moved 18.2%. That is 2 s on an 11-13 s job, so it is close to the 1 s timestamp resolution, and it is on the hosted side, which Mayfly does not control (its own `npm-test` cell moved 3.2%). The Mayfly cells, the ones the baseline is for, repeat well: `go-build` ran between 84 s and 90 s over 20 measured jobs. The claim therefore holds for Mayfly and fails for one noisy hosted cell. The tolerance stays at 10%.

### CPU credits: no throttling

B-series VMs earn CPU credits while idle and spend them under load; an empty balance throttles the VM to its baseline share of CPU and would make a long benchmark look slow for the wrong reason. Azure Monitor shows the balance over the two runs (07:35 to 09:20 UTC):

| metric | value |
|---|---|
| CPU Credits Remaining | 60.8 at the start, 67.3 at the end, rising |
| Average CPU | 24-31% |

The balance never dropped, so the VM was not throttled and the numbers measure Mayfly, not the VM size.

### Why `go-build` is slower on Mayfly

Step durations of one job per runner type (run A), in seconds:

| step | hosted | mayfly |
|---|---|---|
| `actions/setup-go` | 7 | 34 |
| `go build` | 16 | 23 |
| `go vet` | 4 | 7 |
| `go test` | 4 | 6 |
| everything else | 9 | 5 |

Two causes, both visible here:

1. **Toolchain download.** A hosted runner ships Go and Node in its tool cache, so `setup-go` takes 7 s and `setup-node` under 1 s. A fresh Mayfly worker has an empty cache and downloads the toolchain each time: 34 s for Go, 15 s for Node. This is about 27 s of the 38 s `go-build` gap and about 15 s of the 20 s `npm-test` gap. It is the price of a clean environment per job, and it is what the pre-baked worker image (M4) targets.
2. **Slower CPU.** The same compile steps take about 1.5x longer (`go build` 16 s against 23 s). The host has 2 shared vCPUs and the worker shares them with the reaper, Prometheus and Grafana. Locally, on 20 cores, `go-build` on Mayfly took 55 s against 50 s hosted.

Boot adds about 4 s (queue 7 s against 3 s, first step 10 s against 5.5 s): the container start and the runner registering.

This breakdown comes from one job per runner type in run A, so it shows where the time goes, not an exact split.

## Pre-baked worker image of 2026-11-25 (Azure)

The first M4 step, measured alone (decisions 022 to 024): the same workloads on the same host with the worker image of release 0.3.6, which has Go 1.27.4 and Node 24.22.3 in its tool cache.

| | |
|---|---|
| Host | the baseline's: Azure `Standard_B2als_v2` (2 vCPU AMD, 4 GB), Denmark East, `MAYFLY_WORKERS=1` |
| Mayfly | image `0.3.6` with `ghcr.io/teddybertrand/mayfly-worker:0.3.6`; the baseline ran `0.2.6` with the stock runner. Releases 0.3.0 and 0.3.5 changed no behaviour, so the image is the only change that matters here |
| Harness | commit `182327c`, `hosted,mayfly`, 2 warm-up + 10 measured iterations per cell, same as the baseline |
| Runs | two full runs one after the other: [a](../bench/results/2026-11-25-azure-image-a.md) ([CSV](../bench/results/2026-11-25-azure-image-a.csv)) and [b](../bench/results/2026-11-25-azure-image-b.md) ([CSV](../bench/results/2026-11-25-azure-image-b.csv)); 0 failed jobs out of 96 |

Median total time of Mayfly, baseline against the image (run A / run B):

| workflow | baseline | with the image | change |
|---|---|---|---|
| go-build | 85s / 85.5s | 49s / 49s | -42% / -43% |
| npm-test | 31s / 32s | 17.5s / 17s | -44% / -47% |

Against GitHub-hosted runners in the same runs, `go-build` on Mayfly is now about on par (49s against 47s and 48s, from 1.8x slower), and `npm-test` about 1.3x slower (17.5s and 17s against 13s, from 2.8x).

**Startup did not move.** Queue time is 6-7s and the first step starts at 9-10s, as in the baseline. The image removes the toolchain download from the steps, but a container starts as fast as before. The brief's under-10s first step is the warm pool's target, not this image's.

**One step explains the gain.** In a smoke job on this host `setup-go` took 1s, against 34s in the baseline; `setup-node` was 0s in a local run, against 15s. The compile, vet and test steps are unchanged.

### Repeatability

`bench compare` between a and b exits with status 0: all four cells are within ±10%. The Mayfly cells moved 0.0% (`go-build`) and 2.9% (`npm-test`), and `go-build` ran between 48s and 50s over 20 measured jobs.

### CPU credits: no throttling

Azure Monitor over the two runs (13:30 to 14:46 UTC): CPU Credits Remaining rose from 60.5 to 66.4, and average CPU stayed at 24-31%. The VM was not throttled.

### What it costs

| | stock runner | worker image |
|---|---|---|
| Image size on disk | 2.18 GB | 2.83 GB (+645 MB, +30%) |
| First pull on the host | | about 1 minute (the first job queued 68s against the usual 7s) |

The pull happens once per host, and the warm-up iterations of a benchmark never include it because it is paid by the first job. Every release also rebuilds and pushes the image (decision 023).

### What this does not show

Only Go and Node are baked in, and the workloads only use those two, so Rust, C and C++ jobs are not measured and are not faster yet. The remaining `npm-test` gap against hosted (about 4s) is slower install and test steps on 2 shared vCPUs, which caches and tmpfs address in M5. This is two runs on one host on one day.

## Warm pool of 2026-11-26 (Azure), alone and with the worker image

The second M4 step (decisions 025 and 026), measured alone and then combined with the worker image.

| | |
|---|---|
| Host | the baseline's: Azure `Standard_B2als_v2` (2 vCPU AMD, 4 GB), Denmark East, `MAYFLY_WORKERS=1` |
| Mayfly | image `0.3.9`, warm pool on the demo repo with `MAYFLY_POOL_SIZE=1` and the default cap of 2, idle timeout 30m |
| Pool alone | `worker_image` set to the stock `ghcr.io/actions/actions-runner:2.338.2` |
| Combined | the default `ghcr.io/teddybertrand/mayfly-worker:0.3.9`; changing the image replaced the VM |
| Harness | commit `1a4328e`, `hosted,mayfly`, 2 warm-up + 10 measured iterations per cell, as before |
| Runs | two full runs per setup. Pool alone: [a](../bench/results/2026-11-26-azure-pool-a.md) ([CSV](../bench/results/2026-11-26-azure-pool-a.csv)) and [b](../bench/results/2026-11-26-azure-pool-b.md) ([CSV](../bench/results/2026-11-26-azure-pool-b.csv)). Combined: [a](../bench/results/2026-11-26-azure-image-pool-a.md) ([CSV](../bench/results/2026-11-26-azure-image-pool-a.csv)) and [b](../bench/results/2026-11-26-azure-image-pool-b.md) ([CSV](../bench/results/2026-11-26-azure-image-pool-b.csv)). 0 failed jobs out of 192 |

Each run started only once the host's warm runner was online on GitHub.

### Each step against the baseline

Medians of Mayfly, run A / run B. The baseline and the image alone are the runs of 2026-11-25 above.

| | Baseline | Image alone | Pool alone | Combined |
|---|---|---|---|---|
| Queue | 7s / 7s | 7s / 6s | 2s / 2s | 2s / 2s |
| First step, both workflows | 10s / 9.5-10s | 9.5-10s / 9s | **5s / 5s** | **5s / 5s** |
| go-build total | 85s / 85.5s | 49s / 49s | 83s / 84.5s | **45s / 45s** |
| npm-test total | 31s / 32s | 17.5s / 17s | 28s / 29s | **13s / 13s** |

- **First step under 10s: met.** With the pool it starts after 5s at the median in all four runs, and the highest p90 of any Mayfly cell is 7s. The baseline sat at 9.5-10s, so the real result is that startup roughly halved.
- **The two steps are separate gains.** The pool removes the boot from the wait and barely changes total time beyond that (go-build -1 to -2%, npm-test about -10%), since a stock runner still downloads Go and Node in every job. The image shortens the steps but did not move startup. Only the two together do both: against the baseline, go-build is 47% faster and npm-test 58-59%.

The image-alone runs used 0.3.6 and these use 0.3.9, which adds the pool and its metrics.

### Against GitHub-hosted runners in the same runs

Median total time with the pool and the image, run A / run B:

| workflow | GitHub-hosted | Mayfly | hosted min-max (A, B) | Mayfly min-max (A, B) |
|---|---|---|---|---|
| go-build | 48s / 52.5s | 45s / 45s | 36-54s, 36-67s | 44-46s, 44-48s |
| npm-test | 13s / 14s | 13s / 13s | 10-21s, 10-26s | 12-22s, 12-16s |

The claim these numbers support is **on par with GitHub-hosted runners on these two workflows**. `npm-test` is tied within timestamp resolution. Mayfly's `go-build` median is 3s and 7.5s lower, but hosted times spread widely and their fastest runs beat every Mayfly run, so this is not evidence that Mayfly is faster in general. Its `go-build` times were much steadier.

### What the pool did

Mayfly's own counters and logs, saved before each VM was removed:

| | Pool alone | Combined |
|---|---|---|
| Pool requests | 48 hits, 0 misses | 48 hits, 0 misses |
| Jobs run on a `mayfly-warm-*` runner (CSV) | 48 of 48 | 48 of 48 |
| Mean claim time | 4.6s | 5.0s |
| Cold boots, provision failures | 0, 0 | 0, 0 |
| `WARN` or `ERROR` log lines | 0 | 0 |

Claim time is longer than the 2s queue time because it ends when the `in_progress` webhook reaches Mayfly ([metrics.md](metrics.md)). On the combined host the reaper also removed the offline registration left by the replaced VM. No runner idled long enough to be retired, so retirement was not exercised on Azure.

### Repeatability

`bench compare` exits with status 0 for both setups, with all four cells within ±10%. Pool alone moved 1.1% to 7.7%. Combined moved 0.0% on both Mayfly cells, and 9.4% and 7.7% on the hosted ones.

### CPU credits: partly covered

Azure Monitor's readings cover about one hour of each setup, not the whole of it, because the queries set no end time and the VM was destroyed before this was noticed.

| Setup | Readings (UTC) | CPU Credits Remaining | Average CPU |
|---|---|---|---|
| Pool alone (runs 11:52-13:21) | 15-minute points 11:45-12:30 | 60.6 rising to 63.2 | 26-28% |
| Combined (runs 13:26-14:34) | 15-minute points 13:30-14:00 | 61.1 rising to 62.5 | 25-29% |

Nothing points to throttling: the balance stayed high where it was read, and each run B matched its run A. The later part of each run B has no reading.

### What this does not show

- **The pool at its best.** Jobs ran one at a time with a hosted job in between, so the pool had time to refill. Bursts, misses and the cap were not measured here.
- **What the pool costs.** An idle runner holds memory and a live registration while it waits; neither was measured.
- **Readiness.** The pool counts a runner as idle once it is registered, before its container is running and connected. A job could then reserve a runner that is still pulling its image. These runs waited for the runner to be online before starting.
- Two small workflows, one host size, one day.

## Local cache server of 2026-11-27 (Azure)

The first M5 step (#32, decision 028): `actions/cache` traffic from workers goes to a cache server on the host instead of GitHub's cache service. Measured on the cached variants of the two workloads, `go-build-cached` and `npm-test-cached`, which differ from the originals only by their cache inputs ([demo-repo.md](demo-repo.md)).

| | |
|---|---|
| Host | the baseline's: Azure `Standard_B2als_v2` (2 vCPU AMD, 4 GB), Denmark East, `MAYFLY_WORKERS=1` |
| GitHub cache | release `0.4.0`, no cache server: workers cache to GitHub's service, as hosted runners do. `hosted,mayfly` |
| Local cache | release `0.4.1` with `cache_enabled = true`; changing it replaced the VM. `mayfly` only |
| Local cache and pool | release `0.4.1`, cache and warm pool on the demo repo (`MAYFLY_POOL_SIZE=1`); replaced the VM again. `mayfly` only |
| Harness | 2 warm-up + 10 measured iterations per cell, as before. The warm-ups fill the cache, so measured runs are cache hits |
| Runs | one run per setup: [GitHub cache](../bench/results/2026-11-27-azure-cache-github.md) ([CSV](../bench/results/2026-11-27-azure-cache-github.csv)), [local cache](../bench/results/2026-11-27-azure-cache-local.md) ([CSV](../bench/results/2026-11-27-azure-cache-local.csv)), [local cache and pool](../bench/results/2026-11-27-azure-cache-pool.md) ([CSV](../bench/results/2026-11-27-azure-cache-pool.csv)). 0 failed jobs out of 96 |

Hosted runners do not use Mayfly, so they were measured once, in the first setup, and serve as the reference for all three.

### Cache hit on the local server

Before the second setup's run, `go-build-cached` ran twice on Mayfly. The first job missed although GitHub's cache already held the same key from the first setup, so it never asked GitHub, and it saved a 29 MB entry into the cache server's volume. The second logged `Cache hit for: setup-go-…` and `Cache restored successfully`.

### Results

Medians, with the range of total time. Work is total minus first step: the time the job's steps take once it runs.

| | Hosted | Mayfly, GitHub cache | Mayfly, local cache | Mayfly, local cache and pool |
|---|---|---|---|---|
| Queue | 2.5-3s | 6.5-7s | 6-7s | **2s** |
| First step | 4-5.5s | 9.5-10s | 9-10s | **5s** |
| go-build-cached work | 12.5s | 10s | **8s** | **8s** |
| npm-test-cached work | 9s | 10s | **8s** | **7s** |
| go-build-cached total | 17s (10-26s) | 20s (19-25s) | 18s (17-19s) | **13s (13-14s)** |
| npm-test-cached total | 13.5s (10-25s) | 19s (19-21s) | 17s (16-18s) | **12s (12-13s)** |

- **Caching is the large step.** The uncached `go-build` took 49s with the worker image alone (2026-11-25); cached, it takes 18-20s, since modules and build outputs are restored instead of downloaded and compiled.
- **The local server saves about 2s per job** over GitHub's cache on both workflows, all of it in the work: the restore stays on the host.
- **With the pool, Mayfly is ahead of hosted runners in these runs:** 13s against 17s on `go-build-cached` (24% less) and 12s against 13.5s on `npm-test-cached` (11% less). Startup is level (5s against 4-5.5s), and the work is faster and much steadier: Mayfly's totals span 1s, hosted ones 15s.
- **Against the brief's 30-50% target**, the medians fall short (11-24%). Against hosted p90 (24s and 19s), Mayfly is 42% and 32% faster. The registry mirror, package proxy and tmpfs steps of M5 are still to come.

### Deploy bug found

On both fresh hosts with `cache_enabled = true`, `compose up` failed: the `cache` service joins the external `mayfly-workers` network, which only mayfly created, at the first job. It was worked around on the host by creating the network with its bridge name and restarting `mayfly.service`, then fixed in the deploy script (#156) and released as 0.4.2.

### What this does not show

- **Repeatability.** Each setup ran once, so `bench compare` was not run. The Mayfly cells had narrow ranges (1-6s), but a second run would be needed to claim the ±10% of earlier sections.
- **CPU credits.** Azure Monitor was not read for these runs.
- **A cold cache.** Measured runs are all hits on a cache filled by the warm-ups. A first run after a lockfile change pays the full download plus the save.
- **Isolation.** The server checks each job's token and scopes entries per repository and branch; that is the subject of #33 and was not tested here.
- Two small workflows, one host size, one day.

## Worker tmpfs of 2026-11-27/28 (Azure)

The second M5 step (#36, decision 029): each worker keeps `_work`, `/tmp` and the Go and npm caches in memory under a 1500m budget, instead of overlayfs on the VM's disk. Measured with and without it, on top of the local cache and the warm pool, on all four workloads.

| | |
|---|---|
| Host | the baseline's: Azure `Standard_B2als_v2` (2 vCPU AMD, 4 GB), Denmark East, `MAYFLY_WORKERS=1` |
| tmpfs off | release `0.4.3`, `cache_enabled = true`, warm pool on the demo repo |
| tmpfs on | release `0.4.4`, the same plus `worker_tmpfs_size = "1500m"`; changing it replaced the VM. 0.4.4 differs from 0.4.3 only by the `exec` mount option, which has no effect with tmpfs off |
| Harness | `mayfly` only, `go-build,npm-test,go-build-cached,npm-test-cached`, 2 warm-up + 10 measured iterations per cell |
| Memory | a script on the host read each worker's cgroup `memory.current` and the `shmem` line of `memory.stat` every second; the peak per container is used |
| Runs | [off](../bench/results/2026-11-27-azure-tmpfs-off.md) ([CSV](../bench/results/2026-11-27-azure-tmpfs-off.csv), [memory](../bench/results/2026-11-27-azure-tmpfs-off-mem.log)) and [on](../bench/results/2026-11-28-azure-tmpfs-on.md) ([CSV](../bench/results/2026-11-28-azure-tmpfs-on.csv), [memory](../bench/results/2026-11-28-azure-tmpfs-on-mem.log)). 0 failed jobs out of 96, all 80 measured jobs on warm runners |

### A first run that failed

The first run with tmpfs, on 0.4.3, failed 40 of 40 jobs. Docker mounts tmpfs `noexec` by default, so `npm test` could not run `vitest` from `node_modules/.bin` and `go test` could not run its test binaries in `/tmp`. The local check before release had built a binary but never run one. The mounts now add `exec` (#166, release 0.4.4), which restores what the disk allowed; `nosuid` and `nodev` stay.

### Results

Medians, with the range of total time. Work is total minus first step.

| workflow | total, off | total, on | work, off | work, on |
|---|---|---|---|---|
| go-build | 49s (49-50s) | 48s (47-48s) | 44s | 43.5s |
| npm-test | 13.5s (12-22s) | 12s (12-13s) | 8.5s | 8s |
| go-build-cached | 14s (13-14s) | 13s (12-13s) | 9s | 8s |
| npm-test-cached | 13s (12-14s) | 12s (12-13s) | 8s | 8s |

| Memory per worker (peak) | off | on |
|---|---|---|
| Total, median / max | 307 MB / 689 MB | 293 MB / 660 MB |
| Shared memory (tmpfs), median / max | 44 MB / 45 MB | 176 MB / 247 MB |

- **The gain is about 1s per job, at the limit of what the timestamps resolve.** Every cell moved down by 0-1.5s, mostly in the work, but timestamps have 1s resolution, so the claim is "up to about 1s", not a percentage.
- **The page cache was already doing most of the work.** Without tmpfs, the files a job writes stay in memory too, and are counted in the container's memory, which is why the total peak barely changes. The disk was not the bottleneck on these workloads.
- **What tmpfs changes is that the memory is pinned.** Page cache can be reclaimed under pressure; tmpfs pages cannot while the job runs. With tmpfs, up to 247 MB per job was held that way, within a 1.5 GB cap.
- **Decision:** tmpfs stays an off-by-default option. It may pay on jobs that write much more, or on a slower disk, which these runs do not cover.

### What this does not show

- **Repeatability.** One run per setup, as for the cache. The differences are close to the noise floor, so a second run could move them by 1s either way.
- **Heavier I/O.** Both workloads write tens to a few hundred MB. A large monorepo build or many parallel jobs could behave differently.
- **Memory pressure.** With `MAYFLY_WORKERS=1` the host never ran short; the cost of pinned memory with several workers at once was not measured.

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
