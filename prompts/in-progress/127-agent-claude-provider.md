---
status: committing
summary: 'Refactored agent/claude factory.go to pure plumbing: removed CreateAgentForTaskType and CreateDeliverer, split CreateAgent into CreateAgent+CreateAgentFromRunner, added CreateAgentProvider returning lib.AgentProvider, moved boot-time deliverer construction to main.go Run.'
container: agent-127-agent-claude-provider
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T14:00:00Z"
queued: "2026-05-14T14:14:13Z"
started: "2026-05-14T14:14:14Z"
branch: refactor/agent-claude-provider
---

<summary>
- `agent/claude/pkg/factory/factory.go` drops `CreateAgentForTaskType` (which has a `switch` and returns `error` — both factory-pattern violations) and gains `CreateAgentProvider` which returns the pre-wired `agentlib.AgentProvider` from `lib v0.62.13`.
- `CreateDeliverer` is deleted from `factory.go`. The "noop vs kafka" conditional and the `syncProducer` lifecycle move to `main.go Run` per the factory-pattern guide (section 6.2 + 8).
- `agent/claude/main.go` is rewritten to: (a) build the deliverer inline based on `TaskID`/`Brokers` with `defer cleanup`, (b) call `factory.CreateAgentProvider(...)` once, (c) call `provider.Get(ctx, taskType)` to select the agent.
- `factory_test.go` is rewritten as map-entry assertions on the returned `AgentProvider` — no more switch-branch tests.
- `make precommit` passes in `agent/claude/`.
- `cmd/run-task/main.go` is UNCHANGED — local CLI mode still calls the retained `CreateAgent`/`CreateClaudeRunner` factories directly.
</summary>

<objective>
Refactor `agent/claude` to use the new `lib.AgentProvider` seam shipped in `lib v0.62.13`. This is the proof-of-pattern binary — once it lands, prompts 3 (gemini) and 4 (code) apply the same shape mechanically. After this prompt: `factory.go` contains zero logic, zero errors, zero conditionals; `main.go Run` owns boot-time decisions; `agentlib.AgentProvider.Get` owns dispatch.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `~/Documents/workspaces/coding/docs/go-factory-pattern.md` — the design rule motivating this refactor. **Section 5** describes the Provider pattern; **section 6.2** describes lifting boot-time validation to `main.go`; **section 7** covers thin pass-through wrappers (which `CreateSyncProducer` is — leave it as-is).
- `~/Documents/workspaces/coding/docs/go-patterns.md`
- `~/Documents/workspaces/coding/docs/go-testing-guide.md`
- `~/Documents/workspaces/coding/docs/go-error-wrapping-guide.md`

**Precondition check — run before implementing:**
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches. If missing, STOP and report `"status":"failed"` with reason "lib.AgentProvider not yet shipped (precondition: prompt 1)".

**Key files to read in full before editing:**
- `agent/claude/pkg/factory/factory.go` — current shape; identify exactly which functions are dropped/added
- `agent/claude/main.go` — current `Run`; the deliverer + agent construction is what changes
- `agent/claude/cmd/run-task/main.go` — confirm it calls `factory.CreateAgent` (NOT `CreateAgentForTaskType`) so it stays unaffected
- `agent/claude/pkg/factory/factory_test.go` — existing tests rewritten
- `lib/agent_agent-provider.go` — the new interface; understand `Get(ctx, taskType) (*Agent, error)` semantics
- `lib/healthcheck/healthcheck-agent.go` — `NewAgent(step) *Agent`
- `lib/healthcheck/healthcheck-claude-step.go` — `NewClaudeStep(runner) Step`

**Current factory.go shape (lines verified):**
```go
// L95-117 — RETAIN as-is (still useful for cmd/run-task and as internal helper)
func CreateAgent(claudeConfigDir, agentDir, allowedTools, model, claudeEnv, envContext) *agentlib.Agent

// L123-156 — DELETE (switch + error = factory-pattern violations)
func CreateAgentForTaskType(ctx, taskType, claudeConfigDir, agentDir, allowedTools, model, claudeEnv, envContext) (*agentlib.Agent, error)

// L163-194 — DELETE (conditionals + error + cleanup closure = three violations)
func CreateDeliverer(ctx, taskID, brokers, branch, originalContent) (agentlib.ResultDeliverer, func(), error)
```

