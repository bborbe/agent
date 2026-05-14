---
status: draft
spec: [031-agent-repo-task-type-dispatch]
created: "2026-05-14T13:00:00Z"
branch: dark-factory/agent-repo-task-type-dispatch
---

<summary>
- A new `lib/healthcheck/` package is added to the `lib/` Go module with four exported functions and no other exported symbols
- `healthcheck.NewClaudeStep` wraps a Claude CLI runner: runs a `"reply 'ok'"` smoke prompt, returns `done` on non-empty reply, `failed` on error or empty reply
- `healthcheck.NewGeminiStep` wraps an `agentlib.AIParser`: calls Parse with a `"reply 'ok'"` prompt, returns `done` on non-empty parsed field, `failed` on parse error or empty reply
- `healthcheck.NewNopStep` returns a step that immediately returns `done` with output `"ok"` — no external calls
- `healthcheck.NewAgent` wraps any Step into an `*agentlib.Agent` registered under all three phase names (`planning`, `in_progress`, `ai_review`) so the healthcheck runs regardless of which PHASE env the executor injects
- A new constant `lib.TaskTypeHealthcheck TaskType = "healthcheck"` is added immediately after `TaskTypeOAuthProbe` in `lib/agent_task-type.go`
- The `TaskTypeOAuthProbe` GoDoc is updated: `"once introduced by the oauth-probe rename spec"` qualifier is removed
- The `lib/agent_task-type_test.go` valid-constants table gains one new entry for `TaskTypeHealthcheck`
- `make precommit` passes in `lib/`
</summary>

<objective>
Create the shared `lib/healthcheck/` liveness package and add the `TaskTypeHealthcheck` constant. This is the foundation that prompts 2, 3, and 4 (one per binary) depend on — it must merge first before any binary dispatch prompt runs.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping, counterfeiter annotations
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `go-doc-best-practices.md` in `~/.claude/plugins/marketplaces/coding/docs/` — GoDoc comment style for exported symbols
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**
- `lib/agent_task-type.go` — where `TaskTypeHealthcheck` is added; where `TaskTypeOAuthProbe` GoDoc is updated
- `lib/agent_task-type_test.go` — where the valid-constants table Entry is added
- `lib/agent_step.go` — the `Step` interface shape (Name, ShouldRun, Run)
- `lib/agent_agent.go` — `NewAgent(phases ...Phase) *Agent` and `NewPhase(name, steps...) Phase`
- `lib/agent_status.go` — `AgentStatusDone`, `AgentStatusFailed` constants
- `lib/claude/claude-runner.go` — `ClaudeRunner` interface and `ClaudeResult` type
- `lib/agent_parser.go` — `AIParser` interface
- `lib/mocks/claude-claude-runner.go` — existing ClaudeRunner mock (type `mocks.ClaudeRunner`)
- `lib/mocks/agent-ai-parser.go` — existing AIParser mock (type `mocks.AgentAIParser`)
- `lib/mocks/agent-step.go` — existing Step mock (type `mocks.AgentStep`)
- `lib/mocks/agent-result-deliverer.go` — existing ResultDeliverer mock
- `lib/lib_suite_test.go` — Ginkgo suite file pattern to mirror for healthcheck suite

**Inline reference — Step interface (from `lib/agent_step.go`):**
```go
type Step interface {
    Name() string
    ShouldRun(ctx context.Context, md *Markdown) (bool, error)
    Run(ctx context.Context, md *Markdown) (*Result, error)
}
```

**Inline reference — Result struct:**
```go
type Result struct {
    Status        AgentStatus
    NextPhase     string
    Message       string
    ContinueToNext bool
}
```

**Inline reference — ClaudeRunner.Run return type:**
```go
type ClaudeResult struct {
    Result string `json:"result"`
}
```

**Inline reference — healthcheck package's four exported functions (exact signatures):**
```go
func NewClaudeStep(runner claudelib.ClaudeRunner) agentlib.Step
func NewGeminiStep(parser agentlib.AIParser) agentlib.Step
func NewNopStep() agentlib.Step
func NewAgent(step agentlib.Step) *agentlib.Agent
```

**Inline reference — NewAgent wraps step under all three phases:**
```go
func NewAgent(step agentlib.Step) *agentlib.Agent {
    return agentlib.NewAgent(
        agentlib.NewPhase("planning",    step),
        agentlib.NewPhase("in_progress", step),
        agentlib.NewPhase("ai_review",   step),
    )
}
```

