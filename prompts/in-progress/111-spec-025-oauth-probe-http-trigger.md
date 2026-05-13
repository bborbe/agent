---
status: executing
spec: [025-oauth-probe-http-trigger]
container: agent-111-spec-025-oauth-probe-http-trigger
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T19:15:00Z"
queued: "2026-05-13T19:41:16Z"
started: "2026-05-13T19:41:18Z"
branch: dark-factory/oauth-probe-http-trigger
---

<summary>
- The executor gains a POST `/oauth-probe/trigger` HTTP endpoint that fires the same OAuth probe loop the weekly cron fires — no restart, no env-var override needed for ad-hoc probes
- The probe runner instance is shared: cron and HTTP handler both invoke the same `probe.OAuthProbeRunner` object — no duplicate construction, no divergence in probe behavior
- The endpoint is fire-and-forget: the HTTP response (200, fixed acknowledgement body) is returned immediately without waiting for the probe loop to finish
- Single-flight semantics: a second invocation while the first probe loop is in-flight is silently dropped — the in-flight loop continues uninterrupted; OAuth quota waste is bounded regardless of caller behavior
- The change is confined to `task/executor/`: factory refactor, main.go wiring, one new handler file, one new test file; probe logic and shared libraries are untouched
- All previously-registered executor HTTP endpoints (`/healthz`, `/readiness`, `/metrics`, `/agents`, `/setloglevel/{level}`) remain registered and functional
</summary>

<objective>
Add a POST `/oauth-probe/trigger` HTTP endpoint to the `task/executor` service that fires the existing `probe.OAuthProbeRunner` on demand with fire-and-forget + single-flight semantics, sharing the same runner instance already used by the weekly cron. This lets operators verify OAuth token health in seconds without restarting the pod.
</objective>

<context>
Read `CLAUDE.md` at the repo root and `task/executor/CLAUDE.md` (if it exists) for project conventions.

