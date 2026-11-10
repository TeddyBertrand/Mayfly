---
name: decision
description: Walk through a Mayfly design decision (library, architecture, tooling) with options and trade-offs, let the owner choose, then record it in docs/design-decisions.md. Use when the user types /decision or a choice like "go-github vs raw REST" or "Docker vs Firecracker" comes up.
---

# Design decision

The topic is `$ARGUMENTS` (e.g. "GitHub client library"). If empty, ask what needs deciding.

The owner makes the decision. Your job is to make the options understandable and the trade-offs honest.

## Steps

1. **Frame the question.** State the decision, why it has to be made now, and the constraints from `brief.md` (goals, non-goals, security, timeline, cost).
2. **Lay out 2-4 options.** For each: what it is, pros, cons, and how it affects Mayfly's goals (speed, cost, security, simplicity, portfolio value). Explain unfamiliar terms.
3. **Recommend one**, with reasons, but make clear it is a recommendation. Then **stop and wait for the owner's choice.** Do not write the record until they have chosen.
4. **Record it.** Append an entry to `docs/design-decisions.md`, creating the file with a short header if needed, in this format:

   ```markdown
   ## NNN. <Title>
   - **Date:** YYYY-MM-DD
   - **Status:** accepted
   - **Context:** why the decision was needed
   - **Options considered:** each option with its main trade-off
   - **Decision:** what was chosen
   - **Consequences:** what this makes easier, what it makes harder, what to revisit and when
   ```

   Number entries sequentially. If a decision replaces an earlier one, mark the old one `superseded by NNN` instead of deleting it.
5. **Explain it back.** Give a 2-3 sentence version the owner could use to justify the choice to someone else.

## Rules

- Do not apply the decision (add dependencies, change code) in this skill. Say what follows from it and offer `/work-issue`.
- Keep entries factual and short. They are interview material, so accuracy matters more than polish.