**Symbol verification — run before writing:**
```bash
grep -n "type ClaudeRunner interface" lib/claude/claude-runner.go
grep -n "type AIParser interface" lib/agent_parser.go
grep -n "func NewAgent" lib/agent_agent.go
grep -n "func NewPhase" lib/agent_phase.go
grep -n "AgentStatusDone\|AgentStatusFailed" lib/agent_status.go
grep -n "type ClaudeResult struct" lib/claude/claude-result.go
# Confirm existing mocks exist
ls lib/mocks/claude-claude-runner.go lib/mocks/agent-ai-parser.go lib/mocks/agent-step.go lib/mocks/agent-result-deliverer.go
```
</context>

<requirements>

## 1. Create `lib/healthcheck/healthcheck-claude-step.go`

New file. License header required (copy from `lib/agent_task-identifier.go`). Package: `healthcheck`.

```go
package healthcheck

import (
    "context"
    "strings"

    "github.com/bborbe/errors"

    agentlib "github.com/bborbe/agent/lib"
    claudelib "github.com/bborbe/agent/lib/claude"
)

// NewClaudeStep returns a Step that runs a "reply 'ok'" smoke prompt via the
// configured Claude CLI runner. Used by the agent-claude binary to verify
// that its Claude CLI dependency is reachable.
func NewClaudeStep(runner claudelib.ClaudeRunner) agentlib.Step {
    return &claudeStep{runner: runner}
}

type claudeStep struct {
    runner claudelib.ClaudeRunner
}

func (s *claudeStep) Name() string { return "healthcheck-claude" }

func (s *claudeStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
    return true, nil
}

func (s *claudeStep) Run(ctx context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
    result, err := s.runner.Run(ctx, "reply 'ok'")
    if err != nil {
        return &agentlib.Result{
            Status:  agentlib.AgentStatusFailed,
            Message: errors.Wrapf(ctx, err, "healthcheck-claude run failed").Error(),
        }, nil
    }
    trimmed := strings.TrimSpace(result.Result)
    if trimmed == "" {
        return &agentlib.Result{
            Status:  agentlib.AgentStatusFailed,
            Message: "healthcheck-claude reply empty",
        }, nil
    }
    return &agentlib.Result{
        Status:  agentlib.AgentStatusDone,
        Message: trimmed,
    }, nil
}
```

## 2. Create `lib/healthcheck/healthcheck-gemini-step.go`

New file. License header required. Package: `healthcheck`.

The `replyTarget` struct is unexported (no other exports beyond the four listed in Desired Behavior item 1). Field `OK` tagged `json:"ok"` so the parser can populate it via JSON round-trip.

```go
package healthcheck

import (
    "context"

    "github.com/bborbe/errors"

    agentlib "github.com/bborbe/agent/lib"
)

// replyTarget is the parse target for the Gemini healthcheck prompt.
// Kept unexported — only the step function is part of the public API.
type replyTarget struct {
    OK string `json:"ok"`
}

// NewGeminiStep returns a Step that calls the AIParser with a "reply 'ok'"
// prompt. Used by the agent-gemini binary to verify Gemini API reachability.
func NewGeminiStep(parser agentlib.AIParser) agentlib.Step {
    return &geminiStep{parser: parser}
}

type geminiStep struct {
    parser agentlib.AIParser
}

func (s *geminiStep) Name() string { return "healthcheck-gemini" }

func (s *geminiStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
    return true, nil
}

func (s *geminiStep) Run(ctx context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
    var reply replyTarget
    if err := s.parser.Parse(ctx, "reply 'ok'", &reply); err != nil {
        return &agentlib.Result{
            Status:  agentlib.AgentStatusFailed,
            Message: errors.Wrapf(ctx, err, "healthcheck-gemini parse failed").Error(),
        }, nil
    }
    if reply.OK == "" {
        return &agentlib.Result{
            Status:  agentlib.AgentStatusFailed,
            Message: "gemini healthcheck reply empty",
        }, nil
    }
    return &agentlib.Result{
        Status:  agentlib.AgentStatusDone,
        Message: reply.OK,
    }, nil
}
```

## 3. Create `lib/healthcheck/healthcheck-nop-step.go`

New file. License header required. Package: `healthcheck`.

