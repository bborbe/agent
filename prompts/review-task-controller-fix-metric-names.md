---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Fixes inconsistent Prometheus metric name prefixes in pkg/metrics/metrics.go
- FrontmatterCommandsTotal uses `agent_task_controller_*` instead of `agent_controller_*`
- GitRestCallsTotal uses `controller_gitrest_*` instead of `agent_controller_git_rest_*`
</summary>

<objective>
Two metrics have inconsistent name prefixes that break the established naming convention. All other metrics in the file use `agent_controller_*` as prefix. `FrontmatterCommandsTotal` incorrectly uses `agent_task_controller_*` and `GitRestCallsTotal` uses `controller_gitrest_*` (missing `agent_` segment and using `gitrest` instead of `git_rest`). After this fix, all metrics follow the consistent `agent_controller_*` prefix pattern.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/metrics/metrics.go` — lines 55-75, all metric declarations
</context>

<requirements>

1. **In `pkg/metrics/metrics.go`**, fix the metric name strings:

   Change `FrontmatterCommandsTotal` from:
   ```go
   prometheus.NewCounterVec(prometheus.CounterOpts{
       Name: "agent_task_controller_frontmatter_commands_total",
       ...
   })
   ```
   To:
   ```go
   prometheus.NewCounterVec(prometheus.CounterOpts{
       Name: "agent_controller_frontmatter_commands_total",
       ...
   })
   ```

   Change `GitRestCallsTotal` from:
   ```go
   prometheus.NewCounterVec(prometheus.CounterOpts{
       Name: "controller_gitrest_calls_total",
       ...
   })
   ```
   To:
   ```go
   prometheus.NewCounterVec(prometheus.CounterOpts{
       Name: "agent_controller_git_rest_calls_total",
       ...
   })
   ```

   Note: Verify that `git_rest` (with underscore) is the correct convention used by other similar services in this repo before finalizing.

2. **Update the corresponding test** in `pkg/metrics/metrics_test.go` that asserts the metric names, to use the corrected names.

3. **Run tests:**
   ```bash
   cd task/controller && make test
   ```

4. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change `task/controller/pkg/metrics/metrics.go` and its test file
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit
</verification>
