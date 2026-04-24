---
status: completed
summary: 'Reordered applyRetryCounter to run trigger_count cap enforcement before the spawn_notification early return, fixing the live-observed regression where inherited spawn_notification=true caused phase: human_review to revert to ai_review; added regression-guard unit test; updated CHANGELOG.md.'
container: agent-075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification
dark-factory-version: v0.132.0
created: "2026-04-24T12:00:00Z"
queued: "2026-04-24T12:05:59Z"
started: "2026-04-24T12:06:01Z"
completed: "2026-04-24T12:09:50Z"
---

<summary>
- Reorders `resultWriter.applyRetryCounter` so the `trigger_count` cap escalation runs BEFORE the `spawn_notification` early-return branch
- Closes a live-observed regression of prompt 072's hotfix: every agent result write carries an inherited `spawn_notification: true` in the merged frontmatter, so the function short-circuits before reaching the cap check and `phase: human_review` reverts to `ai_review`
- Encodes the live-dev failure mode from task `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9` (controller commit `1a1c570`, v0.52.4) as a regression-guard unit test
- Single-file production change (`task/controller/pkg/result/result_writer.go`); test file extended, not replaced
- Reuses the `containsEscalationSection` helper and `triggerEscalationSection` function introduced in prompt 072 — no duplication
- Preserves prompt 072's behavior when `spawn_notification` is absent (cap-reached writes still escalate)
</summary>

<objective>
Make `trigger_count >= max_triggers` cap enforcement in `resultWriter.applyRetryCounter` unconditional by running it BEFORE the `spawn_notification` early return. This fixes a silent regression where agent result writes inherit `spawn_notification: true` via `mergeFrontmatter` and skip the cap check, clobbering the controller's sticky `phase: human_review` escalation.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — this hotfix introduces no new error paths, but if any arise use `github.com/bborbe/errors`, never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2, external test packages, builder helpers; extend the existing `result_writer_test.go`
- `go-time-injection.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `libtime.CurrentDateTimeGetter` injection; tests use the existing fixed-time fake from the result-writer suite

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — retry vs trigger semantics, escalation rules, `phase` field lifecycle
- `docs/controller-design.md` — `TaskResultExecutor`, `IncrementFrontmatterExecutor`, result-writer responsibilities, `mergeFrontmatter` semantics (incoming keys win per key, existing keys preserved if absent from incoming)

**Why this hotfix exists — the 072 hotfix is half-broken in production.**

Prompt 072 added a `trigger_count >= max_triggers` escalation block to `resultWriter.applyRetryCounter`, but placed it AFTER the existing `merged.SpawnNotification()` early-return branch:

```go
func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {
    if string(merged.Status()) == "completed" {
        return body
    }
    if merged.SpawnNotification() {      // <-- early return short-circuits below
        delete(merged, "spawn_notification")
        return body
    }
    // retry_count block...
    // trigger_count block (added by 072) — never reached when spawn_notification inherited
    return body
}
```

In practice, once the executor has written a spawn-notification update to a task file (setting `spawn_notification: true`), the `mergeFrontmatter` call in `WriteResult` preserves that key on every subsequent write until something explicitly deletes it. The agent's result-publish payload does not set `spawn_notification: false` — it simply omits the key — so `mergeFrontmatter` keeps the existing `true`. Thus every agent result write hits the `SpawnNotification()` early return and skips both the retry-cap and trigger-cap escalation blocks.

**Live-observed failure (dev, 2026-04-24):**

Task: `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9`. Controller commit: `1a1c570` (v0.52.4, includes the 072 hotfix). Git history of the task file:

1. Commit `eff17e6` — `IncrementFrontmatterExecutor` bumps `trigger_count` 2→3, hits cap, sets `phase: ai_review → human_review`. Correct.
2. Commit `80b6dfa` — spawn-notification update write. `spawn_notification: true` lands in the file. `phase: human_review` preserved. Correct.
3. Commit `e9cf6a8` — agent's `publish()` result write through `TaskResultExecutor → WriteResult → applyRetryCounter`. Incoming frontmatter has `phase: ai_review` (agent still thinks it's in AI phase). `mergeFrontmatter` yields merged = `{phase: ai_review (incoming wins), trigger_count: 3, max_triggers: 3, spawn_notification: true (inherited from commit 2), status: in_progress}`. `applyRetryCounter` sees `merged.SpawnNotification() == true`, takes the early return, never reaches the `triggerCount >= MaxTriggers()` check. File lands with `phase: ai_review`, `trigger_count: 3`, no `## Trigger Cap Escalation` section. Dead end — task invisible to human review queue.

