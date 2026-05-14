---
status: draft
spec: [031-agent-repo-task-type-dispatch]
created: "2026-05-14T13:00:00Z"
branch: dark-factory/agent-repo-task-type-dispatch
---

<summary>
- `agent/gemini/pkg/factory/factory.go` gains a new `CreateAgentForTaskType` function that dispatches on `lib.TaskType`
- `TaskTypeHealthcheck` routes to `healthcheck.NewAgent(healthcheck.NewGeminiStep(geminiParser))` — the healthcheck step calls the Gemini AIParser with a smoke prompt
- All other `task_type` values (including a future `"gemini"` literal) hit the default-error branch with a wrapped error containing `unknown task_type` and the accepted list `[healthcheck]`
- `agent/gemini/main.go` replaces the direct `factory.CreateAgent(geminiParser).Run(...)` call with an error-aware `factory.CreateAgentForTaskType(ctx, agentlib.TaskType(a.TaskType), geminiParser)` call whose error branch records metrics
- Factory tests are added: Ginkgo suite + dispatch assertions for both branches (healthcheck → non-nil agent; unknown → nil agent + error)
- `make precommit` passes in `agent/gemini/`
</summary>

<objective>
Add per-task-type dispatch to `agent-gemini`, making it a healthcheck-only binary. The existing `CreateAgent` function and `cmd/run-task/main.go` remain unchanged.
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
ls lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-gemini-step.go
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
If either check fails, STOP. Prompt 1 (`1-spec-031-lib-healthcheck-package.md`) must complete successfully before this prompt runs. Report `"status":"failed"` with reason `"lib/healthcheck package not yet deployed (prompt 1)"`.

**Key files to read in full before editing:**
- `agent/gemini/pkg/factory/factory.go` — existing `CreateAgent(geminiParser agentlib.AIParser) *agentlib.Agent` signature; `CreateGeminiParser` and `CreateDeliverer` functions
- `agent/gemini/main.go` — current `Run()` body with `factory.CreateAgent(geminiParser).Run(...)` call
- `agent/gemini/cmd/run-task/main.go` — MUST remain unchanged
- `lib/agent_task-type.go` — `TaskTypeHealthcheck` constant value
- `lib/healthcheck/healthcheck-agent.go` — `healthcheck.NewAgent(step) *agentlib.Agent`
- `lib/healthcheck/healthcheck-gemini-step.go` — `healthcheck.NewGeminiStep(parser agentlib.AIParser) agentlib.Step`
- `lib/agent_parser.go` — `AIParser` interface (parameter type for `CreateAgentForTaskType`)

**Inline reference — current `CreateAgent` signature (do NOT change):**
```go
func CreateAgent(geminiParser agentlib.AIParser) *agentlib.Agent
```

**Inline reference — new `CreateAgentForTaskType` signature:**
```go
func CreateAgentForTaskType(
    ctx context.Context,
    taskType agentlib.TaskType,
    geminiParser agentlib.AIParser,
) (*agentlib.Agent, error)
```

**Inline reference — switch body (healthcheck-only binary — no domain task type):**
```go
switch taskType {
case agentlib.TaskTypeHealthcheck:
    return healthcheck.NewAgent(healthcheck.NewGeminiStep(geminiParser)), nil
default:
    return nil, errors.Errorf(ctx, "unknown task_type %q for agent-gemini; accepted: [%s]",
        taskType, agentlib.TaskTypeHealthcheck)
}
```

**Inline reference — current `agent/gemini/main.go` agent call (after deliverer creation):**
```go
result, err := factory.CreateAgent(geminiParser).Run(ctx, a.Phase, a.TaskContent, deliverer)
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "agent run failed")
}
```

**Inline reference — replacement block for `agent/gemini/main.go`:**
```go
agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskType(a.TaskType), geminiParser)
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
grep -n "^func New" lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-gemini-step.go

# Confirm agentlib alias in gemini main.go
grep -n "bborbe/agent/lib\"" agent/gemini/main.go

# Confirm existing factory structure
grep -n "func Create\|func.*Parser" agent/gemini/pkg/factory/factory.go
```
</context>

<requirements>

## 1. Add `CreateAgentForTaskType` to `agent/gemini/pkg/factory/factory.go`

Read the full file before editing.

Add the `healthcheck` import to the import block:
```go
healthcheck "github.com/bborbe/agent/lib/healthcheck"
```

Add the new function immediately after the existing `CreateAgent` function. Use the exact signature and switch body from `<context>`.

Key constraints:
- Only ONE case: `TaskTypeHealthcheck`. No `TaskTypeGemini` case — it does not exist as a constant, and any gemini literal hits the default branch.
- The default branch error must contain the literal string `unknown task_type`.
- `CreateAgent` is NOT renamed, NOT removed, NOT modified.
- No claudelib import is added to this factory — the gemini binary does not depend on claudelib.

Verify:
```bash
grep -n "func CreateAgentForTaskType\|func CreateAgent\b" agent/gemini/pkg/factory/factory.go
```
Expected: two matches.

Build check:
```bash
cd agent/gemini && go build ./...
```
Expected: exit 0.

## 2. Update `agent/gemini/main.go`

Read the full file before editing.

Replace the single-line `factory.CreateAgent(geminiParser).Run(...)` block and its error handler with the replacement block from `<context>`. The replacement:
1. Calls `factory.CreateAgentForTaskType(ctx, agentlib.TaskType(a.TaskType), geminiParser)` — this can fail
2. Records metrics on the new error path
3. Calls `agent.Run(...)` on the returned agent — this can also fail (existing pattern)

