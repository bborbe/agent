---
status: completed
summary: Added AgentProvider interface, NewAgentProvider constructor, agentProvider struct+Get method in lib/agent_agent-provider.go; generated counterfeiter mock AgentAgentProvider; added full Ginkgo test suite with 100% coverage; updated CHANGELOG.md with Unreleased entry.
container: agent-126-lib-agent-provider
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T13:30:00Z"
queued: "2026-05-14T13:59:46Z"
started: "2026-05-14T13:59:48Z"
completed: "2026-05-14T14:02:18Z"
branch: refactor/agent-provider
---

<summary>
- A new file `lib/agent_agent-provider.go` introduces an `AgentProvider` interface and an unexported `agentProvider` struct that implements it, plus a `NewAgentProvider` constructor.
- `AgentProvider.Get(ctx, taskType) (*Agent, error)` performs a map lookup; on miss it returns `nil` and an `errors.Errorf`-wrapped error whose message contains the literal `unknown task_type`, the offending value (quoted), the binary/agent name, and the sorted accepted-types list.
- A counterfeiter mock is generated at `lib/mocks/agent-agent-provider.go` with fake name `AgentAgentProvider`.
- A new test file `lib/agent_agent-provider_test.go` covers: hit path returns the mapped agent, miss path returns error with the right message shape, empty-map miss path, and that the accepted-types list in the error message is sorted deterministically.
- `make precommit` passes in `lib/`.
- No call sites change. Existing `factory.CreateAgentForTaskType` in agent-claude/gemini/code remains untouched by this prompt — those binaries are refactored in follow-up prompts.
</summary>

<objective>
Introduce the `AgentProvider` abstraction in the shared `lib/` module. This is the seam that lets per-binary factories return pure-plumbing providers instead of error-returning dispatch functions with `switch` statements. After this prompt ships, the 3 agent-repo factories can be refactored (in subsequent prompts) to expose `CreateAgentProvider() AgentProvider` instead of `CreateAgentForTaskType(...) (*Agent, error)` — moving the conditional + error out of the factory and into a tested impl method, per `~/Documents/workspaces/coding/docs/go-factory-pattern.md`.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `~/Documents/workspaces/coding/docs/go-patterns.md` — interface → constructor → struct order, named types, error wrapping
- `~/Documents/workspaces/coding/docs/go-factory-pattern.md` — the design rule motivating this prompt: factories must be pure plumbing, error returns are a RED flag for factory code. **Section 5 ("When You Need Dispatch — Use a Provider Interface")** describes the exact pattern this prompt implements.
- `~/Documents/workspaces/coding/docs/go-testing-guide.md` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `~/Documents/workspaces/coding/docs/go-doc-best-practices.md` — GoDoc comment style

**Design principle (motivating context):**
The 3 agent-repo factories (`agent/{claude,gemini,code}/pkg/factory/factory.go`) currently expose a `CreateAgentForTaskType(ctx, taskType, ...) (*Agent, error)` function that:
- contains a `switch taskType { ... }` statement (factory pattern guide section 4 forbids conditionals in factories)
- returns `error` (factory pattern guide section 6 forbids — error returns are a RED flag)
- formats the error message with `errors.Errorf` (more logic in a factory)

This prompt introduces the seam that lets us pull all three out of `factory.go` into a tested impl method. The factories become pure plumbing (a map literal), and the dispatch logic + error formatting live in `agentProvider.Get` — a single tested method in `lib/` shared by every binary.

This prompt only adds the new code in `lib/`. It does NOT change any binary's factory or main.go — those refactors are independent follow-up prompts that depend on this one.

**Key files to read in full before editing:**
- `lib/agent_task-identifier.go` — shape reference for the lib's named-type pattern (License header + import style)
- `lib/agent_task-type.go` — defines `TaskType` (used as the map key in this prompt)
- `lib/agent_agent.go` — defines `*Agent` (the map value); also shows the lib's interface/constructor/struct ordering convention
- `lib/agent_step.go` — shows the `//counterfeiter:generate` annotation form
- `lib/agent_parser.go` — another counterfeiter annotation reference
- `lib/lib_suite_test.go` — owns `TestLib`; do NOT add a second `Test*` function in this prompt
- `lib/agent_task_test.go` — example existing test file layout in `package lib_test`

