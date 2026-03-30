---
status: completed
container: agent-024-rename-taskfile-to-task
dark-factory-version: v0.69.0
created: "2026-03-30T19:18:21Z"
queued: "2026-03-30T19:48:49Z"
started: "2026-03-30T19:48:50Z"
completed: "2026-03-30T20:43:20Z"
---

<summary>
- The main task data structure is renamed from its legacy name to match the domain concept
- A new content type with validation prevents empty task content from being published
- All downstream consumers (controller, executor) use the updated names and types
- Mocks are regenerated and all tests pass with zero behavioral changes
</summary>

<objective>
Rename `lib.TaskFile` to `lib.Task` across the entire codebase. The "File" suffix is a historical artifact from when TaskFile was a separate type. Now that the types are unified, the canonical name should be `Task`.

Additionally, introduce a `TaskContent` named type (replacing the raw `string` for the `Content` field) with validation that content must be non-empty. This follows the same pattern as `TaskIdentifier`.
</objective>

<context>
Read CLAUDE.md for project conventions.

Files to read before making changes:
- `lib/agent_task-file.go` — current `TaskFile` struct definition, `Validate` method, `Ptr` method
- `lib/agent_task-file_test.go` — tests referencing `TaskFile`
- `lib/agent_task-identifier.go` — reference pattern for named types (`TaskIdentifier` with `String()`, `Validate()`, `Ptr()`)
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
   - Change the `Content` field type from `string` to `TaskContent`:
     ```go
     // Before:
     Content        string          `json:"content"`
     // After:
     Content        TaskContent     `json:"content"`
     ```
   - Add `Content` to the `Validate` method:
     ```go
     func (t Task) Validate(ctx context.Context) error {
         return validation.All{
             validation.Name("Object", t.Object),
             validation.Name("TaskIdentifier", t.TaskIdentifier),
             validation.Name("Content", t.Content),
         }.Validate(ctx)
     }
     ```

2. **Create `lib/agent_task-content.go`:**
   - Follow the same pattern as `lib/agent_task-identifier.go`
   - Create a named type: `type TaskContent string`
   - Add `String() string` method returning `string(t)`
   - Add `Validate(ctx context.Context) error` that returns `errors.Wrapf(ctx, validation.Error, "content missing")` when `len(t) == 0`
   - Imports: `context`, `github.com/bborbe/errors`, `github.com/bborbe/validation`
   - Standard license header

3. **Rename `lib/agent_task-file_test.go` to `lib/agent_task_test.go`:**
   - Rename the file using `git mv lib/agent_task-file_test.go lib/agent_task_test.go`
   - Replace all `lib.TaskFile` with `lib.Task` in the file
   - Replace `Describe("TaskFile"` with `Describe("Task"`
   - Replace `"returns nil for valid TaskFile"` with `"returns nil for valid Task"` (if such description exists)
   - Add tests for `TaskContent.Validate`: empty content returns error, non-empty content returns nil
   - Add test that `Task.Validate` fails when `Content` is empty

4. **Update `task/controller/pkg/scanner/vault_scanner.go`:**
   - In `ScanResult` struct: `Changed []lib.TaskFile` → `Changed []lib.Task`
   - In `scanFiles` return signature: `[]lib.TaskFile` → `[]lib.Task`
   - In `scanFiles` body: `var changed []lib.TaskFile` → `var changed []lib.Task`
   - In `processFile` return type: `*lib.TaskFile` → `*lib.Task`
   - In `processFile` body: `return &lib.TaskFile{` → `return &lib.Task{`
   - In `processFile` body: `Content: body` → `Content: lib.TaskContent(body)` (cast string to TaskContent)
   - In `injectAndStore` return type: `*lib.TaskFile` → `*lib.Task`
   - Variable name changes in `processFile`: the local `task` variable already exists (line 121: `task, wrote, werr := ...`), so keep it as `task`

5. **Update `task/controller/pkg/publisher/task_publisher.go`:**
   - Interface method: `PublishChanged(ctx context.Context, taskFile lib.TaskFile) error` → `PublishChanged(ctx context.Context, task lib.Task) error`
   - Implementation method signature: `func (p *taskPublisher) PublishChanged(ctx context.Context, taskFile lib.TaskFile) error` → `func (p *taskPublisher) PublishChanged(ctx context.Context, task lib.Task) error`
   - All references inside `PublishChanged` body: `taskFile.` → `task.`
   - Godoc comment: "task file" → "task"

