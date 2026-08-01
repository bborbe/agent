---
status: draft
spec: [046-job-usage-metrics]
created: "2026-08-01T22:05:00Z"
branch: dark-factory/job-usage-metrics
---

<summary>
- Agent jobs can now report how many LLM tokens and conversation turns they burned, alongside the run count, status, and duration they already report.
- Tokens are broken out by kind — fresh input, output, cache-read, and cache-creation — so a plan sold in "prompts per window" can be compared against one sold in "requests per window".
- Turns get their own simple count.
- Both counters exist and read zero from the moment a process starts, before any job has run, so dashboards and alerts built on them work immediately instead of silently reporting no-data.
- A nonsense negative number from the subprocess is skipped for that one counter rather than crashing the job, and the other numbers in the same report still land.
- Everything already published stays exactly as it was — same names, same labels, same meanings.
- The dollar-cost figure the CLI reports is deliberately not recorded; under a non-Anthropic provider it is a fictional number.
- A short reference doc now lists every metric this package publishes, including the three that were previously undocumented.
- This prompt covers only the recording side; the callers that will invoke it live in a separate repository and are explicit follow-up work.
</summary>

<objective>
Add a `RecordUsage` method to the `JobMetrics` interface in `/workspace/metrics`, backed by two new pre-initialized Prometheus counters — `agent_job_tokens_total` (labelled by token `type`) and the unlabelled `agent_job_turns_total` — registered on the caller-owned registry, regenerate the counterfeiter fake, and document the full metric set in `docs/job-metrics.md`. Implements spec 046 Desired Behaviors 5-8 and Acceptance Criteria 6-13, 15.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (Ginkgo v2 / Gomega, counterfeiter, external test packages, `github.com/bborbe/errors`).

Read these coding-plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-prometheus-metrics-guide.md` — in particular the `counter-pre-initialization` rule (`.Add(0)` for every known label combination, because `rate()` returns no-data rather than zero for unseen series and alerts then never fire) and the `help-string-quality` rule (non-empty, non-duplicated, describes the right metric).
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, counterfeiter mocks, external test package, coverage >= 80%.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc starts with the identifier name, full sentences.
- `/home/node/.claude/plugins/marketplaces/coding/docs/documentation-guide.md` — style for the new `docs/job-metrics.md`.
- `/workspace/docs/dod.md` — definition of done for this repo.

Read these files IN FULL before editing:
- `/workspace/metrics/metrics.go` (96 lines) — the only file to change in this package.
- `/workspace/metrics/metrics_test.go` (164 lines) — the existing Ginkgo suite and its `registry.Gather()` assertion style.
- `/workspace/metrics/mocks/job-metrics.go` (113 lines) — the generated fake that must be regenerated.
- `/workspace/docs/agent-job-interface.md` — an existing docs file; match its heading/table style for the new `docs/job-metrics.md`.

Load-bearing snippets, verified verbatim against source.

`/workspace/metrics/metrics.go` lines 17-70 — the counterfeiter directive, the interface, and the constructor as they exist today:
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

// NewJobMetrics creates a JobMetrics that registers three collectors onto the
// caller-owned registry. The caller must NOT pass nil for registry.
// Registration failures (e.g. duplicate registration) panic — they are
// programmer errors caught at startup.
func NewJobMetrics(
	registry *prometheus.Registry,
	currentDateTime libtime.CurrentDateTime,
) JobMetrics {
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

type jobMetrics struct {
	counter         *prometheus.CounterVec
	gauge           *prometheus.GaugeVec
	histogram       prometheus.Histogram
	currentDateTime libtime.CurrentDateTime
}
```

`/workspace/metrics/metrics_test.go` lines 27-31 and 48-60 — the suite setup and the gather-assertion style to follow:
```go
	BeforeEach(func() {
		registry = prometheus.NewRegistry()
		currentDateTime = libtime.NewCurrentDateTime()
		m = libmetrics.NewJobMetrics(registry, currentDateTime)
	})
```
```go
		It("pre-initializes done at zero", func() {
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
```
The test file already imports `dto "github.com/prometheus/client_model/go"` — use `mf.GetName()`, `mf.GetHelp()`, `metric.Counter.GetValue()`, and `metric.Label` (`lp.GetName()` / `lp.GetValue()`) as it already does.

