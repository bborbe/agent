---
status: committing
spec: [030-executor-inject-task-type-env]
summary: Injected TASK_TYPE env var into every spawned K8s Job by adding taskTypeString helper, wiring it into buildJobEnvBuilder after PHASE, switching taskTypeMismatchReason to the typed TaskType() accessor, adding TASK_TYPE assertions to existing and new spawner tests, and updating CHANGELOG.md.
container: agent-119-spec-030-executor-inject
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T12:10:00Z"
queued: "2026-05-14T12:14:15Z"
started: "2026-05-14T12:16:44Z"
branch: dark-factory/executor-inject-task-type-env
---

<summary>
- The executor's `buildJobEnvBuilder` gains a `TASK_TYPE` env var on every spawned Job, sourced from the task's `task_type` frontmatter field
- A package-private `taskTypeString(f lib.TaskFrontmatter) string` helper is added next to `taskPhaseString`, following the exact same pattern
- `TASK_TYPE` is injected immediately after `PHASE` and before per-agent `config.Env` entries — matching the spec's ordering constraint
- The `taskTypeMismatchReason` function in the handler switches from the generic `task.Frontmatter.String("task_type")` call to the typed `task.Frontmatter.TaskType()` accessor
- All existing spawn paths get `TASK_TYPE` — no gating, no opt-in, no agent-config check
- Spawner tests assert `TASK_TYPE=healthcheck` for a task with a matching frontmatter value, and `TASK_TYPE=""` for a task with no `task_type` key
- `make precommit` passes in `task/executor/`
</summary>

