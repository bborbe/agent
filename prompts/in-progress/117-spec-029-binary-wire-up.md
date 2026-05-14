---
status: approved
spec: [029-per-agent-job-metrics-package]
created: "2026-05-14T10:00:00Z"
queued: "2026-05-14T09:22:44Z"
branch: dark-factory/per-agent-job-metrics-package
---

<summary>
- Each of the three agent binaries (`agent/claude`, `agent/code`, `agent/gemini`) gains exactly two new env fields: `PUSHGATEWAY_URL` (default `http://pushgateway:9090`) and `TASK_TYPE` (default `unknown`)
- A file-scope `const agentName` is added to each binary matching the K8s Deployment naming used by operators
- At the top of each binary's `Run()`, before existing logic: a fresh `*prometheus.Registry`, a `JobMetrics` instance, and a `*push.Pusher` configured with `agent` and `task_type` grouping labels are constructed; the pusher's `PushContext` is deferred so it fires on both success and failure paths
- Every existing return path inside `Run()` — infrastructure errors and the success path — is preceded by `metrics.RecordRun(status)` and `metrics.RecordDuration(time.Since(start))` before returning
- `github.com/prometheus/client_golang` is promoted to a direct dependency in each binary's `go.mod`; `github.com/bborbe/metrics` is added as a direct dependency
- The root `CHANGELOG.md` gains an `## Unreleased` section with one bullet describing the new package and the binary wire-up
- `make precommit` exits 0 in all three binary directories
</summary>

<objective>
Wire the `lib/metrics.JobMetrics` interface into the three agent binaries so every Job invocation pushes per-agent Prometheus metrics to the cluster PushGateway at the end of its `Run()` call. The metrics include run outcome (counter), last-run timestamp (gauge), and run duration (histogram), grouped by `agent` and `task_type` in the PushGateway grouping key.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — no new tests required in main.go

**Precondition check — run before implementing:**
```bash
ls lib/metrics/metrics.go lib/metrics/mocks/job-metrics.go
```
If either file is missing, STOP. Prompt 1 (`1-spec-029-lib-metrics.md`) must complete successfully before this prompt runs. Report `"status":"failed"` with reason "lib/metrics package not yet deployed (prompt 1)".

**Key files to read in full before editing:**
- `agent/claude/main.go` — current `application` struct and `Run()` method; all return paths
- `agent/code/main.go` — same
- `agent/gemini/main.go` — has an additional early error path (`CreateGeminiParser`) before existing deliverer logic
- `lib/metrics/metrics.go` — `JobMetrics` interface, `NewJobMetrics`, `BuildJobMetricsName`
- `lib/agent_status.go` — `AgentStatusFailed`, `AgentStatusDone`, etc.

**Inline reference — full current `agent/claude/main.go Run()` body:**
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
    glog.V(2).Infof("agent-claude started phase=%s", a.Phase)

    deliverer, cleanup, err := factory.CreateDeliverer(
        ctx, a.TaskID, a.KafkaBrokers, a.Branch, a.TaskContent,
    )
    if err != nil {
        return errors.Wrap(ctx, err, "create deliverer")
    }
    defer cleanup()

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
        return errors.Wrap(ctx, err, "agent run failed")
    }
    return agentlib.PrintResult(result)
}
```

There are THREE return paths: (1) deliverer creation error, (2) agent run error, (3) PrintResult.

**Inline reference — full current `agent/code/main.go Run()` body:**
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
    glog.V(2).Infof("agent-code started phase=%s", a.Phase)

    deliverer, cleanup, err := factory.CreateDeliverer(
        ctx, a.TaskID, a.KafkaBrokers, a.Branch, a.TaskContent,
    )
    if err != nil {
        return errors.Wrap(ctx, err, "create deliverer")
    }
    defer cleanup()

    result, err := factory.CreateAgent().Run(ctx, a.Phase, a.TaskContent, deliverer)
    if err != nil {
        return errors.Wrap(ctx, err, "agent run failed")
    }
    return agentlib.PrintResult(result)
}
```

Also THREE return paths, same pattern as claude.

