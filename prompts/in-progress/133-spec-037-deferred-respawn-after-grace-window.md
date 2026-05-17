---
status: committing
spec: [037-bug-executor-no-retry-after-grace-window-expiry]
summary: 'Added deferred-respawn reconciliation loop (spec 037): grace-window suppressions now seed a deferredRespawns map; RunDeferredRespawnLoop polls every 30s and fires spawnIfNeeded once grace elapses; terminal-phase events clear deferred entries; startup seed recovers in-flight tasks from taskStore; respawn_after_grace_window metric and log line fire only on actual spawns; factory and main.go wired; 6 new tests covering all ACs; make precommit exits 0.'
container: agent-exec-133-spec-037-deferred-respawn-after-grace-window
dark-factory-version: v0.162.0
created: "2026-05-17T11:00:00Z"
queued: "2026-05-17T11:03:38Z"
started: "2026-05-17T11:03:40Z"
branch: dark-factory/bug-executor-no-retry-after-grace-window-expiry
---

<summary>
- After the spec 036 grace window suppresses a respawn, the executor now guarantees a follow-up evaluation of the same task once grace expires — without waiting for a fresh Kafka event to arrive
- A background reconciliation loop polls every 30 seconds (well under the spec's R ≤ 60 s bound) and re-checks every suppressed task whose grace period has elapsed
- The reconciliation loop is restart-safe: on executor startup, it scans tasks already known to be in-flight (current_job set, phase non-terminal) and seeds them as deferred candidates, so a restart during the grace window does not leave a task stuck
- When the agent eventually writes a terminal phase (done/human_review) for a suppressed task, the corresponding deferred entry is dropped so no stale spawn fires after the task is finished
- The follow-up evaluation re-uses the same spawn predicate as the event path: if a new pod was already spawned during grace, or the trigger cap is reached, or the phase has gone terminal, no duplicate spawn occurs
- A new metric label `respawn_after_grace_window` is incremented only when the follow-up evaluation actually spawns a pod (matching the spec's "results in a spawn" wording), distinct from the existing suppression label so operators can graph "stuck tasks rescued" separately
- An info-level log line `event=respawn_after_grace_window task=<id> current_job=<job> elapsed=<seconds>` is emitted for every follow-up evaluation that proceeds to spawn
- Spec 036's suppression behavior inside the grace window is preserved unchanged; all existing spec 036 tests pass
</summary>

<objective>
After the spec 036 grace window suppresses a respawn, the executor must autonomously re-evaluate the same task once the grace period has elapsed. Today, no re-evaluation is scheduled, leaving tasks permanently stuck when their only event during the grace window is suppressed. This spec adds a periodic reconciliation loop that fires deferred evaluations using the injected clock, eliminating the "stuck forever" failure mode observed on 2026-05-17 (task `cbe79223-...`, PR #128 never reviewed).
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, DescribeTable/Entry, coverage ≥80%
- `go-concurrency-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `sync.Mutex`, caller-owned channels, no raw goroutines; background loop pattern
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — pre-initialisation in `init()`, label naming
- `go-time-injection.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `libtime.CurrentDateTimeGetter`, `SetNow()` in tests
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`, always `errors.Wrapf`
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — entry format and `## Unreleased` rules
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — phase lifecycle and the "Executor respawn gates" subsection (added by spec 036)

**Files to read in full before editing:**
- `task/executor/pkg/handler/task_event_handler.go` — the full file: `checkActiveCurrentJob`, `spawnIfNeeded`, `parseAndFilter`, struct definition, interface definition, and the counterfeiter annotation at line 84
- `task/executor/pkg/handler/task_event_handler_test.go` — full file: understand `BeforeEach`, `buildMsg`, `currentDateTime` usage, existing grace-window Describe block (spec 036), DescribeTable patterns
- `task/executor/pkg/metrics/metrics.go` — `TaskEventsTotal` counter-vec and `init()` pre-initialisation block
- `task/executor/pkg/factory/factory.go` — `CreateConsumer` function (current return type `libkafka.Consumer`; will change)
- `task/executor/main.go` — `service.Run(ctx, ...)` wiring; add deferred loop as another runnable
- `task/executor/mocks/task_event_handler.go` — current generated mock (will be regenerated)

**Key shape of checkActiveCurrentJob (spec 036 state, grep-verified):**
```go
func (h *taskEventHandler) checkActiveCurrentJob(
    ctx context.Context,
    task lib.Task,
    currentJob string,
) (bool, error)
```
Called from `spawnIfNeeded` which already has `config *pkg.AgentConfiguration` in scope.

**Key shape of spawnIfNeeded call site (spec 036 state):**
```go
if currentJob := task.Frontmatter.CurrentJob(); currentJob != "" {
    suppress, err := h.checkActiveCurrentJob(ctx, task, currentJob)
    if err != nil { return err }
    if suppress { return nil }
}
```

**Key shape of parseAndFilter terminal-gate (spec 035 + 036 state):**
```go
if phase != nil && applyPhaseGate(task, *phase) {
    return lib.Task{}, nil, true, nil
}
```

**Clock pattern (grep-verified in codebase):**
- Comparison: `h.currentDateTime.Now().Time()` → `time.Time`
- Tests: `currentDateTime.SetNow(libtimetest.ParseDateTime("2026-05-16T20:19:16Z"))`
- Import: `libtime "github.com/bborbe/time"`, `libtimetest "github.com/bborbe/time/test"` (tests only)

**Counterfeiter annotation (line 84 of task_event_handler.go):**
```go
//counterfeiter:generate -o ../../mocks/task_event_handler.go --fake-name FakeTaskEventHandler . TaskEventHandler
```
Run `cd task/executor && make generate` to regenerate the mock after interface changes.

**service.Run pattern (grep-verified in main.go):**
```go
return service.Run(
    ctx,
    func(ctx context.Context) error { return connector.Listen(...) },
    func(ctx context.Context) error { return consumer.Consume(ctx) },
    func(ctx context.Context) error { return jobWatcher.Run(ctx) },
    a.createHTTPServer(...),
    healthcheckCron.Run,
)
```
Adding `func(ctx context.Context) error { return taskEventHandler.RunDeferredRespawnLoop(ctx) }` follows the same pattern.
</context>

<requirements>

## 1. Add deferredEntry type and deferred map to handler

Read `task/executor/pkg/handler/task_event_handler.go` in full before editing.

### 1a. Add package-level constant

After the existing `defaultRespawnGracePeriod` constant, add:

```go
// deferredRespawnInterval is the polling interval for the deferred-respawn reconciliation loop.
// Must be ≤ 60s to satisfy the R ≤ 60s bound from spec 037 (R = interval + per-tick
// comparison/spawn overhead; 30 s leaves headroom under the 60 s bound).
const deferredRespawnInterval = 30 * time.Second
```

### 1b. Add deferredEntry struct

After the `deferredRespawnInterval` constant, add:

```go
// deferredEntry tracks a task whose respawn was suppressed by the grace window.
// The executor re-evaluates it once retryAfter is reached.
type deferredEntry struct {
    task       lib.Task
    config     pkg.AgentConfiguration
    retryAfter time.Time
}
```

### 1c. Add fields to taskEventHandler struct

In the `taskEventHandler` struct, add two new fields after `currentDateTime`:
```go
    deferredMu       sync.Mutex
    deferredRespawns map[lib.TaskIdentifier]deferredEntry
```

Add `"sync"` to the import block (check if already present before adding).

### 1d. Initialize deferredRespawns in NewTaskEventHandler

In `NewTaskEventHandler`, add to the struct literal:
```go
    deferredRespawns: make(map[lib.TaskIdentifier]deferredEntry),
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

## 2. Update checkActiveCurrentJob to accept config and seed deferredRespawns on suppression

Read the full function before editing.

### 2a. Update signature

Change the signature to accept `config *pkg.AgentConfiguration` as the last parameter:
```go
func (h *taskEventHandler) checkActiveCurrentJob(
    ctx context.Context,
    task lib.Task,
    currentJob string,
    config *pkg.AgentConfiguration,
) (bool, error)
```

### 2b. In the grace-window suppression branch, add to deferredRespawns

Locate the `if elapsed < defaultRespawnGracePeriod` block that currently returns `(true, nil)`:
```go
            if elapsed < defaultRespawnGracePeriod {
                glog.Infof(
                    "event=respawn_grace_window task=%s current_job=%s elapsed=%.0fs",
                    task.TaskIdentifier, currentJob, elapsed.Seconds(),
                )
                metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window").Inc()
                return true, nil
            }
```

Replace with:
```go
            if elapsed < defaultRespawnGracePeriod {
                glog.Infof(
                    "event=respawn_grace_window task=%s current_job=%s elapsed=%.0fs",
                    task.TaskIdentifier, currentJob, elapsed.Seconds(),
                )
                metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window").Inc()
                retryAfter := jobStartedAt.Add(defaultRespawnGracePeriod)
                if config != nil {
                    h.deferredMu.Lock()
                    h.deferredRespawns[task.TaskIdentifier] = deferredEntry{
                        task:       task,
                        config:     *config,
                        retryAfter: retryAfter,
                    }
                    h.deferredMu.Unlock()
                }
                return true, nil
            }
```

### 2c. Update spawnIfNeeded to return (spawned bool, err error)

`evalDeferredRespawns` (step 4) needs to know whether the call actually resulted in a spawn so it can increment `respawn_after_grace_window` only on a real spawn (spec AC #6 wording: "results in a spawn"). Change `spawnIfNeeded`'s signature accordingly.

Change the signature from:
```go
func (h *taskEventHandler) spawnIfNeeded(
    ctx context.Context,
    task lib.Task,
    config *pkg.AgentConfiguration,
) error
```
to:
```go
// spawnIfNeeded returns (spawned, err): spawned is true iff a new k8s Job was actually launched
// (i.e. the call reached SpawnJob successfully). All early-return branches (suppression, trigger
// cap, active job, terminal phase, errors) return spawned=false.
func (h *taskEventHandler) spawnIfNeeded(
    ctx context.Context,
    task lib.Task,
    config *pkg.AgentConfiguration,
) (bool, error)
```

Inside `spawnIfNeeded`:
- Every existing `return nil` or `return err` becomes `return false, nil` / `return false, err`.
- Replace the final successful path (after `SpawnJob` succeeds and `PublishIncrementTriggerCount` succeeds) with `return true, nil`.
- The `checkActiveCurrentJob` call in `spawnIfNeeded` must pass `config`:
  ```go
      if currentJob := task.Frontmatter.CurrentJob(); currentJob != "" {
          suppress, err := h.checkActiveCurrentJob(ctx, task, currentJob, config)
          if err != nil {
              return false, err
          }
          if suppress {
              return false, nil
          }
      }
  ```

Update every other caller of `spawnIfNeeded` to discard the new bool unless interested. Find them:
```bash
grep -n "spawnIfNeeded(" task/executor/pkg/handler/task_event_handler.go
```
For each call site that ignores the spawn outcome, change `if err := h.spawnIfNeeded(...); err != nil` to:
```go
if _, err := h.spawnIfNeeded(ctx, task, config); err != nil {
    return err
}
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

Verify suppression still seeds map:
```bash
grep -n "deferredRespawns\[" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match inside the grace-window suppression branch.

## 3. Remove deferred entry on terminal-phase events

Read `parseAndFilter` before editing. When `applyPhaseGate` returns true (terminal phase), remove any pending deferred entry for that task so the deferred loop does not spawn after the task has finished.

Locate:
```go
    if phase != nil && applyPhaseGate(task, *phase) {
        return lib.Task{}, nil, true, nil
    }
```

Replace with:
```go
    if phase != nil && applyPhaseGate(task, *phase) {
        h.deferredMu.Lock()
        delete(h.deferredRespawns, task.TaskIdentifier)
        h.deferredMu.Unlock()
        return lib.Task{}, nil, true, nil
    }
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

## 4. Add evalDeferredRespawns (private eval method)

After the `checkActiveCurrentJob` function, add:

```go
// evalDeferredRespawns checks all pending deferred-respawn entries and spawns a job
// for each entry whose retryAfter has been reached. Entries are removed once processed.
// The respawn_after_grace_window metric and log line fire ONLY when the call actually
// results in a spawn (spec 037 AC #6: "recorded each time the follow-up evaluation
// results in a spawn"); evaluations that no-op (active job already, trigger cap hit,
// terminal phase) do not increment the metric.
func (h *taskEventHandler) evalDeferredRespawns(ctx context.Context) error {
    now := h.currentDateTime.Now().Time()

    h.deferredMu.Lock()
    var ready []deferredEntry
    for taskID, entry := range h.deferredRespawns {
        if !now.Before(entry.retryAfter) {
            ready = append(ready, entry)
            delete(h.deferredRespawns, taskID)
        }
    }
    h.deferredMu.Unlock()

    for _, entry := range ready {
        entry := entry // capture for closure
        spawned, err := h.spawnIfNeeded(ctx, entry.task, &entry.config)
        if err != nil {
            return errors.Wrapf(
                ctx, err, "deferred respawn for task %s", entry.task.TaskIdentifier,
            )
        }
        if !spawned {
            continue
        }
        jobStartedAt, _ := entry.task.Frontmatter.JobStartedAt()
        elapsed := now.Sub(jobStartedAt)
        glog.Infof(
            "event=respawn_after_grace_window task=%s current_job=%s elapsed=%.0fs",
            entry.task.TaskIdentifier, entry.task.Frontmatter.CurrentJob(), elapsed.Seconds(),
        )
        metrics.TaskEventsTotal.WithLabelValues("respawn_after_grace_window").Inc()
    }
    return nil
}
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

## 5. Update TaskEventHandler interface and add public methods

Read the interface definition before editing. The interface currently has only `ConsumeMessage`.

### 5a. Add two methods to the interface

Replace the existing interface block:
```go
type TaskEventHandler interface {
    ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}
```
with:
```go
// TaskEventHandler processes task event messages from Kafka and manages deferred respawns.
type TaskEventHandler interface {
    ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
    // EvalDeferredRespawns evaluates all pending deferred-respawn entries immediately.
    // Called by RunDeferredRespawnLoop on each tick; also callable directly in tests.
    EvalDeferredRespawns(ctx context.Context) error
    // RunDeferredRespawnLoop polls evalDeferredRespawns every deferredRespawnInterval
    // until ctx is cancelled. Must be run alongside the Kafka consumer.
    RunDeferredRespawnLoop(ctx context.Context) error
}
```

### 5b. Add the public EvalDeferredRespawns method

```go
// EvalDeferredRespawns implements TaskEventHandler.
func (h *taskEventHandler) EvalDeferredRespawns(ctx context.Context) error {
    return h.evalDeferredRespawns(ctx)
}
```

### 5c. Add seedDeferredRespawnsFromStore (restart-safety reconciliation)

The in-memory `deferredRespawns` map is wiped on executor restart. If a task was suppressed and the executor restarted before the deferred eval fired, no further Kafka event will arrive for that task (it is currently `phase=in_progress` with `current_job` set — exactly the bug spec 037 closes). To recover, the loop seeds itself from `h.taskStore` on startup: every task currently tracked there with `current_job != ""` and non-terminal phase is added to `deferredRespawns` with its existing `job_started_at + defaultRespawnGracePeriod` as `retryAfter`. If grace has already elapsed (the common restart case), the first tick fires immediately and the task gets spawned.

> NOTE on the store source: `task/executor/pkg/task_store.go` is the in-memory `TaskStore` populated by `spawnIfNeeded` on spawn (see `task_event_handler.go` line ~409: `h.taskStore.Store(...)`) and cleaned up on terminal events. On a fresh executor process its contents will be empty until the first task event arrives — but the seed STILL covers the in-process restart-style case represented in the unit test, where the test sets up taskStore explicitly before constructing the handler and invoking the loop's startup. The wider "process restart with fully empty store" case is bounded by the existing `job_watcher` informer re-list path (spec 037 §Constraints, option D), which is out of scope here.

Add after `checkActiveCurrentJob` (and after `evalDeferredRespawns`):
```go
// seedDeferredRespawnsFromStore scans the in-memory taskStore for tasks that look
// like in-flight work (current_job set, phase non-terminal) and adds them to
// deferredRespawns with retryAfter = job_started_at + defaultRespawnGracePeriod.
// Called once from RunDeferredRespawnLoop on startup. Idempotent: any entry already
// present in deferredRespawns is left untouched. This restores deferred state lost
// when the in-memory map is wiped by an executor restart, so a stuck task does not
// remain stuck for want of a Kafka event that will never arrive.
func (h *taskEventHandler) seedDeferredRespawnsFromStore() {
    snapshot := h.taskStore.Snapshot()

    h.deferredMu.Lock()
    defer h.deferredMu.Unlock()
    for taskID, task := range snapshot {
        if _, exists := h.deferredRespawns[taskID]; exists {
            continue
        }
        currentJob := task.Frontmatter.CurrentJob()
        if currentJob == "" {
            continue
        }
        phaseStr, _ := task.Frontmatter.Phase()
        if phaseStr == "done" || phaseStr == "human_review" {
            continue
        }
        jobStartedAt, ok := task.Frontmatter.JobStartedAt()
        if !ok {
            continue
        }
        h.deferredRespawns[taskID] = deferredEntry{
            task:       task,
            // config: the agent configuration is resolved at event time; the seed
            // uses an empty value here and relies on evalDeferredRespawns calling
            // spawnIfNeeded which re-resolves via the existing config resolver
            // (see step 5d note for the required adjustment if spawnIfNeeded
            // requires a non-nil config).
            retryAfter: jobStartedAt.Add(defaultRespawnGracePeriod),
        }
    }
}
```

This requires a `Snapshot` accessor on `TaskStore` (currently only `Store`/`Load`/`Delete`). Add it:
```go
// Snapshot returns a shallow copy of the current task map for read-only iteration.
// Safe to call concurrently with Store/Delete; the returned map is owned by the caller.
func (s *TaskStore) Snapshot() map[lib.TaskIdentifier]lib.Task {
    s.mu.RLock()
    defer s.mu.RUnlock()
    out := make(map[lib.TaskIdentifier]lib.Task, len(s.tasks))
    for k, v := range s.tasks {
        out[k] = v
    }
    return out
}
```

> NOTE on config in seeded entries: `evalDeferredRespawns` passes `&entry.config` to `spawnIfNeeded`, which forwards it into `checkActiveCurrentJob` (nil-guarded) and other branches. For seeded entries the config is the zero value of `pkg.AgentConfiguration`; this is acceptable because the seed-restart path is a recovery for the existing event-driven flow — the next genuine Kafka event for the task will replace the seeded entry with the correct config. The agent MAY instead resolve the config inline via the same resolver used by the event path; choose whichever is simpler given the current `spawnIfNeeded` body. Document the choice in the test fixture.

### 5d. Add the public RunDeferredRespawnLoop method

```go
// RunDeferredRespawnLoop implements TaskEventHandler.
func (h *taskEventHandler) RunDeferredRespawnLoop(ctx context.Context) error {
    // Startup reconciliation: recover deferred entries lost across an executor
    // restart by scanning the in-memory taskStore. See seedDeferredRespawnsFromStore
    // for the restart-safety rationale (spec 037 AC #5).
    h.seedDeferredRespawnsFromStore()

    // Fire one eval immediately after seeding so that tasks whose grace has
    // already elapsed at startup are picked up without waiting for the first tick.
    if err := h.evalDeferredRespawns(ctx); err != nil {
        return errors.Wrapf(ctx, err, "deferred respawn loop initial eval")
    }

    ticker := time.NewTicker(deferredRespawnInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            if err := h.evalDeferredRespawns(ctx); err != nil {
                return errors.Wrapf(ctx, err, "deferred respawn loop tick")
            }
        }
    }
}
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

Verify log line in non-test file:
```bash
grep -n "event=respawn_after_grace_window" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match via `glog.Infof` (not `glog.V(...).Infof`).

## 6. Regenerate the counterfeiter mock

The `TaskEventHandler` interface gained two methods. Regenerate:
```bash
cd task/executor && make generate
```
Expected: exit 0. The file `task/executor/mocks/task_event_handler.go` is updated to include `EvalDeferredRespawns` and `RunDeferredRespawnLoop` stubs.

Verify:
```bash
grep -n "EvalDeferredRespawns\|RunDeferredRespawnLoop" task/executor/mocks/task_event_handler.go
```
Expected: ≥2 matches.

Build check:
```bash
cd task/executor && go build ./...
```
Expected: exit 0 (mock compiles).

## 7. Pre-initialise the new metric label in metrics.go

Read `task/executor/pkg/metrics/metrics.go` before editing. Add to `init()`:
```go
    TaskEventsTotal.WithLabelValues("respawn_after_grace_window").Add(0)
```

Verify:
```bash
grep -n "respawn_after_grace_window" task/executor/pkg/metrics/metrics.go
```
Expected: exactly 1 match inside `init()`.

Verify distinctness from suppression label:
```bash
grep -nE "respawn_grace_window|respawn_after_grace_window" task/executor/pkg/metrics/metrics.go
```
Expected: ≥2 distinct lines.

## 8. Update factory.CreateConsumer to return the handler

Read `task/executor/pkg/factory/factory.go` in full before editing.

`CreateConsumer` currently creates `taskEventHandler` internally and returns only `libkafka.Consumer`. Change the return type to expose the handler so `main.go` can wire `RunDeferredRespawnLoop`.

Change the function signature from:
```go
func CreateConsumer(
    ...
) libkafka.Consumer {
```
to:
```go
func CreateConsumer(
    ...
) (libkafka.Consumer, handler.TaskEventHandler) {
```

Add `handler.TaskEventHandler` to the return statement:
```go
    return libkafka.NewOffsetConsumerHighwaterMarks(
        saramaClient,
        topic,
        offsetManager,
        taskEventHandler,
        run.NewTrigger(),
        logSamplerFactory,
    ), taskEventHandler
```

Build check:
```bash
cd task/executor && go build ./pkg/factory/...
```
Expected: compile error in main.go referencing the single-return `consumer := factory.CreateConsumer(...)` — fix in step 9.

## 9. Update main.go to wire RunDeferredRespawnLoop

Read `task/executor/main.go` in full before editing.

### 9a. Update the CreateConsumer call

Change:
```go
    consumer := factory.CreateConsumer(
        saramaClient,
        a.Branch,
        kubeClient,
        a.Namespace,
        a.KafkaBrokers,
        resolver,
        log.DefaultSamplerFactory,
        currentDateTimeGetter,
        resultPublisher,
        taskStore,
    )
```
to:
```go
    consumer, taskEventHandler := factory.CreateConsumer(
        saramaClient,
        a.Branch,
        kubeClient,
        a.Namespace,
        a.KafkaBrokers,
        resolver,
        log.DefaultSamplerFactory,
        currentDateTimeGetter,
        resultPublisher,
        taskStore,
    )
```

### 9b. Add RunDeferredRespawnLoop to service.Run

In the `service.Run(...)` call, add a new func after `consumer.Consume`:
```go
        func(ctx context.Context) error {
            return consumer.Consume(ctx)
        },
        func(ctx context.Context) error {
            return taskEventHandler.RunDeferredRespawnLoop(ctx)
        },
```

Add the `handler` import to `main.go` if not already present:
```go
"github.com/bborbe/agent/task/executor/pkg/handler"
```

Build check:
```bash
cd task/executor && go build ./...
```
Expected: exit 0.

Verify wiring:
```bash
grep -n "RunDeferredRespawnLoop" task/executor/main.go
```
Expected: ≥1 match.

## 10. Add Ginkgo tests in task_event_handler_test.go

Read the full test file before editing. Add a new top-level `Describe` block inside `Describe("TaskEventHandler", func() { ... })`, after the existing `Describe("ConsumeMessage", ...)` closing `})`.

### 10a. Anchor times for the new test block

Use `"2026-05-17T09:34:00Z"` as the clock anchor (the prod incident time). `job_started_at` = same time. Grace window expires at `"2026-05-17T09:39:00Z"` (T+300s). Grace + 30 s = `"2026-05-17T09:39:30Z"` (T+330s, within R=60s).

For T + gracePeriod − 1s: `"2026-05-17T09:38:59Z"`.
For T + gracePeriod + 60s: `"2026-05-17T09:40:00Z"`.

### 10b. Add the EvalDeferredRespawns test block

```go
    Describe("EvalDeferredRespawns (spec 037)", func() {
        const (
            anchorTime      = "2026-05-17T09:34:00Z" // T+0 (pod 1 start)
            insideGrace     = "2026-05-17T09:34:59Z" // T+59s (suppression event time)
            graceExpiredM1  = "2026-05-17T09:38:59Z" // T+299s (1s before grace expiry)
            graceExpiredR   = "2026-05-17T09:39:30Z" // T+330s (within R=60s)
            graceExpiredMax = "2026-05-17T09:40:00Z" // T+360s (= grace + 60s)
        )

        buildGraceTask := func(phase domain.TaskPhase, triggerCount, maxTriggers int) lib.Task {
            return lib.Task{
                TaskIdentifier: lib.TaskIdentifier("tid-deferred-037"),
                Frontmatter: lib.TaskFrontmatter{
                    "status":        "in_progress",
                    "phase":         string(phase),
                    "assignee":      "claude",
                    "stage":         "prod",
                    "current_job":   "pr-reviewer-agent-cbe79223-20260517093325",
                    "job_started_at": anchorTime,
                    "trigger_count": triggerCount,
                    "max_triggers":  maxTriggers,
                },
            }
        }

        BeforeEach(func() {
            fakeSpawner.IsJobActiveReturns(false, nil)
            fakeSpawner.SpawnJobReturns("job-deferred-1", nil)
        })

        It("deferred re-eval fires after grace expiry without a second Kafka event", func() {
            // Step 1: suppression event arrives inside grace window — no spawn
            currentDateTime.SetNow(libtimetest.ParseDateTime(insideGrace))
            task := buildGraceTask(domain.TaskPhaseInProgress, 0, 3)
            err := h.ConsumeMessage(ctx, buildMsg(task))
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))

            // Step 2: no further Kafka event; advance clock to T+330s (within R=60s)
            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredR))

            before := testutil.ToFloat64(
                metrics.TaskEventsTotal.WithLabelValues("respawn_after_grace_window"),
            )
            err = h.EvalDeferredRespawns(ctx)
            Expect(err).To(BeNil())

            // Deferred eval must have spawned once
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
            after := testutil.ToFloat64(
                metrics.TaskEventsTotal.WithLabelValues("respawn_after_grace_window"),
            )
            Expect(after - before).To(Equal(float64(1)))
        })

        It("deferred re-eval bound: no spawn before grace+R, spawn at grace+60s", func() {
            // Suppress inside grace window
            currentDateTime.SetNow(libtimetest.ParseDateTime(insideGrace))
            task := buildGraceTask(domain.TaskPhaseInProgress, 0, 3)
            err := h.ConsumeMessage(ctx, buildMsg(task))
            Expect(err).To(BeNil())

            // At grace-1s: evaluation fires but retryAfter not yet reached → no spawn
            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredM1))
            err = h.EvalDeferredRespawns(ctx)
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))

            // At grace+60s: retryAfter reached → spawn
            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredMax))
            err = h.EvalDeferredRespawns(ctx)
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
        })

        It("deferred re-eval is idempotent when an event-driven spawn occurs during grace", func() {
            // Step 1: suppress inside grace window → deferred entry created
            currentDateTime.SetNow(libtimetest.ParseDateTime(insideGrace))
            task := buildGraceTask(domain.TaskPhaseInProgress, 0, 3)
            err := h.ConsumeMessage(ctx, buildMsg(task))
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))

            // Step 2: a fresh event-driven spawn occurs (new pod is now active)
            fakeSpawner.IsJobActiveReturns(true, nil) // new pod active — simulates event-driven spawn

            // Step 3: advance clock past grace and eval — deferred check finds active job → no duplicate
            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredR))
            before := testutil.ToFloat64(
                metrics.TaskEventsTotal.WithLabelValues("respawn_after_grace_window"),
            )
            err = h.EvalDeferredRespawns(ctx)
            Expect(err).To(BeNil())
            // No duplicate spawn: active job suppresses it
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
            after := testutil.ToFloat64(
                metrics.TaskEventsTotal.WithLabelValues("respawn_after_grace_window"),
            )
            // Spec 037 AC #6: metric increments only when the eval results in a spawn.
            // Here the deferred eval no-ops (active job), so the delta MUST be 0.
            Expect(after - before).To(Equal(float64(0)))
        })

        It("startup seed: stuck task in taskStore is re-evaluated after restart (AC #5)", func() {
            // Simulate the post-restart state: a fresh handler with an empty
            // deferredRespawns map but a taskStore that already holds the stuck task
            // (the taskStore here is populated explicitly to represent state recovered
            // from outside the deferred map — e.g. a prior spawn's Store() call before
            // the in-memory map was wiped). For the wider out-of-process restart case
            // see spec 037 §Constraints option D (informer-driven recovery).
            stuck := buildGraceTask(domain.TaskPhaseInProgress, 0, 3)
            restartStore := pkg.NewTaskStore()
            restartStore.Store(stuck.TaskIdentifier, stuck)

            // Construct a fresh handler (mirroring main.go wiring) using the
            // pre-populated taskStore. NOTE: the exact constructor call must match
            // the existing NewTaskEventHandler signature in this test file's
            // BeforeEach. Reuse the same factory helper if one exists.
            freshHandler := handler.NewTaskEventHandler(
                fakeResolver,
                fakeSpawner,
                resultPublisher,
                restartStore,
                currentDateTime,
            )

            // Clock is past grace expiry — simulating the executor coming back up
            // long after the original suppression event.
            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredMax))

            before := fakeSpawner.SpawnJobCallCount()

            // Drive only the startup path: run the loop in a short-lived context
            // so the goroutine returns after the initial seed + immediate eval.
            shortCtx, cancel := context.WithCancel(ctx)
            done := make(chan error, 1)
            go func() { done <- freshHandler.RunDeferredRespawnLoop(shortCtx) }()
            // Allow the initial eval to run (cooperative; no time.Sleep beyond what
            // Ginkgo's Eventually permits). The "first eval" runs synchronously
            // before the ticker starts, so cancelling immediately is safe.
            Eventually(func() int {
                return fakeSpawner.SpawnJobCallCount()
            }).Should(BeNumerically(">=", before+1))
            cancel()
            Expect(<-done).To(BeNil())

            // Exactly one spawn from the seeded entry.
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(before + 1))
        })

        It("deferred re-eval respects trigger cap", func() {
            // task with trigger_count == max_triggers — will hit skipped_trigger_cap in spawnIfNeeded
            currentDateTime.SetNow(libtimetest.ParseDateTime(insideGrace))
            task := buildGraceTask(domain.TaskPhaseInProgress, 3, 3)
            err := h.ConsumeMessage(ctx, buildMsg(task))
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))

            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredR))
            beforeCap := testutil.ToFloat64(
                metrics.TaskEventsTotal.WithLabelValues("skipped_trigger_cap"),
            )
            err = h.EvalDeferredRespawns(ctx)
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
            afterCap := testutil.ToFloat64(
                metrics.TaskEventsTotal.WithLabelValues("skipped_trigger_cap"),
            )
            Expect(afterCap - beforeCap).To(Equal(float64(1)))
        })

        It("deferred re-eval entry is removed when a terminal-phase event arrives", func() {
            // Step 1: suppress inside grace → deferred entry created
            currentDateTime.SetNow(libtimetest.ParseDateTime(insideGrace))
            task := buildGraceTask(domain.TaskPhaseInProgress, 0, 3)
            err := h.ConsumeMessage(ctx, buildMsg(task))
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))

            // Step 2: terminal-phase event arrives (spec 035 gate fires + removes deferred entry)
            // Use a custom trigger that includes human_review in its Phases so WITHOUT the gate it would spawn.
            fakeResolver.ResolveReturns(
                pkg.AgentConfiguration{
                    Assignee: "claude",
                    Image:    "my-image:latest",
                    Trigger: &agentv1.Trigger{
                        Phases:   domain.TaskPhases{domain.TaskPhaseInProgress, domain.TaskPhaseHumanReview},
                        Statuses: domain.TaskStatuses{domain.TaskStatusInProgress},
                    },
                },
                nil,
            )
            terminalTask := lib.Task{
                TaskIdentifier: lib.TaskIdentifier("tid-deferred-037"),
                Frontmatter: lib.TaskFrontmatter{
                    "status":   "in_progress",
                    "phase":    string(domain.TaskPhaseHumanReview),
                    "assignee": "claude",
                    "stage":    "prod",
                },
            }
            err = h.ConsumeMessage(ctx, buildMsg(terminalTask))
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))

            // Step 3: advance clock past grace and eval — entry was removed by step 2 → no spawn
            currentDateTime.SetNow(libtimetest.ParseDateTime(graceExpiredR))
            err = h.EvalDeferredRespawns(ctx)
            Expect(err).To(BeNil())
            Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
        })
    })
