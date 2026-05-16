---
status: completed
spec: [035-bug-task-executor-respawns-on-terminal-phase]
summary: Added explicit terminal-phase gate in parseAndFilter that suppresses tasks with phase ∈ {human_review, done} before the trigger-phase allowlist, with spawn_suppressed_terminal_phase metric, unknown_phase enum-drift detection, and 7 new Ginkgo tests covering all spec 035 regression rows.
container: agent-131-spec-035-terminal-phase-gate
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-16T15:10:00Z"
queued: "2026-05-16T16:16:54Z"
started: "2026-05-16T16:16:56Z"
completed: "2026-05-16T16:26:27Z"
branch: dark-factory/bug-task-executor-respawns-on-terminal-phase
---

<summary>
- Tasks whose phase is `human_review` or `done` are never spawned by the executor, even when a stale event arrives after the agent has already escalated to a terminal phase
- Every suppressed spawn emits a structured info-level log line `event=spawn_suppressed phase=<phase> task=<id>` — operators can diagnose "stuck task" from logs alone without needing kubectl to inspect the vault file
- A dedicated Prometheus counter label `spawn_suppressed_terminal_phase` lets dashboards and alert rules distinguish terminal-phase suppression from other skip reasons
- Events carrying a phase value not in the vault-cli v0.64.0 enum emit an `event=unknown_phase` log line and a distinct `unknown_phase` metric label, surfacing enum drift before it can cause duplicate spawns after a vault-cli upgrade
- The terminal-phase contract is encoded in a single named symbol (`IsTerminal` / `terminalPhases`) and documented with an invariant comment — removing it makes the regression tests fail
- The nil-phase (parse-error) path is unaffected: it continues to emit `skipped_phase`, not `spawn_suppressed_terminal_phase`
- All existing executor tests continue to pass; new tests are additive
- `make precommit` in `task/executor/` exits 0
</summary>

<objective>
Add an explicit terminal-phase gate in `task/executor/pkg/handler/task_event_handler.go` that rejects every task whose `phase ∈ {human_review, done}` before the existing trigger-phase allowlist runs. The gate emits a named info-level log line and a dedicated Prometheus counter label on every suppressed spawn. This closes the incident from 2026-05-16 where pod 2 spawned for task `22fda7e7` while the task's phase was already `human_review`, dismissed pod 1's correctly-posted GitHub review, and hid a hallucination signal from the human reviewer.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages `package handler_test`, DescribeTable/Entry, coverage ≥80%
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface-based metrics, pre-initialisation in `init()`, label naming
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`, always `errors.Wrapf`
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface/struct patterns
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — entry format and `## Unreleased` rules

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — phase lifecycle (`planning → in_progress → ai_review → human_review | done`), terminal-phase contract

**Files to read in full before editing:**

- `task/executor/pkg/handler/task_event_handler.go` — the `parseAndFilter` function; the gate inserts between the status-check block (ends ~line 141) and the phase-allowlist block that begins `phase := task.Frontmatter.Phase()` (~line 143)
- `task/executor/pkg/handler/task_event_handler_test.go` — existing Ginkgo tests in `package handler_test`; understand `buildMsg`, `fakeSpawner`, `fakeResolver` before adding tests
- `task/executor/pkg/metrics/metrics.go` — `TaskEventsTotal` counter-vec and `init()` pre-initialisation block; new labels go here

**vault-cli v0.64.0 phase constants (grep-verified):**
```
domain.TaskPhaseTodo        = "todo"
domain.TaskPhasePlanning    = "planning"
domain.TaskPhaseInProgress  = "in_progress"
domain.TaskPhaseAIReview    = "ai_review"
domain.TaskPhaseHumanReview = "human_review"
domain.TaskPhaseDone        = "done"
```
These are the ONLY 6 constants in `github.com/bborbe/vault-cli@v0.64.0/pkg/domain/task_phase.go`. Verify before writing the helper:
```bash
grep -n "TaskPhase[A-Z]" $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@v0.64.0/pkg/domain/task_phase.go
```
Expected: exactly the 6 constants above, no `TaskPhaseAborted` or any other constant. STOP if the count differs — spec assumption broken.

**Current `parseAndFilter` phase-check block (inline reference — exact shape at time of spec):**
```go
phase := task.Frontmatter.Phase()
if phase == nil || !effectiveTriggerPhases(config).Contains(*phase) {
    glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, phase)
    metrics.TaskEventsTotal.WithLabelValues("skipped_phase").Inc()
    return lib.Task{}, nil, true, nil
}
```
The gate inserts IMMEDIATELY BEFORE `if phase == nil || ...`. The `phase := task.Frontmatter.Phase()` declaration moves to just before the gate block. See requirement 2 for the exact replacement.

