---
status: approved
spec: [041-spawn-notification-early-return-skips-human-review-guard]
created: "2026-05-25T21:50:00Z"
queued: "2026-05-25T22:35:21Z"
branch: dark-factory/spawn-notification-early-return-skips-human-review-guard
---

<summary>
- Reorders `resultWriter.applyRetryCounter` so the `phase == "human_review"` assignee-clear guard runs BEFORE the `spawn_notification` early return
- Fixes the live 2026-05-25 prod incident where a pr-reviewer task landed at `phase: human_review` with `assignee: pr-reviewer-agent` still set
- Adds a Ginkgo unit test reproducing the prod incident (merged frontmatter with `spawn_notification: true` + incoming `phase: human_review`)
- Preserves the 2026-04-24 hotfix ordering: `applyTriggerCap` still runs before the `spawn_notification` early return
- Single production-code file change; test file extended in place
</summary>

<objective>
After this prompt, the `phase == "human_review"` guard in `resultWriter.applyRetryCounter` runs on every code path where merged frontmatter has `phase == "human_review"`, regardless of `spawn_notification` state. New Ginkgo Context proves the fix by reproducing the prod incident input shape.
</objective>

<context>
Read `CLAUDE.md` for project conventions. Read these guides:
- `$HOME/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `$HOME/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`

Read these project files:
- `task/controller/pkg/result/result_writer.go` — target file for the reorder (lines 150-185)
- `task/controller/pkg/result/result_writer_test.go` — existing Ginkgo suite; mirror style of `Context("spawn notification", ...)` and `Context("needs_input result", ...)`
- `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` — precedent for the reorder pattern

**Bug summary from the spec:** Spec 039 shipped the `human_review` guard at the bottom of `applyRetryCounter`. The guard doesn't run when merged frontmatter also carries `spawn_notification: true` (inherited via `mergeFrontmatter` from the executor's spawn write). The 2026-04-24 hotfix (prompt 075) fixed the same bug class for `applyTriggerCap`. This prompt applies the identical pattern to the `human_review` guard.
</context>

<requirements>

1. **Reorder `applyRetryCounter` in `task/controller/pkg/result/result_writer.go`**

   The current ordering inside the function body (between the `status == "completed"` early return and the final `return body`):
   1. `applyTriggerCap` call (above `SpawnNotification()`, per 2026-04-24 hotfix — DO NOT MOVE)
   2. `SpawnNotification()` early return
   3. `applyRetryCap` call
   4. `phase == "human_review"` guard ← THIS MOVES UP

   New ordering:
   1. `applyTriggerCap` call (unchanged)
   2. **`phase == "human_review"` guard** ← MOVED to just below `applyTriggerCap`, ABOVE the `SpawnNotification()` early return
   3. `SpawnNotification()` early return
   4. `applyRetryCap` call
   5. (no more `human_review` guard at bottom — it moved up)

   Replace the relevant section of `applyRetryCounter` body. Preserve the multi-line comment block above `applyTriggerCap` verbatim (it documents the 2026-04-24 ba1bad61 precedent). Add a new multi-line comment block above the moved `human_review` guard explaining why it must be above the `SpawnNotification()` early return:

   ```go
   // human_review assignee-clear guard runs BEFORE the spawn_notification
   // early return below. On a pr-reviewer agent's first post-spawn write, the merged
   // frontmatter carries spawn_notification: true (inherited from the executor's
   // spawn-time UpdateFrontmatterCommand) AND incoming phase: human_review (from
   // Result.NextPhase via resolveNextPhase). If this guard were below the
   // spawn_notification early return, clearAssignee would never fire and the
   // operator inbox filter (assignee == "") would miss the task. Same bug class
   // as prompt 075 (2026-04-24 applyTriggerCap precedent, task ba1bad61); spec 041
   // fixes the 2026-05-25 prod incident (task bborbe-agent #3).
   if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
       clearAssignee(merged)
   }
   ```

   Key invariants to preserve:
   - `status == "completed"` early return stays at the top (unchanged)
   - `applyTriggerCap` stays above `SpawnNotification()` (AC#6 — preserves 2026-04-24 hotfix)
   - `applyRetryCap` stays below `SpawnNotification()` (unchanged — not relevant to human_review handoff path)
   - `clearAssignee` helper signature and behavior unchanged

2. **Add a regression-guard test to `task/controller/pkg/result/result_writer_test.go`**

   Place the new `Context` adjacent to the existing `Context("spawn notification", ...)` block. Name it:

   ```
   Context("spawn_notification + human_review handoff (spec 041 prod incident reproducer)", func() {
   ```

   Inside, add an `It` named:

   ```
   It("clears assignee when merged frontmatter has spawn_notification:true + incoming phase:human_review, spec 041 reproducer", func() {
   ```

   Test setup and assertions:
   - Write task file on disk with frontmatter: `task_identifier: test-task-uuid-1234, status: in_progress, phase: ai_review, assignee: pr-reviewer-agent, spawn_notification: true`
   - Call `writer.WriteResult` with `taskFile.Frontmatter` = `task_identifier, status: in_progress, phase: human_review`
   - Assertions on the written file:
     - `phase: human_review` preserved
     - `assignee` field cleared to `""` (no `assignee: pr-reviewer-agent` in frontmatter)
     - `previous_assignee: pr-reviewer-agent` populated
     - `spawn_notification` key absent (consumed by the branch)
     - no `## Retry Escalation` or `## Trigger Cap Escalation` sections
     - exactly one write

   Mirror the existing test builder pattern: `writeTaskFile()`, `WriteResult()`, `os.ReadFile()`, string assertions. Use the `identifier` variable from `BeforeEach` and the `fakeGit`/`fakeTime` from the suite.

3. **Run the source-ordering verification** to confirm the new line order satisfies AC#5 and AC#6:
   ```bash
   awk '/^func \(r \*resultWriter\) applyRetryCounter/,/^}/' task/controller/pkg/result/result_writer.go | grep -nE 'phase == "human_review"|SpawnNotification\(\)|applyTriggerCap\('
   ```
   Expected: the line for `phase == "human_review"` appears BEFORE the line for `SpawnNotification()`, AND `applyTriggerCap(` appears before `SpawnNotification()`.

4. **Run the reproducer test** (must pass after your changes):
   ```bash
   cd task/controller && go test ./pkg/result/... -run 'spawn_notification.*human_review' -v
   ```

5. **Run the controller precommit**:
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0. All existing tests (spec 039 `needs_input`, spec 039 `failed`, retry cap, trigger cap, prompt-075 regression) must still pass.
</requirements>

<constraints>
- Do NOT add a defensive duplicate of the `human_review` guard inside the `spawn_notification` early-return branch (rejected in spec Non-goals)
- Do NOT modify `clearAssignee`, `applyTriggerCap`, `applyRetryCap`, `mergeFrontmatter`, or any escalation helpers
- Do NOT touch `task_update_frontmatter_executor.go` or `result-deliverer.go`
- Do NOT commit — dark-factory handles git
- Follow project error-handling conventions (this prompt introduces no new error paths)
- Existing tests must still pass — do not delete or weaken any existing assertion
</constraints>

<verification>
Run the commands in sequence:
```bash
# Source-ordering audit (AC#5 + AC#6)
awk '/^func \(r \*resultWriter\) applyRetryCounter/,/^}/' task/controller/pkg/result/result_writer.go | grep -nE 'phase == "human_review"|SpawnNotification\(\)|applyTriggerCap\('

# Reproducer test (AC#1)
cd task/controller && go test ./pkg/result/... -run 'spawn_notification.*human_review' -v

# Full precommit (AC#10)
cd task/controller && make precommit
```

All three must pass. The reproducer test must show PASS on the new `It` block.
</verification>