Read these guides before starting:
- `go-http-handler-refactoring-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — handler location in `pkg/handler/`, factory naming, no inline handlers
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `Create*` prefix, zero business logic in factories
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, coverage ≥80%
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**

- `task/executor/main.go` — current `application` struct, `Run()` method, and `createHTTPServer()` method; understand all currently-registered routes and the `service.Run(...)` call
- `task/executor/pkg/factory/factory.go` — `CreateOAuthProbeCron` (currently builds runner internally — must be refactored); all existing `Create*` patterns and import block
- `task/executor/pkg/probe/probe.go` — `OAuthProbeRunner` interface and `NewOAuthProbeRunner` constructor; `CommandPublisher` and `ConfigProvider` interfaces; this file is NOT modified
- `task/executor/pkg/handler/agents_handler.go` — existing handler pattern (constructor function returns `http.Handler`, private struct implements `ServeHTTP`)
- `task/executor/pkg/handler/task_event_handler_test.go` — existing test file structure: combined `TestHandler` bootstrap + `Describe` blocks in `package handler_test`; no separate suite file

**Inline reference — `NewBackgroundRunHandler` in `github.com/bborbe/http` v1.26.12 (already a direct dependency):**
```go
// NewBackgroundRunHandler creates an HTTP handler that executes a run.Func in the background.
// The handler uses a ParallelSkipper to prevent multiple concurrent executions (single-flight).
// Returns HTTP 200 immediately; the runFunc runs in a goroutine.
func NewBackgroundRunHandler(ctx context.Context, runFunc run.Func) http.Handler
```
This library function provides both fire-and-forget AND single-flight. The `ParallelSkipper` is created once per handler construction (shared across all requests to that handler instance), so a second HTTP request while the first goroutine is inside `runFunc` is silently dropped.

Verify the symbol exists before writing:
```bash
grep -rn "func NewBackgroundRunHandler" $(go env GOPATH)/pkg/mod/github.com/bborbe/http@v1.26.*/... 2>/dev/null | head -3
```

**Inline reference — existing `CreateOAuthProbeCron` in `factory.go` (full body, before refactor):**
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

**Inline reference — existing `probeCron` wiring in `main.go` (before refactor):**
```go
probeCron := factory.CreateOAuthProbeCron(
    libcron.Expression(a.OAuthProbeCronExpression),
    eventHandlerConfig,
    syncProducer,
    a.Branch,
)
```

**Inline reference — existing `service.Run(...)` call in `main.go` (before refactor):**
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

**Inline reference — existing `createHTTPServer` signature in `main.go` (before refactor):**
```go
func (a *application) createHTTPServer(configProvider pkg.EventHandlerConfig) run.Func {
    return func(ctx context.Context) error {
        router := mux.NewRouter()
        router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
        router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
        router.Path("/metrics").Handler(promhttp.Handler())
        router.Path("/agents").Handler(handler.NewAgentsHandler(configProvider))
        router.Path("/setloglevel/{level}").
            Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))
        glog.V(2).Infof("starting http server listen on %s", a.Listen)
        return libhttp.NewServer(a.Listen, router).Run(ctx)
    }
}
```

**Inline reference — `FakeOAuthProbeRunner` mock location (already generated by spec-024):**
```
task/executor/pkg/probe/mocks/fake_o_auth_probe_runner.go — fake name FakeOAuthProbeRunner
```
Verify it exists before writing tests:
```bash
grep -n "func.*FakeOAuthProbeRunner\|RunCallCount\|RunStub" task/executor/pkg/probe/mocks/fake_o_auth_probe_runner.go | head -10
```
</context>

<requirements>

## 1. Refactor `task/executor/pkg/factory/factory.go`

Read the full file before editing. Two targeted changes:

**a. Extract runner construction into a new `CreateOAuthProbeRunner` factory function.**

Add immediately before `CreateOAuthProbeCron`:

```go
// CreateOAuthProbeRunner creates the OAuth probe runner shared between the cron path and the
// HTTP trigger path. Callers must pass the same instance to both CreateOAuthProbeCron and
// the HTTP handler so probe behavior is identical regardless of invocation path.
func CreateOAuthProbeRunner(
	configProvider pkg.EventHandlerConfig,
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
) probe.OAuthProbeRunner {
	sender := cdb.NewCommandObjectSender(syncProducer, branch, log.DefaultSamplerFactory)
	publisher := probe.NewCommandPublisher(sender)
	return probe.NewOAuthProbeRunner(configProvider, publisher)
}
```

**b. Simplify `CreateOAuthProbeCron` to accept a runner instead of building it.**

Replace the existing `CreateOAuthProbeCron` body (which builds sender/publisher/runner internally) with:

```go
// CreateOAuthProbeCron wraps the given runner in a cron scheduler. Pass the runner returned by
// CreateOAuthProbeRunner so the cron and the HTTP handler share the same instance.
func CreateOAuthProbeCron(
	expression libcron.Expression,
	runner probe.OAuthProbeRunner,
) run.Runnable {
	return libcron.NewExpressionCron(expression, runner)
}
```

Remove the now-unused parameters `configProvider pkg.EventHandlerConfig`, `syncProducer libkafka.SyncProducer`, `branch base.Branch` from `CreateOAuthProbeCron`. The imports for `cdb`, `log`, and `libkafka` must remain because they are still used by `CreateConsumer` and `CreateOAuthProbeRunner`. Verify no import is dropped accidentally.

**Zero-logic check:** After refactor, `CreateOAuthProbeCron` has zero conditionals, zero I/O — pure composition. ✓

## 2. Update `task/executor/main.go`

Two targeted changes to `Run()` and `createHTTPServer()`:

**a. Create `oAuthProbeRunner` before `probeCron`, pass the same instance to both.**

Replace the existing `probeCron` creation block:
```go
// OLD:
probeCron := factory.CreateOAuthProbeCron(
    libcron.Expression(a.OAuthProbeCronExpression),
    eventHandlerConfig,
    syncProducer,
    a.Branch,
)
```
with:
```go
// NEW:
oAuthProbeRunner := factory.CreateOAuthProbeRunner(
    eventHandlerConfig,
    syncProducer,
    a.Branch,
)
probeCron := factory.CreateOAuthProbeCron(
    libcron.Expression(a.OAuthProbeCronExpression),
    oAuthProbeRunner,
)
```

**b. Pass `oAuthProbeRunner` to `createHTTPServer`.**

Change the `service.Run(...)` call to pass the runner:
```go
a.createHTTPServer(eventHandlerConfig, oAuthProbeRunner),
```

**c. Update `createHTTPServer` signature and add the new route.**

Change the method signature to accept the runner:
```go
func (a *application) createHTTPServer(configProvider pkg.EventHandlerConfig, runner probe.OAuthProbeRunner) run.Func {
```

Inside the returned closure, add the new route after the existing `/agents` route and before `/setloglevel/{level}`:
```go
router.Path("/oauth-probe/trigger").Methods(http.MethodPost).Handler(
    handler.NewOAuthProbeTriggerHandler(ctx, runner),
)
```

Add the required import for `probe` package. Check the existing import block — `"github.com/bborbe/agent/task/executor/pkg/probe"` must be added if not already present. The `handler` import is already there (`"github.com/bborbe/agent/task/executor/pkg/handler"`).

After editing, verify the `createHTTPServer` closure captures `ctx` (the parameter of the returned `run.Func`) and passes it to `NewOAuthProbeTriggerHandler`. The `ctx` is already used inside the closure for `log.NewSetLoglevelHandler` — so it is available.

## 3. Create `task/executor/pkg/handler/oauth_probe_trigger_handler.go`

New file. License header required (copy from `agents_handler.go`).

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"net/http"

	libhttp "github.com/bborbe/http"

	"github.com/bborbe/agent/task/executor/pkg/probe"
)

// NewOAuthProbeTriggerHandler returns an HTTP handler that fires the OAuth probe runner
// once per invocation with fire-and-forget + single-flight semantics.
// Concurrent invocations collapse into one in-flight run (second request is silently dropped).
func NewOAuthProbeTriggerHandler(ctx context.Context, runner probe.OAuthProbeRunner) http.Handler {
	return libhttp.NewBackgroundRunHandler(ctx, runner.Run)
}
```

Verify the import path for `libhttp` matches what is used elsewhere in the module:
```bash
grep -rn '"github.com/bborbe/http"' task/executor/main.go task/executor/pkg/
```

## 4. Create `task/executor/pkg/handler/oauth_probe_trigger_handler_test.go`

New test file. Package: `handler_test`. Add test cases to the existing handler test suite (the `TestHandler` function in `task_event_handler_test.go` already bootstraps Ginkgo for the package — do NOT add a second `TestHandler` or `RunSpecs` call).

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/task/executor/pkg/handler"
	"github.com/bborbe/agent/task/executor/pkg/probe/mocks"
)