Prometheus API, grep-verified in `/home/node/go/pkg/mod/github.com/prometheus/client_golang@v1.23.2/prometheus/counter.go` (the version pinned in `/workspace/go.mod`):
```go
func NewCounter(opts CounterOpts) Counter          // counter.go:87
func NewCounterVec(opts CounterOpts, labelNames []string) *CounterVec  // counter.go:194
type Counter interface { ...; Inc(); Add(float64) }  // counter.go:41,44
```

`/workspace/metrics/metrics_suite_test.go` carries the `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate` directive that drives the `//counterfeiter:generate` line in `metrics.go`. The fake lands at `/workspace/metrics/mocks/job-metrics.go` (package `mocks`), NOT the repo-root `/workspace/mocks/`.

`/workspace/Makefile` `generate` target: `rm -rf mocks avro; mkdir -p mocks; echo "package mocks" > mocks/mocks.go; go generate -mod=mod ./...`. It wipes the ROOT `mocks/` only; `metrics/mocks/` is regenerated in place.

`grep -rn "JobMetrics" --include="*.go" /workspace` shows no in-repo implementer of the interface other than `jobMetrics` and the generated fake, and no in-repo caller of `NewJobMetrics` outside the tests. Adding an interface method therefore breaks nothing inside this repository.
</context>

<requirements>
1. **Add the token-type label constants to `/workspace/metrics/metrics.go`**, above `NewJobMetrics`. Label values are compile-time constants owned by this package — callers never pass label strings:

   ```go
   // Token type label values for agent_job_tokens_total. These are compile-time
   // constants: no caller-supplied or session-supplied value ever becomes a label,
   // so the family's cardinality is fixed at four series.
   const (
   	tokenTypeInput         = "input"
   	tokenTypeOutput        = "output"
   	tokenTypeCacheRead     = "cache_read"
   	tokenTypeCacheCreation = "cache_creation"
   )
   ```

2. **Add the exported `JobUsage` summary type** to `/workspace/metrics/metrics.go`, above the `JobMetrics` interface. One struct rather than five positional parameters, so call sites cannot silently transpose two token counts:

   ```go
   // JobUsage is the LLM token and turn summary of one finished agent job.
   // The zero value is valid and records nothing but zeros.
   type JobUsage struct {
   	// InputTokens is the count of fresh (non-cached) input tokens the job consumed.
   	InputTokens int64
   	// OutputTokens is the count of output tokens the job produced.
   	OutputTokens int64
   	// CacheReadTokens is the count of input tokens served from the prompt cache.
   	CacheReadTokens int64
   	// CacheCreationTokens is the count of input tokens written into the prompt cache.
   	CacheCreationTokens int64
   	// Turns is the number of conversation turns the job took.
   	Turns int64
   }
   ```

3. **Add exactly one method to the `JobMetrics` interface**, keeping the two existing methods and their GoDoc untouched:

   ```go
   	// RecordUsage records the token and turn summary of a finished job: each
   	// token count advances its own type-labelled series and the turn count
   	// advances the turn counter. A negative value is skipped for that counter
   	// only; the other counters in the same call still record.
   	RecordUsage(usage JobUsage)
   ```

4. **Construct and register the two new collectors in `NewJobMetrics`.** Build them alongside the existing three, register them in the SAME `registry.MustRegister(...)` call, and pre-initialize all five new series to zero next to the existing pre-initialization block:

   ```go
   	tokenCounter := prometheus.NewCounterVec(
   		prometheus.CounterOpts{
   			Name: "agent_job_tokens_total",
   			Help: "Total LLM tokens consumed by agent jobs, by token type.",
   		},
   		[]string{"type"},
   	)
   	turnCounter := prometheus.NewCounter(
   		prometheus.CounterOpts{
   			Name: "agent_job_turns_total",
   			Help: "Total number of conversation turns taken by agent jobs.",
   		},
   	)
   	registry.MustRegister(counter, gauge, histogram, tokenCounter, turnCounter)
   ```
   and, immediately after the three existing `counter.WithLabelValues(...).Add(0)` lines:
   ```go
   	// Pre-initialize the token series and the turn counter so rate() evaluates to
   	// zero (not no-data) for a process that has not yet run a job.
   	tokenCounter.WithLabelValues(tokenTypeInput).Add(0)
   	tokenCounter.WithLabelValues(tokenTypeOutput).Add(0)
   	tokenCounter.WithLabelValues(tokenTypeCacheRead).Add(0)
   	tokenCounter.WithLabelValues(tokenTypeCacheCreation).Add(0)
   	turnCounter.Add(0)
   ```
   Add `tokenCounter *prometheus.CounterVec` and `turnCounter prometheus.Counter` fields to the `jobMetrics` struct and populate them in the returned literal. Update the `NewJobMetrics` GoDoc: it now registers FIVE collectors, not three — keep the rest of that comment (caller-owned registry, MUST NOT be nil, registration failures panic as startup-time programmer errors) verbatim.

   Both names end in `_total` — the Prometheus client panics at registration otherwise, and that panic would hit every consumer at startup.