<objective>
Wire `TASK_TYPE` env injection into the executor's job spawner so every K8s Job receives the task's `task_type` frontmatter value. After this ships, agent binaries that already declare a `TASK_TYPE` env field (spec 029) will receive the real task-type label instead of their `"unknown"` default, enabling per-task-type Prometheus metric breakdowns.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, coverage ≥80%
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`, never bare `context.Background()` in pkg/
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Precondition check — run before implementing:**
```bash
grep -n "func.*TaskFrontmatter.*TaskType" lib/agent_task-frontmatter.go
```
If no match is found, STOP. Prompt 1 (`1-spec-030-lib-task-type.md`) must complete successfully before this prompt runs. Report `"status":"failed"` with reason "lib.TaskFrontmatter.TaskType() accessor not yet deployed (prompt 1)".

**Key files to read in full before editing:**
- `task/executor/pkg/spawner/job_spawner.go` — full spawner; focus on `buildJobEnvBuilder` (lines ~300–322) and `taskPhaseString` (lines ~276–281) — `taskTypeString` and the `TASK_TYPE` injection mirror these exactly
- `task/executor/pkg/handler/task_event_handler.go` — find `taskTypeMismatchReason` function; it currently calls `task.Frontmatter.String("task_type")` on one line — that line is the only change in this file
- `task/executor/pkg/spawner/job_spawner_test.go` — existing test structure; TASK_TYPE assertions are appended to the existing test, plus two new `It` blocks are added

**Inline reference — current `taskPhaseString` to mirror exactly:**
```go
// taskPhaseString returns the string value of the task's phase, or "" when absent.
func taskPhaseString(f lib.TaskFrontmatter) string {
	if p := f.Phase(); p != nil {
		return string(*p)
	}
	return ""
}
```

**Inline reference — new `taskTypeString` helper (add immediately after `taskPhaseString`):**
```go
// taskTypeString returns the string value of the task's task_type, or "" when absent.
func taskTypeString(f lib.TaskFrontmatter) string {
	return f.TaskType().String()
}
```

**Inline reference — current `buildJobEnvBuilder` env additions (job_spawner.go ~311–320):**
```go
envBuilder.Add("TASK_CONTENT", taskContent)
envBuilder.Add("TASK_ID", string(task.TaskIdentifier))
envBuilder.Add("KAFKA_BROKERS", s.kafkaBrokers)
envBuilder.Add("BRANCH", s.branch)
envBuilder.Add("PHASE", taskPhaseString(task.Frontmatter))
for key, value := range config.Env {
    envBuilder.Add(key, value)
}
```
Add `envBuilder.Add("TASK_TYPE", taskTypeString(task.Frontmatter))` immediately after the `PHASE` line and before the `for key, value := range config.Env` loop.

**Inline reference — `taskTypeMismatchReason` change (handler file ~line 212):**
```go
// BEFORE:
taskType, _ := task.Frontmatter.String("task_type")
if pkg.TaskTypeInSet(taskType, effectiveTypes) {

// AFTER:
taskType := task.Frontmatter.TaskType()
if pkg.TaskTypeInSet(string(taskType), effectiveTypes) {
```
The local variable type changes from `string` to `lib.TaskType`. The call to `pkg.TaskTypeInSet` must pass `string(taskType)` because `TaskTypeInSet` takes `string` (non-goal: keep filter signature stable). Check whether `taskType` is used elsewhere in the function (e.g. in the `fmt.Sprintf` reason string) and update those references too — they should use `string(taskType)` or `taskType.String()`.

**Symbol verification — run before writing:**
```bash
# Confirm TaskType() accessor exists (precondition)
grep -n "func.*TaskFrontmatter.*TaskType" lib/agent_task-frontmatter.go

# Confirm lib.TaskType is importable
grep -n "type TaskType string" lib/agent_task-type.go

# Confirm taskPhaseString exists (for placement reference)
grep -n "func taskPhaseString" task/executor/pkg/spawner/job_spawner.go

# Confirm buildJobEnvBuilder signature and PHASE line
grep -n "PHASE\|buildJobEnvBuilder" task/executor/pkg/spawner/job_spawner.go
```
</context>

<requirements>

## 1. Add `taskTypeString` helper to `task/executor/pkg/spawner/job_spawner.go`

Read the full file before editing.

Find the `taskPhaseString` function (around line 276). Immediately after it, add:

```go
// taskTypeString returns the string value of the task's task_type, or "" when absent.
func taskTypeString(f lib.TaskFrontmatter) string {
	return f.TaskType().String()
}
```

Verify placement:
```bash
grep -n "taskPhaseString\|taskTypeString" task/executor/pkg/spawner/job_spawner.go
```
Expected: `taskTypeString` appears on the line immediately after the end of `taskPhaseString`.

## 2. Inject `TASK_TYPE` in `buildJobEnvBuilder`

In `buildJobEnvBuilder`, find the line:
```go
envBuilder.Add("PHASE", taskPhaseString(task.Frontmatter))
```
Add the following line immediately after it (before the `for key, value := range config.Env` loop):
```go
envBuilder.Add("TASK_TYPE", taskTypeString(task.Frontmatter))
```

The final ordering in the env block must be:
```
TASK_CONTENT
TASK_ID
KAFKA_BROKERS
BRANCH
PHASE
TASK_TYPE          ← new
config.Env entries
```

Verify ordering:
```bash
grep -n "envBuilder.Add\|config.Env" task/executor/pkg/spawner/job_spawner.go | grep -A 10 "TASK_CONTENT"
```
Expected: TASK_TYPE appears between PHASE and the config.Env loop.

## 3. Switch `taskTypeMismatchReason` to typed accessor

**File:** `task/executor/pkg/handler/task_event_handler.go`

Read the full file before editing. Find the `taskTypeMismatchReason` function. Locate the line:
```go
taskType, _ := task.Frontmatter.String("task_type")
```
Replace it with:
```go
taskType := task.Frontmatter.TaskType()
```

After this change, `taskType` is `lib.TaskType` (not `string`). Find every usage of `taskType` within the function body and update them:
- `pkg.TaskTypeInSet(taskType, effectiveTypes)` → `pkg.TaskTypeInSet(string(taskType), effectiveTypes)`
- `taskType == ""` → `taskType == ""`  (still valid — comparing `lib.TaskType` to untyped string constant `""` works in Go)
- Any `fmt.Sprintf` that embeds `taskType` — the `%q` and `%v` verbs call `.String()` automatically, so no change needed for those

Verify the change:
```bash
grep -n "Frontmatter.String\|Frontmatter.TaskType\|taskType" task/executor/pkg/handler/task_event_handler.go | head -10
```
Expected: `Frontmatter.String("task_type")` is absent; `Frontmatter.TaskType()` is present.

Build check:
```bash
cd task/executor && go build ./...
```
Expected: exit 0.

## 4. Add TASK_TYPE assertion to existing spawner test

**File:** `task/executor/pkg/spawner/job_spawner_test.go`

Read the full file before editing. The first `It("creates a job with correct name and env vars", ...)` block already builds `envMap` and asserts `envMap["PHASE"]`. The task in that test has `"phase": "planning"` but NO `"task_type"` frontmatter key — so the expected `TASK_TYPE` env value is the empty string.

Add the following single assertion immediately after the existing `PHASE` assertion:
```go
Expect(envMap["TASK_TYPE"]).To(Equal(""))
```

## 5. Add two new spawner It blocks for TASK_TYPE

Add two new `It` blocks inside the existing `Describe("SpawnJob")` block, after the existing tests:

**Block 1 — task with `task_type` in frontmatter:**
```go
It("includes TASK_TYPE env var matching the frontmatter task_type value", func() {
    task := lib.Task{
        TaskIdentifier: lib.TaskIdentifier("healthcheck-task-id"),
        Frontmatter: lib.TaskFrontmatter{
            "assignee":  "claude",
            "phase":     "planning",
            "task_type": "healthcheck",
        },
        Content: lib.TaskContent("run health probe"),
    }
    config := pkg.AgentConfiguration{
        Assignee: "claude",
        Image:    "claude-agent:latest",
        Env:      map[string]string{},
    }
    _, err := jobSpawner.SpawnJob(ctx, task, config)
    Expect(err).To(BeNil())

    jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
    Expect(err).To(BeNil())
    Expect(jobs.Items).To(HaveLen(1))

    envMap := make(map[string]string)
    for _, e := range jobs.Items[0].Spec.Template.Spec.Containers[0].Env {
        envMap[e.Name] = e.Value
    }
    Expect(envMap["TASK_TYPE"]).To(Equal("healthcheck"))
})
```

**Block 2 — task without `task_type` in frontmatter:**
```go
It("sets TASK_TYPE to empty string when task_type is absent from frontmatter", func() {
    task := lib.Task{
        TaskIdentifier: lib.TaskIdentifier("no-type-task-id"),
        Frontmatter: lib.TaskFrontmatter{
            "assignee": "claude",
            "phase":    "planning",
        },
        Content: lib.TaskContent("work without task type"),
    }
    config := pkg.AgentConfiguration{
        Assignee: "claude",
        Image:    "claude-agent:latest",
        Env:      map[string]string{},
    }
    _, err := jobSpawner.SpawnJob(ctx, task, config)
    Expect(err).To(BeNil())

    jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
    Expect(err).To(BeNil())
    Expect(jobs.Items).To(HaveLen(1))

    envMap := make(map[string]string)
    for _, e := range jobs.Items[0].Spec.Template.Spec.Containers[0].Env {
        envMap[e.Name] = e.Value
    }
    Expect(envMap["TASK_TYPE"]).To(Equal(""))
})
```

**Note on test isolation:** Each `It` block runs against the shared `fakeClient` which is reset in `BeforeEach`. If `BeforeEach` recreates `fakeClient = fake.NewClientset()`, jobs from one test do not bleed into another. Verify this is the case before adding the tests:
```bash
grep -n "fakeClient = fake.NewClientset" task/executor/pkg/spawner/job_spawner_test.go
```
If `fakeClient` is initialized in `BeforeEach` (not `BeforeSuite`), the reset is per-test and the tests are isolated.

## 6. Run iterative tests

```bash
cd task/executor && go test ./pkg/spawner/...
```
Fix compile errors before continuing. Common issue: `taskType` in `taskTypeMismatchReason` is now `lib.TaskType` — if `task/executor` imports `lib` without an alias, the type is accessed as `lib.TaskType`. Confirm the import alias used in the handler file:
```bash
grep -n "bborbe/agent/lib" task/executor/pkg/handler/task_event_handler.go | head -3
```

Then run all executor tests:
```bash
cd task/executor && go test ./...
```
Expected: exit 0.

Coverage check for the spawner:
```bash
cd task/executor && go test -coverprofile=/tmp/spawner-cover.out ./pkg/spawner/... && go tool cover -func=/tmp/spawner-cover.out | grep -E "job_spawner|total"
```
Expected: `job_spawner.go` coverage remains ≥80%.

## 7. Update `CHANGELOG.md` at repo root

Check for an existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

**If `## Unreleased` exists** (created by prompt 1 or earlier): append ONE new bullet to it. Do NOT modify any existing bullet.

**If `## Unreleased` does NOT exist** (prompt 1 was skipped, hand-trimmed, or this runs standalone): insert a new `## Unreleased` section immediately above the first `## v` header, then add the bullet.

The bullet text to add:
```markdown
- feat(task/executor): inject TASK_TYPE env into every spawned Job from task frontmatter task_type field (spec 030)
```

Verify:
```bash
grep -A 5 "^## Unreleased" CHANGELOG.md
```
Expected: at least one bullet present under `## Unreleased` — this executor bullet. If prompt 1 ran in the same release window, two bullets total (lib + executor).

## 8. Run final precommit in `task/executor/`

```bash
cd task/executor && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

</requirements>

<constraints>
- **Precondition:** `lib.TaskFrontmatter.TaskType()` must exist (created by prompt 1). If absent, report `"status":"failed"`.
- `taskTypeString` is a package-private function in `package spawner`. It is placed immediately after `taskPhaseString` in `job_spawner.go`. It has the exact same signature shape: single arg `lib.TaskFrontmatter`, returns `string`.
- `TASK_TYPE` is inserted into `buildJobEnvBuilder` immediately after `PHASE` and before the `config.Env` loop. The ordering is frozen: existing env names (`TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`, `PHASE`) retain their positions.
- The spawner does NOT call `lib.TaskType.Validate`. The value is forwarded opaquely as-is.
- In `taskTypeMismatchReason`, the local variable `taskType` changes type from `string` to `lib.TaskType`. All call sites within the function that pass `taskType` to `string`-typed parameters must use `string(taskType)` or `.String()`.
- `pkg.TaskTypeInSet` signature is unchanged — it still takes `string` as first arg. Pass `string(taskType)` from the refactored call site.
- No change to `EnvBuilder` interface or k8s package.
- No change to `buildJobEnvBuilder` function signature.
- No new dependency is added.
- Only these files are modified: `task/executor/pkg/spawner/job_spawner.go`, `task/executor/pkg/handler/task_event_handler.go`, `task/executor/pkg/spawner/job_spawner_test.go`, and root `CHANGELOG.md`.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify precondition (lib accessor exists):
```bash
grep -n "func.*TaskFrontmatter.*TaskType" lib/agent_task-frontmatter.go
```
Expected: one match.

Verify `taskTypeString` was added:
```bash
grep -n "func taskTypeString" task/executor/pkg/spawner/job_spawner.go
```
Expected: one match.

Verify placement relative to `taskPhaseString`:
```bash
grep -n "taskPhaseString\|taskTypeString" task/executor/pkg/spawner/job_spawner.go
```
Expected: `taskTypeString` line number is within ~5 lines after `taskPhaseString` end.

Verify `TASK_TYPE` injection after `PHASE`:
```bash
grep -n "PHASE\|TASK_TYPE\|config.Env" task/executor/pkg/spawner/job_spawner.go | head -10
```
Expected: PHASE line, then TASK_TYPE line, then config.Env loop — in that order.

Verify handler no longer uses `Frontmatter.String("task_type")`:
```bash
grep -n "Frontmatter.String.*task_type" task/executor/pkg/handler/task_event_handler.go
```
Expected: no matches.

Verify handler uses typed accessor:
```bash
grep -n "Frontmatter.TaskType\|taskType :=" task/executor/pkg/handler/task_event_handler.go | head -5
```
Expected: `Frontmatter.TaskType()` present.

Verify `string(taskType)` coercion in `TaskTypeInSet` call:
```bash
grep -n "TaskTypeInSet" task/executor/pkg/handler/task_event_handler.go
```
Expected: `TaskTypeInSet(string(taskType), effectiveTypes)`.

Run spawner tests:
```bash
cd task/executor && go test ./pkg/spawner/... -v 2>&1 | grep -E "PASS|FAIL|It "
```
Expected: all tests pass including two new TASK_TYPE assertions.

Run all executor tests:
```bash
cd task/executor && go test ./...
```
Expected: exit 0.

Run coverage:
```bash
cd task/executor && go test -coverprofile=/tmp/spawner-cover.out ./pkg/spawner/... && go tool cover -func=/tmp/spawner-cover.out | grep "total:"
```
Expected: ≥80% total coverage for spawner package.

Run precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
