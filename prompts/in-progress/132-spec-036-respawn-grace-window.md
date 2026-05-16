---
status: executing
spec: [036-bug-executor-respawns-before-terminal-write]
container: agent-132-spec-036-respawn-grace-window
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-16T21:30:00Z"
queued: "2026-05-16T21:27:47Z"
started: "2026-05-16T21:27:49Z"
branch: dark-factory/bug-executor-respawns-before-terminal-write
---

<summary>
- When a K8s Job has just finished and the agent's terminal-phase write is still propagating, the executor now suppresses respawn for a configurable grace period instead of immediately re-spawning
- A new `respawn_grace_window` metric label increments on every suppressed spawn so operators can distinguish "agent finished cleanly, terminal write in flight" from "agent crashed, retry needed"
- An info-level structured log line `event=respawn_grace_window task=<id> current_job=<job> elapsed=<seconds>` is emitted on every suppression — visible at default log verbosity without kubectl access
- After 300 seconds (the configurable grace period), a Job that has exited without writing a terminal phase is treated as a genuine crash and retried normally — preserving existing retry-on-crash behavior
- Legacy tasks that lack `job_started_at` in their frontmatter are treated as having exceeded the grace period and are retried without suppression — no behavior change for old tasks
- A new `JobStartedAt() (time.Time, error)` accessor is added to `lib.TaskFrontmatter`, parsing the existing `job_started_at` field written by `PublishSpawnNotification`
- The injected clock from `libtime.CurrentDateTimeGetter` makes the grace window deterministically testable
- The spec 035 terminal-phase gate in `parseAndFilter` is untouched; both gates coexist
- `make precommit` in `lib/` and `task/executor/` exit 0
</summary>

<objective>
Close the duplicate-spawn race window in `task/executor/pkg/handler/task_event_handler.go`: when `current_job` is set and the K8s Job is inactive, suppress respawn for 300 seconds from `job_started_at` to allow the agent's terminal-phase write to propagate, rather than immediately spawning a second pod. This eliminates the class of duplicate spawns observed in prod on 2026-05-16T20:25Z where pod 2 spawned 47 seconds after pod 1 succeeded, but the terminal-phase write arrived 5 minutes later.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages `package handler_test`, DescribeTable/Entry, coverage ≥80%
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — pre-initialisation in `init()`, label naming
- `go-time-injection.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `libtime.CurrentDateTimeGetter` injection, `SetNow()` in tests
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`, always `errors.Wrapf`
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface/struct patterns
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — entry format and `## Unreleased` rules
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — phase lifecycle, `current_job`/`job_started_at` frontmatter contract, result table

