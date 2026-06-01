---
status: completed
spec: [043-executor-zombie-job-detection]
summary: Rewrote PublishFailure to emit update+increment paired commands with TTL LRU dedupe; rewrote PublishTypeMismatchFailure to emit assignee/previous_assignee/current_job only; updated all affected tests
container: agent-zombie-detect-exec-194-spec-043-doctrine-publishers-and-dedupe
dark-factory-version: v0.173.0
created: "2026-06-01T20:30:00Z"
queued: "2026-06-01T20:11:58Z"
started: "2026-06-01T20:12:03Z"
completed: "2026-06-01T20:16:40Z"
---

<summary>
- Fix `PublishFailure` to follow the retry-aware doctrine: leave `phase`, `status`, and `assignee` untouched; clear `current_job`; bump `trigger_count` atomically alongside the body append.
- Fix `PublishTypeMismatchFailure` to escalate immediately via the doctrine-correct shape: leave `phase` and `status` untouched; clear `assignee`; set `previous_assignee`; clear `current_job`.
- Add a publish-layer dedupe so two classifications for the same job emit one Kafka event.
- Update the existing publisher unit tests so they assert the new doctrine shape rather than the old `phase: human_review` / `phase: ai_review` shape.
- After this prompt, transient zombies will participate in the existing `trigger_count` retry cap and type-mismatch failures will surface in the operator inbox via the `assignee == ""` filter directly.
</summary>

<objective>
Make `task/executor/pkg/result_publisher.go` emit the doctrine-correct frontmatter shapes for the two existing failure publishers and add LRU dedupe keyed by `current_job` so a second emission for the same job within a bounded TTL is a no-op.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Spec: `specs/in-progress/043-executor-zombie-job-detection.md` (sections: Desired Behavior 1, 2, 7; Acceptance Criteria 1, 2, 3, 4, 9).

Doctrine reference: `specs/completed/039-controller-stop-setting-human-review-on-failure.md` and `docs/task-flow-and-failure-semantics.md` (Status Taxonomy & Inbox Signal). `phase: human_review` is reserved for agent-emitted successful verdicts that need human confirmation — never for failure escalation.

