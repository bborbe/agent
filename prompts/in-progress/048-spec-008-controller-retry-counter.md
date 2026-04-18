---
status: approved
spec: [008-task-retry-protection]
created: "2026-04-18T15:30:00Z"
queued: "2026-04-18T15:12:26Z"
branch: dark-factory/task-retry-protection
---

<summary>
- Controller's `ResultWriter` increments `retry_count` in the task file frontmatter on every non-completed result write
- When `retry_count >= max_retries` (default 3), the controller sets `phase: human_review` and appends a `## Retry Escalation` section to the file body containing timestamp, attempt count, assignee, and the last failure content
- Completed results (`status: completed`) leave `retry_count` unchanged and set `phase: done` as before
- `ResultWriter` receives a `libtime.CurrentDateTimeGetter` for the escalation timestamp (injected, not `time.Now()`)
- `main.go` and factory are updated to pass `currentDateTime` to `NewResultWriter`
- Tests cover: first failure increments count, third failure escalates, success leaves count unchanged, `max_retries: 0` escalates on first failure, `max_retries: N` escalates only at N
- Executor behaviour is unchanged — `allowedPhases` already excludes `human_review`, no executor edits needed
- `cd task/controller && make precommit` passes with exit 0
</summary>

<objective>
Implement the retry counter in `task/controller/pkg/result/result_writer.go`. After merging frontmatter, if the result is not completed, increment `retry_count` and — when it reaches `max_retries` — override `phase` to `human_review` and append a structured `## Retry Escalation` section. This prevents infinite respawn loops when an agent fails persistently. Precondition: prompt 1 (spec-008 lib changes) must already be applied; this prompt uses `TaskFrontmatter.RetryCount()` and `TaskFrontmatter.MaxRetries()`.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — constructor injection, error wrapping
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega patterns
- `~/.claude/plugins/marketplaces/coding/docs/go-time-injection.md` — `libtime.CurrentDateTimeGetter` injection pattern

**Precondition — verify prompt 1 was applied:**
```bash
grep -n "RetryCount\|MaxRetries" lib/agent_task-frontmatter.go
```
Both methods must exist before implementing this prompt. If they are missing, stop and report that prompt 1 has not been applied.

**Files to read before editing:**
- `task/controller/pkg/result/result_writer.go` — current `WriteResult` and `mergeFrontmatter`; add retry counter here
- `task/controller/pkg/result/result_writer_test.go` — existing tests; add retry counter tests, update constructor calls
- `task/controller/main.go` — locate `resultWriter := result.NewResultWriter(gitClient, a.TaskDir)` and add `currentDateTime` as third argument. `currentDateTime` is already declared earlier in `Run` via `currentDateTime := libtime.NewCurrentDateTime()`.
- `task/controller/pkg/factory/factory.go` — `CreateCommandConsumer` accepts `resultWriter result.ResultWriter`; no change needed here since the factory receives the already-constructed `ResultWriter`
- `lib/agent_task-frontmatter.go` — `RetryCount()`, `MaxRetries()`, `Status()`, `Assignee()` accessors used in the retry logic

**Architecture of `WriteResult` (current flow):**
1. Walk task directory, find file by `task_identifier`
2. Extract and merge existing frontmatter with `req.Frontmatter` (agent wins on conflict)
3. Marshal merged frontmatter, write `---\nfm\n---\nbody`
4. `AtomicWriteAndCommitPush`

**Retry counter insertion point:** After step 2 (frontmatter is merged), before step 3 (marshal). The retry logic reads from and writes into the `merged` map.

**Completed result detection:**
```go
merged.Status() == "completed"
```
`Status()` returns `domain.TaskStatus`; compare as string literal `"completed"`.

**Escalation condition (from spec failure modes table):**
```
retry_count := merged.RetryCount() + 1   // post-increment
merged["retry_count"] = retry_count
if retry_count >= merged.MaxRetries() {   // >= not >
    // escalate
}
```
- `max_retries: 0` → 1 >= 0 → escalate on first failure ✓
- default (3) → escalates on 3rd failure when retry_count reaches 3 ✓
- `max_retries: 10` → only escalates when retry_count reaches 10 ✓

