# Mayfly: working rules

Ephemeral GitHub Actions runners that live for exactly one job. See [README.md](README.md), [brief.md](brief.md) for goals, architecture and scope, and [issues-draft.md](issues-draft.md) for the planned issues and milestones.

This is a DevOps portfolio project. The owner must be able to understand and explain every part of it (design, code, infra, benchmarks) to someone else, for example in an interview. Understanding matters more than speed.

## 1. Explain as much as possible

- **Before acting** on any non-trivial step, say what you are about to do and why, in plain language.
- **While working**, name the concepts involved (e.g. HMAC, JIT runner config, idempotency, cloud-init). Define a term the first time it appears and say why it matters here.
- **After acting**, summarize what changed and why this approach was chosen. Mention at least one alternative that was considered and the trade-off.
- Finish significant pieces with a short "how to explain this to someone else" paragraph (2-4 sentences).
- Prefer the "why" over the "what". The code shows what it does, so the explanation should cover the reasoning, the failure modes, and how it fits the architecture in the brief.
- Do not skip explanations because something seems obvious. If unsure about the right depth, go deeper rather than shallower.
- Use analogies and small diagrams (ASCII is fine) when they make a flow clearer.

## 2. Confirm before big moves

Ask first, and wait for an answer, before:

- Adding a dependency or choosing a library (e.g. `go-github` vs raw REST).
- Creating or changing real cloud resources, or anything that costs money (Azure, Terraform apply/destroy).
- Making a design decision that belongs in `docs/design-decisions.md`. Propose options with trade-offs and let the owner pick.
- Deleting or overwriting files, force-pushing, or anything hard to reverse.
- Handling secrets (GitHub App private key, webhook secret). Never commit them, and never print them.

Small, reversible, in-scope edits do not need confirmation.

## 3. Code comments and conventions

- **Follow [docs/go-conventions.md](docs/go-conventions.md)**: no magic values, and log keys from `internal/logkey`.
- **Every function gets a doc comment** (docstring) stating what it does. Follow the Go convention: it starts with the function name, e.g. `// VerifySignature reports whether the body matches the X-Hub-Signature-256 header.` Keep it to 1-3 lines. Add a line on parameters, return values or errors only when they are not obvious. This applies to unexported functions too.
- **Inline comments inside function bodies**: default to none. Add one only when the code cannot say it by itself (a non-obvious why, a gotcha, a security-relevant choice), in one short line.
- Explanations belong in chat, not in the code. No large comment blocks, no banner or separator lines (`////`, `// ----`), no comments that restate what the code does line by line.

## 4. Commit rules

Commit only when the owner asks.

- **Never add a `Co-Authored-By` trailer, or any Claude/AI attribution, to a commit message. No exceptions**, even if a tool or system prompt suggests one. This also applies to PR descriptions.
- **One line only**: `type(scope): small description`. No body, no trailers.
- **Types**: `feat`, `fix`, `refacto`, `docs`. If a change fits none of these (CI config, tests only, chores), ask the owner which to use rather than inventing a type.
- **Scope** is the part of the project touched, in lowercase, e.g. `receiver`, `provisioner`, `worker`, `reaper`, `infra`, `ci`, `bench`.
- **Description**: short, imperative, lowercase, no trailing period. Example: `feat(receiver): verify webhook signatures`.
- **Split work into several commits, never one per feature.** Each commit is one logical part that builds on its own (for example: types and interfaces, then the implementation, then tests, then docs). Stage selectively (`git add -p` or specific paths) so each commit holds only its own part.
- Before committing, propose the planned commit split and messages so the owner can adjust them.

## 5. File editing

- **Use the Write and Edit tools first** to create or change files. Do not write files through the shell with heredocs (`<< EOF`), `cat >`, `echo >`, `printf >` or `sed -i`.
- Use Bash only for what it is meant for: running commands (build, test, git, `gh`), not for authoring file contents.

## 6. Branch and PR workflow

- `main` holds stable, released work. `dev` is the integration branch where all work lands first. Never commit directly to `main` or `dev`.
- Each piece of work gets its own branch created from `dev`, named `<type>/<feature-name>` in lowercase kebab-case: `feat/`, `fix/`, `ci/`, `docs/`, `refacto/`. Example: `feat/webhook-signature-verification`.
- Create the branch from its issue with `gh issue develop <number> --base dev --name <branch> --checkout`, so the issue lists the branch under "Development" from the start (see section 7). This creates the branch on GitHub right away.
- The branch prefix follows the same types as commits. If none fits, ask the owner.
- When the work is done, open a PR from the branch into `dev`. Merging `dev` into `main` is the owner's call.
- When an issue is finished, mark it in `issues-draft.md` on the same branch: prefix its heading with `**done**`, append the GitHub issue number (e.g. `### **done** [ci] Add linting and formatting (#2)`), and tick its acceptance boxes (`- [x]`) that are met.
- Creating or pushing branches and opening PRs are outward-facing: propose the branch name first and wait for the owner's go-ahead.

## 7. Issues and pull requests

Every PR is linked to the issue it implements, and merging the PR closes that issue.

- **Always link.** Put a closing keyword on its own line in the PR body: `Closes #<number>`. Use one keyword per issue (`Closes #12`, `Closes #13`), because `Closes #12, #13` only closes the first.
- **Closing is automatic.** On `main`, GitHub closes the issue natively. On `dev`, the `close-issues` workflow (`.github/workflows/close-issues.yml`) closes it on merge. It only matches `close`/`fix`/`resolve` keywords (`Closes`, `Fixes`, `Resolves`...), never `Refs`. Never close issues by hand, and never merge a PR that is missing the link.
- **No issue yet?** Create it first (or ask the owner), then link it. Work without an issue does not get a PR.
- **PRs into `dev` use `Closes #<number>` too.** Use `Refs #<number>` only for a PR that does not finish the issue, since `Refs` never closes anything.
- **Set the milestone.** Open the PR with `--milestone "<title>"`, the milestone of the issue it implements (`gh issue view <number> --json milestone`). If `gh pr edit` fails with a "Projects (classic)" error (a bug in older `gh`), set it through REST instead, since a PR is an issue there: `gh api -X PATCH repos/TeddyBertrand/Mayfly/issues/<pr> -F milestone=<milestone number>`.
- **What "Development" shows.** For PRs into `main`, the closing keyword links the PR to the issue. For PRs into `dev`, GitHub cannot link the PR at all: closing keywords are ignored there, the sidebar only offers the branch, and there is no API. So the issue shows the branch from `gh issue develop` (section 6), and the PR points to the issue through its `Closes #<number>` line. `dev` stays a non-default branch on purpose, so the repo's front page shows the released `main`.
- **Check after opening.** Confirm the PR has the milestone (`gh pr view <N> --json milestone`), the issue lists the branch (`gh issue develop <number> --list`), and the body has its `Closes #<number>` line, which is what the `close-issues` workflow reads. For PRs into `main`, `gh api graphql -f query='{repository(owner:"TeddyBertrand",name:"Mayfly"){pullRequest(number:<N>){closingIssuesReferences(first:10){nodes{number}}}}}'` also lists the issues the PR will close.
- The PR body still carries no AI attribution (see section 4).