**Prometheus testutil import — available in go.mod:**
```bash
grep "prometheus/client_golang" task/executor/go.mod
```
Expected: `github.com/prometheus/client_golang v1.23.2` (or later). The `testutil` package at `github.com/prometheus/client_golang/prometheus/testutil` is part of this module and is available for test-file import.

**glog version constraint (from spec):**
`glog.SetOutput` is NOT available in the pinned `glog v1.2.x`. Log-line observation in tests uses path (b): spawn-counter + metric-counter proxy. Do NOT attempt to capture log output in tests.
</context>

<requirements>

## 1. Verify vault-cli phase constants

```bash
grep -n "TaskPhase[A-Z]" $(go env GOPATH)/pkg/mod/github.com/bborbe/vault-cli@v0.64.0/pkg/domain/task_phase.go
```
Expected: exactly 6 lines — `TaskPhaseTodo`, `TaskPhasePlanning`, `TaskPhaseInProgress`, `TaskPhaseAIReview`, `TaskPhaseHumanReview`, `TaskPhaseDone`. STOP and report if a 7th constant (e.g. `TaskPhaseAborted`) is present — the `knownPhases` set below must be updated first.

## 2. Add terminal-phase helper and gate in `task/executor/pkg/handler/task_event_handler.go`

Read the full file before editing.

### 2a. Add package-level vars and helper after the existing `defaultTriggerStatuses` block

Insert after the closing `}` of `defaultTriggerStatuses` and before the `//counterfeiter:generate` line:

```go
// terminalPhases is the set of phases that must never trigger a new spawn.
// Extending this set requires a follow-up spec if vault-cli adds new terminal phases.
var terminalPhases = map[domain.TaskPhase]struct{}{
	domain.TaskPhaseHumanReview: {},
	domain.TaskPhaseDone:        {},
}

// knownPhases contains all phase constants exported by vault-cli v0.64.0.
// Values outside this set trigger enum-drift logging (event=unknown_phase).
var knownPhases = map[domain.TaskPhase]struct{}{
	domain.TaskPhaseTodo:        {},
	domain.TaskPhasePlanning:    {},
	domain.TaskPhaseInProgress:  {},
	domain.TaskPhaseAIReview:    {},
	domain.TaskPhaseHumanReview: {},
	domain.TaskPhaseDone:        {},
}

// IsTerminal reports whether the given phase is in the terminal set.
// Tasks at a terminal phase must not be re-spawned; operator intervention is required.
func IsTerminal(phase domain.TaskPhase) bool {
	_, ok := terminalPhases[phase]
	return ok
}
```

Verify the symbol is exported:
```bash
grep -n "IsTerminal\|terminalPhases\|knownPhases" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥3 matches in the non-test file.

### 2b. Insert the terminal-phase gate in `parseAndFilter`

Locate the existing phase-check block:
```go
	phase := task.Frontmatter.Phase()
	if phase == nil || !effectiveTriggerPhases(config).Contains(*phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, phase)
		metrics.TaskEventsTotal.WithLabelValues("skipped_phase").Inc()
		return lib.Task{}, nil, true, nil
	}
```

Replace it with:
```go
	phase := task.Frontmatter.Phase()

	// terminal phases must not be spawned again — operator escalation required
	if phase != nil && IsTerminal(*phase) {
		glog.Infof(
			"event=spawn_suppressed phase=%s task=%s",
			*phase, task.TaskIdentifier,
		)
		metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase").Inc()
		return lib.Task{}, nil, true, nil
	}

	// Detect enum drift: phase present, not terminal, not a known vault-cli constant.
	// Falls through to the allowlist check which will also reject it via skipped_phase.
	if phase != nil && !knownPhases[*phase] {
		glog.Infof(
			"event=unknown_phase phase=%s task=%s",
			*phase, task.TaskIdentifier,
		)
		metrics.TaskEventsTotal.WithLabelValues("unknown_phase").Inc()
	}

	if phase == nil || !effectiveTriggerPhases(config).Contains(*phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, phase)
		metrics.TaskEventsTotal.WithLabelValues("skipped_phase").Inc()
		return lib.Task{}, nil, true, nil
	}
```

Verify the invariant comment is present:
```bash
grep -n "terminal phases must not be spawned" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match.

Verify the gate is positioned before the allowlist check:
```bash
grep -n "spawn_suppressed_terminal_phase\|effectiveTriggerPhases" task/executor/pkg/handler/task_event_handler.go
```
Expected: the `spawn_suppressed_terminal_phase` line appears at a LOWER line number than the `effectiveTriggerPhases(config).Contains` line.

