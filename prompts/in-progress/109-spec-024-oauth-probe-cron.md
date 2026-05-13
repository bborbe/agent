---
status: committing
spec: [024-oauth-probe-weekly]
summary: 'Added weekly OAuth probe cron to task/executor: new pkg/probe package with ConfigProvider/CommandPublisher/OAuthProbeRunner interfaces, CreateOAuthProbeCron factory, OAuthProbeCronExpression config field wired into service.Run, 92% test coverage, CHANGELOG updated.'
container: agent-109-spec-024-oauth-probe-cron
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T17:15:00Z"
queued: "2026-05-13T17:34:36Z"
started: "2026-05-13T17:34:38Z"
branch: dark-factory/oauth-probe-weekly
---

<summary>
- The executor gains a new startup flag `OAuthProbeCronExpression` (default `0 0 8 * * 1`, Quartz 6-field, Mondays 08:00) for configuring the weekly OAuth probe cadence; a parse failure at startup is fatal
- A new `pkg/probe` package holds the probe-loop logic: list all Config CRs via the existing provider, then for each agent publish one `create-task` + one `update-frontmatter` command to the existing `agent-task-v1-request` topic
- The `create-task` command idempotently bootstraps a per-agent `tasks/probe-<agent>.md` vault file on the first tick (spec-019 no-op on repeated runs with the same `task_identifier`)
- The `update-frontmatter` command resets `phase: planning, trigger_count: 0, retry_count: 0` — this is the actual trigger that causes the controller to emit a new `agent-task-v1-event` and the executor to spawn a fresh agent Job
- A `CommandPublisher` interface in the probe package abstracts command serialisation, making the inner probe loop unit-testable without complex `cdb.CommandObject` construction in tests
- A new `CreateOAuthProbeCron` factory function in `pkg/factory/factory.go` wires the probe loop with `cron.NewExpressionCron` from `github.com/bborbe/cron` — zero business logic, pure composition
- The probe `run.Func` is appended as the fifth goroutine in the existing `service.Run(...)` call in `main.go`
- Four unit tests cover the AC cases: N-Config tick produces 2N ordered commands; empty lister is a no-op; first-publish error propagates; second-publish error propagates after first succeeds (no rollback)
- Changes are confined to `task/executor/` module and root `CHANGELOG.md`
</summary>

<objective>
Add a weekly cron to `task/executor` that publishes two Kafka commands per known Config CR on each tick — `create-task` (idempotent bootstrap) and `update-frontmatter` (resets phase and counters to trigger respawn). Each resulting Job exercises the agent's OAuth credentials via `reply 'ok'`, rotating `.credentials.json` as a side effect. Failed probes surface through the existing `human_review` route. New agents are auto-enrolled at the next tick. No new K8s API calls, no new Kafka topics, no new alert surface — the probe loop is purely a Kafka producer of two existing command types.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, counterfeiter annotations, error wrapping
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `Create*` prefix, zero logic, pure composition
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`, never `fmt.Errorf`, never bare `context.Background()` in pkg/
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, suite files, coverage ≥80%
- `go-concurrency-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `run.CancelOnFirstErrorWait` vs `service.Run`, run.Func usage
- `go-context-cancellation-in-loops.md` in `~/.claude/plugins/marketplaces/coding/docs/` — non-blocking select in loops
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**