```

Run iterative tests:
```bash
cd task/executor && go test ./pkg/handler/... -v -ginkgo.v 2>&1 | grep -E "deferred|respawn_after_grace|startup seed|PASS|FAIL" | head -40
```
Expected: exit 0. All 6 new It blocks appear as PASS (5 deferred-eval + 1 startup-seed for spec AC #5).

Coverage check:
```bash
cd task/executor && go test -coverprofile=/tmp/handler-cover.out ./pkg/handler/... && \
  go tool cover -func=/tmp/handler-cover.out | grep -E "task_event_handler\.go|total"
```
Expected: aggregate `total:` ≥80%.

## 11. Update docs/task-flow-and-failure-semantics.md

Read the file before editing (grep for "Executor respawn gates" to find the section location).

```bash
grep -n "Executor respawn gates" docs/task-flow-and-failure-semantics.md
```

Locate the existing "Executor respawn gates" section (added by spec 036). Insert the following subsection **immediately before the `## References` heading** (or, if no `## References` heading exists, at the very end of the file). This places the spec 037 addition after all existing Gate descriptions without depending on the exact position of any inline code block.

```markdown
**Why suppression alone is insufficient (spec 037):**
During mid-flow phase transitions (e.g. planning → in_progress), the task event triggered by the transition may arrive DURING the grace window and be suppressed. Because the agent only clears `current_job` on terminal writes, no further task events for this task are produced. The executor, being purely event-driven, would never re-check the task — leaving it permanently stuck at `phase=in_progress` with no second pod. Observed on 2026-05-17 (task `cbe79223-...`, PR #128 not reviewed for >2h).

**Gate 3 — Deferred re-evaluation (spec 037):** runs in `evalDeferredRespawns`, called by `RunDeferredRespawnLoop` every 30 seconds. When Gate 2 suppresses a respawn, the task is stored in a `deferredRespawns` map with `retryAfter = job_started_at + defaultRespawnGracePeriod`. After the grace period expires, `evalDeferredRespawns` runs the same `spawnIfNeeded` predicate against the stored task state. If the predicate permits spawn (job inactive, trigger cap not hit, phase non-terminal), a new pod is spawned. Emits `event=respawn_after_grace_window task=<id> current_job=<job> elapsed=<seconds>` log and `respawn_after_grace_window` metric.

**Deferred entry lifecycle:**
- Created: when Gate 2 suppresses (task stored with `retryAfter`)
- Cleared (early): when a terminal-phase Kafka event for the same task is processed (Gate 1 fires in `parseAndFilter`)
- Cleared (normal): when the deferred eval runs and calls `spawnIfNeeded`
- Bounded: R ≤ 60 s after grace expiry (poll interval 30 s + clock comparison overhead)
- Restart behaviour: on executor startup, the deferred-respawn loop scans the in-memory `TaskStore` for tasks with `current_job` set and a non-terminal phase, and seeds the deferred map with `retryAfter = job_started_at + defaultRespawnGracePeriod`. The first eval runs immediately, so tasks whose grace has already elapsed at startup are picked up without waiting for a Kafka event. The wider out-of-process restart case (empty `TaskStore`) is bounded by the existing `job_watcher` informer re-list path described in spec 037 (option D)

```bash
# Deferred respawn fired (stuck task rescued by the deferred retry)
kubectl logs <executor-pod> | grep respawn_after_grace_window
# Suppressed by grace window (spec 036 suppression, within 300s of spawn)
kubectl logs <executor-pod> | grep 'event=respawn_grace_window'
```
```

