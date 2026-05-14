---
status: committing
spec: [031-agent-repo-task-type-dispatch]
summary: 'Added CreateAgentForTaskType to agent/code factory with healthcheck dispatch, updated main.go to use it, added Ginkgo/Gomega tests with 100% coverage for the new function, and added the combined CHANGELOG bullet under ## Unreleased.'
container: agent-123-spec-031-agent-code-dispatch
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T13:00:00Z"
queued: "2026-05-14T12:54:19Z"
started: "2026-05-14T12:59:25Z"
branch: dark-factory/agent-repo-task-type-dispatch
---

<summary>
- `agent/code/pkg/factory/factory.go` gains a new `CreateAgentForTaskType` function that dispatches on `lib.TaskType`
- `TaskTypeHealthcheck` routes to `healthcheck.NewAgent(healthcheck.NewNopStep())` — the Nop step returns `done` immediately, proving the binary booted with no external calls
- All other `task_type` values hit the default-error branch with a wrapped error containing `unknown task_type` and the accepted list `[healthcheck]`
- `agent/code/main.go` replaces the direct `factory.CreateAgent().Run(...)` call with an error-aware `factory.CreateAgentForTaskType(ctx, agentlib.TaskType(a.TaskType))` call whose error branch records metrics
- Factory tests are added: Ginkgo suite + dispatch assertions for both branches
- The third and final CHANGELOG bullet `feat(agent/{claude,gemini,code}): per-task-type dispatch via factory.CreateAgentForTaskType` is added to `## Unreleased`
- `make precommit` passes in `agent/code/`
</summary>

<objective>
Add per-task-type dispatch to `agent-code`, making it a healthcheck-only binary. This is the final binary to update; it also owns the combined CHANGELOG entry for all three agent dispatch changes. The existing `CreateAgent` function and `cmd/run-task/main.go` remain unchanged.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — factory pattern, error wrapping
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `Create*` prefix, zero logic
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`

**Precondition check — run before implementing:**
```bash
ls lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-nop-step.go
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
If either check fails, STOP. Prompt 1 (`1-spec-031-lib-healthcheck-package.md`) must complete successfully before this prompt runs. Report `"status":"failed"` with reason `"lib/healthcheck package not yet deployed (prompt 1)"`.

**Key files to read in full before editing:**
- `agent/code/pkg/factory/factory.go` — existing `CreateAgent() *agentlib.Agent` (no arguments); this is the key difference from claude/gemini factories
- `agent/code/main.go` — current `Run()` body with `factory.CreateAgent().Run(...)` call
- `agent/code/cmd/run-task/main.go` — MUST remain unchanged
- `lib/agent_task-type.go` — `TaskTypeHealthcheck` constant value
- `lib/healthcheck/healthcheck-agent.go` — `healthcheck.NewAgent(step) *agentlib.Agent`
- `lib/healthcheck/healthcheck-nop-step.go` — `healthcheck.NewNopStep() agentlib.Step`

**Inline reference — current `CreateAgent` signature (do NOT change):**
```go
func CreateAgent() *agentlib.Agent
```

**Inline reference — new `CreateAgentForTaskType` signature:**
```go
func CreateAgentForTaskType(
    ctx context.Context,
    taskType agentlib.TaskType,
) (*agentlib.Agent, error)
```

**Inline reference — switch body (healthcheck-only binary — no args beyond ctx + taskType):**
```go
switch taskType {
case agentlib.TaskTypeHealthcheck:
    return healthcheck.NewAgent(healthcheck.NewNopStep()), nil
default:
    return nil, errors.Errorf(ctx, "unknown task_type %q for agent-code; accepted: [%s]",
        taskType, agentlib.TaskTypeHealthcheck)
}
```

**Inline reference — current `agent/code/main.go` agent call (after deliverer creation):**
```go
result, err := factory.CreateAgent().Run(ctx, a.Phase, a.TaskContent, deliverer)
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "agent run failed")
}
```

**Inline reference — replacement block for `agent/code/main.go`:**
```go
agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskType(a.TaskType))
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "create agent for task type")
}

result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "agent run failed")
}
```