- `task/executor/main.go` — application config struct and `Run()` method; the `service.Run(...)` call at line 110 receives the fifth goroutine
- `task/executor/pkg/factory/factory.go` — factory file receiving `CreateOAuthProbeCron`; study the existing `Create*` patterns and import block
- `task/executor/pkg/result_publisher.go` — provides the **exact import block and `publishRaw` pattern** to replicate in the probe package; read in full before writing any command-publishing code
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` — provides the `Config` type; the probe loop reads `config.Spec.Assignee` as the agent name

**Inline reference — current `service.Run(...)` call (main.go:110-122):**
```go
return service.Run(
    ctx,
    func(ctx context.Context) error {
        return connector.Listen(ctx, a.Namespace, resourceEventHandler)
    },
    func(ctx context.Context) error {
        return consumer.Consume(ctx)
    },
    func(ctx context.Context) error {
        return jobWatcher.Run(ctx)
    },
    a.createHTTPServer(eventHandlerConfig),
)
```
The probe cron becomes the fifth `run.Func` argument (`probeCron.Run`).

**Inline reference — `publishRaw` from result_publisher.go (replicate this pattern in the probe's `commandPublisher.Publish`):**
```go
func (p *resultPublisher) publishRaw(
    ctx context.Context,
    operation base.CommandOperation,
    payload interface{},
) error {
    event, err := base.ParseEvent(ctx, payload)
    if err != nil {
        return errors.Wrapf(ctx, err, "parse event for operation %s", operation)
    }
    requestIDCh := make(chan base.RequestID, 1)
    requestIDCh <- base.NewRequestID()
    commandCreator := base.NewCommandCreator(requestIDCh)
    commandObject := cdb.CommandObject{
        Command: commandCreator.NewCommand(
            operation,
            cqrsiam.Initiator("executor"),
            "",
            event,
        ),
        SchemaID: lib.TaskV1SchemaID,
    }
    if err := p.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
        return errors.Wrapf(ctx, err, "send command for operation %s", operation)
    }
    return nil
}
```
Read the **full import block** of `result_publisher.go` before writing probe.go — copy the exact import paths for `base`, `cqrsiam`, and `cdb`.

**Inline reference — Config.Spec.Assignee (types.go):**
```go
type ConfigSpec struct {
    Assignee string `json:"assignee"`
    Image    string `json:"image"`
    // ...
}
```
The agent name in `probe-<agent-name>` is `config.Spec.Assignee`.

**Inline reference — CreateCommand and UpdateFrontmatterCommand (lib/command/task/):**
```go
// create-command.go
const CreateCommandOperation base.CommandOperation = "create-task"
type CreateCommand struct {
    TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
    Title          string              `json:"title"`
    Frontmatter    lib.TaskFrontmatter `json:"frontmatter"`
    Body           string              `json:"body,omitempty"`
}

// update-frontmatter-command.go
const UpdateFrontmatterCommandOperation base.CommandOperation = "update-frontmatter"
type UpdateFrontmatterCommand struct {
    TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
    Updates        lib.TaskFrontmatter `json:"updates"`
    Body           *BodySection        `json:"body,omitempty"`
}
```

**Inline reference — bborbe/cron API (v1.8.17):**
```go
// Expression is a string type
type Expression string

// NewExpressionCron wraps action with a cron scheduler. Returns run.Runnable.
func NewExpressionCron(expression Expression, action run.Runnable) run.Runnable
```
The returned `run.Runnable` starts the scheduler when `.Run(ctx)` is called; invalid expressions cause `Run` to return an error immediately, which `service.Run` propagates — satisfying the "fatal startup error" requirement.

**Symbol verification — before importing any cqrs or k8s symbol, run:**
```bash
grep -rn "type CommandObjectSender\b" $GOPATH/pkg/mod/github.com/bborbe/cqrs@*/cdb/ 2>/dev/null | head -5
grep -rn "func NewCommandObjectSender\b" $GOPATH/pkg/mod/github.com/bborbe/cqrs@*/cdb/ 2>/dev/null | head -5
grep -rn "type EventHandler\b\|type Provider\b" $GOPATH/pkg/mod/github.com/bborbe/k8s@*/k8s_event-handler-type.go 2>/dev/null | head -10
```
</context>

<requirements>

## 1. Add `github.com/bborbe/cron` as a direct dependency

```bash
cd task/executor && go get github.com/bborbe/cron@v1.8.17
cd task/executor && make ensure
```

Verify:
```bash
grep "bborbe/cron" task/executor/go.mod
```
Expected: a direct entry without `// indirect`.

## 2. Create `task/executor/pkg/probe/probe.go`

Create this new file. Package name: `probe`. License header required (copy from another pkg file).

