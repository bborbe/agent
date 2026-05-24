---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Adds a `Metrics` interface to `pkg/metrics/metrics.go` with all existing metric methods
- Updates all business logic packages (`pkg/gitrestclient/`, `pkg/command/`, `pkg/sync/`, `pkg/publisher/`) to use the injected interface instead of calling package-level vars directly
- Enables Counterfeiter mock generation for testability
</summary>

<objective>
The current `pkg/metrics/metrics.go` exports package-level `promauto` variables (e.g., `ScanCyclesTotal`, `TasksPublishedTotal`) that are called directly from business logic packages. This prevents mocking metrics in tests — the anti-pattern documented in `go-prometheus-metrics-guide.md`. After this fix, all metric calls go through an injected `Metrics` interface that can be mocked with Counterfeiter.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- `task/controller/pkg/metrics/metrics.go` — current package-level vars and init() function
- `task/controller/pkg/publisher/task_publisher.go` — calls `metrics.Xxx`
- `task/controller/pkg/gitrestclient/git_rest_client.go` — calls `metrics.KafkaConsumePausedTotal.Inc()`
- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — calls `metrics.FrontmatterCommandsTotal`
- `task/controller/pkg/command/task_update_frontmatter_executor.go` — calls `metrics.FrontmatterCommandsTotal`
- `task/controller/pkg/sync/sync_loop.go` — calls `metrics.ScanCyclesTotal`, `metrics.TasksPublishedTotal`

Read the `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` for the interface pattern.
</context>

<requirements>

1. **Add `Metrics` interface to `pkg/metrics/metrics.go`**

   Add after the existing metric declarations (before `init()`). The interface must include all methods corresponding to the existing package-level vars:

   ```go
   // Metrics defines the interface for accessing Prometheus metrics.
   // Use this interface in business logic packages to enable mock injection in tests.
   type Metrics interface {
       ScanCyclesTotal(outcome string) metrics.Counter
       TasksPublishedTotal(outcome string) metrics.Counter
       ResultsWrittenTotal(outcome string) metrics.Counter
       GitPushTotal(outcome string) metrics.Counter
       ConflictResolutionsTotal() metrics.Counter
       FrontmatterCommandsTotal(operation, outcome string) metrics.Counter
       GitRestCallsTotal(operation, status string) metrics.Counter
   }

   // metrics is the default implementation using promauto.
   // A synthetic test implementation can be created via Counterfeiter.
   var _ Metrics = &metrics{}
   ```

2. **Rename the existing struct** from `metrics` (lowercase, package-level var) to `defaultMetrics` to avoid conflict with the interface name. Update all internal references.

   The struct that currently holds the promauto counters should be renamed to `defaultMetrics` and implement the `Metrics` interface.

3. **Update all business logic packages to accept `Metrics` via constructor injection**

   For each consumer package, update the constructor to accept `m metrics.Metrics` and store it in the struct. Update all `metrics.Xxx.Inc()` calls to `m.Xxx().Inc()`.

   Packages to update:
   - `pkg/publisher/task_publisher.go` — `NewTaskPublisher` signature
   - `pkg/gitrestclient/git_rest_client.go` — `NewGitRestClient` signature  
   - `pkg/sync/sync_loop.go` — `NewSyncLoop` signature

   Note: `pkg/command/` executors call metrics from within the closure passed to `cdb.CommandObjectExecutorTxFunc` — since the closure receives `tx libkv.Tx` and has no constructor, either inject `Metrics` into the executor struct or handle this in a follow-up prompt.

4. **Update `main.go`** to pass `metrics.Default()` (or a `metrics.New()` constructor) to all consumers.

5. **Run `make generate`** to produce Counterfeiter mocks for the new `Metrics` interface.

6. **Run tests:**
   ```bash
   cd task/controller && make test
   ```
   All tests must pass.

7. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change files under `task/controller/`
- Do NOT commit — dark-factory handles git
- Metric names (`ScanCyclesTotal`, `TasksPublishedTotal`, etc.) must remain stable — do NOT rename the Prometheus metric names themselves
- Follow project conventions: factory pattern with `New*` constructors
</constraints>

<verification>
cd task/controller && make precommit
</verification>
