---
name: explain
description: Deep, teaching-style walkthrough of a file, component, concept or flow in Mayfly, so the owner can explain it to someone else (e.g. in an interview). Use when the user types /explain or asks "walk me through", "how does X work", or "help me understand X".
---

# Explain

Goal: the owner must come away able to explain the target to someone else, unprompted. Depth beats brevity.

The target is `$ARGUMENTS` (a file, package, component such as "reaper", or a concept such as "JIT config"). If empty, ask what to explain.

## Steps

1. **Locate it.** Read the relevant code, docs, `brief.md` and `issues-draft.md`. If the target does not exist yet, explain the planned design from the brief and say clearly that it is not implemented.
2. **Write the explanation in this order:**
   1. **One-sentence summary**: what it is and its job in Mayfly.
   2. **Where it fits**: its place in the architecture (receiver, provisioner, worker, reaper, cache, observability), with a small ASCII diagram of inputs and outputs.
   3. **How it works**: step by step, referencing real code as `path:line`. Define each concept the first time it appears (HMAC, idempotency, JIT config, cloud-init, etc.) and say why it matters here.
   4. **Why this way**: the design reasoning, at least one alternative that was or could have been chosen, and the trade-offs.
   5. **Failure modes and security**: what can go wrong, how it is handled, and the relevant threat from the brief's security section.
   6. **Explain it to someone else**: a 3-5 sentence spoken-style summary the owner could say out loud.
3. **Check understanding.** End with 2-3 questions the owner could be asked about this topic, and offer to quiz them.

## Rules

- Be accurate. Never describe behaviour you have not verified in the code or docs. Mark guesses as guesses.
- Do not change any files.
- Match the depth to the question, but when unsure, go deeper.
