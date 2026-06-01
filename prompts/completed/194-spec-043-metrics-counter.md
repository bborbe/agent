---
status: completed
spec: [043-surface-vault-scanner-skip-failures]
summary: Added agent_controller_vault_scanner_skipped_files_total counter with five reason labels, interface method, pre-initialisation, tests, and regenerated mock
container: agent-vault-scanner-alerts-exec-194-spec-043-metrics-counter
dark-factory-version: v0.173.0
created: "2026-06-01T20:00:00Z"
queued: "2026-06-01T20:36:10Z"
started: "2026-06-01T20:36:12Z"
completed: "2026-06-01T20:37:24Z"
branch: dark-factory/surface-vault-scanner-skip-failures
---

<summary>
- The controller's vault scanner is about to start counting every file it skips, and the counter needs a home before any scanner code changes
- A new labelled counter is registered through the existing promauto pattern, alongside the seven existing counters in the same package
- The counter's label values form a closed set of five strings, each declared as an exported constant so future scanner code can reference them by name instead of inlining
- All five label values are pre-initialised to zero in the package's init block so dashboards see them at zero before the first skip ever happens
- The Metrics interface gains one new method that returns the counter for a given reason
- The counterfeiter mock is regenerated to match the new interface — this is a no-op for scanner tests, but the generated file must stay in sync
- Two existing Ginkgo tests are extended: the registration list and a new pre-initialisation test that names every label value
- No scanner code, no constructor signatures, no main.go changes — that is prompt two
</summary>

<objective>
Add the `agent_controller_vault_scanner_skipped_files_total` counter, its interface method, the five-label pre-initialisation, and the matching tests to `task/controller/pkg/metrics`. After this prompt the metrics package exposes the counter at `/metrics` with all five reason labels at zero, ready for the scanner to call in prompt two.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-prometheus-metrics-guide.md` — the Counter Pre-Initialization Pattern section is the load-bearing reference for this prompt.

Key files to read in full before editing:
- `task/controller/pkg/metrics/metrics.go` — the file to extend; read fully to mirror the existing style
- `task/controller/pkg/metrics/metrics_test.go` — read fully; the existing tests are the template for the new ones
- `task/controller/pkg/metrics/metrics_suite_test.go` — Ginkgo suite setup, no changes

Reference file (no edits):
- `task/controller/mocks/metrics.go` — counterfeiter mock for the `Metrics` interface. `make generate` regenerates it from the `//counterfeiter:generate` directive at the top of `metrics.go`. Do NOT hand-edit.

Spec being implemented: `specs/in-progress/043-surface-vault-scanner-skip-failures.md`. The five reason values are spelled out in `## Desired Behavior` items 2 and 3.
</context>

<requirements>

1. **Add the exported reason constants**

   In `task/controller/pkg/metrics/metrics.go`, add a new `const` block immediately after the existing `KafkaConsumePausedTotal` declaration (around line 140) and before the `init()` function. The block exports the five closed-set reason values as untyped string constants. Mirror the style of the existing label strings (lowercase, snake_case) already used in the file:

   ```go
   const (
       ReasonInvalidFrontmatter          = "invalid_frontmatter"
       ReasonDuplicateFrontmatterInvalid = "duplicate_frontmatter_invalid"
       ReasonEmptyStatus                 = "empty_status"
       ReasonInjectTaskIdentifierFailed  = "inject_task_identifier_failed"
       ReasonReadFailed                  = "read_failed"
   )
   ```

