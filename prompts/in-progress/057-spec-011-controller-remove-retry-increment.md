---
status: committing
spec: [011-retry-counter-spawn-time-semantics]
summary: Removed retry_count increment from result_writer.go (executor now owns the counter), updated tests to match new read-only semantics, and updated docs/CHANGELOG accordingly.
container: agent-057-spec-011-controller-remove-retry-increment
dark-factory-version: v0.128.1-3-gf1cfca3-dirty
created: "2026-04-19T17:30:00Z"
queued: "2026-04-19T18:31:24Z"
started: "2026-04-19T19:54:12Z"
branch: dark-factory/retry-counter-spawn-time-semantics
---

<summary>
- Controller's result writer stops incrementing `retry_count` on any incoming `AgentStatus` — the executor owns the counter at spawn time (prompt 1)
- Escalation logic is preserved: writer still sets `phase: human_review` and appends `## Retry Escalation` when `retry_count >= max_retries`, but reads the counter as-is (executor already bumped it)
- Spec 010's "skip increment when incoming phase is human_review" guard is removed — it was only protecting against an increment that no longer happens
- Existing spawn-notification handling (deleting the `spawn_notification` flag from frontmatter) is preserved
- All affected tests in `result_writer_test.go` are updated to match the new semantics; tests that asserted the old increment are corrected
- `docs/task-flow-and-failure-semantics.md` and `docs/controller-design.md` are updated to document the new increment site
- `cd task/controller && make precommit` passes
</summary>

<objective>
Remove `retry_count` increment from `task/controller/pkg/result/result_writer.go`. The executor (prompt 1) now publishes the bump before spawning; the controller only applies escalation when `retry_count >= max_retries`. This is the second and final half of spec 011. Precondition: prompt 1 must already be applied (executor `PublishRetryCountBump` method must exist).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega patterns, external test packages
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `bborbe/errors`, never `fmt.Errorf`

**Precondition — verify prompt 1 was applied:**
```bash
grep -n "PublishRetryCountBump" task/executor/pkg/result_publisher.go task/executor/mocks/result_publisher.go
```
Both files must show the method. If missing, STOP and report that prompt 1 has not been applied.

**Key files to read before editing:**

- `task/controller/pkg/result/result_writer.go` — `applyRetryCounter` (lines 131–151):
  - Line 137: `if phase, _ := merged["phase"].(string); phase == "human_review"` — the spec 010 guard to REMOVE
  - Line 144: `retryCount := merged.RetryCount() + 1` — the increment to REMOVE (`+ 1` becomes just a read)
  - Line 145: `merged["retry_count"] = retryCount` — the write-back to REMOVE
  - Line 146-149: escalation check (`if retryCount >= merged.MaxRetries()`) — KEEP but use `merged.RetryCount()` (no +1)

- `task/controller/pkg/result/result_writer_test.go` — `Context("retry counter", ...)` block (around line 472) and `Context("spawn notification", ...)` block (around line 589); read the whole test file before editing

- `docs/task-flow-and-failure-semantics.md` — update the "Result Routing" pseudocode and "Silent infra failure" scenario section and the "Retry counter" terminology entry

- `docs/controller-design.md` — update "Command Processing" section to note that the writer reads `retry_count` (set by executor) rather than incrementing it

- `CHANGELOG.md` — check if `## Unreleased` exists before writing
</context>

<requirements>

1. **Rewrite `applyRetryCounter` in `task/controller/pkg/result/result_writer.go`**

   Replace the current method body with this implementation:

   ```go
   func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {
       if string(merged.Status()) == "completed" {
           return body
       }
       if merged.SpawnNotification() {
           delete(merged, "spawn_notification")
           return body
       }
       // retry_count is authoritative in the task file — the executor bumped it
       // at spawn time (spec 011). The writer only applies escalation.
       retryCount := merged.RetryCount()
       if retryCount >= merged.MaxRetries() {
           merged["phase"] = "human_review"
           body += r.escalationSection(retryCount, merged)
       }
       return body
   }
   ```

   Changes from current:
   - REMOVED: `if phase, _ := merged["phase"].(string); phase == "human_review" { return body }` (spec 010 guard — dead code)
   - REMOVED: `merged["retry_count"] = retryCount` (no longer writes the counter)
   - CHANGED: `retryCount := merged.RetryCount() + 1` → `retryCount := merged.RetryCount()` (reads without bumping)
   - KEPT: `merged.SpawnNotification()` branch with `delete(merged, "spawn_notification")`
   - KEPT: escalation check and `escalationSection`

