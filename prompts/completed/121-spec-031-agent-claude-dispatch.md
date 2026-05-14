---
status: completed
spec: [031-agent-repo-task-type-dispatch]
summary: Added CreateAgentForTaskType dispatch to agent/claude factory — routes healthcheck/oauth-probe to liveness agent and claude to 3-phase domain agent; updated main.go to use it; added Ginkgo test suite covering all four dispatch branches.
container: agent-121-spec-031-agent-claude-dispatch
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T13:00:00Z"
queued: "2026-05-14T12:54:19Z"
started: "2026-05-14T12:54:20Z"
completed: "2026-05-14T12:56:57Z"
branch: dark-factory/agent-repo-task-type-dispatch
---

<summary>
- `agent/claude/pkg/factory/factory.go` gains a new `CreateAgentForTaskType` function that dispatches on `lib.TaskType` to select which `*agentlib.Agent` to construct
- `TaskTypeClaude` routes to the existing 3-phase domain agent (calls `CreateAgent` internally)
- `TaskTypeHealthcheck` and `TaskTypeOAuthProbe` (fall-through case) both route to `healthcheck.NewAgent(healthcheck.NewClaudeStep(runner))` using the same `CreateClaudeRunner` factory
- Any other `task_type` value returns a wrapped error containing the literal phrase `unknown task_type`, the offending value, and the list of accepted types
- `agent/claude/main.go` replaces the direct `factory.CreateAgent(...)` call with `factory.CreateAgentForTaskType(ctx, agentlib.TaskType(a.TaskType), ...)`, whose error branch records metrics and returns a wrapped error
- Factory tests are added: Ginkgo suite + dispatch assertions for all four branches
- `make precommit` passes in `agent/claude/`
</summary>

<objective>
Add per-task-type dispatch to `agent-claude` so that `healthcheck` and `oauth-probe` tasks route to a dedicated liveness agent instead of burning Claude API minutes on the full 3-phase domain prompt. The existing `CreateAgent` function and its callers (`cmd/run-task/main.go`) remain unchanged.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — factory pattern, error wrapping
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `Create*` prefix, zero logic, constructor rules
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`

**Precondition check — run before implementing:**
```bash
ls lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-claude-step.go
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
If either check fails, STOP. Prompt 1 (`1-spec-031-lib-healthcheck-package.md`) must complete successfully before this prompt runs. Report `"status":"failed"` with reason `"lib/healthcheck package not yet deployed (prompt 1)"`.

**Key files to read in full before editing:**
- `agent/claude/pkg/factory/factory.go` — existing `CreateAgent` and `CreateClaudeRunner` signatures to reuse
- `agent/claude/main.go` — current `Run()` body with `factory.CreateAgent(...)` call; this is the only call site to update
- `agent/claude/cmd/run-task/main.go` — MUST remain unchanged; verify after editing
- `lib/agent_task-type.go` — `TaskTypeClaude`, `TaskTypeHealthcheck`, `TaskTypeOAuthProbe` constants
- `lib/healthcheck/healthcheck-agent.go` — `healthcheck.NewAgent(step agentlib.Step) *agentlib.Agent`
- `lib/healthcheck/healthcheck-claude-step.go` — `healthcheck.NewClaudeStep(runner claudelib.ClaudeRunner) agentlib.Step`

**Inline reference — current `CreateAgent` signature (do NOT change):**
```go
func CreateAgent(
    claudeConfigDir claudelib.ClaudeConfigDir,
    agentDir claudelib.AgentDir,
    allowedTools claudelib.AllowedTools,
    model claudelib.ClaudeModel,
    claudeEnv map[string]string,
    envContext map[string]string,
) *agentlib.Agent
```