var _ = Describe("OAuthProbeTriggerHandler", func() {
	var (
		ctx        context.Context
		fakeRunner *mocks.FakeOAuthProbeRunner
		h          http.Handler
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeRunner = new(mocks.FakeOAuthProbeRunner)
		h = handler.NewOAuthProbeTriggerHandler(ctx, fakeRunner)
	})

	Context("POST request", func() {
		It("returns HTTP 200", func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil)
			h.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("triggers the runner exactly once", func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil)
			h.ServeHTTP(w, req)
			Eventually(fakeRunner.RunCallCount).Should(Equal(1))
		})

		It("returns before the runner finishes (fire-and-forget)", func() {
			firstCallUnblock := make(chan struct{})
			fakeRunner.RunStub = func(ctx context.Context) error {
				<-firstCallUnblock
				return nil
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil)
			// ServeHTTP returns immediately even though the runner is blocked
			h.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
			// Cleanup: unblock to avoid goroutine leak
			close(firstCallUnblock)
		})
	})

	Context("single-flight: second invocation while first is in-flight", func() {
		var (
			firstCallStarted chan struct{}
			firstCallUnblock chan struct{}
		)

		BeforeEach(func() {
			firstCallStarted = make(chan struct{})
			firstCallUnblock = make(chan struct{})
			fakeRunner.RunStub = func(ctx context.Context) error {
				close(firstCallStarted) // signal: first call has started
				<-firstCallUnblock      // block until test unblocks
				return nil
			}
		})

		It("does not invoke the runner a second time", func() {
			// First request — fires runner in background (blocks on firstCallUnblock)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil))

			// Wait for first runner goroutine to enter RunStub
			Eventually(firstCallStarted).Should(BeClosed())

			// Second request while first is still running — must be a no-op
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil))

			// Critical: the second invocation's internal goroutine may not have reached the
			// single-flight check yet. Give it generous time to either be correctly skipped
			// OR (in a buggy implementation) invoke the runner — while the first call is still
			// blocked, the count must stay at 1. A buggy implementation would push it to 2 here.
			Consistently(fakeRunner.RunCallCount, "200ms", "20ms").Should(Equal(1))

			// Unblock the first call so the first goroutine can finish
			close(firstCallUnblock)

			// After unblock, runner was still called exactly once (no late second invocation)
			Eventually(fakeRunner.RunCallCount).Should(Equal(1))
		})

		It("returns HTTP 200 for the second request too", func() {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil))
			Eventually(firstCallStarted).Should(BeClosed())

			w2 := httptest.NewRecorder()
			h.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/oauth-probe/trigger", nil))
			Expect(w2.Code).To(Equal(http.StatusOK))

			close(firstCallUnblock)
		})
	})
})
```

**Note on mock method name:** The `FakeOAuthProbeRunner` stub field is `RunStub func(ctx context.Context) error`. Verify before writing:
```bash
grep -n "RunStub\|RunCallCount\|RunReturns" task/executor/pkg/probe/mocks/fake_o_auth_probe_runner.go | head -10
```
If the generated field names differ (e.g. `RunCalls` instead of `RunCallCount`), adjust accordingly.

## 5. Update `CHANGELOG.md` at repo root

Check whether `## Unreleased` already exists:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. Otherwise insert a new `## Unreleased` section immediately above the first `## v` header.

