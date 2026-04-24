---
status: committing
summary: Replaced ai_review with human_review in PublishFailure, added BodySection type to UpdateFrontmatterCommand, wired ReplaceOrAppendSection into the controller executor, and extended tests in both modules — all three make precommit runs exited 0.
container: agent-074-publish-failure-to-human-review
dark-factory-version: v0.132.0
created: "2026-04-24T11:50:00Z"
queued: "2026-04-24T11:44:56Z"
started: "2026-04-24T11:52:36Z"
---

<summary>
- `PublishFailure` now escalates K8s-level Job failures directly to `phase: human_review` instead of `phase: ai_review` — `ai_review` is reserved for the review-step role, not a failure bucket
- The failure reason (OOMKilled, ImagePullBackOff, exit-code message, etc.) is appended to the task body as a `## Failure` section with timestamp, job name, and reason
- Repeated failures do NOT append duplicate `## Failure` sections — the existing section is replaced in place (most recent failure wins; prior failure context is preserved in git history)
- `status` stays `in_progress`, `current_job` clears to empty — matches the existing PublishFailure contract
- No change to `PublishSpawnNotification` or `PublishIncrementTriggerCount` — only the failure path changes
- A new optional `Body` field on `UpdateFrontmatterCommand` carries the section content; controller's `UpdateFrontmatterExecutor` passes it to the existing `ReplaceOrAppendSection` helper in `lib/delivery/markdown.go`
- Phase-unaware agents still see `status: in_progress` on failure — they continue to work, but the pipeline now correctly surfaces to the human review queue instead of silently re-triggering
</summary>

