---
spec: ["041"]
status: draft
created: "2026-05-25T22:55:00Z"
---

<summary>
- Reorders `resultWriter.applyRetryCounter` so the `phase == "human_review"` assignee-clear guard runs BEFORE the `spawn_notification` early return
- Fixes the live 2026-05-25 prod incident where a pr-reviewer task landed at `phase: human_review` with `assignee: pr-reviewer-agent` still set and no `previous_assignee`
- Adds a Ginkgo unit test that reproduces the prod incident shape (merged frontmatter with `spawn_notification: true` + incoming `phase: human_review`) and asserts the post-fix on-disk shape
- Preserves the 2026-04-24 hotfix invariant: `applyTriggerCap` still runs before the `spawn_notification` early return
- Single production-code file change (`task/controller/pkg/result/result_writer.go`); test file extended in place, no new files
- No semantic change to `spawn_notification`, no refactor of `applyRetryCounter`, no defensive duplication of the guard
</summary>

<objective>
After this prompt, the `phase == "human_review"` guard in `resultWriter.applyRetryCounter` runs on every code path where the merged frontmatter has `phase == "human_review"`, including the path where `spawn_notification: true` is also present. The textual ordering inside the function body is such that no `return` statement appears between the function entry and the `human_review` check on inputs where `phase == "human_review"`. A new Ginkgo Context proves the fix by reproducing the prod incident input shape.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 conventions, external test packages, builder helpers (the existing `result_writer_test.go` already follows this style)
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — no new error paths introduced, but if any arise use `github.com/bborbe/errors`

Read these project files before editing:
- `task/controller/pkg/result/result_writer.go` — the file to edit. Focus on `applyRetryCounter` (currently lines 150-185). Note the current ordering:
  1. `status == "completed"` early return
  2. `applyTriggerCap` call (placed here by the 2026-04-24 hotfix, prompt 075)
  3. `SpawnNotification()` early return (deletes the key and returns)
  4. `applyRetryCap` call
  5. `phase == "human_review"` guard (line 180) — calls `clearAssignee(merged)`
- `task/controller/pkg/result/result_writer_test.go` — existing Ginkgo suite. Mirror the style of the existing `Context("spawn notification", ...)` (around line 715) and `Context("needs_input result", ...)` (around line 769). Reuse the `writeTaskFile` helper, the `fakeGit`/`fakeTime` setup in `BeforeEach`, and the `os.ReadFile` + string-assertion pattern.

Read the predecessor for context:
- `specs/in-progress/041-spawn-notification-early-return-skips-human-review-guard.md` — this spec; review the Failure Modes table and Acceptance Criteria AC#1 / AC#5 / AC#6
- `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` — precedent for the same reorder pattern applied to `applyTriggerCap`

**Background — why the reorder is the right fix:**

Spec 039 shipped the `human_review` guard at the bottom of `applyRetryCounter`. The guard's source is correct (proven by spec-039 ACs), but it is unreachable when the merged frontmatter also carries `spawn_notification: true`, because the `SpawnNotification()` branch returns earlier. Every agent result write inherits `spawn_notification: true` via `mergeFrontmatter` for the first post-spawn write (the executor set it spawn-time), so for a pr-reviewer agent whose first post-spawn result is `Done` + `NextPhase: human_review`, the guard never runs and the operator inbox filter (`assignee == ""`) misses the task.

The 2026-04-24 hotfix (prompt 075) already fixed the same bug class for `applyTriggerCap`. This prompt applies the identical pattern to the `human_review` guard.
</context>

<requirements>