**Inline reference — counterfeiter annotation shape from `lib/agent_step.go:9`:**
```go
//counterfeiter:generate -o mocks/agent-step.go --fake-name AgentStep . Step
```
For `AgentProvider`, this becomes:
```go
//counterfeiter:generate -o mocks/agent-agent-provider.go --fake-name AgentAgentProvider . AgentProvider
```
The double `Agent` in `AgentAgentProvider` is intentional — matches the existing `Agent<TypeName>` fake-name convention namespacing mocks across packages (`AgentStep`, `AgentResultDeliverer`, `AgentAIParser` are all sibling examples).

**Inline reference — error message format (locked):**
The error returned on a map miss must satisfy these assertions:
- contains the literal substring `unknown task_type`
- contains the offending task-type value quoted with `%q` (e.g. `"bogus"`)
- contains the binary/agent name passed to `NewAgentProvider` (e.g. `agent-claude`)
- contains the accepted-types list, sorted alphabetically, in slice-format (e.g. `[claude healthcheck]`)

Exact format string:
```go
"unknown task_type %q for %s; accepted: %v"
```
Where the third arg is the sorted `[]string` of accepted constants. Sorting matters: deterministic error messages make assertions in downstream binary tests stable.

**Symbol verification — bborbe/errors:**
Already used throughout `lib/`. Confirm the import path is `"github.com/bborbe/errors"`:
```bash
grep -n "bborbe/errors" lib/agent_task-identifier.go
```
Expected: one match.
</context>

<requirements>

## 1. Create `lib/agent_agent-provider.go`

New file. Package: `lib`. License header required (copy from `lib/agent_task-identifier.go`).

**Imports:**
```go
import (
	"context"
	"sort"

	"github.com/bborbe/errors"
)
```

**Counterfeiter annotation (above the interface):**
```go
//counterfeiter:generate -o mocks/agent-agent-provider.go --fake-name AgentAgentProvider . AgentProvider
```

**Interface definition:**
```go
// AgentProvider returns the *Agent registered for a given TaskType.
// Implementations are typically configured at boot via NewAgentProvider with
// a binary-specific dispatch table; the Get method is called once per task
// at the Kafka entry point.
type AgentProvider interface {
	Get(ctx context.Context, taskType TaskType) (*Agent, error)
}
```

**Constructor (after the interface):**
```go
// NewAgentProvider wires a task_type → *Agent dispatch table. The name argument
// identifies the consuming binary in the error message returned on a map miss
// (e.g. "agent-claude") and should match the binary's serviceName constant.
//
// The agents map is captured by reference; callers must not mutate it after
// construction. Pass a freshly-built map.
func NewAgentProvider(name string, agents map[TaskType]*Agent) AgentProvider {
	return &agentProvider{name: name, agents: agents}
}
```

**Struct + method (after the constructor):**
```go
type agentProvider struct {
	name   string
	agents map[TaskType]*Agent
}

// Get returns the *Agent registered for taskType. On a map miss, it returns
// nil and an errors.Errorf-wrapped error whose message names the offending
// task_type value, the provider's name, and the sorted list of accepted
// constants.
func (p *agentProvider) Get(ctx context.Context, taskType TaskType) (*Agent, error) {
	if agent, ok := p.agents[taskType]; ok {
		return agent, nil
	}
	accepted := make([]string, 0, len(p.agents))
	for tt := range p.agents {
		accepted = append(accepted, string(tt))
	}
	sort.Strings(accepted)
	return nil, errors.Errorf(
		ctx,
		"unknown task_type %q for %s; accepted: %v",
		taskType, p.name, accepted,
	)
}
```

Verify the file structure after writing:
```bash
grep -n "^type AgentProvider interface\|^func NewAgentProvider\|^type agentProvider struct\|^func (p \*agentProvider) Get" lib/agent_agent-provider.go
```
Expected: 4 lines (interface + constructor + struct + method).

## 2. Generate the counterfeiter mock

```bash
cd lib && make generate
```