```go
package healthcheck

import (
    "context"

    agentlib "github.com/bborbe/agent/lib"
)

// NewNopStep returns a Step that immediately returns done with output "ok".
// No external calls — reaching this step proves the binary booted and the
// framework wired the phase. Used by pure-Go agent binaries.
func NewNopStep() agentlib.Step {
    return &nopStep{}
}

type nopStep struct{}

func (s *nopStep) Name() string { return "healthcheck-nop" }

func (s *nopStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
    return true, nil
}

func (s *nopStep) Run(_ context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
    return &agentlib.Result{
        Status:  agentlib.AgentStatusDone,
        Message: "ok",
    }, nil
}
```

## 4. Create `lib/healthcheck/healthcheck-agent.go`

New file. License header required. Package: `healthcheck`.

```go
package healthcheck

import (
    agentlib "github.com/bborbe/agent/lib"
)

// NewAgent wraps any Step in a phase-agnostic *agentlib.Agent.
// The step is registered under all three phase names so the healthcheck
// task succeeds regardless of which PHASE env the executor injects.
func NewAgent(step agentlib.Step) *agentlib.Agent {
    return agentlib.NewAgent(
        agentlib.NewPhase("planning",    step),
        agentlib.NewPhase("in_progress", step),
        agentlib.NewPhase("ai_review",   step),
    )
}
```

## 5. Create `lib/healthcheck/healthcheck_suite_test.go`

New file. License header required. Package: `healthcheck_test`.

Mirror the pattern from `lib/lib_suite_test.go`:

```go
package healthcheck_test

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

func TestHealthcheck(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Healthcheck Suite")
}
```

Do NOT add a `//go:generate` directive here — healthcheck tests reuse existing mocks from `lib/mocks/`, no new mock generation needed.

## 6. Create `lib/healthcheck/healthcheck-claude-step_test.go`

New file. License header required. Package: `healthcheck_test`.

Tests: success path (runner returns non-empty result → done), error path (runner returns error → failed), empty result path (runner returns empty result → failed).

```go
package healthcheck_test

import (
    "context"
    stderrors "errors"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/bborbe/agent/lib/claude"
    "github.com/bborbe/agent/lib/healthcheck"
    agentlib "github.com/bborbe/agent/lib"
    "github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("NewClaudeStep", func() {
    var (
        ctx         context.Context
        fakeRunner  *mocks.ClaudeRunner
        step        agentlib.Step
    )

    BeforeEach(func() {
        ctx = context.Background()
        fakeRunner = &mocks.ClaudeRunner{}
        step = healthcheck.NewClaudeStep(fakeRunner)
    })

    Describe("Name", func() {
        It("returns healthcheck-claude", func() {
            Expect(step.Name()).To(Equal("healthcheck-claude"))
        })
    })

    Describe("ShouldRun", func() {
        It("always returns true", func() {
            ok, err := step.ShouldRun(ctx, nil)
            Expect(err).To(BeNil())
            Expect(ok).To(BeTrue())
        })
    })

    Describe("Run", func() {
        It("returns done when the runner returns a non-empty result", func() {
            fakeRunner.RunReturns(&claude.ClaudeResult{Result: "ok"}, nil)
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result).NotTo(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
            Expect(result.Message).To(Equal("ok"))
        })

        It("returns done and trims whitespace from the result", func() {
            fakeRunner.RunReturns(&claude.ClaudeResult{Result: "  ok  "}, nil)
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
            Expect(result.Message).To(Equal("ok"))
        })

        It("returns failed when the runner returns an error", func() {
            fakeRunner.RunReturns(nil, stderrors.New("cli failed"))
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result).NotTo(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
            Expect(result.Message).To(ContainSubstring("healthcheck-claude run failed"))
        })

        It("returns failed when the runner returns an empty result", func() {
            fakeRunner.RunReturns(&claude.ClaudeResult{Result: ""}, nil)
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
            Expect(result.Message).To(ContainSubstring("reply empty"))
        })
    })
})
```

**Note on `mocks.ClaudeRunner`:** The mock is in `lib/mocks/claude-claude-runner.go` under package `mocks`. Import path: `"github.com/bborbe/agent/lib/mocks"`. The mock type is `mocks.ClaudeRunner`.

## 7. Create `lib/healthcheck/healthcheck-gemini-step_test.go`

New file. License header required. Package: `healthcheck_test`.

The `replyTarget` struct in the healthcheck package is unexported. Use JSON unmarshalling in the ParseStub to populate the target without referencing the unexported type:

```go
package healthcheck_test

import (
    "context"
    "encoding/json"
    stderrors "errors"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/bborbe/agent/lib/healthcheck"
    agentlib "github.com/bborbe/agent/lib"
    "github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("NewGeminiStep", func() {
    var (
        ctx        context.Context
        fakeParser *mocks.AgentAIParser
        step       agentlib.Step
    )

    BeforeEach(func() {
        ctx = context.Background()
        fakeParser = &mocks.AgentAIParser{}
        step = healthcheck.NewGeminiStep(fakeParser)
    })

    Describe("Name", func() {
        It("returns healthcheck-gemini", func() {
            Expect(step.Name()).To(Equal("healthcheck-gemini"))
        })
    })

    Describe("ShouldRun", func() {
        It("always returns true", func() {
            ok, err := step.ShouldRun(ctx, nil)
            Expect(err).To(BeNil())
            Expect(ok).To(BeTrue())
        })
    })

    Describe("Run", func() {
        It("returns done when the parser populates a non-empty reply", func() {
            fakeParser.ParseStub = func(_ context.Context, _ string, target any) error {
                return json.Unmarshal([]byte(`{"ok":"pong"}`), target)
            }
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result).NotTo(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
            Expect(result.Message).To(Equal("pong"))
        })

        It("returns failed when the parser returns an error", func() {
            fakeParser.ParseReturns(stderrors.New("gemini unavailable"))
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result).NotTo(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
            Expect(result.Message).To(ContainSubstring("healthcheck-gemini parse failed"))
        })

        It("returns failed when the parser populates an empty OK field", func() {
            fakeParser.ParseStub = func(_ context.Context, _ string, target any) error {
                return json.Unmarshal([]byte(`{"ok":""}`), target)
            }
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
            Expect(result.Message).To(ContainSubstring("gemini healthcheck reply empty"))
        })
    })
})
```

**Note on `mocks.AgentAIParser`:** The mock is in `lib/mocks/agent-ai-parser.go`. Import path: `"github.com/bborbe/agent/lib/mocks"`. The mock type is `mocks.AgentAIParser`.

## 8. Create `lib/healthcheck/healthcheck-nop-step_test.go`

New file. License header required. Package: `healthcheck_test`.

```go
package healthcheck_test

import (
    "context"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/bborbe/agent/lib/healthcheck"
    agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("NewNopStep", func() {
    var (
        ctx  context.Context
        step agentlib.Step
    )

    BeforeEach(func() {
        ctx = context.Background()
        step = healthcheck.NewNopStep()
    })

    Describe("Name", func() {
        It("returns healthcheck-nop", func() {
            Expect(step.Name()).To(Equal("healthcheck-nop"))
        })
    })

    Describe("ShouldRun", func() {
        It("always returns true", func() {
            ok, err := step.ShouldRun(ctx, nil)
            Expect(err).To(BeNil())
            Expect(ok).To(BeTrue())
        })
    })

    Describe("Run", func() {
        It("returns done with message ok", func() {
            result, err := step.Run(ctx, nil)
            Expect(err).To(BeNil())
            Expect(result).NotTo(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
            Expect(result.Message).To(Equal("ok"))
        })
    })
})
```

## 9. Create `lib/healthcheck/healthcheck-agent_test.go`

New file. License header required. Package: `healthcheck_test`.

Tests: `NewAgent` returns non-nil; the wrapped step is invoked for all three phase values.

For `agent.Run`, pass a minimal task content string and a mock deliverer. The agent framework calls `ParseMarkdown(ctx, content)` — use simple content. The deliverer's `DeliverResult` is called after the step returns; use the mock's zero-value (returns nil by default).

