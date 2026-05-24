---
status: draft
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `docs/task-flow-and-failure-semantics.md` updated to state that `phase: human_review` is reserved for agent-emitted `Result.NextPhase` handoffs
- The controller and lib/delivery never write `phase: human_review` on failure or cap-exhaustion paths
- The Result Routing section updated to show `phase: unchanged` for `needs_input` instead of `phase: human_review`
</summary>

<objective>
Update `docs/task-flow-and-failure-semantics.md` to reflect the new doctrine established by spec 039: `phase: human_review` is reserved for agent-emitted `Result.NextPhase` handoffs. Controller-side and deliverer-side code never write this phase on failure or cap-exhaustion paths.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `docs/task-flow-and-failure-semantics.md` — specifically:
  - The "Inbox Signal" section (around lines 28-34) which mentions `phase: human_review` meaning
  - The "Result Routing" section (around lines 81-101) which shows `needs_input` setting `phase: human_review`
  - The "Agent emits `needs_input`" scenario description (around lines 114-118)

This spec completes the work from spec-021 (Clear Assignee on Escalation).
</context>

<requirements>

1. **Update the "Inbox Signal" section** (around lines 28-34):
   - Update the bullet point about `phase: human_review` meaning to clarify it now means only the agent-emitted handoff case

   **Old text (around lines 32-33):**
   ```
   - `phase: human_review` means a human must do the actual work (agent emitted `needs_input`).
   ```

   **New text:**
   ```
   - `phase: human_review` means a human must verify the agent's output (agent explicitly requested verification via `Result.NextPhase`). This phase is written only via the `AgentStatusDone` -> `resolveNextPhase` path. Controller-side failure paths (`needs_input`, `failed`, cap-exhaustion) leave phase unchanged and clear assignee instead (spec 039).
   ```

2. **Update the "Result Routing" section** (around lines 81-101):
   - Change the `needs_input` row from `phase = human_review` to `phase = unchanged (lifecycle stage preserved)`

   **Old text (around lines 90-93):**
   ```
   case needs_input:
       status = in_progress
       phase  = human_review       ← terminal, no retry
       retry_count: unchanged
   ```

   **New text:**
   ```
   case needs_input:
       status = in_progress
       phase  = unchanged          ← lifecycle stage preserved; assignee cleared (spec 039/supersedes spec-021)
       retry_count: unchanged
   ```

3. **Update the "Agent emits `needs_input`" scenario** (around lines 113-118):
   - Update the description to reflect the new behavior

   **Old text (around lines 115-118):**
   ```
   2. Controller writes `phase: human_review`, `retry_count: 0`, single `## Result` block.
   ```

   **New text:**
   ```
   2. Controller clears `assignee`, leaves phase unchanged, renders `## Result` block (no `## Failure` since needs_input is not a crash). Controller does NOT write `phase: human_review` — that value is reserved for agent-emitted handoffs via `Result.NextPhase` (spec 039).
   ```

4. **Verify the grep for human_review in the doc**:
   ```bash
   grep -n 'human_review' docs/task-flow-and-failure-semantics.md
   ```
   All remaining references should be in the context of the agent-emitted handoff or the spec-039 supersession note.

</requirements>

<constraints>
- Only update the three specified locations in `docs/task-flow-and-failure-semantics.md`
- Add spec-039 references where appropriate
- Do NOT change any other documentation in this file
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Result Routing updated
grep -A2 'case needs_input' docs/task-flow-and-failure-semantics.md
# Expected: phase shown as "unchanged" not "human_review"

# AC2: Inbox Signal section updated
grep -B1 -A2 'phase.*human_review.*means' docs/task-flow-and-failure-semantics.md
# Expected: mentions spec 039 and agent-emitted handoff

# AC3: Scenario updated
grep -B2 -A2 'spec 039' docs/task-flow-and-failure-semantics.md
# Expected: spec 039 mentioned in needs_input scenario
```
</verification>