**Inline reference — new `CreateAgentForTaskType` signature:**
```go
func CreateAgentForTaskType(
    ctx context.Context,
    taskType agentlib.TaskType,
    claudeConfigDir claudelib.ClaudeConfigDir,
    agentDir claudelib.AgentDir,
    allowedTools claudelib.AllowedTools,
    model claudelib.ClaudeModel,
    claudeEnv map[string]string,
    envContext map[string]string,
) (*agentlib.Agent, error)
```

**Inline reference — switch body:**
```go
switch taskType {
case agentlib.TaskTypeClaude:
    return CreateAgent(claudeConfigDir, agentDir, allowedTools, model, claudeEnv, envContext), nil
case agentlib.TaskTypeHealthcheck, agentlib.TaskTypeOAuthProbe:
    runner := CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv)
    return healthcheck.NewAgent(healthcheck.NewClaudeStep(runner)), nil
default:
    return nil, errors.Errorf(ctx, "unknown task_type %q for agent-claude; accepted: [%s %s %s]",
        taskType, agentlib.TaskTypeClaude, agentlib.TaskTypeHealthcheck, agentlib.TaskTypeOAuthProbe)
}
```

**Inline reference — current `factory.CreateAgent(...)` block in `agent/claude/main.go`:**
```go
agent := factory.CreateAgent(
    a.ClaudeConfigDir,
    a.AgentDir,
    claudelib.ParseAllowedTools(a.AllowedToolsRaw),
    a.Model,
    claudelib.ParseKeyValuePairs(a.ClaudeEnvRaw),
    claudelib.ParseKeyValuePairs(a.EnvContextRaw),
)

result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "agent run failed")
}
```

**Inline reference — replacement block for `agent/claude/main.go`:**
```go
agent, err := factory.CreateAgentForTaskType(
    ctx,
    agentlib.TaskType(a.TaskType),
    a.ClaudeConfigDir,
    a.AgentDir,
    claudelib.ParseAllowedTools(a.AllowedToolsRaw),
    a.Model,
    claudelib.ParseKeyValuePairs(a.ClaudeEnvRaw),
    claudelib.ParseKeyValuePairs(a.EnvContextRaw),
)
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

**Symbol verification — run before writing any code:**
```bash
# Confirm healthcheck package exports
grep -n "^func New" lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-claude-step.go

# Confirm task type constants
grep -n "TaskTypeClaude\|TaskTypeHealthcheck\|TaskTypeOAuthProbe" lib/agent_task-type.go

# Confirm existing CreateClaudeRunner in factory
grep -n "func CreateClaudeRunner\|func CreateAgent\b" agent/claude/pkg/factory/factory.go

# Confirm agentlib alias in main.go
grep -n "bborbe/agent/lib\"" agent/claude/main.go
```
</context>

<requirements>

## 1. Add `CreateAgentForTaskType` to `agent/claude/pkg/factory/factory.go`

Read the full file before editing.

Add the `healthcheck` import to the import block:
```go
healthcheck "github.com/bborbe/agent/lib/healthcheck"
```

Add the new function immediately after the existing `CreateAgent` function. The function signature, switch body, and error message format must match the inline references in `<context>` exactly. Key points:
- `TaskTypeHealthcheck` and `TaskTypeOAuthProbe` share a single `case` line (fall-through)
- The `default` branch error message must contain the literal phrase `unknown task_type`
- The runner used in the healthcheck branch is created via `CreateClaudeRunner` (same factory as domain branch) — single runner for all branches
- `CreateAgent` is NOT renamed — it stays callable by `cmd/run-task/main.go`

Verify after editing:
```bash
grep -n "func CreateAgentForTaskType\|func CreateAgent\b" agent/claude/pkg/factory/factory.go
```
Expected: two matches — both functions present.

Build check:
```bash
cd agent/claude && go build ./...
```
Expected: exit 0.

## 2. Update `agent/claude/main.go`

Read the full file before editing.

Replace the existing `factory.CreateAgent(...)` call and the following `agent.Run(...)` block with the replacement block from `<context>`.

The replacement adds a new error path for `CreateAgentForTaskType` that records metrics before returning — matching the existing `CreateDeliverer` error pattern.

**Do NOT change** the metrics init block, the `CreateDeliverer` call, or the `agentlib.PrintResult(result)` success path.

Verify the change:
```bash
grep -n "CreateAgentForTaskType\|CreateAgent\b" agent/claude/main.go
```
Expected: `CreateAgentForTaskType` present; plain `CreateAgent` absent from main.go (it's only in factory.go now).

Verify `cmd/run-task/main.go` is unchanged:
```bash
grep -n "CreateAgent\|CreateAgentForTaskType" agent/claude/cmd/run-task/main.go
```
Expected: `CreateAgent` still present, `CreateAgentForTaskType` absent.

Build check:
```bash
cd agent/claude && go build ./...
```
Expected: exit 0.

## 3. Create `agent/claude/pkg/factory/factory_suite_test.go`

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

## 4. Create `agent/claude/pkg/factory/factory_test.go`

New file. License header required. Package: `factory_test`.

Tests cover all four dispatch branches for `CreateAgentForTaskType`. Pass zero/nil values for config parameters — no runner behavior is exercised, only dispatch wiring.

```go
package factory_test

