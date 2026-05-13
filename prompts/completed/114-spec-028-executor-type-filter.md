---
status: completed
spec: [028-agent-executor-task-type-filter]
summary: 'Added pre-spawn task-type filter to task/executor: EffectiveTaskTypes/TaskTypeInSet helpers, PublishTypeMismatchFailure publisher method, type_mismatch metric label, 5 handler behavior matrix tests, docs/CHANGELOG updated, make precommit exit 0.'
container: agent-114-spec-028-executor-type-filter
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T20:30:00Z"
queued: "2026-05-13T20:30:17Z"
started: "2026-05-13T20:30:20Z"
completed: "2026-05-13T20:41:10Z"
branch: dark-factory/agent-executor-task-type-filter
---

<summary>
- The task-event handler gains a pre-spawn type filter: before running status/phase/stage checks, the executor verifies the task's `task_type` is in the agent's declared effective type set
- The effective type set is the union of the agent's singular `taskType` (deprecated, still functional) and its `taskTypes` list — computed by a new pure helper testable in isolation
- When the task's `task_type` is missing or not in the effective set, the handler publishes a synthetic failure immediately — no K8s Job is spawned, trigger_count and retry_count are not bumped
- The failure write sets `phase: ai_review`, `assignee: ""`, and `current_job: ""`, surfacing the task in the existing operator inbox (assignee-empty signal from spec 021)
- The failure body includes Timestamp, Assignee (the rejecting agent), and Reason (naming the mismatch or absent field), giving operators a clear signal without inspecting git history
- Five handler test branches cover every path: singular-only match, list-only match, overlap match, mismatch (both fields set, type wrong), missing task_type — each asserting spawn vs failure outcome
- Re-delivered tasks (after assignee cleared to "") are caught by the existing empty-assignee filter before reaching the type filter — inheriting spec 021's idempotency contract
- `docs/task-flow-and-failure-semantics.md` gains a new publisher row and a new "type mismatch" scenario for operator reference
</summary>