Files to read before changing:
- `task/executor/pkg/result_publisher.go` — current implementation; note `PublishFailure` at line 84 currently writes `phase: human_review`, `PublishTypeMismatchFailure` at line 121 currently writes `phase: ai_review`. Both must be rewritten.
- `task/executor/pkg/result_publisher_test.go` — existing tests at lines 166 (`PublishFailure`) and 216 (`PublishTypeMismatchFailure`); these assertions will be flipped by this change.
- `lib/command/task/update-frontmatter-command.go` — `UpdateFrontmatterCommand{TaskIdentifier, Updates, Body}` and `BodySection{Heading, Section}` shapes.
- `lib/command/task/increment-frontmatter-command.go` — `IncrementFrontmatterCommand{TaskIdentifier, Field, Delta}` and `IncrementFrontmatterCommandOperation = "increment-frontmatter"`.
- `task/controller/pkg/result/result_writer.go` — `applyTriggerCap` at line 234 (this is the chokepoint the new shape allows to fire) and `clearAssignee` at line 268 (sets `previous_assignee` controller-side; the executor's type-mismatch path must set it directly because no cap-mediated controller-side clear fires for type mismatch).

Coding plugin docs to consult:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-time-injection.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md`
</context>

<requirements>
### 1. Rewrite `PublishFailure` for the retry-aware zombie doctrine

In `task/executor/pkg/result_publisher.go`, replace the body of `PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error` so it:

1. Builds the `## Failure` body section exactly as today (timestamp from `p.currentDateTime.Now().UTC().Format(time.RFC3339)`, job name, reason — preserve the existing format string).
2. Publishes EXACTLY two CQRS commands in this order, both via the existing `p.publishRaw(...)` helper:
   - First: an `UpdateFrontmatterCommand` whose `Updates` map contains ONLY `"current_job": ""` and whose `Body` is the `## Failure` section. NO `status`, NO `phase`, NO `assignee`, NO `previous_assignee` key in `Updates`.
   - Second: an `IncrementFrontmatterCommand{TaskIdentifier: task.TaskIdentifier, Field: "trigger_count", Delta: 1}`, published with operation `taskcmd.IncrementFrontmatterCommandOperation`. This is the same shape the existing `PublishIncrementTriggerCount` method already uses — model the call after it.
3. If the first `publishRaw` returns an error, return immediately wrapped via `errors.Wrapf(ctx, err, "publish zombie failure update for task %s", task.TaskIdentifier)` and do NOT publish the increment. Because the dedupe entry is recorded ONLY after both publishes succeed (see requirement 3), the caller's next-cycle retry is NOT suppressed and can attempt the publish again — matching the spec's Failure Modes row "Kafka publish of failure command fails: sweeper retries next cycle".
4. If the second `publishRaw` returns an error, return it wrapped via `errors.Wrapf(ctx, err, "publish zombie failure trigger_count increment for task %s", task.TaskIdentifier)`. The `## Failure` body section has already been written at this point; that is acceptable — because dedupe has NOT yet been recorded, the caller's next retry will re-attempt both publishes. The controller-side write path is idempotent (`applyTriggerCap` re-reads frontmatter on every result write), so a re-applied `## Failure` body append is tolerable. Record the dedupe entry ONLY after BOTH commands succeed; this preserves Kafka-failure recovery while still blocking concurrent in-process duplicates (the dedupe is recorded synchronously before the function returns success, so a racing caller that arrives after a successful publish sees the entry).

The two-command sequence is the atomicity contract called out in spec DB #1 ("a single atomic write — either `update-frontmatter` with a paired increment, or a composite command — agent decides at impl time"). Two sequential publishes is the chosen path because (a) `IncrementFrontmatterCommand` is the existing primitive for atomic counter bumps and the only safe way to increment under concurrent writes, and (b) the controller's `applyRetryCounter` re-reads frontmatter on every result write, so eventual consistency suffices.

### 2. Rewrite `PublishTypeMismatchFailure` for immediate escalation

In `task/executor/pkg/result_publisher.go`, replace the body of `PublishTypeMismatchFailure(ctx context.Context, task lib.Task, reason string) error` so it publishes ONE `UpdateFrontmatterCommand` whose `Updates` map contains EXACTLY 3 keys when prior assignee is non-empty (`assignee`, `previous_assignee`, `current_job`); EXACTLY 2 keys (`assignee`, `current_job`) when prior assignee was empty:

- `"assignee": ""`
- `"previous_assignee": <prior assignee value>` — read via `string(task.Frontmatter.Assignee())` (`Assignee()` returns the `TaskAssignee` string alias defined in `lib/agent_task-frontmatter.go`; cast directly to `string`). If the prior assignee is empty, do NOT emit this key (degenerate state, but defensive).
- `"current_job": ""`

`Body` is the existing `## Failure` section (preserve the existing format that includes the assignee bullet and reason). NO `status`, NO `phase` keys in `Updates`.

Wrap publish errors via `errors.Wrapf(ctx, err, "publish type mismatch failure for task %s", task.TaskIdentifier)`.

Type mismatch does NOT participate in dedupe (it is called once per Kafka task event in `task_event_handler.go:199`; the dedupe layer added in requirement 3 keys by `current_job` and is purpose-built for zombie classifications that can race between the informer and the sweeper).

### 3. Add publish-layer dedupe for zombie failures

Add an internal LRU keyed by `current_job` (the `jobName` parameter of `PublishFailure`) to the `resultPublisher` struct. The LRU prevents two concurrent classifications from publishing twice for the same job.

3a. **Storage.** Use `github.com/hashicorp/golang-lru/v2` if it is already a dependency; otherwise implement a minimal map + RWMutex + insertion-order list with manual eviction. Verify dependency: run `grep -r 'hashicorp/golang-lru' ~/Documents/workspaces/agent-zombie-detect/go.mod ~/Documents/workspaces/agent-zombie-detect/go.sum` — if absent, use the inline map + mutex variant; do NOT add new module dependencies in this prompt.

3b. **Capacity and TTL.** Capacity pinned at 1024 entries (constant). TTL pinned at `2 * zombieJobTimeoutSeconds` = `3600 * time.Second` (constant — the CRD-derived value is wired in prompt 4; this prompt uses the hardcoded default so the publisher does not yet need configuration plumbing). Define as package-level constants:

```go
const dedupeCapacity = 1024
const dedupeTTL = 3600 * time.Second
```

3c. **Behavior.** Add two unexported methods on `*resultPublisher` (see 3d for the split rationale): `checkDedupe(jobName string) bool` returns `true` if a non-expired entry exists for `jobName` (caller should no-op), `false` otherwise (caller should proceed). `recordDedupe(jobName string)` inserts/refreshes the entry with the current timestamp; called only AFTER both publishes succeed. Entries past TTL are treated as absent (re-publish allowed; controller idempotency via `applyTriggerCap` handles the result-write side).

3d. **Wiring.** At the top of `PublishFailure`, before any publish, perform a dedupe CHECK (read-only — does NOT record yet) via an unexported method `checkDedupe(jobName string) bool` that returns `true` if a non-expired entry exists. If `checkDedupe` returns `true`, emit `glog.V(2).Infof("event=zombie_dedupe job=%s task=%s", jobName, task.TaskIdentifier)` and `return nil`. Then perform both publishes. ONLY after BOTH publishes succeed, call `p.recordDedupe(jobName)` to insert the entry. This ordering preserves Kafka-failure recovery: if either publish fails, the dedupe entry is NOT set, so the caller's next-cycle retry can attempt publish again — matching the spec's Failure Modes row "Kafka publish of failure command fails: sweeper retries next cycle; dedupe LRU prevents double-publish once Kafka recovers." (The "prevents double-publish once Kafka recovers" clause refers to subsequent successful cycles: once one publish succeeds, dedupe is set and any racing caller is blocked.)

Split `recordAndCheckDedupe` into two methods: `checkDedupe(jobName string) bool` (returns true if a non-expired entry exists, no mutation) and `recordDedupe(jobName string)` (inserts the entry with current timestamp, evicts oldest if at capacity). Both must be safe for concurrent callers via the same RWMutex.

3e. **Logging.** Suppressed duplicate emits one log line: `glog.V(2).Infof("event=zombie_dedupe job=%s task=%s", jobName, task.TaskIdentifier)`. Use `V(2)` per the spec's logging gating constraint.

3f. **Time source.** Use `p.currentDateTime.Now().Time()` for TTL math — NEVER `time.Now()` directly. The existing `currentDateTime` field already exists on `resultPublisher`.

### 4. Update existing publisher unit tests

In `task/executor/pkg/result_publisher_test.go`:

4a. Replace the `Describe("PublishFailure", ...)` block (around line 166) with assertions that match the new shape. The test must:
- Send `PublishFailure(ctx, task, "claude-20260418120000", "pod OOM killed")` once.
- Assert `len(producer.messages) == 2` (the update + the increment).
- Decode `producer.messages[0]` as `UpdateFrontmatterCommand` via the existing `decodeUpdateFrontmatterCommand` helper. Assert:
  - `cmd.Updates` has EXACTLY ONE key, `"current_job"`, equal to `""`.
  - `cmd.Updates` does NOT contain `"status"`, `"phase"`, `"assignee"`, `"previous_assignee"`, `"trigger_count"`.
  - `cmd.Body` is non-nil, `cmd.Body.Heading == "## Failure"`, and `cmd.Body.Section` contains the timestamp, job name, and reason.
- Decode `producer.messages[1]` as `IncrementFrontmatterCommand` via the existing `decodeIncrementFrontmatterCommand` helper at `task/executor/pkg/result_publisher_test.go:94` — do NOT add a duplicate. Assert `Field == "trigger_count"`, `Delta == 1`, `TaskIdentifier == "test-task-2"`.

4b. Replace the `Describe("PublishTypeMismatchFailure", ...)` block (around line 216) with assertions that match the new shape:
- After `PublishTypeMismatchFailure(ctx, task, "task_type ...")`, assert `len(producer.messages) == 1`.
- Decode the single message as `UpdateFrontmatterCommand`. Assert `cmd.Updates` has EXACTLY THREE keys: `"assignee" == ""`, `"previous_assignee" == "agent-pr-reviewer"`, `"current_job" == ""`.
- Assert `cmd.Updates` does NOT contain `"status"`, `"phase"`, `"trigger_count"`.
- Assert `cmd.Body.Heading == "## Failure"` and `cmd.Body.Section` contains the reason verbatim and the prior assignee value.

4c. Add a new `Describe("PublishFailure dedupe", ...)` block that:
- Calls `PublishFailure` twice in succession with the same `jobName` (e.g. `"claude-20260418120000"`).
- Asserts `len(producer.messages) == 2` after the FIRST call (one update + one increment).
- Asserts `len(producer.messages) == 2` after the SECOND call (still 2 — second call was deduped, NO new messages).

4d. Add a new test that confirms the doctrine-correct trigger_count math across multiple zombie calls. Given a task with `max_triggers: 3` and `trigger_count: 0`, call `PublishFailure` once, then assert:
- Exactly 2 messages sent (1 update + 1 increment).
- The increment's `Delta == 1`.
- No `assignee` key written.

Note: this test does NOT need to simulate the controller's `applyRetryCounter` re-read; it asserts only the executor's emission shape. The controller-side cap behavior is covered by existing controller tests.

### 5. Constraints on the rewrite

- Do NOT change the `ResultPublisher` interface method signatures — only their behavior.
- Do NOT remove or rename `PublishSpawnNotification`, `PublishIncrementTriggerCount`, or `PublishRaw` — they remain unchanged.
- Update the GoDoc on `PublishFailure` and `PublishTypeMismatchFailure` to describe the new shapes. The current GoDoc (`PublishFailure publishes a partial frontmatter update setting status, phase, and current_job`) is now wrong; replace it with text matching the new behavior. For example:
  ```go
  // PublishFailure publishes a zombie failure: clears current_job and atomically
  // bumps trigger_count by 1 via a paired IncrementFrontmatterCommand. Leaves
  // phase, status, and assignee untouched so the existing trigger_count retry
  // cap (applyTriggerCap in task/controller/pkg/result/result_writer.go) handles
  // eventual operator-inbox escalation. Idempotent per current_job via a TTL'd
  // LRU; concurrent classifications for the same job emit one event.
  ```
- All wraps use `github.com/bborbe/errors.Wrapf(ctx, err, ...)` — NEVER `fmt.Errorf` and NEVER bare `return err`.
- Time math uses `p.currentDateTime.Now()` — NEVER `time.Now()`.
- glog non-error lines use `V(2)` per the spec's logging gating constraint.

### 6. Verify

```
cd task/executor && make precommit
```

Must exit 0. The build will fail until the existing tests are updated (requirement 4) — that is expected and is part of this prompt.
</requirements>

<constraints>
- `github.com/bborbe/errors.Wrapf(ctx, err, ...)` for wrapping; no `fmt.Errorf`; no bare `return err`.
- `libtime.CurrentDateTimeGetter` for all time math; never `time.Now()` directly.
- Ginkgo/Gomega + counterfeiter mocks for tests.
- glog non-error logs gated with `V(n)`.
- Do NOT add new go.mod dependencies; if `hashicorp/golang-lru/v2` is not already present, implement the small LRU inline.
- Do NOT commit — dark-factory handles git.
- Only touch files under `task/executor/pkg/`.
- Verification command is `cd task/executor && make precommit` — never `make precommit` at repo root.
</constraints>

<verification>
```
cd task/executor && make precommit
```

Must exit 0. In particular:
- `PublishFailure` produces 2 Kafka messages (update + increment) with the new shape, validated by the rewritten test.
- `PublishTypeMismatchFailure` produces 1 Kafka message with the new shape, validated by the rewritten test.
- A second call to `PublishFailure` with the same job name produces 0 additional Kafka messages and one `event=zombie_dedupe` log line.
</verification>
