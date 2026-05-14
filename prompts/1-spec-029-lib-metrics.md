---
status: draft
spec: [029-per-agent-job-metrics-package]
created: "2026-05-14T10:00:00Z"
branch: dark-factory/per-agent-job-metrics-package
---

<summary>
- A new package `github.com/bborbe/agent/lib/metrics` exports a `JobMetrics` interface and a `NewJobMetrics` constructor that registers three correctly-shaped Prometheus collectors on a caller-owned registry
- Consumers get a typed interface to drive at the result-publish boundary — no Prometheus boilerplate, no registry wiring, no counter pre-init
- Three metric shapes are frozen as contracts: a counter for run outcomes (pre-initialized for `done`/`failed`/`needs_input`), a gauge for last-run timestamp, and a histogram for run duration with agent-run-sized buckets
- The constructor uses injected `libtime.CurrentDateTime` for the gauge timestamp, making tests deterministic via `SetNow()`
- A counterfeiter-generated mock at `lib/metrics/mocks/job-metrics.go` (struct name `JobMetrics`) lets downstream test code substitute the real implementation
- A `BuildJobMetricsName(agentName string) string` helper exports the standardized PushGateway job name convention (`agent_job_<agentName>`) so all binaries use the same string without re-implementing the naming logic
- `github.com/bborbe/metrics` is added to `lib/go.mod` as a direct dependency (consumed by `BuildJobMetricsName`)
- `github.com/prometheus/client_golang` is promoted to a direct dependency of `lib/go.mod`
- All Ginkgo v2 tests pass; `cd lib && make precommit` exits 0
</summary>

<objective>
Create the `lib/metrics` package that defines the `JobMetrics` interface, its pure constructor, and the three frozen Prometheus metric shapes. This is the shared library consumed by all agent binaries to push per-job metrics to the PushGateway with a uniform metric schema.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct order, counterfeiter annotations, error wrapping
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — pure constructors: zero I/O, zero conditionals
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages (`package_test`), coverage ≥80%, counterfeiter mocks
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — metric naming, registration, interface patterns
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**
- `lib/agent_status.go` — `AgentStatus` type and constants (`AgentStatusDone`, `AgentStatusFailed`, `AgentStatusNeedsInput`)
- `lib/agent_step.go` — `Result` struct and its `Status AgentStatus` field
- `lib/lib_suite_test.go` — `//go:generate` directive version to replicate in new suite file
- `lib/go.mod` — current dependencies; `prometheus/client_golang` is indirect (needs promotion), `bborbe/metrics` is absent (needs adding)
- `task/executor/pkg/metrics/metrics.go` — reference for counter pre-init in an `init()` block; NOTE: that file uses `promauto` (package-level registry) which is explicitly NOT the pattern for this package — we register onto a caller-owned `*prometheus.Registry` instead

**Inline reference — frozen constructor signature:**
```go
func NewJobMetrics(registry *prometheus.Registry, currentDateTime libtime.CurrentDateTime) JobMetrics
```
This signature is frozen. Any follow-up spec that changes it is a breaking change.

**Inline reference — frozen metric names and shapes:**
```
agent_job_run_total          CounterVec  labels: [status]  pre-init: done, failed, needs_input
agent_job_last_run_timestamp_seconds  GaugeVec  labels: [status]
agent_job_duration_seconds   Histogram   no labels  buckets: [0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800]
```

**Inline reference — `libtime.CurrentDateTime` and `DateTime.Time()`:**
```go
// CurrentDateTime interface (from github.com/bborbe/time):
type CurrentDateTime interface {
    Now() DateTime
    SetNow(now DateTime)
}

// DateTime has a Unix() method that returns int64, and a Time() method that returns time.Time.
// In the gauge: float64(currentDateTime.Now().Unix())
```

**Inline reference — `bborbe/metrics.BuildName` (the only symbol used from this package):**
```go
// BuildName joins strings with "_", lowercases, replaces illegal chars.
// BuildName("agent-job", "claude-agent") → "agent_job_claude_agent"
func BuildName(names ...string) Name  // Name is type Name string
```

