---
status: completed
spec: [009-executor-job-failure-detection]
summary: Added SpawnNotification() and CurrentJob() accessors to TaskFrontmatter, modified applyRetryCounter to skip retry increment for spawn notifications, and added tests for all new behavior in both lib and task/controller.
container: agent-050-spec-009-lib-controller-spawn-notification
dark-factory-version: v0.125.1
created: "2026-04-18T20:00:00Z"
queued: "2026-04-18T19:29:44Z"
started: "2026-04-18T19:35:39Z"
completed: "2026-04-18T19:40:56Z"
branch: dark-factory/executor-job-failure-detection
---

<summary>
- `TaskFrontmatter` gains two new typed accessors: `SpawnNotification() bool` and `CurrentJob() string`
- The controller's `ResultWriter` skips the retry-count increment when a result carries `spawn_notification: true`
- Spawn notifications still write frontmatter fields (`current_job`, `job_started_at`) to the task file without side-effecting the retry counter
- Spawn notification body does not overwrite the existing task body (uses `req.Content` which the executor sets to the original task body)
- Tests cover: `SpawnNotification()` absent → false, present true → true; `CurrentJob()` absent → empty string, present → value
- Tests cover: spawn notification result skips retry increment; normal failure result still increments
- `cd lib && make precommit` and `cd task/controller && make precommit` both pass
</summary>

<objective>
Add foundational lib and controller changes required by spec 009. The executor (prompt 2) will publish a "spawn notification" result to Kafka after spawning a K8s Job; this result carries `spawn_notification: true` in its frontmatter to signal to the controller that it should write the tracking fields (`current_job`, `job_started_at`) without incrementing the retry counter. Without this change, every job spawn would produce a false retry increment, causing premature escalation.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — accessor conventions, error wrapping
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega, external test packages

**Precondition — verify spec 008 lib changes are applied (RetryCount/MaxRetries must exist):**
```bash
grep -n "func (f TaskFrontmatter) RetryCount\|func (f TaskFrontmatter) MaxRetries" lib/agent_task-frontmatter.go
```
Both must be present. If missing, STOP and report that spec 008 (prompt 047) must be applied first.

**Files to read before editing:**
- `lib/agent_task-frontmatter.go` — existing accessors (`Status`, `Phase`, `Assignee`, `Stage`, `RetryCount`, `MaxRetries`); add new ones here following the same pattern
- `lib/agent_task_test.go` — existing lib tests; add new accessor tests here (do NOT create a new file)
- `task/controller/pkg/result/result_writer.go` — `WriteResult` implementation; the retry-counter logic block to modify
- `task/controller/pkg/result/result_writer_test.go` — existing retry-counter tests; add spawn-notification bypass test here

**`TaskFrontmatter` is `map[string]interface{}`.** Values from YAML unmarshal as Go native types (bool → bool); values from JSON (Kafka) also unmarshal booleans as bool. No float64 handling needed for bool fields.

**Retry counter block (from `result_writer.go`, added by spec 008):**
```go
body := string(req.Content)
if string(merged.Status()) != "completed" {
    retryCount := merged.RetryCount() + 1
    merged["retry_count"] = retryCount
    if retryCount >= merged.MaxRetries() {
        merged["phase"] = "human_review"
        body += r.escalationSection(retryCount, merged)
    }
}
```
Change the outer condition to:
```go
if string(merged.Status()) != "completed" && !merged.SpawnNotification() {
    // ... unchanged retry logic ...
}
```
The spawn-notification branch still falls through to frontmatter marshalling and file write — only the retry increment is skipped.
</context>

<requirements>

1. **Add `SpawnNotification() bool` accessor to `lib/agent_task-frontmatter.go`**

   Insert after `MaxRetries()`. Return false when the key is absent or not a bool:
   ```go
   // SpawnNotification returns true when this result is a job-spawn tracking update
   // rather than an agent outcome. The controller skips the retry counter for these.
   func (f TaskFrontmatter) SpawnNotification() bool {
       v, _ := f["spawn_notification"].(bool)
       return v
   }
   ```

2. **Add `CurrentJob() string` accessor to `lib/agent_task-frontmatter.go`**

   Insert after `SpawnNotification()`:
   ```go
   // CurrentJob returns the K8s Job name recorded when the executor spawned a Job for this task.
   // Returns an empty string when not set.
   func (f TaskFrontmatter) CurrentJob() string {
       v, _ := f["current_job"].(string)
       return v
   }
   ```