5. **Implement `RecordUsage` on `*jobMetrics`**, below `RecordDuration`, with a private helper that guards the negative case per counter:

   ```go
   func (m *jobMetrics) RecordUsage(usage JobUsage) {
   	m.addTokens(tokenTypeInput, usage.InputTokens)
   	m.addTokens(tokenTypeOutput, usage.OutputTokens)
   	m.addTokens(tokenTypeCacheRead, usage.CacheReadTokens)
   	m.addTokens(tokenTypeCacheCreation, usage.CacheCreationTokens)
   	if usage.Turns >= 0 {
   		m.turnCounter.Add(float64(usage.Turns))
   	}
   }

   // addTokens advances the token counter for one token type. A negative count is
   // skipped: prometheus.Counter.Add panics on a negative delta, and the counts
   // originate from a subprocess's stdout, so a hostile or buggy value must not be
   // able to take the job down.
   func (m *jobMetrics) addTokens(tokenType string, count int64) {
   	if count < 0 {
   		return
   	}
   	m.tokenCounter.WithLabelValues(tokenType).Add(float64(count))
   }
   ```
   Adding zero is fine and is a no-op on the counter value — do not special-case it.

6. **Regenerate the counterfeiter fake** so `/workspace/metrics/mocks/job-metrics.go` satisfies the widened interface:
   ```bash
   cd /workspace && go generate -mod=mod ./metrics/...
   ```
   Do not hand-edit the generated file. Confirm with `grep -n 'RecordUsage' /workspace/metrics/mocks/job-metrics.go` (must return at least one line) and that the trailing `var _ metrics.JobMetrics = new(JobMetrics)` assertion still compiles.

7. **Extend `/workspace/metrics/metrics_test.go`** with new `Context` blocks inside the existing `Describe("NewJobMetrics", ...)`, reusing the existing `BeforeEach` (fresh `prometheus.NewRegistry()` per spec). Do NOT modify or delete any existing test. Add a small file-local helper for family lookup to keep each `It` short, e.g.:
   ```go
   	findFamily := func(name string) *dto.MetricFamily {
   		mfs, err := registry.Gather()
   		Expect(err).NotTo(HaveOccurred())
   		for _, mf := range mfs {
   			if mf.GetName() == name {
   				return mf
   			}
   		}
   		return nil
   	}
   ```
   Cover exactly these cases:

   - **AC6 — both families register.** Gather and assert the family names contain `agent_job_tokens_total` and `agent_job_turns_total`.
   - **AC7a — token pre-initialization.** `findFamily("agent_job_tokens_total").Metric` has `HaveLen(4)`; the collected `type` label values are exactly `input`, `output`, `cache_read`, `cache_creation` (use `ConsistOf`); every `metric.Counter.GetValue()` equals `0.0`.
   - **AC7b — turn pre-initialization.** `agent_job_turns_total` is present with one metric at value `0.0`.
   - **AC8 — distinct values per kind.** Call `m.RecordUsage(libmetrics.JobUsage{InputTokens: 11, OutputTokens: 22, CacheReadTokens: 33, CacheCreationTokens: 44, Turns: 5})` and assert the four series equal `11.0`, `22.0`, `33.0`, `44.0` respectively (match on the `type` label value, not on slice index — `Gather()` ordering is not part of the contract) and the turn counter equals `5.0`. Deliberately use four DIFFERENT token values so a transposed field assignment fails the test.
   - **AC9 — accumulation.** Call `RecordUsage` twice with the same summary and assert every series equals twice its value (`22.0`, `44.0`, `66.0`, `88.0`, turns `10.0`).
   - **AC10 — negative value is skipped, siblings still record.** Call `m.RecordUsage(libmetrics.JobUsage{InputTokens: -5, OutputTokens: 7, CacheReadTokens: 8, CacheCreationTokens: 9, Turns: 3})` inside `Expect(func() { ... }).NotTo(Panic())`, then assert `input` is still `0.0` while `output` is `7.0`, `cache_read` is `8.0`, `cache_creation` is `9.0`, and turns is `3.0`. Add a second `It` for a negative `Turns` (e.g. `JobUsage{InputTokens: 4, Turns: -1}`): no panic, turns stays `0.0`, `input` is `4.0`.
   - **AC11 — help-string quality.** 🚨 **Call `m.RecordRun(agentlib.AgentStatusDone)` FIRST, before gathering.** `agent_job_last_run_timestamp_seconds` is a `GaugeVec` with no pre-initialization, so it collects nothing and Prometheus omits the family entirely from `registry.Gather()` until a `RecordRun` call materializes a child. The existing suite already encodes this — `metrics_test.go` asserts `ContainElements("agent_job_run_total", "agent_job_duration_seconds")` and deliberately omits the gauge. Without the priming call, any assertion expecting five families produces a red test. After priming, gather all families; assert every family's `GetHelp()` is non-empty and that the full set of help strings across all five families is pairwise distinct (e.g. collect them into a slice, then assert the deduplicated length equals the slice length).
   - **AC13 — no regression.** The existing tests already cover `agent_job_run_total` (3 pre-initialized series at `0.0`), the gauge, the histogram, and `BuildJobMetricsName`; they must pass UNMODIFIED. Do not touch them.