**Symbol verification — run before importing any bborbe/metrics symbol:**
```bash
grep -rn "func BuildName\b" $(go env GOPATH)/pkg/mod/github.com/bborbe/metrics@*/  2>/dev/null | head -3
grep -rn "type Name\b" $(go env GOPATH)/pkg/mod/github.com/bborbe/metrics@*/ 2>/dev/null | head -3
```

**Symbol verification — prometheus registry and testutil:**
```bash
grep -n "func (r \*Registry) MustRegister" $(go env GOPATH)/pkg/mod/github.com/prometheus/client_golang@v1.23.2/prometheus/registry.go
grep -n "func ToFloat64\|func CollectAndCount" $(go env GOPATH)/pkg/mod/github.com/prometheus/client_golang@v1.23.2/prometheus/testutil/testutil.go
```
</context>

<requirements>

## 1. Add dependencies to `lib/go.mod`

```bash
cd lib
go get github.com/bborbe/metrics
go get github.com/prometheus/client_golang/prometheus/testutil
go mod tidy
```

After `go mod tidy`, verify both appear as **direct** entries (no `// indirect`):
```bash
grep "bborbe/metrics\|prometheus/client_golang" lib/go.mod | grep -v indirect
```
Expected: two lines, both without `// indirect`.

## 2. Create `lib/metrics/metrics.go`

New file. License header required (copy from `lib/agent_status.go`). Package: `metrics`.

Follow `go-architecture-patterns.md` ordering: interface → constructor → struct → methods.

**Interface with counterfeiter annotation:**
```go
//counterfeiter:generate -o mocks/job-metrics.go --fake-name JobMetrics . JobMetrics

// JobMetrics records per-job Prometheus metrics at the result-publish boundary.
type JobMetrics interface {
    // RecordRun atomically increments the run counter and sets the last-run
    // gauge for the given status label. Both operations use the same label
    // value; they cannot drift.
    RecordRun(status agentlib.AgentStatus)
    // RecordDuration observes the run duration histogram.
    RecordDuration(d time.Duration)
}
```

Import `agentlib "github.com/bborbe/agent/lib"` for `AgentStatus`.

**Constructor:**
```go
// NewJobMetrics creates a JobMetrics that registers three collectors onto the
// caller-owned registry. The caller must NOT pass nil for registry.
// Registration failures (e.g. duplicate registration) panic — they are
// programmer errors caught at startup.
func NewJobMetrics(registry *prometheus.Registry, currentDateTime libtime.CurrentDateTime) JobMetrics {
    counter := prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "agent_job_run_total",
            Help: "Total number of agent job runs by terminal status.",
        },
        []string{"status"},
    )
    gauge := prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "agent_job_last_run_timestamp_seconds",
            Help: "Unix timestamp (seconds) of the last agent job run, by terminal status.",
        },
        []string{"status"},
    )
    histogram := prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "agent_job_duration_seconds",
            Help:    "Duration of agent job runs in seconds.",
            Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800},
        },
    )
    registry.MustRegister(counter, gauge, histogram)
    // Pre-initialize counter for all terminal statuses so absent() alerts work
    // even before any Job has run.
    counter.WithLabelValues(string(agentlib.AgentStatusDone)).Add(0)
    counter.WithLabelValues(string(agentlib.AgentStatusFailed)).Add(0)
    counter.WithLabelValues(string(agentlib.AgentStatusNeedsInput)).Add(0)
    return &jobMetrics{
        counter:         counter,
        gauge:           gauge,
        histogram:       histogram,
        currentDateTime: currentDateTime,
    }
}
```

**Struct:**
```go
type jobMetrics struct {
    counter         *prometheus.CounterVec
    gauge           *prometheus.GaugeVec
    histogram       prometheus.Histogram
    currentDateTime libtime.CurrentDateTime
}
```

**Methods:**
```go
func (m *jobMetrics) RecordRun(status agentlib.AgentStatus) {
    s := string(status)
    m.counter.WithLabelValues(s).Inc()
    m.gauge.WithLabelValues(s).Set(float64(m.currentDateTime.Now().Unix()))
}

func (m *jobMetrics) RecordDuration(d time.Duration) {
    m.histogram.Observe(d.Seconds())
}
```

