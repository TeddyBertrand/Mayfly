# Makefile

The [Makefile](../Makefile) is the entry point for everyday work on Mayfly: building and checking the code, running it locally, and testing it by hand against a real GitHub repository. Every target is a thin wrapper around a command you could type yourself; the Makefile only saves remembering the flags.

```
make <target> [VAR=value ...]
```

## At a glance

| Group | Target | What it does | Needs |
|---|---|---|---|
| Code | `make build` | Compiles every package | Go |
| | `make test` | Runs the unit tests | Go |
| | `make fmt-check` | Fails if any file is not `gofmt`-formatted | Go |
| | `make lint` | `fmt-check`, then `golangci-lint` | Go, golangci-lint |
| Run | `make dev` | Mayfly behind a Cloudflare tunnel, webhook pointed at it | Docker, cloudflared, App key |
| | `make reaper` | The reaper, with the same defaults as `make dev` | Docker, App key |
| Jobs | `make smoke` | Runs the short demo job and waits for its result | gh |
| | `make long` | Starts the 10-minute demo job | gh |
| | `make crash` | Kills the `make dev` mayfly with `SIGKILL` | a running `make dev` |
| Inspect | `make workers` | Lists worker containers and their labels | Docker |
| | `make runners` | Lists runner registrations on GitHub | gh |
| | `make check-clean` | Fails unless nothing is left behind | Docker, gh |

## Variables

Set them in the environment or on the command line (`make runners DEMO_REPO=me/other-demo`). A value already set always wins over the default, because the Makefile uses `?=` ("set only if unset"), the same rule as `${VAR:-default}` in [scripts/dev.sh](../scripts/dev.sh).

| Variable | Default | Used by |
|---|---|---|
| `MAYFLY_APP_ID` | `5714458` | `reaper` |
| `MAYFLY_APP_KEY_FILE` | `~/.config/mayfly/app.pem` | `reaper` |
| `DEMO_REPO` | `TeddyBertrand/mayfly-demo` | `smoke`, `long`, `runners`, `check-clean` |

Only the key's **path** is passed on the command line, never its contents, so the key does not show up in shell history or `ps`. The other `MAYFLY_*` settings (worker timeout, reaper interval, max age...) are read straight from the environment by the binaries; see the tables in [local-dev.md](local-dev.md).

## Code targets

These are the same checks CI runs (`make fmt-check test build` in [ci.yml](../.github/workflows/ci.yml), plus `golangci-lint`), so a green local run predicts a green CI run.

- **`build`**: `go build ./...`. Catches compile errors in every package, including binaries with no tests.
- **`test`**: `go test ./...`. Tests that need a Docker daemon skip themselves when none answers. Add `-race` by hand (`go test -race ./...`) when touching concurrent code such as the provisioner or reaper.
- **`fmt-check`**: lists unformatted files and fails. It only reports; run `gofmt -w .` to fix.
- **`lint`**: runs `fmt-check` first, then `golangci-lint run`. Use the same golangci-lint version as CI. If it is not found, add Go's bin directory to your path: `export PATH="$PATH:$(go env GOPATH)/bin"`.

## Run targets

- **`dev`**: runs [scripts/dev.sh](../scripts/dev.sh). It builds mayfly, starts it on `127.0.0.1:8080`, opens a cloudflared quick tunnel, waits until `/healthz` answers through the tunnel, and points the GitHub App's webhook at the new URL. Ctrl-C stops everything gracefully: mayfly removes its own workers. A quick tunnel sometimes fails to come up on the first try (`healthz is not reachable`); run it again.
- **`reaper`**: runs `go run ./cmd/reaper` in the foreground. It sweeps at start, then every minute, and logs one `orphaned worker removed` line per container it removes, with the reason. It finds mayfly through `http://127.0.0.1:8080/healthz`, which is where `make dev` listens. Run it in its own terminal, so it keeps running while mayfly is crashed and restarted.

## Job targets

Mayfly never starts a job itself: GitHub does, when a workflow asks for a runner with the `mayfly` label. These targets trigger the demo workflows in `DEMO_REPO`, which both use `runs-on: [self-hosted, mayfly]` and `on: workflow_dispatch` (run only by hand). They need `make dev` running, or the webhook goes nowhere.

- **`smoke`**: triggers `mayfly-smoke` (an `echo`, a few seconds), waits 3 seconds for GitHub to create the run, then follows that run with `gh run watch --exit-status`. The target fails if the job fails, so `make smoke` alone is a pass/fail check of the whole chain: webhook, JIT config, container, runner.
- **`long`**: triggers `mayfly-long` (`sleep 600`, with a 15-minute job timeout as a safety net) and returns at once. It exists to have a job running long enough to crash mayfly under it.
- **`crash`**: sends `SIGKILL` to the mayfly binary built by `make dev`, matched by its path (`/tmp/mayfly-dev.*/mayfly`), so no other process is touched. `SIGKILL` cannot be caught, so mayfly gets no chance to remove its containers: they keep running with nobody managing them. That is an orphan, on purpose. `dev.sh` notices mayfly is gone, stops the tunnel and exits.

## Inspect targets

- **`workers`**: `docker ps -a` filtered on the `mayfly.job_id` label, showing each container's runner name and `mayfly.owner`, the instance ID of the mayfly that started it. After a crash and restart, the owner shown here no longer matches the instance ID in the new mayfly's logs; that mismatch is what the reaper acts on (decision 012).
- **`runners`**: the runner registrations GitHub holds for `DEMO_REPO`, as `id name status busy`. A healthy idle system shows nothing: JIT runners remove their registration when their job ends.
- **`check-clean`**: counts both and prints them, then fails unless both are zero. This is the "nothing left behind" check from the brief. It also fails, with `cannot reach Docker` or `cannot reach GitHub`, when it cannot look: a check that cannot see must not report "clean".

## Recipes

**Does the whole chain work?**

```
make dev            # terminal 1, leave running
make smoke          # terminal 2: exits 0 on success
make check-clean    # 0 containers, 0 registrations
```

**Does the reaper catch a worker orphaned by a crash?**

```
make dev            # terminal 1
make long           # terminal 2; wait for "worker started" in terminal 1
make workers        # note the owner
make crash          # terminal 1's make dev exits; the container keeps running
make dev            # terminal 1 again: new instance ID
make reaper         # terminal 3: first sweep logs reason="owner instance gone"
make workers        # empty
make check-clean    # 0 containers; the registration is still listed, see below
```

The `mayfly-long` run on GitHub then fails ("lost communication with the server"). That is expected: a job whose worker was orphaned is killed, and must be rerun (decision 012).

## Known gaps

- **Stale registration after a crash.** The reaper removes the container, not the runner registration on GitHub. After the crash recipe, `make runners` still shows the killed runner as `offline`, and `make check-clean` fails. Deleting stale registrations is issue #15. Until then, delete it by hand with the ID from `make runners`: `gh api -X DELETE repos/<DEMO_REPO>/actions/runners/<id>`.
- **One mayfly at a time.** `crash` kills every mayfly started by `make dev`, and the reaper only knows the instance on `127.0.0.1:8080`.