2. **Update `Context("retry counter", ...)` block in `task/controller/pkg/result/result_writer_test.go`**

   The existing tests in this block must be updated to reflect the new semantics. Replace each test as follows:

   a. **"increments retry_count on first failure and keeps ai_review phase"** → rename and change assertion:
   ```go
   It("does not modify retry_count on failure and keeps ai_review phase", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nassignee: claude\nretry_count: 1\n---\nAgent output\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
           },
           Content: lib.TaskContent("Result body\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 1")) // unchanged — executor owns the counter
       Expect(s).To(ContainSubstring("phase: ai_review"))
       Expect(s).NotTo(ContainSubstring("human_review"))
   })
   ```
   Note: task file starts with `retry_count: 1` (executor already bumped it at spawn time).

   b. **"escalates to human_review after three failures (default max_retries)"** → rename and change setup:
   ```go
   It("escalates to human_review when retry_count (set by executor) meets default max_retries", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nretry_count: 3\nassignee: claude\n---\nAgent output\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
               "retry_count":     3,
           },
           Content: lib.TaskContent("Agent output\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 3")) // unchanged — executor set it
       Expect(s).To(ContainSubstring("phase: human_review"))
       Expect(s).To(ContainSubstring("## Retry Escalation"))
   })
   ```
   Note: `retry_count: 3` equals `max_retries: 3` (default) → escalates immediately without incrementing.

   c. **"does not modify retry_count when result is completed"** — keep this test unchanged (it already asserts no modification).

   d. **"escalates on first failure when max_retries is 0"** → rename and change setup:
   ```go
   It("escalates immediately when retry_count (set by executor) meets max_retries 0", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nmax_retries: 0\nretry_count: 1\nassignee: claude\n---\nAgent output\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
               "max_retries":     0,
               "retry_count":     1,
           },
           Content: lib.TaskContent("Agent output\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 1")) // unchanged
       Expect(s).To(ContainSubstring("phase: human_review"))
       Expect(s).To(ContainSubstring("## Retry Escalation"))
   })
   ```
   Note: executor bumped to 1 before spawn; 1 >= 0 → escalation.

   e. **"does not escalate before max_retries is reached"** → rename and fix assertion:
   ```go
   It("does not escalate when retry_count (set by executor) is below max_retries", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nretry_count: 3\nmax_retries: 5\nassignee: claude\n---\nAgent output\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
               "retry_count":     3,
               "max_retries":     5,
           },
           Content: lib.TaskContent("Agent output\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 3")) // unchanged — 3 < 5, no escalation
       Expect(s).NotTo(ContainSubstring("human_review"))
       Expect(s).NotTo(ContainSubstring("Retry Escalation"))
   })
   ```

3. **Update `Context("spawn notification", ...)` block in `result_writer_test.go`**

   a. Keep the test **"does not increment retry_count when spawn_notification is true"** unchanged — it still correctly verifies that spawn_notification:true skips all mutation and the flag is cleaned up.

   b. **"does increment retry_count when spawn_notification is absent"** — rename and change assertion to match new semantics:
   ```go
   It("does not modify retry_count when spawn_notification is absent", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\nretry_count: 0\n---\nOriginal body\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
           },
           Content: lib.TaskContent("Result body\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 0")) // unchanged — executor owns the counter
   })
   ```

4. **Verify `Context("needs_input result", ...)` block stays intact**

   After the changes above, run:
   ```bash
   grep -n "needs_input result\|does not increment retry_count when phase is human_review\|terminal guard" task/controller/pkg/result/result_writer_test.go
   ```
   Both tests must still be present. Do NOT modify them — they remain valid (the writer never increments, so these assertions are still correct).