Build check:
```bash
cd task/executor && go build ./pkg/handler/...
```
Expected: exit 0.

## 3. Pre-initialise new metric labels in `task/executor/pkg/metrics/metrics.go`

Read the full file. Add two new pre-initialisation calls to the `init()` function alongside the existing ones:

```go
	TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase").Add(0)
	TaskEventsTotal.WithLabelValues("unknown_phase").Add(0)
```

Verify:
```bash
grep -n "spawn_suppressed_terminal_phase\|unknown_phase" task/executor/pkg/metrics/metrics.go
```
Expected: exactly 2 matches.

Build check:
```bash
cd task/executor && go build ./pkg/metrics/...
```
Expected: exit 0.

## 4. Write Ginkgo tests in `task/executor/pkg/handler/task_event_handler_test.go`

Read the full file before editing. Do NOT delete or rename any existing test. Append a new `Describe("terminal phase gate", ...)` block inside the existing top-level `Describe("TaskEventHandler", ...)`, after the last existing `It(...)` block and before the closing `})` of `Describe("ConsumeMessage", ...)`.

### 4a. Add two new imports to the test file

Add to the import block:
```go
"github.com/prometheus/client_golang/prometheus/testutil"

"github.com/bborbe/agent/task/executor/pkg/metrics"
```

### 4b. Add the terminal phase gate test block

Append inside `Describe("ConsumeMessage", func() { ... })` (before its closing `})`):

```go
		Describe("terminal phase gate", func() {
			// DescribeTable covers the 5 regression rows from spec 035.
			// Rows 2 and 3 (terminal phases) use a custom trigger that includes
			// human_review/done in its Phases — so WITHOUT the gate the second
			// event would spawn (count > 0). The gate MUST fire to keep count=0.
			DescribeTable("phase/status combinations",
				func(
					status string,
					phase domain.TaskPhase,
					customTriggerPhases domain.TaskPhases,
					expectSpawn int,
					expectSuppress float64,
				) {
					if len(customTriggerPhases) > 0 {
						fakeResolver.ResolveReturns(
							pkg.AgentConfiguration{
								Assignee: "claude",
								Image:    "my-image:latest",
								Trigger: &agentv1.Trigger{
									Phases:   customTriggerPhases,
									Statuses: domain.TaskStatuses{domain.TaskStatusInProgress},
								},
							},
							nil,
						)
					}
					fakeSpawner.IsJobActiveReturns(false, nil)
					fakeSpawner.SpawnJobReturns("job-1", nil)

					before := testutil.ToFloat64(
						metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase"),
					)
					task := lib.Task{
						TaskIdentifier: lib.TaskIdentifier("tid-gate-table"),
						Frontmatter: lib.TaskFrontmatter{
							"status":   status,
							"phase":    string(phase),
							"assignee": "claude",
						},
					}
					err := h.ConsumeMessage(ctx, buildMsg(task))
					Expect(err).To(BeNil())
					Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(expectSpawn))
					after := testutil.ToFloat64(
						metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase"),
					)
					Expect(after - before).To(Equal(expectSuppress))
				},
				Entry(
					"status=in_progress phase=in_progress => spawn",
					"in_progress", domain.TaskPhaseInProgress, domain.TaskPhases(nil), 1, float64(0),
				),
				Entry(
					"status=in_progress phase=human_review => no spawn",
					// Custom trigger includes human_review — without the gate this would spawn.
					"in_progress", domain.TaskPhaseHumanReview,
					domain.TaskPhases{domain.TaskPhaseInProgress, domain.TaskPhaseHumanReview},
					0, float64(1),
				),
				Entry(
					"status=in_progress phase=done => no spawn",
					// Custom trigger includes done — without the gate this would spawn.
					"in_progress", domain.TaskPhaseDone,
					domain.TaskPhases{domain.TaskPhaseInProgress, domain.TaskPhaseDone},
					0, float64(1),
				),
				Entry(
					"status=completed phase=in_progress => no spawn",
					// Filtered by status check, not terminal gate.
					"completed", domain.TaskPhaseInProgress, domain.TaskPhases(nil), 0, float64(0),
				),
			)

			It("sequential events in_progress->human_review => exactly 1 spawn total", func() {
				// Custom trigger includes human_review in its Phases.
				// Without the terminal gate, the second event (phase=human_review)
				// would also spawn because human_review IS in the trigger → total count=2.
				// The gate MUST fire on the second event to keep count=1.
				// If IsTerminal() is removed, this test fails on the Equal(1) assertion.
				fakeResolver.ResolveReturns(
					pkg.AgentConfiguration{
						Assignee: "claude",
						Image:    "my-image:latest",
						Trigger: &agentv1.Trigger{
							Phases: domain.TaskPhases{
								domain.TaskPhaseInProgress,
								domain.TaskPhaseHumanReview,
							},
							Statuses: domain.TaskStatuses{domain.TaskStatusInProgress},
						},
					},
					nil,
				)
				fakeSpawner.IsJobActiveReturns(false, nil)
				fakeSpawner.SpawnJobReturns("job-seq-1", nil)

				// Event 1: phase=in_progress → spawns (legitimate spawn)
				event1 := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("22fda7e7"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    string(domain.TaskPhaseInProgress),
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(event1))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))

				// Event 2: phase=human_review (terminal) → gate suppresses.
				// The metric delta proves the gate fired, not the allowlist.
				before := testutil.ToFloat64(
					metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase"),
				)
				event2 := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("22fda7e7"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    string(domain.TaskPhaseHumanReview),
						"assignee": "claude",
					},
				}
				err = h.ConsumeMessage(ctx, buildMsg(event2))
				Expect(err).To(BeNil())
				// Total spawn count must remain 1 — the terminal gate prevented the second spawn.
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
				after := testutil.ToFloat64(
					metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase"),
				)
				Expect(after - before).To(Equal(float64(1)))
			})

			It("emits unknown_phase metric+log on enum drift (phase outside vault-cli v0.64.0 set)", func() {
				// Guards Desired Behavior #8 from spec 035: a phase value not in the
				// knownPhases map increments the unknown_phase metric and falls through
				// to the allowlist's skipped_phase path (no spawn).
				before := testutil.ToFloat64(
					metrics.TaskEventsTotal.WithLabelValues("unknown_phase"),
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-unknown-phase-035"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    "future_enum_value_not_in_v0.64.0",
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
				after := testutil.ToFloat64(
					metrics.TaskEventsTotal.WithLabelValues("unknown_phase"),
				)
				Expect(after - before).To(Equal(float64(1)))
			})

			It("does not emit spawn_suppressed on nil phase (parse-error / missing phase path)", func() {
				// Guards Failure Modes row 4 from spec 035: a task with missing/unparseable
				// phase must NOT emit spawn_suppressed_terminal_phase — it takes the
				// existing skipped_phase path.
				before := testutil.ToFloat64(
					metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase"),
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-nil-phase-035"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"assignee": "claude",
						// phase intentionally absent → Phase() returns nil
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
				after := testutil.ToFloat64(
					metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase"),
				)
				Expect(after - before).To(Equal(float64(0)))
			})
		})
```