```go
package healthcheck_test

import (
    "context"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/bborbe/vault-cli/pkg/domain"

    "github.com/bborbe/agent/lib/healthcheck"
    agentlib "github.com/bborbe/agent/lib"
    "github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("NewAgent", func() {
    var (
        ctx       context.Context
        fakeStep  *mocks.AgentStep
        agent     *agentlib.Agent
        deliverer *mocks.AgentResultDeliverer
    )

    BeforeEach(func() {
        ctx = context.Background()
        fakeStep = &mocks.AgentStep{}
        fakeStep.ShouldRunReturns(true, nil)
        fakeStep.RunReturns(&agentlib.Result{Status: agentlib.AgentStatusDone}, nil)
        agent = healthcheck.NewAgent(fakeStep)
        deliverer = &mocks.AgentResultDeliverer{}
        // DeliverResult returns nil by default (zero value of mock)
    })

    It("returns a non-nil agent", func() {
        Expect(agent).NotTo(BeNil())
    })

    DescribeTable("dispatches to the wrapped step for each phase",
        func(phase domain.TaskPhase) {
            result, err := agent.Run(ctx, phase, "# Task\n\ncontent\n", deliverer)
            Expect(err).To(BeNil())
            Expect(result).NotTo(BeNil())
            Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
        },
        Entry("planning phase", domain.TaskPhase("planning")),
        Entry("in_progress phase", domain.TaskPhase("in_progress")),
        Entry("ai_review phase", domain.TaskPhase("ai_review")),
    )

    It("invokes the step once per agent.Run call", func() {
        _, _ = agent.Run(ctx, "planning", "# Task\n\ncontent\n", deliverer)
        Expect(fakeStep.RunCallCount()).To(Equal(1))
    })
})
```

**Note on `mocks.AgentResultDeliverer`:** Located at `lib/mocks/agent-result-deliverer.go`. The mock's `DeliverResult` returns nil by default. If it panics (unexpected calls), initialize with `deliverer.DeliverResultReturns(nil)`.

**Note on `domain.TaskPhase`:** Import `"github.com/bborbe/vault-cli/pkg/domain"`. Verify it's a dependency of lib:
```bash
grep "bborbe/vault-cli" lib/go.mod
```
Expected: present.

## 10. Add `TaskTypeHealthcheck` constant to `lib/agent_task-type.go`

Read the full file before editing. Add a new constant immediately after `TaskTypeOAuthProbe`:

```go
// TaskTypeHealthcheck is the liveness task type for all agent binaries.
// Dispatches to the binary's corresponding healthcheck step in lib/healthcheck.
TaskTypeHealthcheck TaskType = "healthcheck"
```

Also update the `TaskTypeOAuthProbe` GoDoc from:
```go
// Deprecated: use TaskTypeHealthcheck once introduced by the oauth-probe rename spec.
```
to:
```go
// Deprecated: use TaskTypeHealthcheck.
```

Verify after editing:
```bash
grep -n "TaskTypeHealthcheck\|TaskTypeOAuthProbe" lib/agent_task-type.go
```
Expected: both constants present; `TaskTypeOAuthProbe` deprecation comment is updated.

## 11. Add `TaskTypeHealthcheck` to `lib/agent_task-type_test.go` valid-constants table

Read the full file before editing. Find the `DescribeTable("valid values", ...)` block. Add the following `Entry` immediately after the `Entry("oauth-probe constant", lib.TaskTypeOAuthProbe)` line:

```go
Entry("healthcheck constant", lib.TaskTypeHealthcheck),
```

Verify after editing:
```bash
grep -n "healthcheck constant\|TaskTypeHealthcheck" lib/agent_task-type_test.go
```
Expected: one match.

## 12. Build check

```bash
cd lib && go build ./...
```
Expected: exit 0.

## 13. Run iterative tests

```bash
cd lib && go test ./...
```
Fix compile errors before continuing.

Common issues:
- Import cycle: healthcheck → lib (OK), healthcheck → lib/claude (OK), healthcheck → lib/mocks (OK in tests). None of these create cycles because healthcheck is a sub-package of the lib module, not a reverse import.
- `mocks.AgentResultDeliverer` — verify type name: `grep -n "type.*struct" lib/mocks/agent-result-deliverer.go | head -3`
- `json.Unmarshal` in the Gemini step test — add `"encoding/json"` to imports
- `domain.TaskPhase` — add `"github.com/bborbe/vault-cli/pkg/domain"` import to `healthcheck-agent_test.go`

Coverage check:
```bash
cd lib && go test -coverprofile=/tmp/healthcheck-cover.out ./healthcheck/... && \
  go tool cover -func=/tmp/healthcheck-cover.out | grep -E "healthcheck|total"
```
Expected: each file ≥80% statement coverage.

## 14. Update `CHANGELOG.md` at repo root

Check for an existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add BOTH bullets (both are owned by this prompt):
```markdown
- feat(lib/healthcheck): shared liveness handler package — Claude/Gemini/Nop step flavors + NewAgent wrapper (spec 031)
- feat(lib): add TaskTypeHealthcheck constant; update TaskTypeOAuthProbe GoDoc (drop "once introduced" qualifier) (spec 031)
```

