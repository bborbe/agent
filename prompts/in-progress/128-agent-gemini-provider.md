---
status: committing
summary: Deleted CreateAgentForTaskType and CreateDeliverer from factory.go, added CreateAgentProvider returning lib.AgentProvider (healthcheck-only), rewrote main.go Run with inline deliverer construction and provider dispatch, rewrote factory_test.go as map-entry assertions on the provider, and make precommit passed with exit 0.
container: agent-128-agent-gemini-provider
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T14:00:00Z"
queued: "2026-05-14T14:14:13Z"
started: "2026-05-14T14:17:37Z"
branch: refactor/agent-gemini-provider
---

<summary>
- `agent/gemini/pkg/factory/factory.go` drops `CreateAgentForTaskType` (switch + error = factory-pattern violations) and gains `CreateAgentProvider` returning `lib.AgentProvider`.
- `CreateDeliverer` is deleted from `factory.go`. Boot-time deliverer construction (noop vs kafka, syncProducer lifecycle) moves to `main.go Run`.
- `agent/gemini/main.go` is rewritten to build the deliverer inline, call `factory.CreateAgentProvider(parser)`, then `provider.Get(ctx, taskType)`.
- `factory_test.go` is rewritten as map-entry assertions on the returned `AgentProvider`.
- `make precommit` passes in `agent/gemini/`.
- `cmd/run-task/main.go` is UNCHANGED.
</summary>

<objective>
Apply the proven pattern from prompt 2 (agent-claude) to agent-gemini. Spec assumption: agent-gemini is a healthcheck-only binary — no domain task type yet (no `TaskTypeGemini` constant exists; Config CR for agent-gemini does not yet exist in this repo). The provider's dispatch table contains exactly one entry: `TaskTypeHealthcheck` → liveness agent built with `healthcheck.NewGeminiStep(parser)`.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `~/Documents/workspaces/coding/docs/go-factory-pattern.md` — section 5 (Provider Pattern), section 6.2 (boot-time validation in main.go), section 7 (pass-through wrappers)
- `~/Documents/workspaces/coding/docs/go-patterns.md`
- `~/Documents/workspaces/coding/docs/go-testing-guide.md`
- `~/Documents/workspaces/coding/docs/go-error-wrapping-guide.md`

**Precondition check:**
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches. STOP if absent.

Sibling reference (prompt 2 should have landed first — recommended but not required):
```bash
grep -n "^func CreateAgentProvider" agent/claude/pkg/factory/factory.go
```
Expected: 1 match. This prompt mirrors that file's shape.

**Key files to read in full:**
- `agent/gemini/pkg/factory/factory.go` — current shape
- `agent/gemini/main.go` — current `Run`
- `agent/gemini/cmd/run-task/main.go` — confirm it calls `factory.CreateAgent`
- `agent/gemini/pkg/factory/factory_test.go` — existing tests to rewrite
- `lib/healthcheck/healthcheck-gemini-step.go` — `NewGeminiStep(parser agentlib.AIParser) Step`
- `lib/agent_agent-provider.go` — the new interface

**Current factory.go shape (verify against actual line numbers):**
- `CreateSyncProducer` — keep (pass-through wrapper, allowed)
- `CreateGeminiParser` — keep (pass-through wrapper, allowed)
- `CreateKafkaResultDeliverer` — keep (pure wiring)
- `CreateFileResultDeliverer` — keep (pure wiring)
- `CreateAgent(parser)` — KEEP (used by cmd/run-task)
- `CreateAgentForTaskType` — DELETE (switch + error)
- `CreateDeliverer` — DELETE (conditionals + error + cleanup closure)

**New addition:**
```go
// CreateAgentProvider wires the per-task-type dispatch for agent-gemini.
// Healthcheck-only binary: TaskTypeHealthcheck routes to the gemini liveness
// agent; any other value hits the default-error branch of lib.AgentProvider.Get.
func CreateAgentProvider(parser agentlib.AIParser) agentlib.AgentProvider {
	livenessAgent := healthcheck.NewAgent(healthcheck.NewGeminiStep(parser))
	return agentlib.NewAgentProvider("agent-gemini", map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeHealthcheck: livenessAgent,
	})
}
```