Run iterative tests:
```bash
cd task/executor && go test ./pkg/handler/... -v 2>&1 | tail -40
```
Expected: exit 0. Output includes all 5 DescribeTable entries and the 2 additional `It` blocks as PASS lines. Fix any compile or assertion errors before continuing.

Coverage check:
```bash
cd task/executor && go test -coverprofile=/tmp/handler-cover.out ./pkg/handler/... && \
  go tool cover -func=/tmp/handler-cover.out | grep -E "task_event_handler\.go|total"
```
Expected: aggregate `total:` line ≥80%. Per-function thresholds are NOT a gate — do not add tests purely to lift unrelated function coverage; focus on the gate and its negative paths only.

## 5. Add CHANGELOG entry

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add the following bullet:
```markdown
- fix(task/executor): add explicit terminal-phase gate in `parseAndFilter` — tasks with `phase ∈ {human_review, done}` are suppressed before the trigger-phase allowlist, emitting `event=spawn_suppressed` log and `spawn_suppressed_terminal_phase` metric; unknown phases emit `event=unknown_phase`; closes the 2026-05-16 incident where pod 2 dismissed pod 1's GitHub review on task 22fda7e7
```

Verify:
```bash
grep -n "spawn_suppressed\|terminal.phase.gate\|22fda7e7" CHANGELOG.md | head -5
```
Expected: at least 1 match.

## 6. Run `make test` iteratively then `make precommit` once

```bash
cd task/executor && make test
```
Expected: exit 0. Fix any test failures before proceeding.

```bash
cd task/executor && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make gosec`, `make errcheck`) and fix before retrying full precommit.

</requirements>

