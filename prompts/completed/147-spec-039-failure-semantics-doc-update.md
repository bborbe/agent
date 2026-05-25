---
status: completed
spec: [039-controller-stop-setting-human-review-on-failure]
summary: 'Updated docs/task-flow-and-failure-semantics.md to reflect spec-039 doctrine: phase: human_review is reserved for agent-emitted Result.NextPhase handoffs; controller-side failure paths leave phase unchanged and clear assignee instead'
container: agent-exec-147-spec-039-failure-semantics-doc-update
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
started: "2026-05-24T23:28:00Z"
completed: "2026-05-24T23:28:55Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `docs/task-flow-and-failure-semantics.md` updated to state that `phase: human_review` is reserved for agent-emitted `Result.NextPhase` handoffs
- The controller and lib/delivery never write `phase: human_review` on failure or cap-exhaustion paths
- The Result Routing section updated to show `phase: unchanged` for `needs_input` instead of `phase: human_review`
- The Terminology table Escalation row updated to drop the "also sets `phase: human_review`" clause
</summary>

<objective>
Update `docs/task-flow-and-failure-semantics.md` to reflect the new doctrine established by spec 039: `phase: human_review` is reserved for agent-emitted `Result.NextPhase` handoffs. Controller-side and deliverer-side code never write this phase on failure or cap-exhaustion paths.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `docs/task-flow-and-failure-semantics.md` — specifically:
  - Line 22 — the Phase row of the Terminology table (the enum `planning → in_progress → (ai_review | done | human_review)`). MUST NOT be modified — `human_review` is still reachable via `Result.NextPhase`.
  - Line 26 — the Escalation row of the Terminology table (currently says "For `needs_input` the controller also sets `phase: human_review`").
  - Lines 28-34 — the "Inbox Signal" section.
  - The "Result Routing" section (around lines 81-101) which shows `needs_input` setting `phase: human_review`.
  - The "Agent emits `needs_input`" scenario description (around lines 114-118).
  - Line 205 — Gate-1 enum reference (`phase ∈ {human_review, done}`). MUST NOT be modified — this is the executor terminal-phase gate which still applies to legitimate handoffs.

This spec completes the work from spec-021 (Clear Assignee on Escalation).
</context>

<requirements>

1. **Update the Terminology table — Escalation row** at line 26:
   - Drop the clause "For `needs_input` the controller also sets `phase: human_review`."
   - Replace with: "For `needs_input` the lifecycle phase is left at whatever stage it held when the agent returned `needs_input` (spec 039)."

   **Old text (within line 26):**
   ```
   For `needs_input` the controller also sets `phase: human_review`.
   ```

   **New text:**
   ```
   For `needs_input` the lifecycle phase is left at whatever stage it held when the agent returned `needs_input` (spec 039) — only `assignee` is cleared.
   ```

2. **Update the "Inbox Signal" section** (around lines 28-34):
   - Update the bullet point about `phase: human_review` meaning to clarify it now means only the agent-emitted handoff case.

   **Old text (around lines 32-33):**
   ```
   - `phase: human_review` means a human must do the actual work (agent emitted `needs_input`).
   ```

   **New text:**
   ```
   - `phase: human_review` means a human must verify the agent's output (agent explicitly requested verification via `Result.NextPhase`). This phase is written only via the `AgentStatusDone` -> `resolveNextPhase` path. Controller-side failure paths (`needs_input`, `failed`, cap-exhaustion) leave phase unchanged and clear assignee instead (spec 039).
   ```

3. **Update the "Result Routing" section** (around lines 81-101):
   - Change the `needs_input` row from `phase = human_review` to `phase = unchanged (lifecycle stage preserved)`.

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

4. **Update the "Agent emits `needs_input`" scenario** (around lines 113-118):
   - Update the description to reflect the new behavior.

   **Old text (around lines 115-118):**
   ```
   2. Controller writes `phase: human_review`, `retry_count: 0`, single `## Result` block.
   ```

   **New text (this exact sentence MUST be present verbatim and is asserted by verification):**
   ```
   2. Controller clears `assignee`, leaves phase unchanged, renders `## Result` block (no `## Failure` since needs_input is not a crash). Controller does NOT write `phase: human_review` — that value is reserved for agent-emitted handoffs via `Result.NextPhase` (spec 039).
   ```

5. **MUST NOT modify** the following lines (they are still correct under spec 039 because `human_review` remains reachable via `Result.NextPhase`):
   - Line 22 — the Phase row enum `planning → in_progress → (ai_review | done | human_review)`. Do NOT "tidy up" and remove `human_review` from this enum.
   - Line 205 — Gate-1 description `Tasks whose phase ∈ {human_review, done} are suppressed`. Do NOT modify.

</requirements>

<constraints>
- Only update the four specified locations in `docs/task-flow-and-failure-semantics.md`
- Add spec-039 references where appropriate
- Do NOT remove `human_review` from the line 22 enum or the line 205 Gate-1 description — both are still legitimate
- Do NOT change any other documentation in this file
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Terminology Escalation row updated — old clause gone
grep -n 'controller also sets `phase: human_review`' docs/task-flow-and-failure-semantics.md
# Expected: 0 matches

grep -n 'lifecycle phase is left at whatever stage it held when the agent returned' docs/task-flow-and-failure-semantics.md
# Expected: 1 match (the new wording in the Escalation row)

# AC2: Result Routing updated
grep -A2 'case needs_input' docs/task-flow-and-failure-semantics.md
# Expected: phase shown as "unchanged" not "human_review"

# AC3: Inbox Signal section updated
grep -n 'agent explicitly requested verification via `Result.NextPhase`' docs/task-flow-and-failure-semantics.md
# Expected: 1 match (the new bullet)

# AC4: Scenario sentence introduced verbatim
grep -F "Controller clears \`assignee\`, leaves phase unchanged, renders \`## Result\` block" docs/task-flow-and-failure-semantics.md
# Expected: 1 match

grep -F "that value is reserved for agent-emitted handoffs via \`Result.NextPhase\` (spec 039)" docs/task-flow-and-failure-semantics.md
# Expected: 1 match

# AC5: Enumerate remaining human_review mentions and verify each is legitimate
grep -n 'human_review' docs/task-flow-and-failure-semantics.md
# Expected matches and ONLY these categories:
#   - Line ~22: Phase enum `planning → in_progress → (ai_review | done | human_review)` (DO NOT remove)
#   - Line ~32: Inbox Signal bullet — new wording about Result.NextPhase
#   - Line ~205: Gate-1 description `phase ∈ {human_review, done}`
#   - New wording introduced by reqs 1, 2, 4 (mentions in spec-039 supersession context)
# Any line that asserts the controller WRITES `phase: human_review` on a failure path is a FAIL.
```
</verification>