import (
    "context"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/bborbe/agent/agent/claude/pkg/factory"
    agentlib "github.com/bborbe/agent/lib"
    claudelib "github.com/bborbe/agent/lib/claude"
)

var _ = Describe("CreateAgentForTaskType", func() {
    var ctx context.Context

    BeforeEach(func() {
        ctx = context.Background()
    })

    It("returns a non-nil agent for TaskTypeClaude", func() {
        agent, err := factory.CreateAgentForTaskType(
            ctx,
            agentlib.TaskTypeClaude,
            claudelib.ClaudeConfigDir(""),
            claudelib.AgentDir(""),
            nil,
            claudelib.ClaudeModel(""),
            nil,
            nil,
        )
        Expect(err).To(BeNil())
        Expect(agent).NotTo(BeNil())
    })

    It("returns a non-nil agent for TaskTypeHealthcheck", func() {
        agent, err := factory.CreateAgentForTaskType(
            ctx,
            agentlib.TaskTypeHealthcheck,
            claudelib.ClaudeConfigDir(""),
            claudelib.AgentDir(""),
            nil,
            claudelib.ClaudeModel(""),
            nil,
            nil,
        )
        Expect(err).To(BeNil())
        Expect(agent).NotTo(BeNil())
    })

    It("returns a non-nil agent for TaskTypeOAuthProbe (alias to healthcheck)", func() {
        agent, err := factory.CreateAgentForTaskType(
            ctx,
            agentlib.TaskTypeOAuthProbe,
            claudelib.ClaudeConfigDir(""),
            claudelib.AgentDir(""),
            nil,
            claudelib.ClaudeModel(""),
            nil,
            nil,
        )
        Expect(err).To(BeNil())
        Expect(agent).NotTo(BeNil())
    })

    It("returns nil agent and error for an unsupported task type", func() {
        agent, err := factory.CreateAgentForTaskType(
            ctx,
            agentlib.TaskType("bogus"),
            claudelib.ClaudeConfigDir(""),
            claudelib.AgentDir(""),
            nil,
            claudelib.ClaudeModel(""),
            nil,
            nil,
        )
        Expect(err).NotTo(BeNil())
        Expect(agent).To(BeNil())
        Expect(err.Error()).To(ContainSubstring("unknown task_type"))
        Expect(err.Error()).To(ContainSubstring("bogus"))
    })
})
```

## 5. Run iterative tests

```bash
cd agent/claude && go test ./...
```
Expected: exit 0.

Coverage check for the factory package:
```bash
cd agent/claude && go test -coverprofile=/tmp/claude-factory-cover.out ./pkg/factory/... && \
  go tool cover -func=/tmp/claude-factory-cover.out | grep -E "factory|total"