6. **Update `task/controller/pkg/command/task_result_executor.go`:**
   - `var req lib.TaskFile` → `var req lib.Task`
   - Error message strings: `"malformed TaskFile command"` → `"malformed Task command"`
   - Error message strings: `"invalid TaskFile"` → `"invalid Task"`

7. **Update `task/controller/pkg/result/result_writer.go`:**
   - Interface method: `WriteResult(ctx context.Context, req lib.TaskFile) error` → `WriteResult(ctx context.Context, req lib.Task) error`
   - Implementation method: `func (r *resultWriter) WriteResult(ctx context.Context, req lib.TaskFile) error` → `func (r *resultWriter) WriteResult(ctx context.Context, req lib.Task) error`
   - In body: `req.Content` is used in string concatenation — cast to `string(req.Content)` since Go does not auto-convert named types in `+` expressions
   - Godoc comment: "TaskFile" → "Task"

8. **Update `task/controller/pkg/sync/sync_loop.go`:**
   - Variable name in range: `for _, taskFile := range result.Changed` → `for _, task := range result.Changed`
   - Update all `taskFile.` references in that loop to `task.`

9. **Update `task/executor/pkg/handler/task_event_handler.go`:**
   - `var taskFile lib.TaskFile` → `var task lib.Task`
   - All `taskFile.` references → `task.`
   - This includes: `taskFile.TaskIdentifier`, `taskFile.Frontmatter.Status()`, `taskFile.Frontmatter.Phase()`, `taskFile.Frontmatter.Assignee()`, `taskFile.Content`

10. **Update `task/executor/pkg/spawner/job_spawner.go`:**
    - Interface: `SpawnJob(ctx context.Context, taskFile lib.TaskFile, image string) error` → `SpawnJob(ctx context.Context, task lib.Task, image string) error`
    - Implementation: `func (s *jobSpawner) SpawnJob(ctx context.Context, taskFile lib.TaskFile, image string) error` → `func (s *jobSpawner) SpawnJob(ctx context.Context, task lib.Task, image string) error`
    - All `taskFile.` → `task.` in the method body
    - `task.Content` used as K8s env var `Value` (type `string`) — cast to `string(task.Content)`

11. **Update ALL test files** — apply the same `TaskFile` → `Task` and `taskFile` → `task` replacements in:
    - `task/controller/pkg/publisher/task_publisher_test.go`
    - `task/controller/pkg/command/task_result_executor_test.go`
    - `task/controller/pkg/result/result_writer_test.go`
    - `task/controller/pkg/sync/sync_loop_test.go`
    - `task/executor/pkg/handler/task_event_handler_test.go`
    - `task/executor/pkg/spawner/job_spawner_test.go`
    - Where test code assigns `Content: "some string"`, update to `Content: lib.TaskContent("some string")`

12. **Regenerate mocks** in task/controller:
    ```bash
    cd task/controller && make generate
    ```
    This regenerates:
    - `task/controller/mocks/task_publisher.go`
    - `task/controller/mocks/result_writer.go`

13. **Regenerate mocks** in task/executor:
    ```bash
    cd task/executor && make generate
    ```
    This regenerates:
    - `task/executor/mocks/job_spawner.go`

14. **Run tests** in both modules:
    ```bash
    cd task/controller && make test
    cd task/executor && make test
    ```
</requirements>

<constraints>
- This is a mechanical rename + one type introduction (`TaskContent`) — NO other logic changes
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
grep -rn "TaskFile" --include="*.go" lib/ task/controller/pkg/ task/executor/pkg/
```
Must produce no output.

Verify the old files are gone:

```bash
ls lib/agent_task-file.go lib/agent_task-file_test.go 2>&1
```
Must show "No such file" for both.

Verify the new files exist:

```bash
ls lib/agent_task.go lib/agent_task_test.go lib/agent_task-content.go
```
Must show all three files.

Run tests in task/controller:

```bash
cd task/controller && make test
```
Must pass with exit code 0.

Run tests in task/executor:

```bash
cd task/executor && make test
```
Must pass with exit code 0.
</verification>
