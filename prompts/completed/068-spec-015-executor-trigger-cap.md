---
status: completed
spec: [015-atomic-frontmatter-increment-and-trigger-cap]
summary: Replaced PublishRetryCountBump call in spawnIfNeeded with trigger_count cap check and PublishIncrementTriggerCount, added publishRaw helper to result_publisher, updated mock, pre-initialized skipped_trigger_cap metric, added 5 new test scenarios, updated docs and CHANGELOG
container: agent-068-spec-015-executor-trigger-cap
dark-factory-version: v0.132.0
created: "2026-04-24T07:42:14Z"
queued: "2026-04-24T08:05:26Z"
started: "2026-04-24T08:19:49Z"
completed: "2026-04-24T08:27:04Z"
branch: dark-factory/atomic-frontmatter-increment-and-trigger-cap
---

<summary>
- `ResultPublisher` interface gains `PublishIncrementTriggerCount(ctx, task)` which sends an `IncrementFrontmatterCommand{Field: "trigger_count", Delta: 1}` on the existing Kafka topic using the new `IncrementFrontmatterCommandOperation` kind
- `PublishRetryCountBump` is NO LONGER called in `spawnIfNeeded`; it remains on the interface for silent-deprecation compatibility but is not invoked (executor stops bumping `retry_count` as of this release)
- A cap check is inserted in `spawnIfNeeded` before any publish: if `task.Frontmatter.TriggerCount() >= task.Frontmatter.MaxTriggers()`, the spawn is skipped, `skipped_trigger_cap` metric is incremented, and no increment command is published
- `skipped_trigger_cap` label is added to `TaskEventsTotal` in executor metrics
- Ordering invariant enforced: `PublishIncrementTriggerCount` completes (Kafka ACK) before `SpawnJob` is called; if publish fails, `SpawnJob` is not called
- Unit tests cover: cap-reached skip (no publish, no spawn, metric incremented), publish-then-spawn happy path, publish failure blocks spawn
- `docs/task-flow-and-failure-semantics.md` updated: trigger_count / max_triggers / cap behaviour documented
- `CHANGELOG.md` updated under `## Unreleased`
- `cd task/executor && make precommit` passes
</summary>

<objective>
Replace the executor's `retry_count` bump with a `trigger_count` increment that uses the new atomic command kind, and add a pre-spawn cap check that prevents indefinite spawn loops. When `trigger_count >= max_triggers`, the executor skips the spawn entirely (no publish, no Job). When below the cap, it publishes the increment command and waits for Kafka ACK before calling `SpawnJob` — preserving the spec-011 ordering invariant. This prompt depends on prompts 1 and 2 (the lib types and controller handlers must exist).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, counterfeiter mocks, external test packages
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — counter labels, pre-initialisation
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf

**This prompt depends on prompts 1 and 2 being complete.** Verify:
```bash
grep -n "IncrementFrontmatterCommand\|TriggerCount\|MaxTriggers" lib/agent_task-commands.go lib/agent_task-frontmatter.go 2>/dev/null
```
All must be present. If absent, stop.

**Key files to read in full before editing:**

- `task/executor/pkg/result_publisher.go` — `ResultPublisher` interface (~line 20), `resultPublisher` struct, `PublishRetryCountBump` (~line 96), and the private `publish(ctx, taskID, fm, content)` helper (~line 105); understand exactly how `publish()` constructs the `cdb.CommandObject` and sends it
- `task/executor/pkg/handler/task_event_handler.go` — `spawnIfNeeded` function; find the exact line where `PublishRetryCountBump` is called and where `SpawnJob` is called relative to it; understand the full function flow
- `task/executor/pkg/metrics/metrics.go` — `TaskEventsTotal` counter vec; existing labels listed in comments or `WithLabelValues(...)` calls in the handler
- `task/executor/pkg/handler/task_event_handler_test.go` — existing test structure, counterfeiter mock setup for `ResultPublisher`, how publish errors are simulated

Run these before editing:
```bash
grep -n "PublishRetryCountBump\|PublishIncrementTriggerCount\|SpawnJob\|spawnIfNeeded" task/executor/pkg/handler/task_event_handler.go
grep -n "func.*ResultPublisher\|interface" task/executor/pkg/result_publisher.go | head -20
grep -n "WithLabelValues\|skipped_" task/executor/pkg/handler/task_event_handler.go | head -20
grep -n "TaskEventsTotal\|skipped_" task/executor/pkg/metrics/metrics.go
```
</context>