No domain agent in the map. `CreateAgent` stays available for `cmd/run-task` but is not wired into the provider — Kafka entry will only dispatch healthcheck. If/when a Config CR introduces `agent-gemini` with a domain task type, add the entry then.

**Note on parser construction**: `CreateGeminiParser` returns `(AIParser, error)`. Per guide section 7 this is an allowed pass-through. main.go calls it directly (not the factory) because the error needs to surface at boot.

**New main.go Run shape (excerpt):**
```go
parser, err := factory.CreateGeminiParser(ctx, a.GeminiAPIKey, a.GeminiModel)
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "create gemini parser")
}

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

provider := factory.CreateAgentProvider(parser)
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

## 2. Delete `CreateAgentForTaskType` and `CreateDeliverer` from `factory.go`

Read `agent/gemini/pkg/factory/factory.go` in full. Remove both functions entirely.

Verify:
```bash
grep -nE "CreateAgentForTaskType|^func CreateDeliverer" agent/gemini/pkg/factory/factory.go
```
Expected: zero matches.

After deleting `CreateDeliverer`, the `glog` import in `factory.go` becomes unused. Explicitly remove the `"github.com/golang/glog"` line. Verify:
```bash
grep -n "glog" agent/gemini/pkg/factory/factory.go
```
Expected: zero matches.

## 3. Add `CreateAgentProvider`

Add the new function (signature in `<context>`) at the bottom of factory.go (after the existing `CreateAgent`).

Verify:
```bash
grep -n "^func CreateAgentProvider" agent/gemini/pkg/factory/factory.go
```
Expected: 1 match.

The `healthcheck` import is already present (it was imported for the deleted `CreateAgentForTaskType`). Confirm:
```bash
grep -n "lib/healthcheck" agent/gemini/pkg/factory/factory.go
```
Expected: at least 1 match.

## 4. Rewrite `agent/gemini/main.go` Run

Read the current `Run` in full. Replace the block that:
1. Builds the parser
2. Calls `factory.CreateDeliverer(...)`
3. Calls `factory.CreateAgent(parser)` or `factory.CreateAgentForTaskType(...)`
4. Calls `agent.Run(...)`

…with the new shape in `<context>`. Keep everything from `result, err := agent.Run(...)` onward unchanged.

Add the import:
```go
delivery "github.com/bborbe/agent/lib/delivery"
```

Verify:
```bash
grep -nE "factory\.CreateAgentProvider|provider\.Get|delivery\.NewNoopResultDeliverer" agent/gemini/main.go
```
Expected: all 3 present.

```bash
grep -nE "factory\.CreateDeliverer|factory\.CreateAgent\(" agent/gemini/main.go
```
Expected: zero matches.

Build:
```bash
cd agent/gemini && go build ./...
```
Expected: exit 0.

## 5. Confirm `cmd/run-task/main.go` unchanged

```bash
grep -nE "factory\.CreateAgent\(|factory\.CreateAgentProvider" agent/gemini/cmd/run-task/main.go
```
Expected: `factory.CreateAgent(` present; `CreateAgentProvider` absent.

## 6. Rewrite `agent/gemini/pkg/factory/factory_test.go`

The healthcheck-only binary has a tighter test surface than agent-claude. Test cases:

- `CreateAgentProvider(nil)` returns a non-nil provider. (`NewGeminiStep` only stores parser — no nil deref at construction time.)
- `provider.Get(ctx, TaskTypeHealthcheck)` returns non-nil agent + nil err.
- `provider.Get(ctx, lib.TaskType("gemini"))` returns nil + error containing `unknown task_type`, `"gemini"`, `agent-gemini`, `[healthcheck]`. (Literal "gemini" string is rejected — confirms no implicit domain task type.)
- `provider.Get(ctx, lib.TaskType("bogus"))` returns nil + same shape.

**Test scaffold:**
```go
package factory_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/agent/gemini/pkg/factory"
	agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("CreateAgentProvider", func() {
	var (
		ctx      context.Context
		provider agentlib.AgentProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		// nil parser is safe — NewGeminiStep stores the parser without invoking it.
		provider = factory.CreateAgentProvider(nil)
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
				Expect(err.Error()).To(ContainSubstring("agent-gemini"))
				Expect(err.Error()).To(ContainSubstring("[healthcheck]"))
			},
			Entry("literal gemini rejected (no implicit domain type)", agentlib.TaskType("gemini"), `"gemini"`),
			Entry("bogus value", agentlib.TaskType("bogus"), `"bogus"`),
			Entry("empty value", agentlib.TaskType(""), `""`),
		)
	})
})
```

The existing `factory_suite_test.go` and `Test*` function are unchanged.

Run tests:
```bash
cd agent/gemini && go test ./pkg/factory/...
```
Expected: exit 0.

## 7. Update root `CHANGELOG.md`

Append to `## Unreleased`:
```markdown
- refactor(agent/gemini): factory.go is pure plumbing — `CreateAgentForTaskType` and `CreateDeliverer` removed; new `CreateAgentProvider` returns lib.AgentProvider (healthcheck-only binary); boot-time deliverer construction moved to main.go Run
```

