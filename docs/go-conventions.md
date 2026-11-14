# Go conventions

Rules every Go change in Mayfly follows. Choices with trade-offs are recorded in [design-decisions.md](design-decisions.md); this page is the day-to-day rulebook. Comment and commit rules live in [CLAUDE.md](../CLAUDE.md).

## No magic values

A number or string that carries a meaning gets a name, defined once, next to the code that owns it. Typical cases: defaults, timeouts, limits, sizes, protocol values (API versions, statuses, header and environment names a third party defines), and any value two places must agree on.

```go
// Bad: what is 12, and does the reaper agree with the backend?
if len(id) > 12 { ... }

// Good: one name, used everywhere.
const shortIDLen = 12
```

**Where the name lives.** In the package that owns the meaning. A value another package must agree with is exported from its owner (`github.RunnerNamePrefix`, which the reaper reads), never copied. Shared defaults live in `internal/config`, which imports no other internal package.

**What Go allows.** `const` holds only booleans, numbers, strings and runes, including named types built on them such as `time.Duration` or `workflow.Action`. Slices, maps and structs cannot be constants. A fixed collection becomes an unexported `var` that nothing writes to, or a function that returns a fresh copy when callers might modify it, since a slice shares its backing array.

**Literals that stay.**

| Literal | Why it stays |
|---|---|
| `0`, `1`, `""`, `nil` in their usual roles (empty, first, increment) | The meaning is the value itself |
| Standard library idioms, such as `strconv.ParseInt(v, 10, 64)` | Base 10 and 64 bits read as part of the call |
| Log and error messages | They are prose, not values |
| Struct tags (`json:"id"`) | They are the field's wire name, written once |
| Test fixtures | A literal in a test table is easier to read than a name |
| `MAYFLY_*` environment variable names | Each is read in exactly one place and documents itself |
| Command-line flags (`--healthcheck`) and HTTP route or API paths (`/containers/create`) | Each appears once and is its own description |

## Log attribute keys

Log keys are constants from `internal/logkey`, never string literals:

```go
log := p.logger.With(logkey.JobID, job.ID, logkey.Repo, job.Repo)
log.Warn("jit config failed, retrying", logkey.Attempt, attempt, logkey.Err, err)
```

A typo in a key then fails to compile instead of silently breaking a log search, and the same names can label metrics. Keys are lowercase `snake_case`. Add a key to `logkey` before using it.

## What the linters enforce

`golangci-lint` runs `mnd` (magic number detector) and `goconst` (repeated strings) on production code; test files are excluded because fixtures stay literal.

They catch unnamed numbers and strings repeated three or more times in one package, in comparisons, assignments and returns. They do **not** catch a meaningful string used once or twice, a value repeated across packages, or strings inside composite literals such as `[]string{...}` or map literals (checked: `goconst` ignores those). Those are this page's job, and code review's.

## Tests

Where a test lives depends on what it can see ([decision 019](design-decisions.md)).

| Kind | Where | Package | Runs with |
|---|---|---|---|
| Unit test | `foo_test.go` beside `foo.go` | `package foo` (same package, so it reaches unexported code) | `go test ./...` |
| Test against a real external system (GitHub, the Docker daemon) that still tests internals | beside the code it tests | `package foo` | `go test ./...`, skipped unless enabled |
| End-to-end test that drives Mayfly from outside, through exported APIs or the built binary | `tests/` | its own package, starting with `//go:build e2e` | `go test -tags e2e ./tests/...` |

**Unit tests.** Table-driven where the cases share a shape. Fixtures stay literal (see [No magic values](#no-magic-values)). Fakes implement the same interfaces the code depends on (`worker.Inventory`, `provisioner.JITIssuer`), so no mocking library is needed.

**Tests against real systems.** They skip themselves unless their system is there, so `go test ./...` passes on any machine: `MAYFLY_LIVE_TEST=1` enables `internal/github/live_test.go` (real GitHub, needs App credentials), the Docker tests skip when no daemon answers, and `MAYFLY_DOCKER_TEST=1` enables the one that pulls the full runner image.

**End-to-end tests.** The first line of every file in `tests/` is the build constraint, followed by a blank line:

```go
//go:build e2e

package tests
```

Go compiles the file only when `-tags e2e` is passed, so `go test ./...` never runs it and stays fast. The `tests/` folder and a `make e2e` target are added with the first e2e test: `go test ./tests/...` fails while the folder does not exist. The first candidate is `internal/provisioner/handoff_test.go`, which wires the real receiver to the provisioner.

**Readability.** Tests are not moved to declutter the tree; the editor hides them. In Zed, add to the user settings (not the repo's `.zed/`, since it is a personal preference):

```json
{
  "hidden_files": ["**/.*", "**/*_test.go"],
  "project_panel": { "hide_hidden": true }
}
```

Toggle them back with `project panel: toggle hide hidden`.