**Escalation section format (`## Retry Escalation`):**
When escalating, BOTH the frontmatter and the body must be updated:
- Set `merged["phase"] = "human_review"` (overrides agent's phase)
- Append the following markdown to the body (after `req.Content`):

```
\n## Retry Escalation\n\n- **Timestamp:** <ISO8601>\n- **Attempts:** <retry_count>\n- **Assignee:** <assignee>\n- **Last error:** see agent output above\n
```

Use `currentDateTime.Now().UTC().Format(time.RFC3339)` for the timestamp.
Use `merged.Assignee()` for the assignee (returns empty string if absent — that is fine).

The body written to disk is: `string(req.Content) + escalationSection`.

**Time injection — `CurrentDateTimeGetter`:**
```go
import libtime "github.com/bborbe/time"

func NewResultWriter(
    gitClient gitclient.GitClient,
    taskDir string,
    currentDateTime libtime.CurrentDateTimeGetter,
) ResultWriter {
    return &resultWriter{
        gitClient:       gitClient,
        taskDir:         taskDir,
        currentDateTime: currentDateTime,
    }
}

type resultWriter struct {
    gitClient       gitclient.GitClient
    taskDir         string
    currentDateTime libtime.CurrentDateTimeGetter
}
```

`main.go` already has `currentDateTime` in scope (used for `CreateSyncLoop`). Pass it to `NewResultWriter`.

**Test mocking for time:**
In `result_writer_test.go`, import `libtimemocks "github.com/bborbe/time/mocks"` and use `libtimemocks.FakeCurrentDateTimeGetter`. Set it up in `BeforeEach`:
```go
fakeTime = &libtimemocks.FakeCurrentDateTimeGetter{}
fakeTime.NowReturns(libtime.DateTime(time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)))
writer = result.NewResultWriter(fakeGit, taskDir, fakeTime)
```

Check if `libtimemocks` is already vendored: `ls task/controller/vendor/github.com/bborbe/time/mocks/` — if it exists, use it. If not, search for `FakeCurrentDateTimeGetter` in the vendor tree to find the correct import path.

**Retry counter does NOT apply when result is completed:**
When `merged.Status() == "completed"`, skip the entire retry block. `retry_count` stays as it was in the existing file's frontmatter (showing total attempts taken), and `phase: done` is written as provided by the agent.

**Non-completed detection covers all failure paths:**
Any status other than `"completed"` triggers the counter increment: `"in_progress"`, `""` (empty/missing), any other value. This matches the spec's intent: the counter tracks all failures.
</context>

<requirements>

1. **Verify precondition — `RetryCount()` and `MaxRetries()` exist in `lib`**

   ```bash
   grep -n "func (f TaskFrontmatter) RetryCount\|func (f TaskFrontmatter) MaxRetries" lib/agent_task-frontmatter.go
   ```
   Both must be present. If missing, STOP and report that prompt 1 must be applied first.

2. **Add `currentDateTime libtime.CurrentDateTimeGetter` to `ResultWriter`**

   In `task/controller/pkg/result/result_writer.go`:

   a. Add import: `libtime "github.com/bborbe/time"` and `"time"` (for `time.RFC3339`)

   b. Update `NewResultWriter` signature:
   ```go
   func NewResultWriter(
       gitClient gitclient.GitClient,
       taskDir string,
       currentDateTime libtime.CurrentDateTimeGetter,
   ) ResultWriter {
       return &resultWriter{
           gitClient:       gitClient,
           taskDir:         taskDir,
           currentDateTime: currentDateTime,
       }
   }
   ```

   c. Add `currentDateTime libtime.CurrentDateTimeGetter` field to `resultWriter` struct.

3. **Implement retry counter logic in `WriteResult`**

   After the `merged := mergeFrontmatter(existingFrontmatter, req.Frontmatter)` line and BEFORE the `marshaledFrontmatter, err := yaml.Marshal(...)` line, insert:

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

   Then update the `newContent` assembly to use `body` instead of `string(req.Content)`:
   ```go
   newContent := []byte("---\n" + string(marshaledFrontmatter) + "---\n" + body)
   ```

4. **Add `escalationSection` helper method to `resultWriter`**

   ```go
   func (r *resultWriter) escalationSection(retryCount int, merged lib.TaskFrontmatter) string {
       ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
       return fmt.Sprintf(
           "\n## Retry Escalation\n\n- **Timestamp:** %s\n- **Attempts:** %d\n- **Assignee:** %s\n- **Last error:** see agent output above\n",
           ts,
           retryCount,
           string(merged.Assignee()),
       )
   }
   ```

   Add `"fmt"` import if not already present (it already is).

5. **Update `task/controller/main.go`**

   Find the line:
   ```go
   resultWriter := result.NewResultWriter(gitClient, a.TaskDir)
   ```
   Change to:
   ```go
   resultWriter := result.NewResultWriter(gitClient, a.TaskDir, currentDateTime)
   ```
   `currentDateTime` is already declared earlier in `Run` (via `currentDateTime := libtime.NewCurrentDateTime()` and passed into `CreateSyncLoop`). Reuse the same variable.

6. **Update tests in `task/controller/pkg/result/result_writer_test.go`**

   a. Import `libtimemocks "github.com/bborbe/time/mocks"` and `libtime "github.com/bborbe/time"` and `"time"`.

   b. Add `fakeTime *libtimemocks.FakeCurrentDateTimeGetter` to the `var` block.

   c. In `BeforeEach`, construct `fakeTime` and update the `writer` construction:
   ```go
   fakeTime = &libtimemocks.FakeCurrentDateTimeGetter{}
   fakeTime.NowReturns(libtime.DateTime(time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)))
   writer = result.NewResultWriter(fakeGit, taskDir, fakeTime)
   ```

   d. All existing tests that call `result.NewResultWriter(fakeGit, taskDir)` must be updated to include `fakeTime` as the third argument. Search the file for `NewResultWriter` and update every call site.

   e. Update any existing completed-result tests that use `status: done` in their `Frontmatter` map to use `status: completed` instead. Per spec, the retry counter's "success path" is triggered only by `status: completed`; any other status (including `done`) would otherwise trigger counter increment and break unrelated tests.

   f. Add a new `Context("retry counter", ...)` block with these test cases:

   **Case 1: First failure increments retry_count and keeps phase: ai_review**
   ```go
   It("increments retry_count on first failure and keeps ai_review phase", func() {
       writeTaskFile("my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\n---\nAgent output\n")
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
       Expect(s).To(ContainSubstring("phase: ai_review"))
       Expect(s).NotTo(ContainSubstring("human_review"))
       Expect(s).NotTo(ContainSubstring("Retry Escalation"))
   })
   ```

   **Case 2: Third failure (default max_retries=3) escalates to human_review**
   ```go
   It("escalates to human_review after three failures (default max_retries)", func() {
       writeTaskFile("my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nretry_count: 2\nassignee: claude\n---\nAgent output\n")
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
       Expect(s).To(ContainSubstring("retry_count: 3"))
       Expect(s).To(ContainSubstring("phase: human_review"))
       Expect(s).To(ContainSubstring("## Retry Escalation"))
       Expect(s).To(ContainSubstring("**Attempts:** 3"))
       Expect(s).To(ContainSubstring("**Assignee:** claude"))
       Expect(s).To(ContainSubstring("2026-04-18T12:00:00Z"))
   })
   ```

   **Case 3: Completed result leaves retry_count unchanged**
   ```go
   It("does not modify retry_count when result is completed", func() {
       writeTaskFile("my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nretry_count: 2\nassignee: claude\n---\nAgent output\n")
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "completed",
               "phase":           "done",
           },
           Content: lib.TaskContent("Success output\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 2"))
       Expect(s).To(ContainSubstring("phase: done"))
       Expect(s).NotTo(ContainSubstring("human_review"))
       Expect(s).NotTo(ContainSubstring("Retry Escalation"))
   })
   ```

   **Case 4: max_retries: 0 escalates on first failure**
   ```go
   It("escalates on first failure when max_retries is 0", func() {
       writeTaskFile("my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nmax_retries: 0\nassignee: claude\n---\nAgent output\n")
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
           },
           Content: lib.TaskContent("Failed\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 1"))
       Expect(s).To(ContainSubstring("phase: human_review"))
       Expect(s).To(ContainSubstring("## Retry Escalation"))
   })
   ```

   **Case 5: max_retries: 5 does not escalate before fifth failure**
   ```go
   It("does not escalate before max_retries is reached", func() {
       writeTaskFile("my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nretry_count: 3\nmax_retries: 5\nassignee: claude\n---\nAgent output\n")
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
           },
           Content: lib.TaskContent("Failed\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("retry_count: 4"))
       Expect(s).NotTo(ContainSubstring("human_review"))
       Expect(s).NotTo(ContainSubstring("Retry Escalation"))
   })
   ```

7. **Run `make test` iteratively after each meaningful change**

   After adding the time injection and updating the constructor: `cd task/controller && make test`
   After adding retry counter logic: `cd task/controller && make test`
   After all tests pass: `cd task/controller && make precommit`

</requirements>

<constraints>
- Do NOT change executor code, `allowedPhases`, or any file in `task/executor/` — spec non-goal
- Do NOT change agent result publishing — agents keep publishing results unchanged
- Do NOT change the Kafka event/request schema or `lib.Task` struct
- Do NOT change `mergeFrontmatter` or `extractFrontmatter` — only add retry logic after the merge
- Do NOT use `time.Now()` directly — use `currentDateTime.Now()` from the injected getter
- Do NOT use `context.Background()` — always forward the `ctx` parameter
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` for error wrapping — never `fmt.Errorf`
- `retry_count` and `max_retries` are written as plain integers in YAML frontmatter (not strings)
- The escalation section is appended AFTER `req.Content` — never replaces it
- `phase: human_review` is only set when `retry_count >= max_retries`; otherwise the agent's phase (typically `ai_review`) is preserved from the merged frontmatter
- Do NOT commit — dark-factory handles git
- All existing tests must pass (update constructor calls to pass `fakeTime`)
- `make precommit` must pass in `task/controller/` with exit code 0
- `make precommit` must also still pass in `lib/` (fallback generator change from prompt 1 must remain)

**Failure mode coverage (from spec §Failure Modes):**

| Trigger | Covered by |
|---------|-----------|
| Agent fails once (transient) | Case 1: retry_count 0→1, phase stays ai_review |
| Agent fails 3 times (persistent) | Case 2: retry_count→3, phase→human_review, escalation appended |
| Agent succeeds after 2 failures | Case 3: status=completed, retry_count stays at 2 |
| Task has `max_retries: 0` | Case 4: escalates on first failure |
| Task has `max_retries: 5` | Case 5: no escalation at retry_count=4 |
</constraints>

<verification>
First verify prompt 1 precondition:
```bash
grep -n "func (f TaskFrontmatter) RetryCount\|func (f TaskFrontmatter) MaxRetries" lib/agent_task-frontmatter.go
```
Must show both methods.

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

Verify retry counter is in result_writer:
```bash
grep -n "RetryCount\|MaxRetries\|retry_count\|human_review\|Retry Escalation" task/controller/pkg/result/result_writer.go
```
Must show all retry-counter symbols.

Verify time injection in constructor:
```bash
grep -n "CurrentDateTimeGetter\|currentDateTime" task/controller/pkg/result/result_writer.go
```
Must show the field and parameter.

Verify main.go passes currentDateTime:
```bash
grep -n "NewResultWriter" task/controller/main.go
```
Must show three arguments.

Verify executor is untouched:
```bash
git diff task/executor/
```
Must show no changes.

Verify escalation section format (frozen heading for downstream tooling):
```bash
grep -n "Retry Escalation" task/controller/pkg/result/result_writer.go
```
Must show the heading string.

Verify `max_retries: 0` test case exists:
```bash
grep -n "max_retries.*0\|max_retries is 0" task/controller/pkg/result/result_writer_test.go
```
Must show the test.
</verification>