Verify the mock file was produced:
```bash
ls lib/mocks/agent-agent-provider.go
```
Expected: file present with type `AgentAgentProvider`.

```bash
grep -n "type AgentAgentProvider struct" lib/mocks/agent-agent-provider.go
```
Expected: one match.

## 3. Create `lib/agent_agent-provider_test.go`

New test file. Package: `lib_test`. License header required.

Do NOT add a new `TestLib` function — `lib/lib_suite_test.go` already defines it. This file only adds Ginkgo `Describe` blocks.

**Imports:**
```go
import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/bborbe/agent/lib"
)
```
(No alias — the package is named `lib`; use `lib.AgentProvider` etc. directly. Matches existing `lib/agent_task_test.go` style.)

**Test structure:**
```go
var _ = Describe("AgentProvider", func() {
	var (
		ctx     context.Context
		domain  *lib.Agent
		liveness *lib.Agent
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Construct two distinct Agent values to verify the map returns the right one.
		// Empty Phases are fine — the test only asserts identity, not Run behavior.
		domain = lib.NewAgent()
		liveness = lib.NewAgent()
	})

	Describe("Get", func() {
		It("returns the agent registered for the given task_type", func() {
			provider := lib.NewAgentProvider("test-binary", map[lib.TaskType]*lib.Agent{
				lib.TaskTypeClaude:      domain,
				lib.TaskTypeHealthcheck: liveness,
			})
			result, err := provider.Get(ctx, lib.TaskTypeClaude)
			Expect(err).To(BeNil())
			Expect(result).To(BeIdenticalTo(domain))
		})

		It("returns a different agent for a different task_type from the same provider", func() {
			provider := lib.NewAgentProvider("test-binary", map[lib.TaskType]*lib.Agent{
				lib.TaskTypeClaude:      domain,
				lib.TaskTypeHealthcheck: liveness,
			})
			result, err := provider.Get(ctx, lib.TaskTypeHealthcheck)
			Expect(err).To(BeNil())
			Expect(result).To(BeIdenticalTo(liveness))
		})

		Describe("miss path", func() {
			var provider lib.AgentProvider

			BeforeEach(func() {
				provider = lib.NewAgentProvider("test-binary", map[lib.TaskType]*lib.Agent{
					lib.TaskTypeClaude:      domain,
					lib.TaskTypeHealthcheck: liveness,
				})
			})

			It("returns nil agent on unknown task_type", func() {
				result, err := provider.Get(ctx, lib.TaskType("bogus"))
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			DescribeTable("error message contents",
				func(matcher types.GomegaMatcher) {
					_, err := provider.Get(ctx, lib.TaskType("bogus"))
					Expect(err.Error()).To(matcher)
				},
				Entry("literal 'unknown task_type'", ContainSubstring("unknown task_type")),
				Entry("offending value quoted", ContainSubstring(`"bogus"`)),
				Entry("provider name", ContainSubstring("test-binary")),
				Entry("accepted list contains claude", ContainSubstring("claude")),
				Entry("accepted list contains healthcheck", ContainSubstring("healthcheck")),
			)

			It("returns accepted-types list sorted alphabetically (deterministic)", func() {
				_, err := provider.Get(ctx, lib.TaskType("bogus"))
				Expect(err.Error()).To(ContainSubstring("[claude healthcheck]"))
			})
		})

		It("returns nil agent and an error when the map is empty", func() {
			provider := lib.NewAgentProvider("empty-binary", map[lib.TaskType]*lib.Agent{})
			result, err := provider.Get(ctx, lib.TaskTypeHealthcheck)
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("empty-binary"))
			Expect(err.Error()).To(ContainSubstring("accepted: []"))
		})
	})
})
```

Verify the test file compiles and runs:
```bash
cd lib && go test ./... -run TestLib -v 2>&1 | grep -E "AgentProvider|PASS|FAIL" | head -10
```
Expected: AgentProvider specs pass.

## 4. Run coverage check