**Imports needed:**
```go
import (
    "time"

    agentlib "github.com/bborbe/agent/lib"
    libtime "github.com/bborbe/time"
    "github.com/prometheus/client_golang/prometheus"
)
```

## 3. Add `BuildJobMetricsName` helper to `lib/metrics/metrics.go`

This makes `github.com/bborbe/metrics` a genuine direct dependency of `lib/go.mod` and provides a single canonical job-name builder for all consumers.

Add after the `jobMetrics` methods:

```go
// BuildJobMetricsName returns the standardized PushGateway job name for an
// agent job binary. All agent binaries must use this function to ensure the
// job name is consistent across deployments.
//
// Example: BuildJobMetricsName("claude-agent") → "agent_job_claude_agent"
func BuildJobMetricsName(agentName string) string {
    return bborbemetrics.BuildName("agent-job", agentName).String()
}
```

Add `bborbemetrics "github.com/bborbe/metrics"` to the import block.

Verify:
```bash
grep -n "BuildJobMetricsName\|bborbemetrics" lib/metrics/metrics.go
```
Expected: both names present.

## 4. Create `lib/metrics/metrics_suite_test.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate

func TestMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Suite")
}
```

## 5. Run `make generate` in `lib/`

```bash
cd lib && make generate
```

This triggers the `//go:generate` directive in `lib/metrics/metrics_suite_test.go` and the `//counterfeiter:generate` annotation in `lib/metrics/metrics.go`, producing `lib/metrics/mocks/job-metrics.go`.

Verify the mock exists with the correct struct name:
```bash
grep -n "type JobMetrics struct\|func.*JobMetrics.*RecordRun\|func.*JobMetrics.*RecordDuration" lib/metrics/mocks/job-metrics.go
```
Expected: struct definition and two method stubs.

If `make generate` also regenerates other mocks in `lib/mocks/` — that is expected and correct.

## 6. Create `lib/metrics/metrics_test.go`

Package: `metrics_test`. License header required.

The tests use `testutil.ToFloat64` and `testutil.CollectAndCount` from `github.com/prometheus/client_golang/prometheus/testutil`.
For deterministic gauge values, inject a `libtime.CurrentDateTime` mock via `libtime.NewCurrentDateTime()` and call `SetNow(knownTime)`.

**Imports:**
```go
import (
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/testutil"

    agentlib "github.com/bborbe/agent/lib"
    libmetrics "github.com/bborbe/agent/lib/metrics"
    libtime "github.com/bborbe/time"
)
```