**Files to read in full before editing:**
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` type and all existing typed accessors (e.g. `CurrentJob()` at line 113); add `JobStartedAt()` here
- `lib/agent_task_test.go` — existing tests for frontmatter accessors including `CurrentJob` block (~line 220); add `JobStartedAt` tests alongside it
- `task/executor/pkg/handler/task_event_handler.go` — the `spawnIfNeeded` function (lines 292–376); the grace window check inserts inside the `currentJob != "" && !active` branch
- `task/executor/pkg/handler/task_event_handler_test.go` — existing Ginkgo tests; understand `buildMsg`, `BeforeEach`, local `NewTaskEventHandler` calls at lines ~532 and ~577 before editing
- `task/executor/pkg/metrics/metrics.go` — `TaskEventsTotal` counter-vec and `init()` pre-initialisation block
- `task/executor/pkg/factory/factory.go` — `CreateConsumer` function (lines ~87–93); add clock parameter passthrough here

**libtime pattern (grep-verified in codebase):**
- Production: `libtime.NewCurrentDateTime()` creates a `libtime.CurrentDateTime` (implements both `CurrentDateTimeGetter` and the setter interface)
- Tests: `currentDateTime.SetNow(libtimetest.ParseDateTime("2026-04-03T17:35:00Z"))` — see `task/executor/pkg/spawner/job_spawner_test.go` lines 49–50 for the exact pattern
- `libtime.DateTime.Time()` converts to `time.Time` — use `h.currentDateTime.Now().Time()` in the grace check
- Import: `libtime "github.com/bborbe/time"`, `libtimetest "github.com/bborbe/time/test"` (for test files only)

**`job_started_at` format (grep-verified at `task/executor/pkg/result_publisher.go:75`):**
```go
"job_started_at": p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
```
Use `time.RFC3339` in `JobStartedAt()` (`"2006-01-02T15:04:05Z07:00"` is exactly `time.RFC3339`).

**Existing `spawnIfNeeded` inactive-job branch (inline reference — exact shape):**
```go
if currentJob := task.Frontmatter.CurrentJob(); currentJob != "" {
    active, err := h.jobSpawner.IsJobActive(ctx, task.TaskIdentifier)
    if err != nil {
        metrics.TaskEventsTotal.WithLabelValues("error").Inc()
        return errors.Wrapf(ctx, err, "check current_job active for task %s", task.TaskIdentifier)
    }
    if active {
        glog.V(3).Infof("skip task %s: current_job %s still active (from frontmatter)", ...)
        metrics.TaskEventsTotal.WithLabelValues("skipped_active_job").Inc()
        return nil
    }
    // ← INSERT GRACE WINDOW CHECK HERE (requirement 4b)
    glog.V(2).Infof("task %s: current_job %s no longer active, proceeding to spawn", ...)
}
```

The grace window check replaces the bare fall-through comment. The existing `glog.V(2).Infof("proceeding to spawn")` line MUST remain (it fires when grace period has elapsed — legitimate crash retry path).

**glog version constraint:** `glog.SetOutput` is NOT available in `glog v1.2.x`. Log-line observation in tests uses spawn-counter + metric-counter proxy (same approach as spec 035). Do NOT attempt to capture log output in tests.
</context>

<requirements>

## 1. Add `JobStartedAt()` accessor to `lib/agent_task-frontmatter.go`

Read the full file before editing.

Add the following method after the existing `CurrentJob()` method (~line 113):

```go
// JobStartedAt parses the job_started_at frontmatter field written by PublishSpawnNotification.
// Returns (time.Time{}, nil) when the field is absent — callers treat zero time as "grace elapsed".
// Returns (time.Time{}, err) when the field is present but unparseable.
func (f TaskFrontmatter) JobStartedAt() (time.Time, error) {
	v, _ := f["job_started_at"].(string)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}
```

Add `"time"` to the import block (it is not currently imported in this file).

Verify:
```bash
grep -n "func.*JobStartedAt\|\"time\"" lib/agent_task-frontmatter.go
```
Expected: ≥2 matches — the function signature and the `"time"` import.

Build check:
```bash
cd lib && go build ./...
```
Expected: exit 0.

## 2. Add `JobStartedAt` tests to `lib/agent_task_test.go`

Read the full test file before editing. Locate the existing `Describe("CurrentJob", ...)` block (~line 220). Add a new `Describe("JobStartedAt", ...)` block immediately after the closing `})` of `Describe("CurrentJob", ...)`.

```go
Describe("JobStartedAt", func() {
    It("returns zero time and nil when field is absent", func() {
        fm := lib.TaskFrontmatter{}
        t, err := fm.JobStartedAt()
        Expect(err).To(BeNil())
        Expect(t.IsZero()).To(BeTrue())
    })

    It("returns parsed time when field holds a valid RFC3339 string", func() {
        fm := lib.TaskFrontmatter{"job_started_at": "2026-05-16T20:19:16Z"}
        t, err := fm.JobStartedAt()
        Expect(err).To(BeNil())
        Expect(t.UTC()).To(Equal(time.Date(2026, 5, 16, 20, 19, 16, 0, time.UTC)))
    })

    It("returns error when field holds an invalid string", func() {
        fm := lib.TaskFrontmatter{"job_started_at": "not-a-time"}
        _, err := fm.JobStartedAt()
        Expect(err).NotTo(BeNil())
    })
})
```

The test file already imports `"time"` — verify before adding. If it is absent, add it.

Run iterative tests:
```bash
cd lib && go test ./... -v 2>&1 | grep -E "PASS|FAIL|JobStartedAt"
```
Expected: all 3 new rows appear as PASS.

## 3. Run `lib/make precommit`

```bash
cd lib && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make errcheck`, etc.) and fix before retrying.

## 4. Add grace-window logic to `task/executor/pkg/handler/task_event_handler.go`

Read the full file before editing.

### 4a. Add package-level constant

Add immediately after the closing `}` of `defaultTriggerStatuses` (before the `terminalPhases` var block):

```go
// defaultRespawnGracePeriod is the window after job_started_at during which the executor
// suppresses respawn when the K8s Job is inactive but no terminal phase has been observed.
// The window gives the agent's terminal-phase write time to propagate through the vault pipeline.
const defaultRespawnGracePeriod = 300 * time.Second
```

Add `"time"` to the import block (check whether it is already present before adding).

Verify:
```bash
grep -n "defaultRespawnGracePeriod" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match with value `300 * time.Second`.

