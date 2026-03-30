---
status: approved
created: "2026-03-30T17:29:00Z"
queued: "2026-03-30T18:21:32Z"
---

<summary>
- The executor consumes the unified file-based task type from Kafka instead of the old structured type
- Status, phase, and assignee are read from frontmatter accessors instead of direct struct fields
- Phase checking correctly handles missing phase (nil pointer from accessor)
- The job spawner receives the file content as TASK_CONTENT and the UUID as TASK_ID
- Duplicate tracking uses the stable UUID business key
- All executor tests use frontmatter maps instead of typed fields
- Mocks are regenerated after interface signature changes
</summary>

<objective>
Update the task/executor service to consume the unified file-based task type from Kafka. The handler reads task metadata from frontmatter accessors, and the job spawner passes content and UUID as environment variables to K8s Jobs.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Prerequisites:** Previous prompts modified `lib/` types (Task deleted, TaskFile unified with base.Object) and task/controller (scanner/publisher/sync_loop now produce TaskFile events).

Key files to read before making changes:
- `lib/agent_task-file.go` — `TaskFile` (after previous prompts: has `base.Object[base.Identifier]` + `TaskIdentifier` + `Frontmatter` + `Content`)
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` with `Status()`, `Phase() *domain.TaskPhase` (pointer, after previous prompt), `Assignee()` accessors
- `task/executor/pkg/handler/task_event_handler.go` — currently deserializes `lib.Task`, checks typed fields
- `task/executor/pkg/handler/task_event_handler_test.go` — tests constructing `lib.Task` objects
- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob(ctx, task lib.Task, image string)` interface
- `task/executor/pkg/spawner/job_spawner_test.go` — tests using `lib.Task`
- `task/executor/pkg/factory/factory.go` — factory wiring
- `task/executor/mocks/` — generated mocks
</context>

<requirements>
1. **Modify `task/executor/pkg/handler/task_event_handler.go`** — Switch from `lib.Task` to `lib.TaskFile`:

   a. Change the deserialization in `ConsumeMessage`:
   ```go
   // Before:
   var task lib.Task
   if err := json.Unmarshal(msg.Value, &task); err != nil {
   // After:
   var taskFile lib.TaskFile
   if err := json.Unmarshal(msg.Value, &taskFile); err != nil {
   ```

   b. Change all field accesses from typed `task.Field` to frontmatter accessor:

   - `task.TaskIdentifier` → `taskFile.TaskIdentifier` (same field name on TaskFile)
   - `task.Status` → `taskFile.Frontmatter.Status()` (returns `domain.TaskStatus`)
   - `task.Phase` → `taskFile.Frontmatter.Phase()` (returns `*domain.TaskPhase` — the nil check `task.Phase == nil` becomes `taskFile.Frontmatter.Phase() == nil`)
   - `task.Assignee` → `taskFile.Frontmatter.Assignee()` (returns `TaskAssignee`)

   c. The status check changes from:
   ```go
   if task.Status != "in_progress" {
   ```
   to:
   ```go
   if taskFile.Frontmatter.Status() != "in_progress" {
   ```

   d. The phase check changes from:
   ```go
   if task.Phase == nil || !allowedPhases.Contains(*task.Phase) {
   ```
   to:
   ```go
   phase := taskFile.Frontmatter.Phase()
   if phase == nil || !allowedPhases.Contains(*phase) {
   ```

   e. The assignee check:
   ```go
   if taskFile.Frontmatter.Assignee() == "" {
   ```

   f. The image lookup:
   ```go
   image, ok := h.assigneeImages[string(taskFile.Frontmatter.Assignee())]
   ```

   g. The duplicate check and spawn call:
   ```go
   if h.duplicateTracker.IsDuplicate(taskFile.TaskIdentifier) {
       ...
   }
   if err := h.jobSpawner.SpawnJob(ctx, taskFile, image); err != nil {
       ...
   }
   h.duplicateTracker.MarkProcessed(taskFile.TaskIdentifier)
   ```

   h. Update all log messages from `task.TaskIdentifier` to `taskFile.TaskIdentifier`, `task.Status` to `taskFile.Frontmatter.Status()`, etc.

   i. Remove the import for `"github.com/bborbe/vault-cli/pkg/domain"` if it is only used for the old typed fields. Keep it if `domain.TaskPhases` or `domain.TaskPhase*` constants are still referenced (they are — for `allowedPhases`).

2. **Modify `task/executor/pkg/spawner/job_spawner.go`** — Change `JobSpawner` interface and implementation:

   a. Change interface:
   ```go
   // Before:
   SpawnJob(ctx context.Context, task lib.Task, image string) error
   // After:
   SpawnJob(ctx context.Context, taskFile lib.TaskFile, image string) error
   ```

   b. Change implementation signature:
   ```go
   func (s *jobSpawner) SpawnJob(ctx context.Context, taskFile lib.TaskFile, image string) error {
   ```

   c. Update env var references:
   ```go
   {Name: "TASK_CONTENT", Value: taskFile.Content},
   {Name: "TASK_ID", Value: string(taskFile.TaskIdentifier)},
   ```

   d. Update job name and log messages:
   ```go
   jobName := jobNameFromTask(taskFile.TaskIdentifier)
   ```

