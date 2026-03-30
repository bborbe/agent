---
status: draft
created: "2026-03-30T19:18:21Z"
---

<summary>
- The `TaskFile` struct in `lib/` is renamed to `Task` since the old separate Task type was already deleted
- The source file is renamed from `agent_task-file.go` to `agent_task.go` (and test file accordingly)
- All references across task/controller and task/executor are updated from `lib.TaskFile` to `lib.Task`
- Variable names `taskFile` become `task` where they don't shadow the package name
- Interface signatures, mock comments, godoc, and log messages all reflect the new name
- Mocks are regenerated in both task/controller and task/executor
- No behavioral or logic changes — pure mechanical rename
</summary>

<objective>
Rename `lib.TaskFile` to `lib.Task` across the entire codebase. The "File" suffix is a historical artifact from when TaskFile was a separate type. Now that the types are unified, the canonical name should be `Task`.
</objective>

<context>
Read CLAUDE.md for project conventions.

Files to read before making changes:
- `lib/agent_task-file.go` — current `TaskFile` struct definition, `Validate` method, `Ptr` method
- `lib/agent_task-file_test.go` — tests referencing `TaskFile`
- `task/controller/pkg/scanner/vault_scanner.go` — `ScanResult.Changed []lib.TaskFile`, `scanFiles` return type, `processFile` return type, `injectAndStore` return type, struct literal `&lib.TaskFile{...}`
- `task/controller/pkg/publisher/task_publisher.go` — `PublishChanged(ctx, taskFile lib.TaskFile)` interface and implementation
- `task/controller/pkg/publisher/task_publisher_test.go` — test references
- `task/controller/pkg/command/task_result_executor.go` — `var req lib.TaskFile`, error messages mentioning "TaskFile"
- `task/controller/pkg/command/task_result_executor_test.go` — test references
- `task/controller/pkg/result/result_writer.go` — `WriteResult(ctx, req lib.TaskFile)` interface and implementation
- `task/controller/pkg/result/result_writer_test.go` — test references
- `task/controller/pkg/sync/sync_loop.go` — `taskFile` variable in range loop
- `task/controller/pkg/sync/sync_loop_test.go` — test references
- `task/executor/pkg/handler/task_event_handler.go` — `var taskFile lib.TaskFile`, all `taskFile.` accesses
- `task/executor/pkg/handler/task_event_handler_test.go` — test references
- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob(ctx, taskFile lib.TaskFile, image string)` interface and implementation
- `task/executor/pkg/spawner/job_spawner_test.go` — test references
</context>

<requirements>
1. **Rename `lib/agent_task-file.go` to `lib/agent_task.go`:**
   - Rename the file using `git mv lib/agent_task-file.go lib/agent_task.go`
   - Inside the file, rename the struct:
     ```go
     // Before:
     type TaskFile struct {
     // After:
     type Task struct {
     ```
   - Update the comment above the struct: replace "TaskFile" with "Task" in the godoc
   - Update the `Validate` method receiver:
     ```go
     // Before:
     func (t TaskFile) Validate(ctx context.Context) error {
     // After:
     func (t Task) Validate(ctx context.Context) error {
     ```
   - Update the `Ptr` method receiver and return type:
     ```go
     // Before:
     func (t TaskFile) Ptr() *TaskFile {
         return &t
     }
     // After:
     func (t Task) Ptr() *Task {
         return &t
     }
     ```

2. **Rename `lib/agent_task-file_test.go` to `lib/agent_task_test.go`:**
   - Rename the file using `git mv lib/agent_task-file_test.go lib/agent_task_test.go`
   - Replace all `lib.TaskFile` with `lib.Task` in the file
   - Replace `Describe("TaskFile"` with `Describe("Task"`
   - Replace `"returns nil for valid TaskFile"` with `"returns nil for valid Task"` (if such description exists)

3. **Update `task/controller/pkg/scanner/vault_scanner.go`:**
   - In `ScanResult` struct: `Changed []lib.TaskFile` → `Changed []lib.Task`
   - In `scanFiles` return signature: `[]lib.TaskFile` → `[]lib.Task`
   - In `scanFiles` body: `var changed []lib.TaskFile` → `var changed []lib.Task`
   - In `processFile` return type: `*lib.TaskFile` → `*lib.Task`
   - In `processFile` body: `return &lib.TaskFile{` → `return &lib.Task{`
   - In `injectAndStore` return type: `*lib.TaskFile` → `*lib.Task`
   - Variable name changes in `processFile`: the local `task` variable already exists (line 121: `task, wrote, werr := ...`), so keep it as `task`

4. **Update `task/controller/pkg/publisher/task_publisher.go`:**
   - Interface method: `PublishChanged(ctx context.Context, taskFile lib.TaskFile) error` → `PublishChanged(ctx context.Context, task lib.Task) error`
   - Implementation method signature: `func (p *taskPublisher) PublishChanged(ctx context.Context, taskFile lib.TaskFile) error` → `func (p *taskPublisher) PublishChanged(ctx context.Context, task lib.Task) error`
   - All references inside `PublishChanged` body: `taskFile.` → `task.`
   - Godoc comment: "task file" → "task"

5. **Update `task/controller/pkg/command/task_result_executor.go`:**
   - `var req lib.TaskFile` → `var req lib.Task`
   - Error message strings: `"malformed TaskFile command"` → `"malformed Task command"`
   - Error message strings: `"invalid TaskFile"` → `"invalid Task"`

6. **Update `task/controller/pkg/result/result_writer.go`:**
   - Interface method: `WriteResult(ctx context.Context, req lib.TaskFile) error` → `WriteResult(ctx context.Context, req lib.Task) error`
   - Implementation method: `func (r *resultWriter) WriteResult(ctx context.Context, req lib.TaskFile) error` → `func (r *resultWriter) WriteResult(ctx context.Context, req lib.Task) error`
   - Godoc comment: "TaskFile" → "Task"

7. **Update `task/controller/pkg/sync/sync_loop.go`:**
   - Variable name in range: `for _, taskFile := range result.Changed` → `for _, task := range result.Changed`
   - Update all `taskFile.` references in that loop to `task.`

8. **Update `task/executor/pkg/handler/task_event_handler.go`:**
   - `var taskFile lib.TaskFile` → `var task lib.Task`
   - All `taskFile.` references → `task.`
   - This includes: `taskFile.TaskIdentifier`, `taskFile.Frontmatter.Status()`, `taskFile.Frontmatter.Phase()`, `taskFile.Frontmatter.Assignee()`, `taskFile.Content`

9. **Update `task/executor/pkg/spawner/job_spawner.go`:**
   - Interface: `SpawnJob(ctx context.Context, taskFile lib.TaskFile, image string) error` → `SpawnJob(ctx context.Context, task lib.Task, image string) error`
   - Implementation: `func (s *jobSpawner) SpawnJob(ctx context.Context, taskFile lib.TaskFile, image string) error` → `func (s *jobSpawner) SpawnJob(ctx context.Context, task lib.Task, image string) error`
   - All `taskFile.` → `task.` in the method body

10. **Update ALL test files** — apply the same `TaskFile` → `Task` and `taskFile` → `task` replacements in:
    - `task/controller/pkg/publisher/task_publisher_test.go`
    - `task/controller/pkg/command/task_result_executor_test.go`
    - `task/controller/pkg/result/result_writer_test.go`
    - `task/controller/pkg/sync/sync_loop_test.go`
    - `task/executor/pkg/handler/task_event_handler_test.go`
    - `task/executor/pkg/spawner/job_spawner_test.go`

11. **Regenerate mocks** in task/controller:
    ```bash
    cd /workspace/task/controller && make generate
    ```
    This regenerates:
    - `task/controller/mocks/task_publisher.go`
    - `task/controller/mocks/result_writer.go`

12. **Regenerate mocks** in task/executor:
    ```bash
    cd /workspace/task/executor && make generate
    ```
    This regenerates:
    - `task/executor/mocks/job_spawner.go`

13. **Run tests** in both modules:
    ```bash
    cd /workspace/task/controller && make test
    cd /workspace/task/executor && make test
    ```
</requirements>

<constraints>
- This is a pure mechanical rename — NO logic changes, NO behavioral changes
- `TaskFrontmatter` keeps its name (it IS frontmatter, not being renamed)
- `TaskIdentifier` keeps its name
- `TaskAssignee` keeps its name
- Variable names: `taskFile` → `task` where it doesn't shadow a package import named `task`
- In files that import a package aliased as `task`, keep the variable name as something else (check each file)
- Use `git mv` for file renames to preserve git history
- Do NOT update CHANGELOG.md (trivial rename)
- Do NOT commit — dark-factory handles git
- Existing tests must still pass with zero behavioral changes
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
</constraints>

<verification>
Verify no references to `TaskFile` remain in source code (excluding mocks, which are regenerated):

```bash
grep -rn "TaskFile" --include="*.go" /workspace/lib/ /workspace/task/controller/pkg/ /workspace/task/executor/pkg/
```
Must produce no output.

Verify the old files are gone:

```bash
ls /workspace/lib/agent_task-file.go /workspace/lib/agent_task-file_test.go 2>&1
```
Must show "No such file" for both.

Verify the new files exist:

```bash
ls /workspace/lib/agent_task.go /workspace/lib/agent_task_test.go
```
Must show both files.

Run tests in task/controller:

```bash
cd /workspace/task/controller && make test
```
Must pass with exit code 0.

Run tests in task/executor:

```bash
cd /workspace/task/executor && make test
```
Must pass with exit code 0.
</verification>