Verify:
```bash
grep -n "respawn_after_grace_window\|deferred re-evaluation\|Gate 3" docs/task-flow-and-failure-semantics.md
```
Expected: ≥3 distinct lines.

## 12. Add CHANGELOG entry

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add:
```markdown
- fix(task/executor): add deferred-respawn reconciliation loop — when `checkActiveCurrentJob` suppresses respawn inside the grace window, the task is queued for re-evaluation after `defaultRespawnGracePeriod`; `RunDeferredRespawnLoop` polls every 30s and calls `spawnIfNeeded` once grace elapses; emits `event=respawn_after_grace_window` log and `respawn_after_grace_window` metric; eliminates the "stuck forever" failure mode from 2026-05-17 (task `cbe79223`, PR #128 not reviewed for >2h)
- fix(task/executor): terminal-phase Kafka events now remove any pending deferred-respawn entry for the same task, preventing a stale spawn after the task has transitioned to `human_review` or `done`
```

Verify:
```bash
grep -n "respawn_after_grace_window\|deferred.respawn" CHANGELOG.md | head -5
```
Expected: ≥2 matches under `## Unreleased`.

## 13. Iterative test then final precommit

```bash
cd task/executor && make test
```
Expected: exit 0.

```bash
cd task/executor && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make gosec`, `make errcheck`) and fix before retrying.