**Root cause:** the 072 hotfix's ordering assumed the spawn-notification branch was mutually exclusive with cap-reached result writes. It is not — `mergeFrontmatter` makes the two signals overlap on every result write that follows a spawn-notification write. The cap-enforcement block must run BEFORE any early return so it is a true invariant over the on-disk state after `applyRetryCounter`.

**Key files to read in full before editing:**

- `task/controller/pkg/result/result_writer.go` — current state (verified 2026-04-24):
  - Line 124: `body := r.applyRetryCounter(merged, string(req.Content))` inside `WriteResult`
  - Line 150: `func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {`
  - Line 151-153: `completed` short-circuit
  - Line 154-157: `SpawnNotification()` early return (THIS is what moves down)
  - Line 158-166: retry_count cap block with dedup
  - Line 167-180: trigger_count cap block with dedup (THIS is what moves up)
  - Line 184-192: `escalationSection` helper
  - Line 194-206: `triggerEscalationSection` helper
  - Line 208-215: `containsEscalationSection` helper
  - `mergeFrontmatter` below these — do NOT touch
- `task/controller/pkg/result/result_writer_test.go` — existing Ginkgo test; extend this file (do NOT create a parallel file). Read it end-to-end to understand the builder pattern, the fake git client (how written file contents are read back for assertion), the fake `CurrentDateTimeGetter`, and how the 072 tests (`Context("trigger_count cap escalation", ...)`) are structured. Place the new `It(...)` inside that existing context or a sibling context named `"trigger_count cap escalation with inherited spawn_notification"` — choose whichever placement keeps tests grouped logically.
- `task/controller/pkg/result/result_suite_test.go` — Ginkgo suite root; no changes expected.
- `lib/agent_task-frontmatter.go` — no changes. Confirmed accessors used: `Status()`, `SpawnNotification()`, `TriggerCount()`, `MaxTriggers()`, `RetryCount()`, `MaxRetries()`, `Assignee()`. `SpawnNotification()` returns `bool`; the existing code pattern `if merged.SpawnNotification() { delete(merged, "spawn_notification"); return body }` is the canonical consumption — do NOT change its semantics, only its position relative to the cap block.

Grep before editing to confirm the landscape is as described above:

```bash
cd ~/Documents/workspaces/agent
grep -n "applyRetryCounter\|SpawnNotification\|TriggerCount\|MaxTriggers\|RetryCount\|MaxRetries\|containsEscalationSection\|triggerEscalationSection" task/controller/pkg/result/result_writer.go
grep -n "spawn_notification\|SpawnNotification\|trigger_count\|Trigger Cap Escalation" task/controller/pkg/result/result_writer_test.go
```
</context>

<requirements>