### 4b. Add `currentDateTime` field to handler struct and update constructor

In the `taskEventHandler` struct (around line 101), add:
```go
currentDateTime libtime.CurrentDateTimeGetter
```

Update `NewTaskEventHandler` to accept the clock as the last parameter:
```go
func NewTaskEventHandler(
    jobSpawner spawner.JobSpawner,
    branch base.Branch,
    resolver pkg.ConfigResolver,
    resultPublisher pkg.ResultPublisher,
    taskStore *pkg.TaskStore,
    currentDateTime libtime.CurrentDateTimeGetter,
) TaskEventHandler {
    return &taskEventHandler{
        jobSpawner:      jobSpawner,
        branch:          branch,
        resolver:        resolver,
        resultPublisher: resultPublisher,
        taskStore:       taskStore,
        currentDateTime: currentDateTime,
    }
}
```

Add the import `libtime "github.com/bborbe/time"` to the import block.

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: compile error referencing the factory call — fix in step 5 below.

### 4c. Insert grace-window check in `spawnIfNeeded`

Locate the inactive-job fall-through inside the `if currentJob := task.Frontmatter.CurrentJob(); currentJob != ""` block. The current shape is:

```go
        glog.V(2).Infof(
            "task %s: current_job %s no longer active, proceeding to spawn",
            task.TaskIdentifier, currentJob,
        )
```

Insert the following block BEFORE that `glog.V(2).Infof("proceeding to spawn")` line:

```go
        // Grace window: suppress respawn while the agent's terminal-phase write propagates.
        // The `current_job` cleared path is already handled by the outer `if currentJob != ""`
        // check at the start of this block — we only reach here when current_job is still set.
        // Treat missing or unparseable job_started_at as elapsed (preserves legacy-task behavior).
        jobStartedAt, err := task.Frontmatter.JobStartedAt()
        if err != nil {
            glog.Warningf(
                "task %s: failed to parse job_started_at: %v; treating grace period as elapsed",
                task.TaskIdentifier, err,
            )
        } else if !jobStartedAt.IsZero() {
            elapsed := h.currentDateTime.Now().Time().Sub(jobStartedAt)
            if elapsed < defaultRespawnGracePeriod {
                glog.Infof(
                    "event=respawn_grace_window task=%s current_job=%s elapsed=%.0fs",
                    task.TaskIdentifier, currentJob, elapsed.Seconds(),
                )
                metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window").Inc()
                return nil
            }
        }
```

The existing `glog.V(2).Infof("proceeding to spawn")` line immediately follows and is preserved.

Verify:
```bash
grep -n "respawn_grace_window\|defaultRespawnGracePeriod\|jobStartedAt" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥3 distinct lines in the non-test file.

## 5. Update `task/executor/pkg/factory/factory.go`

Read the file before editing. Pass `currentDateTimeGetter` (already in scope in `CreateConsumer`) as the new last argument to `handler.NewTaskEventHandler`:

```go
taskEventHandler := handler.NewTaskEventHandler(
    jobSpawner,
    branch,
    resolver,
    resultPublisher,
    taskStore,
    currentDateTimeGetter,
)
```

Build check (fixes the compile error from step 4b):
```bash
cd task/executor && go build ./...
```
Expected: exit 0.

## 6. Pre-initialise the new metric label in `task/executor/pkg/metrics/metrics.go`

Read the file before editing. Add to the `init()` function:
```go
    TaskEventsTotal.WithLabelValues("respawn_grace_window").Add(0)