**Test structure:**
```go
var _ = Describe("NewJobMetrics", func() {
    var (
        registry        *prometheus.Registry
        currentDateTime libtime.CurrentDateTime
        m               libmetrics.JobMetrics
    )

    BeforeEach(func() {
        registry = prometheus.NewRegistry()
        currentDateTime = libtime.NewCurrentDateTime()
        m = libmetrics.NewJobMetrics(registry, currentDateTime)
    })

    Context("collector registration", func() {
        It("registers exactly 3 metrics on the registry", func() {
            Expect(testutil.CollectAndCount(registry)).To(Equal(3))
        })
    })

    Context("counter pre-initialization", func() {
        It("pre-initializes done at zero", func() {
            // Gather from the registry to get the CounterVec; use testutil.CollectAndCompare
            // or extract via a helper. Simplest: use a fresh gather from registry.
            mfs, err := registry.Gather()
            Expect(err).NotTo(HaveOccurred())
            var counterMF *dto.MetricFamily
            for _, mf := range mfs {
                if mf.GetName() == "agent_job_run_total" {
                    counterMF = mf
                }
            }
            Expect(counterMF).NotTo(BeNil(), "agent_job_run_total metric family not found")
            Expect(counterMF.Metric).To(HaveLen(3), "expected 3 pre-initialized label combinations")
        })

        It("pre-initialized counter values are zero before any RecordRun call", func() {
            mfs, err := registry.Gather()
            Expect(err).NotTo(HaveOccurred())
            for _, mf := range mfs {
                if mf.GetName() == "agent_job_run_total" {
                    for _, m := range mf.Metric {
                        Expect(m.Counter.GetValue()).To(Equal(0.0))
                    }
                }
            }
        })
    })

    Context("RecordRun", func() {
        var fixedTime time.Time

        BeforeEach(func() {
            fixedTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
            currentDateTime.SetNow(libtime.DateTime(fixedTime))
        })

        It("increments the run counter for the given status", func() {
            m.RecordRun(agentlib.AgentStatusDone)
            mfs, err := registry.Gather()
            Expect(err).NotTo(HaveOccurred())
            for _, mf := range mfs {
                if mf.GetName() == "agent_job_run_total" {
                    for _, metric := range mf.Metric {
                        for _, lp := range metric.Label {
                            if lp.GetName() == "status" && lp.GetValue() == "done" {
                                Expect(metric.Counter.GetValue()).To(Equal(1.0))
                            }
                        }
                    }
                }
            }
        })

        It("sets the gauge to the injected timestamp (Unix seconds)", func() {
            m.RecordRun(agentlib.AgentStatusDone)
            mfs, err := registry.Gather()
            Expect(err).NotTo(HaveOccurred())
            for _, mf := range mfs {
                if mf.GetName() == "agent_job_last_run_timestamp_seconds" {
                    for _, metric := range mf.Metric {
                        for _, lp := range metric.Label {
                            if lp.GetName() == "status" && lp.GetValue() == "done" {
                                Expect(metric.Gauge.GetValue()).To(Equal(float64(fixedTime.Unix())))
                            }
                        }
                    }
                }
            }
        })
    })

    Context("RecordDuration", func() {
        It("observes the histogram without error", func() {
            m.RecordDuration(5 * time.Second)
            mfs, err := registry.Gather()
            Expect(err).NotTo(HaveOccurred())
            var histMF *dto.MetricFamily
            for _, mf := range mfs {
                if mf.GetName() == "agent_job_duration_seconds" {
                    histMF = mf
                }
            }
            Expect(histMF).NotTo(BeNil())
            Expect(histMF.Metric).To(HaveLen(1))
            Expect(histMF.Metric[0].Histogram.GetSampleCount()).To(Equal(uint64(1)))
        })

        It("observes the correct bucket (5s lands in the ≤5 bucket)", func() {
            m.RecordDuration(5 * time.Second)
            mfs, err := registry.Gather()
            Expect(err).NotTo(HaveOccurred())
            for _, mf := range mfs {
                if mf.GetName() == "agent_job_duration_seconds" {
                    found := false
                    for _, bucket := range mf.Metric[0].Histogram.Bucket {
                        if bucket.GetUpperBound() == 5.0 {
                            Expect(bucket.GetCumulativeCount()).To(Equal(uint64(1)))
                            found = true
                        }
                    }
                    Expect(found).To(BeTrue(), "bucket with upper bound 5.0 not found")
                }
            }
        })
    })

    Context("BuildJobMetricsName", func() {
        It("returns a stable job name string for claude-agent", func() {
            Expect(libmetrics.BuildJobMetricsName("claude-agent")).To(Equal("agent_job_claude_agent"))
        })

        It("returns a stable job name string for code-agent", func() {
            Expect(libmetrics.BuildJobMetricsName("code-agent")).To(Equal("agent_job_code_agent"))
        })
    })
})
```

**Note on dto import:** The `io_prometheus_client` DTO package is needed for inspecting `MetricFamily`. Add to imports:
```go
dto "github.com/prometheus/client_model/go"
```

Verify this is available:
```bash
grep "client_model" lib/go.mod
```
If absent, add it: `cd lib && go get github.com/prometheus/client_model/go && go mod tidy`

**Coverage check:**
```bash
cd lib && go test -coverprofile=/tmp/metrics-cover.out ./metrics/... && go tool cover -func=/tmp/metrics-cover.out | grep -E "metrics|total"
```
Expected: ≥80% coverage for `lib/metrics/metrics.go`.

## 7. Run iterative tests

```bash
cd lib && go test ./metrics/...
```