The file defines three interfaces with counterfeiter annotations, their implementations, and one constructor.

**ConfigProvider** — thin alias for `k8s.Provider[agentv1.Config]` (allows passing `EventHandlerConfig` directly):
```go
//counterfeiter:generate . ConfigProvider
type ConfigProvider interface {
    Get(ctx context.Context) ([]agentv1.Config, error)
}
```

**CommandPublisher** — abstracts command serialisation; takes the operation as a plain string so tests can assert without constructing `cdb.CommandObject`:
```go
//counterfeiter:generate . CommandPublisher
type CommandPublisher interface {
    Publish(ctx context.Context, operation string, payload interface{}) error
}
```

**commandPublisher** — real implementation that replicates the `publishRaw` pattern from `result_publisher.go`:
```go
type commandPublisher struct {
    commandObjectSender cdb.CommandObjectSender
}

func NewCommandPublisher(sender cdb.CommandObjectSender) CommandPublisher {
    return &commandPublisher{commandObjectSender: sender}
}

func (p *commandPublisher) Publish(ctx context.Context, operation string, payload interface{}) error {
    // replicate publishRaw exactly; convert string → base.CommandOperation(operation)
    event, err := base.ParseEvent(ctx, payload)
    if err != nil {
        return errors.Wrapf(ctx, err, "parse event for operation %s", operation)
    }
    requestIDCh := make(chan base.RequestID, 1)
    requestIDCh <- base.NewRequestID()
    commandCreator := base.NewCommandCreator(requestIDCh)
    commandObject := cdb.CommandObject{
        Command: commandCreator.NewCommand(
            base.CommandOperation(operation),
            cqrsiam.Initiator("executor"),
            "",
            event,
        ),
        SchemaID: lib.TaskV1SchemaID,
    }
    if err := p.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
        return errors.Wrapf(ctx, err, "send command for operation %s", operation)
    }
    return nil
}
```

**OAuthProbeRunner** interface and implementation:
```go
//counterfeiter:generate . OAuthProbeRunner
type OAuthProbeRunner interface {
    Run(ctx context.Context) error
}

type oAuthProbeRunner struct {
    configProvider ConfigProvider
    publisher      CommandPublisher
}

func NewOAuthProbeRunner(configProvider ConfigProvider, publisher CommandPublisher) OAuthProbeRunner {
    return &oAuthProbeRunner{
        configProvider: configProvider,
        publisher:      publisher,
    }
}
```

**Run method** (the inner probe loop — called once per cron tick):
```go
func (r *oAuthProbeRunner) Run(ctx context.Context) error {
    configs, err := r.configProvider.Get(ctx)
    if err != nil {
        return errors.Wrap(ctx, err, "list configs")
    }
    for _, config := range configs {
        agentName := config.Spec.Assignee
        taskID := lib.TaskIdentifier("probe-" + agentName)

        // Publish create-task (idempotent bootstrap — spec-019 no-op if file exists)
        createCmd := taskcmd.CreateCommand{
            TaskIdentifier: taskID,
            Title:          "probe-" + agentName,
            Frontmatter: lib.TaskFrontmatter{
                "task_type": "oauth-probe",
                "status":    "in_progress",
                "phase":     "planning",
                "assignee":  agentName,
            },
            Body: "reply 'ok'",
        }
        if err := r.publisher.Publish(ctx, string(taskcmd.CreateCommandOperation), createCmd); err != nil {
            return errors.Wrapf(ctx, err, "publish create-task for %s", agentName)
        }

        // Publish update-frontmatter (actual re-trigger: resets phase + counters)
        updateCmd := taskcmd.UpdateFrontmatterCommand{
            TaskIdentifier: taskID,
            Updates: lib.TaskFrontmatter{
                "phase":         "planning",
                "trigger_count": 0,
                "retry_count":   0,
            },
        }
        if err := r.publisher.Publish(ctx, string(taskcmd.UpdateFrontmatterCommandOperation), updateCmd); err != nil {
            return errors.Wrapf(ctx, err, "publish update-frontmatter for %s", agentName)
        }
    }
    return nil
}
```