1. **Reorder `applyRetryCounter` in `task/controller/pkg/result/result_writer.go`**

   Current body (verify by reading the file — anchors at function signature `func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string`; line numbers are hints only):

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
       // (long comment from prompt 072 — preserve it)
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

   Replace with this exact body (the `completed` short-circuit stays on top; the trigger_count block moves ABOVE the spawn_notification early return; the retry_count block stays where it is, below the spawn_notification return):

   ```go
   func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string {
       if string(merged.Status()) == "completed" {
           return body
       }

       // Trigger-count cap enforcement runs unconditionally before any early
       // returns below: it is a derived invariant on the on-disk state that
       // must hold after every WriteResult. Placing it here also prevents the
       // spawn_notification short-circuit below from silently skipping
       // escalation on agent result writes that inherited spawn_notification
       // from a previous merge (observed live on dev 2026-04-24, task
       // ba1bad61: spawn-notification update set spawn_notification=true,
       // then the agent's result publish inherited it via mergeFrontmatter
       // and skipped the cap check, reverting phase: human_review to
       // ai_review). The triggerCount > 0 guard prevents degenerate
       // escalation of brand-new tasks where trigger_count is absent.
       triggerCount := merged.TriggerCount()
       if triggerCount > 0 && triggerCount >= merged.MaxTriggers() {
           merged["phase"] = "human_review"
           if !containsEscalationSection(body, "## Trigger Cap Escalation") {
               body += r.triggerEscalationSection(triggerCount, merged)
           }
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
       return body
   }
   ```

   New execution order: `status → trigger_count → spawn_notification → retry_count`. Previous order (from prompt 072): `status → spawn_notification → retry_count → trigger_count`.

   Notes:
   - Do NOT duplicate the `containsEscalationSection` helper or the `triggerEscalationSection` function. Both already exist from prompt 072 (verified present in the current file — see `<context>` line anchors). Leave them exactly as they are.
   - Do NOT modify `escalationSection` (the retry escalation section helper).
   - Do NOT modify `mergeFrontmatter` or any other function in the file.
   - The `merged["phase"] = "human_review"` raw-string assignment is correct (matches prompt 072 choice; the map is `map[string]interface{}` and is YAML-marshalled as a string). Do NOT introduce `domain.TaskPhaseHumanReview` here.
   - Why retry_count stays below the spawn_notification return: the retry_count path has been in place since spec 011 and has not been observed to interact with the spawn_notification inheritance bug (retry_count is only bumped by the executor at spawn, and the spawn path does not set spawn_notification). Moving it would expand the hotfix scope without evidence. If a future live regression shows retry-cap escalation skipped on spawn-notification writes, extract the retry block up in a follow-up hotfix.