```

Verify:
```bash
grep -n "respawn_grace_window" task/executor/pkg/metrics/metrics.go
```
Expected: exactly 1 match inside `init()`.

Build check:
```bash
cd task/executor && go build ./pkg/metrics/...
```
Expected: exit 0.

## 7. Add Ginkgo tests in `task/executor/pkg/handler/task_event_handler_test.go`

Read the full test file before editing.

### 7a. Update the import block and `BeforeEach`

Add to the import block:
```go
libtime "github.com/bborbe/time"
libtimetest "github.com/bborbe/time/test"
```

In the `var` declarations block (inside `Describe("TaskEventHandler", ...)`), add:
```go
currentDateTime libtime.CurrentDateTime
```

In the `BeforeEach`, add after `taskStore = pkg.NewTaskStore()`:
```go
currentDateTime = libtime.NewCurrentDateTime()
```

Replace the `NewTaskEventHandler` call in `BeforeEach` with the updated signature:
```go
h = handler.NewTaskEventHandler(
    fakeSpawner,
    base.Branch("prod"),
    fakeResolver,
    fakeResultPublisher,
    taskStore,
    currentDateTime,
)
```

Update the two local `handler.NewTaskEventHandler(...)` calls (for stage-based tests at ~lines 532 and ~577). These do not need a controllable clock — pass `libtime.NewCurrentDateTime()` as the last argument:
```go
localHandler := handler.NewTaskEventHandler(
    localSpawner,
    base.Branch("dev"),
    localResolver,
    &mocks.FakeResultPublisher{},
    pkg.NewTaskStore(),
    libtime.NewCurrentDateTime(),
)
```

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

### 7b. Add grace-window test block

Append a new `Describe("grace window (spec 036)", ...)` inside the existing `Describe("ConsumeMessage", func() { ... })`, after the last existing `It(...)` block and before its closing `})`.

The `job_started_at` value used in the test is set to the time pinned by `currentDateTime.SetNow(...)` minus an offset. Use a fixed anchor time of `"2026-05-16T20:19:16Z"` (the prod incident start time) for legibility.

```go
        Describe("grace window (spec 036)", func() {
            BeforeEach(func() {
                fakeSpawner.IsJobActiveReturns(false, nil)
                fakeSpawner.SpawnJobReturns("job-grace-1", nil)
            })

            DescribeTable("grace-window decision matrix",
                func(
                    currentJob string,
                    jobStartedAt string,
                    nowAt string,
                    expectSpawn int,
                    expectSuppress float64,
                ) {
                    currentDateTime.SetNow(libtimetest.ParseDateTime(nowAt))
                    fm := lib.TaskFrontmatter{
                        "status":   "in_progress",
                        "phase":    string(domain.TaskPhaseInProgress),
                        "assignee": "claude",
                    }
                    if currentJob != "" {
                        fm["current_job"] = currentJob
                    }
                    if jobStartedAt != "" {
                        fm["job_started_at"] = jobStartedAt
                    }
                    task := lib.Task{
                        TaskIdentifier: lib.TaskIdentifier("tid-grace-table"),
                        Frontmatter:    fm,
                    }
                    before := testutil.ToFloat64(
                        metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window"),
                    )
                    err := h.ConsumeMessage(ctx, buildMsg(task))
                    Expect(err).To(BeNil())
                    Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(expectSpawn))
                    after := testutil.ToFloat64(
                        metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window"),
                    )
                    Expect(after - before).To(Equal(expectSuppress))
                },
                Entry(
                    "current_job set, job inactive, within grace => suppress",
                    "pod-A", "2026-05-16T20:19:16Z", "2026-05-16T20:19:26Z", // T+10s
                    0, float64(1),
                ),
                Entry(
                    "current_job set, job inactive, past grace => spawn",
                    "pod-A", "2026-05-16T20:19:16Z", "2026-05-16T20:24:26Z", // T+310s
                    1, float64(0),
                ),
                Entry(
                    "current_job empty, job inactive => spawn (no grace check)",
                    "", "", "2026-05-16T20:19:26Z",
                    1, float64(0),
                ),
                Entry(
                    "current_job set, job inactive, job_started_at absent (legacy) => spawn",
                    "pod-legacy", "", "2026-05-16T20:19:26Z",
                    1, float64(0),
                ),
            )
        })