2. **Add the counter declaration**

   **Insertion order (critical):** requirement #1 inserts the `const` block first, immediately after `KafkaConsumePausedTotal`. This requirement #2 inserts the `var SkippedFilesTotal` declaration **immediately after the const block** (so the const block sits between `KafkaConsumePausedTotal` and the new `var`, and both new decls sit before `init()`).

   Add a new package-level `CounterVec` immediately after the const block from requirement #1 and before the `init()` function:

   ```go
   // SkippedFilesTotal counts vault task files the scanner skipped during a scan cycle,
   // labelled by the structured reason for the skip. A non-zero value on any label
   // indicates operator-actionable vault health issues (broken frontmatter, empty status,
   // unreadable files, injection failures); a stuck broken file will keep the relevant
   // label rate-positive until repaired. The closed set of reason values is declared
   // as constants above and pre-initialised in init().
   var SkippedFilesTotal = promauto.NewCounterVec(
       prometheus.CounterOpts{
           Name: "agent_controller_vault_scanner_skipped_files_total",
           Help: "Total number of vault task files the scanner skipped during a scan cycle, by reason. Increments exactly once per skipped file per cycle — re-scans of an unrepaired broken file keep the rate positive.",
       },
       []string{"reason"},
   )
   ```

   Use `promauto.NewCounterVec` (the import is already present at the top of the file from `github.com/prometheus/client_golang/prometheus/promauto`). The metric name MUST be exactly `agent_controller_vault_scanner_skipped_files_total` (the AC#1 contract).

3. **Add the interface method**

   Extend the `Metrics` interface in the same file. Insert a single new line at the end of the method list, immediately after `KafkaConsumePausedTotal() prometheus.Counter`:

   ```go
   SkippedFilesTotal(reason string) prometheus.Counter
   ```

   Keep the interface methods sorted by visual symmetry with the existing list — the new method sits between `KafkaConsumePausedTotal` and the closing brace of the interface.

4. **Add the default-implementation method**

   Add the matching method on `*defaultMetrics` immediately after the existing `KafkaConsumePausedTotal` method body:

   ```go
   func (m *defaultMetrics) SkippedFilesTotal(reason string) prometheus.Counter {
       return SkippedFilesTotal.WithLabelValues(reason)
   }
   ```

   Do NOT change the existing `var _ Metrics = &defaultMetrics{}` compile-time assertion — the new method keeps the interface satisfied.

5. **Pre-initialise all five reason labels**

   At the end of the existing `init()` function (the last existing call is the loop over `GitRestCallsTotal` at the bottom of the function), add a final pre-init block:

   ```go
   for _, reason := range []string{
       ReasonInvalidFrontmatter,
       ReasonDuplicateFrontmatterInvalid,
       ReasonEmptyStatus,
       ReasonInjectTaskIdentifierFailed,
       ReasonReadFailed,
   } {
       SkippedFilesTotal.WithLabelValues(reason).Add(0)
   }
   ```

   The `Add(0)` pattern is what the existing `init()` already uses elsewhere in the file. This satisfies the Counter Pre-Initialization Pattern from the metrics guide and the AC#2 contract.

6. **Extend the registration test**

   In `task/controller/pkg/metrics/metrics_test.go`, the existing test `It("registers all expected metric names in the default registry", ...)` keeps its `names` map but adds one new assertion at the end of the assertion list. Insert this line as the last `Expect(names).To(HaveKey(...))` call inside that `It` block, after the existing seven:

   ```go
   Expect(names).To(HaveKey("agent_controller_vault_scanner_skipped_files_total"))
   ```

   Do NOT add a separate `It` for registration. The list-based assertion style is the project convention.

7. **Add a pre-initialisation test**

   In the same `metrics_test.go` file, add a new `It` block immediately after the existing `It("pre-initializes all git_rest_calls_total label combinations", ...)` (which is currently the last `It` in the file). The new test asserts all five reason labels are visible at zero before any scan:

   ```go
   It("pre-initializes all vault_scanner_skipped_files_total label combinations", func() {
       mfs, err := prometheus.DefaultGatherer.Gather()
       Expect(err).NotTo(HaveOccurred())

       labels := gatherLabels(mfs, "agent_controller_vault_scanner_skipped_files_total", "reason")
       Expect(labels).To(ContainElements(
           "invalid_frontmatter",
           "duplicate_frontmatter_invalid",
           "empty_status",
           "inject_task_identifier_failed",
           "read_failed",
       ))
   })
   ```

   Use the inline string literals (not the constants) for the assertions — this matches the style of the other pre-init tests in the file and makes the test self-documenting.

8. **Regenerate the counterfeiter mock**

   Run from the service directory:

   ```bash
   cd task/controller && make generate
   ```

   This regenerates `task/controller/mocks/metrics.go` to include stub methods and helpers for the new `SkippedFilesTotal` interface method. Verify the regeneration succeeded by checking that `mocks/metrics.go` now contains a `SkippedFilesTotalStub` field and a `func (fake *Metrics) SkippedFilesTotal(reason string) prometheus.Counter` method.

9. **Run precommit and verify**

   From `task/controller`:

   ```bash
   cd task/controller && make precommit
   ```

   Must pass with exit code 0. The new tests in `metrics_test.go` must turn green; the existing seven `It` blocks must remain green (AC#7 — no regression in the existing counter registrations or pre-init assertions).
</requirements>

<constraints>
- Do NOT change the existing seven counters, their names, their labels, or their pre-initialised values. The `metrics_test.go` registration list gains one entry; nothing is removed.
- Do NOT change the existing `Metrics` interface methods other than adding the new one. Do NOT reorder existing methods.
- Do NOT add a config knob, env var, or feature flag to disable the counter or its pre-initialisation. The spec Non-goal #5 forbids it.
- Do NOT change the `Metrics` interface to use a more specific return type (e.g. `*prometheus.CounterVec`). All existing methods return `prometheus.Counter`; the new method must match.
- Do NOT add additional reason values to the closed set. The spec names exactly five: `invalid_frontmatter`, `duplicate_frontmatter_invalid`, `empty_status`, `inject_task_identifier_failed`, `read_failed`. Adding a sixth without a spec change is a YAGNI violation.
- Do NOT touch `task/controller/pkg/scanner/vault_scanner.go` or any other scanner file in this prompt. Scanner instrumentation is prompt two.
- Do NOT touch `task/controller/main.go` in this prompt. Constructor signature change and wiring is prompt two.
- Do NOT hand-edit `task/controller/mocks/metrics.go`. The `make generate` step is the only source of truth.
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- Use the existing `bborbe/errors` and `github.com/golang/glog` patterns if any new log lines are added; the only file edited in this prompt is `metrics.go` and `metrics_test.go`, neither of which logs.
</constraints>

<verification>
```bash
cd task/controller && go test ./pkg/metrics/... -v -run 'vault_scanner_skipped'
```
Must show two passing `It` blocks: the extended registration test and the new pre-init test.

```bash
cd task/controller && go test ./pkg/metrics/... -v
```
Must pass with all pre-existing tests still green (AC#7).

```bash
cd task/controller && grep -n 'agent_controller_vault_scanner_skipped_files_total' pkg/metrics/metrics.go
```
Must show one match (the `Name:` field in the `CounterOpts` literal).

```bash
cd task/controller && grep -nE 'ReasonInvalidFrontmatter|ReasonDuplicateFrontmatterInvalid|ReasonEmptyStatus|ReasonInjectTaskIdentifierFailed|ReasonReadFailed' pkg/metrics/metrics.go
```
Must show exactly **ten** matches: five `const` declarations (one per name) PLUS five references in the `init()` pre-init block's `[]string{...}` slice (one per name). Per-name count is exactly 2; total across all five names is 10.

```bash
cd task/controller && grep -nE 'SkippedFilesTotal' pkg/metrics/metrics.go
```
Must show at least three matches: the `var SkippedFilesTotal =` declaration, the interface method `SkippedFilesTotal(reason string) prometheus.Counter`, and the `func (m \*defaultMetrics) SkippedFilesTotal(...)` implementation. A fourth match in the `init()` pre-init block is expected.

```bash
cd task/controller && grep -nE 'SkippedFilesTotal' mocks/metrics.go | head -5
```
Must show the regenerated mock has the new method (e.g. `SkippedFilesTotalStub`, `func (fake \*Metrics) SkippedFilesTotal`).

```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
