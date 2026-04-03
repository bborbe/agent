---
status: completed
summary: Changed K8s Job naming in task executor from agent-{taskID[:8]} to {assignee}-{YYYYMMDDHHMMSS}, injecting time via CurrentDateTimeGetter per project conventions
container: agent-028-job-name-assignee-timestamp
dark-factory-version: v0.89.1-dirty
created: "2026-04-03T18:00:00Z"
queued: "2026-04-03T17:53:57Z"
started: "2026-04-03T17:53:59Z"
completed: "2026-04-03T18:03:27Z"
---

<summary>
- K8s Job names change from static "agent-{taskID}" to "{assignee}-{YYYYMMDDHHMMSS}"
- Every retriggered task gets a unique job name, eliminating collisions
- Time is injected via CurrentDateTimeGetter, not called directly
- Empty assignee falls back to "agent" as default prefix
- AlreadyExists safety net remains unchanged
- Factory wiring passes the new time dependency through
</summary>

<objective>
Change K8s Job naming in the task executor from `agent-{first8charsOfTaskID}` to `{assignee}-{YYYYMMDDHHMMSS}` so that retriggered tasks no longer collide on job names. Inject time via `CurrentDateTimeGetter` per project conventions.
</objective>

<context>
Read CLAUDE.md for project conventions.

Read the coding plugin guide `go-time-injection.md` for the time injection pattern (CurrentDateTimeGetter, constructor injection, test mocking).

Key types from `github.com/bborbe/time`:
- `CurrentDateTimeGetter` is an interface with `.Now() DateTime` method
- `CurrentDateTime` implements both `CurrentDateTimeGetter` and `CurrentDateTimeSetter` -- use `libtime.NewCurrentDateTime()` to create
- `DateTime` is `type DateTime stdtime.Time` -- has `.Format()`, `.UTC()` methods directly. Convert to `time.Time` via `time.Time(dateTime)`
- In tests use `libtimetest "github.com/bborbe/time/test"` with `libtimetest.ParseDateTime("2026-04-03T17:35:00Z")`

Files to read before making changes:
- `task/executor/pkg/spawner/job_spawner.go` -- current job naming and spawner struct
- `task/executor/pkg/spawner/job_spawner_test.go` -- existing Ginkgo tests with fake K8s client
- `task/executor/pkg/factory/factory.go` -- where `NewJobSpawner` is called (factory must NOT create dependencies, only receive them)
- `task/executor/main.go` -- where `CreateConsumer` is called (new dependency created here)
- `lib/agent_task-frontmatter.go` -- `TaskFrontmatter.Assignee()` returns `TaskAssignee`
- `lib/agent_task-assignee.go` -- `TaskAssignee` type with `String()` method

Reference completed prompt for the same pattern (time injection into this codebase):
- `prompts/completed/025-fix-time-injection-and-test-race.md`
- `prompts/completed/027-pass-gemini-api-key-to-spawned-jobs.md` (adding constructor params through factory)

Project doc for job naming context:
- `docs/agent-job-interface.md` -- documents job naming conventions and executor role
</context>

<requirements>
1. **Add `currentDateTimeGetter` field to `jobSpawner` struct** in `task/executor/pkg/spawner/job_spawner.go`:
   - Add field: `currentDateTimeGetter libtime.CurrentDateTimeGetter`
   - Add import: `libtime "github.com/bborbe/time"`
   - Add `currentDateTimeGetter libtime.CurrentDateTimeGetter` as last parameter to `NewJobSpawner`
   - Store it in the struct literal

2. **Change `jobNameFromTask` signature and implementation** in `task/executor/pkg/spawner/job_spawner.go`:
   - Old signature: `func jobNameFromTask(taskID lib.TaskIdentifier) string`
   - New signature: `func jobNameFromTask(assignee string, now libtime.DateTime) string`
   - Implementation:
     ```go
     func jobNameFromTask(assignee string, now libtime.DateTime) string {
         if assignee == "" {
             assignee = "agent"
         }
         return assignee + "-" + now.UTC().Format("20060102150405")
     }
     ```
   - Update the godoc comment to reflect the new format: `{assignee}-{YYYYMMDDHHMMSS}`