<objective>
Add a task-type pre-spawn filter to the `task/executor` handler that computes the agent's effective task-type set (`{cfg.TaskType} ∪ cfg.TaskTypes`) after resolving the Config CR, then publishes a synthetic failure (via the existing publisher mechanism) when the task's `task_type` is absent or mismatched — clearing `assignee` and leaving no Job. Operators see the rejection in the existing `assignee == ""` operator inbox and can correct either the task's `task_type` or the agent's CRD before re-delegating.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, coverage ≥80%
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `Create*` prefix, zero logic in factories
- `go-composition.md` in `~/.claude/plugins/marketplaces/coding/docs/` — injected interfaces, no package-level calls
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`; never bare `context.Background()` in pkg/
- `go-context-cancellation-in-loops.md` in `~/.claude/plugins/marketplaces/coding/docs/` — context checks in loops
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**

- `task/executor/pkg/handler/task_event_handler.go` — full handler: `parseAndFilter` (lines ~83–169), `spawnIfNeeded`, existing filter order; the type filter is inserted AFTER Config resolution and BEFORE the status filter
- `task/executor/pkg/result_publisher.go` — `ResultPublisher` interface and `resultPublisher` implementation; `PublishFailure` body format, `publishRaw` pattern; the new `PublishTypeMismatchFailure` method mirrors this pattern with `phase: ai_review` and `assignee: ""`
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct; read before adding `TaskType` and `TaskTypes` fields
- `task/executor/pkg/config_resolver.go` — `convert()` function; add `TaskType` and `TaskTypes` to the mapping
- `task/executor/pkg/handler/task_event_handler_test.go` — existing test structure; new behavior-matrix tests are added here (do NOT add a second `TestHandler` — it already exists)

**Inline reference — `ResultPublisher` interface (result_publisher.go:28-38):**
```go
type ResultPublisher interface {
    PublishSpawnNotification(ctx context.Context, task lib.Task, jobName string) error
    PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error
    PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error
}
```
Add `PublishTypeMismatchFailure(ctx context.Context, task lib.Task, reason string) error` as the fourth method.

**Inline reference — `PublishFailure` body format (result_publisher.go:77-103):**
```go
section := fmt.Sprintf(
    "## Failure\n\n- **Timestamp:** %s\n- **Job:** %s\n- **Reason:** %s\n",
    now, jobName, reason,
)
cmd := taskcmd.UpdateFrontmatterCommand{
    TaskIdentifier: task.TaskIdentifier,
    Updates: lib.TaskFrontmatter{
        "status":      "in_progress",
        "phase":       "human_review",
        "current_job": "",
    },
    Body: &taskcmd.BodySection{Heading: "## Failure", Section: section},
}
```
`PublishTypeMismatchFailure` differs in three ways: body uses `Assignee` bullet instead of `Job`; `phase` is `"ai_review"` not `"human_review"`; `assignee` key is added with value `""`.

**Inline reference — `AgentConfiguration` struct (agent_configuration.go:12-37):**
```go
type AgentConfiguration struct {
    Assignee          string
    Image             string
    Env               map[string]string
    VolumeClaim       string
    VolumeMountPath   string
    SecretName        string
    Resources         *agentv1.AgentResources
    PriorityClassName string
    Trigger           *agentv1.Trigger
}
```
Add two fields immediately after `Assignee`:
```go
TaskType  string   // singular taskType from ConfigSpec; deprecated but still effective
TaskTypes []string // list taskTypes from ConfigSpec; may be nil when only TaskType is set
```

**Inline reference — `convert()` in config_resolver.go:66-78:**
```go
func convert(obj agentv1.Config, branch string) AgentConfiguration {
    return AgentConfiguration{
        Assignee:          obj.Spec.Assignee,
        Image:             obj.Spec.Image + ":" + branch,
        Env:               copyEnv(obj.Spec.Env),
        SecretName:        obj.Spec.SecretName,
        VolumeClaim:       obj.Spec.VolumeClaim,
        VolumeMountPath:   obj.Spec.VolumeMountPath,
        Resources:         obj.Spec.Resources.DeepCopy(),
        PriorityClassName: obj.Spec.PriorityClassName,
        Trigger:           obj.Spec.Trigger,
    }
}
```
Add `TaskType: obj.Spec.TaskType` and `TaskTypes: append([]string(nil), obj.Spec.TaskTypes...)` after `Assignee`.

**Inline reference — `TaskFrontmatter.String` generic accessor (lib/agent_task-frontmatter.go:114-121):**
```go
func (f TaskFrontmatter) String(key string) (string, bool) {
    v, ok := f[key]
    if !ok { return "", false }
    s, ok := v.(string)
    return s, ok
}
```
Use `task.Frontmatter.String("task_type")` to read the task's type value. Empty string (key absent or value not a string) is treated as missing — strict semantics.

**Inline reference — filter insertion point in `parseAndFilter` (task_event_handler.go:136-168):**
```go
// --- INSERT TYPE FILTER HERE (after config resolution, before effectiveStatuses) ---

effectiveStatuses := effectiveTriggerStatuses(config)
if !effectiveStatuses.Contains(task.Frontmatter.Status()) { ... }

phase := task.Frontmatter.Phase()
if phase == nil || !effectiveTriggerPhases(config).Contains(*phase) { ... }

stage := task.Frontmatter.Stage()
if stage != string(h.branch) { ... }

if task.Frontmatter.Assignee() == "" { ... }

