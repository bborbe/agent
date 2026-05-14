---
status: completed
summary: 'Deleted CreateAgentForTaskType and CreateDeliverer from agent/code factory.go, added CreateAgentProvider (healthcheck-only, pure-Go), rewrote main.go Run to build deliverer inline and use provider.Get, rewrote factory_test.go as DescribeTable assertions, updated CHANGELOG.md under ## Unreleased.'
container: agent-129-agent-code-provider
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T14:00:00Z"
queued: "2026-05-14T14:14:13Z"
started: "2026-05-14T14:20:59Z"
completed: "2026-05-14T14:23:36Z"
branch: refactor/agent-code-provider
---

<summary>
- `agent/code/pkg/factory/factory.go` drops `CreateAgentForTaskType` (switch + error) and gains `CreateAgentProvider` returning `lib.AgentProvider`.
- `CreateDeliverer` is deleted. Boot-time deliverer construction moves to `main.go Run`.
- `agent/code/main.go` is rewritten to build the deliverer inline, call `factory.CreateAgentProvider()`, then `provider.Get(ctx, taskType)`.
- `factory_test.go` is rewritten as map-entry assertions.
- `make precommit` passes in `agent/code/`.
- `cmd/run-task/main.go` is UNCHANGED.
</summary>

<objective>
Apply the proven pattern from prompts 2 (agent-claude) and 3 (agent-gemini) to agent-code. Spec assumption: agent-code is a healthcheck-only binary — no domain task type, no LLM dependencies. The provider's dispatch table contains exactly one entry: `TaskTypeHealthcheck` → liveness agent built with `healthcheck.NewNopStep()`. The Nop step returns `done` immediately — reaching it proves the binary booted, envconfig parsed, Kafka client opened.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `~/Documents/workspaces/coding/docs/go-factory-pattern.md` — section 5, 6.2, 7
- `~/Documents/workspaces/coding/docs/go-patterns.md`
- `~/Documents/workspaces/coding/docs/go-testing-guide.md`
- `~/Documents/workspaces/coding/docs/go-error-wrapping-guide.md`

**Precondition check:**
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches. STOP if absent.

**Key files to read in full:**
- `agent/code/pkg/factory/factory.go` — current shape (smallest of the 3 binaries)
- `agent/code/main.go` — current `Run`
- `agent/code/cmd/run-task/main.go` — confirm it calls `factory.CreateAgent()`
- `agent/code/pkg/factory/factory_test.go` — existing tests to rewrite
- `lib/healthcheck/healthcheck-nop-step.go` — `NewNopStep() Step` (no dependencies)
- `lib/agent_agent-provider.go` — the new interface

**Current factory.go shape:**
- `CreateSyncProducer` — keep (pass-through)
- `CreateKafkaResultDeliverer` — keep (pure wiring)
- `CreateFileResultDeliverer` — keep (pure wiring)
- `CreateAgent()` — KEEP (used by cmd/run-task; no args — agent-code is fully pure-Go)
- `CreateAgentForTaskType` — DELETE
- `CreateDeliverer` — DELETE

**New addition:**
```go
// CreateAgentProvider wires the per-task-type dispatch for agent-code.
// Healthcheck-only binary, pure-Go (no LLM): TaskTypeHealthcheck routes to a
// Nop liveness agent — reaching it proves binary booted, envconfig parsed,
// Kafka client opened. Any other task_type hits the default-error branch.
func CreateAgentProvider() agentlib.AgentProvider {
	livenessAgent := healthcheck.NewAgent(healthcheck.NewNopStep())
	return agentlib.NewAgentProvider("agent-code", map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeHealthcheck: livenessAgent,
	})
}
```

No runner, no parser — `NewNopStep()` takes nothing. Provider has exactly one map entry.

**New main.go Run shape (excerpt):**
```go
deliverer := delivery.NewNoopResultDeliverer()
if a.TaskID != "" {
    if len(a.KafkaBrokers) == 0 {
        jobMetrics.RecordRun(agentlib.AgentStatusFailed)
        jobMetrics.RecordDuration(time.Since(start))
        return errors.Errorf(ctx, "KAFKA_BROKERS must be set when TASK_ID is set")
    }
    syncProducer, err := factory.CreateSyncProducer(ctx, a.KafkaBrokers)
    if err != nil {
        jobMetrics.RecordRun(agentlib.AgentStatusFailed)
        jobMetrics.RecordDuration(time.Since(start))
        return errors.Wrap(ctx, err, "create sync producer")
    }
    defer func() {
        if err := syncProducer.Close(); err != nil {
            glog.Warningf("close sync producer failed: %v", err)
        }
    }()
    deliverer = factory.CreateKafkaResultDeliverer(
        syncProducer, a.Branch, a.TaskID, a.TaskContent,
        libtime.NewCurrentDateTime(),
    )
}

provider := factory.CreateAgentProvider()
agent, err := provider.Get(ctx, agentlib.TaskType(a.TaskType))
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "select agent for task_type")
}

result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
// ... existing post-Run block stays ...
```

Imports added: `delivery "github.com/bborbe/agent/lib/delivery"`.
</context>

<requirements>

## 1. Verify precondition