8. **Write `/workspace/docs/job-metrics.md`** documenting every metric this package publishes. It must mention at least five distinct `agent_job_` names across at least five lines (the spec checks `grep -c 'agent_job_' docs/job-metrics.md` >= 5). Include:
   - A one-paragraph intro: these are per-job metrics registered on a caller-owned registry by `metrics.NewJobMetrics` and pushed to the PushGateway under the job name from `metrics.BuildJobMetricsName` (example: `claude-agent` -> `agent_job_claude_agent`), so the per-agent breakdown comes from the push job name, not from a metric label.
   - A table with columns Metric / Type / Labels / Meaning covering `agent_job_run_total` (counter, `status`), `agent_job_last_run_timestamp_seconds` (gauge, `status`), `agent_job_duration_seconds` (histogram, none), `agent_job_tokens_total` (counter, `type` with the four values), and `agent_job_turns_total` (counter, none).
   - A short "Pre-initialization" note explaining that every counter series is created at zero at construction so `rate()` evaluates to zero rather than no-data before the first job runs.
   - A short "Not recorded" note: the Claude CLI's cost figure is deliberately not captured, because under a non-Anthropic base URL the CLI computes it at Anthropic list pricing and the number would be counterfactual. Refer to it as "the CLI's cost field" — do NOT write the literal key name (the spec greps for it under `metrics/`, and keeping the phrasing consistent across docs and code avoids a future copy-paste into either tree).
   - Keep lines readable; no trailing whitespace.

9. **Add a CHANGELOG entry.** In `/workspace/CHANGELOG.md`, use the `## Unreleased` section immediately after the SemVer preamble and before `## v0.79.0`. If a sibling prompt already created that section, APPEND this bullet to it — do not create a second `## Unreleased` heading:
   ```markdown
   - feat: metrics: `JobMetrics` gains `RecordUsage(JobUsage)`, backed by two new pre-initialized counters `agent_job_tokens_total` (label `type`: input, output, cache_read, cache_creation) and `agent_job_turns_total`; negative values are skipped instead of panicking
   ```
</requirements>

