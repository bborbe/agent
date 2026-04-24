---
status: approved
spec: [016-partial-frontmatter-publishers]
created: "2026-04-24T10:00:00Z"
queued: "2026-04-24T10:06:18Z"
branch: dark-factory/partial-frontmatter-publishers
---

<summary>
- Adds an integration test to `task/controller/pkg/command/` that verifies `trigger_count` is preserved when an `IncrementFrontmatterCommand` is followed by a spawn-notification `UpdateFrontmatterCommand`
- The test proves the core fix: an `UpdateFrontmatterCommand` with only `current_job`, `job_started_at`, `spawn_notification` does NOT revert the previously incremented `trigger_count`
- A companion test verifies that a failure `UpdateFrontmatterCommand` (keys: `status`, `phase`, `current_job`) also does not touch `trigger_count`
- Uses the existing real-file test infrastructure already in place for `IncrementFrontmatterExecutor` and `UpdateFrontmatterExecutor` tests (no new test infrastructure needed)
- `cd task/controller && make precommit` passes
</summary>

<objective>
Add integration tests to the controller command package proving that an `UpdateFrontmatterCommand` for spawn notification (keys: `current_job`, `job_started_at`, `spawn_notification`) and for failure (keys: `status`, `phase`, `current_job`) cannot clobber a `trigger_count` that was written by a prior `IncrementFrontmatterCommand`. This is the controller-side acceptance test for spec 016: it validates that the UpdateFrontmatterExecutor's partial-merge semantics correctly preserve all keys not named in the Updates map.

This prompt depends on spec 015 being complete (the `IncrementFrontmatterExecutor` and `UpdateFrontmatterExecutor` must exist in `task/controller/pkg/command/`).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, DescribeTable
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf

**Verify the prerequisite executors exist before starting:**
```bash
grep -n "NewIncrementFrontmatterExecutor\|NewUpdateFrontmatterExecutor" task/controller/pkg/command/task_increment_frontmatter_executor.go task/controller/pkg/command/task_update_frontmatter_executor.go 2>/dev/null
```
If either file is missing, stop — the spec 015 prompts have not been applied.

**Key files to read in full before editing:**

- `task/controller/pkg/command/task_increment_frontmatter_executor_test.go` — existing tests; understand the test setup: how a temp directory is created, how a task YAML file is seeded with initial frontmatter, how the executor is invoked via `cdb.CommandObject`, and how the resulting file bytes are parsed back to assert frontmatter values
- `task/controller/pkg/command/task_update_frontmatter_executor_test.go` — existing tests for UpdateFrontmatterExecutor; understand the same test setup pattern
- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — the executor under test; note the function signature for `NewIncrementFrontmatterExecutor`
- `task/controller/pkg/command/task_update_frontmatter_executor.go` — the executor under test; note the function signature for `NewUpdateFrontmatterExecutor`
- `lib/agent_task-commands.go` — `IncrementFrontmatterCommand`, `UpdateFrontmatterCommand`, and their operation constants

Run these before editing to understand the test infrastructure:
```bash
grep -n "tmpDir\|os.MkdirTemp\|WriteFile\|cdb.CommandObject\|ExecuteTx\|frontmatter\|trigger_count" task/controller/pkg/command/task_increment_frontmatter_executor_test.go | head -40
grep -n "func.*suite\|RunSpecs\|RegisterFailHandler" task/controller/pkg/command/ -r | head -10
```
</context>

<requirements>

1. **Verify spec 015 prerequisites**

   ```bash
   grep -n "NewIncrementFrontmatterExecutor" task/controller/pkg/command/task_increment_frontmatter_executor.go
   grep -n "NewUpdateFrontmatterExecutor" task/controller/pkg/command/task_update_frontmatter_executor.go
   ```
   Both must return matches. If either is absent, stop.

2. **Read the existing executor test files in full**

   Read `task/controller/pkg/command/task_increment_frontmatter_executor_test.go` and `task_update_frontmatter_executor_test.go` completely to understand:
   - How the temp directory + task file is set up (seeded YAML frontmatter)
   - The exact shape of a `cdb.CommandObject` with `IncrementFrontmatterCommand` or `UpdateFrontmatterCommand` payload
   - How the executor's `ExecuteTx` (or equivalent) is called
   - How the resulting file bytes are read back and the frontmatter is parsed for assertions
   - What fake/stub `GitClient` is used (if any) — or whether tests use a real gitclient with a temp git repo

   Do NOT deviate from the established test infrastructure pattern.