```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches. STOP if absent.

## 2. Delete `CreateAgentForTaskType` and `CreateDeliverer`

Read `agent/code/pkg/factory/factory.go` in full. Remove both functions entirely.

Verify:
```bash
grep -nE "CreateAgentForTaskType|^func CreateDeliverer" agent/code/pkg/factory/factory.go
```
Expected: zero matches.

After deleting `CreateDeliverer`, the `glog` import in `factory.go` becomes unused. Explicitly remove the `"github.com/golang/glog"` line. Verify:
```bash
grep -n "glog" agent/code/pkg/factory/factory.go
```
Expected: zero matches.

## 3. Add `CreateAgentProvider`

Add the new function (signature in `<context>`) at the bottom of factory.go.

Verify:
```bash
grep -n "^func CreateAgentProvider" agent/code/pkg/factory/factory.go
```
Expected: 1 match.

The `healthcheck` import is already present (used by the deleted `CreateAgentForTaskType`):
```bash
grep -n "lib/healthcheck" agent/code/pkg/factory/factory.go
```
Expected: at least 1 match.

## 4. Rewrite `agent/code/main.go` Run

Read the current `Run` in full. Replace the deliverer-construction + agent-construction block with the new shape from `<context>`. Keep everything from `result, err := agent.Run(...)` onward unchanged.

Add the import:
```go
delivery "github.com/bborbe/agent/lib/delivery"
```

Verify:
```bash
grep -nE "factory\.CreateAgentProvider|provider\.Get|delivery\.NewNoopResultDeliverer" agent/code/main.go
```
Expected: all 3 present.

```bash
grep -nE "factory\.CreateDeliverer|factory\.CreateAgent\(" agent/code/main.go
```
Expected: zero matches.

Build:
```bash
cd agent/code && go build ./...
```
Expected: exit 0.

## 5. Confirm `cmd/run-task/main.go` unchanged

```bash
grep -nE "factory\.CreateAgent\(|factory\.CreateAgentProvider" agent/code/cmd/run-task/main.go
```
Expected: `factory.CreateAgent(` present; `CreateAgentProvider` absent.

## 6. Rewrite `agent/code/pkg/factory/factory_test.go`

**Test scaffold:**
```go
package factory_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/agent/code/pkg/factory"
	agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("CreateAgentProvider", func() {
	var (
		ctx      context.Context
		provider agentlib.AgentProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		provider = factory.CreateAgentProvider()
	})

	It("returns a non-nil provider", func() {
		Expect(provider).NotTo(BeNil())
	})

	It("Get returns the liveness agent for TaskTypeHealthcheck", func() {
		agent, err := provider.Get(ctx, agentlib.TaskTypeHealthcheck)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	Describe("Get with unknown task_type", func() {
		DescribeTable("error shape",
			func(taskType agentlib.TaskType, expectedSubstr string) {
				agent, err := provider.Get(ctx, taskType)
				Expect(agent).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unknown task_type"))
				Expect(err.Error()).To(ContainSubstring(expectedSubstr))
				Expect(err.Error()).To(ContainSubstring("agent-code"))
				Expect(err.Error()).To(ContainSubstring("[healthcheck]"))
			},
			Entry("literal code rejected", agentlib.TaskType("code"), `"code"`),
			Entry("bogus value", agentlib.TaskType("bogus"), `"bogus"`),
			Entry("empty value", agentlib.TaskType(""), `""`),
		)
	})
})
```

Run tests:
```bash
cd agent/code && go test ./pkg/factory/...
```
Expected: exit 0.

## 7. Update root `CHANGELOG.md`

Append to `## Unreleased`:
```markdown
- refactor(agent/code): factory.go is pure plumbing — `CreateAgentForTaskType` and `CreateDeliverer` removed; new `CreateAgentProvider` returns lib.AgentProvider (healthcheck-only binary); boot-time deliverer construction moved to main.go Run
```

Verify:
```bash
grep -n "agent/code.*CreateAgentProvider" CHANGELOG.md
```
Expected: at least 1 match.

## 8. Run final precommit

```bash
cd agent/code && make precommit
```
Must exit 0.

</requirements>

<constraints>
- Healthcheck-only binary: provider map contains exactly one entry (`TaskTypeHealthcheck`). The literal `"code"` MUST fall into the default-error branch.
- `cmd/run-task/main.go` is FROZEN.
- `factory.go` after this prompt contains: `CreateSyncProducer`, `CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgent`, `CreateAgentProvider`. No `CreateAgentForTaskType`, no `CreateDeliverer`.
- `CreateAgent()` takes no args (agent-code has no LLM deps).
- Error format on dispatch miss is owned by `lib.AgentProvider.Get`. Sorted accepted list is `[healthcheck]`.
- Error wrapping uses `github.com/bborbe/errors`. Never `fmt.Errorf`.
- Do NOT commit.
- `cd agent/code && make precommit` must exit 0.
</constraints>

<verification>

Precondition:
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches.

factory.go shape:
```bash
grep -nE "^func " agent/code/pkg/factory/factory.go
```
Expected: `CreateSyncProducer`, `CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgent`, `CreateAgentProvider`. No `CreateAgentForTaskType`, no `CreateDeliverer`.

main.go uses new shape:
```bash
grep -nE "factory\.CreateAgentProvider|provider\.Get|delivery\.NewNoopResultDeliverer" agent/code/main.go
```
Expected: all 3 present.

cmd/run-task unchanged:
```bash
grep -nE "factory\.CreateAgent\(|factory\.CreateAgentProvider" agent/code/cmd/run-task/main.go
```
Expected: `factory.CreateAgent(` present; `CreateAgentProvider` absent.

All tests pass:
```bash
cd agent/code && go test ./...
```
Expected: exit 0.

CHANGELOG:
```bash
grep -n "agent/code.*CreateAgentProvider" CHANGELOG.md
```
Expected: at least 1 match.

Precommit:
```bash
cd agent/code && make precommit
```
Expected: exit 0.

</verification>