Fix compile errors before continuing. Common issues:
- `dto` import missing — add `dto "github.com/prometheus/client_model/go"` and ensure `client_model` is in `lib/go.mod`
- `libtime.DateTime(fixedTime)` type conversion — `DateTime` is `type DateTime time.Time`, so `libtime.DateTime(fixedTime)` is the correct cast
- `testutil.CollectAndCount(registry)` takes a `Collector`, not a `*Registry` — pass the registry directly (it implements `Gatherer` which works)

## 8. Run final precommit in `lib/`

```bash
cd lib && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

If `make precommit` reports mock drift, re-run `make generate`, verify only `lib/metrics/mocks/job-metrics.go` is new, then re-run the failing target.

</requirements>

<constraints>
- The package lives at `lib/metrics/` within the existing `github.com/bborbe/agent/lib` module. No new `go.mod` is created.
- Constructor signature `NewJobMetrics(registry *prometheus.Registry, currentDateTime libtime.CurrentDateTime) JobMetrics` is frozen. No parameters may be added or changed.
- Metric names (`agent_job_run_total`, `agent_job_last_run_timestamp_seconds`, `agent_job_duration_seconds`), the `status` label name, the three pre-initialized status values (`done`, `failed`, `needs_input`), and the bucket slice `[0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800]` are all frozen contracts. Do not alter any of them.
- `registry.MustRegister(...)` is used — registration failures panic (programmer error, caught at startup). Do NOT return an error from the constructor.
- No use of the default Prometheus registry (`prometheus.DefaultRegisterer`, `promauto`). The constructor must accept and register onto the caller-provided `*prometheus.Registry`.
- The constructor follows the pure-factory rule: no I/O, no conditionals on arguments, no `context.Background()`.
- Counterfeiter mock uses the explicit two-step form: `//go:generate` in `metrics_suite_test.go`, `//counterfeiter:generate` directive in `metrics.go`.
- External test package: `package metrics_test`. No `package metrics` in test files.
- No `task/executor/pkg/metrics/metrics.go` or any file outside `lib/metrics/` is modified by this prompt.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify package structure exists:
```bash
ls lib/metrics/
```
Expected: `metrics.go`, `metrics_suite_test.go`, `metrics_test.go`, `mocks/` directory.

Verify interface and constructor:
```bash
grep -n "type JobMetrics interface\|func NewJobMetrics\|BuildJobMetricsName" lib/metrics/metrics.go
```
Expected: three definitions.

Verify metric names are correct:
```bash
grep -n "agent_job_run_total\|agent_job_last_run_timestamp_seconds\|agent_job_duration_seconds" lib/metrics/metrics.go
```
Expected: three distinct metric names.

Verify bucket layout:
```bash
grep -n "0.1.*0.5.*1.*5.*10.*30.*60.*120.*300.*600.*1800" lib/metrics/metrics.go
```
Expected: one match in the histogram definition.

Verify pre-initialization:
```bash
grep -n "AgentStatusDone\|AgentStatusFailed\|AgentStatusNeedsInput" lib/metrics/metrics.go
```
Expected: three uses (one per pre-init call).

Verify mock was generated:
```bash
grep -n "type JobMetrics struct\|RecordRunCallCount\|RecordDurationCallCount" lib/metrics/mocks/job-metrics.go
```
Expected: struct definition and two call-count functions.

Verify BuildJobMetricsName:
```bash
grep -n "func BuildJobMetricsName" lib/metrics/metrics.go
```
Expected: one function definition.

Verify direct dependencies:
```bash
grep "bborbe/metrics\|prometheus/client_golang" lib/go.mod | grep -v indirect
```
Expected: both as direct (no `// indirect`).

Run tests:
```bash
cd lib && go test ./metrics/...
```
Expected: exit 0, all specs pass.

Run coverage:
```bash
cd lib && go test -coverprofile=/tmp/metrics-cover.out ./metrics/... && go tool cover -func=/tmp/metrics-cover.out | grep "total:"
```
Expected: ≥80% total coverage.

Run precommit:
```bash
cd lib && make precommit
```
Expected: exit 0.

</verification>