```

Run iterative tests:
```bash
cd task/executor && go test ./pkg/handler/... -v 2>&1 | grep -E "grace window|PASS|FAIL" | head -30
```
Expected: exit 0. Output includes at least 4 PASS lines for the grace-window rows.

Coverage check:
```bash
cd task/executor && go test -coverprofile=/tmp/handler-cover.out ./pkg/handler/... && \
  go tool cover -func=/tmp/handler-cover.out | grep -E "task_event_handler\.go|total"
```
Expected: aggregate `total:` ≥80%.

## 8. Update `docs/task-flow-and-failure-semantics.md`

Read the file before editing. Add a new subsection "Executor respawn gates" at the end of the file (or at a logical location after the existing "Spawn-trigger cap" section). The subsection must describe both the spec 035 terminal-phase gate and the spec 036 grace-period gate, so operators and future contributors understand the two-layer design.

Add:

```markdown
## Executor respawn gates

The executor applies two sequential gates before spawning a K8s Job for a task:

**Gate 1 — Terminal-phase gate (spec 035):** runs in `parseAndFilter`. Tasks whose `phase ∈ {human_review, done}` are suppressed before the trigger-phase allowlist. Emits `event=spawn_suppressed phase=<phase>` log and `spawn_suppressed_terminal_phase` metric. This gate fires when the agent's terminal-phase write has already arrived at the executor.

**Gate 2 — Grace-period gate (spec 036):** runs in `spawnIfNeeded`, inside the `current_job != "" && !active` branch. When the K8s Job has exited but no terminal phase has been observed yet, the executor suppresses respawn for `defaultRespawnGracePeriod` (300 seconds) from `job_started_at`. Emits `event=respawn_grace_window task=<id> current_job=<job> elapsed=<seconds>` log and `respawn_grace_window` metric. After the grace period, a genuinely crashed agent (no terminal write, no field cleared) is retried normally.

**Why two gates:** Gate 1 fires when the write has propagated; Gate 2 fires during the propagation window. Together they close the duplicate-spawn race: a clean-exit agent triggers Gate 2 during the window, then Gate 1 once the write lands. A crashed agent triggers neither gate (it never writes a terminal phase) and is retried after Gate 2's grace period expires.