Verify:
```bash
grep -A 5 "^## Unreleased" CHANGELOG.md
```
Expected: both bullets present.

## 15. Run final precommit in `lib/`

```bash
cd lib && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

</requirements>

<constraints>
- `lib/healthcheck/` lives inside the `lib/` Go module (`github.com/bborbe/agent/lib`). No new `go.mod` is created.
- Only four exported symbols exist in the package: `NewClaudeStep`, `NewGeminiStep`, `NewNopStep`, `NewAgent`. No exported types, no exported constants.
- `replyTarget` in `healthcheck-gemini-step.go` is unexported. Field `OK string \`json:"ok"\`` allows JSON-based test stubs without exposing the type.
- All three step types (`claudeStep`, `geminiStep`, `nopStep`) are unexported structs with unexported fields.
- `ShouldRun` always returns `(true, nil)` for all three step types — no idempotency guard, healthchecks always run.
- Step `Run` methods accept `_ *agentlib.Markdown` (ignored) — the healthcheck steps do not read or mutate the markdown body.
- `NewAgent` registers the step under EXACTLY these three phase names in this order: `"planning"`, `"in_progress"`, `"ai_review"`. No other phases.
- `TaskTypeHealthcheck` constant is added immediately AFTER `TaskTypeOAuthProbe` in the `const(...)` block.
- `TaskTypeOAuthProbe` GoDoc changes ONLY the trailing qualifier; the `// Deprecated:` prefix and ` use TaskTypeHealthcheck.` body are preserved.
- Test files are in `package healthcheck_test`. The suite file (`healthcheck_suite_test.go`) owns the `TestHealthcheck` function — no other file in the package declares a `TestHealthcheck` function.
- Tests reuse existing mocks from `lib/mocks/`: `mocks.ClaudeRunner`, `mocks.AgentAIParser`, `mocks.AgentStep`, `mocks.AgentResultDeliverer`. No new counterfeiter directives are added.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`. Use `errors.Wrapf(ctx, err, "message")` for wrapping.
- No changes to `agent/claude/`, `agent/gemini/`, `agent/code/`, or `task/executor/`.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify package files exist:
```bash
ls lib/healthcheck/healthcheck-claude-step.go \
   lib/healthcheck/healthcheck-gemini-step.go \
   lib/healthcheck/healthcheck-nop-step.go \
   lib/healthcheck/healthcheck-agent.go
```
Expected: all four files present.

Verify exported function signatures:
```bash
grep -n "^func New" lib/healthcheck/healthcheck-claude-step.go \
                    lib/healthcheck/healthcheck-gemini-step.go \
                    lib/healthcheck/healthcheck-nop-step.go \
                    lib/healthcheck/healthcheck-agent.go
```
Expected: four lines — one `NewClaudeStep`, one `NewGeminiStep`, one `NewNopStep`, one `NewAgent`.

Verify step names:
```bash
grep -n '"healthcheck-claude"\|"healthcheck-gemini"\|"healthcheck-nop"' lib/healthcheck/
```
Expected: one match per step file.

Verify TaskTypeHealthcheck added:
```bash
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
Expected: constant definition and GoDoc.

Verify TaskTypeOAuthProbe GoDoc updated:
```bash
grep -B 2 "TaskTypeOAuthProbe TaskType" lib/agent_task-type.go
```
Expected: GoDoc comment says `// Deprecated: use TaskTypeHealthcheck.` without "once introduced" qualifier.

Verify test table updated:
```bash
grep -n "healthcheck constant" lib/agent_task-type_test.go
```
Expected: one match.

Run all lib tests:
```bash
cd lib && go test ./...
```
Expected: exit 0.

Run healthcheck coverage:
```bash
cd lib && go test -coverprofile=/tmp/hc-cover.out ./healthcheck/... && \
  go tool cover -func=/tmp/hc-cover.out | grep "total:"
```
Expected: ≥80% total coverage for the healthcheck package.

Verify CHANGELOG:
```bash
grep -A 6 "^## Unreleased" CHANGELOG.md
```
Expected: both `feat(lib/healthcheck)` and `feat(lib)` bullets present.

Run precommit:
```bash
cd lib && make precommit
```
Expected: exit 0.

</verification>