3. **Update `task/executor/pkg/handler/task_event_handler_test.go`**:

   a. Change the `buildMsg` helper:
   ```go
   // Before:
   buildMsg := func(task lib.Task) *sarama.ConsumerMessage {
       value, err := json.Marshal(task)
   // After:
   buildMsg := func(taskFile lib.TaskFile) *sarama.ConsumerMessage {
       value, err := json.Marshal(taskFile)
   ```

   b. Convert every test's `lib.Task{...}` to `lib.TaskFile{...}` with frontmatter map. For example:

   **"skips task with status != in_progress":**
   ```go
   // Before:
   task := lib.Task{
       TaskIdentifier: "tid-1",
       Status:         "todo",
       Phase:          domain.TaskPhaseInProgress.Ptr(),
       Assignee:       "claude",
   }
   // After:
   taskFile := lib.TaskFile{
       TaskIdentifier: "tid-1",
       Frontmatter: lib.TaskFrontmatter{
           "status":   "todo",
           "phase":    string(domain.TaskPhaseInProgress),
           "assignee": "claude",
       },
   }
   ```

   **"skips task with nil phase":**
   ```go
   // Before:
   task := lib.Task{
       TaskIdentifier: "tid-2",
       Status:         "in_progress",
       Phase:          nil,
       Assignee:       "claude",
   }
   // After (omit "phase" key entirely to get nil from Phase() accessor):
   taskFile := lib.TaskFile{
       TaskIdentifier: "tid-2",
       Frontmatter: lib.TaskFrontmatter{
           "status":   "in_progress",
           "assignee": "claude",
       },
   }
   ```

   **"skips task with empty TaskIdentifier":**
   ```go
   taskFile := lib.TaskFile{
       Frontmatter: lib.TaskFrontmatter{
           "status":   "in_progress",
           "phase":    string(domain.TaskPhaseInProgress),
           "assignee": "claude",
       },
   }
   ```

   **"spawns job for qualifying task with known assignee":**
   ```go
   taskFile := lib.TaskFile{
       TaskIdentifier: lib.TaskIdentifier("tid-8"),
       Frontmatter: lib.TaskFrontmatter{
           "status":   "in_progress",
           "phase":    string(domain.TaskPhaseInProgress),
           "assignee": "claude",
       },
       Content: "do the work",
   }
   ```
   Update the spawner assertion:
   ```go
   _, spawnedTaskFile, image := fakeSpawner.SpawnJobArgsForCall(0)
   Expect(string(spawnedTaskFile.TaskIdentifier)).To(Equal("tid-8"))
   ```

   c. Apply the same pattern to ALL test cases — every `lib.Task` literal becomes `lib.TaskFile` with frontmatter map. There are approximately 13 test cases that need updating.

   d. For phase-related tests (`"skips task with phase todo"`, `"skips task with phase human_review"`, `"accepts task with phase planning"`, `"accepts task with phase ai_review"`), set phase as a string in frontmatter:
   ```go
   "phase": string(domain.TaskPhaseTodo),
   ```

4. **Update `task/executor/pkg/spawner/job_spawner_test.go`**:

   Convert all `lib.Task{...}` to `lib.TaskFile{...}`:

   a. "creates a job with correct name and env vars":
   ```go
   taskFile := lib.TaskFile{
       TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
       Frontmatter: lib.TaskFrontmatter{
           "assignee": "claude",
       },
       Content: "do the work",
   }
   err := jobSpawner.SpawnJob(ctx, taskFile, "my-image:latest")
   ```

   b. Apply same pattern to "truncates task ID", "handles short task ID", "returns nil when job already exists", "returns error on unexpected K8s error".

5. **Regenerate counterfeiter mocks**:

   Run `make generate` in `task/executor/` to regenerate:
   - `task/executor/mocks/job_spawner.go` — `SpawnJob` now takes `lib.TaskFile`

6. **Run `make test` and `make precommit` in `task/executor/`.**
</requirements>

<constraints>
- Follow CQRS entity pattern: `base.Object[base.Identifier]` + business key
- TaskIdentifier is UUID from frontmatter, NOT file path
- TaskFrontmatter stays as map[string]interface{} with typed accessors
- `TaskFrontmatter.Phase()` returns `*domain.TaskPhase` — check for nil before dereferencing
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` or `task/controller/` — they were updated in previous prompts
- Existing test behaviors must be preserved (all skip/spawn scenarios)
- `make precommit` in `task/executor/` must pass
</constraints>

<verification>
Run in `task/executor/`:

```bash
make generate
```
Must succeed (regenerates mocks).

```bash
make test
```
Must pass with exit code 0.

```bash
make precommit
```
Must pass with exit code 0.

Verify no references to `lib.Task{` (the old struct literal) remain:
```bash
grep -rn "lib\.Task{" task/executor/
```
Must produce no output (only `lib.TaskFile{` should exist).

Verify no references to `lib.TaskContent` remain:
```bash
grep -rn "TaskContent" task/executor/
```
Must produce no output.

Verify SpawnJob accepts TaskFile:
```bash
grep "SpawnJob" task/executor/pkg/spawner/job_spawner.go
```
Must show `lib.TaskFile` in the signature.
</verification>