<objective>
Replace the wrong `phase: ai_review` behavior in `resultPublisher.PublishFailure` with a correct `phase: human_review` + body-section append of the failure reason. After this change, K8s-level Job failures (detected by `job_watcher.go`) surface immediately to the human review queue with a machine-readable record of what went wrong, rather than silently re-triggering under a misused `ai_review` phase.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `github.com/bborbe/errors`, never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2, external test packages
- `go-time-injection.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `libtime.CurrentDateTimeGetter` injection; timestamps in the body section use the existing time injection

Design rationale (for the human reviewer, not the agent — the requirements below are self-contained):
- Outside-container file: Agent Phase Dispatch Guide §"Resolving the `PublishFailure` conflict". This prompt implements that section (minus the retry_count semantics handled separately by `trigger_count` / `max_triggers`).

**Key files to read in full before editing:**

- `task/executor/pkg/result_publisher.go` — contains `PublishFailure` at ~line 74-89. Current body forces `phase: ai_review`:
  ```go
  cmd := lib.UpdateFrontmatterCommand{
      TaskIdentifier: task.TaskIdentifier,
      Updates: lib.TaskFrontmatter{
          "status":      "in_progress",
          "phase":       "ai_review",       // ← wrong; to become human_review
          "current_job": "",
      },
  }
  ```
  Note: the `reason` parameter is already passed in but currently discarded (only used via log lines elsewhere).

- `lib/agent_task-commands.go` — defines `UpdateFrontmatterCommand`:
  ```go
  type UpdateFrontmatterCommand struct {
      TaskIdentifier TaskIdentifier
      Updates        TaskFrontmatter
  }
  ```
  This prompt extends it with an optional `Body` field (backward-compatible — unset means frontmatter-only update, matching current behavior).

- `task/controller/pkg/command/task_update_frontmatter_executor.go` — the controller-side handler for `UpdateFrontmatterCommand`. Must be extended to honor the new `Body` field by calling `ReplaceOrAppendSection`.

- `lib/delivery/markdown.go` — exports `ReplaceOrAppendSection(content, heading, newSection string) string`. This is the idempotent body-update primitive. Reuse, do not reimplement.

- `task/executor/pkg/job_watcher.go` — calls `PublishFailure` at ~line 150 with a `reason` string already formatted (e.g. `"job failed: OOMKilled"`). No changes here.

- `task/executor/pkg/result_publisher_test.go` — existing unit tests for the publisher. Extend (do not create parallel file).

- `task/controller/pkg/command/task_update_frontmatter_executor_test.go` — existing tests for the controller-side executor. Extend with cases covering the new `Body` field.

Grep before editing (all paths repo-relative, container-safe):
```bash
grep -n "PublishFailure\|ai_review\|human_review" task/executor/pkg/result_publisher.go
grep -n "UpdateFrontmatterCommand\b" lib/agent_task-commands.go
grep -n "ReplaceOrAppendSection\|UpdateFrontmatterExecutor\|buildUpdateModifyFn" task/controller/pkg/command/task_update_frontmatter_executor.go
grep -rn "ReplaceOrAppendSection" lib/delivery/
```
</context>

<requirements>

1. **Extend `lib.UpdateFrontmatterCommand` with an optional `Body` field**

   Edit `lib/agent_task-commands.go`:

   ```go
   // UpdateFrontmatterCommand is the payload for UpdateFrontmatterCommandOperation.
   // Merges Updates into the existing frontmatter (partial merge — absent keys preserved).
   // When Body is set, its section is appended to (or replaced in) the task body via
   // lib/delivery.ReplaceOrAppendSection. Unset Body means frontmatter-only update.
   type UpdateFrontmatterCommand struct {
       TaskIdentifier TaskIdentifier          `json:"task_identifier"`
       Updates        TaskFrontmatter         `json:"updates"`
       Body           *BodySection            `json:"body,omitempty"`
   }

   // BodySection describes an idempotent body-section write: the controller's
   // UpdateFrontmatterExecutor calls ReplaceOrAppendSection(content, Heading, Section).
   // Heading MUST include the markdown prefix (e.g. "## Failure"). Section MUST
   // include the heading as its first line and a trailing newline.
   type BodySection struct {
       Heading string `json:"heading"`
       Section string `json:"section"`
   }
   ```

   Use `*BodySection` (pointer) so the field is truly optional and JSON-omittable.

   Do NOT add a new `base.CommandOperation` kind. The wire format stays under the existing `UpdateFrontmatterCommandOperation` — only the payload schema grows, which is a backward-compatible change (nil `Body` = current behavior).

2. **Update `task/controller/pkg/command/task_update_frontmatter_executor.go`**

   The real seam is the private helper `buildUpdateModifyFn` at line 88, called from inside `NewUpdateFrontmatterExecutor` at line 70. Current shape:

   ```go
   func buildUpdateModifyFn(
       ctx context.Context,
       updates lib.TaskFrontmatter,
   ) func([]byte) ([]byte, error) {
       return func(current []byte) ([]byte, error) {
           frontmatterStr, err := result.ExtractFrontmatter(ctx, current)
           if err != nil {
               return nil, errors.Wrapf(ctx, err, "extract frontmatter")
           }
           body, err := result.ExtractBody(ctx, current)
           if err != nil {
               return nil, errors.Wrapf(ctx, err, "extract body")
           }
           fm, err := parseTaskFrontmatter(frontmatterStr)
           if err != nil {
               return nil, errors.Wrapf(ctx, err, "parse frontmatter")
           }
           for k, v := range updates {
               fm[k] = v
           }
           return marshalFileContent(ctx, fm, body)
       }
   }
   ```

   Extend it to accept and apply the optional body section:

   ```go
   func buildUpdateModifyFn(
       ctx context.Context,
       updates lib.TaskFrontmatter,
       bodySection *lib.BodySection,
   ) func([]byte) ([]byte, error) {
       return func(current []byte) ([]byte, error) {
           frontmatterStr, err := result.ExtractFrontmatter(ctx, current)
           if err != nil {
               return nil, errors.Wrapf(ctx, err, "extract frontmatter")
           }
           body, err := result.ExtractBody(ctx, current)
           if err != nil {
               return nil, errors.Wrapf(ctx, err, "extract body")
           }
           fm, err := parseTaskFrontmatter(frontmatterStr)
           if err != nil {
               return nil, errors.Wrapf(ctx, err, "parse frontmatter")
           }
           for k, v := range updates {
               fm[k] = v
           }
           if bodySection != nil {
               body = delivery.ReplaceOrAppendSection(body, bodySection.Heading, bodySection.Section)
           }
           return marshalFileContent(ctx, fm, body)
       }
   }
   ```

   Update the single caller at line 70 from:
   ```go
   buildUpdateModifyFn(ctx, cmd.Updates),
   ```
   to:
   ```go
   buildUpdateModifyFn(ctx, cmd.Updates, cmd.Body),
   ```

   **Also update the empty-Updates early return at line 47** — it currently short-circuits when `len(cmd.Updates) == 0`, which would silently drop a Body-only command. Replace:
   ```go
   if len(cmd.Updates) == 0 {
       return nil, nil, nil
   }
   ```
   with:
   ```go
   if len(cmd.Updates) == 0 && cmd.Body == nil {
       return nil, nil, nil
   }
   ```

   Add import: `delivery "github.com/bborbe/agent/lib/delivery"`.

   Variable `body` in `buildUpdateModifyFn` is a `string` (returned by `result.ExtractBody`) and `ReplaceOrAppendSection(content, heading, newSection string) string` matches — no type conversion needed.

3. **Rewrite `PublishFailure` in `task/executor/pkg/result_publisher.go`**

   Replace the function body:

   ```go
   func (p *resultPublisher) PublishFailure(
       ctx context.Context,
       task lib.Task,
       jobName string,
       reason string,
   ) error {
       now := p.currentDateTime.Now().UTC().Format(time.RFC3339)
       section := fmt.Sprintf(
           "## Failure\n\n- **Timestamp:** %s\n- **Job:** %s\n- **Reason:** %s\n",
           now,
           jobName,
           reason,
       )
       cmd := lib.UpdateFrontmatterCommand{
           TaskIdentifier: task.TaskIdentifier,
           Updates: lib.TaskFrontmatter{
               "status":      "in_progress",
               "phase":       "human_review",
               "current_job": "",
           },
           Body: &lib.BodySection{
               Heading: "## Failure",
               Section: section,
           },
       }
       return p.publishRaw(ctx, lib.UpdateFrontmatterCommandOperation, cmd)
   }
   ```

   - Add imports if not present: `"fmt"`, `"time"`.
   - The existing `p.currentDateTime` (injected `libtime.CurrentDateTimeGetter`) is already in the struct — reuse. Do NOT call `time.Now()` directly; tests use a fake clock.
   - Escape `reason` is unnecessary — it's already a plain string from `job_watcher.go`. If a future reason contains backticks or newlines, the section format still renders as valid markdown.

4. **Do NOT modify `PublishSpawnNotification` or `PublishIncrementTriggerCount`** — they stay frontmatter-only updates with `Body: nil`.

5. **Do NOT modify `job_watcher.go`** — the caller's contract is unchanged. `PublishFailure(ctx, task, jobName, reason)` still takes the same args.

6. **Extend `task/executor/pkg/result_publisher_test.go`**

   Read the existing tests for `PublishFailure`. Update the assertions:
   - The captured command's `Updates["phase"]` is now `"human_review"` (not `"ai_review"`).
   - The captured command's `Body` is not nil.
   - `cmd.Body.Heading` equals `"## Failure"`.
   - `cmd.Body.Section` contains the timestamp (use the fake time's known value), the job name, and the reason string verbatim.

   Add a new `It` case if no existing test covers this path. Name it descriptively: `It("publishes a failure command with phase human_review and a ## Failure body section", ...)`.

7. **Extend `task/controller/pkg/command/task_update_frontmatter_executor_test.go`**

   Add two test cases:

   **Test X — Body field applies a new section:**
   - Seed a task file on disk with frontmatter `phase: ai_review, status: in_progress` and body containing only a `## Result` section.
   - Dispatch an `UpdateFrontmatterCommand` with `Updates: {phase: human_review, status: in_progress, current_job: ""}` and `Body: {Heading: "## Failure", Section: "## Failure\n\n- **Timestamp:** 2026-04-24T12:00:00Z\n- **Job:** job-abc\n- **Reason:** OOMKilled\n"}`.
   - Assert: written file frontmatter has `phase: human_review`; body contains BOTH `## Result` (preserved) AND `## Failure` (appended) with OOMKilled reason.

   **Test Y — Body nil leaves body untouched:**
   - Seed a task file with body `## Result\n\nok\n`.
   - Dispatch `UpdateFrontmatterCommand` with `Updates` set, `Body: nil`.
   - Assert: frontmatter updated; body byte-for-byte unchanged.

8. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased`:

   ```markdown
   - fix(executor): `PublishFailure` now escalates K8s Job failures to `phase: human_review` (was: `ai_review`) and records the failure reason in a `## Failure` body section with timestamp and job name
   - feat(lib): `UpdateFrontmatterCommand` gains an optional `Body` field (`*BodySection`); controller's executor applies `ReplaceOrAppendSection` when set — backward-compatible, nil Body preserves current frontmatter-only behavior
   ```

9. **Verification commands**

   Must exit 0 (run from repo root):
   ```bash
   cd task/executor   && make precommit && cd ../..
   cd task/controller && make precommit && cd ../..
   cd lib             && make precommit && cd ..
   ```

   Spot checks:
   ```bash
   grep -n 'human_review' task/executor/pkg/result_publisher.go
   ```
   Must show at least one match (in the new PublishFailure body).

   ```bash
   grep -c 'ai_review' task/executor/pkg/result_publisher.go
   ```
   Must be 0 — no remaining `ai_review` strings in the publisher.

   ```bash
   grep -n '"## Failure"' task/executor/pkg/result_publisher.go
   ```
   Must show one match (the Heading constant).

   ```bash
   grep -n 'BodySection\b' lib/agent_task-commands.go
   ```
   Must show the new type definition.

   ```bash
   grep -n 'cmd.Body\|cmd\.Body' task/controller/pkg/command/task_update_frontmatter_executor.go
   ```
   Must show the new body-apply branch.

</requirements>

<constraints>
- Only edit these files:
  - `lib/agent_task-commands.go` (add `BodySection` + `UpdateFrontmatterCommand.Body` field)
  - `task/executor/pkg/result_publisher.go` (rewrite `PublishFailure`)
  - `task/executor/pkg/result_publisher_test.go` (extend tests)
  - `task/controller/pkg/command/task_update_frontmatter_executor.go` (honor `cmd.Body`)
  - `task/controller/pkg/command/task_update_frontmatter_executor_test.go` (extend tests)
  - `CHANGELOG.md`
- Do NOT modify `job_watcher.go`, `IncrementFrontmatterCommand`, `PublishSpawnNotification`, `PublishIncrementTriggerCount`, `resultWriter`, or any Kafka schema constant.
- Do NOT add a new `base.CommandOperation` — reuse `UpdateFrontmatterCommandOperation`; the schema grows backward-compatibly.
- Use `p.currentDateTime.Now()` in `PublishFailure` — never `time.Now()` directly. Tests rely on the fake clock.
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this prompt introduces none).
- `BodySection` pointer (`*BodySection`) is mandatory — value type would always be non-zero and break the "unset means no body change" semantics.
- The `## Failure` section MUST use heading `"## Failure"` exactly — tests and future introspection depend on this literal. If a future spec wants a different heading, that's a new change.
- Phase-unaware agents still see `status: in_progress` on failure. The change is only to `phase`. Existing agents continue to work.
- Ginkgo v2 only. External test packages follow the existing file conventions.
- All existing tests must pass after the change.
- Do NOT commit — dark-factory handles git.
- `make precommit` must exit 0 in `task/executor`, `task/controller`, and `lib`.
</constraints>