**Imports:** Use `result_publisher.go`'s import block as a reference for the exact `base` / `cqrsiam` / `cdb` / `lib` paths. Only import what `probe.go` actually references — `goimports` (run by `make precommit`) will drop unused imports anyway, so a minimal hand-curated import list is preferable to copy-paste-then-trim.

**Context-cancellation note:** The `Run` loop iterates `configs` which is bounded by the number of Config CRs (typically < 20). No long-running I/O inside the loop body beyond the two `Publish` calls. A non-blocking `select` is NOT required per the context-cancellation guide (applies only to loops with significant runtime). If configs grow large in the future, it can be added then.

## 3. Create `task/executor/pkg/probe/probe_suite_test.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProbe(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Probe Suite")
}
```

## 4. Run `make generate` to produce counterfeiter mocks

```bash
cd task/executor && make generate
```

This processes the `//counterfeiter:generate` annotations in `pkg/probe/probe.go` and writes mocks under `task/executor/pkg/probe/mocks/`. Verify:
```bash
ls task/executor/pkg/probe/mocks/
```
Expected: at least `fake_config_provider.go`, `fake_command_publisher.go`, `fake_o_auth_probe_runner.go`.

If `make generate` also regenerates other mocks in the module (e.g. `mocks/` at module root), that is expected and correct — commit the drift.

## 5. Create `task/executor/pkg/probe/probe_test.go`

