---
status: draft
created: "2026-04-04T00:00:00Z"
---

<summary>
- The in-memory DuplicateTracker is replaced by a K8s Job lookup before spawning
- Before creating a new Job, the executor lists active Jobs with label `component={taskIdentifier}` in the namespace
- If an active Job exists for this task, the event is skipped — no duplicate spawn
- If no active Job exists (never ran, completed, or failed), a new Job is spawned
- Retriggers work: editing a completed task back to `in_progress` spawns a new Job
- Executor restarts no longer lose dedup state — K8s is the source of truth
- The `DuplicateTracker` interface and `InMemoryDuplicateTracker` are deleted
</summary>

<objective>
Replace the in-memory `DuplicateTracker` with a K8s Job label lookup so that deduplication survives executor restarts and completed tasks can be retriggered by setting them back to `in_progress`.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read the coding plugin guides from `~/Documents/workspaces/coding/docs/`: `go-architecture-patterns.md`, `go-testing-guide.md`, `go-error-wrapping-guide.md`.

The current flow in `task/executor/pkg/handler/task_event_handler.go`:
1. Parse task event from Kafka
2. Filter: status=in_progress, phase∈{planning,in_progress,ai_review}, assignee set, known image
3. Check `DuplicateTracker.IsDuplicate(taskIdentifier)` — in-memory map, permanent block
4. Spawn K8s Job
5. `DuplicateTracker.MarkProcessed(taskIdentifier)` — marks forever

Problems:
- **Retrigger broken**: once processed, a taskIdentifier is blocked forever until pod restart
- **State lost on restart**: after executor pod restart, all dedup state is lost — replayed Kafka events respawn completed jobs
- **No awareness of job state**: doesn't know if a previous job completed, failed, or is still running

The fix: before spawning, list K8s Jobs in the namespace with label `component={taskIdentifier}`. Jobs already have this label (set by `jobBuilder.SetComponent` in `job_spawner.go`). If any Job is active (not completed/failed), skip. Otherwise spawn.

Key files to read before making changes:
- `task/executor/pkg/handler/task_event_handler.go` — current handler with DuplicateTracker
- `task/executor/pkg/handler/task_event_handler_test.go` — tests using FakeDuplicateTracker
- `task/executor/pkg/spawner/job_spawner.go` — SpawnJob, already sets `component` label
- `task/executor/pkg/spawner/job_spawner_test.go` — existing spawner tests
- `task/executor/pkg/factory/factory.go` — wiring, creates InMemoryDuplicateTracker
- `task/executor/mocks/duplicate_tracker.go` — counterfeiter mock to delete

Kafka events are consumed sequentially (single partition, single consumer), so there is no race between the list check and the create.
</context>

<requirements>
### 1. Add `IsJobActive` method to `JobSpawner` interface

In `task/executor/pkg/spawner/job_spawner.go`, extend the `JobSpawner` interface:

```go
// IsJobActive returns true if an active (not completed/failed) K8s Job exists
// for the given task identifier. Uses the `component` label set by SpawnJob.
IsJobActive(ctx context.Context, taskIdentifier lib.TaskIdentifier) (bool, error)
```

Implement on `jobSpawner`: list Jobs with label selector `component={taskIdentifier}`, check if any has `status.active > 0` or has no completion/failure condition yet.

A Job is considered active if:
- `job.Status.Active > 0`, OR
- `job.Status.Succeeded == 0 AND job.Status.Failed == 0` (just created, not yet scheduled)

A Job is NOT active if:
- `job.Status.Succeeded > 0` (completed), OR
- `job.Status.Failed > 0` AND `job.Status.Active == 0` (failed, no retry)

### 2. Replace DuplicateTracker with IsJobActive in TaskEventHandler

In `task/executor/pkg/handler/task_event_handler.go`:

- Remove `DuplicateTracker` interface, `InMemoryDuplicateTracker`, and `NewInMemoryDuplicateTracker`
- Remove `duplicateTracker` field from `taskEventHandler` struct
- Remove `duplicateTracker` parameter from `NewTaskEventHandler`
- Add `jobSpawner` is already available on the struct — use `h.jobSpawner.IsJobActive`