<constraints>
- **Only edit these files:** `task/executor/pkg/handler/task_event_handler.go`, `task/executor/pkg/metrics/metrics.go`, `task/executor/pkg/handler/task_event_handler_test.go`, `CHANGELOG.md`. No other production file changes.
- **`IsTerminal` must be exported** (capital I) — the spec's AC grep uses `IsTerminal|terminalPhases` on non-test files.
- **Terminal set is exactly `{human_review, done}`** as of vault-cli v0.64.0. Verify the constants before writing the map. Do NOT invent `TaskPhaseAborted` or any constant not present in the pinned module.
- **Known phases set must list all 6 constants** from vault-cli v0.64.0 — no more, no fewer.
- **Gate position:** terminal-phase check runs AFTER `phase := task.Frontmatter.Phase()` and BEFORE `effectiveTriggerPhases(config).Contains(*phase)`. Existing checks (status, config resolution, type filter) remain in their current order.
- **glog V(0) for gate log lines:** use `glog.Infof(...)` (not `glog.V(3).Infof(...)`). The suppression signal must be visible at the default log level.
- **Do NOT use `glog.SetOutput`** in tests — not available in glog v1.2.x. Log assertions are out of scope; test only via spawn-counter and metric-counter proxy (path b).
- **Metric delta pattern in tests:** always read `testutil.ToFloat64(...)` before the handler call and compare `after - before`, not absolute values. Prometheus counters accumulate across test cases; absolute values are non-deterministic.
- **External test package:** keep `package handler_test` — do not change the package declaration.
- **No new mocks:** the existing `FakeJobSpawner`, `FakeConfigResolver`, `FakeResultPublisher` cover all test scenarios. Do not add Counterfeiter annotations or generated mocks for this fix.
- **Existing tests must pass unchanged.** The `"skips task with phase human_review"` test (tid-4) already exercises the `human_review` path and will now route via the terminal gate instead of the allowlist; the assertion (spawn count = 0) still holds. Do NOT delete or rename it.
- **Error wrapping uses `github.com/bborbe/errors`.** The gate introduces no new error returns — it is a pure data check. Do not add `fmt.Errorf` anywhere.
- **Do NOT commit.** dark-factory handles git.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Terminal-phase helper present in non-test file:
```bash
grep -nE "IsTerminal|terminalPhases" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥2 matches (the `var terminalPhases` definition and the `IsTerminal` function; referenced in `parseAndFilter`).

Terminal set contains exactly the two constants:
```bash
grep -A 5 "terminalPhases = map" task/executor/pkg/handler/task_event_handler.go
```
Expected: body contains `TaskPhaseHumanReview` and `TaskPhaseDone`, nothing else.

Invariant comment present:
```bash
grep -n "terminal phases must not be spawned" task/executor/pkg/handler/task_event_handler.go
```
Expected: ≥1 match.

Gate positioned before allowlist:
```bash
grep -n "spawn_suppressed_terminal_phase\|effectiveTriggerPhases(config)" task/executor/pkg/handler/task_event_handler.go
```
Expected: the `spawn_suppressed_terminal_phase` line appears at a lower line number than `effectiveTriggerPhases(config).Contains`.

New metric labels pre-initialised:
```bash
grep -n "spawn_suppressed_terminal_phase\|unknown_phase" task/executor/pkg/metrics/metrics.go
```
Expected: exactly 2 matches.

Both new labels referenced in handler and tests:
```bash
grep -rn "spawn_suppressed_terminal_phase\|unknown_phase" task/executor/pkg/handler/
```
Expected: ≥1 production-code line for `spawn_suppressed_terminal_phase` and ≥1 test-assertion line for `spawn_suppressed_terminal_phase`.

Ginkgo table row names in test output:
```bash
cd task/executor && go test ./pkg/handler/... -v -ginkgo.v 2>&1 | grep -E "in_progress =>|human_review =>|done =>|completed =>|sequential events"
```
Expected: 5 lines matching the spec's AC row names.

Cross-cycle test asserts spawn count = 1:
```bash
grep -n "Equal(1)\|SpawnJobCallCount" task/executor/pkg/handler/task_event_handler_test.go | grep -A1 "sequential"
```
Expected: the sequential events `It` block asserts `SpawnJobCallCount()` equals 1 after two handler invocations.

Nil-phase test does not emit suppress metric:
```bash
grep -n "spawn_suppressed_terminal_phase\|nil-phase\|nil phase" task/executor/pkg/handler/task_event_handler_test.go
```
Expected: ≥1 test asserting `after - before` equals `float64(0)` for the nil-phase scenario.

CHANGELOG entry present:
```bash
grep -n "spawn_suppressed\|terminal.phase" CHANGELOG.md | head -3
```
Expected: at least 1 match under `## Unreleased`.

Full precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