The `agentlib` alias is already present in the import block as `agentlib "github.com/bborbe/agent/lib"`. No new imports are needed.

Verify:
```bash
grep -n "CreateAgentForTaskType\|factory\.CreateAgent\b" agent/gemini/main.go
```
Expected: `CreateAgentForTaskType` present; `factory.CreateAgent(` absent.

Verify `cmd/run-task/main.go` unchanged:
```bash
grep -n "CreateAgent\|CreateAgentForTaskType" agent/gemini/cmd/run-task/main.go
```
Expected: `CreateAgent` or similar present; `CreateAgentForTaskType` absent.

Build check:
```bash
cd agent/gemini && go build ./...
```
Expected: exit 0.

## 3. Create `agent/gemini/pkg/factory/factory_suite_test.go`

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

## 4. Create `agent/gemini/pkg/factory/factory_test.go`

New file. License header required. Package: `factory_test`.

For testing `CreateAgentForTaskType`, pass a nil `geminiParser` — the function only dispatches; it never calls Parse. A nil `agentlib.AIParser` is safe to pass to `healthcheck.NewGeminiStep` at construction time (the step only calls Parse when Run is invoked).

```go
package factory_test

import (
    "context"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/bborbe/agent/agent/gemini/pkg/factory"
    agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("CreateAgentForTaskType", func() {
    var ctx context.Context

    BeforeEach(func() {
        ctx = context.Background()
    })

    It("returns a non-nil agent for TaskTypeHealthcheck", func() {
        agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskTypeHealthcheck, nil)
        Expect(err).To(BeNil())
        Expect(agent).NotTo(BeNil())
    })

    It("returns nil agent and error for an unsupported task type", func() {
        agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskType("bogus"), nil)
        Expect(err).NotTo(BeNil())
        Expect(agent).To(BeNil())
        Expect(err.Error()).To(ContainSubstring("unknown task_type"))
        Expect(err.Error()).To(ContainSubstring("bogus"))
    })

    It("returns nil agent and error for the gemini literal string (not a known constant)", func() {
        agent, err := factory.CreateAgentForTaskType(ctx, agentlib.TaskType("gemini"), nil)
        Expect(err).NotTo(BeNil())
        Expect(agent).To(BeNil())
        Expect(err.Error()).To(ContainSubstring("unknown task_type"))
    })
})
```

## 5. Run iterative tests

```bash
cd agent/gemini && go test ./...
```
Expected: exit 0.

Coverage check:
```bash
cd agent/gemini && go test -coverprofile=/tmp/gemini-factory-cover.out ./pkg/factory/... && \
  go tool cover -func=/tmp/gemini-factory-cover.out | grep -E "factory|total"
```
Expected: `factory.go` coverage ≥80%.

## 6. Run final precommit in `agent/gemini/`

```bash
cd agent/gemini && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target and fix before retrying.

</requirements>

<constraints>
- **Precondition:** `lib/healthcheck/` package and `lib.TaskTypeHealthcheck` must exist. If absent, report `"status":"failed"`.
- `CreateAgentForTaskType` for agent-gemini accepts ONLY `TaskTypeHealthcheck` as a valid value. `TaskTypeGemini` does NOT exist as a constant — any value including the literal string `"gemini"` hits the default-error branch.
- `CreateAgent` is NOT renamed, NOT removed, NOT modified. It stays available for `cmd/run-task/main.go`.
- The switch has exactly two branches: `case agentlib.TaskTypeHealthcheck:` and `default:`.
- The default branch error must contain the literal string `unknown task_type`.
- No claudelib dependency is added to `agent/gemini/pkg/factory/factory.go`. The healthcheck package handles the AIParser abstraction — no new import of `lib/claude` in gemini factory.
- `agent/gemini/cmd/run-task/main.go` is NOT modified.
- No CHANGELOG entry — the combined `feat(agent/{claude,gemini,code})` bullet is owned by prompt 4.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd agent/gemini && make precommit` must exit 0.
</constraints>

<verification>

Verify precondition:
```bash
ls lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-gemini-step.go
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
Expected: both files present; constant defined.

Verify `CreateAgentForTaskType` added:
```bash
grep -n "func CreateAgentForTaskType" agent/gemini/pkg/factory/factory.go
```
Expected: one match.

Verify `CreateAgent` still present:
```bash
grep -n "func CreateAgent\b" agent/gemini/pkg/factory/factory.go
```
Expected: one match.

Verify switch has healthcheck case and default only:
```bash
grep -n "TaskTypeHealthcheck\|unknown task_type" agent/gemini/pkg/factory/factory.go
```
Expected: both strings present; no `TaskTypeClaude` or `TaskTypeOAuthProbe`.

Verify main.go updated:
```bash
grep -n "CreateAgentForTaskType\|factory\.CreateAgent\b" agent/gemini/main.go
```
Expected: `CreateAgentForTaskType` present; `factory.CreateAgent(` absent.

Verify run-task unchanged:
```bash
grep -n "CreateAgent\|CreateAgentForTaskType" agent/gemini/cmd/run-task/main.go
```
Expected: `CreateAgent` present; `CreateAgentForTaskType` absent.

Build all:
```bash
cd agent/gemini && go build ./...
```
Expected: exit 0.

Run tests:
```bash
cd agent/gemini && go test ./...
```
Expected: exit 0.

Run precommit:
```bash
cd agent/gemini && make precommit
```
Expected: exit 0.

</verification>