**New factory.go addition:**
```go
// CreateAgentProvider wires the per-task-type dispatch table for agent-claude.
// Returns lib.AgentProvider — main.go calls Get(ctx, taskType) to select the
// appropriate *Agent. Pure plumbing; no conditional, no error.
//
// TaskTypeClaude routes to the existing 3-phase domain agent. TaskTypeHealthcheck
// and TaskTypeOAuthProbe (transition alias) both route to the shared
// healthcheck-Claude liveness agent, reusing the same ClaudeRunner.
func CreateAgentProvider(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	claudeEnv map[string]string,
	envContext map[string]string,
) agentlib.AgentProvider {
	runner := CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv)
	domainAgent := CreateAgentFromRunner(runner, envContext)
	livenessAgent := healthcheck.NewAgent(healthcheck.NewClaudeStep(runner))
	return agentlib.NewAgentProvider("agent-claude", map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeClaude:      domainAgent,
		agentlib.TaskTypeHealthcheck: livenessAgent,
		agentlib.TaskTypeOAuthProbe:  livenessAgent,  // transition alias
	})
}
```

**Note**: the new function needs to share `runner` between the domain agent and the liveness agent. The current `CreateAgent` constructs its own runner internally — that doesn't work here. **Split `CreateAgent`** into:
- `CreateAgent(...)` — KEEP signature; builds runner internally, calls `CreateAgentFromRunner`. Used by `cmd/run-task/main.go`. Pure plumbing wrapper.
- `CreateAgentFromRunner(runner claudelib.ClaudeRunner, envContext map[string]string) *agentlib.Agent` — NEW; takes pre-built runner, returns the domain agent.

This preserves `cmd/run-task/main.go`'s call to `factory.CreateAgent(...)` while letting `CreateAgentProvider` share one runner across both branches.

**New main.go Run shape (excerpt — replaces the CreateDeliverer + CreateAgent + agent.Run block):**
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

provider := factory.CreateAgentProvider(
    a.ClaudeConfigDir,
    a.AgentDir,
    claudelib.ParseAllowedTools(a.AllowedToolsRaw),
    a.Model,
    claudelib.ParseKeyValuePairs(a.ClaudeEnvRaw),
    claudelib.ParseKeyValuePairs(a.EnvContextRaw),
)
agent, err := provider.Get(ctx, agentlib.TaskType(a.TaskType))
if err != nil {
    jobMetrics.RecordRun(agentlib.AgentStatusFailed)
    jobMetrics.RecordDuration(time.Since(start))
    return errors.Wrap(ctx, err, "select agent for task_type")
}

result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
// ... existing post-Run metrics + PrintResult block stays identical ...
```

The existing pre-block (metrics setup, defer pusher.PushContext, factory.CreateDeliverer call) gets rewritten as shown. Everything after `agent.Run` stays.

Imports added to `main.go`: `delivery "github.com/bborbe/agent/lib/delivery"` (for the noop default).
</context>

<requirements>

## 1. Verify precondition

```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches. STOP if absent.

## 2. Split `CreateAgent` into two functions

Read `agent/claude/pkg/factory/factory.go` in full.

Locate `CreateAgent` (currently L95-117). Refactor:

**Keep** `CreateAgent` with its current signature — change the body to delegate:
```go
func CreateAgent(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	claudeEnv map[string]string,
	envContext map[string]string,
) *agentlib.Agent {
	return CreateAgentFromRunner(
		CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv),
		envContext,
	)
}
```

**Add** the new `CreateAgentFromRunner` immediately after:
```go
// CreateAgentFromRunner builds the 3-phase claude agent given a pre-constructed
// ClaudeRunner. Used by CreateAgentProvider to share one runner across the
// domain agent and the healthcheck-Claude liveness agent.
func CreateAgentFromRunner(
	runner claudelib.ClaudeRunner,
	envContext map[string]string,
) *agentlib.Agent {
	step := claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "claude-task",
		Runner:        runner,
		Instructions:  prompts.BuildInstructions(),
		EnvContext:    envContext,
		OutputSection: "## Result",
		NextPhase:     "done",
	})
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("in_progress", step),
		agentlib.NewPhase("ai_review", step),
	)
}
```

Verify:
```bash
grep -nE "^func CreateAgent\(|^func CreateAgentFromRunner\(" agent/claude/pkg/factory/factory.go
```
Expected: 2 matches.

## 3. Delete `CreateAgentForTaskType`

Remove the entire function (currently L123-156) from `agent/claude/pkg/factory/factory.go`.

Verify:
```bash
grep -n "CreateAgentForTaskType" agent/claude/pkg/factory/factory.go
```
Expected: zero matches.

## 4. Add `CreateAgentProvider`

Add the new factory function (signature shown in `<context>` above) immediately after `CreateAgentFromRunner`.