3. **Update `SpawnJob` method** in `task/executor/pkg/spawner/job_spawner.go`:
   - Old call: `jobName := jobNameFromTask(task.TaskIdentifier)`
   - New call:
     ```go
     assignee := task.Frontmatter.Assignee().String()
     now := s.currentDateTimeGetter.Now()
     jobName := jobNameFromTask(assignee, now)
     ```
   - `CurrentDateTimeGetter.Now()` returns `libtime.DateTime` which is passed directly to `jobNameFromTask`

4. **Update factory and main.go wiring** (dependency flows: main.go → factory → spawner):

   **`task/executor/main.go`**:
   - Add import: `libtime "github.com/bborbe/time"`
   - Create `libtime.NewCurrentDateTime()` in main.go and pass to `factory.CreateConsumer`
   - Add `currentDateTimeGetter` as new parameter in the `CreateConsumer` call

   **`task/executor/pkg/factory/factory.go`**:
   - Add `currentDateTimeGetter libtime.CurrentDateTimeGetter` as last parameter to `CreateConsumer`
   - Pass it through to `spawner.NewJobSpawner`:
     ```go
     // Old:
     jobSpawner := spawner.NewJobSpawner(kubeClient, namespace, kafkaBrokers, string(branch), geminiAPIKey)
     // New:
     jobSpawner := spawner.NewJobSpawner(kubeClient, namespace, kafkaBrokers, string(branch), geminiAPIKey, currentDateTimeGetter)
     ```
   - Factory must NOT create the dependency itself (go-time-injection guide: "never create inside factory")

5. **Update tests** in `task/executor/pkg/spawner/job_spawner_test.go`:
   - Add imports:
     ```go
     libtime "github.com/bborbe/time"
     libtimetest "github.com/bborbe/time/test"
     ```
   - In `BeforeEach`, create a mock time provider and inject it:
     ```go
     currentDateTime := libtime.NewCurrentDateTime()
     currentDateTime.SetNow(libtimetest.ParseDateTime("2026-04-03T17:35:00Z"))
     ```
   - Update `NewJobSpawner` call to pass `currentDateTime` as last argument
   - Update all job name assertions to use the new format `{assignee}-20260403173500`. Read existing tests to find current assignee values in fixtures and update expected names accordingly
   - Add or repurpose one test for **empty assignee fallback**: set `Frontmatter: lib.TaskFrontmatter{}` (no assignee key) -- expect job name prefix `"agent-"`
   - Update AlreadyExists test: the pre-existing job's `Name` must match the expected generated name for that test's assignee and fixed time

6. **Regenerate counterfeiter mock** if the `JobSpawner` interface changed:
   - The interface (`SpawnJob(ctx, task, image) error`) does NOT change -- no regeneration needed
   - Only the constructor and internal implementation change

7. **K8s name validation**: Job names must be <= 63 chars and match `[a-z0-9]([-a-z0-9]*[a-z0-9])?`. The assignee values in production are short strings like `backtest-agent`, `claude`, `agent`. The timestamp is 14 chars. `backtest-agent-20260403173500` = 30 chars, well within limits. No runtime validation needed but document this constraint in the godoc of `jobNameFromTask`.
</requirements>

<constraints>
- Do NOT change the `JobSpawner` interface -- same `SpawnJob(ctx context.Context, task lib.Task, image string) error` signature
- Do NOT remove the `AlreadyExists` handling -- keep it as safety net
- Do NOT remove any existing env vars passed to spawned jobs
- Use `libtime "github.com/bborbe/time"` for time injection, never `time.Now()` in production code
- Use `libtimetest "github.com/bborbe/time/test"` only in test files
- Use `github.com/bborbe/errors` for error wrapping -- never `fmt.Errorf`
- Do NOT update CHANGELOG.md
- Do NOT commit -- dark-factory handles git
- Existing tests must still pass after changes
</constraints>

<verification>
Run tests:

```bash
cd task/executor && make test
```
Must pass with exit code 0.

Verify no `time.Now()` in production spawner code:

```bash
grep -n "time.Now()" task/executor/pkg/spawner/job_spawner.go
```
Must produce no output.

Verify new job name format in code:

```bash
grep -n "Format" task/executor/pkg/spawner/job_spawner.go
```
Must show `"20060102150405"` format string.

Verify assignee fallback:

```bash
grep -n '"agent"' task/executor/pkg/spawner/job_spawner.go
```
Must show the fallback assignment.

Run precommit:

```bash
cd task/executor && make precommit
```
Must pass with exit code 0.
</verification>