Verify:
```bash
grep -n "agent/gemini.*CreateAgentProvider" CHANGELOG.md
```
Expected: at least 1 match.

## 8. Run final precommit

```bash
cd agent/gemini && make precommit
```
Must exit 0.

</requirements>

<constraints>
- Healthcheck-only binary: provider map contains exactly one entry (`TaskTypeHealthcheck`). The literal `"gemini"` string MUST fall into the default-error branch.
- `cmd/run-task/main.go` is FROZEN.
- `factory.go` after this prompt contains: `CreateSyncProducer`, `CreateGeminiParser`, `CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgent`, `CreateAgentProvider`. No `CreateAgentForTaskType`, no `CreateDeliverer`.
- `CreateGeminiParser` is called from `main.go Run` directly (not transitively via a factory). Its error surfaces at boot.
- Error format on dispatch miss is owned by `lib.AgentProvider.Get`: `"unknown task_type %q for %s; accepted: %v"`. Sorted accepted list is `[healthcheck]`.
- Error wrapping uses `github.com/bborbe/errors`. Never `fmt.Errorf`.
- Do NOT commit.
- `cd agent/gemini && make precommit` must exit 0.
</constraints>

<verification>

Precondition:
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches.

factory.go shape:
```bash
grep -nE "^func " agent/gemini/pkg/factory/factory.go
```
Expected: `CreateSyncProducer`, `CreateGeminiParser`, `CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgent`, `CreateAgentProvider`.

```bash
grep -nE "CreateAgentForTaskType|^func CreateDeliverer" agent/gemini/pkg/factory/factory.go
```
Expected: zero matches.

main.go uses new shape:
```bash
grep -nE "factory\.CreateAgentProvider|provider\.Get" agent/gemini/main.go
```
Expected: both present.

cmd/run-task unchanged:
```bash
grep -nE "factory\.CreateAgent\(|factory\.CreateAgentProvider" agent/gemini/cmd/run-task/main.go
```
Expected: `factory.CreateAgent(` present; `CreateAgentProvider` absent.

All tests pass:
```bash
cd agent/gemini && go test ./...
```
Expected: exit 0.

CHANGELOG:
```bash
grep -n "agent/gemini.*CreateAgentProvider" CHANGELOG.md
```
Expected: at least 1 match.

Precommit:
```bash
cd agent/gemini && make precommit
```
Expected: exit 0.

</verification>