return task, config, false, nil
```

**Verify before importing any external symbol:**
```bash
grep -n "func EffectiveTaskTypes\|func TaskTypeInSet" task/executor/pkg/ 2>/dev/null || echo "not yet created"
grep -n "PublishTypeMismatchFailure" task/executor/pkg/result_publisher.go 2>/dev/null || echo "not yet created"
```
</context>

<requirements>

## 1. Add `TaskType` and `TaskTypes` to `AgentConfiguration`

**File:** `task/executor/pkg/agent_configuration.go`

Read the full file before editing. Add two fields to `AgentConfiguration`, immediately after `Assignee`:

```go
// TaskType is the singular task_type value from ConfigSpec.TaskType.
// Deprecated in favour of TaskTypes; stays functional.
TaskType string
// TaskTypes is the list of task_type values from ConfigSpec.TaskTypes.
// Nil when the CRD only sets the singular TaskType field.
TaskTypes []string
```

Update `TaggedConfigurations` (lines 55–71) to carry the new fields through the copy:
```go
result[i] = AgentConfiguration{
    Assignee:          c.Assignee,
    TaskType:          c.TaskType,
    TaskTypes:         append([]string(nil), c.TaskTypes...),
    Image:             c.Image + ":" + branch,
    // ... rest unchanged
}
```

## 2. Update `convert()` in `task/executor/pkg/config_resolver.go`

Read the full file before editing. In `convert()`, add the two new fields immediately after `Assignee`:

```go
return AgentConfiguration{
    Assignee:          obj.Spec.Assignee,
    TaskType:          obj.Spec.TaskType,
    TaskTypes:         append([]string(nil), obj.Spec.TaskTypes...),
    Image:             obj.Spec.Image + ":" + branch,
    // ... rest unchanged
}
```

The `append([]string(nil), ...)` copy prevents mutations of the CRD's slice from leaking into the AgentConfiguration.

## 3. Create `task/executor/pkg/task_type_filter.go`

New file. License header required (copy from `agent_configuration.go`).

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

// EffectiveTaskTypes computes the union of a singular taskType and a taskTypes list.
// The singular taskType is first in the result when non-empty. Duplicates are removed
// while preserving order. The result is the set an agent accepts.
func EffectiveTaskTypes(taskType string, taskTypes []string) []string {
	seen := make(map[string]struct{})
	var result []string
	if taskType != "" {
		seen[taskType] = struct{}{}
		result = append(result, taskType)
	}
	for _, t := range taskTypes {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			result = append(result, t)
		}
	}
	return result
}

// TaskTypeInSet reports whether taskType is in the effectiveTypes set.
// Empty taskType never matches — strict semantics, no bypass for legacy tasks.
func TaskTypeInSet(taskType string, effectiveTypes []string) bool {
	if taskType == "" {
		return false
	}
	for _, t := range effectiveTypes {
		if t == taskType {
			return true
		}
	}
	return false
}
```

No imports needed — pure Go, no external packages.

## 4. Add `PublishTypeMismatchFailure` to `ResultPublisher` interface

**File:** `task/executor/pkg/result_publisher.go`

Read the full file before editing. Add the new method to the `ResultPublisher` interface (after `PublishIncrementTriggerCount`):

```go
// PublishTypeMismatchFailure publishes a synthetic failure when the task's task_type
// is not in the agent's effective type set. Sets phase=ai_review and clears assignee
// so the task surfaces in the operator inbox. Does not bump trigger_count or retry_count.
PublishTypeMismatchFailure(ctx context.Context, task lib.Task, reason string) error
```

## 5. Implement `PublishTypeMismatchFailure` in `resultPublisher`

**File:** `task/executor/pkg/result_publisher.go`

Add the implementation after `PublishIncrementTriggerCount` and before `publishRaw`:

```go
func (p *resultPublisher) PublishTypeMismatchFailure(
	ctx context.Context,
	task lib.Task,
	reason string,
) error {
	now := p.currentDateTime.Now().UTC().Format(time.RFC3339)
	section := fmt.Sprintf(
		"## Failure\n\n- **Timestamp:** %s\n- **Assignee:** %s\n- **Reason:** %s\n",
		now,
		task.Frontmatter.Assignee(),
		reason,
	)
	cmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates: lib.TaskFrontmatter{
			"status":      "in_progress",
			"phase":       "ai_review",
			"assignee":    "",
			"current_job": "",
		},
		Body: &taskcmd.BodySection{
			Heading: "## Failure",
			Section: section,
		},
	}
	return p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, cmd)
}
```

