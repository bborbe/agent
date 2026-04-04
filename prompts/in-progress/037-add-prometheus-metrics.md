---
status: approved
created: "2026-04-04T20:19:27Z"
queued: "2026-04-04T20:19:27Z"
---

<summary>
- Both services gain operational visibility through Prometheus counters
- task/controller tracks scan cycles, task events published, results written, git push retries, and conflict resolutions
- task/executor tracks task events consumed, jobs spawned, jobs skipped (by reason), and duplicate checks
- All metrics follow Prometheus naming conventions with agent_controller_ and agent_executor_ prefixes
- Existing /metrics endpoint already serves default Go metrics — custom metrics appear automatically via promauto
</summary>

<objective>
Add Prometheus metrics to task/controller and task/executor so pipeline health is observable via Grafana/alerting. Currently both services expose /metrics but have zero custom metrics — operators have no visibility into scan rates, job spawn success, result writeback failures, or conflict resolution events.
</objective>

<context>
Read CLAUDE.md for project conventions.

**task/controller** key files:
- `task/controller/pkg/sync/sync_loop.go` — `processResult` method publishes changed/deleted tasks
- `task/controller/pkg/result/result_writer.go` — `WriteResult` writes agent results back to vault files
- `task/controller/pkg/gitclient/git_client.go` — `pushWithRetry`, `resolveConflicts`, `handleConflictsAndPush`
- `task/controller/pkg/publisher/task_publisher.go` — `PublishChanged`, `PublishDeleted`
- `task/controller/main.go` — wires everything, already has `promhttp.Handler()` on `/metrics`

**task/executor** key files:
- `task/executor/pkg/handler/task_event_handler.go` — `ConsumeMessage` with filtering + job spawn
- `task/executor/pkg/spawner/job_spawner.go` — `SpawnJob`, `IsJobActive`
- `task/executor/main.go` — wires everything, already has `promhttp.Handler()` on `/metrics`

Both services already import `github.com/prometheus/client_golang` (for promhttp). Use `promauto` for metric registration.

Follow Prometheus naming conventions: snake_case, `_total` suffix for counters, pre-initialize all label combinations with `.Add(0)` in `init()`. Use package-level `promauto.NewCounterVec` vars (not interface-based metrics — these are simple fire-and-forget counters, no mocking needed).
</context>

<requirements>
1. Create `task/controller/pkg/metrics/metrics.go` with package-level metric vars using `promauto.NewCounterVec`:
   - `agent_controller_scan_cycles_total` (counter, labels: result={changes,no_changes,error})
   - `agent_controller_tasks_published_total` (counter, labels: type={changed,deleted})
   - `agent_controller_results_written_total` (counter, labels: result={success,not_found,error})
   - `agent_controller_git_push_total` (counter, labels: result={success,retry_success,conflict_resolved,error})
   - `agent_controller_conflict_resolutions_total` (counter, labels: result={success,error})

2. Create `task/executor/pkg/metrics/metrics.go` with:
   - `agent_executor_task_events_total` (counter, labels: result={spawned,skipped_status,skipped_phase,skipped_assignee,skipped_unknown_assignee,skipped_active_job,error})
   - `agent_executor_jobs_spawned_total` (counter)

3. Instrument `task/controller/pkg/sync/sync_loop.go`:
   - In `processResult`: increment `scan_cycles_total` with "changes" or "no_changes"
   - In `processResult`: increment `tasks_published_total` for each changed/deleted task

4. Instrument `task/controller/pkg/result/result_writer.go`:
   - In `WriteResult`: increment `results_written_total` with "success", "not_found", or "error"

5. Instrument `task/controller/pkg/gitclient/git_client.go`:
   - In `pushWithRetry`: increment `git_push_total` based on outcome (success, retry_success, conflict_resolved, error)
   - In `resolveConflicts`: increment `conflict_resolutions_total` per file resolved

6. Instrument `task/executor/pkg/handler/task_event_handler.go`:
   - In `ConsumeMessage`: increment `task_events_total` with appropriate label for each skip reason and for successful spawn

7. Pre-initialize all counter label combinations in an `init()` function in each `metrics.go` using `.Add(0)` to ensure they appear in /metrics output before first increment

8. Add Ginkgo v2/Gomega tests for the new metrics packages:
   - `task/controller/pkg/metrics/metrics_test.go` — use external test package (`metrics_test`), add `metrics_suite_test.go` with `RegisterFailHandler`/`RunSpecs`, verify all metrics are registered and pre-initialized (collect from default registry, assert expected metric names present)
   - `task/executor/pkg/metrics/metrics_test.go` — same Ginkgo pattern
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Do NOT change any interfaces or function signatures — metrics are fire-and-forget increments
- Do NOT add metrics as constructor parameters — use package-level promauto vars (they self-register)
- Use `promauto` not manual `prometheus.MustRegister`
- All metric names must start with `agent_controller_` or `agent_executor_` prefix
- Follow Prometheus naming conventions: snake_case, _total suffix for counters
- Do NOT add histogram/summary for now — only counters (simpler, sufficient for v1)
</constraints>

<verification>
Run `cd task/controller && make precommit` — must pass.
Run `cd task/executor && make precommit` — must pass.
</verification>