Package: `probe_test`. Uses the counterfeiter mocks from step 4.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	taskcmd "github.com/bborbe/agent/lib/command/task"
	agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"
	"github.com/bborbe/agent/task/executor/pkg/probe"
	"github.com/bborbe/agent/task/executor/pkg/probe/mocks"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("OAuthProbeRunner", func() {
	var (
		ctx            context.Context
		configProvider *mocks.FakeConfigProvider
		publisher      *mocks.FakeCommandPublisher
		runner         probe.OAuthProbeRunner
	)

	BeforeEach(func() {
		ctx = context.Background()
		configProvider = new(mocks.FakeConfigProvider)
		publisher = new(mocks.FakeCommandPublisher)
		runner = probe.NewOAuthProbeRunner(configProvider, publisher)
	})

	Context("N configs produce 2N commands in the expected order", func() {
		BeforeEach(func() {
			configProvider.GetReturns([]agentv1.Config{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
					Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-b"},
					Spec:       agentv1.ConfigSpec{Assignee: "agent-b"},
				},
			}, nil)
		})

		It("calls Publish exactly 4 times for 2 configs", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			Expect(publisher.PublishCallCount()).To(Equal(4))
		})

		It("first call is create-task for agent-a", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			_, op, _ := publisher.PublishArgsForCall(0)
			Expect(op).To(Equal("create-task"))
		})

		It("second call is update-frontmatter for agent-a", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			_, op, _ := publisher.PublishArgsForCall(1)
			Expect(op).To(Equal("update-frontmatter"))
		})

		It("third call is create-task for agent-b", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			_, op, _ := publisher.PublishArgsForCall(2)
			Expect(op).To(Equal("create-task"))
		})

		It("fourth call is update-frontmatter for agent-b", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			_, op, _ := publisher.PublishArgsForCall(3)
			Expect(op).To(Equal("update-frontmatter"))
		})

		It("returns no error", func() {
			Expect(runner.Run(ctx)).To(Succeed())
		})
	})

	Context("empty lister", func() {
		BeforeEach(func() {
			configProvider.GetReturns([]agentv1.Config{}, nil)
		})

		It("produces zero Publish calls", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			Expect(publisher.PublishCallCount()).To(Equal(0))
		})

		It("returns no error", func() {
			Expect(runner.Run(ctx)).To(Succeed())
		})
	})

	Context("create-task publish fails", func() {
		BeforeEach(func() {
			configProvider.GetReturns([]agentv1.Config{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
					Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
				},
			}, nil)
			publisher.PublishReturnsOnCall(0, fmt.Errorf("kafka unavailable"))
		})

		It("returns a wrapped error", func() {
			err := runner.Run(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kafka unavailable"))
		})

		It("makes exactly 1 Publish call — no rollback", func() {
			runner.Run(ctx)
			Expect(publisher.PublishCallCount()).To(Equal(1))
		})
	})

	Context("update-frontmatter publish fails after create-task succeeded", func() {
		BeforeEach(func() {
			configProvider.GetReturns([]agentv1.Config{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
					Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
				},
			}, nil)
			publisher.PublishReturnsOnCall(0, nil)                              // create-task succeeds
			publisher.PublishReturnsOnCall(1, fmt.Errorf("write timeout")) // update-frontmatter fails
		})

		It("returns a wrapped error containing the timeout message", func() {
			err := runner.Run(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("write timeout"))
		})

		It("makes exactly 2 Publish calls — no rollback", func() {
			runner.Run(ctx)
			Expect(publisher.PublishCallCount()).To(Equal(2))
		})
	})

	Context("emitted commands satisfy library validation (boundary contract)", func() {
		BeforeEach(func() {
			configProvider.GetReturns([]agentv1.Config{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
					Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
				},
			}, nil)
		})

		It("the create-task payload passes CreateCommand.Validate", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			_, _, payload := publisher.PublishArgsForCall(0)
			createCmd, ok := payload.(taskcmd.CreateCommand)
			Expect(ok).To(BeTrue(), "payload at call 0 must be a CreateCommand")
			Expect(createCmd.Validate(ctx)).To(Succeed())
		})

		It("the update-frontmatter payload passes UpdateFrontmatterCommand.Validate", func() {
			Expect(runner.Run(ctx)).To(Succeed())
			_, _, payload := publisher.PublishArgsForCall(1)
			updateCmd, ok := payload.(taskcmd.UpdateFrontmatterCommand)
			Expect(ok).To(BeTrue(), "payload at call 1 must be an UpdateFrontmatterCommand")
			Expect(updateCmd.Validate(ctx)).To(Succeed())
		})
	})
})
```

**Why the boundary-validation tests matter:** the controller's CommandObjectExecutors call `.Validate(ctx)` on each consumed command. Shape-only tests (assert what was passed to the fake) would pass even if `probe-<agent>` ever violated title char rules or if the frontmatter map omitted a required key. The two `It`s above traverse the same validation boundary the controller traverses at consume time, catching the canonical "compiled fine, fails at runtime" class before deploy. If `taskcmd.CreateCommand` or `UpdateFrontmatterCommand` does not yet expose a `Validate(ctx) error` method, search for the actual method name (`Check(ctx)`, `Validate()` without ctx, etc.) in `lib/command/task/*.go` and use that instead — the contract is the same, only the receiver differs.

**Note on mock method signatures:** Counterfeiter generates `PublishArgsForCall(i int)` returning `(ctx context.Context, operation string, payload interface{})` — adjust the tuple destructuring if the generated mock uses a different order. Run `grep -n "func.*PublishArgsForCall" task/executor/pkg/probe/mocks/fake_command_publisher.go` to verify the exact return order before writing assertions.

## 6. Add `CreateOAuthProbeCron` to `task/executor/pkg/factory/factory.go`

Read the full file before editing. Add a new factory function at the end of the file (after the last existing `Create*` function):

```go
func CreateOAuthProbeCron(
	expression libcron.Expression,
	configProvider pkg.EventHandlerConfig,
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
) run.Runnable {
	sender := cdb.NewCommandObjectSender(syncProducer, branch, log.DefaultSamplerFactory)
	publisher := probe.NewCommandPublisher(sender)
	runner := probe.NewOAuthProbeRunner(configProvider, publisher)
	return libcron.NewExpressionCron(expression, runner)
}
```

Add the following imports (using the `lib*` alias convention already in the file):
- `libcron "github.com/bborbe/cron"` — add to imports
- `"github.com/bborbe/agent/task/executor/pkg/probe"` — add to imports

Verify `cdb`, `base`, `log`, and `libkafka` are already imported in factory.go (they are used by `CreateConsumer`). If not, add them copying from `result_publisher.go`.

**Verify before importing:**
```bash
grep -rn "func NewExpressionCron\b" $GOPATH/pkg/mod/github.com/bborbe/cron@v1.8.17/ 2>/dev/null | head -3
grep -rn "func NewCommandObjectSender\b" $GOPATH/pkg/mod/github.com/bborbe/cqrs@*/cdb/ 2>/dev/null | head -3
```

**Note:** `pkg.EventHandlerConfig` is `k8s.EventHandler[agentv1.Config]`, which embeds `k8s.Provider[agentv1.Config]` and therefore satisfies `probe.ConfigProvider`. No adapter needed.

**Factory zero-logic check:** `CreateOAuthProbeCron` contains no conditionals, no loops, no I/O — it is pure composition. ✓

## 7. Update `task/executor/main.go`

Two targeted changes:

**a. Add `OAuthProbeCronExpression` field to the application config struct.**

Find the existing struct (the one with `SentryDSN`, `Listen`, `KafkaBrokers`, `Branch`, `Namespace`, `BuildGitVersion` fields). Add the new field at the end of the struct, before the closing brace:

```go
OAuthProbeCronExpression string `arg:"oauth-probe-cron-expression" env:"OAUTH_PROBE_CRON_EXPRESSION" default:"0 0 8 * * 1" usage:"Cron expression for Claude OAuth health probes"`
```

**b. Wire the probe cron into `service.Run(...)`.**

In the `Run()` method, after the line that creates `resultPublisher` (around line 93), add:

```go
probeCron := factory.CreateOAuthProbeCron(
    libcron.Expression(a.OAuthProbeCronExpression),
    eventHandlerConfig,
    syncProducer,
    a.Branch,
)
```

Append `probeCron.Run` as the fifth argument to `service.Run`:

```go
return service.Run(
    ctx,
    func(ctx context.Context) error {
        return connector.Listen(ctx, a.Namespace, resourceEventHandler)
    },
    func(ctx context.Context) error {
        return consumer.Consume(ctx)
    },
    func(ctx context.Context) error {
        return jobWatcher.Run(ctx)
    },
    a.createHTTPServer(eventHandlerConfig),
    probeCron.Run,
)
```

Add `libcron "github.com/bborbe/cron"` to the import block.

**Startup validation:** If `OAuthProbeCronExpression` cannot be parsed, `probeCron.Run(ctx)` returns an error on the first call. `service.Run` propagates it and the process exits — satisfying the AC "Cron expression fails to parse at startup → executor refuses to start."

## 8. Update `CHANGELOG.md` at repo root

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header:

```markdown
## Unreleased

