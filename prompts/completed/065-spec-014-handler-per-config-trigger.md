---
status: completed
spec: [014-configurable-task-triggers]
summary: 'Replaced hardcoded allowedPhases with per-Config trigger consulting: moved Config resolution before status/phase checks in parseAndFilter, added effectiveTriggerPhases/effectiveTriggerStatuses helpers with default fallback, added 7 new Ginkgo test scenarios covering all trigger combinations, updated CHANGELOG.md.'
container: agent-065-spec-014-handler-per-config-trigger
dark-factory-version: v0.132.0
created: "2026-04-23T21:15:00Z"
queued: "2026-04-24T07:11:07Z"
started: "2026-04-24T07:27:58Z"
completed: "2026-04-24T07:34:41Z"
---

<summary>
- The hardcoded `allowedPhases` package-level variable is removed from `task_event_handler.go`
- Config is resolved earlier in the filter chain — between the completed-task taskstore cleanup and the status check — so the per-Config trigger can gate spawning
- When assignee is empty at Config resolution time the handler skips Config lookup and lets the existing empty-assignee filter produce `skipped_assignee` (behavior preserved)
- When Config is not found for a non-empty assignee the handler increments `skipped_unknown_assignee`, logs a warning, and returns nil (existing behavior preserved)
- The status check now consults `config.Trigger.Statuses` (or falls back to the default `[in_progress]` when nil/empty)
- The phase check now consults `config.Trigger.Phases` (or falls back to the default `[planning, in_progress, ai_review]` when nil/empty)
- Existing metrics labels `skipped_status` and `skipped_phase` are unchanged — no new labels are added
- Seven handler-level Ginkgo test scenarios cover default behavior, per-Config phases, per-Config statuses, combined triggers, metric increments, and empty-list fallback
- `cd task/executor && make precommit` passes
</summary>

<objective>
Update the task-event handler to consult the per-agent Config's `Trigger` field when deciding whether to spawn a Job, instead of using the hardcoded `allowedPhases` list. Config is resolved before the status and phase checks so both filters can use the per-Config trigger. When `Trigger` or its sub-lists are nil or empty, the existing defaults apply unchanged. This prompt depends on prompt 1 (`AgentConfiguration.Trigger` must exist before editing the handler).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting (all under the coding plugin docs, available in-container at `/home/node/.claude/plugins/marketplaces/coding/docs/`):
- `go-patterns.md` — error wrapping, interface composition
- `go-testing-guide.md` — Ginkgo/Gomega, counterfeiter mocks, external test packages
- `go-error-wrapping-guide.md` — bborbe/errors, never fmt.Errorf

**This prompt depends on prompt 1 being complete.** Verify before starting:
```bash
grep -n "Trigger" task/executor/pkg/agent_configuration.go
```
If the field is absent, stop — prompt 1 has not been applied.

**Key files to read in full before editing:**