**Inline reference — full current `agent/gemini/main.go Run()` body:**
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
    glog.V(2).Infof("agent-gemini started phase=%s", a.Phase)

    geminiParser, err := factory.CreateGeminiParser(ctx, a.GeminiAPIKey, a.GeminiModel)
    if err != nil {
        return errors.Wrap(ctx, err, "create gemini parser")
    }

    deliverer, cleanup, err := factory.CreateDeliverer(
        ctx, a.TaskID, a.KafkaBrokers, a.Branch, a.TaskContent,
    )
    if err != nil {
        return errors.Wrap(ctx, err, "create deliverer")
    }
    defer cleanup()

    result, err := factory.CreateAgent(geminiParser).Run(ctx, a.Phase, a.TaskContent, deliverer)
    if err != nil {
        return errors.Wrap(ctx, err, "agent run failed")
    }
    return agentlib.PrintResult(result)
}
```

FOUR return paths for gemini: (1) gemini parser error, (2) deliverer error, (3) agent run error, (4) PrintResult.

**Inline reference — new application struct fields (identical for all three binaries):**
```go
PushgatewayURL string `required:"false" arg:"pushgateway-url" env:"PUSHGATEWAY_URL" usage:"Prometheus PushGateway URL" default:"http://pushgateway:9090"`
TaskType       string `required:"false" arg:"task-type"       env:"TASK_TYPE"       usage:"Task type label for metric grouping" default:"unknown"`
```
Add these at the end of the existing struct body (before the closing brace), maintaining the existing struct-tag layout.

**Inline reference — metrics init block to insert at the TOP of `Run()` (before existing logic):**
```go
registry := prometheus.NewRegistry()
metrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
pusher := push.New(a.PushgatewayURL, libmetrics.BuildJobMetricsName(agentName)).
    Grouping("agent", agentName).
    Grouping("task_type", a.TaskType).
    Collector(registry)
defer func() {
    if err := pusher.PushContext(ctx); err != nil {
        glog.Warningf("prometheus push failed: %v", err)
        return
    }
    glog.V(2).Infof("prometheus push completed")
}()
start := libtime.NewCurrentDateTime().Now().Time()
```

Note: `push.New(url, jobName string)` is from `github.com/prometheus/client_golang/prometheus/push`, NOT `bborbe/metrics`. The `bborbe/metrics` wrapper (`bborbemetrics.NewPusher`) does NOT expose a `Grouping` method — only the raw Prometheus `push.Pusher` does.

Verify before using:
```bash
grep -n "func.*Grouping\b\|func (p \*Pusher) Grouping" $(go env GOPATH)/pkg/mod/github.com/prometheus/client_golang@v1.23.2/prometheus/push/push.go
```
Expected: one match at the `Pusher.Grouping` method.

**Inline reference — `libtime.DateTime.Time()` conversion:**
`libtime.NewCurrentDateTime().Now()` returns `libtime.DateTime` (a named type over `time.Time`).
`time.Since()` requires `time.Time`. Use `.Time()` to convert:
```go
start := libtime.NewCurrentDateTime().Now().Time()
// ...later:
metrics.RecordDuration(time.Since(start))
```

**Inline reference — `agentName` constants per binary:**
```go
// agent/claude/main.go
const agentName = "claude-agent"

// agent/code/main.go
const agentName = "code-agent"

// agent/gemini/main.go
const agentName = "gemini-agent"
```
These MUST match the K8s Deployment naming. Operators filter dashboards by these strings.

**Inline reference — return paths after metrics init for `agent/claude` (template for all three):**
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
    // --- metrics init block here (see above) ---

    glog.V(2).Infof("agent-claude started phase=%s", a.Phase)

    deliverer, cleanup, err := factory.CreateDeliverer(...)
    if err != nil {
        metrics.RecordRun(agentlib.AgentStatusFailed)
        metrics.RecordDuration(time.Since(start))
        return errors.Wrap(ctx, err, "create deliverer")
    }
    defer cleanup()

    agent := factory.CreateAgent(...)

    result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
    if err != nil {
        metrics.RecordRun(agentlib.AgentStatusFailed)
        metrics.RecordDuration(time.Since(start))
        return errors.Wrap(ctx, err, "agent run failed")
    }
    metrics.RecordRun(result.Status)
    metrics.RecordDuration(time.Since(start))
    return agentlib.PrintResult(result)
}
```