3. **Add integration tests to the appropriate test file**

   The tests must go into either:
   - A new file `task/controller/pkg/command/task_frontmatter_sequence_test.go`, OR
   - Appended to `task/controller/pkg/command/task_update_frontmatter_executor_test.go`

   Prefer a new file if the existing files are long (>150 lines). Match the exact Ginkgo structure and package name used in the existing test files.

   **Test A — increment then spawn-notification preserves trigger_count:**

   Setup:
   - Create a temp task file with frontmatter:
     ```yaml
     task_identifier: seq-test-001
     trigger_count: 0
     max_triggers: 3
     status: in_progress
     phase: ai_review
     ```
   - Invoke `IncrementFrontmatterExecutor` with `IncrementFrontmatterCommand{TaskIdentifier: "seq-test-001", Field: "trigger_count", Delta: 1}`
   - Assert `trigger_count == 1` on disk after the increment

   Then:
   - Invoke `UpdateFrontmatterExecutor` with:
     ```go
     UpdateFrontmatterCommand{
         TaskIdentifier: "seq-test-001",
         Updates: lib.TaskFrontmatter{
             "current_job":        "claude-20260424120000",
             "job_started_at":     "2026-04-24T12:00:00Z",
             "spawn_notification": true,
         },
     }
     ```
     Note: `lib.TaskFrontmatter` is `map[string]interface{}` (verified in `lib/agent_task-frontmatter.go:14`) — the composite literal above is the correct Go syntax.
   - Read the file back
   - Assert `trigger_count == 1` (unchanged by the update — not reset to 0)
   - Assert `max_triggers == 3` (unchanged — this is the core invariant the spec defends)
   - Assert `current_job == "claude-20260424120000"` (correctly written)
   - Assert `job_started_at == "2026-04-24T12:00:00Z"` (correctly written)
   - Assert `spawn_notification == true` (correctly written)
   - Assert `status == "in_progress"` (unchanged by the update)
   - Assert `phase == "ai_review"` (unchanged by the update)

   **Test B — increment then failure-update preserves trigger_count:**

   Setup:
   - Create a temp task file with frontmatter:
     ```yaml
     task_identifier: seq-test-002
     trigger_count: 2
     max_triggers: 3
     status: in_progress
     phase: ai_review
     current_job: claude-old-job
     ```

   Then:
   - Invoke `UpdateFrontmatterExecutor` with:
     ```go
     UpdateFrontmatterCommand{
         TaskIdentifier: "seq-test-002",
         Updates: lib.TaskFrontmatter{
             "status":      "in_progress",
             "phase":       "ai_review",
             "current_job": "",
         },
     }
     ```
   - Read the file back
   - Assert `trigger_count == 2` (unchanged — trigger_count is NOT in the Updates map)
   - Assert `status == "in_progress"` (correctly written)
   - Assert `phase == "ai_review"` (correctly written)
   - Assert `current_job == ""` (correctly written)

   **Test C — UpdateFrontmatterCommand with empty Updates is a no-op:**

   Setup:
   - Create a temp task file with frontmatter:
     ```yaml
     task_identifier: seq-test-003
     trigger_count: 1
     status: in_progress
     ```

   Then:
   - Invoke `UpdateFrontmatterExecutor` with `UpdateFrontmatterCommand{TaskIdentifier: "seq-test-003", Updates: nil}`
   - Read the file back
   - Assert `trigger_count == 1` (file unchanged)
   - Assert `status == "in_progress"` (file unchanged)

4. **Match the existing Ginkgo suite bootstrap**

   The package `task/controller/pkg/command/` must already have a `*_suite_test.go` file with `RunSpecs`. If not:
   ```bash
   ls task/controller/pkg/command/*suite*test*
   ```
   If missing, add one following the pattern from any other package's suite file (e.g. `task/controller/pkg/result/`).

5. **Run tests iteratively**

   ```bash
   cd task/controller && make test
   ```
   Must exit 0. Fix any failures before running `make precommit`.

</requirements>

<constraints>
- Do NOT modify `task/executor/`, `lib/`, `prompt/`, or `agent/claude/` in this prompt.
- The tests MUST validate the on-disk file contents after each command — assert `trigger_count` by reading the file bytes back and parsing frontmatter, not by mocking the write.
- Use the same gitclient/file setup as the existing executor tests in the same package — do not invent a new test infrastructure.
- Use Ginkgo v2 only (`Describe`, `Context`, `It`, `BeforeEach`, `AfterEach` for cleanup). No Ginkgo v1.
- Use `github.com/bborbe/errors` for any new error wrapping — never `fmt.Errorf`.
- All existing tests must pass.
- Do NOT commit — dark-factory handles git.
- `cd task/controller && make precommit` must exit 0.
</constraints>

<verification>

Verify test file exists:
```bash
ls task/controller/pkg/command/*sequence*test* task/controller/pkg/command/*frontmatter*test* 2>/dev/null || ls task/controller/pkg/command/task_update_frontmatter_executor_test.go
```
Must show the new test file or the updated existing file.

Verify the key assertions exist (trigger_count preserved):
```bash
grep -n "trigger_count.*1\|seq-test-001\|seq-test-002" task/controller/pkg/command/ -r --include="*_test.go"
```
Must show test cases.

Run all controller tests:
```bash
cd task/controller && make test
```
Must exit 0.

Run precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

</verification>