```bash
cd lib && go test -coverprofile=/tmp/agent-provider-cover.out ./... && go tool cover -func=/tmp/agent-provider-cover.out | grep "agent_agent-provider"
```
Expected: every function in `agent_agent-provider.go` at 100% statement coverage. The file is small enough that every branch is exercised: `Get` hit path (2 cases), `Get` miss path (multiple `DescribeTable` entries), empty-map path, sort-determinism assertion.

## 5. Update `CHANGELOG.md` at the repo root

Check for existing `## Unreleased`:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

**If `## Unreleased` exists**, append a new bullet to it.
**If absent**, insert a new `## Unreleased` section immediately above the first `## v` header, then add the bullet.

Bullet text:
```markdown
- feat(lib): add `AgentProvider` interface for task_type → *Agent dispatch — map-based provider with sorted-accepted-types error message; consumed by per-binary factory refactors that drop `CreateAgentForTaskType` switch statements (factory pattern compliance)
```

Verify:
```bash
grep -n "AgentProvider" CHANGELOG.md
```
Expected: at least one match under `## Unreleased`.

## 6. Run final precommit in `lib/`

```bash
cd lib && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

</requirements>

<constraints>
- The `AgentProvider` interface lives in `lib/agent_agent-provider.go`. The unexported `agentProvider` struct and `Get` method live in the SAME file (one file per type per the lib's convention).
- The interface returns `(*Agent, error)`. The error is part of the interface contract — this is the correct place for error returns, not the factory.
- `NewAgentProvider` is the ONLY exported constructor. The struct stays unexported.
- The provider does NOT copy the agents map — it captures by reference. The constructor's GoDoc explicitly warns callers not to mutate after construction.
- The accepted-types list in the error message MUST be sorted alphabetically. This is a stability requirement so binary tests can assert against fixed strings (e.g. `[claude healthcheck]`, not order-dependent).
- The error message format string is locked: `"unknown task_type %q for %s; accepted: %v"`. No deviation.
- No mock for `*Agent` is required — the test uses two `lib.NewAgent()` calls to produce distinct identity-comparable values.
- The counterfeiter mock for `AgentProvider` IS required and goes in `lib/mocks/agent-agent-provider.go` with fake name `AgentAgentProvider`.
- Test file is `package lib_test`. NO new `TestLib` function.
- This prompt does NOT touch `agent/{claude,gemini,code}/pkg/factory/factory.go` or any `main.go`. Those refactors are separate follow-up prompts.
- Error wrapping uses `github.com/bborbe/errors` only — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify the new file exists:
```bash
ls lib/agent_agent-provider.go lib/agent_agent-provider_test.go lib/mocks/agent-agent-provider.go
```
Expected: all three present.

Verify the four core symbols are defined:
```bash
grep -nE "^type AgentProvider interface|^func NewAgentProvider|^type agentProvider struct|^func \(p \*agentProvider\) Get" lib/agent_agent-provider.go
```
Expected: 4 matches.

Verify the mock file is `mocks.AgentAgentProvider`:
```bash
grep -n "type AgentAgentProvider struct" lib/mocks/agent-agent-provider.go
```
Expected: one match.

Verify the error format string is exact:
```bash
grep -n 'unknown task_type %q for %s; accepted: %v' lib/agent_agent-provider.go
```
Expected: one match.

Verify the accepted-types list is sorted:
```bash
grep -n "sort.Strings" lib/agent_agent-provider.go
```
Expected: one match.

Verify the test file does NOT redefine `TestLib`:
```bash
grep -c "^func TestLib" lib/agent_agent-provider_test.go
```
Expected: `0`.

Run tests:
```bash
cd lib && go test ./...
```
Expected: exit 0.

Run coverage:
```bash
cd lib && go test -coverprofile=/tmp/agent-provider-cover.out ./... && go tool cover -func=/tmp/agent-provider-cover.out | grep "agent_agent-provider"
```
Expected: each function in `agent_agent-provider.go` at 100% (it's tiny — every branch is covered by the tests above).

Run precommit:
```bash
cd lib && make precommit
```
Expected: exit 0.

CHANGELOG entry:
```bash
grep -A 3 "^## Unreleased" CHANGELOG.md | head -5
```
Expected: at least one bullet present mentioning `AgentProvider`.

</verification>