For `agent/gemini`, add the same pattern to the `geminiParser` error path (the additional early return).

**Symbol verification — run before writing any code:**
```bash
# Verify lib/metrics package exports exist
grep -n "func NewJobMetrics\|func BuildJobMetricsName\|type JobMetrics interface" lib/metrics/metrics.go

# Verify prometheus/push Grouping method
grep -n "Grouping" $(go env GOPATH)/pkg/mod/github.com/prometheus/client_golang@v1.23.2/prometheus/push/push.go | head -3

# Verify prometheus.NewRegistry exists
grep -n "func NewRegistry\b" $(go env GOPATH)/pkg/mod/github.com/prometheus/client_golang@v1.23.2/prometheus/registry.go

# Verify libtime.CurrentDateTime.Now() return type has Time() method
grep -n "func (d DateTime) Time()" $(go env GOPATH)/pkg/mod/github.com/bborbe/time@v1.27.0/time_date-time.go
```
</context>

<requirements>

## 1. Add dependencies to each binary's `go.mod`

For each of the three binaries:

```bash
cd agent/claude
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/push
go mod tidy

cd ../code
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/push
go mod tidy

cd ../gemini
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/push
go mod tidy
```

Verify for each binary:
```bash
grep "prometheus/client_golang" agent/claude/go.mod | grep -v indirect
grep "prometheus/client_golang" agent/code/go.mod | grep -v indirect
grep "prometheus/client_golang" agent/gemini/go.mod | grep -v indirect
```
Expected: one direct line per binary (the `prometheus` and `push` sub-packages share the same module path). NOTE: `github.com/bborbe/metrics` is NOT a direct dep of these binaries — only `lib/go.mod` depends on it. The binaries use the raw `prometheus/client_golang/prometheus/push` API directly because `bborbe/metrics`' `Pusher` wrapper does not expose `Grouping`.

## 2. Update `agent/claude/main.go`

Read the full file before editing.

**a. Add `const agentName` and two struct fields:**

After the import block, before `func main()`, add:
```go
const agentName = "claude-agent"
```

Add to the end of the `application` struct body (after the last existing field, before closing brace):
```go
PushgatewayURL string `required:"false" arg:"pushgateway-url" env:"PUSHGATEWAY_URL" usage:"Prometheus PushGateway URL" default:"http://pushgateway:9090"`
TaskType       string `required:"false" arg:"task-type"       env:"TASK_TYPE"       usage:"Task type label for metric grouping" default:"unknown"`
```

**b. Add imports:**

Add to the import block:
```go
libmetrics "github.com/bborbe/agent/lib/metrics"
agentlib    "github.com/bborbe/agent/lib"
libtime     "github.com/bborbe/time"
"github.com/prometheus/client_golang/prometheus"
"github.com/prometheus/client_golang/prometheus/push"
"time"
```

Note: `agentlib` is already imported as `agentlib "github.com/bborbe/agent/lib"`. Check if the alias already exists; if so, use it. If the package is imported without an alias, add the alias or use the existing name.

**c. Rewrite `Run()` to insert metrics init at the top and RecordRun/RecordDuration at every return:**