- feat(task/executor): add weekly OAuth probe cron (`OAUTH_PROBE_CRON_EXPRESSION`, default `0 0 8 * * 1`) — publishes `create-task` + `update-frontmatter` commands per Config CR on each tick to keep agent PVC OAuth credentials warm; failed probes escalate via existing `human_review` route; new agents auto-enrolled at next tick
```

## 9. Run iterative tests

```bash
cd task/executor && make test
```

Fix compile errors before continuing. Common issues:
- Missing imports in `probe.go` — copy full import block from `result_publisher.go` as starting point
- `metav1` import path in test file: `k8s.io/apimachinery/pkg/apis/meta/v1`
- Mock method name mismatches — check generated mock file names with `ls task/executor/pkg/probe/mocks/`
- `PublishArgsForCall` tuple order — grep the generated file to verify return value order

## 10. Check test coverage for `pkg/probe`

```bash
cd task/executor && go test -coverprofile=/tmp/probe-cover.out ./pkg/probe/... && go tool cover -func=/tmp/probe-cover.out | grep -E "probe|total"
```

Coverage for the `oAuthProbeRunner.Run` method and its branches must be ≥80%. The four behavioural tests plus the validation contract tests cover: success path, empty path, first-error path, second-error path, plus library-validation contract for both command types.

**Do NOT pass `-mod=vendor`** here. Step 1's `make ensure` runs `rm -rf vendor` in the executor module, so the `vendor/` tree is absent during iterative testing; the default `-mod=mod` is correct. `make precommit` in step 11 regenerates and re-uses vendor as part of its own pipeline.

## 11. Run final precommit

```bash
cd task/executor && make precommit
```

Must exit 0. If any linter fails, run only the failing target (e.g., `make lint`, `make gosec`, `make errcheck`) and fix before retrying `make precommit`.

If `make precommit` reports generate drift after codegen: re-run `make generate`, verify the diff is expected (new mocks only), then re-run the failing target.

</requirements>

<constraints>
- Change is confined to the `task/executor` module (its `main.go`, `pkg/factory/factory.go`, new `pkg/probe/` package and tests) and root `CHANGELOG.md`. No file in `lib/*`, `task/controller/*`, `agent/*`, or `prompt/*` is modified. No downstream `go.mod` is bumped.
- The `Config` CR schema is NOT changed. No new CRD fields.
- Task IDs are `probe-<agent-name>` where `agent-name` is `config.Spec.Assignee` — stable per agent, bounded by the number of Config CRs.
- The probe loop publishes to the same `agent-task-v1-request` topic (via existing `syncProducer` + `branch`). No new Kafka topic.
- `CreateOAuthProbeCron` factory function has zero business logic: no conditionals, no loops, no I/O. It is pure composition.
- The `CommandPublisher` interface takes `operation string` (not `base.CommandOperation`) to keep the probe package free of cqrs internals in its interface surface. The real implementation converts internally via `base.CommandOperation(operation)`.
- Cron expression default `0 0 8 * * 1` is Quartz 6-field format (Mondays 08:00). An invalid expression is a fatal startup error.
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks. External test package `probe_test`.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`, never bare `context.Background()` in pkg/ code.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required.
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify `bborbe/cron` is a direct dependency:
```bash
grep "bborbe/cron" task/executor/go.mod
```
Expected: direct entry (no `// indirect`).

Verify `OAuthProbeCronExpression` field exists in main.go:
```bash
grep -n "OAuthProbeCronExpression\|oauth-probe-cron-expression\|OAUTH_PROBE_CRON_EXPRESSION" task/executor/main.go
```
Expected: three matches (field name, arg tag, env tag).

Verify `CreateOAuthProbeCron` factory function exists:
```bash
grep -n "func CreateOAuthProbeCron" task/executor/pkg/factory/factory.go
```
Expected: one function definition.

Verify probe package interfaces exist:
```bash
grep -n "type OAuthProbeRunner\b\|type ConfigProvider\b\|type CommandPublisher\b" task/executor/pkg/probe/probe.go
```
Expected: three interface definitions.

Verify counterfeiter mocks were generated:
```bash
ls task/executor/pkg/probe/mocks/
```
Expected: at least three files (FakeConfigProvider, FakeCommandPublisher, FakeOAuthProbeRunner).

Verify probe cron is wired into service.Run:
```bash
grep -n "probeCron\|CreateOAuthProbeCron" task/executor/main.go
```
Expected: creation line and wiring into service.Run.

Verify CHANGELOG updated:
```bash
grep -n -i "oauth probe\|OAuthProbe\|oauth-probe" CHANGELOG.md | head -5
```
Expected: at least one match under `## Unreleased`.

Run tests:
```bash
cd task/executor && make test
```
Expected: exit 0, all specs pass including the new `Probe Suite`.

Run coverage:
```bash
cd task/executor && go test -coverprofile=/tmp/probe-cover.out ./pkg/probe/... && go tool cover -func=/tmp/probe-cover.out | grep "total:"
```
Expected: ≥80% total coverage for the probe package. Do NOT pass `-mod=vendor` — `make ensure` in step 1 removed `vendor/`; default mode is correct here.

Run precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
