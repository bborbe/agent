---
status: approved
spec: [010-failure-vs-needs-input-semantics]
created: "2026-04-19T11:00:00Z"
queued: "2026-04-19T10:51:05Z"
branch: dark-factory/failure-vs-needs-input-semantics
---

<summary>
- Updates the Claude agent's workflow prompt to instruct Claude when to use `needs_input` vs `failed` vs `done`
- Fixes current guidance that says "cannot complete → `failed`" — this is wrong for task-level impossibility; `needs_input` is correct
- After this change, agents self-classify task-level impossibility as `needs_input` (no retry) vs infra failure as `failed` (retry-eligible), matching controller and content-generator routing
- Markdown-only change — no Go code touched
- Verifies the agent module still builds and precommit passes
</summary>

<objective>
Complete Desired Behavior #5 from spec 010: "Agent prompts instruct Claude to emit `needs_input` for semantically impossible or underspecified tasks (missing data, contradictory parameters, zero results where results were required)." The current `workflow.md` directs Claude to always return `failed` when it cannot complete a task, which is incorrect — it conflates task-content problems with infrastructure problems. Update the guidance so Claude self-classifies correctly.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

**Key files to read before editing:**

- `agent/claude/pkg/prompts/workflow.md` — current guidance (about 16 lines); the `Rules` section says "If you cannot complete the task, return a `failed` status with a clear explanation" — this must be updated to distinguish `needs_input` from `failed`
- `agent/claude/pkg/prompts/output-format.md` — already documents the three statuses correctly: `done` (success), `failed` (error), `needs_input` (blocked on missing info); the workflow rules must align with this

**Why this matters (spec 010 §Problem):**

The observed failure mode: an agent correctly determines there are zero trades in a window, but the current workflow rules say "cannot complete → `failed`". This triggers the retry counter N times before escalation. The correct classification is `needs_input` — the task content is the problem, not the infrastructure. `needs_input` routes directly to `human_review` with no retry.

**Three-way classification:**

| Situation | Correct status |
|-----------|----------------|
| Task executed successfully, results available | `done` |
| Tool/API error, network failure, CLI crash, unexpected exception | `failed` |
| Task is semantically impossible or underspecified: zero results where results were required, contradictory parameters, missing required data, ambiguous scope that cannot be resolved without human input | `needs_input` |
</context>

<requirements>

1. **Edit `agent/claude/pkg/prompts/workflow.md`**

   Find the `Rules` section. It currently contains:
   ```
   - If you cannot complete the task, return a `failed` status with a clear explanation
   ```

   Replace this single rule with three rules that distinguish the three statuses:

   ```
   - If the task executed successfully and you have results to report, return `done`
   - If the task is semantically impossible or underspecified — zero results where results were required, missing required data, contradictory parameters, ambiguous scope that cannot be resolved without human clarification — return `needs_input`
   - If you encountered an infrastructure error (tool failure, API error, network problem, unexpected exception) that prevented execution, return `failed` with a clear explanation

   Do not use `needs_input` for transient infrastructure errors — those are `failed` and eligible for retry.
   ```

   The goal is that Claude can distinguish "the task itself is wrong" (`needs_input`) from "something broke while executing" (`failed`).

   Preserve all other lines in the file exactly as-is (the `Instructions` section, other `Rules` bullets, and the reference to `<output-format>`).

2. **Verify the edited file reads correctly**

   ```bash
   cat agent/claude/pkg/prompts/workflow.md
   ```
   Must show the updated Rules section with all three distinctions.

3. **Run precommit for agent/claude module to confirm no regressions**

   Only Go code is compiled; the `.md` files are embedded at build time. Verify the module still builds:
   ```bash
   cd agent/claude && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Edit `workflow.md` only — do NOT modify `output-format.md`, any `.go` files, or any other files
- Do NOT commit — dark-factory handles git
- The file has no copyright header (it is a prompt template, not a Go source file) — do NOT add one
- Preserve the existing `Instructions` numbered list and all other `Rules` bullets verbatim; only replace the single "If you cannot complete" bullet with the three new bullets
- All existing tests must pass (no Go code changes, so test suite is unaffected)
</constraints>

<verification>
Verify the updated guidance includes needs_input:
```bash
grep -n "needs_input\|failed\|done" agent/claude/pkg/prompts/workflow.md
```
Must show all three statuses named in the Rules section.

Verify "semantically impossible" or equivalent wording is present:
```bash
grep -n "semantically\|impossible\|underspecified\|zero results" agent/claude/pkg/prompts/workflow.md
```
Must show at least one match.

Verify the old incorrect rule is gone:
```bash
grep -n "cannot complete the task, return a .failed." agent/claude/pkg/prompts/workflow.md
```
Must return zero matches.

Verify agent/claude module builds:
```bash
cd agent/claude && make precommit
```
Must exit 0.
</verification>