- `task/executor/pkg/handler/task_event_handler.go` — the complete handler; understand the filter chain in `parseAndFilter` (line 74), where `allowedPhases` is used (line 25, consulted at line 106), where Config is currently resolved (inside `spawnIfNeeded` via the `resolveConfig` helper, called at line 133; `resolveConfig` calls `h.resolver.Resolve(ctx, string(task.Frontmatter.Assignee()))` at line 213), and where `skipped_unknown_assignee` is emitted (line 221)
- `task/executor/pkg/handler/task_event_handler_test.go` — existing Ginkgo tests; understand the setup, counterfeiter mocks, and `AgentConfiguration` construction used in tests
- `task/executor/pkg/config_resolver.go` — `ConfigResolver` interface (method is `Resolve(ctx, assignee string) (pkg.AgentConfiguration, error)`) and `ErrConfigNotFound` sentinel
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct with the new `Trigger *agentv1.Trigger` field (from prompt 1)
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` — `Trigger` struct definition (from prompt 1)

**Note:** The resolver method is `Resolve`, not `FindByAssignee`. `FindByAssignee` is a method on the `AgentConfigurations` slice type, not on the `ConfigResolver` interface.

**Note on Status/Phase types:** `task.Frontmatter.Status()` returns `domain.TaskStatus` (string-typed enum); `task.Frontmatter.Phase()` returns `*domain.TaskPhase` (pointer). Use these typed values directly with `domain.TaskStatuses.Contains(status)` / `domain.TaskPhases.Contains(*phase)` — no manual string conversion required.

Run these to map the current handler structure:
```bash
grep -n "allowedPhases\|skipped_phase\|skipped_status\|skipped_unknown\|skipped_assignee\|ConfigResolver\|resolver\.Resolve\|ErrConfigNotFound\|resolveConfig" task/executor/pkg/handler/task_event_handler.go
```
```bash
grep -n "AgentConfiguration{" task/executor/pkg/handler/task_event_handler_test.go | head -10
```
</context>

<requirements>

1. **Verify prompt 1 is applied**

   ```bash
   grep -n "Trigger" task/executor/pkg/agent_configuration.go
   grep -n "Trigger" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
   ```
   Both must show the field. If either is absent, stop.

2. **Read `task/executor/pkg/handler/task_event_handler.go` in full**

   Understand:
   - Where `allowedPhases` is declared (line 25, hardcoded package-level `var`)
   - The `parseAndFilter` function (line 74) and its exact filter order
   - Config is currently resolved in `spawnIfNeeded` via the `resolveConfig` helper (called at line 133); `resolveConfig` itself calls `h.resolver.Resolve(ctx, string(task.Frontmatter.Assignee()))` at line 213
   - `skipped_unknown_assignee` is emitted at line 221 (inside `resolveConfig`)
   - `skipped_status` is emitted at line 101; `skipped_phase` at line 108
   - The handler accesses the task's assignee via `task.Frontmatter.Assignee()`

3. **Remove the `allowedPhases` package-level variable**

   Delete the `var allowedPhases = domain.TaskPhases{...}` declaration entirely. It will be replaced by default constants defined below.

4. **Add default trigger constants**

   In place of the removed `allowedPhases`, add two package-level variables (or unexported consts) that express the defaults:

   ```go
   var defaultTriggerPhases = domain.TaskPhases{
       domain.TaskPhasePlanning,
       domain.TaskPhaseInProgress,
       domain.TaskPhaseAIReview,
   }

   var defaultTriggerStatuses = domain.TaskStatuses{
       domain.TaskStatusInProgress,
   }
   ```

   These are the contract defaults referenced in the spec: do not rename, reorder, or drop values.

5. **Move Config resolution to before the status and phase checks**

   Currently the Config lookup happens in `spawnIfNeeded` via the `resolveConfig` helper (which calls `h.resolver.Resolve`), after `parseAndFilter` has already run. Restructure so that Config resolution occurs **inside `parseAndFilter`, between the completed-task taskstore cleanup step and the status check**.

   The new filter order must be:
   1. Empty message check (unchanged)
   2. JSON unmarshal (unchanged)
   3. Empty `TaskIdentifier` check (unchanged)
   4. Completed-task taskstore cleanup (unchanged)
   5. **[NEW] Config resolution** — read the assignee from the task, look up the Config; handle two sub-cases:
      - If assignee is empty: **skip the Config lookup**, allow the existing "empty assignee → `skipped_assignee`" filter at step 9 to handle it (do NOT return early here, do NOT produce `skipped_unknown_assignee`)
      - If assignee is non-empty and Config not found (`ErrConfigNotFound` or equivalent): increment `skipped_unknown_assignee`, log a warning (structured, same format as today), return nil (no error)
      - If assignee is non-empty and Config found: store the resolved `AgentConfiguration` for use in steps 6 and 7
   6. **[MODIFIED] Status check** — use the resolved Config's trigger statuses (or `defaultTriggerStatuses` if Config is nil, or if `Trigger` is nil, or if `Trigger.Statuses` is empty); increment `skipped_status` if the event status is not in the effective list
   7. **[MODIFIED] Phase check** — use the resolved Config's trigger phases (or `defaultTriggerPhases` under same conditions); increment `skipped_phase` if the event phase is not in the effective list
   8. Stage match (unchanged)
   9. Assignee empty check → `skipped_assignee` (unchanged, now only reached when Config resolution was skipped at step 5)

   **Important:** When Config is nil at steps 6/7 (only possible if step 5 skipped the lookup due to empty assignee), apply the defaults. This keeps the existing behavior for the empty-assignee path.

   **Helper function for effective phases/statuses:**
   Consider extracting a small helper to pick the effective list:
   ```go
   func effectiveTriggerPhases(cfg *pkg.AgentConfiguration) domain.TaskPhases {
       if cfg == nil || cfg.Trigger == nil || len(cfg.Trigger.Phases) == 0 {
           return defaultTriggerPhases
       }
       return cfg.Trigger.Phases
   }

   func effectiveTriggerStatuses(cfg *pkg.AgentConfiguration) domain.TaskStatuses {
       if cfg == nil || cfg.Trigger == nil || len(cfg.Trigger.Statuses) == 0 {
           return defaultTriggerStatuses
       }
       return cfg.Trigger.Statuses
   }
   ```

   Use `domain.TaskPhases.Contains` and `domain.TaskStatuses.Contains` (which already exist in vault-cli) for the membership check — do not reimplement.

6. **Ensure the skip-log lines match existing handler style**

   The handler uses `glog` (`github.com/golang/glog`) with printf formatting — NOT `slog`. When `skipped_phase` or `skipped_status` is incremented, match the existing lines exactly in level and format. Example:
   ```go
   glog.V(3).Infof("skip task %s with phase %v (not in per-Config trigger)", task.TaskIdentifier, phase)
   glog.V(3).Infof("skip task %s with status %s (not in per-Config trigger)", task.TaskIdentifier, task.Frontmatter.Status())
   ```
   Do not introduce `slog` or any new logger.

7. **Remove the now-redundant Config lookup in `spawnIfNeeded` and thread the resolved Config through**

   Today `parseAndFilter` returns `(lib.Task, bool)` and `spawnIfNeeded` calls `h.resolveConfig(ctx, task)` at line 133 to look up the Config a second time. After step 5, the Config is already resolved inside `parseAndFilter`. Refactor as follows:

   a. Change `parseAndFilter` signature to:
      ```go
      func (h *taskEventHandler) parseAndFilter(ctx context.Context, msg *sarama.ConsumerMessage) (lib.Task, *pkg.AgentConfiguration, bool)
      ```
      (Context is passed in because Config resolution needs it.)

   b. The function returns `(task, config, false)` on success, `(_, _, true)` on skip. `config` may be `nil` only when the empty-assignee path reaches step 9 (see step 5 above); otherwise it is always a resolved pointer to the per-assignee Config.

   c. Change `spawnIfNeeded` signature to accept the pre-resolved config:
      ```go
      func (h *taskEventHandler) spawnIfNeeded(ctx context.Context, task lib.Task, config *pkg.AgentConfiguration) error
      ```
      Remove the `h.resolveConfig(ctx, task)` call from the top of `spawnIfNeeded` — config is now passed in.

   d. Update `ConsumeMessage` to wire the new return/argument shape:
      ```go
      task, config, skip := h.parseAndFilter(ctx, msg)
      if skip {
          return nil
      }
      return h.spawnIfNeeded(ctx, task, config)
      ```

   e. The `resolveConfig` helper (and its `ErrConfigNotFound` → `skipped_unknown_assignee` logic) moves INTO the step-5 Config-resolution filter step. Delete the old helper from `spawnIfNeeded`. The `skipped_unknown_assignee` metric label and warning log must continue to be emitted with identical formatting.

8. **Add seven handler-level Ginkgo test scenarios**

   Read `task/executor/pkg/handler/task_event_handler_test.go` in full first. Understand the counterfeiter mock setup for `ConfigResolver`, how `AgentConfiguration` is stubbed, and how Kafka messages are constructed. Match the exact test style.

   Add the following seven `It` blocks (inside the appropriate `Describe`/`Context` wrapping — follow the existing structure):

   **Scenario 1 — Default behavior preserved (Trigger nil):**
   ```
   Resolved Config has Trigger == nil → event with phase=in_progress and status=in_progress → Job spawned
   ```
   Stub `configResolver.Resolve` to return an `AgentConfiguration` with `Trigger: nil`.
   Send a task event with `phase=in_progress`, `status=in_progress`.
   Assert that the spawner was called (or `skipped_*` metrics were NOT incremented).

   **Scenario 2 — Per-Config phase list used:**
   ```
   Config has Trigger.Phases = [todo], Trigger.Statuses = nil → event with phase=todo and status=in_progress → Job spawned
   ```
   Stub `Trigger = &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhaseTodo}}`.
   Send event with `phase=todo`, `status=in_progress`.
   Assert spawn (skipped_phase NOT incremented).

   **Scenario 3 — Per-Config status list used:**
   ```
   Config has Trigger.Phases = nil, Trigger.Statuses = [completed] → event with phase=in_progress and status=completed → Job spawned
   ```
   Stub `Trigger = &agentv1.Trigger{Statuses: domain.TaskStatuses{domain.TaskStatusCompleted}}`.
   Send event with `phase=in_progress`, `status=completed`.
   Assert spawn (skipped_status NOT incremented).

   **Scenario 4a — Combined trigger, matching event spawns:**
   ```
   Config has Trigger.Phases = [done], Trigger.Statuses = [completed] → matching event → Job spawned
   ```
   Stub full trigger. Send matching event. Assert spawn.

   **Scenario 4b — Combined trigger, non-matching event does not spawn:**
   ```
   Config has Trigger.Phases = [done], Trigger.Statuses = [completed] → event with phase=planning, status=in_progress → not spawned
   ```
   Assert skipped_phase OR skipped_status incremented (whichever filter fires first per the handler's order).

   **Scenario 5 — Phase matches but status does not:**
   ```
   Event phase matches Config trigger phases, event status does not match → skipped_status incremented, no spawn
   ```
   Assert `skipped_status` metric counter was incremented. Assert spawner was NOT called.

   **Scenario 6 — Status matches but phase does not:**
   ```
   Event status matches Config trigger statuses, event phase does not match → skipped_phase incremented, no spawn
   ```
   Assert `skipped_phase` metric counter was incremented. Assert spawner was NOT called.

   **Scenario 7 — Empty-list Trigger behaves like nil Trigger:**
   ```
   Config has Trigger = &Trigger{Phases: []TaskPhase{}, Statuses: []TaskStatus{}} → event with phase=in_progress and status=in_progress → Job spawned (defaults apply)
   ```
   Stub `Trigger = &agentv1.Trigger{}` (both lists nil/empty).
   Send event with `phase=in_progress`, `status=in_progress`.
   Assert spawn (defaults applied; skipped_phase and skipped_status NOT incremented).

   For each scenario, use the counterfeiter mock pattern already established in the test file. Assert metric behavior using the same metrics registry approach already in the test. If the existing tests verify metric counts via a test Prometheus registry, follow that pattern exactly.

9. **Run tests iteratively**

   After each major change (step 5, step 7, step 8), run:
   ```bash
   cd task/executor && make test
   ```
   Fix failures before continuing.

10. **Update `CHANGELOG.md` at repo root**

    Check for existing `## Unreleased`:
    ```bash
    grep -n "^## Unreleased" CHANGELOG.md | head -3
    ```
    Append to the existing section (from prompt 1) or create if absent:
    ```markdown
    - feat: task-event handler consults per-Config trigger phases/statuses with default fallback; removes hardcoded allowedPhases list
    ```