Add the import for `healthcheck` if not already present (it IS already imported per the current file's line 24, so this is a no-op verification):
```bash
grep -n "lib/healthcheck" agent/claude/pkg/factory/factory.go
```
Expected: at least one match.

Verify the new function exists:
```bash
grep -n "^func CreateAgentProvider" agent/claude/pkg/factory/factory.go
```
Expected: one match.

## 5. Delete `CreateDeliverer`

Remove the entire function (currently L163-194) from `agent/claude/pkg/factory/factory.go`.

After deleting `CreateDeliverer`, the `glog` import in `factory.go` becomes unused (no other function in the file calls `glog`). Explicitly remove the `"github.com/golang/glog"` line from the import block. Verify:
```bash
grep -n "glog" agent/claude/pkg/factory/factory.go
```
Expected: zero matches.

Verify:
```bash
grep -n "^func CreateDeliverer" agent/claude/pkg/factory/factory.go
```
Expected: zero matches.

## 6. Rewrite `agent/claude/main.go` Run

Read `agent/claude/main.go` in full.

Locate the `Run` method. Find the block that:
1. Calls `factory.CreateDeliverer(...)` to build the deliverer
2. Calls `factory.CreateAgent(...)` to build the agent
3. Calls `agent.Run(...)`

Replace the deliverer construction + agent construction (everything from `deliverer, cleanup, err := factory.CreateDeliverer(...)` through the line BEFORE `result, err := agent.Run(...)`) with the new shape shown in `<context>`. Keep the existing `result, err := agent.Run(...)` and everything after it unchanged.

Add the `delivery` import to `main.go`:
```go
delivery "github.com/bborbe/agent/lib/delivery"
```
Group it alphabetically with the other internal imports.

Verify:
```bash
grep -nE "factory\.CreateDeliverer|factory\.CreateAgent\(|factory\.CreateAgentProvider|provider\.Get" agent/claude/main.go
```
Expected: `factory.CreateDeliverer` absent; `factory.CreateAgent(` absent; `factory.CreateAgentProvider` present; `provider.Get` present.

```bash
grep -n "delivery.NewNoopResultDeliverer\|defer func() {" agent/claude/main.go
```
Expected: noop deliverer present; defer for syncProducer cleanup present.

Build check:
```bash
cd agent/claude && go build ./...
```
Expected: exit 0.

## 7. Confirm `cmd/run-task/main.go` is unchanged

```bash
grep -nE "factory\.CreateAgent\(|factory\.CreateAgentProvider|factory\.CreateAgentFromRunner" agent/claude/cmd/run-task/main.go
```
Expected: `factory.CreateAgent(` present (unchanged); `CreateAgentProvider`/`CreateAgentFromRunner` absent.

`cmd/run-task` MUST NOT be edited in this prompt.

## 8. Rewrite `agent/claude/pkg/factory/factory_test.go`

Read the current file. The existing tests cover `CreateAgentForTaskType` switch branches — those are now deleted. The new test surface is:

- `CreateAgentProvider` returns a non-nil `AgentProvider` with the expected 3 entries (TaskTypeClaude, TaskTypeHealthcheck, TaskTypeOAuthProbe).
- `provider.Get(ctx, TaskTypeClaude)` returns a non-nil `*Agent` and nil error.
- `provider.Get(ctx, TaskTypeHealthcheck)` returns a non-nil `*Agent` and nil error.
- `provider.Get(ctx, TaskTypeOAuthProbe)` returns the SAME `*Agent` as TaskTypeHealthcheck (alias proof — use `BeIdenticalTo`).
- `provider.Get(ctx, lib.TaskType("bogus"))` returns nil and an error whose message contains `unknown task_type`, `"bogus"`, `agent-claude`, and the sorted accepted list (`[claude healthcheck oauth-probe]`).

**Test scaffold:**
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

var _ = Describe("CreateAgentProvider", func() {
	var (
		ctx      context.Context
		provider agentlib.AgentProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		provider = factory.CreateAgentProvider(
			claudelib.ClaudeConfigDir(""),
			claudelib.AgentDir("agent"),
			claudelib.AllowedTools{},
			claudelib.ClaudeModel("sonnet"),
			map[string]string{},
			map[string]string{},
		)
	})

	It("returns a non-nil provider", func() {
		Expect(provider).NotTo(BeNil())
	})

	It("Get returns the domain agent for TaskTypeClaude", func() {
		agent, err := provider.Get(ctx, agentlib.TaskTypeClaude)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("Get returns the liveness agent for TaskTypeHealthcheck", func() {
		agent, err := provider.Get(ctx, agentlib.TaskTypeHealthcheck)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("Get returns the SAME liveness agent for TaskTypeOAuthProbe (alias)", func() {
		healthcheckAgent, err := provider.Get(ctx, agentlib.TaskTypeHealthcheck)
		Expect(err).To(BeNil())
		oauthProbeAgent, err := provider.Get(ctx, agentlib.TaskTypeOAuthProbe)
		Expect(err).To(BeNil())
		Expect(oauthProbeAgent).To(BeIdenticalTo(healthcheckAgent))
	})

	Describe("Get with unknown task_type", func() {
		var err error

		BeforeEach(func() {
			_, err = provider.Get(ctx, agentlib.TaskType("bogus"))
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("error message contains the unknown task_type literal", func() {
			Expect(err.Error()).To(ContainSubstring("unknown task_type"))
		})

		It("error message contains the offending value quoted", func() {
			Expect(err.Error()).To(ContainSubstring(`"bogus"`))
		})

		It("error message contains the binary name", func() {
			Expect(err.Error()).To(ContainSubstring("agent-claude"))
		})

		It("error message contains the sorted accepted-types list", func() {
			Expect(err.Error()).To(ContainSubstring("[claude healthcheck oauth-probe]"))
		})
	})
})
```

The existing `factory_suite_test.go` and `Test*` function are unchanged.

Run the tests:
```bash
cd agent/claude && go test ./pkg/factory/...
```
Expected: exit 0.

## 9. Update root `CHANGELOG.md`

Add to the `## Unreleased` section (create the section above the first `## v` if absent):
```markdown
- refactor(agent/claude): factory.go is pure plumbing — `CreateAgentForTaskType` and `CreateDeliverer` removed; new `CreateAgentProvider` returns lib.AgentProvider; boot-time deliverer construction moved to main.go Run per go-factory-pattern.md
```

Verify:
```bash
grep -n "CreateAgentProvider" CHANGELOG.md
```
Expected: at least one match.

## 10. Run final precommit

```bash
cd agent/claude && make precommit
```
Must exit 0. If linter flags an unused import (likely `glog` in factory.go), let `goimports`/auto-fix handle it via the precommit chain.

</requirements>

<constraints>
- `lib/agent_agent-provider.go` already exists at `lib v0.62.13` — this prompt CONSUMES it, does not modify it.
- `cmd/run-task/main.go` is FROZEN — must not be edited. The split of `CreateAgent` into `CreateAgent` + `CreateAgentFromRunner` preserves `CreateAgent`'s signature exactly so cmd/run-task keeps building.
- `factory.go` after this prompt contains: `CreateClaudeRunner`, `CreateSyncProducer` (error-returning pass-through, allowed per guide §7), `CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgent`, `CreateAgentFromRunner`, `CreateAgentProvider`. No `CreateAgentForTaskType`, no `CreateDeliverer`.
- The `delivery` import is now needed in `main.go` (for `NewNoopResultDeliverer`). It was not needed before because the factory hid it.
- Error format on dispatch miss is determined by `lib.AgentProvider.Get` (locked in lib): `"unknown task_type %q for %s; accepted: %v"`. Tests must assert against `[claude healthcheck oauth-probe]` (sorted alphabetically) exactly.
- `TaskTypeOAuthProbe` is still in the map as a transition alias mapping to the same `*Agent` as `TaskTypeHealthcheck`. This matches the existing behavior shipped in v0.62.9.
- The `defer cleanup` for syncProducer is OWNED BY `main.go Run`, NOT by factory.go.
- Error wrapping uses `github.com/bborbe/errors` only — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- `cd agent/claude && make precommit` must exit 0.
</constraints>

<verification>

Precondition:
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider" lib/agent_agent-provider.go
```
Expected: 2 matches.

factory.go has the right shape:
```bash
grep -nE "^func " agent/claude/pkg/factory/factory.go
```
Expected: `CreateClaudeRunner`, `CreateSyncProducer`, `CreateKafkaResultDeliverer`, `CreateFileResultDeliverer`, `CreateAgent`, `CreateAgentFromRunner`, `CreateAgentProvider`. No `CreateAgentForTaskType`, no `CreateDeliverer`.

```bash
grep -nE "CreateAgentForTaskType|^func CreateDeliverer" agent/claude/pkg/factory/factory.go
```
Expected: zero matches.

main.go uses the new shape:
```bash
grep -nE "factory\.CreateAgentProvider|provider\.Get|delivery\.NewNoopResultDeliverer|defer func\(\) \{" agent/claude/main.go
```
Expected: all four present.

```bash
grep -nE "factory\.CreateDeliverer|factory\.CreateAgent\(" agent/claude/main.go
```
Expected: zero matches (CreateAgent is no longer called from main).

cmd/run-task unchanged:
```bash
grep -nE "factory\.CreateAgent\(|factory\.CreateAgentProvider" agent/claude/cmd/run-task/main.go
```
Expected: `factory.CreateAgent(` present; `CreateAgentProvider` absent.

All tests pass:
```bash
cd agent/claude && go test ./...
```
Expected: exit 0. Factory test should show 5+ specs under `Describe("CreateAgentProvider")`.

CHANGELOG entry:
```bash
grep -n "CreateAgentProvider" CHANGELOG.md
```
Expected: at least one match.

Precommit:
```bash
cd agent/claude && make precommit
```
Expected: exit 0.

</verification>