1. **Reorder `applyRetryCounter` in `task/controller/pkg/result/result_writer.go`** so the `phase == "human_review"` guard runs BEFORE the `SpawnNotification()` early return.

   Current body (around lines 150-185, capture verbatim from current source):
   ```go
   func (r *resultWriter) applyRetryCounter(merged, existing lib.TaskFrontmatter, body string) string {
       if string(merged.Status()) == "completed" {
           return body
       }

       // Trigger-count cap enforcement runs unconditionally before any early
       // returns below: ...
       triggerCount := merged.TriggerCount()
       body = r.applyTriggerCap(merged, existing, triggerCount, body)

       if merged.SpawnNotification() {
           delete(merged, "spawn_notification")
           return body
       }

       // retry_count is authoritative in the task file ...
       retryCount := merged.RetryCount()
       body = r.applyRetryCap(merged, existing, retryCount, body)

       // needs_input: agent explicitly requested human review — clear assignee so task surfaces in operator inbox
       if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
           clearAssignee(merged) // sets previous_assignee and clears assignee
       }

       return body
   }
   ```

   New body (move the `human_review` guard up so it sits between `applyTriggerCap` and the `SpawnNotification()` early return; keep `applyTriggerCap` first per the 2026-04-24 hotfix invariant):
   ```go
   func (r *resultWriter) applyRetryCounter(merged, existing lib.TaskFrontmatter, body string) string {
       if string(merged.Status()) == "completed" {
           return body
       }

       // Trigger-count cap enforcement runs unconditionally before any early
       // returns below: ... (preserve the existing multi-line comment verbatim,
       // including the 2026-04-24 ba1bad61 reference — do NOT shorten or rewrite it)
       triggerCount := merged.TriggerCount()
       body = r.applyTriggerCap(merged, existing, triggerCount, body)

       // human_review assignee-clear guard runs BEFORE the spawn_notification
       // early return below. Reason: on a pr-reviewer agent's first post-spawn
       // write, the merged frontmatter carries spawn_notification: true (inherited
       // from the executor's spawn-time UpdateFrontmatterCommand) AND incoming
       // phase: human_review (from Result.NextPhase via resolveNextPhase). If this
       // guard were below the spawn_notification early return, clearAssignee would
       // never fire and the operator inbox filter (assignee == "") would miss the
       // task. This is the second instance of the same bug class — see prompt 075
       // (2026-04-24) for the applyTriggerCap precedent, and spec 041 for the
       // 2026-05-25 prod incident reproducer.
       if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
           clearAssignee(merged) // sets previous_assignee and clears assignee
       }

       if merged.SpawnNotification() {
           delete(merged, "spawn_notification")
           return body
       }

       // retry_count is authoritative in the task file — the executor bumped it
       // at spawn time (spec 011). The writer only applies escalation.
       retryCount := merged.RetryCount()
       body = r.applyRetryCap(merged, existing, retryCount, body)

       return body
   }
   ```

   Key invariants the new ordering must preserve:
   - `status == "completed"` early return stays at the top (unchanged).
   - `applyTriggerCap` stays above the `SpawnNotification()` early return (preserves 2026-04-24 hotfix, prompt 075).
   - The `human_review` guard moves to sit BETWEEN `applyTriggerCap` and the `SpawnNotification()` early return.
   - `applyRetryCap` stays below the `SpawnNotification()` early return (unchanged from spec 039 — `applyRetryCap` is not relevant to the `human_review` handoff path and intentionally remains suppressed on spawn writes).
   - The `clearAssignee` helper signature and behavior are unchanged.
   - The multi-line comment block above `applyTriggerCap` is preserved verbatim (it documents the 2026-04-24 ba1bad61 precedent).