**Symbol verification — run before writing:**
```bash
# Confirm healthcheck exports
grep -n "^func New" lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-nop-step.go

# Confirm agentlib alias in code main.go
grep -n "bborbe/agent/lib\"" agent/code/main.go

# Confirm existing factory structure (note: CreateAgent takes no args)
grep -n "func Create" agent/code/pkg/factory/factory.go
```
</context>

<requirements>

## 1. Add `CreateAgentForTaskType` to `agent/code/pkg/factory/factory.go`

Read the full file before editing.

Add the `healthcheck` import to the import block:
```go
healthcheck "github.com/bborbe/agent/lib/healthcheck"
```

Also add `"context"` if not already present (check the import block first — code factory already imports it for `CreateDeliverer`).

Add the new function immediately after the existing `CreateAgent` function. Use the exact signature and switch body from `<context>`.

Key constraints:
- `CreateAgentForTaskType` takes only `ctx context.Context` and `taskType agentlib.TaskType` — no runner or parser parameters, because the Nop step needs nothing.
- Only ONE case: `TaskTypeHealthcheck`. The code binary is a pure-Go agent — it has no domain task type.
- The default branch error must contain the literal string `unknown task_type`.
- `CreateAgent` is NOT renamed, NOT removed, NOT modified.
- No claudelib or gemini-specific imports are added.

Verify:
```bash
grep -n "func CreateAgentForTaskType\|func CreateAgent\b" agent/code/pkg/factory/factory.go
```
Expected: two matches.

Build check:
```bash
cd agent/code && go build ./...
```
Expected: exit 0.

## 2. Update `agent/code/main.go`

Read the full file before editing.

Replace the single-line `factory.CreateAgent().Run(...)` block and its error handler with the replacement block from `<context>`.

The `agentlib` alias is already present as `agentlib "github.com/bborbe/agent/lib"`. No new imports are needed in main.go.

Verify:
```bash
grep -n "CreateAgentForTaskType\|factory\.CreateAgent\b" agent/code/main.go
```
Expected: `CreateAgentForTaskType` present; `factory.CreateAgent(` absent.

Verify `cmd/run-task/main.go` unchanged:
```bash
grep -n "CreateAgent\|CreateAgentForTaskType" agent/code/cmd/run-task/main.go
```
Expected: `CreateAgent` or similar present; `CreateAgentForTaskType` absent.

Build check:
```bash
cd agent/code && go build ./...
```
Expected: exit 0.

## 3. Create `agent/code/pkg/factory/factory_suite_test.go`

New file. License header required. Package: `factory_test`.

```go
package factory_test

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

func TestFactory(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Factory Suite")
}
```

## 4. Create `agent/code/pkg/factory/factory_test.go`

New file. License header required. Package: `factory_test`.

`CreateAgentForTaskType` for the code binary takes only `ctx` and `taskType` — no parser or runner arguments.

```go
package factory_test

import (
    "context"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/bborbe/agent/agent/code/pkg/factory"
    agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("CreateAgentForTaskType", func() {
    var ctx context.Context

    BeforeEach(func() {
        ctx = context.Background()
    })

    It("returns a non-nil agent for TaskTypeHealthcheck", func() {
        agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskTypeHealthcheck)
        Expect(err).To(BeNil())
        Expect(agent).NotTo(BeNil())
    })

    It("returns nil agent and error for an unsupported task type", func() {
        agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskType("bogus"))
        Expect(err).NotTo(BeNil())
        Expect(agent).To(BeNil())
        Expect(err.Error()).To(ContainSubstring("unknown task_type"))
        Expect(err.Error()).To(ContainSubstring("bogus"))
    })

    It("returns nil agent and error for the unknown default value", func() {
        agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskType("unknown"))
        Expect(err).NotTo(BeNil())
        Expect(agent).To(BeNil())
        Expect(err.Error()).To(ContainSubstring("unknown task_type"))
    })
})
```

## 5. Run iterative tests

```bash
cd agent/code && go test ./...
```
Expected: exit 0.

