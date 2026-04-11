---
status: draft
---

<summary>
- Add shared `agent_build_info` Prometheus gauge exposing build timestamp as Unix time
- Shared `BuildInfoMetrics` interface + impl in new `lib/metrics/` subpackage, mirroring `trading/lib/metrics/build-info-metrics.go`
- Wire `BuildGitCommit` and `BuildDate` env/arg fields into `task/controller/main.go` AND `task/executor/main.go`
- Both services call `libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)` once at startup
- Dockerfiles already set `BUILD_GIT_COMMIT` and `BUILD_DATE` as ENV — no Dockerfile or k8s changes needed
</summary>

<objective>
Expose build metadata (commit + timestamp) from `agent-task-controller` and
`agent-task-executor` as a Prometheus `agent_build_info` gauge. This lets us
tell from Prometheus/Grafana which commit is actually running in each
environment — essential for dev→prod rollout verification.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Pattern to mirror (read first):
- `/Users/bborbe/Documents/workspaces/trading/lib/metrics/build-info-metrics.go` — the canonical impl. Gauge with `Namespace: "trading"`, name `build_info` → exposes as `trading_build_info`. `SetBuildInfo(*libtime.DateTime)` is a no-op when the arg is nil.
- `/Users/bborbe/Documents/workspaces/trading/skeleton/main.go` — shows the wiring pattern: `BuildGitCommit` + `BuildDate` struct fields with arg/env tags, and `libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)` as the first statement of `Run`.

Key files to read before making changes (agent repo):
- `lib/go.mod` — lib is its own Go module (`github.com/bborbe/agent/lib`). `github.com/prometheus/client_golang v1.23.2` and `github.com/bborbe/time v1.25.3` are already present as indirect deps — `go mod tidy` will promote them to direct once the new code uses them.
- `lib/agent_task-frontmatter.go` — existing lib code style, BSD license header, `// Copyright (c) 2026 Benjamin Borbe All rights reserved.`
- `lib/lib_suite_test.go` — the Ginkgo suite entrypoint for the lib module. New metrics tests live in `lib/metrics/metrics_suite_test.go` to run in the same module test run.
- `task/executor/main.go` — target for field + wiring. Existing application struct already uses the `required`/`arg`/`env`/`usage`/`default` tag style.
- `task/controller/main.go` — same pattern as executor, identical wiring expected.
- `task/executor/Dockerfile` and `task/controller/Dockerfile` — **already** declare `ARG BUILD_GIT_COMMIT=none`, `ARG BUILD_DATE=unknown`, and `ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}` / `ENV BUILD_DATE=${BUILD_DATE}`. No Dockerfile change needed.
- `Makefile.docker` — already passes `BUILD_GIT_COMMIT=$(git rev-parse --short HEAD)` and `BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)` as build args. No Makefile change needed.
- `task/executor/pkg/metrics/metrics_test.go` — reference pattern for Prometheus gauge/counter tests using `prometheus.DefaultGatherer.Gather()`.

Important facts:
- The agent repo has **no** `lib/metrics/` subpackage yet — this prompt creates it.
- `libtime.DateTime` is the wrapper from `github.com/bborbe/time` that parses RFC3339 when used as an env/arg. It has an `.Unix()` method returning `int64`.
- The existing executor metric `agent_executor_task_events_total` uses `Name:` directly (no `Namespace:` field). The new build-info metric uses `Namespace: "agent"` + `Name: "build_info"` so the exported metric is `agent_build_info` — **shared** across both services, disambiguated by the Prometheus `job` label (same approach as `trading_build_info`).
</context>

<requirements>