Replace the duplicate check block:
```go
// Before:
if h.duplicateTracker.IsDuplicate(task.TaskIdentifier) {
    glog.V(3).Infof("skip duplicate task %s", task.TaskIdentifier)
    return nil
}
```

With:
```go
active, err := h.jobSpawner.IsJobActive(ctx, task.TaskIdentifier)
if err != nil {
    return errors.Wrapf(ctx, err, "check active job for task %s", task.TaskIdentifier)
}
if active {
    glog.V(3).Infof("skip task %s: active job exists", task.TaskIdentifier)
    return nil
}
```

Remove the `MarkProcessed` call after `SpawnJob` — no longer needed.

### 3. Update factory wiring

In `task/executor/pkg/factory/factory.go`:
- Remove `NewInMemoryDuplicateTracker()` creation
- Remove `duplicateTracker` from `NewTaskEventHandler` call
- `NewTaskEventHandler` now takes only `jobSpawner` and `assigneeImages`

### 4. Delete duplicate tracker mock

Delete `task/executor/mocks/duplicate_tracker.go` — no longer needed.

### 5. Regenerate JobSpawner mock

The `JobSpawner` interface gained `IsJobActive`. Regenerate:
```bash
cd task/executor && go generate ./pkg/spawner/...
```

### 6. Update task_event_handler_test.go

- Remove all `FakeDuplicateTracker` references
- Remove `fakeTracker` variable and setup
- Update `NewTaskEventHandler` calls to remove tracker parameter
- Replace duplicate-skip test: instead of `fakeTracker.IsDuplicateReturns(true)`, configure `fakeSpawner.IsJobActiveReturns(true, nil)` and verify SpawnJob is NOT called
- Add test: `IsJobActive` returns false → SpawnJob IS called
- Add test: `IsJobActive` returns error → ConsumeMessage returns error
- Keep all existing filter tests (empty message, wrong status, wrong phase, unknown assignee) — they don't touch the tracker

### 7. Add IsJobActive tests to job_spawner_test.go

Using the existing `fake.Clientset`:
- **Test: no jobs exist** → returns false
- **Test: active job exists** (status.active > 0) → returns true
- **Test: completed job exists** (status.succeeded > 0) → returns false (allows retrigger)
- **Test: failed job exists** (status.failed > 0, active == 0) → returns false (allows retry)

### 8. Run tests and precommit

```bash
cd task/executor && make test
cd task/executor && make precommit
```
</requirements>

<constraints>
- Kafka events are consumed sequentially — no race between IsJobActive check and SpawnJob create
- Jobs already have `component` label set to `taskIdentifier` (via `jobBuilder.SetComponent`) — no label changes needed in SpawnJob
- The `component` label value must match exactly between SpawnJob and IsJobActive
- Do NOT add any new in-memory state — K8s is the single source of truth
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Use label selector `component={taskIdentifier}` for Job listing (not field selector)
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `make precommit` passes in `task/executor`
</constraints>

<verification>
Verify DuplicateTracker is fully removed:
```bash
grep -rn "DuplicateTracker\|duplicateTracker\|MarkProcessed\|IsDuplicate\|InMemoryDuplicate" task/executor/pkg/ task/executor/mocks/
```
Must produce no output (except possibly in test files referencing the fake spawner's new method).

Verify IsJobActive exists on interface and implementation:
```bash
grep -n "IsJobActive" task/executor/pkg/spawner/job_spawner.go task/executor/mocks/job_spawner.go task/executor/pkg/handler/task_event_handler.go
```
Must appear in all three files.

Verify duplicate tracker mock is deleted:
```bash
ls task/executor/mocks/duplicate_tracker.go 2>/dev/null
```
Must produce "No such file".

Run tests:
```bash
cd task/executor && make test
```
Must exit 0.

Run precommit:
```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