Key differences from `PublishFailure`:
- Body: `Assignee` bullet (not `Job`) — identifies the rejecting agent before it is cleared
- `phase`: `"ai_review"` (not `"human_review"` — type mismatch is operator-resolvable, not human-work)
- `assignee`: `""` — clears the assignee to surface in operator inbox (per spec 021)

## 6. Modify `parseAndFilter` in the task event handler

**File:** `task/executor/pkg/handler/task_event_handler.go`

Read the full file before editing. In `parseAndFilter`, insert the type filter block immediately after the config resolution section (after line `config = &resolved`) and before `effectiveStatuses := effectiveTriggerStatuses(config)`.

Add the `"fmt"` import if not already present (check the existing import block first).

```go
// Type filter: effective set = {cfg.TaskType} ∪ cfg.TaskTypes. Task must declare a
// matching task_type or the executor rejects it with a synthetic failure (no Job spawned).
if config != nil {
    effectiveTypes := pkg.EffectiveTaskTypes(config.TaskType, config.TaskTypes)
    taskType, _ := task.Frontmatter.String("task_type")
    if !pkg.TaskTypeInSet(taskType, effectiveTypes) {
        var reason string
        if taskType == "" {
            reason = fmt.Sprintf(
                "task has no task_type; agent %q accepts %v",
                config.Assignee, effectiveTypes,
            )
        } else {
            reason = fmt.Sprintf(
                "task_type %q not in effective set %v of agent %q",
                taskType, effectiveTypes, config.Assignee,
            )
        }
        if err := h.resultPublisher.PublishTypeMismatchFailure(ctx, task, reason); err != nil {
            metrics.TaskEventsTotal.WithLabelValues("error").Inc()
            return lib.Task{}, nil, false, errors.Wrapf(
                ctx, err, "publish type mismatch failure for task %s", task.TaskIdentifier,
            )
        }
        glog.V(2).Infof("type mismatch: %s (task %s)", reason, task.TaskIdentifier)
        metrics.TaskEventsTotal.WithLabelValues("type_mismatch").Inc()
        return lib.Task{}, nil, true, nil
    }
}
```

**Return semantics:**
- Publish succeeds → `(lib.Task{}, nil, true, nil)` — caller checks skip=true, returns nil (no spawn)
- Publish fails → `(lib.Task{}, nil, false, err)` — caller checks err!=nil first, returns error

The `nil` config guard (`if config != nil`) means: when assignee is empty (config not resolved), the type filter is skipped — the existing empty-assignee filter (later in the function) handles that case.

Verify no import is added that the file already has. Check the existing import block before editing.

## 6b. Pre-initialize the new `type_mismatch` metric label

**File:** `task/executor/pkg/metrics/metrics.go`

The agent repo pre-initializes every `TaskEventsTotal` label combination at startup (the metrics test asserts this). Step 6 introduces `WithLabelValues("type_mismatch")` as a new label value — without pre-init, the metrics test will fail and `make precommit` will not exit 0.

Read the file before editing. Find the existing pre-init block — it looks like a series of `TaskEventsTotal.WithLabelValues("<label>").Add(0)` calls (or equivalent). Add a new line for the `type_mismatch` label using the same idiom:

```go
TaskEventsTotal.WithLabelValues("type_mismatch").Add(0)
```

**File:** `task/executor/pkg/metrics/metrics_test.go`

Find the existing assertion (around line 30) that verifies all `TaskEventsTotal` label combinations are pre-initialized. Extend the expected-labels list to include `"type_mismatch"`.

Verify both files via:
```bash
grep -n "type_mismatch" task/executor/pkg/metrics/metrics.go task/executor/pkg/metrics/metrics_test.go
```
Expected: at least one match in each file.