1. **Create `lib/metrics/build-info-metrics.go`**

   New file at `lib/metrics/build-info-metrics.go`. Mirror the trading impl but with `Namespace: "agent"`:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package metrics

   import (
       libtime "github.com/bborbe/time"
       "github.com/prometheus/client_golang/prometheus"
   )

   var (
       buildInfo = prometheus.NewGauge(
           prometheus.GaugeOpts{
               Namespace: "agent",
               Name:      "build_info",
               Help:      "Build timestamp as Unix time. Service identified by Prometheus job label.",
           },
       )
   )

   func init() {
       prometheus.MustRegister(buildInfo)
   }

   //counterfeiter:generate -o ../mocks/build-info-metrics.go --fake-name BuildInfoMetrics . BuildInfoMetrics
   type BuildInfoMetrics interface {
       SetBuildInfo(buildDate *libtime.DateTime)
   }

   func NewBuildInfoMetrics() BuildInfoMetrics {
       return &buildInfoMetrics{}
   }

   type buildInfoMetrics struct{}

   func (m *buildInfoMetrics) SetBuildInfo(buildDate *libtime.DateTime) {
       if buildDate == nil {
           return
       }
       buildInfo.Set(float64(buildDate.Unix()))
   }
   ```

   Note: the counterfeiter directive path (`../mocks/build-info-metrics.go`) writes the mock into `lib/mocks/`, matching the existing lib mocks location.

2. **Create `lib/metrics/metrics_suite_test.go` and `lib/metrics/build-info-metrics_test.go`**

   Suite file:
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

   func TestMetrics(t *testing.T) {
       RegisterFailHandler(Fail)
       RunSpecs(t, "Metrics Suite")
   }
   ```

   Test file must cover:
   - `agent_build_info` is registered in the default Prometheus registry
   - `SetBuildInfo(nil)` does not panic and does not set the gauge
   - `SetBuildInfo(&validDateTime)` sets the gauge to the unix timestamp
   - Coverage target: ≥80% for the new package (trivial — the package is ~20 LOC)

   Use `prometheus.DefaultGatherer.Gather()` to read back the metric value, matching the style in `task/executor/pkg/metrics/metrics_test.go`. Use `libtime.NewDateTime(time.Unix(1234567890, 0))` or equivalent constructor to build a test `*libtime.DateTime` — check the `github.com/bborbe/time` package for the actual constructor signature before writing the test.

3. **Wire build-info fields into `task/executor/main.go`**

   Add two new struct fields on `application`, placed at the **end** of the struct (after `GeminiAPIKey`), matching the skeleton pattern exactly:

   ```go
   BuildGitCommit string             `required:"false" arg:"build-git-commit" env:"BUILD_GIT_COMMIT" usage:"Build Git commit hash"    default:"none"`
   BuildDate      *libtime.DateTime  `required:"false" arg:"build-date"       env:"BUILD_DATE"       usage:"Build timestamp (RFC3339)"`
   ```

   Add import alias `libmetrics "github.com/bborbe/agent/lib/metrics"` to the executor's import block.

   Insert this as the **first executable line** of `Run()`, before `glog.V(1).Infof("agent-task-executor started")`:

   ```go
   libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)
   ```

   Do NOT rename any existing fields. Do NOT change any other behaviour.

4. **Wire build-info fields into `task/controller/main.go`**

   Exactly the same changes as (3), but for the controller:
   - Append `BuildGitCommit` + `BuildDate` fields at the end of the controller's `application` struct (after `GeminiAPIKey`).
   - Add `libmetrics "github.com/bborbe/agent/lib/metrics"` import.
   - Insert `libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildDate)` as the first line of `Run()`, before `glog.V(1).Infof("agent-task-controller started")`.

5. **Update CHANGELOG.md**

   Add one line under the `## Unreleased` section (create the section if missing):
   ```
   - feat: Add `agent_build_info` Prometheus gauge and wire `BUILD_GIT_COMMIT` / `BUILD_DATE` into task/controller + task/executor so Prometheus can report the running commit per service
   ```

6. **Run `go mod tidy` in `lib/` and both service modules**

   After creating the new package, run `go mod tidy` in:
   - `lib/` — promotes `github.com/prometheus/client_golang` and `github.com/bborbe/time` from indirect → direct
   - `task/controller/` — refreshes go.sum if needed
   - `task/executor/` — refreshes go.sum if needed

   Do not manually edit `go.sum`. If `make precommit` reports further tidy issues, just re-run tidy.

</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Do NOT touch Dockerfiles or k8s manifests — build metadata is already passed via Dockerfile ARG→ENV.
- Do NOT introduce a second or per-service build_info metric. One shared `agent_build_info` gauge for the whole repo, mirroring trading's approach.
- Do NOT add `Help` text beyond the single line in the requirement — match the trading impl verbatim aside from the namespace.
- Do NOT touch `task/executor/pkg/metrics/` or `task/controller/pkg/metrics/` — build-info lives in `lib/metrics/` to be shared.
- No changes to `vault-cli` or any other external module.
- Repo-relative paths only.
</constraints>

<verification>
Run `make precommit` in each of: `lib/`, `task/executor/`, `task/controller/` — all must pass.

Spot-check expected after success:
- `grep -r 'agent_build_info\|Namespace: "agent"' lib/metrics/` shows the new metric.
- `grep -n 'SetBuildInfo(a.BuildDate)' task/executor/main.go task/controller/main.go` finds the wiring in both files.
- `grep -n 'BuildGitCommit\|BuildDate' task/executor/main.go task/controller/main.go` finds the struct fields in both files.
- `cd lib && go test -run TestMetrics ./metrics/...` passes.
</verification>