Coverage check:
```bash
cd agent/code && go test -coverprofile=/tmp/code-factory-cover.out ./pkg/factory/... && \
  go tool cover -func=/tmp/code-factory-cover.out | grep -E "factory|total"
```
Expected: `factory.go` coverage ≥80%.

## 6. Update `CHANGELOG.md` at repo root

This prompt owns the third and final CHANGELOG bullet for spec 031.

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

**If `## Unreleased` exists** (created by prompt 1 of this spec): verify the two lib bullets from prompt 1 are present, then APPEND one new bullet. Do NOT modify existing bullets.

**If `## Unreleased` does NOT exist** (prompt 1 missed CHANGELOG, or this runs standalone): insert a new `## Unreleased` section immediately above the first `## v` header and add the bullet.

Add exactly ONE bullet:
```markdown
- feat(agent/{claude,gemini,code}): per-task-type dispatch via factory.CreateAgentForTaskType — healthcheck task type routes to a dedicated liveness agent; unknown task_type fails fast with an accepted-types error (spec 031)
```

Verify:
```bash
grep -n "per-task-type dispatch via factory.CreateAgentForTaskType" CHANGELOG.md
```
Expected: exactly one match — the new bullet under `## Unreleased`.

## 7. Run final precommit in `agent/code/`

```bash
cd agent/code && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target and fix before retrying.

</requirements>

<constraints>
- **Precondition:** `lib/healthcheck/` package and `lib.TaskTypeHealthcheck` must exist. If absent, report `"status":"failed"`.
- `CreateAgentForTaskType` for agent-code takes ONLY `ctx context.Context` and `taskType agentlib.TaskType` — no runner, no parser, no config parameters. The Nop step requires no dependencies.
- The switch has exactly two branches: `case agentlib.TaskTypeHealthcheck:` and `default:`.
- The default branch error must contain the literal string `unknown task_type`.
- `CreateAgent` is NOT renamed, NOT removed, NOT modified. It stays callable by `cmd/run-task/main.go`.
- `agent/code/cmd/run-task/main.go` is NOT modified.
- No claudelib or gemini-related imports are added to the code factory.
- This prompt owns the third CHANGELOG bullet: `feat(agent/{claude,gemini,code}): per-task-type dispatch...`. Prompts 2 and 3 do NOT add CHANGELOG entries — check that the CHANGELOG does NOT have duplicate bullets for claude or gemini before appending.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd agent/code && make precommit` must exit 0.
</constraints>

<verification>

Verify precondition:
```bash
ls lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-nop-step.go
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
Expected: both files present; constant defined.

Verify `CreateAgentForTaskType` added:
```bash
grep -n "func CreateAgentForTaskType" agent/code/pkg/factory/factory.go
```
Expected: one match.

Verify `CreateAgent` still present:
```bash
grep -n "func CreateAgent\b" agent/code/pkg/factory/factory.go
```
Expected: one match.

Verify switch has healthcheck case and default only:
```bash
grep -n "TaskTypeHealthcheck\|unknown task_type" agent/code/pkg/factory/factory.go
```
Expected: both strings present.

Verify main.go updated:
```bash
grep -n "CreateAgentForTaskType\|factory\.CreateAgent\b" agent/code/main.go
```
Expected: `CreateAgentForTaskType` present; `factory.CreateAgent(` absent.

Verify run-task unchanged:
```bash
grep -n "CreateAgent\|CreateAgentForTaskType" agent/code/cmd/run-task/main.go
```
Expected: `CreateAgent` present; `CreateAgentForTaskType` absent.

Build all:
```bash
cd agent/code && go build ./...
```
Expected: exit 0.

Run tests:
```bash
cd agent/code && go test ./...
```
Expected: exit 0.

Verify CHANGELOG has the third bullet:
```bash
grep "agent.*claude.*gemini.*code\|feat(agent" CHANGELOG.md | head -5
```
Expected: one bullet matching `feat(agent/{claude,gemini,code})`.

Run precommit:
```bash
cd agent/code && make precommit
```
Expected: exit 0.

</verification>
