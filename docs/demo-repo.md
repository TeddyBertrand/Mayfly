# Demo repo

The benchmark target is [`TeddyBertrand/mayfly-demo`](https://github.com/TeddyBertrand/mayfly-demo), the only repository the GitHub App is installed on ([github-app.md](github-app.md)). It is private, so its canonical copy lives here in [`bench/demo/`](../bench/demo/) and is pushed to it.

## Workflows

| Workflow | Work | Runner types |
|---|---|---|
| `go-build` | Toolchain setup, `go mod download` (chi, uuid, testify), build, vet, test | hosted, mayfly, classic |
| `npm-test` | Toolchain setup, `npm ci` (vitest, 67 packages), test | hosted, mayfly, classic |
| `mayfly-smoke` | One echo step | mayfly |
| `mayfly-long` | Sleeps 10 minutes, for reaper and shutdown tests | mayfly |

The benchmark workflows are `workflow_dispatch` only and take a `runner` input. It drives the one line that differs between runner types:

```yaml
run-name: go-build on ${{ inputs.runner }}
runs-on: ${{ fromJSON('{"hosted":"ubuntu-24.04","mayfly":["self-hosted","mayfly"],"classic":["self-hosted","classic"]}')[inputs.runner] }}
```

`fromJSON` turns the JSON object into a lookup table and `[inputs.runner]` picks the labels. `classic` is the stock long-lived self-hosted runner from [benchmarks.md](benchmarks.md#runner-types). `run-name` puts the runner type in the run title, so runs are easy to tell apart in the UI. Mayfly ignores the `workflow_job` events of hosted and classic runs, because their labels do not include `mayfly` (`MAYFLY_RUNNER_LABELS`).

```bash
gh workflow run go-build -R TeddyBertrand/mayfly-demo -f runner=mayfly
gh run list -R TeddyBertrand/mayfly-demo --workflow go-build
```

## Keeping the comparison fair

All runner types do exactly the same work:

- **Same OS.** `ubuntu-24.04` on the hosted side; the worker image (`actions-runner`), which the classic runner also uses, is Ubuntu 24.04 too.
- **Pinned toolchains.** Go `1.27.4` and Node `24.22.3`, so a new release between runs does not change the work.
- **No caching.** `setup-go` has `cache: false` and `setup-node` gets no `cache` input, so every run downloads modules and packages. This is the unoptimized baseline; caching is an M4 optimization, measured separately. The classic runner is the one exception by nature: it keeps its tool cache (`_work/_tool`) between jobs, so `setup-go` and `setup-node` download only once. That persistent state is what it stands for in the comparison.
- **No cgo, no `-race`.** The worker image has no C compiler, so `CGO_ENABLED=0` on both sides.
- **Private repo.** Hosted runners for private repositories have 2 vCPU and 7 GB, close to the 2 vCPU `Standard_B2als_v2` benchmark host. Public repositories get 4 vCPU, and a public repo would also let outside contributors aim workflows at the self-hosted runners.

## Not covered yet

A Docker build workload. Workers have no Docker daemon: the socket is root-equivalent on the host (decision 015), so a `docker build` job would only pass on hosted runners. It needs a sandboxed builder in the worker first.

## Syncing to the demo repo

Change the files in `bench/demo/` first, then copy them over a clone of the demo repo. `--delete` makes the clone match exactly, so a workflow removed here is removed there.

```bash
git clone git@github.com:TeddyBertrand/mayfly-demo.git /tmp/mayfly-demo
rsync -a --delete --exclude .git bench/demo/ /tmp/mayfly-demo/
git -C /tmp/mayfly-demo status
```

Then commit and push in the clone. To check the workloads locally before pushing:

```bash
(cd bench/demo/go && CGO_ENABLED=0 go test ./...)
(cd bench/demo/node && npm ci && npm test)
```

`bench/demo/go` is its own Go module, so Mayfly's `go build ./...`, tests and lint never include it.