```
Expected: `factory.go` coverage ≥80%.

## 6. Run final precommit in `agent/claude/`

```bash
cd agent/claude && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

</requirements>

<constraints>
- **Precondition:** `lib/healthcheck/` package and `lib.TaskTypeHealthcheck` constant must exist (created by prompt 1). If absent, report `"status":"failed"`.
- `CreateAgentForTaskType` is a NEW function added alongside `CreateAgent`. `CreateAgent` is NOT renamed, NOT removed, NOT modified. Its signature and return type (`*agentlib.Agent`, not `(*agentlib.Agent, error)`) remain unchanged.
- The switch in `CreateAgentForTaskType` has exactly three `case` lines: `TaskTypeClaude`, `TaskTypeHealthcheck, TaskTypeOAuthProbe` (single line, fall-through), and `default`.
- The error message from the `default` branch MUST contain the literal string `unknown task_type` (for upstream log matching per spec item 13).
- The runner for the healthcheck branch is created with `CreateClaudeRunner(...)` — the same factory function used by `CreateAgent` internally. Single runner instance; do not inline the runner construction.
- In `agent/claude/main.go`: the `agentlib` alias is already `agentlib "github.com/bborbe/agent/lib"`, so `agentlib.TaskType(a.TaskType)` is correct. Do NOT add a second import for lib.
- The new error path in `main.go` for `CreateAgentForTaskType` records `jobMetrics.RecordRun(agentlib.AgentStatusFailed)` THEN `jobMetrics.RecordDuration(time.Since(start))` — same order as the existing `CreateDeliverer` error path.
- `agent/claude/cmd/run-task/main.go` is NOT modified. After editing, verify it still compiles with `cd agent/claude && go build ./cmd/run-task/...`.
- No new imports in `main.go` — `agentlib` alias already covers both `agentlib.TaskType(...)` and the existing `agentlib.AgentStatusFailed`.
- No CHANGELOG entry — this prompt does not own a changelog bullet (the combined `feat(agent/{claude,gemini,code})` bullet is owned by prompt 4).
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`. The `errors.Errorf(ctx, ...)` call in the default branch is correct (creates a new error with context).
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd agent/claude && make precommit` must exit 0.
</constraints>

<verification>

Verify precondition:
```bash
ls lib/healthcheck/healthcheck-agent.go lib/healthcheck/healthcheck-claude-step.go
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
Expected: both files present; constant defined.

Verify `CreateAgentForTaskType` added:
```bash
grep -n "func CreateAgentForTaskType" agent/claude/pkg/factory/factory.go
```
Expected: one match.

Verify `CreateAgent` still present (unchanged):
```bash
grep -n "func CreateAgent\b" agent/claude/pkg/factory/factory.go
```
Expected: one match.

Verify switch branches:
```bash
grep -n "TaskTypeClaude\|TaskTypeHealthcheck\|TaskTypeOAuthProbe\|unknown task_type" agent/claude/pkg/factory/factory.go
```
Expected: all four strings present.

Verify main.go uses `CreateAgentForTaskType`:
```bash
grep -n "CreateAgentForTaskType\|factory\.CreateAgent\b" agent/claude/main.go
```
Expected: `CreateAgentForTaskType` present; `factory.CreateAgent(` absent.

Verify run-task entry point unchanged:
```bash
grep -n "CreateAgent\|CreateAgentForTaskType" agent/claude/cmd/run-task/main.go
```
Expected: `CreateAgent` or similar present; `CreateAgentForTaskType` absent.

Build all:
```bash
cd agent/claude && go build ./...
```
Expected: exit 0.

Run tests:
```bash
cd agent/claude && go test ./...
```
Expected: exit 0. Output should include dispatch assertions for all four branches.

Run precommit:
```bash
cd agent/claude && make precommit
```
Expected: exit 0.

</verification>
