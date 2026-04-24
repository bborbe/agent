---
status: committing
spec: [016-partial-frontmatter-publishers]
summary: Migrated PublishSpawnNotification and PublishFailure from full-frontmatter rewrites to UpdateFrontmatterCommand (partial keys only), removed PublishRetryCountBump from interface and implementation along with the private publish() helper, regenerated the counterfeiter mock, updated tests with capturing producer and exact key-set assertions, updated docs and CHANGELOG.
container: agent-070-spec-016-executor-publisher-migration
dark-factory-version: v0.132.0
created: "2026-04-24T10:00:00Z"
queued: "2026-04-24T10:06:18Z"
started: "2026-04-24T10:06:19Z"
branch: dark-factory/partial-frontmatter-publishers
---

<summary>
- `PublishSpawnNotification` switches from a full-frontmatter rewrite to an `UpdateFrontmatterCommand` carrying exactly three keys: `current_job`, `job_started_at`, `spawn_notification` — no other frontmatter fields are touched
- `PublishFailure` switches from a full-frontmatter rewrite to an `UpdateFrontmatterCommand` carrying exactly three keys: `status`, `phase`, `current_job` — body content is no longer mutated by this publisher (body writes are the agent's responsibility via `TaskResultExecutor`)
- `PublishRetryCountBump` is deleted from the `ResultPublisher` interface and implementation; any future caller fails to compile
- The private `publish()` helper that built full-task rewrites is deleted alongside `PublishRetryCountBump` (it has no remaining callers)
- The counterfeiter mock at `task/executor/mocks/result_publisher.go` is regenerated to reflect the new interface
- Unit tests assert the exact command kind and key set for each publisher: spawn notification must NOT contain `trigger_count`, `status`, or `phase`; failure must NOT contain `trigger_count`
- `docs/task-flow-and-failure-semantics.md` is updated with a per-publisher table showing which command kind each method emits and which keys it carries
- `CHANGELOG.md` is updated under `## Unreleased`
- `cd task/executor && make precommit` passes
</summary>

<objective>
Migrate the two remaining full-frontmatter-rewrite publishers (`PublishSpawnNotification`, `PublishFailure`) in `task/executor/pkg/result_publisher.go` to use `UpdateFrontmatterCommand`, carrying only the exact keys each publisher is responsible for. Remove the deprecated `PublishRetryCountBump` method and its supporting private `publish()` helper. After this change, no executor publisher can silently clobber `trigger_count` or other frontmatter fields it does not own, which allows the atomic increment (spec 015) to accumulate monotonically toward the cap.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, counterfeiter mocks, external test packages
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf
- `go-cqrs.md` in `~/.claude/plugins/marketplaces/coding/docs/` — CommandOperation shape, UpdateFrontmatterCommand usage

**Key files to read in full before editing:**

- `task/executor/pkg/result_publisher.go` — full file; understand `ResultPublisher` interface, all four methods, the private `publish()` helper (lines ~118–136), and `publishRaw()` (lines ~138–164). The migration reuses `publishRaw()` for both new methods.
- `task/executor/pkg/result_publisher_test.go` — existing tests to update; both `PublishSpawnNotification` and `PublishFailure` tests need to assert key sets, not just success
- `task/executor/pkg/handler/task_event_handler.go` — verify `PublishSpawnNotification` is called here (line ~260); it is best-effort after `SpawnJob` — do NOT change the ordering or error handling in this file
- `task/executor/pkg/job_watcher.go` — `PublishFailure` is called here (~line 150); verify the call signature is still correct after the body parameter is removed from `PublishFailure`'s behavior (the method signature stays the same — `jobName` and `reason` parameters remain, but the method no longer appends body content)
- `task/executor/mocks/result_publisher.go` — the counterfeiter mock; regenerate via `make generate` rather than editing manually
- `lib/agent_task-commands.go` — `UpdateFrontmatterCommand` struct and `UpdateFrontmatterCommandOperation` constant

Run these before editing to confirm current state:
```bash
grep -n "func.*resultPublisher\|publish\|PublishRaw" task/executor/pkg/result_publisher.go
grep -n "PublishRetryCountBump\|PublishSpawnNotification\|PublishFailure" task/executor/pkg/result_publisher.go task/executor/pkg/handler/task_event_handler.go task/executor/pkg/job_watcher.go
grep -n "UpdateFrontmatterCommand\|UpdateFrontmatterCommandOperation" lib/agent_task-commands.go
```
</context>

<requirements>

1. **Read `task/executor/pkg/result_publisher.go` in full**

   Understand the four interface methods and two private helpers before making any changes. Specifically note:
   - `PublishSpawnNotification` (lines ~64–78): copies all frontmatter, sets three keys, calls `publish()`
   - `PublishFailure` (lines ~80–98): copies all frontmatter, sets three keys, appends body content, calls `publish()`
   - `PublishRetryCountBump` (lines ~100–107): copies all frontmatter, sets one key, calls `publish()`
   - `publish()` (lines ~118–136): constructs a full `lib.Task` object and calls `publishRaw()` with `base.UpdateOperation`
   - `publishRaw()` (lines ~138–164): the shared low-level send helper — this is what the new methods use directly

2. **Migrate `PublishSpawnNotification` to `UpdateFrontmatterCommand`**

   Replace the current implementation:
   ```go
   // BEFORE (full rewrite — clobbers trigger_count):
   fm := lib.TaskFrontmatter{}
   for k, v := range task.Frontmatter {
       fm[k] = v
   }
   fm["spawn_notification"] = true
   fm["current_job"] = jobName
   fm["job_started_at"] = p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
   return p.publish(ctx, task.TaskIdentifier, fm, task.Content)
   ```

   With:
   ```go
   // AFTER (partial update — only named keys):
   cmd := lib.UpdateFrontmatterCommand{
       TaskIdentifier: task.TaskIdentifier,
       Updates: lib.TaskFrontmatter{
           "spawn_notification": true,
           "current_job":        jobName,
           "job_started_at":     p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
       },
   }
   return p.publishRaw(ctx, lib.UpdateFrontmatterCommandOperation, cmd)
   ```

   The method signature `PublishSpawnNotification(ctx context.Context, task lib.Task, jobName string) error` is unchanged.

3. **Migrate `PublishFailure` to `UpdateFrontmatterCommand`**

   Replace the current implementation:
   ```go
   // BEFORE (full rewrite — copies all frontmatter, appends body):
   fm := lib.TaskFrontmatter{}
   for k, v := range task.Frontmatter {
       fm[k] = v
   }
   fm["status"] = "in_progress"
   fm["phase"] = "ai_review"
   fm["current_job"] = ""
   body := string(task.Content) + "\n\n## Job Failure\n\nJob `" + jobName + "` failed: " + reason + "\n"
   return p.publish(ctx, task.TaskIdentifier, fm, lib.TaskContent(body))
   ```

   With:
   ```go
   // AFTER (partial update — only named keys, body NOT mutated):
   cmd := lib.UpdateFrontmatterCommand{
       TaskIdentifier: task.TaskIdentifier,
       Updates: lib.TaskFrontmatter{
           "status":      "in_progress",
           "phase":       "ai_review",
           "current_job": "",
       },
   }
   return p.publishRaw(ctx, lib.UpdateFrontmatterCommandOperation, cmd)
   ```

   The method signature `PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error` is unchanged. The `jobName` and `reason` parameters remain on the signature for caller compatibility but are no longer used in the implementation. The body content (the `## Job Failure` section) is removed — per spec 016, body writes are the responsibility of the agent's own result publish through `TaskResultExecutor`.

   **IMPORTANT**: verify that `task/executor/pkg/job_watcher.go` still compiles after this change — the call site `w.publisher.PublishFailure(ctx, task, job.Name, reason)` must remain unchanged.

4. **Remove `PublishRetryCountBump` from the interface and implementation**

   Delete:
   - The `PublishRetryCountBump` method declaration from the `ResultPublisher` interface
   - The `PublishRetryCountBump` implementation on `*resultPublisher`
   - The `publish()` private helper (it has no remaining callers once `PublishRetryCountBump` is gone)

   Verify the `publish()` helper has no other callers before deleting:
   ```bash
   grep -n "p\.publish\b" task/executor/pkg/result_publisher.go
   ```
   Should show only `PublishRetryCountBump` and `PublishSpawnNotification`/`PublishFailure` (which you've already migrated away). After migration, `p.publish` must be unreferenced.

   After deletion, `publishRaw()` is the only private helper remaining.

5. **Regenerate the counterfeiter mock**

   The target exists (confirmed in `task/executor/Makefile`). Run:
   ```bash
   cd task/executor && make generate
   ```

   This regenerates `task/executor/mocks/result_publisher.go` from the updated interface. The regenerated mock will automatically omit `PublishRetryCountBump` (since it's removed from the interface) and keep `PublishSpawnNotification`, `PublishFailure`, `PublishIncrementTriggerCount`.

   After regenerating: run `cd task/executor && make test` to confirm the mock compiles and tests pass.

6. **Update unit tests in `task/executor/pkg/result_publisher_test.go`** — MANDATORY capturing producer

   The existing tests use `libkafka.NewSyncProducerNop()` which silently discards messages. Replace it with a capturing `sarama.SyncProducer` so tests can inspect the wire payload. This is NOT optional — the acceptance criteria require exact-key-set assertions, which cannot be made against a no-op producer.

   Add this capturing producer type (either in the test file itself or in a sibling `*_test.go` helper file — do NOT put it outside the `_test` package):

   ```go
   type capturingSyncProducer struct {
       messages []*sarama.ProducerMessage
   }

   func (c *capturingSyncProducer) SendMessage(ctx context.Context, msg *sarama.ProducerMessage) (int32, int64, error) {
       c.messages = append(c.messages, msg)
       return 0, 0, nil
   }

   func (c *capturingSyncProducer) SendMessages(ctx context.Context, msgs []*sarama.ProducerMessage) error {
       c.messages = append(c.messages, msgs...)
       return nil
   }

   func (c *capturingSyncProducer) Close() error { return nil }
   ```

   Match the exact interface of `libkafka.SyncProducer` — read `vendor/github.com/bborbe/kafka/` to confirm the method signatures before implementing. If the interface has more methods than shown above (e.g. `Produce`, transaction methods), stub them with no-op returns.

   In each test, wire this capturing producer in place of `libkafka.NewSyncProducerNop()`. After calling `PublishSpawnNotification` or `PublishFailure`, extract the single captured message, deserialize `msg.Value` as `base.Command`, and inspect the `Operation` field plus the embedded `Data` payload unmarshaled into `lib.UpdateFrontmatterCommand`.

   **Required assertions for `PublishSpawnNotification` test:**
   - `len(capturingProducer.messages) == 1`
   - Deserialized `command.Operation == lib.UpdateFrontmatterCommandOperation`
   - Deserialized `cmd.Updates` has exactly 3 keys: `spawn_notification`, `current_job`, `job_started_at`
   - `cmd.Updates["trigger_count"]`, `cmd.Updates["status"]`, `cmd.Updates["phase"]` are all ABSENT (use `_, ok := cmd.Updates[key]; Expect(ok).To(BeFalse())`)
   - `cmd.Updates["spawn_notification"] == true`
   - `cmd.Updates["current_job"] == jobName`
   - `cmd.Updates["job_started_at"]` matches the frozen timestamp from the fake `currentDateTimeGetter`

   **Required assertions for `PublishFailure` test:**
   - `len(capturingProducer.messages) == 1`
   - `command.Operation == lib.UpdateFrontmatterCommandOperation`
   - `cmd.Updates` has exactly 3 keys: `status`, `phase`, `current_job`
   - `cmd.Updates["trigger_count"]`, `cmd.Updates["spawn_notification"]` absent
   - `cmd.Updates["status"] == "in_progress"`
   - `cmd.Updates["phase"] == "ai_review"`
   - `cmd.Updates["current_job"] == ""`

   These assertions ARE the Definition of Done for this step. Do NOT fall back to TODOs or "mock producer received something" checks. If a test cannot be written to satisfy these assertions, the implementation is wrong — rework requirements 2 and 3 first.

   A compile-time check for `PublishRetryCountBump` being removed is redundant because requirement 2 already removes it from the interface; Go will fail to compile any surviving caller automatically.

7. **Verify `PublishRetryCountBump` callers are gone**

   ```bash
   grep -rn "PublishRetryCountBump" --include="*.go" . | grep -v vendor
   ```
   Must return zero matches after removing the interface method, implementation, and the mock entry.

   If any test file still references `PublishRetryCountBump` (e.g. `task/executor/pkg/handler/task_event_handler_test.go` line ~350 asserts `PublishRetryCountBumpCallCount()`), remove those assertions — the mock no longer has the method.

8. **Update `docs/task-flow-and-failure-semantics.md`**

   Read the file first. Add a new section after the "References" section (or update the "Full Flow" section) titled **"Executor Publisher Command Kinds"**:

   ```markdown
   ## Executor Publisher Command Kinds

   Each `ResultPublisher` method publishes a specific command kind on `agent-task-v1-request`.
   Only the listed frontmatter keys are written; all other keys — including `trigger_count` — are never touched by executor-originated publishes.

   | Publisher method | Command kind | Frontmatter keys written |
   |---|---|---|
   | `PublishIncrementTriggerCount` | `increment-frontmatter` | `trigger_count` (delta +1) |
   | `PublishSpawnNotification` | `update-frontmatter` | `current_job`, `job_started_at`, `spawn_notification` |
   | `PublishFailure` | `update-frontmatter` | `status`, `phase`, `current_job` |
   ```

   Also update the "Related specs" list to include:
   ```markdown
   - `specs/in-progress/016-partial-frontmatter-publishers.md` — migrate executor publishers to UpdateFrontmatterCommand; delete PublishRetryCountBump
   ```

   Also update the Terminology table row for "Retry counter" to note `PublishRetryCountBump` is removed (not just deprecated):
   ```markdown
   | **Retry counter** | `retry_count` frontmatter field. Removed as of spec 016 — `PublishRetryCountBump` deleted from executor; `retry_count` still readable in existing task files but is no longer written. |
   ```

9. **Update `CHANGELOG.md` at repo root**

   Check for existing `## Unreleased`:
   ```bash
   grep -n "^## Unreleased" CHANGELOG.md | head -3
   ```
   Append to the existing section or create if absent:
   ```markdown
   - fix: migrate executor PublishSpawnNotification and PublishFailure from full-frontmatter rewrite to UpdateFrontmatterCommand (partial keys only), eliminating clobber of trigger_count; delete PublishRetryCountBump from ResultPublisher interface and implementation (spec 016, builds on spec 015 atomic primitives)
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
    Fix any failures before running `make precommit`.

</requirements>

<constraints>
- The executor remains stateless relative to the vault. It publishes commands on Kafka and never writes git directly.
- `UpdateFrontmatterCommand.Updates` must contain ONLY the keys the publisher is responsible for. Do NOT copy `task.Frontmatter` into `Updates` — that would recreate the clobber bug.
- `PublishFailure` must NOT mutate the task body. The `## Job Failure` section is no longer appended by this publisher. Body writes remain the responsibility of the agent's own result publish through `TaskResultExecutor`.
- The `PublishFailure` method signature `(ctx, task, jobName, reason string) error` is preserved for call-site compatibility even though `jobName` and `reason` are no longer used in the implementation.
- The `PublishSpawnNotification` method signature `(ctx, task, jobName string) error` is unchanged.
- `publish()` private helper must be deleted (no remaining callers after migration).
- `publishRaw()` private helper must be kept (still used by `PublishSpawnNotification`, `PublishFailure`, and `PublishIncrementTriggerCount`).
- Ordering in `task_event_handler.go`: `PublishSpawnNotification` is called AFTER `SpawnJob` (best-effort, non-blocking on error). Do NOT change the ordering or error handling in the handler. This prompt only changes `result_publisher.go`.
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`.
- All existing tests must pass (update any that reference `PublishRetryCountBump`).
- Do NOT touch `task/controller/`, `lib/`, `prompt/`, or `agent/claude/` in this prompt.
- Do NOT commit — dark-factory handles git.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify `PublishRetryCountBump` is gone from interface and implementation:
```bash
grep -rn "PublishRetryCountBump" task/executor/ --include="*.go"
```
Must return zero matches.

Verify `publish()` helper is gone:
```bash
grep -n "func.*resultPublisher.*publish\b" task/executor/pkg/result_publisher.go
```
Must return zero matches (only `publishRaw` remains).

Verify spawn notification uses `UpdateFrontmatterCommandOperation`:
```bash
grep -n "UpdateFrontmatterCommandOperation\|UpdateFrontmatterCommand" task/executor/pkg/result_publisher.go
```
Must show matches in both `PublishSpawnNotification` and `PublishFailure`.

Verify exact key sets in implementation (no trigger_count in spawn notification or failure):
```bash
grep -n "trigger_count" task/executor/pkg/result_publisher.go
```
Must return zero matches (trigger_count only appears in `PublishIncrementTriggerCount` which uses `IncrementFrontmatterCommand`).

Verify docs updated:
```bash
grep -n "Executor Publisher Command Kinds\|update-frontmatter\|increment-frontmatter" docs/task-flow-and-failure-semantics.md
```
Must show the new table section.

Verify CHANGELOG updated:
```bash
grep -n "PublishSpawnNotification\|PublishFailure\|spec 016" CHANGELOG.md
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
