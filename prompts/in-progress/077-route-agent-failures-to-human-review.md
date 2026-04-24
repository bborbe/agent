---
status: approved
created: "2026-04-24T13:00:00Z"
queued: "2026-04-24T12:57:37Z"
---

<summary>
- Agent-returned `status: failed` now routes the task to `phase: human_review` instead of `phase: ai_review` — the `ai_review` phase is reserved for a review-step role, not a failure bucket
- The failure reason (agent's Message field) is written to the task body as a dedicated `## Failure` section rather than the generic `## Result` section — outcome is distinguishable at a glance
- `needs_input` in `content-generator.go` is unchanged (already routes to `human_review`); in `result-deliverer.go` it gains an explicit case (was falling through to `default` → `ai_review`)
- `done` path is unchanged — completes normally with `## Result` + `phase: done`
- Symmetric with prompt 074: K8s Job crashes (`PublishFailure`) and agent-returned failures both produce `## Failure` body sections and both end in `phase: human_review`
- No retry-via-phase mechanism remains — retries happen only via `trigger_count` / `max_triggers` at the controller level; this change codifies the intent that failure is always human-surfaced
- Two comment updates remove stale rationale about `ai_review` as a retry bucket (`result-deliverer.go` + `content-generator.go`)
- Existing tests updated; new test case added for the `## Failure` section content
</summary>

<objective>
Replace the last two sources of agent-initiated `phase: ai_review` writes with `phase: human_review`, and emit a dedicated `## Failure` body section when the agent returns `status: failed`. After this change, ALL failure paths (K8s Job crashes, agent-returned failures, `needs_input` tasks) converge on `phase: human_review` with a body section that surfaces the reason. Retry is the controller's job (via `trigger_count`), not the result-writer's. Note: the controller's sticky-phase invariant in `result_writer.go:applyRetryCounter` (shipped in v0.52.7) remains as belt-and-suspenders — it defends against stale payloads racing against concurrent escalation; this prompt eliminates the specific source of stale `ai_review` writes it was catching.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `github.com/bborbe/errors`, never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2, external test packages

**Key files to read in full before editing:**

- `lib/delivery/content-generator.go` — `fallbackContentGenerator.Generate` and `applyStatusFrontmatter`. The default-case branch at line 51-53 writes `phase: ai_review` on any non-Done, non-NeedsInput status. `Generate` unconditionally writes a `## Result` section via `ReplaceOrAppendSection` at line 37.

- `lib/delivery/content-generator_test.go` — existing Ginkgo tests. The `It("sets status=in_progress and phase=ai_review for failed result", ...)` block at line 42 asserts the OLD behavior and must be updated.

- `lib/delivery/result-deliverer.go` — `kafkaResultDeliverer.DeliverResult` at line 104-141. Line 120-129 sets `phase: ai_review` explicitly on any non-Done status, with a stale comment claiming this "allows the controller's retry counter to manage retries before escalating". That retry path is the new `trigger_count` mechanism which does NOT depend on `ai_review` — the comment is a remnant.

- `lib/delivery/result-deliverer_test.go` — Ginkgo tests for the deliverers. Read to identify whether they assert on phase values (likely yes for the kafka deliverer).

- `lib/delivery/status.go` — `AgentStatus` constants: `AgentStatusDone`, `AgentStatusFailed`, `AgentStatusNeedsInput`.

- `lib/delivery/markdown.go` — `SetFrontmatterField` and `ReplaceOrAppendSection` primitives. Reuse, do not reimplement.

**Why `## Failure` instead of `## Result` on failure:**
- Matches prompt 074's convention for K8s Job-crash failures (`PublishFailure` writes `## Failure`)
- Readers scanning the vault can distinguish success from failure by section header alone
- Enables future automation to pick only `## Failure` sections for root-cause aggregation without parsing the Status field

**Vault convention (from prompt 074, now a repo-wide convention):**
```markdown
## Failure

- **Timestamp:** 2026-04-24T12:42:47Z
- **Reason:** claude CLI failed: : exit status 1
```
The `## Failure` heading is a frozen contract — do NOT rename it. Prompt 074 uses the same heading for K8s-crash-level failures; consistency matters.

Grep before editing (all paths repo-relative, container-safe):
```bash
grep -n "ai_review\|applyStatusFrontmatter" lib/delivery/content-generator.go
grep -n "ai_review\|kafkaResultDeliverer\|DeliverResult" lib/delivery/result-deliverer.go
grep -n "ai_review\|phase: human_review\|## Failure\|## Result" lib/delivery/content-generator_test.go lib/delivery/result-deliverer_test.go
```
</context>

<requirements>

1. **`lib/delivery/content-generator.go` — update `applyStatusFrontmatter`**

   Replace the `default` case in `applyStatusFrontmatter` (line 51-54):

   ```go
   default: // failed and any other status — infra failure, eligible for retry
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "phase", "ai_review")
   ```

   Replace with:

   ```go
   default:
       // Agent returned status: failed (or unknown). Route to human_review immediately —
       // retry is the controller's job via trigger_count / max_triggers, not a phase loop.
       // The ## Failure body section carries the reason for the human reviewer.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "phase", "human_review")
   ```

2. **`lib/delivery/content-generator.go` — rewrite `fallbackContentGenerator.Generate` to emit `## Failure` on failed status**

   Current body:
   ```go
   func (g *fallbackContentGenerator) Generate(
       _ context.Context,
       originalContent string,
       result AgentResultInfo,
   ) (string, error) {
       updated := applyStatusFrontmatter(originalContent, result.Status)
       section := result.Output
       if section == "" {
           section = buildMinimalResultSection(result)
       }
       return ReplaceOrAppendSection(updated, "## Result", section), nil
   }
   ```

   Replace with:
   ```go
   func (g *fallbackContentGenerator) Generate(
       _ context.Context,
       originalContent string,
       result AgentResultInfo,
   ) (string, error) {
       updated := applyStatusFrontmatter(originalContent, result.Status)
       if result.Status == AgentStatusFailed {
           section := buildFailureSection(result)
           return ReplaceOrAppendSection(updated, "## Failure", section), nil
       }
       section := result.Output
       if section == "" {
           section = buildMinimalResultSection(result)
       }
       return ReplaceOrAppendSection(updated, "## Result", section), nil
   }
   ```

3. **`lib/delivery/content-generator.go` — add `buildFailureSection`**

   Sibling to `buildMinimalResultSection`:

   ```go
   // buildFailureSection renders a `## Failure` block with a human-readable
   // reason extracted from the agent's result. Used when the agent returns
   // status: failed — symmetric with PublishFailure's K8s-crash failure path.
   func buildFailureSection(result AgentResultInfo) string {
       var b strings.Builder
       b.WriteString("## Failure\n\n")
       if result.Message != "" {
           b.WriteString("- **Reason:** ")
           b.WriteString(result.Message)
           b.WriteString("\n")
       } else {
           b.WriteString("- **Reason:** agent returned status: failed (no message provided)\n")
       }
       return b.String()
   }
   ```

   Note: this generator does NOT have a `CurrentDateTimeGetter` injection. Timestamp is omitted here — callers that have a clock (the Kafka result-deliverer with injected `d.currentDateTime`) can enrich later; the fallback stays deterministic for testability.

4. **`lib/delivery/result-deliverer.go` — update `kafkaResultDeliverer.DeliverResult`**

   Replace the `switch` block at ~line 122 (along with its preceding comment block at lines 117-121 — the comment's `ai_review` rationale is now stale) with:

   ```go
   // Set status/phase from result.Status directly. The content generator may not
   // have frontmatter to update (TASK_CONTENT is body-only), so we set it explicitly.
   // Failed tasks route to human_review — retry is the controller's responsibility
   // via trigger_count / max_triggers, not a phase loop.
   switch result.Status {
   case AgentStatusDone:
       frontmatter["status"] = "completed"
       frontmatter["phase"] = "done"
   case AgentStatusNeedsInput:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   default:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   }
   ```

   Note: `needs_input` and the default case both set `human_review`. Keep them as separate cases (explicit intent) rather than merging — future divergence (e.g. distinct section headers) becomes a single-case edit.

5. **`lib/delivery/content-generator_test.go` — update the failing-result test**

   Find `It("sets status=in_progress and phase=ai_review for failed result", ...)` at line 42. Rename and update:

   ```go
   It("sets status=in_progress and phase=human_review for failed result with ## Failure section", func() {
       // (preserve existing test setup)
       generated, err := generator.Generate(ctx, originalContent, AgentResultInfo{
           Status:  AgentStatusFailed,
           Message: "claude CLI failed: exit status 1",
       })
       Expect(err).NotTo(HaveOccurred())
       Expect(generated).To(ContainSubstring("status: in_progress"))
       Expect(generated).To(ContainSubstring("phase: human_review"))
       Expect(generated).NotTo(ContainSubstring("phase: ai_review"))
       Expect(generated).To(ContainSubstring("## Failure"))
       Expect(generated).To(ContainSubstring("claude CLI failed: exit status 1"))
       Expect(generated).NotTo(ContainSubstring("## Result"))
   })
   ```

   Adapt to the existing test's variable names and `originalContent` fixture — do not introduce new fixtures.

   **Add a new test case** for `needs_input` → `## Result` (not `## Failure`):
   ```go
   It("keeps status=in_progress, phase=human_review, ## Result section for needs_input", func() {
       generated, err := generator.Generate(ctx, originalContent, AgentResultInfo{
           Status:  AgentStatusNeedsInput,
           Message: "no date range in task",
       })
       Expect(err).NotTo(HaveOccurred())
       Expect(generated).To(ContainSubstring("status: in_progress"))
       Expect(generated).To(ContainSubstring("phase: human_review"))
       Expect(generated).To(ContainSubstring("## Result"))
       Expect(generated).NotTo(ContainSubstring("## Failure"))
   })
   ```

   The `done` path test (if present) needs no change.

6. **`lib/delivery/result-deliverer_test.go` — update kafka-deliverer failure assertions**

   Find any test that asserts on `phase: ai_review` in the published Kafka payload. Update to assert `phase: human_review`.

   If a test exists that asserts failure → ai_review, rename to describe the new behavior:
   `It("publishes failed result with phase=human_review", ...)`.

   If no such test exists, add one:
   ```go
   It("publishes failed result with phase=human_review", func() {
       // (reuse the existing test setup — build a kafkaResultDeliverer with the test fake Kafka)
       err := deliverer.DeliverResult(ctx, AgentResultInfo{
           Status:  AgentStatusFailed,
           Message: "task runner failed: timeout",
       })
       Expect(err).NotTo(HaveOccurred())
       // Assert the captured Kafka event's frontmatter contains phase: human_review.
       // (adapt to how the existing tests inspect captured events)
   })
   ```

   Mirror this with a test for `needs_input` → `phase: human_review` if not already present.

7. **Global grep — confirm no remaining `phase: ai_review` writes in lib/delivery**

   After the edits, this must return no production-code matches (test references are allowed only if they're negated — e.g. `NotTo(ContainSubstring("phase: ai_review"))`):

   ```bash
   grep -n 'phase.*ai_review\|ai_review.*phase\|"ai_review"' lib/delivery/*.go | grep -v _test.go
   ```

   Must be empty.

8. **Do NOT modify:**
   - `lib/delivery/status.go` — `AgentStatus` constants stay as-is
   - `lib/delivery/markdown.go` — `SetFrontmatterField`, `ReplaceOrAppendSection` primitives unchanged
   - `lib/delivery/print.go`
   - `lib/delivery/content-generator.go`'s `buildMinimalResultSection` function — still used for `needs_input` + any non-failure path
   - `task/executor/pkg/result_publisher.go` — already correct after prompt 074
   - `task/controller/pkg/result/result_writer.go` — already correct after hotfix 072 (sticky human_review at trigger cap)
   - Any Kafka schema constant or `base.CommandOperation` — wire format unchanged
   - `task/executor/pkg/handler/task_event_handler.go` — default trigger phases still include `ai_review` because other (pre-existing) code paths or human ops may land tasks in `ai_review`; removing it from the trigger list is a separate concern

9. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased`:

   ```markdown
   - fix(lib): agent-returned `status: failed` now routes to `phase: human_review` (was: `ai_review`) and writes a dedicated `## Failure` body section with the failure reason — symmetric with `PublishFailure` behavior for K8s Job crashes
   - fix(lib): `kafkaResultDeliverer.DeliverResult` no longer emits `phase: ai_review` on failure; `needs_input` and `failed` both route to `human_review` (retries are the controller's job via `trigger_count`)
   ```

10. **Verification commands** (all paths repo-relative)

    Must exit 0:
    ```bash
    cd lib && make precommit
    ```

    Spot checks:
    ```bash
    grep -c 'ai_review' lib/delivery/content-generator.go lib/delivery/result-deliverer.go
    ```
    Production file count of `ai_review` must be 0 (comments and code both gone).

    ```bash
    grep -n 'human_review' lib/delivery/content-generator.go lib/delivery/result-deliverer.go
    ```
    Must show at least 3 matches across both files (the two `phase` assignments plus the new comment).

    ```bash
    grep -n 'buildFailureSection\|## Failure' lib/delivery/content-generator.go
    ```
    Must show the new helper + the heading constant.

</requirements>

<constraints>
- Only edit these files:
  - `lib/delivery/content-generator.go` (update `applyStatusFrontmatter`, add `buildFailureSection`, update `Generate`)
  - `lib/delivery/content-generator_test.go` (update + add tests)
  - `lib/delivery/result-deliverer.go` (update `DeliverResult` switch)
  - `lib/delivery/result-deliverer_test.go` (update + add tests)
  - `CHANGELOG.md`
- The `## Failure` heading is a frozen contract — do NOT rename. Matches prompt 074 / `PublishFailure` convention.
- Do NOT touch `status.go`, `markdown.go`, `print.go`, `result_publisher.go`, `result_writer.go`, `task_event_handler.go`, Kafka schema, or any `base.CommandOperation`.
- Keep `needs_input` and `failed` as separate `case` branches in the kafka deliverer — even though both currently set `human_review`, splitting them is intentional for future divergence.
- `buildFailureSection` uses no time injection — the `fallbackContentGenerator` has no clock injected. Kafka deliverer has one but the section format is shared; leave timestamps out of this helper to keep it deterministic. (Future enhancement: inject clock if needed.)
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this prompt introduces none).
- Ginkgo v2 only. External test packages match existing file conventions.
- All existing tests must pass after the change. Tests that asserted the old `ai_review` behavior are updated (not deleted).
- Do NOT commit — dark-factory handles git.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify production-code `ai_review` is gone from lib/delivery:
```bash
grep -n 'ai_review' lib/delivery/*.go | grep -v _test.go
```
Must return no results.

Verify `human_review` assignments:
```bash
grep -n '"human_review"\|phase.*human_review' lib/delivery/content-generator.go lib/delivery/result-deliverer.go
```
Must show at least 3 matches (content-generator default case, kafka deliverer needs_input case, kafka deliverer default case).

Verify `buildFailureSection` exists:
```bash
grep -nA1 'func buildFailureSection' lib/delivery/content-generator.go
```
Must show the function signature.

Verify `Generate` routes failed to `## Failure`:
```bash
grep -n 'AgentStatusFailed\|"## Failure"' lib/delivery/content-generator.go
```
Must show both: the status check and the heading string.

Verify tests cover new behavior:
```bash
grep -n '## Failure\|human_review\|buildFailureSection' lib/delivery/content-generator_test.go lib/delivery/result-deliverer_test.go
```
Must show matches in both test files — at least one `## Failure` assertion and one `human_review` assertion.

Run focused tests:
```bash
cd lib && go test -v ./delivery/...
```
Must exit 0. Output must include PASS lines for the updated/new tests.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n 'human_review\|## Failure' CHANGELOG.md
```
Must show the Unreleased entries.

Post-merge live verification (NOT part of this prompt's execution — documented for the human):
1. Deploy new lib release. Deploy task-executor + task-controller (they vendor lib).
2. Trigger an agent failure (e.g. post a task to an agent whose Claude OAuth is missing).
3. Observe task file commit: frontmatter shows `phase: human_review`; body contains a `## Failure` section with `- **Reason:** <agent message>`.
4. Confirm the task appears in the human_review queue, not ai_review.
</verification>
