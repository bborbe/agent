---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-14T08:58:05Z"
generating: "2026-05-14T08:58:06Z"
prompted: "2026-05-14T09:08:50Z"
verifying: "2026-05-14T09:33:20Z"
branch: dark-factory/per-agent-job-metrics-package
---

## Summary

- Today every agent Job pushes nothing to Prometheus. Existing aggregate counters in `task/executor` and `task/controller` carry no per-agent labels, so operators cannot answer "when did claude-agent last run", "what's the 24h failure rate of code-agent", or "which agents haven't run this week" without walking the vault git log.
- This spec ships the shared metrics package — a new `lib/metrics` module exposing a `JobMetrics` interface, a pure factory, and three Prometheus collectors registered to a caller-owned `*prometheus.Registry` — AND wires it into the three agent binaries in this repo: `agent/claude`, `agent/code`, `agent/gemini`. No K8s manifest changes. No Grafana dashboard. No maintainer / trading repo changes.
- Three metrics: a counter for run outcomes (`status` label, pre-initialized for `done`/`failed`/`needs_input`), a gauge for the last-run timestamp (also `status` label), and a label-free histogram for run duration with buckets sized for agent runs.
- `agent` and `task_type` are pusher `Grouping()` dimensions, NOT metric labels — they are constant per Job invocation and belong on the pusher, not on the series. `agent` is a hard-coded constant per binary. `task_type` comes from a new `TASK_TYPE` env on each binary's `application` struct (default `"unknown"`; executor-side population is a follow-up spec).
- All three binaries share an identical wire-up shape via the lib: build a `prometheus.NewRegistry()` once at startup, construct the `JobMetrics` against it, configure a `bborbe/metrics` pusher with `Grouping("agent", <const>).Grouping("task_type", <env>).Collector(registry)`, defer `pusher.Push(ctx)`, and call `RecordRun(status)` + `RecordDuration(d)` at the result-publish boundary in `Run()`.
- Paired tag release at end of work: bump root `CHANGELOG.md`, cut `vX.Y.Z` + `lib/vX.Y.Z` at the same commit, per the repo tag policy. `maintainer/agent/pr-reviewer` and the three trading agents ship in follow-up specs in their own repos after this release lands.

## Referenced Specs (one-line glosses)

- **Task source**: `~/Documents/Obsidian/Personal/24 Tasks/Per-agent Prometheus metrics via PushGateway.md` — problem statement, approach, and success criteria for the broader effort. This spec is the first slice (shared library only).
- **Spec 010** — split `failed` (transient, retried) vs `needs_input` (operator-resolvable, not retried). This spec adopts those status values as the counter's pre-initialized label set, plus `done`.
- **Spec 024** — weekly OAuth probe. Concrete future consumer of these metrics; out of scope here.

## Problem

Agent Jobs are short-lived: the pod terminates within seconds of completion, before Prometheus can scrape it. The current observability path is the controller/executor's aggregate counters, none of which carry an `agent` dimension. Operators cannot answer last-run, per-agent failure rate, or staleness questions without reading vault frontmatter task-by-task. The fix at the system level is to push per-Job metrics to the cluster's existing PushGateway at Job end — but every agent reimplementing the same metric definitions, registry wiring, and counter pre-init would diverge silently. A single shared library defines the metric shape once and gives every consumer a typed `JobMetrics` interface to drive.

## Goal

After this spec ships, `github.com/bborbe/agent/lib/metrics` exports a `JobMetrics` interface and a pure constructor `NewJobMetrics(registry, currentDateTime)` that registers three correctly-shaped Prometheus collectors onto the caller-owned registry, pre-initializes the run counter for every status value, and returns an interface a consumer can drive at the result-publish boundary. A consumer that imports this package gets metrics whose names, labels, help strings, and bucket layout are uniform across all agents — without writing any Prometheus boilerplate of its own. A counterfeiter-generated mock is available for downstream tests.

The three agent binaries in this repo (`agent/claude`, `agent/code`, `agent/gemini`) consume the library: each main.go acquires a fresh `prometheus.NewRegistry()`, constructs the `JobMetrics`, builds a `bborbe/metrics` pusher with `Grouping("agent", <const>).Grouping("task_type", <env>).Collector(registry)`, defers `pusher.Push(ctx)` for after-Run() emission (success and failure paths), captures a wall-clock start at the top of `Run()`, and on each return path calls `RecordRun(status)` + `RecordDuration(time.Since(start))` before returning. Each binary's `application` struct gains two new env fields: `PUSHGATEWAY_URL` (default `"http://pushgateway:9090"`) and `TASK_TYPE` (default `"unknown"`).