2. **Mandatory regression-guard test — extend `task/controller/pkg/result/result_writer_test.go`**

   Read the existing test file end-to-end first. Identify:
   - The top-level `Describe("resultWriter", ...)` (or equivalent root).
   - The `Context("trigger_count cap escalation", ...)` block added by prompt 072.
   - The builder helpers used to set up existing on-disk file frontmatter and incoming `lib.Task` payloads.
   - The fake git client — how it exposes written file content for assertion (typically a read-back method returning the full file contents as `[]byte` or `string`).
   - The fake `CurrentDateTimeGetter` — so new timestamp assertions are deterministic (or avoided; the regression-guard test below does not need to assert on timestamp values, only on presence of the section header).

   Append one new `It(...)` inside the existing `Context("trigger_count cap escalation", ...)` from prompt 072 (preferred), OR add a sibling `Context("trigger_count cap escalation with inherited spawn_notification", ...)` at the same level if placement inside the existing context creates awkward builder-setup duplication. Use the existing helpers — do not introduce parallel infrastructure.

   **Test: "trigger_count cap escalation sticks even when merged frontmatter carries spawn_notification=true"**

   This test encodes the live-dev failure from task `ba1bad61`.

   Setup:
   - Existing file on disk: frontmatter `phase: human_review, trigger_count: 3, max_triggers: 3, status: in_progress, spawn_notification: true` (simulates the state after the executor's spawn-notification write at commit `80b6dfa`). Body: contains a prior `## Result` block from an earlier agent run. Optionally also contains a prior `## Trigger Cap Escalation` block — include it in a sub-variant only if the existing `Context` already tests the no-prior-section case; otherwise keep the body simple (one prior `## Result`).
   - Incoming `lib.Task`: `Frontmatter` = `{phase: ai_review, status: in_progress}` (simulates the agent's stale publish — the agent has not observed the escalation); `Content` = `## Result\nStatus: failed\nMessage: gh auth failed\n` (fresh agent output).

   Call `WriteResult(ctx, req)` with the appropriate request struct (follow the pattern in the existing tests; typically the builder constructs a `lib.Task` and the test invokes the writer method with it).

   Expected after `WriteResult`:
   - Written file frontmatter has `phase: human_review` — the sticky escalation survived despite the incoming `phase: ai_review` AND the inherited `spawn_notification: true`. This is the core assertion.
   - Written file frontmatter has `trigger_count: 3, max_triggers: 3` preserved.
   - Written file frontmatter has NO `spawn_notification` key — the reordered function still hits the `delete(merged, "spawn_notification"); return body` branch after the cap block has already set `phase`, so the key is consumed exactly once per write.
   - Written file body contains the string `## Trigger Cap Escalation` exactly once (assert via `strings.Count(bodyStr, "## Trigger Cap Escalation") == 1`).
   - Written file body contains the agent's fresh `## Result` content — assert on the substrings `Status: failed` and `gh auth failed` to prove the agent's publish lands (nothing is lost; the reorder does not swallow content).

   Name the `It(...)` descriptively, e.g.:
   `It("keeps phase: human_review sticky and appends Trigger Cap Escalation even when merged frontmatter carries inherited spawn_notification=true", func() { ... })`

   This test is THE regression guard for the reorder. It must fail against the pre-reorder code (the 072 state) and pass against the post-reorder code.

3. **Retain the prompt-072 "cap-reached without spawn_notification" test unchanged**

   The existing `Context("trigger_count cap escalation", ...)` from prompt 072 already contains Test B ("at cap with stale `phase: ai_review` incoming — clobber protection"). That test does NOT include `spawn_notification: true` in the existing frontmatter. It must continue to pass after the reorder, proving the non-spawn-notification path still escalates. Do NOT delete, rename, or modify any existing test. If you find a test that accidentally includes `spawn_notification: true` and was passing before the reorder only because the early return was hit first, note it in your summary — but leave it alone unless it starts failing.

4. **No other production changes**

   Do NOT modify `lib/agent_task-frontmatter.go`, `agent-task-executor`, `IncrementFrontmatterExecutor`, `mergeFrontmatter`, `escalationSection`, `triggerEscalationSection`, `containsEscalationSection`, any Kafka schema file, any `base.CommandOperation` constant, or any other file.

5. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased` (create the section if absent; do not touch released sections):

   ```markdown
   - fix: reorder `resultWriter.applyRetryCounter` to run `trigger_count` cap escalation BEFORE the `spawn_notification` early return; fixes a live-observed regression of the 072 hotfix where agent result writes that inherited `spawn_notification: true` via `mergeFrontmatter` skipped the cap check and reverted `phase: human_review` to `ai_review` (task `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9` on dev, controller v0.52.4); adds a regression-guard unit test
   ```

6. **Verification commands**

   Must exit 0:

   ```bash
   cd ~/Documents/workspaces/agent/task/controller && make precommit
   ```

   Structural spot checks (must show the new ordering):

   ```bash
   awk '/func.*applyRetryCounter/,/^}$/' \
     ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer.go \
     | grep -n 'TriggerCount\|SpawnNotification\|RetryCount'
   ```
   Expected output (line numbers relative to the awk slice; the relative order is what matters): `TriggerCount` appears on a lower line number than `SpawnNotification`, which appears on a lower line number than `RetryCount`.

   ```bash
   grep -n 'spawn_notification.*true\|inherited spawn_notification\|sticky.*spawn_notification' \
     ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer_test.go
   ```
   Must show at least one match corresponding to the new regression-guard test.

   Focused test run:

   ```bash
   cd ~/Documents/workspaces/agent/task/controller && go test -v ./pkg/result/...
   ```
   Must exit 0. Output must include PASS for the new `It` and for every existing `It` in the file (no regressions).

   CHANGELOG check:

   ```bash
   grep -n 'reorder.*applyRetryCounter\|spawn_notification.*early return\|inherited.*spawn_notification' \
     ~/Documents/workspaces/agent/CHANGELOG.md
   ```
   Must show the Unreleased entry.

</requirements>

<constraints>
- Only edit `task/controller/pkg/result/result_writer.go`, `task/controller/pkg/result/result_writer_test.go`, and `CHANGELOG.md`. No other production file changes.
- Do NOT modify `agent-task-executor`, `IncrementFrontmatterExecutor`, `lib/agent_task-frontmatter.go`, `mergeFrontmatter`, `escalationSection`, `triggerEscalationSection`, `containsEscalationSection`, any Kafka schema, or any `base.CommandOperation` constant.
- Do NOT duplicate `containsEscalationSection` or `triggerEscalationSection` — they already exist from prompt 072. The reorder only moves existing call sites around inside `applyRetryCounter`.
- The retry_count cap block stays BELOW the `spawn_notification` early return. Do NOT move it up — that expands scope without live-observed justification. This hotfix targets the trigger_count path only.
- The `triggerCount > 0` guard is mandatory — keep it exactly as in prompt 072. Without it, brand-new tasks with `max_triggers: N` and no `trigger_count` field would degenerately escalate if a future refactor flips the inequality.
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this hotfix introduces none).
- Ginkgo v2 only (`Describe`, `Context`, `It`, `DescribeTable`, `Entry`). External test package (`package result_test`) — follow whatever the existing `result_writer_test.go` uses.
- Use the existing `r.currentDateTime` time injection for any timestamps; no `time.Now()` directly. The regression-guard test does not need to assert on timestamp values — asserting on section-header presence and count is sufficient.
- All existing tests must pass after the reorder. If any existing test starts failing because it was accidentally relying on the old ordering (e.g. a test with `spawn_notification: true` that previously short-circuited the cap block), DO NOT "fix" it by reverting the reorder — diagnose the test, update it to reflect the new correct behavior, and note the change explicitly in your summary. The new ordering IS the correct behavior.
- Do NOT commit — dark-factory handles git.
- `cd task/controller && make precommit` must exit 0.
</constraints>

<verification>

Verify the reorder is in place structurally (relative line-number ordering of the three accessor calls inside `applyRetryCounter`):

```bash
awk '/func.*applyRetryCounter/,/^}$/' \
  ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer.go \
  | grep -n 'TriggerCount\|SpawnNotification\|RetryCount'
```
The `TriggerCount` match line must precede the `SpawnNotification` match line, which must precede the `RetryCount` match line. This is the single structural assertion that proves the reorder.

Verify no duplication of helpers introduced by prompt 072:

```bash
grep -c 'func.*triggerEscalationSection\|func containsEscalationSection' \
  ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer.go
```
Must output `2` (one definition each). If `3` or more, a duplicate was added — remove it.

Verify the regression-guard test exists:

```bash
grep -n 'spawn_notification\|inherited\|sticky' \
  ~/Documents/workspaces/agent/task/controller/pkg/result/result_writer_test.go
```
Must show at least one match describing the new `It`.

Run focused tests:

```bash
cd ~/Documents/workspaces/agent/task/controller && go test -v ./pkg/result/...
```
Must exit 0. Output must include PASS for the new `It` and for every prompt-072 `It` (Tests A–E from prompt 072's description).

Run full precommit:

```bash
cd ~/Documents/workspaces/agent/task/controller && make precommit
```
Must exit 0.

Verify CHANGELOG entry:

```bash
grep -n 'reorder.*applyRetryCounter\|spawn_notification.*early return\|inherited.*spawn_notification' \
  ~/Documents/workspaces/agent/CHANGELOG.md
```
Must show the Unreleased entry.

Post-merge live verification (NOT part of this prompt's execution — documented for the human operator):
1. Deploy controller with this fix to dev.
2. Identify or create a task that will go through the spawn-notification-then-result-publish sequence (any task that triggers a spawn-notification update before the agent's final publish).
3. Force `trigger_count` to reach `max_triggers` (low `max_triggers` value, failing assignee).
4. Inspect the task file's git history: after the increment commit (escalation), the spawn-notification commit, and the agent's result-publish commit, the on-disk file must read `phase: human_review` — not `ai_review`. Exactly one `## Trigger Cap Escalation` section must be present. This is the inverse of the `ba1bad61` failure.

</verification>