3. **Add tests for `SpawnNotification()` and `CurrentJob()` in `lib/agent_task_test.go`**

   Add inside the existing `Describe("TaskFrontmatter", ...)` block (look for it and append — do NOT create a new file). Test package stays `lib_test`.

   Required test cases for `SpawnNotification()`:
   - Key absent → false
   - Key present as `bool(true)` → true
   - Key present as `bool(false)` → false

   Required test cases for `CurrentJob()`:
   - Key absent → `""`
   - Key present as non-empty string → that string
   - Key present as empty string → `""`

4. **Modify retry counter in `task/controller/pkg/result/result_writer.go`**

   Find the block:
   ```go
   if string(merged.Status()) != "completed" {
   ```
   Replace with:
   ```go
   if string(merged.Status()) != "completed" && !merged.SpawnNotification() {
   ```
   The body of the `if` block is UNCHANGED. No other modifications to `WriteResult`.

5. **Add spawn-notification bypass test to `task/controller/pkg/result/result_writer_test.go`**

   Add inside the existing `Context("retry counter", ...)` block (or append a new `Context` block adjacent to it):

   ```go
   Context("spawn notification", func() {
       It("does not increment retry_count when spawn_notification is true", func() {
           writeTaskFile("my-task.md",
               "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\nretry_count: 0\n---\nOriginal body\n")
           taskFile = lib.Task{
               TaskIdentifier: identifier,
               Frontmatter: lib.TaskFrontmatter{
                   "task_identifier":    "test-task-uuid-1234",
                   "status":             "in_progress",
                   "phase":              "ai_review",
                   "spawn_notification": true,
                   "current_job":        "claude-20260418120000",
                   "job_started_at":     "2026-04-18T12:00:00Z",
               },
               Content: lib.TaskContent("Original body\n"),
           }
           Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
           written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
           s := string(written)
           // retry_count must NOT be incremented
           Expect(s).To(ContainSubstring("retry_count: 0"))
           // current_job and job_started_at must be written
           Expect(s).To(ContainSubstring("current_job: claude-20260418120000"))
           Expect(s).To(ContainSubstring("job_started_at: 2026-04-18T12:00:00Z"))
           // phase must stay ai_review (no escalation)
           Expect(s).To(ContainSubstring("phase: ai_review"))
           Expect(s).NotTo(ContainSubstring("human_review"))
           Expect(s).NotTo(ContainSubstring("Retry Escalation"))
       })

       It("does increment retry_count when spawn_notification is absent", func() {
           writeTaskFile("my-task.md",
               "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\nretry_count: 0\n---\nOriginal body\n")
           taskFile = lib.Task{
               TaskIdentifier: identifier,
               Frontmatter: lib.TaskFrontmatter{
                   "task_identifier": "test-task-uuid-1234",
                   "status":          "in_progress",
                   "phase":           "ai_review",
               },
               Content: lib.TaskContent("Failed output\n"),
           }
           Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
           written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
           s := string(written)
           Expect(s).To(ContainSubstring("retry_count: 1"))
       })
   })
   ```

</requirements>

<constraints>
- Do NOT change `Status()`, `Phase()`, `Assignee()`, `Stage()`, `RetryCount()`, `MaxRetries()` — only add new accessors
- Do NOT change `TaskFrontmatter` type definition, `lib.Task` struct, or any Kafka topic schema
- Do NOT commit — dark-factory handles git
- Use `github.com/bborbe/errors` for any new error-returning code (new accessors do not return errors)
- All existing tests must pass (no regressions in lib or controller)
- Test package for lib tests is `lib_test` (external package per project convention)
- `make precommit` must pass in both `lib/` and `task/controller/` with exit code 0
- Must not change the retry counter logic for non-spawn-notification results (Cases 1-5 from spec 008 must still pass)
- Must not change `mergeFrontmatter`, `extractFrontmatter`, or the escalation section format
- Controller constraint from spec 008: do NOT change its Kafka consumer or the `escalationSection` method
</constraints>

<verification>
Verify spec 008 precondition:
```bash
grep -n "func (f TaskFrontmatter) RetryCount\|func (f TaskFrontmatter) MaxRetries" lib/agent_task-frontmatter.go
```
Must show both methods.

Run lib precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify new accessors exist:
```bash
grep -n "SpawnNotification\|CurrentJob" lib/agent_task-frontmatter.go
```
Must show both methods.

Run controller precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

Verify the retry condition change:
```bash
grep -n "SpawnNotification" task/controller/pkg/result/result_writer.go
```
Must show the modified condition.

Verify spawn-notification test exists:
```bash
grep -n "spawn_notification\|SpawnNotification" task/controller/pkg/result/result_writer_test.go
```
Must show the test cases.

Verify spec 008 retry tests still pass (no regressions):
```bash
cd task/controller && make test
```
Must exit 0.
</verification>