**Diagnostic commands:**
```bash
# Suppressed by grace window (within 300s of spawn)
kubectl logs <executor-pod> | grep respawn_grace_window
# Suppressed by terminal gate (write already propagated)
kubectl logs <executor-pod> | grep spawn_suppressed_terminal_phase
```
```

Verify:
```bash
grep -n "Executor respawn gates\|respawn_grace_window" docs/task-flow-and-failure-semantics.md
```
Expected: ≥2 matches.

## 9. Add CHANGELOG entry

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add:
```markdown
- fix(task/executor): add grace-period gate in `spawnIfNeeded` — when `current_job` is set and the K8s Job is inactive, respawn is suppressed for 300s from `job_started_at` to allow the agent's terminal-phase write to propagate; emits `event=respawn_grace_window` log + metric; closes the duplicate-spawn race from 2026-05-16T20:25Z prod incident
- fix(lib): add `JobStartedAt() (time.Time, error)` accessor to `TaskFrontmatter` to parse the `job_started_at` frontmatter field written by `PublishSpawnNotification`
```

Verify:
```bash
grep -n "respawn_grace_window\|JobStartedAt" CHANGELOG.md | head -5
```
Expected: ≥2 matches.

## 10. Final precommit runs

```bash
cd task/executor && make test
```
Expected: exit 0.

```bash
cd task/executor && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make gosec`, `make errcheck`) and fix before retrying full precommit.

</requirements>

<constraints>
- **Files modified in `lib/`:** only `lib/agent_task-frontmatter.go` and `lib/agent_task_test.go`. No other lib files.
- **Files modified in `task/executor/`:** `pkg/handler/task_event_handler.go`, `pkg/handler/task_event_handler_test.go`, `pkg/factory/factory.go`, `pkg/metrics/metrics.go`. No other executor files.
- **`AgentConfiguration` is NOT modified** — no per-agent grace period override. The constant is package-level only. Evidence: `grep -rn 'RespawnGracePeriod' task/executor/ task/controller/ lib/` must return zero matches.
- **The spec 035 terminal-phase gate is NOT modified** — `terminalPhases`, `knownPhases`, `IsTerminal`, `applyPhaseGate`, and the gate call in `parseAndFilter` must remain exactly as they are. Do NOT touch them.
- **Grace check position:** inside the `if currentJob := task.Frontmatter.CurrentJob(); currentJob != ""` block, AFTER the `if active` early-return, BEFORE the `glog.V(2).Infof("proceeding to spawn")` line. The existing "proceeding to spawn" log line remains as the fall-through for the past-grace / crash-retry path.
- **`defaultRespawnGracePeriod` is 300 seconds exactly** — no other value.
- **Info-level log for suppression:** use `glog.Infof(...)` not `glog.V(N).Infof(...)`. The suppression signal must be visible at default verbosity.
- **Zero `job_started_at` → elapsed:** if `JobStartedAt()` returns zero time and nil error (field absent), the `!jobStartedAt.IsZero()` condition is false → no suppression → spawn proceeds. This preserves legacy-task behavior.
- **Error from `JobStartedAt()`:** log a warning and treat as elapsed (allow spawn). Do NOT return the error to the caller.
- **`currentDateTime` is the LAST parameter** to `NewTaskEventHandler` — do not change the order of existing parameters.
- **Do NOT use `glog.SetOutput`** in tests — not available in `glog v1.2.x`. Observe side effects via spawn-counter and metric-counter proxy only.
- **Metric delta pattern in tests:** always read `testutil.ToFloat64(...)` before the handler call and compare `after - before`. Do not use absolute values.
- **External test package:** keep `package handler_test` — do not change the package declaration.
- **No new Counterfeiter annotations or generated mocks** for this fix — `libtime.CurrentDateTime` is used directly (not faked), matching the pattern in `job_spawner_test.go`.
- **Do NOT commit.** dark-factory handles git.
- `cd lib && make precommit` must exit 0 (run this as part of step 3 before moving to step 4).
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

`JobStartedAt` accessor present in lib:
```bash
grep -rn "func.*JobStartedAt" task/ lib/
```
Expected: ≥1 line declaring the method in `lib/agent_task-frontmatter.go`.

`JobStartedAt` tests cover 3 cases:
```bash
grep -n "JobStartedAt\|absent\|invalid" lib/agent_task_test.go | head -10
```
Expected: ≥3 lines in the `JobStartedAt` Describe block.

Grace window constant:
```bash
grep -n "defaultRespawnGracePeriod" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match with value `300 * time.Second`.

No per-agent override:
```bash
grep -rn "RespawnGracePeriod" task/executor/ task/controller/ lib/
```
Expected: zero matches.

Grace window log line present in handler:
```bash
grep -n "event=respawn_grace_window" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match in non-test code.

Grace window metric pre-initialised:
```bash
grep -n "respawn_grace_window" task/executor/pkg/metrics/metrics.go
```
Expected: exactly 1 match in `init()`.

Clock field in handler struct:
```bash
grep -n "currentDateTime" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥2 matches (field declaration + constructor assignment + usage in spawnIfNeeded).

Factory updated:
```bash
grep -n "currentDateTimeGetter" task/executor/pkg/factory/factory.go
```
Expected: ≥1 match where it is passed to `handler.NewTaskEventHandler`.

Spec 035 gate untouched:
```bash
grep -n "terminalPhases\|IsTerminal\|applyPhaseGate" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥3 matches (the definitions and the gate call in `parseAndFilter`), none of them changed from the pre-existing state.

Three spec 036 AC test row names in output:
```bash
cd task/executor && go test ./pkg/handler/... -v -ginkgo.v 2>&1 | grep -E "within grace|past grace|no grace|legacy"
```
Expected: ≥3 PASS lines.

Documentation updated:
```bash
grep -n "Executor respawn gates\|respawn_grace_window" docs/task-flow-and-failure-semantics.md
```
Expected: ≥2 matches.

CHANGELOG updated:
```bash
grep -n "respawn_grace_window\|JobStartedAt" CHANGELOG.md | head -5
```
Expected: ≥2 matches under `## Unreleased`.

Full precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
