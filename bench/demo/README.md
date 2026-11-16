# mayfly-demo

Benchmark target for [Mayfly](https://github.com/TeddyBertrand/Mayfly). The canonical copy lives in Mayfly's `bench/demo/`; see `docs/demo-repo.md` there.

Every workflow is started by hand and takes a `runner` input: `hosted` runs on `ubuntu-24.04`, `mayfly` on `[self-hosted, mayfly]`, `classic` on the stock long-lived runner `[self-hosted, classic]`.

```bash
gh workflow run go-build -f runner=mayfly
gh workflow run npm-test -f runner=hosted
```

| Workflow | Work |
|---|---|
| `go-build` | `go mod download`, build, vet and test of a small HTTP API (`go/`) |
| `npm-test` | `npm ci` and vitest tests of a cart pricing module (`node/`) |
| `mayfly-smoke` | One echo step, Mayfly only |
| `mayfly-long` | Sleeps 10 minutes, Mayfly only (reaper and shutdown tests) |
