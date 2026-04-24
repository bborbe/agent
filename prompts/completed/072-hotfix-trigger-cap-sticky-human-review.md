---
status: completed
summary: Added trigger_count >= max_triggers escalation enforcement to resultWriter.applyRetryCounter with dedup for both Retry and Trigger Cap escalation sections, plus 5 new unit tests covering the live dev clobber scenario
container: agent-072-hotfix-trigger-cap-sticky-human-review
dark-factory-version: v0.132.0
created: "2026-04-24T10:00:00Z"
queued: "2026-04-24T11:38:27Z"
started: "2026-04-24T11:38:37Z"
completed: "2026-04-24T11:43:48Z"
---

<summary>
- Makes `phase: human_review` sticky once `trigger_count >= max_triggers`, so a stale agent result write cannot silently revert escalation back to `ai_review`
- Closes a live-observed dead end in dev (task `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9`): file correctly escalated by increment handler, then clobbered by the agent's `TaskResultExecutor` publish of a stale `phase: ai_review` payload — invisible to the human review queue
- Adds server-side enforcement in `resultWriter.applyRetryCounter` mirroring the existing retry_count escalation logic, but for the spec-015 `trigger_count` / `max_triggers` pair
- Adds a dedicated escalation section (`## Trigger Cap Escalation`) with timestamp, counts, and assignee, appended exactly once
- Adds deduplication so repeated writes-at-cap don't append the section multiple times; applies the same dedup to the existing `## Retry Escalation` path
- Adds unit tests in the existing `result_writer_test.go` covering: below-cap no-op, at-cap clobber-protection (the live dev bug encoded as a test), dedup across multiple writes, and zero-trigger_count defensive guard
- No Kafka schema changes, no new operation kinds, no executor-side changes
</summary>