The library version is cut as a paired `vX.Y.Z` + `lib/vX.Y.Z` tag pair at the same commit as the three binary updates, per the repo tag policy.

## Non-goals

- **No cross-repo consumer wire-up.** `maintainer/agent/pr-reviewer` and `trading/agent/{trade-analysis,backtest,hypothesis}` are NOT modified. Each lands in a follow-up spec in its own repo after this lib is tagged.
- **No K8s manifest changes.** Default `PUSHGATEWAY_URL` resolves the pusher inside the cluster; `TASK_TYPE` defaults to `"unknown"` if unset. Operators can override either via env without re-deploying.
- **No executor-side `TASK_TYPE` population.** The executor passing the task's actual `task_type` field to the spawned Job's `TASK_TYPE` env is a separate follow-up spec. Until that lands, metrics will carry `task_type="unknown"` in the cluster — still per-agent observable via the `agent` grouping label.
- **No Grafana dashboard.** "Agent Health" lands in a later spec after at least one binary is producing real series in dev.
- **No alerting rules.** Out of scope; would land after the dashboard.
- **No deprecation or change to existing aggregate metrics** in `task/executor/pkg/metrics` and `task/controller/pkg/metrics`. They keep their current shape.
- **No new metric beyond the three named.** Build-info / runtime / GC metrics are not in scope (a consumer may add `libmetrics.NewBuildInfoMetrics()` separately in a follow-up).
- **No changes to `agent` semantics** — it remains a per-binary constant chosen at compile-time, hard-coded in each main.go and passed to the pusher's `Grouping("agent", <const>)`.
- **No change to existing `application` struct fields** in the three binaries beyond adding the two new env fields. `Run()` signatures are unchanged.

## Alternatives Considered

