---
name: work-issue
description: Take one Mayfly issue from issues-draft.md (or GitHub) from plan to implementation to tests, with explanations at each step. Use when the user types /work-issue or says "let's do issue X", "start M1", or "work on <issue title>".
disable-model-invocation: true
---

# Work an issue

The issue is `$ARGUMENTS` (a title, a milestone item, or a GitHub issue number). If empty, show the next unchecked issue in the lowest open milestone of `issues-draft.md` and ask whether to start it.

Follow the rules in `CLAUDE.md` throughout: explain first, and confirm before big moves.

## Steps

1. **Understand.** Read the issue and its acceptance criteria (the `- [ ]` lines), plus the related parts of `brief.md`. Restate the goal and the criteria in your own words and explain the concepts involved.
2. **Plan.** Propose a short plan: files to create or change, the approach, and at least one alternative with its trade-off. Flag anything that needs confirmation (new dependency, cloud cost, design decision, secrets). **Stop and wait for approval before writing code.**
3. **Branch.** After approval, create a branch named `<milestone>/<short-slug>` (e.g. `m1/verify-webhook-signatures`) from `main`. Do not work on `main` directly.
4. **Implement in small steps.** For each step, say what you are doing and why, then do it. Write tests alongside the code (table-driven in Go where it fits). Run `go build ./...`, `go vet ./...` and `go test ./...` once the Go module exists, and report results faithfully, including failures.
5. **Verify against acceptance criteria.** Go through each `- [ ]` line and state whether it is met and how it was checked. Say plainly what is not verified.
6. **Record decisions.** If a design decision was made, offer to run `/decision` so it lands in `docs/design-decisions.md`.
7. **Wrap up.** Summarize what changed and why, add a short "how to explain this to someone else" paragraph, and propose a commit split following the commit rules in `CLAUDE.md` (several one-line commits, one per logical part, no `Co-Authored-By`). **Commit and open a PR only if the user asks.** The PR body must contain `Closes #<issue number>` on its own line (see `CLAUDE.md` section 5) so merging closes the issue, and list the acceptance criteria, with no AI attribution. After opening, verify the link with `gh pr view --json closingIssuesReferences`.

## Rules

- One issue at a time. Do not drift into neighbouring issues; note them instead.
- Never commit secrets, and never print them.
- Never touch real cloud resources without explicit confirmation.
