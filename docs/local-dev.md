# Local development

How to run Mayfly on your own machine and have GitHub send it real jobs. At the end, a workflow in `mayfly-demo` runs on a throwaway Docker container started by Mayfly.

```
GitHub ──webhook──► https://<random>.trycloudflare.com ──► cloudflared ──► mayfly :8080 ──► Docker
                     (public URL)                          (outbound       (receiver +       (one container
                                                            connection)     provisioner)      per job)
```

**Why a tunnel:** GitHub pushes webhooks to a public HTTPS URL, and a laptop behind NAT has none. `cloudflared` opens an *outbound* connection to Cloudflare and receives requests for its public URL through it, so no port forwarding or firewall change is needed.

**Shortcut:** once the prerequisites and one-time setup below are done, `make dev` (which runs [scripts/dev.sh](../scripts/dev.sh)) does sections 4 and 5 for you: it builds and starts Mayfly, opens the tunnel, checks `/healthz` through it, and points the App's webhook at the tunnel with `cmd/setwebhook`. Ctrl-C stops both. Logs are kept in a temporary directory it prints. It uses the same `MAYFLY_*` variables, with the App ID and the `~/.config/mayfly/` paths as defaults. The manual steps stay below because they explain what the script does.

## 1. Prerequisites

| Tool | Why | Check |
|---|---|---|
| Go, version from `go.mod` (1.23+) | Build and test | `go version` |
| Docker, with your user allowed on the socket | Workers are containers | `docker ps` runs without `sudo` |
| GitHub CLI `gh`, logged in with the `repo` and `workflow` scopes | Trigger and inspect runs | `gh auth status` |
| `cloudflared` | Tunnel | `cloudflared --version` |
| `golangci-lint` v2.1.6 (same as CI) | `make lint` | `golangci-lint version` |

Install notes:

