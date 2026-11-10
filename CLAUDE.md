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
- Creating or changing real cloud resources, or anything that costs money (Hetzner, Terraform apply/destroy).
- Making a design decision that belongs in `docs/design-decisions.md`. Propose options with trade-offs and let the owner pick.
- Deleting or overwriting files, force-pushing, or anything hard to reverse.
- Handling secrets (GitHub App private key, webhook secret). Never commit them, and never print them.

Small, reversible, in-scope edits do not need confirmation.

## 3. Code comments

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
- The branch prefix follows the same types as commits. If none fits, ask the owner.
- When the work is done, open a PR from the branch into `dev`. Merging `dev` into `main` is the owner's call.
- Creating or pushing branches and opening PRs are outward-facing: propose the branch name first and wait for the owner's go-ahead.