2. **Add a new Ginkgo Context to `task/controller/pkg/result/result_writer_test.go`** that reproduces the prod incident.

   Location: inside the existing `Describe("ResultWriter", ...)` → `Describe("WriteResult", ...)`. Place the new Context adjacent to the existing `Context("spawn notification", ...)` block (around line 715). Suggested name: `Context("spawn_notification + human_review (spec 041 prod incident reproducer)", ...)`.

   The Context must contain at least one `It` block that:
   - Writes a task file on disk with frontmatter `task_identifier: test-task-uuid-1234`, `status: in_progress`, `phase: ai_review`, `assignee: pr-reviewer-agent`, `spawn_notification: true`. (The executor wrote `spawn_notification: true` spawn-time; `assignee` is the pr-reviewer agent name.)
   - Calls `writer.WriteResult` with `taskFile.Frontmatter` containing `task_identifier`, `status: in_progress`, and `phase: human_review` (the deliverer's `AgentStatusDone` + `NextPhase: human_review` shape — mimicking what `resolveNextPhase` writes).
   - Reads the resulting on-disk file and asserts ALL of:
     - `Expect(s).To(ContainSubstring("phase: human_review"))`
     - `Expect(s).NotTo(ContainSubstring("\nassignee: pr-reviewer-agent"))` — assignee in frontmatter cleared
     - `Expect(s).To(ContainSubstring("assignee: \"\""))` OR equivalent assertion that the YAML-serialized assignee is the empty string (mirror whichever form the existing spec-039 tests use — see the existing `Context("needs_input result", ...)` `It("clears assignee via line-180 guard ...")` test for the exact assertion form)
     - `Expect(s).To(ContainSubstring("previous_assignee: pr-reviewer-agent"))`
     - `Expect(s).NotTo(ContainSubstring("spawn_notification"))` — key consumed and deleted by the `SpawnNotification()` branch
     - `Expect(s).NotTo(ContainSubstring("## Retry Escalation"))` — no retry escalation on this path
     - `Expect(s).NotTo(ContainSubstring("## Trigger Cap Escalation"))` — no trigger cap escalation on this path
     - `Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))` — exactly one write

   The test name should reference the spec or incident (e.g. `It("clears assignee when merged frontmatter has spawn_notification + incoming phase human_review (spec 041)", ...)`). Use the existing `writeTaskFile` helper for the on-disk fixture and the `identifier` variable from `BeforeEach`.

   **Test naming for grep targetability:** the spec verification block runs `go test ./pkg/result/... -run 'spawn_notification.*human_review' -v`. The `Describe`/`Context`/`It` chain assembled into the Ginkgo test name must contain BOTH the literal substrings `spawn_notification` and `human_review` so the regex matches. The suggested Context name above satisfies this — keep both substrings if you rename.

3. **Do NOT add a defensive duplicate of the `human_review` guard** inside the `spawn_notification` early-return branch. The reorder is the fix; duplicating doctrine across two sites is explicitly rejected in the spec's Non-goals.

4. **Do NOT modify** the `clearAssignee` helper, the `applyTriggerCap` function, the `applyRetryCap` function, the `restoreExistingPhase` helper, the `mergeFrontmatter` function, or any escalation-section helpers. This is a textual reorder of three lines plus a new comment block.

5. **Do NOT modify** the existing `Context("spawn notification", ...)` test (around line 715) — its `phase: ai_review` input does NOT trigger the `human_review` guard, so the test's assertions are unchanged by this reorder. Similarly do NOT modify the existing `Context("needs_input result", ...)` tests; the existing test at the end of that Context (`clears assignee via line-180 guard when agent legitimately handed off via Result.NextPhase=human_review`) feeds NO `spawn_notification`, so it exercises the same guard via a different path and must continue to pass unchanged.

6. **Verify the source-ordering acceptance criterion (AC#5 + AC#6)** by running, from the repo root:
   ```bash
   awk '/^func \(r \*resultWriter\) applyRetryCounter/,/^}/' task/controller/pkg/result/result_writer.go | grep -nE 'phase == "human_review"|SpawnNotification\(\)|applyTriggerCap\('
   ```
   Expected: three matched lines. The line numbers (from `grep -n`, i.e. line number within the awk-extracted function body, not within the file) must satisfy:
   - `applyTriggerCap(` line number < `SpawnNotification()` line number  (AC#6, preserves 2026-04-24 hotfix)
   - `phase == "human_review"` line number < `SpawnNotification()` line number  (AC#5, this fix)

7. **Run the regression check from the spec** to confirm the new test fails on master and passes after the fix:
   ```bash
   cd task/controller && go test ./pkg/result/... -run 'spawn_notification.*human_review' -v
   ```
   Must pass after your changes.

8. **Run the controller's full precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0. The existing spec-039 Contexts (`needs_input`, `failed`, retry cap, trigger cap) and the 2026-04-24 hotfix regression test (`keeps phase: human_review sticky despite inherited spawn_notification=true at already-parked task`) must all still pass — these are AC#2, AC#3, AC#4, AC#6, AC#7 from the spec.

</requirements>

<constraints>
- Do NOT touch `task/executor/pkg/command/task_update_frontmatter_executor.go` — the `spawn_notification` write site is out of scope per the spec's Non-goals.
- Do NOT touch `lib/delivery/result-deliverer.go` — the `AgentStatusDone` branch already writes `phase: human_review` correctly via `resolveNextPhase` (spec 039 AC#6).
- Do NOT refactor `applyRetryCounter` into discrete passes called from `WriteResult` (Option B in the driving task) — explicitly deferred.
- Do NOT introduce a feature flag, env var, per-agent override, or any opt-out for the reordered guard — explicitly forbidden by the spec's Non-goals ("An escape hatch on the goal is itself a regression").
- Do NOT migrate the existing on-disk pr-reviewer incident task — operator clears it by hand (spec's Non-goals).
- Do NOT change the `status == "completed"` early return at the top of `applyRetryCounter` — its placement and behavior are unchanged.
- Do NOT commit — dark-factory handles git.
- Follow project conventions: error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`), but this prompt should introduce no new error paths.
- Existing tests must still pass. Do NOT delete or weaken any existing assertion in `result_writer_test.go`.
</constraints>

<verification>
From the repo root (`/workspace`):

```bash
# Source-ordering audit (AC#5 + AC#6)
awk '/^func \(r \*resultWriter\) applyRetryCounter/,/^}/' task/controller/pkg/result/result_writer.go | grep -nE 'phase == "human_review"|SpawnNotification\(\)|applyTriggerCap\('
```
Expected: `applyTriggerCap(` line < `SpawnNotification()` line AND `phase == "human_review"` line < `SpawnNotification()` line.

```bash
# Reproducer test (AC#1)
cd task/controller && go test ./pkg/result/... -run 'spawn_notification.*human_review' -v
```
Expected: PASS after the fix; the new Ginkgo Context's `It` block is in the output.

```bash
# Full controller precommit (AC#10)
cd task/controller && make precommit
```
Expected: exit 0; final log line contains `ready to commit`.
</verification>