</requirements>

<constraints>
- **Only modify files in `task/executor/`** (handler, factory, main, metrics, mocks, tests) plus `docs/task-flow-and-failure-semantics.md` and `CHANGELOG.md`. No changes to `lib/`, `task/controller/`, the agent, or any Kafka topic/schema.
- **`defaultRespawnGracePeriod` is NOT changed** (remains 300 s). Only `deferredRespawnInterval` (30 s) is new.
- **`respawn_after_grace_window` must be pre-initialised** in `metrics.go` `init()` alongside existing labels, consistent with spec 035 and 036 patterns.
- **Info-level log for deferred eval**: use `glog.Infof(...)` not `glog.V(N).Infof(...)`. The signal must be visible at default verbosity.
- **Clock injectable in new code paths**: `evalDeferredRespawns` uses `h.currentDateTime.Now().Time()` for the `now` comparison. `RunDeferredRespawnLoop` may use a real `time.NewTicker` for the sleep interval — only the comparison inside `evalDeferredRespawns` needs the injected clock. No bare `time.Now()` in new predicate code.
- **`EvalDeferredRespawns` in the interface** — must be exported so `package handler_test` can call it directly without needing to cast to the concrete type.
- **`deferredRespawns` map must be protected by `sync.Mutex`** — `evalDeferredRespawns` and the terminal-phase cleanup in `parseAndFilter` may execute concurrently with the Kafka consumer goroutine.
- **Spec 036 suppression behavior is unchanged** — the `respawn_grace_window` metric and `event=respawn_grace_window` log line must fire exactly as before. The suppression decision and existing metric/log emission for `checkActiveCurrentJob` must remain unchanged on the grace-window path; the signature may gain the `config *pkg.AgentConfiguration` parameter (step 2a) and the body may gain map-write side effects to populate `deferredRespawns` (step 2b), but no existing return value, branch order, log message, or metric label is altered.
- **`checkActiveCurrentJob` config parameter**: if `config` is nil (defensive), skip the deferred-map write. Do NOT dereference nil config.
- **Deferred eval calls the full `spawnIfNeeded`** — do not bypass the trigger cap check, IsJobActive check, or PublishIncrementTriggerCount. The deferred eval is a retry of the full spawn predicate, not a forced spawn.
- **No per-task goroutine** — use the shared reconciliation loop, not `go func()` per task.
- **Do NOT use `glog.SetOutput`** in tests — not available in `glog v1.2.x`. Observe side effects via spawn-counter and metric-counter proxy only.
- **Metric delta pattern in tests**: always read `testutil.ToFloat64(...)` before the handler call and compare `after - before`, not absolute values.
- **External test package**: keep `package handler_test` — do not change the package declaration.
- **Counterfeiter mock regeneration** is required after interface change. Use `make generate` in `task/executor/`.
- **Do NOT commit.** dark-factory handles git.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Deferred entry seeded on suppression:
```bash
grep -n "deferredRespawns\[" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match inside the grace-window suppression branch of `checkActiveCurrentJob`.

Terminal-phase cleanup present:
```bash
grep -n "delete(h.deferredRespawns" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match inside `parseAndFilter` (after `applyPhaseGate` returns true).