The final `Run()` body (replacing the existing body entirely):
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	registry := prometheus.NewRegistry()
	metrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
	pusher := push.New(a.PushgatewayURL, libmetrics.BuildJobMetricsName(agentName)).
		Grouping("agent", agentName).
		Grouping("task_type", a.TaskType).
		Collector(registry)
	defer func() {
		if err := pusher.PushContext(ctx); err != nil {
			glog.Warningf("prometheus push failed: %v", err)
			return
		}
		glog.V(2).Infof("prometheus push completed")
	}()
	start := libtime.NewCurrentDateTime().Now().Time()

	glog.V(2).Infof("agent-claude started phase=%s", a.Phase)

	deliverer, cleanup, err := factory.CreateDeliverer(
		ctx, a.TaskID, a.KafkaBrokers, a.Branch, a.TaskContent,
	)
	if err != nil {
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "create deliverer")
	}
	defer cleanup()

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
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "agent run failed")
	}
	metrics.RecordRun(result.Status)
	metrics.RecordDuration(time.Since(start))
	return agentlib.PrintResult(result)
}
```

Verify all return paths have RecordRun + RecordDuration:
```bash
grep -n "RecordRun\|RecordDuration\|return " agent/claude/main.go
```
Expected: each `return errors.Wrap` or `return agentlib.PrintResult` is preceded by both RecordRun and RecordDuration lines.

Build check:
```bash
cd agent/claude && go build ./...
```
Expected: exit 0.

## 3. Update `agent/code/main.go`

Read the full file before editing. Apply identical pattern.

**a. Add `const agentName = "code-agent"` after imports, before `func main()`.**

**b. Add same two struct fields as step 2.**

**c. Add same imports as step 2 (omit `claudelib` since it's not used in code binary).**

**d. Rewrite `Run()`:**
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	registry := prometheus.NewRegistry()
	metrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
	pusher := push.New(a.PushgatewayURL, libmetrics.BuildJobMetricsName(agentName)).
		Grouping("agent", agentName).
		Grouping("task_type", a.TaskType).
		Collector(registry)
	defer func() {
		if err := pusher.PushContext(ctx); err != nil {
			glog.Warningf("prometheus push failed: %v", err)
			return
		}
		glog.V(2).Infof("prometheus push completed")
	}()
	start := libtime.NewCurrentDateTime().Now().Time()

	glog.V(2).Infof("agent-code started phase=%s", a.Phase)

	deliverer, cleanup, err := factory.CreateDeliverer(
		ctx, a.TaskID, a.KafkaBrokers, a.Branch, a.TaskContent,
	)
	if err != nil {
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "create deliverer")
	}
	defer cleanup()

	result, err := factory.CreateAgent().Run(ctx, a.Phase, a.TaskContent, deliverer)
	if err != nil {
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "agent run failed")
	}
	metrics.RecordRun(result.Status)
	metrics.RecordDuration(time.Since(start))
	return agentlib.PrintResult(result)
}
```

Build check:
```bash
cd agent/code && go build ./...
```

## 4. Update `agent/gemini/main.go`

Read the full file before editing. The gemini binary has an ADDITIONAL early error path (`CreateGeminiParser`) that must also call RecordRun + RecordDuration.

**a. Add `const agentName = "gemini-agent"` after imports, before `func main()`.**

**b. Add same two struct fields as step 2.**

**c. Add same imports (omit `claudelib` since not used).**

**d. Rewrite `Run()`:**
```go
func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	registry := prometheus.NewRegistry()
	metrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
	pusher := push.New(a.PushgatewayURL, libmetrics.BuildJobMetricsName(agentName)).
		Grouping("agent", agentName).
		Grouping("task_type", a.TaskType).
		Collector(registry)
	defer func() {
		if err := pusher.PushContext(ctx); err != nil {
			glog.Warningf("prometheus push failed: %v", err)
			return
		}
		glog.V(2).Infof("prometheus push completed")
	}()
	start := libtime.NewCurrentDateTime().Now().Time()

	glog.V(2).Infof("agent-gemini started phase=%s", a.Phase)

	geminiParser, err := factory.CreateGeminiParser(ctx, a.GeminiAPIKey, a.GeminiModel)
	if err != nil {
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "create gemini parser")
	}

	deliverer, cleanup, err := factory.CreateDeliverer(
		ctx, a.TaskID, a.KafkaBrokers, a.Branch, a.TaskContent,
	)
	if err != nil {
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "create deliverer")
	}
	defer cleanup()

	result, err := factory.CreateAgent(geminiParser).Run(ctx, a.Phase, a.TaskContent, deliverer)
	if err != nil {
		metrics.RecordRun(agentlib.AgentStatusFailed)
		metrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "agent run failed")
	}
	metrics.RecordRun(result.Status)
	metrics.RecordDuration(time.Since(start))
	return agentlib.PrintResult(result)
}
```