- **Put `agent` and `task_type` on the metric labels instead of pusher groupings.** Rejected. They are constant per Job (the pusher pushes one series-set per agent/task_type combo by design via `Grouping()`), and putting them on labels would either explode cardinality at the registry level or duplicate the grouping-vs-label information. The trading-services pattern in `shared/command-job/main.go` already groups by job-scope dimensions; this spec follows that pattern.
- **Use `promauto` / package-level globals like the existing executor/controller metrics.** Rejected. Package-level globals register into the default registry implicitly and cannot be re-registered cleanly per Job. Each agent Job needs a fresh `*prometheus.Registry` to pair with the pusher so it does not push the global default registry's collectors. The constructor takes the registry explicitly to make ownership unambiguous.
- **Expose three separate metric types (no `JobMetrics` interface), let the consumer call `.WithLabelValues(...).Inc()` directly.** Rejected. Without an interface there is no chokepoint to mock in consumer tests, and the per-status counter-pre-init contract has nowhere to live. The interface also keeps the gauge-set + counter-inc pair atomic per status update.
- **Combine `RecordRun(status)` and `RecordDuration(duration)` into a single method `RecordCompletion(status, duration)`.** Rejected. Run duration is a wall-clock measurement that some consumers may not be able to compute cleanly (e.g. if the Job's start time is not visible at the result-publish call site). Keeping them separate lets a consumer record duration optionally without losing the status counter/gauge update.
- **Add a `success_total` and `failure_total` pair instead of a single counter with a `status` label.** Rejected. Three statuses (`done`/`failed`/`needs_input`) need to be distinguished, not two; a labeled counter is the standard shape for outcome enums and matches `AgentStatus`.
- **Bucket layout: use Prometheus default histogram buckets.** Rejected. Defaults peak at 10s; agent runs routinely take minutes (claude up to 30 min, backtests longer). Buckets must extend to 1800s for the histogram to be informative. The bucket choice is part of the spec contract — consumers depend on it being stable across versions.

## Desired Behavior

### Library (`lib/metrics`)

1. A new Go package at `lib/metrics/` (within the `github.com/bborbe/agent/lib` module) exports a `JobMetrics` interface and a pure constructor with the frozen signature `NewJobMetrics(registry *prometheus.Registry, currentDateTime libtime.CurrentDateTime) JobMetrics`. The constructor registers all three collectors onto the caller-owned registry (no use of the default Prometheus registry), pre-initializes the counter, and returns the interface. No I/O, no conditionals, no `context.Background()`.
2. **Run counter** — a `CounterVec` named `agent_job_run_total` with a single label `status`. Pre-initialized via `.Add(0)` for exactly three status values: `done`, `failed`, `needs_input`. Other `AgentStatus` values (e.g. `in_progress`) are NOT pre-initialized because they are not terminal Job outcomes.
3. **Last-run timestamp gauge** — a `GaugeVec` named `agent_job_last_run_timestamp_seconds` with a single label `status`. Set via the injected `currentDateTime.Now().Unix()` (NOT the package-level `time.Now()`) as a `float64` at the moment a run outcome is recorded.
4. **Run duration histogram** — a `Histogram` named `agent_job_duration_seconds` with no labels and buckets `[0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800]` seconds. The bucket layout is part of the spec contract; changing it is a breaking change for downstream dashboards. Help strings on all three metrics are descriptive and reference units where ambiguous.
5. The interface exposes at minimum two operations: a terminal-run recorder that atomically increments the counter AND sets the gauge for the same `status` label in a single call (the two cannot drift), and a duration recorder that observes the histogram with `duration.Seconds()`. The exact method names and grouping (one method vs two) are an implementation-prompt decision; the atomicity contract is frozen.
6. A counterfeiter-generated mock for `JobMetrics` is produced into `lib/metrics/mocks/`, driven by the explicit two-line `//go:generate` + `//counterfeiter:generate` directive form. Tests are Ginkgo v2 / Gomega in an external `*_test` package, using the counterfeiter mock of `libtime.CurrentDateTime` to drive deterministic gauge values, and using the standard Prometheus testing helpers (`testutil.ToFloat64`, `testutil.CollectAndCount`) to assert collector registration, counter pre-init, gauge value, and histogram observation.

### Binary wire-up (`agent/{claude,code,gemini}`)

7. Each of the three binaries gains exactly two new fields on its `application` struct, in the existing struct-tag style, plus a file-scope `const agentName` matching the K8s Deployment naming (`"claude-agent"`, `"code-agent"`, `"gemini-agent"`):
    ```
    PushgatewayURL string `required:"false" arg:"pushgateway-url" env:"PUSHGATEWAY_URL" usage:"Prometheus PushGateway URL" default:"http://pushgateway:9090"`
    TaskType       string `required:"false" arg:"task-type"       env:"TASK_TYPE"       usage:"Task type label for metric grouping" default:"unknown"`
    ```
8. At the top of each binary's `Run(ctx, _ libsentry.Client)` method, before any existing logic, the binary constructs the metrics + pusher and defers the push:
    ```go
    registry := prometheus.NewRegistry()
    metrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
    pusher := bborbemetrics.NewPusher(a.PushgatewayURL, bborbemetrics.BuildName("agent-job", agentName)).
        Grouping("agent", agentName).
        Grouping("task_type", a.TaskType).
        Collector(registry)
    defer func() {
        if err := pusher.Push(ctx); err != nil {
            glog.Warningf("prometheus push failed: %v", err)
            return
        }
        glog.V(2).Infof("prometheus push completed")
    }()
    start := libtime.NewCurrentDateTime().Now()
    ```
9. Every `return` path inside each binary's `Run()` body — both error returns and the success return — is preceded by `metrics.RecordRun(status)` + `metrics.RecordDuration(time.Since(start))`. Infrastructure-error returns use `AgentStatusFailed`. The success path takes `status` from the agent run result's `Status` field (already one of `done`/`failed`/`needs_input` per `lib/agent_status.go`). No path through `Run()` exits without recording.
10. Each binary's build is clean: `go.mod` includes `github.com/bborbe/metrics` and `github.com/prometheus/client_golang` as direct deps (`github.com/bborbe/agent/lib/metrics` is already covered by the existing `replace github.com/bborbe/agent/lib => ../../lib`), `go mod tidy` is clean, `make precommit` is clean in each of `lib/`, `agent/claude/`, `agent/code/`, `agent/gemini/`.

### Release

11. A `## Unreleased` bullet is added to the root `CHANGELOG.md` describing (a) the new `lib/metrics` package and the three metric names, and (b) the three-binary wire-up plus the two new env fields (`PUSHGATEWAY_URL`, `TASK_TYPE`).
12. At end of work, a paired `vX.Y.Z` + `lib/vX.Y.Z` tag pair is cut at the same commit, per the repo tag policy in `CLAUDE.md`. The exact version is whatever is one above the highest existing root AND lib tag. The `## Unreleased` header is renamed to `## vX.Y.Z` with the release date.

## Constraints

- The package lives at `lib/metrics/` within the existing `github.com/bborbe/agent/lib` module. No new go.mod is added.
- `github.com/bborbe/metrics` (already used by `trading/shared/command-job/main.go` for `Pusher` + `BuildName`) is added to `lib/go.mod` as a direct dependency. Its API for `NewPusher`, `BuildName`, and the `Grouping(...).Collector(...)` builder must NOT be re-implemented or wrapped here — pushers are constructed by consumers, not by this package.
- The constructor signature `NewJobMetrics(registry *prometheus.Registry, currentDateTime libtime.CurrentDateTime) JobMetrics` is frozen. Adding parameters in a follow-up is a breaking change for downstream consumers.
- The three exported metric NAMES (`agent_job_run_total`, `agent_job_last_run_timestamp_seconds`, `agent_job_duration_seconds`), the status label NAME (`status`), the status label VALUES (`done`, `failed`, `needs_input`), and the histogram BUCKETS are frozen contracts. Changing any of them after release is a Grafana-dashboard-breaking change.
- `prometheus.MustRegister` is used inside the constructor — registration failures are programmer errors and must panic at startup, not return errors. (This matches the trading-services pattern and existing `task/{controller,executor}/pkg/metrics/metrics.go` shape.)
- No use of the default Prometheus registry. No `promauto`. The constructor must accept the registry and register onto it.
- The constructor follows the repo's pure-factory rule: no I/O, no conditionals on its arguments, no `context.Background()`. Argument validation happens at the type system level (a nil registry passed in is the caller's bug).
- The implementation follows `go-architecture-patterns.md`: Interface → Constructor → Struct → Method order in the source file.
- Tests use external test packages (`package metrics_test`) per the repo convention.
- Counterfeiter mocks are produced with the explicit form documented in `~/Documents/workspaces/agent/CLAUDE.md`:
  ```
  //go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
  //counterfeiter:generate -o mocks/job-metrics.go --fake-name JobMetrics . JobMetrics
  ```
  (The shortform `//counterfeiter:generate . Foo` is a silent no-op in this repo's `make generate` pipeline.)
- No `task/controller/pkg/metrics/metrics.go` or `task/executor/pkg/metrics/metrics.go` file is modified.
- The only files modified outside `lib/` are: `agent/claude/main.go`, `agent/code/main.go`, `agent/gemini/main.go`, the three corresponding `go.mod` + `go.sum` pairs, and `CHANGELOG.md`. No other repo file is touched. (K8s manifests, `Dockerfile`, `Makefile`, `pkg/`, `cmd/` directories — all unchanged.)
- The three binary main.go updates follow the existing struct-tag layout (one tag per directive, aligned). No `init()` functions are added. No new top-level helpers extracted from `main.go` — the metrics + pusher setup lives inline at the top of `Run()` to keep each main.go self-contained.
- The `agentName` constant in each binary's main.go MUST match the K8s Deployment / pod naming used today: `"claude-agent"`, `"code-agent"`, `"gemini-agent"`. Operator dashboards filter on these strings.
- The CHANGELOG bullet under `## Unreleased` is required before the release tags are cut.
- The release pair (`vX.Y.Z` + `lib/vX.Y.Z`) is cut at the same commit; both numbers MUST equal the latest `## vX.Y.Z` header in `CHANGELOG.md`. Version chosen is one above the highest existing root AND lib tag (`git tag -l "v*" --sort=-v:refname`, `git tag -l "lib/v*" --sort=-v:refname`).

## Assumptions

- The `github.com/bborbe/metrics` module's API for `NewPusher(url, jobName)`, `BuildName(parts...)`, and the `Grouping(key, value).Collector(registry)` builder is stable and matches the usage in `trading/shared/command-job/main.go`. This package does NOT depend on those types at the API level — they are consumer concerns — but adding the module to `lib/go.mod` validates that the dependency resolves and downstream consumers can import the pusher from the same module set.
- `libtime.CurrentDateTime` (from `github.com/bborbe/time`, already a direct dependency of `lib/go.mod`) exposes a `Now() time.Time` method whose result can be `.Unix()`-converted. This is the same accessor pattern other `lib/` files use for injectable time.
- The Prometheus testing helpers (`testutil.ToFloat64`, `testutil.CollectAndCount`, `testutil.CollectAndCompare`) are available from `github.com/prometheus/client_golang/prometheus/testutil`. They will be added to `lib/go.mod` as a test dependency.
- Cross-repo consumer specs (`maintainer/agent/pr-reviewer`, `trading/agent/{trade-analysis,backtest,hypothesis}`) will run AFTER this lib release lands. Each will `go get github.com/bborbe/agent/lib@vX.Y.Z` and follow the same wire-up shape this spec freezes in `agent/{claude,code,gemini}`.
- The three binaries in this repo already share an `application` struct shape (verified: each has `SentryDSN`, `SentryProxy`, `Branch`, `TaskContent`, plus binary-specific fields). Adding two new fields to each struct does not collide with existing field names.
- The existing `replace github.com/bborbe/agent/lib => ../../lib` directive in each binary's `go.mod` (verified) makes same-commit lib+binary updates resolve correctly at build time. No `replace` is added or removed by this spec.
- `bborbemetrics.NewPusher(...).Grouping(...).Collector(registry)` matches the API used by `~/Documents/workspaces/trading/shared/command-job/main.go`. If that builder's signature has drifted in a newer `bborbe/metrics` version, the implementation prompt adapts to the current version — but the call-site shape (grouping by `agent` and `task_type`, custom registry) is frozen.
- The repo's `make generate` invokes counterfeiter via `//go:generate` and the explicit directive form documented in the project CLAUDE.md. If the shortform is used by mistake, mocks silently fail to generate; the explicit form is non-negotiable.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Consumer calls the run-recording method with an unrecognized `AgentStatus` value (e.g. `in_progress`) | Counter is incremented for that label value (Prometheus accepts arbitrary label values), gauge is set, but the value is NOT pre-initialized. This is a consumer bug — they should record only terminal outcomes. Spec does not protect against it. | Consumer fix: only record terminal statuses. |
| Consumer passes a `nil` registry to the constructor | `MustRegister` panics. This is a programmer error caught at startup. | Consumer fix: instantiate a registry. |
| Two `JobMetrics` instances are created against the same registry in the same process | The second `MustRegister` panics (Prometheus disallows duplicate collector registration). The constructor is intentionally non-idempotent; one registry → one instance. | Consumer fix: instantiate once at startup. |
| Consumer pushes to the pusher BEFORE calling `RecordRun` | The push produces a counter at zero for every pre-initialized status and an unset gauge. This is the desired pre-init behavior — the series exist at zero, `absent()` alerts work, no false negatives. | None — this is the contract. |
| Binary's process crashes between `Run()` entry and the deferred `pusher.Push` | The deferred push still fires (Go defer runs on panic + os.Exit-via-`service.Main` paths). If the crash is hard (`SIGKILL`, OOM), no push lands — acceptable per the broader task's data-loss semantics (single-instance PushGateway, no HA). | None — out of scope. |
| `TASK_TYPE` env unset by operator or executor | Metric series carry `task_type="unknown"`. Series are still queryable by `agent` grouping label; the `task_type` dimension just degrades to a single bucket. | Operator: set TASK_TYPE per Job. Long-term: executor-side population (follow-up spec). |
| `PUSHGATEWAY_URL` unset, no `pushgateway:9090` service in the cluster | `pusher.Push(ctx)` returns an error, logged via `glog.Warningf`, Job exit code unaffected — agent run is NOT failed by push failure. | Operator: deploy PushGateway, or override `PUSHGATEWAY_URL`. |
| `make precommit` flags drift after `make generate` | Implementation regenerates mocks, commits the drift, re-runs precommit. | None — caught at the verification rung. |
| Existing tag-bumping tool advances only `vX.Y.Z` or only `lib/vX.Y.Z` | Manual fix per the `CLAUDE.md` procedure: pick a version above both, bump CHANGELOG header, commit, tag both, push all three refs. | Manual fix; documented in CLAUDE.md. |

## Security / Abuse Cases

- This package neither reads user input nor performs I/O. It exports an interface and constructor. No HTTP, no file access, no network.
- The metric series and label values originate from typed `AgentStatus` values defined in `lib/agent_status.go` — no operator-supplied strings reach Prometheus through this package.
- A consumer that pushes to a malicious or unreachable PushGateway URL is a consumer-side concern. This package does not construct the pusher.
- No secrets are emitted. The metric series carry only enum status values and unix timestamps.
- Histogram bucket exhaustion (a run taking longer than 1800s) results in observation in the `+Inf` bucket — Prometheus-standard behavior, no leak, no exception.

## Acceptance Criteria

### Library

- [ ] Package `github.com/bborbe/agent/lib/metrics` exists with at least one source file containing the `JobMetrics` interface and the `NewJobMetrics` constructor.
- [ ] The constructor signature is exactly `NewJobMetrics(registry *prometheus.Registry, currentDateTime libtime.CurrentDateTime) JobMetrics`.
- [ ] The constructor registers exactly three collectors on the provided registry: a `CounterVec` named `agent_job_run_total` with a single `status` label, a `GaugeVec` named `agent_job_last_run_timestamp_seconds` with a single `status` label, and a `Histogram` named `agent_job_duration_seconds` with the bucket layout `[0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800]`.
- [ ] The counter is pre-initialized with `.Add(0)` for each of `done`, `failed`, `needs_input`.
- [ ] The interface's run-recording method updates BOTH the counter (increment) AND the gauge (set to `currentDateTime.Now().Unix()` as `float64`) for the same `status` label in a single call — the two cannot drift.
- [ ] The interface's duration-recording method observes the histogram with the duration in seconds (`duration.Seconds()`).
- [ ] A counterfeiter-generated mock exists at `lib/metrics/mocks/job-metrics.go` (or analogous path) and is driven by the explicit `//go:generate` + `//counterfeiter:generate` directive form.
- [ ] Tests are Ginkgo v2 / Gomega in an external `package metrics_test`, use the counterfeiter mock of `libtime.CurrentDateTime` to drive a deterministic timestamp, and assert: collectors are registered, counter pre-init values are zero for all three statuses, run-record method increments the counter AND sets the gauge to the injected time's unix-seconds, duration-record observes the histogram bucket consistent with the duration.
- [ ] `github.com/bborbe/metrics` is added to `lib/go.mod` as a direct dependency. `github.com/prometheus/client_golang` is a direct dependency. `go mod tidy` is clean.
- [ ] `make precommit` is clean in `lib/`.

### Binary wire-up

- [ ] Each of `agent/claude/main.go`, `agent/code/main.go`, `agent/gemini/main.go` has its `application` struct extended with EXACTLY two new fields: `PushgatewayURL` (default `"http://pushgateway:9090"`) and `TaskType` (default `"unknown"`), plus a file-scope `const agentName` with the value `"claude-agent"`, `"code-agent"`, `"gemini-agent"` respectively.
- [ ] At the top of each binary's `Run(ctx, _ libsentry.Client)` method, before existing logic, the binary constructs a `prometheus.NewRegistry()`, a `JobMetrics` against it via `libmetrics.NewJobMetrics`, and a `bborbemetrics.Pusher` configured with `Grouping("agent", agentName).Grouping("task_type", a.TaskType).Collector(registry)`, then defers `pusher.Push(ctx)` (logging warning on error, V(2) info on success). A wall-clock `start` is captured from `libtime.NewCurrentDateTime().Now()`.
- [ ] Every `return` path inside each binary's `Run()` body — including all existing error returns and the success return — is preceded by a call to `metrics.RecordRun(status)` and `metrics.RecordDuration(time.Since(start))`. Existing-error paths use `AgentStatusFailed`. The success path reads `status` from the agent run result's `Status` field.
- [ ] Build cleanliness: each binary's `go.mod` includes `github.com/prometheus/client_golang` as a direct dep (the binary directly imports `prometheus.NewRegistry` and `push.New` for `Grouping` support). `github.com/bborbe/metrics` is NOT a direct dep of the binaries — it appears as `// indirect` because the binaries use the upstream `prometheus/client_golang/prometheus/push` package directly (which exposes `Grouping`; `bborbe/metrics.Pusher` does not). The lib alone keeps `bborbe/metrics` as a direct dep. `go mod tidy` is clean, and `make precommit` is clean in `agent/claude/`, `agent/code/`, `agent/gemini/`.

### Release

- [ ] `## Unreleased` section of root `CHANGELOG.md` gains a bullet describing (a) the new `lib/metrics` package and the three metric names, and (b) the three-binary wire-up plus the two new env fields (`PUSHGATEWAY_URL`, `TASK_TYPE`).
- [ ] Paired tag release: `vX.Y.Z` and `lib/vX.Y.Z` are cut at the same commit. The `## Unreleased` section is renamed to `## vX.Y.Z` with the release date. Both tag numbers equal the latest CHANGELOG header. Both tags are pushed.

## Scenario Coverage

**Library:** unit tests against the constructor — collector registration via `testutil.CollectAndCount`, counter pre-init via `testutil.ToFloat64`, gauge values via direct read, histogram observation via `testutil.CollectAndCompare`. No I/O, no network, no concurrency. Fully covered by Ginkgo v2 in `package metrics_test`.

**Binary wire-up:** no new scenario file. The wire-up is structurally identical across the three binaries; verification is per-binary `make precommit` plus a dev-cluster smoke after deploy.

The dev PushGateway resolves at the in-cluster service `pushgateway.dev:9090` (verified via `kubectlquant -n dev get svc pushgateway`). Smoke steps for THIS spec (gates only `agent/claude` — see follow-up note below):

1. Deploy `agent/claude` to dev via `/make-buca agent/claude dev`.
2. Trigger one task at the claude agent (existing flow: oauth-probe trigger via `POST /admin/agent-task-executor/oauth-probe-trigger` creates a probe task with `task_type: oauth-probe`; the existing `agent-claude` Config CR routes it to the claude binary).
3. Via the admin gateway: `curl -s https://dev.quant.benjamin-borbe.de/admin/pushgateway/metrics | grep claude-agent` — expect at least one row with `agent="claude-agent"` and a non-zero counter for the executed status.
4. PromQL `time() - agent_job_last_run_timestamp_seconds{agent="claude-agent"}` returns a small number (seconds since last run).

**`agent/code` and `agent/gemini` smoke is deferred to a follow-up.** Both binaries are built, deployed, and structurally identical to `agent/claude` per the frozen wire-up pattern. BUT neither has a `Config CR` (no `k8s/agent-code.yaml` / `k8s/agent-gemini.yaml` exist), so the executor has no route for tasks to reach them, and they cannot be probed. Adding Config CRs is out-of-scope for THIS spec (which is about the lib + wire-up code only); it is captured as the follow-up "Create Config CRs for `agent-code` and `agent-gemini`". Once those land, the same probe-and-grep smoke applies to those two agents — but since the wire-up code is IDENTICAL and `agent/claude` exercises that wire-up end-to-end, the runtime contract is proved for the wire-up shape.

End-to-end Grafana dashboard verification: not applicable — no Grafana deployed.

## Verification

Library:

```
cd lib
make precommit
```

Each binary:

```
cd agent/claude && make precommit
cd ../code     && make precommit
cd ../gemini   && make precommit
```

Plus the manual tag-release procedure from `~/Documents/workspaces/agent/CLAUDE.md` "Versioning and tags":

```
git tag -l "v*" --sort=-v:refname | head -3
git tag -l "lib/v*" --sort=-v:refname | head -3
# pick a version above both
git commit -m "release vX.Y.Z"
git tag vX.Y.Z
git tag lib/vX.Y.Z
git push origin master vX.Y.Z lib/vX.Y.Z
```

Dev-cluster smoke (post-deploy, via `/make-buca`):

```
# Smoke gated on agent/claude only — agent/code and agent/gemini Config CRs are a follow-up.
curl -s https://dev.quant.benjamin-borbe.de/admin/pushgateway/metrics | grep 'agent="claude-agent"'
# Expect at least one row of agent_job_run_total with a non-zero counter for the executed status.
```

After the tags push, cross-repo consumer specs (maintainer, trading) can `go get github.com/bborbe/agent/lib@vX.Y.Z`.

## Do-Nothing Option

Skip this spec, and every future consumer of per-agent metrics ships its own copy-pasted Prometheus boilerplate: three collector definitions, three help strings, three names, one bucket layout, one pre-init loop. Doing it three times in this repo's `agent/{claude,code,gemini}` already gives three points of drift before the maintainer + trading specs even start. The histogram bucket choice — the only piece of the contract that cannot be normalized post-hoc without invalidating historical data — is the most likely thing to drift, because each author will make their own judgment about what "sensible buckets" means.

The cost of THIS spec is bounded: one new lib package (a few hundred lines of Go), three nearly-identical ~30-line additions to three existing main.go files, two new env fields per binary, one CHANGELOG bullet, one tag-release. The library is the cohesive part; the wire-up is mechanical and identical across binaries — which is precisely the case where centralizing into a lib pays off.