Deferred log line in non-test file:
```bash
grep -n "event=respawn_after_grace_window" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match via `glog.Infof` (not V).

New metric label pre-initialised:
```bash
grep -n "respawn_after_grace_window" task/executor/pkg/metrics/metrics.go
```
Expected: exactly 1 match inside `init()`.

Distinct from suppression label:
```bash
grep -nE "respawn_grace_window|respawn_after_grace_window" task/executor/pkg/metrics/metrics.go
```
Expected: ≥2 distinct lines.

Interface has both new methods:
```bash
grep -n "EvalDeferredRespawns\|RunDeferredRespawnLoop" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥4 matches (interface declaration ×2, implementation ×2).

Startup seed present (spec 037 AC #5):
```bash
grep -n "seedDeferredRespawnsFromStore" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥2 matches (the function definition and the call in `RunDeferredRespawnLoop`).

TaskStore snapshot accessor present:
```bash
grep -n "func (s \*TaskStore) Snapshot" task/executor/pkg/task_store.go
```
Expected: exactly 1 match.

Metric increment is conditional on spawn outcome (spec 037 AC #6):
```bash
grep -nB1 -A1 "respawn_after_grace_window\".Inc()" task/executor/pkg/handler/task_event_handler.go
```
Expected: the `.Inc()` line is inside an `if spawned {...}` block (or equivalent guard) — NOT unconditional after `spawnIfNeeded` returns.

Mock regenerated:
```bash
grep -n "EvalDeferredRespawns\|RunDeferredRespawnLoop" task/executor/mocks/task_event_handler.go
```
Expected: ≥2 matches.

Factory returns handler:
```bash
grep -n "handler.TaskEventHandler" task/executor/pkg/factory/factory.go
```
Expected: ≥1 match in the return type of `CreateConsumer`.

Deferred loop wired in main.go:
```bash
grep -n "RunDeferredRespawnLoop" task/executor/main.go
```
Expected: ≥1 match.

No bare time.Now() in new handler code:
```bash
grep -n "time\.Now()" task/executor/pkg/handler/task_event_handler.go
```
Expected: zero matches.

Spec 036 suppression unchanged:
```bash
grep -n "event=respawn_grace_window" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match (the existing spec 036 log line, unchanged).

6 new test cases present (5 deferred-eval + 1 startup-seed AC #5):
```bash
grep -n "deferred re-eval\|fires after grace\|bound:\|idempotent\|trigger cap\|entry is removed\|startup seed" task/executor/pkg/handler/task_event_handler_test.go
```
Expected: ≥6 matches.

Docs updated:
```bash
grep -n "respawn_after_grace_window\|Gate 3\|deferred re-evaluation" docs/task-flow-and-failure-semantics.md
```
Expected: ≥3 distinct lines.

CHANGELOG updated:
```bash
grep -n "respawn_after_grace_window\|deferred.respawn" CHANGELOG.md | head -5
```
Expected: ≥2 matches under `## Unreleased`.

Full precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