Build check:
```bash
cd agent/gemini && go build ./...
```

## 5. Update root `CHANGELOG.md`

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header. Add:

Prompt 1 (lib/metrics) owns the `feat(lib/metrics):` bullet under `## Unreleased`. This prompt OWNS the binary-wire-up bullet and ONLY appends it. Do NOT add or modify the lib bullet here.

Append exactly ONE bullet to the existing `## Unreleased` section:

```markdown
- feat(agent/{claude,code,gemini}): wire `JobMetrics` into each binary's `Run()` — constructs a fresh registry + pusher at startup, defers `PushContext` for end-of-run metric delivery, records run outcome and duration at every return path; adds `PUSHGATEWAY_URL` (default `http://pushgateway:9090`) and `TASK_TYPE` (default `unknown`) env fields
```

If `## Unreleased` does not exist (prompt 1 should have created it; if not, prompt 1 failed and this prompt's precondition gate caught it earlier), create the section AND add this bullet. Do NOT add a `feat(lib/metrics)` bullet — that is prompt 1's responsibility.

Verify after the edit:
```bash
grep -A 10 "^## Unreleased" CHANGELOG.md
```
Expected: exactly one `## Unreleased` header, with the binary-wire-up bullet present. No duplicate `## Unreleased` headers anywhere in the file.

## 6. Run iterative tests and precommit

Run tests for each binary (they have no new test files, but existing tests must pass):
```bash
cd agent/claude && go test ./...
cd agent/code  && go test ./...
cd agent/gemini && go test ./...
```

Then run precommit for each binary. Run them sequentially (each `make precommit` is independent):
```bash
cd agent/claude && make precommit
```
Fix any failures before continuing.
```bash
cd agent/code && make precommit
```
Fix any failures before continuing.
```bash
cd agent/gemini && make precommit
```

All three must exit 0.

**Common compile errors to expect:**
- `metrics` local variable name conflicts with an existing import — if `metrics` is already used as a package import alias, rename the local variable to `jobMetrics` consistently throughout `Run()`
- `agentlib` alias already exists with a different name — check existing import alias and use it (e.g., the existing code uses `agentlib "github.com/bborbe/agent/lib"`)
- `time` import conflict — the standard `time` package and `libtime` are separate; both can coexist in the import block
- `push.New(...).Grouping(...)` returns `*push.Pusher`, and `Collector(registry)` also returns `*push.Pusher` — the whole chain is a single expression assigned to `pusher`

**Import alias check for each binary:**
```bash
grep -n "bborbe/agent/lib\"" agent/claude/main.go
```
If the existing import has no alias (i.e., just `"github.com/bborbe/agent/lib"`), the package name is `lib`. Add or check an `agentlib` alias when adding the new import to avoid ambiguity.

</requirements>

<constraints>
- **Precondition:** `lib/metrics/metrics.go` must exist (created by prompt 1). If absent, report `"status":"failed"`.
- Only these files are modified: `agent/claude/main.go`, `agent/code/main.go`, `agent/gemini/main.go`, their respective `go.mod`/`go.sum` pairs, and `CHANGELOG.md`. No K8s manifests, Dockerfiles, Makefiles, or `pkg/` directories are touched.
- The two new struct fields use EXACTLY the field names `PushgatewayURL` and `TaskType` with the specified env/arg/default tags. No other struct fields are added or modified.
- `const agentName` values MUST be `"claude-agent"`, `"code-agent"`, `"gemini-agent"` exactly — operator dashboards filter on these strings.
- The metrics init block is inserted at the TOP of `Run()`, before the existing `glog.V(2).Infof(...)` log line.
- No new top-level helper functions are extracted from `main.go`. The metrics + pusher setup lives inline in `Run()`.
- No `init()` functions are added to any binary.
- `push.New(url, jobName string)` from `github.com/prometheus/client_golang/prometheus/push` is used directly — NOT `bborbemetrics.NewPusher()`, which lacks the `Grouping` method.
- `pusher.PushContext(ctx)` is used in the deferred func (NOT `pusher.Push()` which is deprecated and takes no context).
- `start` is a `time.Time` (from `libtime.NewCurrentDateTime().Now().Time()`, NOT directly `libtime.DateTime`).
- `time.Since(start)` computes the duration. `metrics.RecordDuration(time.Since(start))` is the correct call.
- Every return path (including infrastructure errors like create-deliverer and create-gemini-parser) records `AgentStatusFailed`. The success path reads `result.Status`.
- **Recording call order is frozen:** `metrics.RecordRun(status)` MUST be called BEFORE `metrics.RecordDuration(time.Since(start))` at every return path. The status update is the primary signal; duration is secondary. Reversed order would still record both metrics, but the contract is: RecordRun first, RecordDuration second, no exceptions.
- Error wrapping: `github.com/bborbe/errors` (already used in each binary) — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- `make precommit` must exit 0 in each of `agent/claude/`, `agent/code/`, `agent/gemini/`.
</constraints>