5. **Update `docs/task-flow-and-failure-semantics.md`**

   a. In the `## Terminology` table, update the "Retry counter" row:
   ```
   | **Retry counter** | `retry_count` frontmatter field, incremented by the executor at job spawn time (spec 011). The controller reads it but never modifies it. |
   ```

   b. In the `## Result Routing (spec 010)` pseudocode block, update the `default (failed)` branch:
   ```
   default (failed):
       status = in_progress
       phase  = ai_review           ← re-enters executor allowlist
       retry_count: unchanged        ← executor already bumped it at spawn time (spec 011)
       if retry_count >= max_retries:
           phase = human_review     ← escalated
   ```
   Remove the old `retry_count++` line.

   c. Update the **"Why `failed` counts"** paragraph to reference the new site:
   ```
   **Why `failed` counts:** could be transient (network, rate limit, OOM). The executor bumps `retry_count` before each spawn attempt so the counter equals invocations attempted, not failure events observed.
   ```

   d. In the **"Silent infra failure (spec 009)"** scenario, update step 4:
   ```
   4. Flows through the normal `failed` path (ai_review). `retry_count` was already bumped when the Job was spawned; the synthesised failure does not bump it again.
   ```

   e. In the **Related specs** list at the top, add:
   ```
   - `specs/in-progress/011-retry-counter-spawn-time-semantics.md` — retry_count moved to spawn time
   ```

6. **Update `docs/controller-design.md`**

   In the **"Command Processing (Kafka → git)"** section, add a note after "merge frontmatter + apply retry counter":
   ```
   ├── read retry_count from merged frontmatter (set by executor at spawn time, spec 011)
   ├── if retry_count >= max_retries → set phase: human_review, append ## Retry Escalation
   ```
   Replace the old note "merge frontmatter + apply retry counter" with:
   ```
   ├── merge frontmatter + apply escalation check (counter set by executor, not incremented here)
   ```

7. **Update CHANGELOG.md**

   First check:
   ```bash
   grep -n "^## Unreleased" CHANGELOG.md | head -3
   ```
   If `## Unreleased` already exists (e.g., from prompt 1), APPEND the bullet. Otherwise INSERT a new section immediately above the first `## v` heading:
   ```markdown
   - fix: controller result writer no longer increments retry_count — counter is maintained by executor at spawn time, preventing inflation from kubectl job deletions (spec 011)
   - refactor: remove spec 010's phase==human_review guard from result writer — dead code after spawn-time accounting
   ```

</requirements>

<constraints>
- Do NOT change the executor package — all changes are in `task/controller/` and `docs/`
- Do NOT change `lib/` — `RetryCount()` accessor is already correct
- Do NOT commit — dark-factory handles git
- The escalation section (`## Retry Escalation`) must still be appended when `retry_count >= max_retries`; only the increment is removed
- `spawn_notification` flag handling (delete from merged frontmatter) must be preserved
- `completed` status early-return must be preserved
- Test package stays `result_test` (external package, matches existing convention)
- Use `github.com/bborbe/errors` for any error wrapping in new code
- All existing tests must pass (including `needs_input result` block added by spec 010)
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>
Verify precondition:
```bash
grep -n "PublishRetryCountBump" task/executor/pkg/result_publisher.go task/executor/mocks/result_publisher.go
```
Both must show the method. If missing, STOP.

Verify increment is removed from result_writer:
```bash
grep -n "RetryCount.*+ 1\|retry_count.*retryCount\|retryCount :=.*RetryCount.*1" task/controller/pkg/result/result_writer.go
```
Must return nothing (no increment expression).

Verify spec 010 guard is removed:
```bash
grep -n "phase.*human_review\|human_review.*phase" task/controller/pkg/result/result_writer.go
```
Must return nothing — the guard is gone; `human_review` only appears in `escalationSection` output.

Verify escalation check remains:
```bash
grep -n "MaxRetries\|human_review\|Retry Escalation" task/controller/pkg/result/result_writer.go
```
Must still show the escalation block.

Verify spawn_notification handling remains:
```bash
grep -n "SpawnNotification\|spawn_notification" task/controller/pkg/result/result_writer.go
```
Must show the branch and `delete(merged, ...)` call.

Verify tests updated — no test asserts an increment:
```bash
grep -n "retry_count: 4\|retry_count: 1.*first\|increments retry_count" task/controller/pkg/result/result_writer_test.go
```
Must return nothing (old increment assertions removed).

Verify docs updated:
```bash
grep -n "spec 011\|spawn time\|executor.*bump\|bumped" docs/task-flow-and-failure-semantics.md
```
Must show the new wording.

Run controller tests:
```bash
cd task/controller && make test
```
Must exit 0.

Run full precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
