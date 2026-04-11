---
status: failed
container: agent-038-executor-stage-filter
dark-factory-version: v0.108.0-dirty
created: "2026-04-11T10:20:32Z"
queued: "2026-04-11T10:20:32Z"
started: "2026-04-11T10:20:34Z"
completed: "2026-04-11T10:29:53Z"
lastFailReason: 'validate completion report: completion report status: partial'
---

<summary>
- Executor filters task events by a new `stage` field from task frontmatter
- Each executor instance only spawns jobs for tasks matching its own branch (dev/prod)
- Tasks without an explicit `stage` field default to `prod`
- Dev and prod executors can safely share the same git repo without double-execution
- Filter follows the existing skip pattern used for status, phase, and assignee
- New metric label `skipped_stage` tracks how many events were filtered out
- New `Stage()` helper on `lib.TaskFrontmatter` consistent with `Status()`/`Phase()`/`Assignee()`
</summary>

<objective>
Add a stage filter to `agent-task-executor` so each executor (dev or prod) only
spawns K8s Jobs for tasks whose frontmatter `stage` matches its own branch.
This prevents dev and prod from both processing the same task when they share
the same git source.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/controller-design.md` — task frontmatter handling context.

Key files to read before making changes:
- `lib/agent_task-frontmatter.go` — `lib.TaskFrontmatter` is `map[string]interface{}` with typed helper methods (`Status`, `Phase`, `Assignee`). The new `Stage()` helper must follow the exact same pattern.
- `task/executor/pkg/handler/task_event_handler.go` — existing filter chain (status → phase → assignee → unknown_assignee → active_job). Stage filter will be inserted into this chain.
- `task/executor/pkg/handler/task_event_handler_test.go` — existing Ginkgo tests built with `lib.TaskFrontmatter{"status": "in_progress", ...}` map literals.
- `task/executor/pkg/factory/factory.go` — `CreateConsumer` already has `branch base.Branch` in scope; used to construct Kafka topic and image tags. Needs to also pass `branch` to the handler constructor.
- `task/executor/pkg/metrics/metrics.go` — metric labels are pre-initialized in `init()` so they appear at zero on startup.

Important type facts:
- `lib.Task.Frontmatter` has type `lib.TaskFrontmatter` (defined in `lib/agent_task-frontmatter.go`), NOT `domain.TaskFrontmatter` from vault-cli.
- `lib.TaskFrontmatter` is literally `map[string]interface{}` with a few methods. To read a string field: `v, _ := f["key"].(string)`.
- `base.Branch` from `github.com/bborbe/cqrs/base` — string type, executor currently uses values `dev` and `prod`. Factory.go already calls `string(branch)` for image tags, so `string(h.branch)` is the established pattern.
</context>

<requirements>

1. **Add `Stage()` helper method to `lib.TaskFrontmatter`**

   Edit `lib/agent_task-frontmatter.go`. Append a new method right after `Assignee()`, using the same map-lookup-with-type-assertion pattern as the existing helpers:

   ```go
   // Stage returns the execution stage from the "stage" key.
   // Returns "prod" if the key is absent or empty.
   func (f TaskFrontmatter) Stage() string {
       v, _ := f["stage"].(string)
       if v == "" {
           return "prod"
       }
       return v
   }
   ```

   Do NOT introduce a new type for stage — plain `string` is enough and matches how `base.Branch` is compared elsewhere via `string(branch)`.

2. **Add `branch` field and constructor argument to the handler**

   Edit `task/executor/pkg/handler/task_event_handler.go`:

   - Add the import `"github.com/bborbe/cqrs/base"` if not already present.
   - Change `NewTaskEventHandler` signature — insert `branch base.Branch` as a new argument between `jobSpawner` and `assigneeImages`:
     ```go
     func NewTaskEventHandler(
         jobSpawner spawner.JobSpawner,
         branch base.Branch,
         assigneeImages map[string]string,
     ) TaskEventHandler
     ```
   - Add a `branch base.Branch` field to the `taskEventHandler` struct and initialise it from the constructor.

3. **Insert the stage filter in `ConsumeMessage`**

   In `task/executor/pkg/handler/task_event_handler.go`, inside `ConsumeMessage`, add the stage check **right after the phase check and BEFORE the empty-assignee check**. This keeps cross-stage tasks out of the assignee / image / active-job checks entirely.

   Insert this block:

   ```go
   stage := task.Frontmatter.Stage()
   if stage != string(h.branch) {
       glog.V(3).Infof(
           "skip task %s with stage %s (executor branch %s)",
           task.TaskIdentifier, stage, h.branch,
       )
       metrics.TaskEventsTotal.WithLabelValues("skipped_stage").Inc()
       return nil
   }
   ```

   Rules:
   - Use the new `task.Frontmatter.Stage()` helper (which applies the `"prod"` default internally).
   - Comparison is a simple string equality after the default has been applied by `Stage()`.
   - Skip returns `nil` (not an error) — same as the other filters.
   - Do NOT change the order or behaviour of any other filter.

4. **Pre-initialize the new metric label**

   In `task/executor/pkg/metrics/metrics.go`, inside the existing `init()` function, add one line near the other `skipped_*` entries:
   ```go
   TaskEventsTotal.WithLabelValues("skipped_stage").Add(0)
   ```

5. **Wire `branch` through the factory**

   In `task/executor/pkg/factory/factory.go`, update the existing call to `handler.NewTaskEventHandler` inside `CreateConsumer` to pass the already-in-scope `branch` value:
   ```go
   taskEventHandler := handler.NewTaskEventHandler(jobSpawner, branch, assigneeImages)
   ```

6. **Update handler tests — existing cases**

   Edit `task/executor/pkg/handler/task_event_handler_test.go`:

   - In the `BeforeEach`, update the handler construction to pass a branch. Use `base.Branch("prod")` so the existing test tasks (which have no `stage` key → default `prod`) still flow through the spawner as before:
     ```go
     h = handler.NewTaskEventHandler(fakeSpawner, base.Branch("prod"), assigneeImages)
     ```
   - Add `"github.com/bborbe/cqrs/base"` to the test file's imports.
   - Do NOT modify any of the existing `It(...)` assertions — they must all keep passing unchanged.

7. **Update handler tests — add four new cases**

   Append four new `It` blocks inside the existing `Describe("ConsumeMessage", ...)` block, matching the style of the existing skip/spawn tests. Each test constructs its own local handler with the desired branch (do NOT reuse the BeforeEach handler when the branch needs to differ).

   a. **default stage → prod, executor=prod, spawns**
      - Local handler: `base.Branch("prod")`.
      - Frontmatter has no `stage` key (`status=in_progress`, `phase=in_progress`, `assignee=claude`).
      - `fakeSpawner.IsJobActiveReturns(false, nil)`.
      - Assertions: `ConsumeMessage` returns `nil` AND `fakeSpawner.SpawnJobCallCount() == 1`.

   b. **explicit stage=dev, executor=prod, skips**
      - Local handler: `base.Branch("prod")`.
      - Frontmatter `status=in_progress`, `phase=in_progress`, `assignee=claude`, `stage=dev`.
      - Assertions: `ConsumeMessage` returns `nil` AND `fakeSpawner.SpawnJobCallCount() == 0`.

   c. **explicit stage=dev, executor=dev, spawns**
      - Local handler: `base.Branch("dev")`.
      - Frontmatter `status=in_progress`, `phase=in_progress`, `assignee=claude`, `stage=dev`.
      - `fakeSpawner.IsJobActiveReturns(false, nil)`.
      - Assertions: `ConsumeMessage` returns `nil` AND `fakeSpawner.SpawnJobCallCount() == 1`.

   d. **default stage → prod, executor=dev, skips (dev-default check)**
      - Local handler: `base.Branch("dev")`.
      - Frontmatter has no `stage` key (`status=in_progress`, `phase=in_progress`, `assignee=claude`).
      - Assertions: `ConsumeMessage` returns `nil` AND `fakeSpawner.SpawnJobCallCount() == 0`.

   All four tests use fresh `mocks.FakeJobSpawner` instances (either reuse `fakeSpawner` from `BeforeEach` when the branch matches, or construct a local one when the branch differs). Tests must not leak state between each other.

8. **Leave existing behaviours unchanged**

   - No change to status / phase / assignee / unknown_assignee / active_job filters or their order relative to each other.
   - No change to Kafka topic construction, image map, or job spawner.
   - No change to `vault-cli` — do NOT add `Stage()` to `domain.TaskFrontmatter` or any other vault-cli type.
   - Do NOT rename the metric `agent_executor_task_events_total`.
   - The `TaskEventHandler` interface is unchanged (only the concrete struct and constructor change). Do NOT re-run `go generate` or modify `task/executor/mocks/task_event_handler.go` — existing mocks are still valid.

</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- No changes to vault-cli.
- Repo-relative paths only.
- Follow existing filter-chain style in `ConsumeMessage`: one `if` per filter, increment metric, return `nil`.
- Metric label naming convention: `skipped_<reason>`.
- Default value for absent `stage` is `"prod"` — do NOT treat it as "both", an error, or anything else.
- `Stage()` helper lives in `lib/agent_task-frontmatter.go` next to the existing helpers — not in a new file, not in vault-cli.
- `lib.TaskFrontmatter` is a plain `map[string]interface{}`; do NOT refactor it to a struct.
</constraints>

<verification>
Run `make precommit` in `task/executor/` — must pass.

Spot-check expected after success:
- `go test ./task/executor/...` includes 4 new `It` blocks under the `ConsumeMessage` describe.
- `grep -r 'Stage()' lib/` shows the new helper.
- `grep 'skipped_stage' task/executor/pkg/metrics/metrics.go` finds the pre-init line.
</verification>