## 7. Regenerate mocks

```bash
cd task/executor && make generate
```

This regenerates `mocks/result_publisher.go`, adding `PublishTypeMismatchFailure`, `PublishTypeMismatchFailureCallCount`, `PublishTypeMismatchFailureArgsForCall`, etc. to `FakeResultPublisher`.

Verify:
```bash
grep -n "PublishTypeMismatchFailure" task/executor/mocks/result_publisher.go | head -5
```
Expected: method stub, call count function, args accessor.

## 8. Create `task/executor/pkg/task_type_filter_test.go`

New test file. Package: `pkg_test`. License header required.

Check for an existing suite file for the `pkg` package:
```bash
ls task/executor/pkg/*_suite_test.go 2>/dev/null || echo "no suite file"
```

If no suite file exists, create `task/executor/pkg/pkg_suite_test.go`:
```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPkg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pkg Suite")
}
```

If a suite file already exists, do NOT add a second `TestPkg` function — just add the test file.

**`task/executor/pkg/task_type_filter_test.go`:**
```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	pkg "github.com/bborbe/agent/task/executor/pkg"
)

var _ = Describe("EffectiveTaskTypes", func() {
	It("includes singular taskType when non-empty", func() {
		result := pkg.EffectiveTaskTypes("pr-review", nil)
		Expect(result).To(Equal([]string{"pr-review"}))
	})

	It("includes all taskTypes list elements", func() {
		result := pkg.EffectiveTaskTypes("", []string{"pr-review", "oauth-probe"})
		Expect(result).To(Equal([]string{"pr-review", "oauth-probe"}))
	})

	It("unions singular and list, singular first", func() {
		result := pkg.EffectiveTaskTypes("pr-review", []string{"oauth-probe"})
		Expect(result).To(Equal([]string{"pr-review", "oauth-probe"}))
	})

	It("deduplicates when singular appears in list", func() {
		result := pkg.EffectiveTaskTypes("pr-review", []string{"pr-review", "oauth-probe"})
		Expect(result).To(Equal([]string{"pr-review", "oauth-probe"}))
	})

	It("returns empty slice when both are empty", func() {
		result := pkg.EffectiveTaskTypes("", nil)
		Expect(result).To(BeNil())
	})

	It("skips empty singular taskType (empty string is not in result)", func() {
		result := pkg.EffectiveTaskTypes("", []string{"pr-review"})
		Expect(result).To(Equal([]string{"pr-review"}))
	})
})

var _ = Describe("TaskTypeInSet", func() {
	It("returns true when taskType is in the set", func() {
		Expect(pkg.TaskTypeInSet("pr-review", []string{"pr-review", "oauth-probe"})).To(BeTrue())
	})

	It("returns false when taskType is not in the set", func() {
		Expect(pkg.TaskTypeInSet("code-review", []string{"pr-review", "oauth-probe"})).To(BeFalse())
	})

	It("returns false for empty taskType regardless of set", func() {
		Expect(pkg.TaskTypeInSet("", []string{"pr-review"})).To(BeFalse())
	})

	It("returns false for empty taskType against empty set", func() {
		Expect(pkg.TaskTypeInSet("", nil)).To(BeFalse())
	})

	It("returns false when set is empty", func() {
		Expect(pkg.TaskTypeInSet("pr-review", nil)).To(BeFalse())
	})
})
```

## 8b. Add boundary-validation test for the synthetic-failure payload

**File:** `task/executor/pkg/result_publisher_test.go`

The synthetic failure builds an `UpdateFrontmatterCommand` and ships it through the cqrs publish path. The cqrs framework validates command payloads at publish time via `cmd.Validate(ctx)` (see `lib/command/task/update-frontmatter-command.go` for the contract). A shape-only test that only inspects what the publisher CALLED on the fake sender would silently let a malformed command slip through.

Add a test inside the existing `Describe("ResultPublisher")` block that:

1. Calls `publisher.PublishTypeMismatchFailure(ctx, task, "reason text")` with a real publisher (use the existing constructor pattern in this test file, with a counterfeiter fake `CommandObjectSender`).
2. Captures the published `cdb.CommandObject` via the fake sender's `SendCommandObjectArgsForCall(0)`.
3. Extracts the `Event` payload, type-asserts to `taskcmd.UpdateFrontmatterCommand`, and calls `cmd.Validate(ctx)`.
4. Asserts `Validate(ctx)` returns no error (`Expect(cmd.Validate(ctx)).To(Succeed())`).

The exact unmarshal/type-assert pattern should mirror what `result_publisher_test.go` already uses for inspecting published commands — grep the existing test file for `SendCommandObjectArgsForCall` to find the pattern. If no precedent exists in this test file, do NOT invent a new unmarshal approach; instead inspect the command's `Operation` and verify `cmd.TaskIdentifier` is non-empty and `cmd.Updates` contains `assignee = ""`, `phase = "ai_review"`, `current_job = ""` — all of which are the shape contract the controller will rely on.

This boundary test catches a schema-tightening regression (if `UpdateFrontmatterCommand.Validate` ever gets stricter) before it silently breaks the synthetic failure path in production.

## 9. Add handler behavior matrix tests

**File:** `task/executor/pkg/handler/task_event_handler_test.go`

Read the full test file before editing. Add five behavior-matrix `It` blocks inside the existing `Describe` for `TaskEventHandler`. They test all five branches:

1. **Singular-only match** — `config.TaskType="pr-review"`, `config.TaskTypes=nil`, `task_type="pr-review"` → spawns Job, no `PublishTypeMismatchFailure` call
2. **List-only match** — `config.TaskType=""`, `config.TaskTypes=["oauth-probe"]`, `task_type="oauth-probe"` → spawns Job
3. **Overlap match** — `config.TaskType="pr-review"`, `config.TaskTypes=["oauth-probe"]`, `task_type="oauth-probe"` → spawns Job
4. **Mismatch** — `config.TaskType="pr-review"`, `config.TaskTypes=["oauth-probe"]`, `task_type="code-review"` → `PublishTypeMismatchFailureCallCount() == 1`, `SpawnJobCallCount() == 0`
5. **Missing task_type** — `config.TaskType="pr-review"`, `task_type` key absent from frontmatter → `PublishTypeMismatchFailureCallCount() == 1`, `SpawnJobCallCount() == 0`

**Pattern for setting up a task with a given task_type:**
```go
task := lib.Task{
    TaskIdentifier: "test-task-id",
    Frontmatter: lib.TaskFrontmatter{
        "status":    "in_progress",
        "phase":     "planning",
        "stage":     "dev",       // match executor branch
        "assignee":  "agent-pr-reviewer",
        "task_type": "pr-review", // set to the value under test
    },
}
```
For the "missing task_type" case, omit the `"task_type"` key entirely.

**Pattern for setting up the resolver to return a config:**

Before writing: check the mock method name for the resolver:
```bash
grep -n "func.*FakeConfigResolver.*Resolve\b" task/executor/mocks/config_resolver.go | head -3
```
Use `fakeResolver.ResolveReturns(pkg.AgentConfiguration{...}, nil)` with `TaskType` and `TaskTypes` set appropriately.

**For spawn cases:** assert `fakeResultPublisher.PublishTypeMismatchFailureCallCount() == 0` and that `fakeJobSpawner.SpawnJobCallCount() == 1`.

**For mismatch cases:** assert `fakeResultPublisher.PublishTypeMismatchFailureCallCount() == 1` and `fakeJobSpawner.SpawnJobCallCount() == 0`. Also assert the reason passed to `PublishTypeMismatchFailure` (via `PublishTypeMismatchFailureArgsForCall(0)`) contains the mismatched type string.

After writing, verify existing tests still compile and pass:
```bash
cd task/executor && make test
```

## 10. Update `docs/task-flow-and-failure-semantics.md`