<verification>

Verify precondition (lib/metrics exists):
```bash
ls lib/metrics/metrics.go lib/metrics/mocks/job-metrics.go
```
Expected: both files present.

Verify `agentName` constants:
```bash
grep -n "const agentName" agent/claude/main.go agent/code/main.go agent/gemini/main.go
```
Expected: three matches — `"claude-agent"`, `"code-agent"`, `"gemini-agent"`.

Verify new struct fields in each binary:
```bash
grep -n "PushgatewayURL\|TaskType\|PUSHGATEWAY_URL\|TASK_TYPE" agent/claude/main.go agent/code/main.go agent/gemini/main.go
```
Expected: both fields in each file.

Verify metrics init is at the top of each Run():
```bash
grep -n "prometheus.NewRegistry\|libmetrics.NewJobMetrics\|push.New\|PushContext\|start.*Now.*Time" agent/claude/main.go agent/code/main.go agent/gemini/main.go
```
Expected: each file has all five constructs.

Verify every return path calls RecordRun + RecordDuration:
```bash
grep -n "RecordRun\|RecordDuration\|return " agent/claude/main.go
```
Expected: each `return` line (except the defer's `return`) is preceded by RecordRun and RecordDuration.

```bash
grep -n "RecordRun\|RecordDuration\|return " agent/code/main.go
grep -n "RecordRun\|RecordDuration\|return " agent/gemini/main.go
```

Verify gemini has 4 RecordRun calls (3 error paths + 1 success):
```bash
grep -c "RecordRun" agent/gemini/main.go
```
Expected: 4.

Verify direct deps in each binary's go.mod:
```bash
grep "prometheus/client_golang" agent/claude/go.mod | grep -v indirect
grep "prometheus/client_golang" agent/code/go.mod | grep -v indirect
grep "prometheus/client_golang" agent/gemini/go.mod | grep -v indirect
```
Expected: one direct entry per file. `bborbe/metrics` is NOT expected as a direct dep here — only `lib/go.mod` depends on it.

Verify CHANGELOG updated:
```bash
grep -n "lib/metrics\|agent.*claude.*code.*gemini\|JobMetrics\|PUSHGATEWAY_URL" CHANGELOG.md | head -5
```
Expected: at least one match under `## Unreleased`.

Build all three binaries:
```bash
cd agent/claude && go build ./... && cd ../code && go build ./... && cd ../gemini && go build ./...
```
Expected: no compile errors.

Run tests:
```bash
cd agent/claude && go test ./... && cd ../code && go test ./... && cd ../gemini && go test ./...
```
Expected: exit 0.

Run precommit (sequentially):
```bash
cd agent/claude && make precommit
cd agent/code   && make precommit
cd agent/gemini && make precommit
```
Expected: all three exit 0.

**Post-completion note for management session:** After these prompts ship and the management session commits, cut the paired release tags:
```bash
git tag -l "v*" --sort=-v:refname | head -3
git tag -l "lib/v*" --sort=-v:refname | head -3
# Pick version above both, rename ## Unreleased → ## vX.Y.Z in CHANGELOG.md
git commit -m "release vX.Y.Z"
git tag vX.Y.Z
git tag lib/vX.Y.Z
git push origin master vX.Y.Z lib/vX.Y.Z
```

</verification>