Add:
```markdown
- feat(task/executor): add POST `/oauth-probe/trigger` HTTP endpoint — fires the OAuth probe loop on demand with fire-and-forget and single-flight semantics; the runner instance is shared with the existing weekly cron so behavior is identical regardless of invocation path
```

## 6. Run iterative tests

```bash
cd task/executor && make test
```

Fix compile errors before continuing. Common issues:
- Import of `"github.com/bborbe/agent/task/executor/pkg/probe"` missing in `main.go` — add it
- `FakeOAuthProbeRunner` stub field name mismatch — grep the generated mock to confirm
- `handler.NewOAuthProbeTriggerHandler` not found — confirm the new file is in `pkg/handler/`

Check test coverage for changed packages:
```bash
cd task/executor && go test -coverprofile=/tmp/handler-cover.out ./pkg/handler/... && go tool cover -func=/tmp/handler-cover.out | grep -E "oauth_probe|total"
```
Coverage for `oauth_probe_trigger_handler.go` must be ≥80% (the new file has one function — all branches covered by the four `It` blocks above).

## 7. Run final precommit

```bash
cd task/executor && make precommit
```

Must exit 0. If any linter fails, run only the failing target (e.g. `make lint`) and fix before retrying.

</requirements>

<constraints>
- The `probe.OAuthProbeRunner` instance MUST be constructed once (via `factory.CreateOAuthProbeRunner`) and passed to both `factory.CreateOAuthProbeCron` and `handler.NewOAuthProbeTriggerHandler`. No duplicate construction.
- Probe behavior is frozen: do NOT modify `task/executor/pkg/probe/probe.go` or any shared library. All changes are confined to `task/executor/main.go`, `task/executor/pkg/factory/factory.go`, and new/updated files in `task/executor/pkg/handler/`.
- The HTTP endpoint path is `/oauth-probe/trigger`, method POST. Method filtering is enforced via gorilla/mux `.Methods(http.MethodPost)`.
- `NewOAuthProbeTriggerHandler` delegates entirely to `libhttp.NewBackgroundRunHandler` — no custom single-flight logic, no custom response body construction.
- `factory.CreateOAuthProbeCron` after refactor must have zero business logic: one line only (`return libcron.NewExpressionCron(expression, runner)`).
- All existing executor HTTP handlers (`/healthz`, `/readiness`, `/metrics`, `/agents`, `/setloglevel/{level}`) remain registered and unmodified.
- Test files: `package handler_test` (external test package). Do NOT add a second `TestHandler(t *testing.T)` — it already exists in `task_event_handler_test.go`.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`, never bare `context.Background()` in pkg/ code.
- Do NOT commit — dark-factory handles git.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify `CreateOAuthProbeRunner` factory function was added:
```bash
grep -n "func CreateOAuthProbeRunner" task/executor/pkg/factory/factory.go
```
Expected: one function definition returning `probe.OAuthProbeRunner`.

Verify `CreateOAuthProbeCron` was simplified (accepts runner, not configProvider/syncProducer/branch):
```bash
grep -A5 "func CreateOAuthProbeCron" task/executor/pkg/factory/factory.go
```
Expected: signature `(expression libcron.Expression, runner probe.OAuthProbeRunner) run.Runnable` and body is a single `return` statement.

Verify `oAuthProbeRunner` is created in `main.go`:
```bash
grep -n "oAuthProbeRunner\|CreateOAuthProbeRunner\|CreateOAuthProbeCron" task/executor/main.go
```
Expected: three matches — `CreateOAuthProbeRunner` call, `CreateOAuthProbeCron` call with runner, `createHTTPServer` call with runner.

Verify new route is registered:
```bash
grep -n "oauth-probe/trigger" task/executor/main.go
```
Expected: one match inside `createHTTPServer`.

Verify new handler file exists:
```bash
ls task/executor/pkg/handler/oauth_probe_trigger_handler.go task/executor/pkg/handler/oauth_probe_trigger_handler_test.go
```
Expected: both files present.

Verify `NewOAuthProbeTriggerHandler` delegates to `NewBackgroundRunHandler`:
```bash
grep -n "NewBackgroundRunHandler" task/executor/pkg/handler/oauth_probe_trigger_handler.go
```
Expected: one match.

Verify CHANGELOG updated:
```bash
grep -n "oauth-probe/trigger\|oauth probe trigger" CHANGELOG.md | head -5
```
Expected: at least one match under `## Unreleased`.

Run tests:
```bash
cd task/executor && make test
```
Expected: exit 0, `Handler Suite` passes with new OAuthProbeTriggerHandler specs.

Run coverage:
```bash
cd task/executor && go test -coverprofile=/tmp/handler-cover.out ./pkg/handler/... && go tool cover -func=/tmp/handler-cover.out | grep "total:"
```
Expected: ≥80% total coverage for `pkg/handler/`.

Run precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