Read the file before editing.

**a.** Add a row to the "Executor Publisher Command Kinds" table (after the `PublishFailure` row):

```markdown
| `PublishTypeMismatchFailure` | `update-frontmatter` | `status`, `phase` (`ai_review`), `assignee` (`""`), `current_job` |
```

**b.** Add a "Type mismatch (spec 028)" scenario under `## Failure Scenarios`, after the "Silent infra failure" section:

```markdown
### Type mismatch (spec 028)

1. Task `phase: planning`, `task_type: oauth-probe`, routed to `agent-pr-reviewer` whose effective set is `[pr-review]`.
2. Executor resolves Config CR for `agent-pr-reviewer`, computes effective set `[pr-review]`.
3. Type filter detects mismatch — no Job is spawned.
4. Executor synthesises a failure: `phase: ai_review`, `assignee: ""`, `current_job: ""`. Failure body names the mismatch.
5. Task surfaces in operator inbox (assignee-empty signal). `trigger_count` is NOT incremented.
6. Operator either edits the task's `task_type` to `pr-review`, or adds `oauth-probe` to the agent's `taskTypes` CRD field, then re-delegates by setting `assignee`.
```

**c.** Update the Filter description in `## Full Flow` (the bullet starting `│  filter: status=in_progress...`) to include the type filter:

```markdown
│  filter: task_type ∈ agent's effective set AND
│           status=in_progress AND phase ∈ {planning, in_progress, ai_review}
│           AND matching assignee AND matching stage
```

## 11. Update `CHANGELOG.md` at repo root

Check for an existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header. Add:

```markdown
- feat(task/executor): add pre-spawn task-type filter — executor computes effective type set (`taskType` ∪ `taskTypes`) from the Config CR and publishes a synthetic failure (phase=ai_review, assignee="" cleared) when a task's `task_type` is absent or mismatched; no Job is spawned and trigger_count/retry_count are not bumped; **NOTE:** tasks without a `task_type` frontmatter field will now be rejected on first event delivery — operators must add `task_type` to legacy task templates before deploying this change
```

## 12. Run iterative tests

```bash
cd task/executor && make test
```

Fix compile errors before continuing. Common issues:
- `"fmt"` missing from handler import block — add it
- `pkg.EffectiveTaskTypes` / `pkg.TaskTypeInSet` not found — confirm `task_type_filter.go` is in package `pkg` (not `handler`)
- `PublishTypeMismatchFailure` not in `FakeResultPublisher` — run `make generate` first
- `AgentConfiguration.TaskType` / `.TaskTypes` not found in mock setup — add the fields to `AgentConfiguration` in step 1 before writing tests

Check test coverage for changed packages:
```bash
cd task/executor && go test -coverprofile=/tmp/filter-cover.out ./pkg/... && go tool cover -func=/tmp/filter-cover.out | grep -E "task_type_filter|total"
```
Coverage for `task_type_filter.go` must be ≥80% (all branches covered by the helper tests).

```bash
cd task/executor && go test -coverprofile=/tmp/handler-cover.out ./pkg/handler/... && go tool cover -func=/tmp/handler-cover.out | grep "total:"
```
Handler coverage must remain ≥80%.

## 13. Run final precommit

```bash
cd task/executor && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

If `make precommit` reports mock drift: re-run `make generate`, verify only `mocks/result_publisher.go` changed (new `PublishTypeMismatchFailure` stubs), then re-run the failing target.

</requirements>

<constraints>
- **Sibling-spec dependency:** this prompt assumes spec 026 (`agent-config-task-types-list`) has already shipped — `ConfigSpec.TaskType` and `ConfigSpec.TaskTypes` are both present in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`. Verify before implementing:
  ```bash
  grep -n "TaskTypes" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go | head -5
  ```
  Expected: `TaskTypes []string` field with `json:"taskTypes,omitempty"`. If absent, STOP and report `"status":"failed"` — this prompt's precondition is not met.