<verification>

Verify the frontmatter change:
```bash
grep -n 'phase.*human_review\|phase.*ai_review' \
  task/executor/pkg/result_publisher.go
```
Must show exactly one `human_review` match; zero `ai_review` matches.

Verify `BodySection` type exists:
```bash
grep -nA3 'type BodySection' \
  lib/agent_task-commands.go
```
Must show the type definition with `Heading` and `Section` fields.

Verify the `Body` field wires to the controller executor:
```bash
grep -n 'cmd.Body\|ReplaceOrAppendSection' \
  task/controller/pkg/command/task_update_frontmatter_executor.go
```
Must show both: the nil-check on `cmd.Body` and the call to `ReplaceOrAppendSection`.

Verify test coverage:
```bash
grep -n 'human_review\|## Failure\|OOMKilled' \
  task/executor/pkg/result_publisher_test.go \
  task/controller/pkg/command/task_update_frontmatter_executor_test.go
```
Must show matches in both test files — at least one `human_review` and one `## Failure`.

Run focused tests:
```bash
go test -v \
  ./task/executor/pkg/... \
  ./task/controller/pkg/command/... \
  ./lib/...
```
Must exit 0. Output must include PASS lines for the new/amended tests.

Run full precommit per module:
```bash
cd task/executor   && make precommit
cd task/controller && make precommit
cd lib              && make precommit
```
All three must exit 0.

Verify CHANGELOG updated:
```bash
grep -n 'human_review\|## Failure\|BodySection' CHANGELOG.md
```
Must show the two Unreleased entries.

Post-merge live verification (NOT part of this prompt's execution — documented for the human):
1. Deploy to dev.
2. Trigger a Job failure (e.g. push an image that fails ImagePullBackOff, post a task).
3. Watch the task file commit: frontmatter should show `phase: human_review`; body should contain a `## Failure` section with timestamp, job name, and the Kubernetes-reported reason.
4. Confirm the task now appears in the human_review queue, not the ai_review one.
</verification>