<objective>
Enforce the `trigger_count >= max_triggers` cap server-side in `resultWriter.applyRetryCounter` so that `phase: human_review` is sticky across every `WriteResult` call, even when the incoming `lib.Task` payload carries a stale `phase: ai_review`. Protects the controller's atomic escalation (set by `IncrementFrontmatterExecutor`) from being overwritten by the agent's subsequent result publish.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `github.com/bborbe/errors`, never `fmt.Errorf` for new error paths (this prompt likely introduces none)
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2, external test packages, `DescribeTable` / `Entry`
- `go-time-injection.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `libtime.CurrentDateTimeGetter` injection; tests must use a fixed-time fake

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — retry vs trigger semantics, escalation rules, `phase` field lifecycle
- `docs/controller-design.md` — `TaskResultExecutor`, `IncrementFrontmatterExecutor`, result-writer responsibilities

**Why this hotfix exists — live-observed clobber.**

Dev task `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9` manifested the bug today:

1. `IncrementFrontmatterExecutor` correctly set `phase: human_review` when `trigger_count` reached `max_triggers` (atomic, under lock).
2. The agent then published its failure result via `publish()` → full-frontmatter rewrite through `TaskResultExecutor` → `resultWriter.WriteResult` → `applyRetryCounter`. The incoming payload carried a stale `phase: ai_review`, which was merged into the file via `mergeFrontmatter` (incoming keys win).
3. Git history of the task file: commit `f1399eb` shows `-phase: human_review / +phase: ai_review` on the agent's result write, immediately after the increment commit that had escalated it.
4. Executor's cap filter still blocks further spawns (no infinite loop), but the file reads `phase: ai_review, status: in_progress` on disk — INVISIBLE to the human review queue. Operational dead end.

Root cause: `resultWriter.applyRetryCounter` has escalation logic ONLY for the deprecated `retry_count / max_retries` pair. It was never migrated to also enforce `trigger_count / max_triggers`. Spec 015 added the fields, spec 016 migrated the executor publishers, but this server-side enforcement never landed. Any writer passing through `WriteResult` → `applyRetryCounter` with a stale `phase` payload can silently revoke the controller's escalation.

**Key files to read in full before editing:**

- `task/controller/pkg/result/result_writer.go` — contains `applyRetryCounter` (currently ~line 150), `escalationSection` (~line 168), `mergeFrontmatter` (~line 180). This is the only production file that changes.
- `task/controller/pkg/result/result_writer_test.go` — existing Ginkgo test; extend this file (do NOT create a parallel file). Read it end-to-end to understand the builder pattern, fake git client, fake time, and how the existing retry_count escalation tests are structured.
- `task/controller/pkg/result/result_suite_test.go` — Ginkgo suite root; no changes expected.
- `lib/agent_task-frontmatter.go` — accessor signatures. Confirmed present:
  - `func (f TaskFrontmatter) Phase() *domain.TaskPhase` — note: returns `*domain.TaskPhase` (pointer), **nil when absent**. Use `p := merged.Phase(); p != nil && *p == domain.TaskPhaseHumanReview`.
  - `func (f TaskFrontmatter) RetryCount() int`
  - `func (f TaskFrontmatter) MaxRetries() int`
  - `func (f TaskFrontmatter) TriggerCount() int`
  - `func (f TaskFrontmatter) MaxTriggers() int`
  - `func (f TaskFrontmatter) Assignee() TaskAssignee`
  - `func (f TaskFrontmatter) Status() domain.TaskStatus`
- `domain/task_phase.go` (or wherever `TaskPhase` is defined under `github.com/bborbe/vault-cli/pkg/domain`) — grep for the `human_review` constant. Use the constant, never the string literal, in production code.

Grep before editing to confirm the landscape:

```bash
cd ~/Documents/workspaces/agent
grep -n "applyRetryCounter\|escalationSection\|TriggerCount\|MaxTriggers\|Retry Escalation\|human_review" task/controller/pkg/result/result_writer.go
grep -n "TriggerCount\|MaxTriggers\|trigger_count\|max_triggers" task/controller/pkg/result/result_writer_test.go
grep -rn "TaskPhaseHumanReview\|human_review" --include='*.go' | grep -v vendor | head -20
```
</context>

<requirements>

1. **Add trigger-cap escalation to `resultWriter.applyRetryCounter` in `task/controller/pkg/result/result_writer.go`**

   Current body (verify by reading the file before edit — line numbers may shift):

   ```go
   func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {
       if string(merged.Status()) == "completed" {
           return body
       }
       if merged.SpawnNotification() {
           delete(merged, "spawn_notification")
           return body
       }
       // retry_count is authoritative in the task file — the executor bumped it
       // at spawn time (spec 011). The writer only applies escalation.
       retryCount := merged.RetryCount()
       if retryCount >= merged.MaxRetries() {
           merged["phase"] = "human_review"
           body += r.escalationSection(retryCount, merged)
       }
       return body
   }
   ```

   Replace with:

   ```go
   func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {
       if string(merged.Status()) == "completed" {
           return body
       }
       if merged.SpawnNotification() {
           delete(merged, "spawn_notification")
           return body
       }
       // retry_count is authoritative in the task file — the executor bumped it
       // at spawn time (spec 011). The writer only applies escalation.
       retryCount := merged.RetryCount()
       if retryCount >= merged.MaxRetries() {
           merged["phase"] = "human_review"
           if !containsEscalationSection(body, "## Retry Escalation") {
               body += r.escalationSection(retryCount, merged)
           }
       }
       // trigger_count cap enforcement — derived invariant from spec 015.
       // Any write that leaves the file with trigger_count >= max_triggers
       // MUST land in phase: human_review. Protects against stale-payload
       // writers (agent result publish, legacy integrations) revoking the
       // increment handler's escalation. The `triggerCount > 0` guard prevents
       // degenerate escalation of brand-new tasks where trigger_count is absent
       // (parsed as 0) and max_triggers defaults to a small positive integer.
       triggerCount := merged.TriggerCount()
       if triggerCount > 0 && triggerCount >= merged.MaxTriggers() {
           merged["phase"] = "human_review"
           if !containsEscalationSection(body, "## Trigger Cap Escalation") {
               body += r.triggerEscalationSection(triggerCount, merged)
           }
       }
       return body
   }
   ```

   Notes:
   - Keep the assignment `merged["phase"] = "human_review"` as a raw string (mirrors existing retry path; `merged` is a `map[string]interface{}`, and yaml-marshal will write it correctly). If you prefer to use the `domain.TaskPhaseHumanReview` constant, verify the constant's string value is exactly `"human_review"` and cast to string when assigning into the map. Do NOT introduce a `domain.TaskPhase`-valued entry — the map is deserialized from YAML as `string` everywhere else, and mixing types would break `yaml.Marshal`.
   - The trigger-cap block runs AFTER the retry block. If both conditions are true, `phase` ends as `human_review` and both sections are appended (each deduplicated independently). This is acceptable — both escalations are informative — and matches the principle that escalations are additive.

2. **Add the helper `triggerEscalationSection` as a sibling of `escalationSection`**

   Immediately below `escalationSection`:

   ```go
   func (r *resultWriter) triggerEscalationSection(triggerCount int, merged lib.TaskFrontmatter) string {
       ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
       return fmt.Sprintf(
           "\n## Trigger Cap Escalation\n\n- **Timestamp:** %s\n- **Trigger count:** %d\n- **Max triggers:** %d\n- **Assignee:** %s\n- **Last agent output:** see `## Result` above\n",
           ts,
           triggerCount,
           merged.MaxTriggers(),
           string(merged.Assignee()),
       )
   }
   ```

   Uses the same `r.currentDateTime` time injection as the existing `escalationSection` — tests can control timestamps via the existing fake.

3. **Add the dedup helper `containsEscalationSection`**

   As a package-private function in the same file (placement: below `triggerEscalationSection`, above `mergeFrontmatter`):

   ```go
   // containsEscalationSection reports whether body already has the given
   // escalation header on its own line. Used to prevent duplicate escalation
   // sections when WriteResult runs multiple times on a task that is already
   // at cap (e.g. agent publishes another result while the task sits in
   // phase: human_review).
   func containsEscalationSection(body, header string) bool {
       return strings.Contains(body, "\n"+header+"\n")
   }
   ```

   `strings` is already imported. Do not add any new imports.

4. **Apply the same dedup guard to the existing retry_count escalation**

   This was already done inline in step 1's replacement snippet. Double-check the final file: the retry-cap branch must be wrapped in `if !containsEscalationSection(body, "## Retry Escalation")` around the `body += r.escalationSection(...)` line. The unconditional `merged["phase"] = "human_review"` MUST remain outside the dedup guard — we always want to enforce phase, we only dedup the section append.

5. **Verify `lib.TaskFrontmatter.Phase()` exists** (it does: returns `*domain.TaskPhase`). Do NOT add or modify this accessor. Do NOT call `string(merged.Phase())` — `Phase()` returns a pointer. If you need to check current phase, dereference: `if p := merged.Phase(); p != nil && *p == domain.TaskPhaseHumanReview { ... }`. For this hotfix the production code does NOT need to read the current phase — it unconditionally sets `phase: human_review` when at cap, which is idempotent.

6. **Tests — extend `task/controller/pkg/result/result_writer_test.go`**

   Read the existing test file end-to-end first. Identify:
   - The Ginkgo `Describe`/`Context`/`It` structure.
   - The fake git client and how it exposes written content for assertion.
   - The fake `CurrentDateTimeGetter` — used to make timestamps deterministic.
   - How existing retry_count escalation tests set up `merged` frontmatter (there should be at least one).

   Append a new `Context("trigger_count cap escalation", func() { ... })` block inside the existing top-level `Describe("resultWriter", ...)` (or whichever root describe exists). Do NOT delete or rename existing tests. Use the existing builder helpers; do not introduce parallel test infrastructure.

   All four tests below write an existing task file to the fake git working tree, call `WriteResult` with an incoming `lib.Task`, then read back the written file content and assert on frontmatter and body.

   **Test A — below cap, phase untouched**

   Setup:
   - Existing file on disk: frontmatter `phase: ai_review, trigger_count: 2, max_triggers: 3, status: in_progress`, body empty or with a prior `## Result` section.
   - Incoming `lib.Task`: `Frontmatter` containing `phase: ai_review` (stale but equal to current), no trigger_count override, with `Content` = some normal `## Result\nStatus: completed\n`.

   Assert after `WriteResult`:
   - Written file frontmatter has `phase: ai_review` (unchanged).
   - Written file body does NOT contain the string `## Trigger Cap Escalation`.

   **Test B — at cap with stale `phase: ai_review` incoming (CLOBBER PROTECTION — the live dev bug)**

   Setup:
   - Existing file on disk: frontmatter `phase: human_review, trigger_count: 3, max_triggers: 3, status: in_progress` (state just after `IncrementFrontmatterExecutor` escalated).
   - Incoming `lib.Task`: `Frontmatter` containing `phase: ai_review, trigger_count: 3, max_triggers: 3, status: in_progress` (simulates the agent's stale result-publish payload), `Content` = `## Result\nStatus: failed\nMessage: gh auth failed\n`.

   Assert after `WriteResult`:
   - Written file frontmatter has `phase: human_review` (sticky — the stale `ai_review` in the incoming payload did NOT clobber).
   - Written file body contains `## Trigger Cap Escalation` exactly once.
   - Written file body still contains the agent's `## Result` content (`Status: failed`, `gh auth failed`) — nothing is lost.

   **This test is THE test. It is the live-dev failure mode from task `ba1bad61`, encoded as a unit. Name the `It(...)` descriptively, e.g. `It("keeps phase: human_review sticky when incoming payload carries stale phase: ai_review at cap", func() { ... })`.**

   **Test C — dedup across multiple cap-reached writes**

   Setup:
   - Existing file on disk: frontmatter at cap (`trigger_count: 3, max_triggers: 3, phase: human_review, status: in_progress`), body already contains `## Trigger Cap Escalation\n\n- **Timestamp:** <earlier>\n- **Trigger count:** 3\n...` (simulate that a previous WriteResult already appended the section).
   - Incoming `lib.Task`: another at-cap result write.

   Assert after `WriteResult`:
   - Written file body contains the substring `## Trigger Cap Escalation` exactly once (use `strings.Count(bodyStr, "## Trigger Cap Escalation") == 1`).
   - Agent's `## Result` is updated/present.
   - `phase: human_review` still present.

   **Test D — zero trigger_count defensive guard**

   Setup:
   - Existing file on disk: frontmatter `phase: ai_review, status: in_progress, max_triggers: 3` (no `trigger_count` field → parsed as 0 by `TriggerCount()`).
   - Incoming `lib.Task`: normal update, `phase: ai_review`, `Content` = `## Result\nStatus: completed\n` but keep `status: in_progress` in the test (Test A's `completed` short-circuits; this one must NOT short-circuit, so keep `status: in_progress` and use a non-completion result content).

   Assert after `WriteResult`:
   - Written file frontmatter has `phase: ai_review` (unchanged — the `triggerCount > 0` guard prevents degenerate escalation).
   - Written file body does NOT contain `## Trigger Cap Escalation`.

   **Test E — retry_count escalation dedup (regression-proof the existing path)**

   Setup:
   - Existing file on disk: frontmatter at retry cap (`retry_count: 3, max_retries: 3, phase: human_review, status: in_progress`), body already contains one `## Retry Escalation` section.
   - Incoming `lib.Task`: another at-cap result write.

   Assert after `WriteResult`:
   - Written file body contains `## Retry Escalation` exactly once.
   - `phase: human_review` still present.

   If the existing test file already has a Test-E analogue, skip adding a new one and note this in your summary.

7. **Do NOT modify `lib/agent_task-frontmatter.go`, `agent-task-executor`, or `IncrementFrontmatterExecutor`**. The executor-side cap filter and the increment handler's escalation write are already correct. This hotfix is one-file surgery in the result writer + its test.

8. **Do NOT bump any Kafka schema version or introduce any new `base.CommandOperation` kind.** Wire format is unchanged.

9. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased` (create the section if absent; do not touch released sections):

   ```markdown
   - fix: enforce `trigger_count >= max_triggers` escalation server-side in `resultWriter.applyRetryCounter` so `phase: human_review` stays sticky across stale-payload result writes; adds `## Trigger Cap Escalation` section with dedup; adds dedup to the existing `## Retry Escalation` path; unit-tested for the live dev clobber scenario
   ```

10. **Verification commands**

    Must exit 0:

    ```bash
    cd ~/Documents/workspaces/agent/task/controller && make precommit
    ```

    Spot checks:

    ```bash
    grep -n "TriggerCount\|triggerEscalationSection\|containsEscalationSection\|Trigger Cap Escalation" \
      ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer.go
    grep -n "Trigger Cap Escalation\|trigger_count.*human_review\|sticky\|clobber" \
      ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer_test.go
    ```

    The first grep must show matches in both the `applyRetryCounter` block and the new helpers. The second must show matches in at least Test B and Test C.

</requirements>

<constraints>
- Only edit `task/controller/pkg/result/result_writer.go` and `task/controller/pkg/result/result_writer_test.go` (and `CHANGELOG.md`). No other production file changes.
- No changes to `agent-task-executor`, `IncrementFrontmatterExecutor`, `lib/agent_task-frontmatter.go`, Kafka schema, or any `base.CommandOperation` constant.
- `lib.TaskFrontmatter.Phase()` returns `*domain.TaskPhase` (pointer, nil when absent). Do NOT call `string(merged.Phase())` — it won't compile. The production code in this hotfix sets phase unconditionally at cap; it does not need to read the current phase.
- Use the existing `r.currentDateTime` time injection for timestamps in `triggerEscalationSection`. No `time.Now()` directly.
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this hotfix introduces none).
- Ginkgo v2 only (`DescribeTable`, `Entry`, `Describe`, `Context`, `It`). No Ginkgo v1. External test package (`package result_test`) — follow whatever the existing `result_writer_test.go` uses.
- Dedup check must use literal `"\n## Trigger Cap Escalation\n"` and `"\n## Retry Escalation\n"` — anchored to newlines so a partial match in prose (e.g. a task description mentioning the header text) does not falsely suppress the append.
- The `triggerCount > 0` guard is mandatory. Without it, a freshly created task with `max_triggers: 3` and no `trigger_count` field would have `TriggerCount()` return 0, `MaxTriggers()` return 3, `0 >= 3` is false — but a bad future refactor could flip the inequality; the guard documents intent.
- All existing tests must pass after the change.
- Do NOT commit — dark-factory handles git.
- `cd task/controller && make precommit` must exit 0.
</constraints>

<verification>

Verify the new enforcement is in place:
```bash
grep -n "triggerEscalationSection\|containsEscalationSection\|Trigger Cap Escalation\|triggerCount > 0 && triggerCount >= merged.MaxTriggers" \
  ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer.go
```
Must show matches for: the `applyRetryCounter` guard line, the `triggerEscalationSection` function, the `containsEscalationSection` helper, and the section header format string.

Verify the retry path also has dedup:
```bash
grep -n "containsEscalationSection(body, \"## Retry Escalation\")" \
  ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer.go
```
Must show exactly one match.

Verify tests exist and cover the clobber scenario:
```bash
grep -n "sticky\|clobber\|Trigger Cap Escalation\|trigger_count" \
  ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer_test.go
```
Must show at least four distinct matches, including one describing the sticky / clobber-protection behavior (Test B) and one describing dedup (Test C).

Run the focused tests:
```bash
cd ~/Documents/workspaces/agent/task/controller && go test -v ./pkg/result/...
```
Must exit 0. Output must include PASS lines for the four new `It`s (Tests A–D; E if added).

Run full precommit:
```bash
cd ~/Documents/workspaces/agent/task/controller && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "trigger_count.*human_review\|Trigger Cap Escalation\|sticky" \
  ~/Documents/workspaces/agent/CHANGELOG.md
```
Must show the Unreleased entry.

Post-merge live verification (NOT part of this prompt's execution — documented for the human):
1. Deploy to dev.
2. Re-run the dev reproducer task (or create a new one with low `max_triggers` and a failing assignee).
3. Watch the task file in git history: once `trigger_count` reaches `max_triggers`, the file must show `phase: human_review` on disk AND stay there across every subsequent agent result-publish commit. The `## Trigger Cap Escalation` section must appear exactly once.

</verification>