- Change is confined to the `task/executor` module and root `CHANGELOG.md` and `docs/task-flow-and-failure-semantics.md`. No file in `lib/*`, `task/controller/*`, `agent/*`, or `prompt/*` is modified.
- The CRD schema (`task/executor/pkg/k8s_connector.go`) is NOT modified by this prompt.
- The effective-set computation (`EffectiveTaskTypes`, `TaskTypeInSet`) must be pure functions — no I/O, no external calls. They live in `task/executor/pkg/` (package `pkg`), not in the handler package.
- The type filter is layered AFTER Config resolution and BEFORE status/phase/stage/assignee filters in `parseAndFilter`. The type filter only fires when `config != nil` (i.e., when the Config was successfully resolved for a non-empty assignee).
- Type-mismatch failures do NOT call `PublishIncrementTriggerCount` and do NOT call `SpawnJob`. The short-circuit returns immediately after the failure publish.
- The `PublishTypeMismatchFailure` implementation must use the existing `publishRaw` helper — no direct Kafka calls, no second `CommandObjectSender`. Reuse the established publish path.
- `PublishTypeMismatchFailure` sets `phase: "ai_review"` (NOT `"human_review"`). Type mismatch is operator-resolvable without human intervention.
- `PublishTypeMismatchFailure` sets `assignee: ""` in the Updates map — this is the operator-inbox signal (spec 021).
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks. External test package (`pkg_test`, `handler_test`).
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`, never bare `context.Background()` in pkg/ code.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required. It MUST explicitly flag the strict-empty-task_type behavior.
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify `TaskType` and `TaskTypes` added to `AgentConfiguration`:
```bash
grep -n "TaskType\|TaskTypes" task/executor/pkg/agent_configuration.go
```
Expected: two field declarations; `TaskTypes` also appears in `TaggedConfigurations` copy.

Verify `convert()` maps both fields:
```bash
grep -n "TaskType\|TaskTypes" task/executor/pkg/config_resolver.go
```
Expected: `TaskType: obj.Spec.TaskType` and `TaskTypes: append(...)` in the `convert` return literal.

Verify helper file exists with both functions:
```bash
grep -n "func EffectiveTaskTypes\|func TaskTypeInSet" task/executor/pkg/task_type_filter.go
```
Expected: two function definitions.

Verify `PublishTypeMismatchFailure` is in the interface and implementation:
```bash
grep -n "PublishTypeMismatchFailure" task/executor/pkg/result_publisher.go
```
Expected: one method in interface, one implementation.

Verify mock was regenerated with the new method:
```bash
grep -n "PublishTypeMismatchFailure" task/executor/mocks/result_publisher.go | head -3
```
Expected: stub, call-count function, args accessor.

Verify type filter is in the handler (between config resolution and status filter):
```bash
grep -n "EffectiveTaskTypes\|TaskTypeInSet\|PublishTypeMismatchFailure\|type_mismatch" task/executor/pkg/handler/task_event_handler.go
```
Expected: all four names present.

Verify docs updated with new publisher row and scenario:
```bash
grep -n "PublishTypeMismatchFailure\|Type mismatch" docs/task-flow-and-failure-semantics.md
```
Expected: table row and scenario heading.

Verify CHANGELOG updated with type-filter bullet:
```bash
grep -n "task-type filter\|task_type.*filter\|type-filter" CHANGELOG.md | head -5
```
Expected: at least one match under `## Unreleased` mentioning the strict empty-task_type behavior.

Run tests:
```bash
cd task/executor && make test
```
Expected: exit 0, all specs pass including helper tests and five behavior-matrix handler tests.

Run coverage check:
```bash
cd task/executor && go test -coverprofile=/tmp/filter-cover.out ./pkg/... && go tool cover -func=/tmp/filter-cover.out | grep -E "task_type_filter|total"
```
Expected: `task_type_filter.go` coverage ≥80%.

Run precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
