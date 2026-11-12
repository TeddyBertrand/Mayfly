# GitHub App

Mayfly talks to GitHub as a GitHub App. This page records how the App is configured and why.

## Why an App and not a personal access token

| | Personal access token | GitHub App |
|---|---|---|
| Identity | The user who created it | Its own bot identity |
| Scope | Every repo the user can reach | Only the repos it is installed on |
| Permissions | Coarse scopes | Fine-grained, one by one |
| Credential lifetime | Months, until revoked | Installation tokens expire after 1 hour |
| Webhooks | Configured per repo | Built into the App |

A leaked installation token is useless after an hour, and the App can never reach a repo it is not installed on.

## Authentication flow

```
private key (.pem) --signs--> JWT (RS256, valid at most 10 min, iss = App ID)
JWT --POST /app/installations/{id}/access_tokens--> installation token (1 h)
installation token --POST /repos/{owner}/{repo}/actions/runners/generate-jitconfig--> JIT config
```

The provisioner caches the installation token and refreshes it before it expires (issue #10).

## Configuration

- **Name:** `mayfly-teddybertrand` (App names are global on GitHub).
- **App ID:** `5714458`
- **Installation ID:** `180929139`
- **Homepage URL:** the Mayfly repository.
- **Webhook:** **Active**, with a placeholder URL until the receiver exists (M1). GitHub hides event subscriptions while the webhook is inactive, so it must stay on; deliveries to the placeholder simply fail. The webhook secret is set from the start.
- **Where can it be installed:** only on this account.
- **Installed on:** `mayfly-demo` only.

### Permissions

| Permission | Level | Reason |
|---|---|---|
| Repository: Administration | Read and write | Required by `generate-jitconfig` and to delete stale runner registrations (reaper). |
| Repository: Actions | Read | Required to subscribe to `workflow_job`; also used to read job timings for benchmarks. |
| Repository: Metadata | Read | Mandatory for every App. |

Nothing else is granted. In particular, no Contents, Pull requests or Secrets access.

### Events

- `workflow_job` only. The receiver acts on `queued` and uses `in_progress` and `completed` for worker state.

### Changing permissions or repository access

- Editing permissions in the App settings does not change existing installations. The installation owner must open the installation page (**Settings → Applications → Installed GitHub Apps → Configure**) and **accept the new permissions**. Until then, calls keep failing with `403 Resource not accessible by integration`.
- Restrict **Repository access** to `mayfly-demo` *before* accepting a write permission, so the wider permission is never granted on other repositories, even briefly.
- Check what the installation actually holds with the live test (`MAYFLY_LIVE_TEST=1 go test -v -run Live ./internal/github/`). It logs the granted permissions, `repository_selection` and the reachable repositories. Expected: `administration:write`, `selected`, `[TeddyBertrand/mayfly-demo]`.

## Accepted risk: Administration is broad

For repositories owned by a personal account, GitHub offers no permission narrower than **Administration: write** for managing self-hosted runners. That permission also covers repo settings and repo deletion. Organizations have a narrower "Self-hosted runners" permission, but Mayfly runs on a personal account.

Mitigation: the App is installed only on `mayfly-demo`, a throwaway repository with nothing of value. It is never installed on other repositories. If Mayfly moves to an organization, switch to the organization "Self-hosted runners: write" permission and drop Administration.

## Secrets

| Secret | Created by | Used for |
|---|---|---|
| Webhook secret | `openssl rand -hex 32`, locally | HMAC-SHA256 signature on every webhook (`X-Hub-Signature-256`), checked by the receiver |
| Private key (`.pem`) | GitHub, downloaded once | Signing the JWT that proves the caller is the App |

Rules:

- Stored outside the repo, in `~/.config/mayfly/` with mode `600`, and in a password manager.
- Never committed. `.gitignore` excludes `*.pem` and `.env` as a safety net.
- Passed to Mayfly through environment variables or file paths, never as command-line arguments (they would show up in `ps`).

The App ID and installation ID are identifiers, not secrets.

### Rotation

- **Private key:** generate a new key in the App settings (**Credentials → Key pairs → New key**), deploy it, then delete the old key. GitHub allows several active keys, so there is no downtime.
- **Webhook secret:** set the new value in the App settings and deploy it to the receiver at the same time. Webhooks delivered in between fail their signature check and can be redelivered from the App's "Advanced" tab.
- **If a key leaks:** delete it in the App settings immediately. This invalidates it at once.