<constraints>
- Do NOT record the Claude CLI's cost figure, and do NOT add a field for it to `JobUsage`. Under a non-Anthropic base URL that number is computed at Anthropic list pricing and is counterfactual; a wrong number in a cost dashboard is worse than no cost dashboard. The literal key name must not appear anywhere under `/workspace/metrics/` — including comments and test names. Write "the CLI's cost field" instead. (Spec Non-goal, invariant.)
- Do NOT add an `agent` label to either new metric. The per-agent breakdown already comes free from the PushGateway job name; adding the label would duplicate an existing dimension. (Spec Non-goal, invariant.)
- Do NOT add a config flag, env var, or opt-out to disable usage recording. Recording is unconditional. (Spec Non-goal.)
- Do NOT add histograms, summaries, or per-model / per-session labels for tokens. Counters only, exactly two new families, exactly one label on one of them. (Spec Non-goal.)
- Do NOT wire `RecordUsage` into any consumer binary or call site. The call sites live in a separate repository and are explicit follow-up. (Spec Non-goal.)
- Do NOT touch `/workspace/claude/` in this prompt — the parser change is a sibling prompt and the two packages share no symbol. `JobUsage` is defined in `metrics` and must NOT import anything from `claude`.
- Both new counter names MUST end in `_total`; registration panics otherwise and the panic hits every consumer at startup. (Spec Constraint.)
- Counter pre-initialization with `.Add(0)` is mandatory for all four token label combinations and the turn counter. Without it `rate()` returns no-data instead of zero for unseen series and alerts built on it never fire — neither when healthy nor when broken. (Spec Constraint; `go-prometheus-metrics-guide.md` counter-pre-initialization rule.)
- Each new family gets its own non-empty help text, distinct from the other new family and from all three pre-existing families. (Spec Constraint; `go-prometheus-metrics-guide.md` help-string-quality rule.)
- The registry stays caller-owned — do NOT substitute `prometheus.DefaultRegisterer` or any global. Registration failures continue to panic via `MustRegister`; they are startup-time programmer errors. (Spec Constraint.)
- A negative count must be skipped for that counter only, never passed to `Counter.Add` (which panics), and must not stop the sibling counters in the same call from recording. Negative is the only unrepresentable case reachable from integer wire fields — do NOT add handling for NaN, overflow, or other float pathologies. (Spec Desired Behavior 7, Security.)
- Everything already published stays byte-identical in name, label set, and semantics: `agent_job_run_total`, `agent_job_last_run_timestamp_seconds`, `agent_job_duration_seconds`, and `BuildJobMetricsName` are untouched. The existing tests must pass unmodified. (Spec Desired Behavior 8.)
- The counterfeiter fake at `/workspace/metrics/mocks/job-metrics.go` must be regenerated (not hand-edited) so it satisfies the widened interface. (Spec Constraint.)
- Metric and label naming stays under the existing `agent_job_` family prefix. (Spec Constraint.)
- Every new exported type, field, and method carries a GoDoc comment. (Spec Constraint.)
- Error handling stays on `github.com/bborbe/errors` with context wrapping if any error path is introduced (none is expected here — recording is in-memory counter arithmetic and returns nothing). No `fmt.Errorf`, no bare `return err`. (Spec Constraint.)
- Tests are Ginkgo v2 / Gomega in the external `metrics_test` package, asserting via `registry.Gather()` on a fresh `prometheus.NewRegistry()` per spec. (Spec Constraint.)
- Coverage for the changed package must be >= 80%; the negative-value tests are what cover the `count < 0` and `Turns < 0` branches.
- Line length limit is 100 characters (golines runs in `make format`).
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
# Package tests — AC6 through AC13.
cd /workspace && go test -mod=mod -race ./metrics/... 2>&1 | tail -20
# Must report ok / PASS.
```

```bash
# Coverage for the changed package.
cd /workspace && go test -coverprofile=/tmp/cover.out -mod=mod ./metrics/... && go tool cover -func=/tmp/cover.out | grep -E 'RecordUsage|addTokens|NewJobMetrics'
# RecordUsage and addTokens must be >= 80%.
```

```bash
# AC14 — the fake implements the new method.
grep -n 'RecordUsage' /workspace/metrics/mocks/job-metrics.go
# Must return at least one line.
```

```bash
# AC15 — the CLI's cost field must not have entered the tree.
! grep -rq 'total_cost_usd' /workspace/metrics/
# Must return zero lines (exit 1).
```

```bash
# AC — both new families are declared in the source.
grep -n 'agent_job_tokens_total\|agent_job_turns_total' /workspace/metrics/metrics.go
# Must return AT LEAST 2 lines (the two Name: fields). More is expected and fine —
# requirement 1 mandates a const comment that also names agent_job_tokens_total.
# Do NOT delete that comment to make a count match.
```

```bash
# Docs coverage.
grep -c 'agent_job_' /workspace/docs/job-metrics.md
# Must return >= 5.
```

```bash
# Changelog entry present.
grep -n -A6 '## Unreleased' /workspace/CHANGELOG.md | grep -iE 'token|turn'
# Must return at least one line.
```

```bash
# Final full validation at the repository root.
cd /workspace && make precommit
# Must exit 0.
```
</verification>