- **cloudflared** (no root needed): download the `cloudflared-linux-amd64` binary from the [releases page](https://github.com/cloudflare/cloudflared/releases), then `install -m 755 cloudflared-linux-amd64 ~/.local/bin/cloudflared`.
- **golangci-lint**: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6`. It lands in `$(go env GOPATH)/bin`; add that to your `PATH` if `make lint` says it is not found.
- **gh workflow scope**: `gh auth refresh -h github.com -s workflow`. Without it, pushing files under `.github/workflows/` fails with a 404.

## 2. One-time setup

1. **GitHub App.** Create and configure it as described in [github-app.md](github-app.md): Administration read and write, Actions read, `workflow_job` events, installed on `mayfly-demo` only. After any permission change, accept it on the installation page, or every call fails with `403`.
2. **Secrets**, stored outside the repo, readable only by you:

   ```bash
   mkdir -p ~/.config/mayfly && chmod 700 ~/.config/mayfly
   openssl rand -hex 32 > ~/.config/mayfly/webhook-secret
   chmod 600 ~/.config/mayfly/webhook-secret
   ```

   Paste the same value into the App settings (**General → Webhook secret**). Save the App's private key (downloaded once from **General → Private keys**) as `~/.config/mayfly/app.pem` with `chmod 600`. Never commit either file; `.gitignore` excludes `*.pem` as a safety net.
3. **Smoke workflow** in `mayfly-demo`, at `.github/workflows/mayfly-smoke.yml`:

   ```yaml
   name: mayfly-smoke
   on: workflow_dispatch
   jobs:
     hello:
       runs-on: [self-hosted, mayfly]
       steps:
         - run: echo "Hello from $RUNNER_NAME on $(hostname)" && uname -a
   ```

   `workflow_dispatch` means it only runs when triggered by hand.

## 3. Build and test

```bash
gh repo clone TeddyBertrand/Mayfly && cd Mayfly    # the repo is private; gh uses your login
make build test lint
```

The Docker tests in `internal/worker/docker` run against your local daemon and skip if there is none. Two opt-in tests touch real systems:

```bash
MAYFLY_DOCKER_TEST=1 go test -run RunnerImage ./internal/worker/docker/   # real runner image, ~500 MB pull
MAYFLY_LIVE_TEST=1 MAYFLY_APP_KEY_FILE=$HOME/.config/mayfly/app.pem go test -v -run Live ./internal/github/
```

The live test prints the installation's permissions and repositories, creates one JIT runner registration on `mayfly-demo` and deletes it. Expected first line: `administration:write ... repository_selection: selected`.

## 4. Run Mayfly

```bash
go build -o mayfly ./cmd/mayfly
MAYFLY_LISTEN_ADDR=127.0.0.1:8080 \
MAYFLY_APP_ID=5714458 \
MAYFLY_APP_KEY_FILE=$HOME/.config/mayfly/app.pem \
MAYFLY_WEBHOOK_SECRET_FILE=$HOME/.config/mayfly/webhook-secret \
./mayfly
```

Expected output (JSON, one line each): `receiver listening` and `provisioner started` with labels `self-hosted, linux, x64, mayfly`.

| Variable | Default | Meaning |
|---|---|---|
| `MAYFLY_APP_ID` | required | GitHub App ID |
| `MAYFLY_APP_KEY_FILE` | required | Path to the App private key (PEM) |
| `MAYFLY_WEBHOOK_SECRET_FILE` | required | Path to the webhook secret |
| `MAYFLY_LISTEN_ADDR` | `:8080` | Receiver address; `127.0.0.1:8080` keeps it off the LAN |
| `MAYFLY_RUNNER_LABELS` | `mayfly` | Labels a job must request to be accepted (decision 007) |
| `MAYFLY_WORKERS` | `2` | Jobs provisioned in parallel |
| `MAYFLY_QUEUE_SIZE` | `100` | Jobs waiting before the receiver answers 503 |
| `MAYFLY_WORKER_IMAGE` | `ghcr.io/actions/actions-runner:2.338.2` | Worker image |
| `MAYFLY_WORKER_NETWORK` | Docker default `bridge` | Docker network workers attach to; mayfly creates it (bridge interface of the same name) when missing |
| `MAYFLY_WORKER_TIMEOUT` | `6h` | Maximum worker lifetime |
| `MAYFLY_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker daemon socket |
| `MAYFLY_GITHUB_API_URL` | `https://api.github.com` | GitHub API base URL |
| `MAYFLY_SHUTDOWN_TIMEOUT` | `10s` | Time to drain HTTP requests on SIGTERM |

Make targets for running jobs, crashing mayfly and checking cleanup are described in [makefile.md](makefile.md), with step-by-step recipes for the smoke and orphan tests.

The reaper (`make reaper`, with the same App defaults as `make dev`) reads the App, Docker and worker timeout variables above, plus:

| Variable | Default | Meaning |
|---|---|---|
| `MAYFLY_REAPER_INTERVAL` | `1m` | Time between sweeps |
| `MAYFLY_WORKER_MAX_AGE` | worker timeout + `15m` | Workers older than this are removed; must exceed `MAYFLY_WORKER_TIMEOUT` |
| `MAYFLY_REAPER_GRACE` | `2m` | Younger workers skip the GitHub runner check, since their runner may still be booting |
| `MAYFLY_REAPER_MISSED_PROBES` | `2` | Sweeps in a row `/healthz` must fail before its workers are removed |
| `MAYFLY_HEALTH_URL` | `http://127.0.0.1:8080/healthz` | Where the reaper asks mayfly for its instance ID (decision 012) |
| `MAYFLY_REAPER_STALE_AFTER` | `10m` | How long a `mayfly-*` registration must stay offline with no container before it is deleted; must outlast an image pull |
| `MAYFLY_REAPER_STUCK_AFTER` | `10m` | How long a mayfly job may wait in `queued` before it is logged as stuck (decision 013) |
| `MAYFLY_RUNNER_LABELS` | `mayfly` | Same as the receiver's: which queued jobs count as mayfly's |

The first job pulls the worker image (~500 MB) and is slower; later jobs reuse it.

## 5. Open the tunnel

In a second terminal:

```bash
cloudflared tunnel --no-autoupdate --url http://127.0.0.1:8080
```

It prints a URL like `https://<four-random-words>.trycloudflare.com`. Then:

1. Check it reaches Mayfly: `curl https://<url>/healthz` answers `ok`.
2. Point the App's webhook at it, with the same `MAYFLY_APP_ID` and `MAYFLY_APP_KEY_FILE` as Mayfly:

   ```bash
   go run ./cmd/setwebhook https://<url>/webhook
   ```

   It logs the old and new URL. It calls `PATCH /app/hook/config` with the App JWT and sends only the URL, so the webhook secret is untouched. To do it by hand instead: App settings, **General → Webhook URL**, then **Save changes**.

A quick tunnel gets a new URL every run, so step 2 repeats each time (`make dev` does it for you). A named Cloudflare tunnel keeps a stable URL but needs a Cloudflare account and a domain.

## 6. Run the smoke test

```bash
gh workflow run mayfly-smoke -R TeddyBertrand/mayfly-demo
gh run watch -R TeddyBertrand/mayfly-demo
```

Expected Mayfly log lines, in order:

```
workflow_job queued          a signed delivery for a job with the mayfly label
runner registered            JIT config obtained, about 1s
worker started               container created and started
workflow_job action ignored  in_progress delivery
worker finished              runner exited after the job
worker removed               container deleted
job done
workflow_job action ignored  completed delivery
```

On GitHub, the run ends **success** and its "Set up job" log shows `Runner name: 'mayfly-<job id>-<hex>'`. Then check nothing is left behind:

```bash
docker ps -a --filter label=mayfly.job_id      # no containers
gh api repos/TeddyBertrand/mayfly-demo/actions/runners --jq .total_count   # 0
```

## 7. Stop

Press Ctrl-C in both terminals. Mayfly cancels running workers (their containers are removed and their jobs fail on GitHub) and drops queued jobs. Until the next run, webhook deliveries to the old tunnel URL fail; this is harmless.

## Run with Docker Compose

The same deploy unit as the cloud host (decisions 014 and 015): mayfly and the reaper run from one image, with the secrets mounted as files. Needs Docker Compose v2 and the one-time setup above; Go and cloudflared are not needed on the host.

1. Copy `.env.example` to `.env` (gitignored) and fill it in. `MAYFLY_UID` must own the secret files (`id -u`), and `DOCKER_GID` comes from `getent group docker | cut -d: -f3`.
2. Start everything behind a tunnel and point the App webhook at it:

   ```bash
   make compose-dev
   ```

   This builds the image, waits for mayfly's healthcheck, reads the tunnel hostname from cloudflared's `/quicktunnel` endpoint, and runs `setwebhook` from the image.
3. Run `make smoke`, then stop with `make compose-down`. mayfly removes its workers on SIGTERM; Compose waits up to a minute for it.

Without the tunnel, `docker compose up -d` runs mayfly and the reaper only, for example on a host with a public address. Workers attach to the `mayfly-workers` network, which mayfly creates on first use. To run a published release instead of a local build, set `MAYFLY_VERSION` to its version and run `docker compose pull`.

| Symptom | Cause | Fix |
|---|---|---|
| `required variable ... is missing` | `.env` not filled in | Copy `.env.example` and set every value |
| `permission denied` on `/run/secrets/...` | `MAYFLY_UID` does not own the secret files | Set it to the owner's UID (`stat -c %u <file>`) |
| `permission denied` on `docker.sock` | Wrong `DOCKER_GID` | Use the GID from `getent group docker` |

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `jit config failed ... 403 Resource not accessible by integration` | App permission changed but not accepted, or the repo is not in the installation | Accept the request on the installation page; run the live test to see actual permissions and repos |
| `webhook signature rejected` | Secret file does not match the App's secret | Rewrite one of them so both hold the same value; trailing newlines are trimmed by Mayfly |
| No log line at all after triggering | Tunnel URL changed and the App still points at the old one | Rerun `setwebhook` (or `make dev`); check the App's **Advanced → Recent Deliveries** and redeliver from there |
| `set webhook url failed` | Wrong App ID or key, or the key was revoked | Check `MAYFLY_APP_ID` and `MAYFLY_APP_KEY_FILE`; the error shows GitHub's status and message |
| `workflow_job not for mayfly` | Job's `runs-on` lacks the `mayfly` label | Add `mayfly` to `runs-on` |
| Job stays "Queued" on GitHub after `worker started` | Job asks for a label the runner does not have (e.g. `gpu`) | Use only `self-hosted`, `linux`, `x64` and the Mayfly labels |
| `workflow_job handoff failed`, HTTP 503 | Queue full | Raise `MAYFLY_QUEUE_SIZE` or `MAYFLY_WORKERS`; redeliver from GitHub |
| `permission denied` on `/var/run/docker.sock` | User not in the `docker` group | `sudo usermod -aG docker $USER`, then log in again |
| `MAYFLY_APP_KEY_FILE: required` or another config error at startup | Missing or invalid variable | The message names the variable; see the table in section 4 |