</requirements>

<constraints>
- The handler's existing filter order (empty message → malformed JSON → empty identifier → completed-task taskstore cleanup → status → phase → stage → assignee) must be preserved. Config resolution is inserted between taskstore cleanup and the status check — it is a new step, not a reordering of existing steps.
- Existing metric labels `skipped_phase` and `skipped_status` are unchanged — do NOT add new labels for the per-Config trigger path
- `ErrConfigNotFound` must still produce the existing `skipped_unknown_assignee` metric + warning log and must not cause an error return
- The empty-assignee path must still produce `skipped_assignee` (not `skipped_unknown_assignee`) — skip the Config lookup when assignee is empty
- Default phases stay exactly `{planning, in_progress, ai_review}`. Default status stays exactly `{in_progress}`. Do not rename, reorder, or drop values.
- `domain.TaskPhases.Contains` and `domain.TaskStatuses.Contains` must be reused — do not reimplement membership testing
- `Trigger = &Trigger{Phases: []TaskPhase{}, Statuses: []TaskStatus{}}` is treated identically to `Trigger = nil` (defaults apply for both dimensions)
- The verdict/result schema (`{status, message, output}`) is unchanged
- The Kafka task-event envelope is unchanged
- Do NOT touch `task/controller/`, `prompt/`, `lib/`, or `agent/claude/`
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `cd task/executor && make precommit` must exit 0
</constraints>

<verification>
Verify `allowedPhases` was removed:
```bash
grep -n "allowedPhases" task/executor/pkg/handler/task_event_handler.go
```
Must return no matches.

Verify default trigger constants exist:
```bash
grep -n "defaultTriggerPhases\|defaultTriggerStatuses" task/executor/pkg/handler/task_event_handler.go
```
Must show both.

Verify the handler consults per-Config trigger:
```bash
grep -n "Trigger\|effectiveTrigger" task/executor/pkg/handler/task_event_handler.go
```
Must show the trigger being consulted (either via helper functions or inline).

Verify the seven test scenarios exist:
```bash
grep -n "Trigger == nil\|Phases.*todo\|Statuses.*completed\|empty-list Trigger\|skipped_status.*incremented\|skipped_phase.*incremented\|non-matching\|defaults apply" task/executor/pkg/handler/task_event_handler_test.go
```
Must show matches for the test descriptions (adjust grep for the exact `It("...")` descriptions used).

Run handler tests specifically:
```bash
cd task/executor && go test ./pkg/handler/...
```
Must exit 0.

Run all tests:
```bash
cd task/executor && make test
```
Must exit 0.

Run precommit:
```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