<requirements>

1. **Verify prompts 1 and 2 are applied**

   ```bash
   grep -n "IncrementFrontmatterCommand\|IncrementFrontmatterCommandOperation" lib/agent_task-commands.go
   grep -n "TriggerCount\|MaxTriggers" lib/agent_task-frontmatter.go
   grep -n "AtomicReadModifyWriteAndCommitPush" task/controller/pkg/gitclient/git_client.go
   ```
   All must return matches. If any is absent, stop.

2. **Add `PublishIncrementTriggerCount` to `ResultPublisher` interface and implementation**

   In `task/executor/pkg/result_publisher.go`:

   a. Add to the `ResultPublisher` interface:
   ```go
   PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error
   ```

   b. Add implementation on `*resultPublisher`. Read `PublishRetryCountBump` first — the new method follows a similar structure but sends a different payload and operation:

   ```go
   func (p *resultPublisher) PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error {
       cmd := lib.IncrementFrontmatterCommand{
           TaskIdentifier: task.TaskIdentifier,
           Field:          "trigger_count",
           Delta:          1,
       }
       // Marshal cmd into JSON bytes for the command payload
       data, err := json.Marshal(cmd)
       if err != nil {
           return errors.Wrapf(ctx, err, "marshal IncrementFrontmatterCommand")
       }
       // Build a cdb.CommandObject — read publish() to understand the exact shape
       // Use lib.IncrementFrontmatterCommandOperation as the operation string
       // Use lib.TaskV1SchemaID as the schema (same topic, new operation kind)
       // Follow the same cdb.CommandObject construction as in publish()
       ...
   }
   ```

   Read the existing `publish()` private method carefully — understand exactly how it builds and sends the `cdb.CommandObject`. The new method must follow the same shape but with:
   - Operation: `lib.IncrementFrontmatterCommandOperation`
   - Data payload: JSON-marshaled `lib.IncrementFrontmatterCommand` (NOT a full `lib.Task`)

   If `publish()` is reusable for arbitrary operation strings and data bytes, refactor it to accept those as parameters and call it from both `PublishRetryCountBump` and `PublishIncrementTriggerCount`. If not, add a private helper `publishRaw(ctx, operation, data)` to avoid duplication.

3. **Update the counterfeiter mock for `ResultPublisher`**

   After updating the interface, regenerate the mock:
   ```bash
   cd task/executor && make generate
   ```
   Or if the mock is checked-in, update it manually to add `PublishIncrementTriggerCount` — the existing mock file is `task/executor/mocks/result_publisher.go` (mock type name: `FakeResultPublisher`). Read it to understand the counterfeiter pattern, then add the new method.

   After generating: run `cd task/executor && make test` to confirm the mock compiles.

4. **Add `skipped_trigger_cap` to executor metrics**

   In `task/executor/pkg/metrics/metrics.go`, find `TaskEventsTotal`. Add `"skipped_trigger_cap"` to its label documentation (comment listing valid values). Then pre-initialize it in the `init()` function (if one exists) or at declaration time:

   ```go
   // Pre-initialize to ensure the label appears at zero even before any event:
   TaskEventsTotal.WithLabelValues("skipped_trigger_cap").Add(0)
   ```

   The existing file already uses the `.Add(0)` suffix (see `metrics.go:30-37` for the other `skipped_*` labels). Use that exact pattern — NOT bare `.WithLabelValues(...)`.

5. **Modify `spawnIfNeeded` in `task/executor/pkg/handler/task_event_handler.go`**

   Read `spawnIfNeeded` in full. Find:
   - The call to `h.resultPublisher.PublishRetryCountBump(ctx, task)` — **remove this call**
   - The call to `h.jobSpawner.SpawnJob(ctx, task, *config)` — this stays

   Replace the removed `PublishRetryCountBump` call with the following logic (insert before `SpawnJob`):

   ```go
   // Cap check — must run before publishing the increment (check-then-increment per spec 015 Design Decision 1).
   if task.Frontmatter.TriggerCount() >= task.Frontmatter.MaxTriggers() {
       glog.V(2).Infof("skip task %s: trigger_count %d >= max_triggers %d",
           task.TaskIdentifier,
           task.Frontmatter.TriggerCount(),
           task.Frontmatter.MaxTriggers(),
       )
       metrics.TaskEventsTotal.WithLabelValues("skipped_trigger_cap").Inc()
       return nil
   }

   // Publish the increment BEFORE spawning — if this fails, no Job is created.
   if err := h.resultPublisher.PublishIncrementTriggerCount(ctx, task); err != nil {
       metrics.TaskEventsTotal.WithLabelValues("error").Inc()
       return errors.Wrapf(ctx, err, "publish increment trigger_count for task %s", task.TaskIdentifier)
   }
   ```

   **Important**: the `metrics.TaskEventsTotal.WithLabelValues("error").Inc()` call on publish failure matches the existing error-metric pattern used elsewhere in `spawnIfNeeded` (e.g. lines near `"check active job for task"`, `"spawn job for task"`). Do NOT drop this `.Inc()` — every error-return path in `spawnIfNeeded` increments the `"error"` label before returning.

   The `SpawnJob` call immediately follows the increment publish (unchanged).

   **Over-count tolerance**: if `PublishIncrementTriggerCount` succeeds but the subsequent `SpawnJob` fails, `trigger_count` has been bumped by 1 while no Job ran. This is the spec's documented over-count path (spec 015 Constraints, "over-count bounded by 1 per spawn attempt — `max_triggers` absorbs it"). Do NOT attempt to decrement, rollback, or compensate the counter on `SpawnJob` failure. The existing `SpawnJob` error path already returns the wrapped error; leave it as-is.

   **Verify the ordering**: after your edit, confirm that in all code paths:
   - Cap check runs first
   - Publish runs second (only if below cap)
   - `SpawnJob` runs third (only if publish succeeded)

   Use `glog` for logging (same as the rest of the handler — NOT `slog`).

6. **Add unit tests to `task/executor/pkg/handler/task_event_handler_test.go`**

   Read the existing test file in full first. Understand the counterfeiter mock setup and how messages are constructed. Add the following test scenarios inside the appropriate `Describe`/`Context` block:

   **Scenario A — cap reached, no spawn, no publish:**
   ```
   task.Frontmatter has trigger_count=3, max_triggers=3
   → spawnIfNeeded returns nil
   → PublishIncrementTriggerCount was NOT called
   → SpawnJob was NOT called
   → TaskEventsTotal "skipped_trigger_cap" incremented by 1
   ```

   **Scenario B — below cap, happy path:**
   ```
   task.Frontmatter has trigger_count=1, max_triggers=3
   → PublishIncrementTriggerCount is called once
   → SpawnJob is called once
   → TaskEventsTotal "spawned" incremented
   → "skipped_trigger_cap" NOT incremented
   ```

   **Scenario C — publish fails, spawn blocked:**
   ```
   task.Frontmatter has trigger_count=0, max_triggers=3
   → PublishIncrementTriggerCount returns an error
   → SpawnJob is NOT called
   → spawnIfNeeded returns a non-nil error
   ```

   **Scenario D — cap=0 skips immediately:**
   ```
   task.Frontmatter has trigger_count=0, max_triggers=0
   → spawnIfNeeded returns nil
   → no publish, no spawn
   → "skipped_trigger_cap" incremented
   ```

   **Scenario E — publish OK, spawn fails, counter over-count documented:**
   ```
   task.Frontmatter has trigger_count=1, max_triggers=3
   fakeSpawner.SpawnJobReturns("", errors.New("k8s create failed"))
   → PublishIncrementTriggerCount called once (no error)
   → SpawnJob called once (returns error)
   → spawnIfNeeded returns a non-nil error (wrapped)
   → TaskEventsTotal "error" incremented
   → No rollback/decrement attempt on counter — not asserted directly but confirmed by absence of any extra publish calls (PublishIncrementTriggerCountCallCount stays at 1, no decrement publish)
   ```

   For each scenario, use the counterfeiter mock for `ResultPublisher`. Assert call counts via `<mock>.PublishIncrementTriggerCountCallCount()` etc. Follow the exact mock pattern already in the test file.

7. **Confirm `PublishRetryCountBump` is NOT removed from the interface**

   Per spec 015 Design Decision 3 (silent deprecation), `retry_count` is kept readable in frontmatter and `PublishRetryCountBump` stays on the interface during this release. Only the CALL SITE in `spawnIfNeeded` is removed. The method itself remains for one release cycle.

   Verify:
   ```bash
   grep -n "PublishRetryCountBump" task/executor/pkg/result_publisher.go
   ```
   Must still show the method (interface + implementation). It just has no callers now.

8. **Update `docs/task-flow-and-failure-semantics.md`**

   Read the file first. Add or update the relevant section to document:
   - `trigger_count`: incremented atomically by the controller when it receives `IncrementFrontmatterCommand`; counts spawn-trigger events independent of job outcome; distinct from `retry_count`
   - `max_triggers`: frontmatter field (default 3); when `trigger_count >= max_triggers`, executor skips further spawns; controller sets `phase: human_review` on the same increment that reaches the cap
   - `retry_count`: silently deprecated as of this release; still readable in task files but no longer bumped; will be removed in the next release
   - Failure mode: byte-identical agent output no longer causes indefinite spawn loops — counter always increments (atomic, never idempotent at controller level), and cap is enforced at executor level before publishing

9. **Update `CHANGELOG.md` at repo root**

   Check for existing `## Unreleased`:
   ```bash
   grep -n "^## Unreleased" CHANGELOG.md | head -3
   ```
   Append to the existing section or create if absent:
   ```markdown
   - feat: trigger_count / max_triggers frontmatter fields bound executor spawn loops; atomic IncrementFrontmatterCommand makes counter non-idempotent; retry_count silently deprecated (executor no longer bumps it)
   ```

10. **Run tests iteratively**

    After steps 2–5:
    ```bash
    cd task/executor && make test
    ```
    After step 6:
    ```bash
    cd task/executor && make test
    ```
    Fix any failures before continuing.

</requirements>

<constraints>
- Controller remains the single writer to vault. The executor publishes commands on Kafka and never writes git directly.
- Ordering invariant (spec 011 extended by spec 015): `PublishIncrementTriggerCount` MUST complete before `SpawnJob` is called. If publish fails, `SpawnJob` MUST NOT be called.
- Cap check MUST run before the publish (check-then-increment per Design Decision 1). Do NOT publish a useless increment when the cap is already reached.
- `PublishRetryCountBump` MUST remain on the `ResultPublisher` interface and implementation (silent deprecation — callers removed, method kept). Do NOT delete it.
- The `retry_count` field in task files is NOT rewritten by this prompt. Existing tasks with `retry_count` set continue to work; the `applyRetryCounter` logic in the controller's `result_writer.go` is NOT touched here (it was already updated to read `retry_count` from the task file rather than bumping it in spec 011).
- `skipped_trigger_cap` label must be pre-initialized in metrics (same as other `skipped_*` labels) so it appears in Prometheus output at zero from startup.
- Use `glog` for logging in the handler — NOT `slog`. Match the existing log format and verbosity levels in the handler file.
- Do NOT touch `task/controller/`, `lib/`, `prompt/`, or `agent/claude/` in this prompt.
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`.
- Do NOT commit — dark-factory handles git.
- All existing tests must pass.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>
Verify `PublishIncrementTriggerCount` is on the interface:
```bash
grep -n "PublishIncrementTriggerCount" task/executor/pkg/result_publisher.go
```
Must show interface method and implementation.

Verify `PublishRetryCountBump` still exists (NOT removed):
```bash
grep -n "PublishRetryCountBump" task/executor/pkg/result_publisher.go
```
Must still show it.

Verify the call site replacement in the handler:
```bash
grep -n "PublishRetryCountBump\|PublishIncrementTriggerCount\|TriggerCount\|MaxTriggers\|skipped_trigger_cap" task/executor/pkg/handler/task_event_handler.go
```
Must show: `PublishIncrementTriggerCount` called, `TriggerCount`/`MaxTriggers` consulted, `skipped_trigger_cap` label used. Must NOT show `PublishRetryCountBump` call.

Verify `skipped_trigger_cap` metric pre-initialized:
```bash
grep -n "skipped_trigger_cap" task/executor/pkg/metrics/metrics.go
```
Must show the label (comment and/or pre-init call).

Verify ordering (cap check before publish before spawn) in handler:
```bash
grep -n "TriggerCount\|PublishIncrementTriggerCount\|SpawnJob" task/executor/pkg/handler/task_event_handler.go
```
Line numbers must be in ascending order: TriggerCount check < PublishIncrementTriggerCount < SpawnJob.

Verify docs updated:
```bash
grep -n "trigger_count\|max_triggers" docs/task-flow-and-failure-semantics.md
```
Must show new content.

Verify CHANGELOG updated:
```bash
grep -n "trigger_count\|max_triggers" CHANGELOG.md
```
Must show the Unreleased entry.

Run all executor tests:
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
