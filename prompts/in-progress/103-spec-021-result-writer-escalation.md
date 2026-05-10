---
status: committing
spec: [021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation]
summary: Modified result_writer.go to clear assignee on all escalation paths and preserve lifecycle phase on cap escalations; refactored into applyTriggerCap/applyRetryCap helpers; updated and extended tests to 94.1% coverage; updated CHANGELOG.md
container: agent-103-spec-021-result-writer-escalation
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-10T16:45:00Z"
queued: "2026-05-10T20:09:43Z"
started: "2026-05-10T20:09:45Z"
branch: dark-factory/clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation
---

<summary>
- Parked tasks (cap-reached or needs-input) now surface with `assignee: ""` so operator boards can detect unclaimed tasks using a single consistent field
- Trigger cap escalation no longer overwrites `phase` to `human_review`; the task stays at the lifecycle stage where the cap fired (`planning`, `in_progress`, `ai_review`) so the operator can distinguish infra failures from genuine human-work requests
- Retry cap escalation behaves the same — phase is preserved, assignee is cleared
- The `needs_input` path continues to produce `phase: human_review` and now also clears `assignee: ""`
- Escalation section text still records the agent name that was active at escalation time (captured before the clear), so the body still answers "who burned the budget?"
- Once a task is parked (escalation section present, `assignee: ""`), a subsequent stale agent result cannot revive the assignee or clobber the lifecycle phase — cap stickiness is preserved under the new shape
</summary>

<objective>
Modify `result_writer.go` so that every escalation path (trigger cap, retry cap, needs_input) writes `assignee: ""` on the task file, while trigger-cap and retry-cap escalations stop overwriting `phase` to `human_review` and instead preserve the lifecycle stage the task was at when the cap fired. The existing phase-stickiness invariant (spec 015) is extended: once a task is parked at cap, repeated stale-result writes preserve both the cap-fired phase and the empty assignee.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, Counterfeiter, external test packages, ≥80% coverage
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**

- `task/controller/pkg/result/result_writer.go` — full read; focus on `applyRetryCounter`, `triggerEscalationSection`, `escalationSection`, `mergeFrontmatter`, and the `WriteResult` call site
- `task/controller/pkg/result/result_writer_test.go` — full read; all existing Context blocks must be understood before writing changes
- `lib/agent_task-frontmatter.go` — read `Assignee()`, `Phase()`, `TriggerCount()`, `MaxTriggers()`, `RetryCount()`, `MaxRetries()`, `SpawnNotification()` so you know their exact return types

Current `applyRetryCounter` signature (inline for reference — verify against actual file):
```go
func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string
```

Current `triggerEscalationSection` signature:
```go
func (r *resultWriter) triggerEscalationSection(triggerCount int, merged lib.TaskFrontmatter) string
// reads merged.Assignee() and merged.MaxTriggers() internally
```

Current `escalationSection` signature:
```go
func (r *resultWriter) escalationSection(retryCount int, merged lib.TaskFrontmatter) string
// reads merged.Assignee() internally
```

Current call site in `WriteResult`:
```go
merged := mergeFrontmatter(existingFrontmatter, req.Frontmatter)
body := r.applyRetryCounter(merged, string(req.Content))
```
</context>

<requirements>

1. **Update `applyRetryCounter` signature to receive `existing lib.TaskFrontmatter`**

   Change:
   ```go
   func (r *resultWriter) applyRetryCounter(merged lib.TaskFrontmatter, body string) string
   ```
   To:
   ```go
   func (r *resultWriter) applyRetryCounter(merged, existing lib.TaskFrontmatter, body string) string
   ```

   Update the call site in `WriteResult`:
   ```go
   body := r.applyRetryCounter(merged, existingFrontmatter, string(req.Content))
   ```
   `existingFrontmatter` is already available in `WriteResult` from the `FindTaskFilePath` return.

2. **Rewrite the trigger-cap block inside `applyRetryCounter`**

   Replace:
   ```go
   if triggerCount > 0 && triggerCount >= merged.MaxTriggers() {
       merged["phase"] = "human_review"
       if !containsEscalationSection(body, "## Trigger Cap Escalation") {
           body += r.triggerEscalationSection(triggerCount, merged)
       }
   }
   ```

   With:
   ```go
   if triggerCount > 0 && triggerCount >= merged.MaxTriggers() {
       agentName := string(merged.Assignee()) // capture before clear
       merged["assignee"] = ""
       if containsEscalationSection(body, "## Trigger Cap Escalation") {
           // task already parked: restore existing lifecycle phase (cap stickiness)
           if p := existing.Phase(); p != nil {
               merged["phase"] = string(*p)
           }
       } else {
           body += r.triggerEscalationSection(triggerCount, agentName, merged)
       }
   }
   ```

   Key points:
   - Assignee is captured **before** `merged["assignee"] = ""` so the escalation section records who burned the budget
   - `merged["phase"] = "human_review"` is **removed** — phase is left at whatever the merge produced (the pre-cap lifecycle stage)
   - When the escalation section is **already present** (already parked), restore the on-disk phase via `existing.Phase()` to prevent stale-result phase clobber. If `existing.Phase()` returns nil (key absent on disk), leave `merged["phase"]` as-is
   - When the section is **not present** (first escalation), only append the section; phase is already correct

3. **Rewrite the retry-cap block inside `applyRetryCounter`**

   Replace:
   ```go
   if retryCount >= merged.MaxRetries() {
       merged["phase"] = "human_review"
       if !containsEscalationSection(body, "## Retry Escalation") {
           body += r.escalationSection(retryCount, merged)
       }
   }
   ```

   With:
   ```go
   if retryCount >= merged.MaxRetries() {
       agentName := string(merged.Assignee()) // capture before clear
       merged["assignee"] = ""
       if containsEscalationSection(body, "## Retry Escalation") {
           // task already parked: restore existing lifecycle phase (cap stickiness)
           if p := existing.Phase(); p != nil {
               merged["phase"] = string(*p)
           }
       } else {
           body += r.escalationSection(retryCount, agentName)
       }
   }
   ```

   Same pattern as trigger cap: capture assignee before clearing, restore existing phase when section already present, drop `merged["phase"] = "human_review"`.

4. **Add `needs_input` assignee clear at the end of `applyRetryCounter`**

   After the retry-cap block (before `return body`), add:
   ```go
   // needs_input: agent explicitly requested human review — clear assignee so task surfaces in operator inbox
   if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
       merged["assignee"] = ""
   }
   ```

   This fires when the agent emitted `needs_input` (setting `phase: human_review` in its frontmatter) and no cap has overridden the phase. If the trigger-cap block already fired and restored the existing phase to `human_review` (task parked before spec-021 under old behavior), the assignee is cleared again — idempotent no-op.

5. **Update `triggerEscalationSection` signature and body**

   Change from reading `merged.Assignee()` internally to accepting the pre-cleared agent name:

   Replace:
   ```go
   func (r *resultWriter) triggerEscalationSection(
       triggerCount int,
       merged lib.TaskFrontmatter,
   ) string {
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

   With:
   ```go
   func (r *resultWriter) triggerEscalationSection(
       triggerCount int,
       agentName string,
       merged lib.TaskFrontmatter,
   ) string {
       ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
       return fmt.Sprintf(
           "\n## Trigger Cap Escalation\n\n- **Timestamp:** %s\n- **Trigger count:** %d\n- **Max triggers:** %d\n- **Assignee:** %s\n- **Last agent output:** see `## Result` above\n",
           ts,
           triggerCount,
           merged.MaxTriggers(),
           agentName,
       )
   }
   ```

   `agentName` is passed by the caller (captured before `merged["assignee"] = ""`). `merged.MaxTriggers()` is still read from the merged map (field not modified by the cap block).

6. **Update `escalationSection` signature and body**

   Replace:
   ```go
   func (r *resultWriter) escalationSection(retryCount int, merged lib.TaskFrontmatter) string {
       ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
       return fmt.Sprintf(
           "\n## Retry Escalation\n\n- **Timestamp:** %s\n- **Attempts:** %d\n- **Assignee:** %s\n- **Last error:** see agent output above\n",
           ts,
           retryCount,
           string(merged.Assignee()),
       )
   }
   ```

   With:
   ```go
   func (r *resultWriter) escalationSection(retryCount int, agentName string) string {
       ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
       return fmt.Sprintf(
           "\n## Retry Escalation\n\n- **Timestamp:** %s\n- **Attempts:** %d\n- **Assignee:** %s\n- **Last error:** see agent output above\n",
           ts,
           retryCount,
           agentName,
       )
   }
   ```

7. **Run `make test` after code changes, before touching tests**

   ```bash
   cd task/controller && make test
   ```

   Expected: tests likely compile fine — they call only `WriteResult` (public API), not `applyRetryCounter` / `triggerEscalationSection` / `escalationSection` directly. Test failures will be assertion failures (phase / assignee values), not compile errors. If any test does call an internal helper directly, update those call sites.

8. **Update existing tests in `result_writer_test.go` — modified behavior**

   Read all tests carefully (full file read already done in step 1). Apply these changes:

   a. **Context `retry counter` — "escalates to human_review when retry_count (set by executor) meets default max_retries"**

      The disk has no `phase` field, agent sends `phase: ai_review`. With new behavior, phase is NOT overwritten to `human_review` — it stays `ai_review`. Update assertions:
      - Remove: `Expect(s).To(ContainSubstring("phase: human_review"))`
      - Add: `Expect(s).To(ContainSubstring("phase: ai_review"))`
      - Add: `Expect(s).NotTo(ContainSubstring("assignee: claude"))` (assignee cleared)
      - Keep: `Expect(s).To(ContainSubstring("## Retry Escalation"))`, `Expect(s).To(ContainSubstring("**Attempts:** 3"))`, `Expect(s).To(ContainSubstring("**Assignee:** claude"))` (section text still records who burned budget)

   b. **Context `retry counter` — "escalates immediately when retry_count (set by executor) meets max_retries 0"**

      Same pattern: disk has no phase, agent sends `phase: ai_review`. Update assertions:
      - Remove: `phase: human_review` assertion (if present)
      - Add: `phase: ai_review` assertion
      - Add: assignee cleared assertion

   c. **Context `trigger_count cap escalation` — "keeps phase: human_review sticky when incoming payload carries stale phase: ai_review at cap"**

      Disk has `phase: human_review`. **Verify whether the test's disk body already contains a `## Trigger Cap Escalation` section.** Under the new logic, phase restoration via `existing.Phase()` only fires when the escalation section is already present (the "already parked" signal). Two cases:
      - **If section IS present in disk setup**: restoration fires → phase stays `human_review` → assertion still holds. EXTEND with: `Expect(s).NotTo(ContainSubstring("assignee: claude"))` (assignee cleared).
      - **If section is NOT present in disk setup**: the test exercises an inconsistent legacy state ("phase: human_review but no section"). Under new logic, no restoration fires, phase merges to `ai_review` from agent. Fix by ADDING the escalation section to the disk body so the test now exercises true cap-stickiness ("already parked → stays sticky on next stale write"). Then assertions stay valid plus add `NotTo(ContainSubstring("assignee: claude"))`.

   d. **Context `trigger_count cap escalation` — "does not append duplicate Trigger Cap Escalation section on repeated writes at cap"**

      Disk has `phase: human_review`. Verify section presence in disk setup (same as 8c). If present → restoration fires → assertions hold. If absent → ADD the section to disk setup. Either way EXTEND:
      - `Expect(s).NotTo(ContainSubstring("assignee: claude"))` — assignee cleared

   e. **Context `trigger_count cap escalation` — "does not append duplicate Retry Escalation section on repeated writes at retry cap"**

      Disk has `phase: human_review` and a `## Retry Escalation` section (the test is specifically about deduplication, so the section MUST be in the setup). Phase restoration fires. EXTEND:
      - `Expect(s).NotTo(ContainSubstring("assignee: claude"))` — assignee cleared

   f. **Context `trigger_count cap escalation` — "keeps phase: human_review sticky and appends Trigger Cap Escalation even when merged frontmatter carries inherited spawn_notification=true"**

      Title says "appends" — implies section NOT present in disk setup. Under new logic, no restoration → phase merges from agent payload. Fix by ADDING the section to the disk setup (so the test now asserts "already parked stays sticky despite spawn_notification") OR update assertion to expect the agent-side phase. Recommend the former (preserves test intent). EXTEND:
      - `Expect(s).NotTo(ContainSubstring("assignee: claude"))` — assignee cleared

   g. **Context `needs_input result` — "does not increment retry_count when phase is human_review (needs_input path)"**

      EXTEND with:
      - `Expect(s).NotTo(ContainSubstring("assignee: claude"))` — needs_input clears assignee

   h. **Context `needs_input result` — "does not increment retry_count when phase is already human_review and retry_count > 0 (terminal guard)"**

      EXTEND with:
      - `Expect(s).NotTo(ContainSubstring("assignee: claude"))` — assignee remains empty

9. **Add new tests in `result_writer_test.go` — trigger cap, three phase variants (AC 2)**

   Inside Context `trigger_count cap escalation`, add:

   ```
   It("writes assignee: empty and preserves phase: ai_review at trigger cap", func() {
       // disk: phase ai_review, trigger_count 3 >= max_triggers 3, assignee claude
       // agent: phase ai_review, trigger_count 3
       // expected: phase ai_review, assignee "", escalation section records claude
   })

   It("writes assignee: empty and preserves phase: in_progress at trigger cap", func() {
       // disk: phase in_progress, trigger_count 3 >= max_triggers 3, assignee claude
       // agent: phase in_progress
       // expected: phase in_progress, assignee "", section records claude
   })

   It("writes assignee: empty and preserves phase: planning at trigger cap", func() {
       // disk: phase planning, trigger_count 3 >= max_triggers 3, assignee claude
       // agent: phase planning
       // expected: phase planning, assignee "", section records claude
   })
   ```

   For each: write a task file with the specified on-disk state, call `WriteResult`, read the written file, assert:
   - `Expect(s).To(ContainSubstring("phase: <lifecycle_stage>"))`
   - `Expect(s).NotTo(ContainSubstring("phase: human_review"))`
   - `Expect(s).NotTo(ContainSubstring("assignee: claude"))`
   - `Expect(s).To(ContainSubstring("**Assignee:** claude"))` — section text records pre-clear agent
   - `Expect(s).To(ContainSubstring("## Trigger Cap Escalation"))`

10. **Add new tests in `result_writer_test.go` — retry cap, three phase variants (AC 3)**

    Inside Context `retry counter`, add three parallel `It` cases:

    ```
    It("writes assignee: empty and preserves phase: ai_review at retry cap", func() {...})
    It("writes assignee: empty and preserves phase: in_progress at retry cap", func() {...})
    It("writes assignee: empty and preserves phase: planning at retry cap", func() {...})
    ```

    Same assertion pattern as trigger cap tests in step 9, substituting `## Retry Escalation`.

11. **Add new test — `needs_input` clears assignee (AC 1)**

    Inside Context `needs_input result`, add:

    ```
    It("clears assignee when agent emits needs_input (phase: human_review)", func() {
        // disk: phase ai_review, assignee claude
        // agent: phase human_review (needs_input)
        // expected: phase human_review, assignee "", no escalation section
    })
    ```

    Assert:
    - `phase: human_review`
    - `Expect(s).NotTo(ContainSubstring("assignee: claude"))`
    - `Expect(s).NotTo(ContainSubstring("## Retry Escalation"))`
    - `Expect(s).NotTo(ContainSubstring("## Trigger Cap Escalation"))`

12. **Add new test — stale result at already-parked task (AC 4)**

    Inside Context `trigger_count cap escalation`, add:

    ```
    It("keeps assignee empty and phase unchanged when stale result arrives at already-parked task", func() {
        existingEscalationBody := "\n## Trigger Cap Escalation\n\n- **Timestamp:** 2026-04-18T11:00:00Z\n- **Trigger count:** 3\n- **Max triggers:** 3\n- **Assignee:** claude\n- **Last agent output:** see `## Result` above\n"
        // Disk: phase ai_review, assignee "", trigger_count 3, body has escalation section
        writeTaskFile("my-task.md", "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\ntrigger_count: 3\nmax_triggers: 3\nassignee: \"\"\n---\n"+existingEscalationBody)
        // Stale agent result: phase planning (different), assignee claude (different)
        taskFile = lib.Task{
            TaskIdentifier: identifier,
            Frontmatter: lib.TaskFrontmatter{
                "task_identifier": "test-task-uuid-1234",
                "status":          "in_progress",
                "phase":           "planning",    // stale different phase
                "trigger_count":   3,
                "max_triggers":    3,
                "assignee":        "claude",      // stale assignee
            },
            Content: lib.TaskContent("## Result\nStatus: failed\n" + existingEscalationBody),
        }
        Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
        written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
        s := string(written)
        // phase must be restored to disk's ai_review (not stale planning)
        Expect(s).To(ContainSubstring("phase: ai_review"))
        Expect(s).NotTo(ContainSubstring("phase: planning"))
        // assignee must remain empty (stale claude not revived)
        Expect(s).NotTo(ContainSubstring("assignee: claude"))
        // escalation section count stays at 1
        Expect(strings.Count(s, "## Trigger Cap Escalation")).To(Equal(1))
    })
    ```

13. **Add new test — escalation section still records pre-clear assignee (AC 5)**

    This is already covered by the assertions in steps 9 and 10 (`**Assignee:** claude` in section text). Add a targeted test in Context `trigger_count cap escalation`:

    ```
    It("escalation section body records the agent name active at escalation time, not the cleared value", func() {
        // disk: assignee claude, trigger_count 3 >= max_triggers 3
        // agent: same state
        // assert: "**Assignee:** claude" is in the section text, assignee field itself is ""
    })
    ```

    Assert separately:
    - `Expect(s).To(ContainSubstring("**Assignee:** claude"))` (section text)
    - `Expect(s).NotTo(ContainSubstring("assignee: claude"))` (frontmatter field cleared)

14. **Run tests iteratively and fix failures**

    ```bash
    cd task/controller && make test
    ```

    Common failure patterns:
    - Compiler errors from old function signatures → fixed by steps 1–6
    - Assertions expecting `phase: human_review` where behavior now preserves lifecycle phase → fixed by step 8a–8b
    - Missing `assignee: ""` assertions → added in steps 8–13

15. **Check coverage for `pkg/result/` package**

    ```bash
    cd task/controller && go test -coverprofile=/tmp/result-cover.out -mod=vendor ./pkg/result/...
    go tool cover -func=/tmp/result-cover.out | grep "total:"
    ```

    Coverage must be ≥80%.

16. **Update `CHANGELOG.md` at repo root**

    If `## Unreleased` section exists, append to it. Otherwise create above the latest version header.

    Add:
    ```markdown
    - feat: clear `assignee` to empty on all escalation paths (trigger cap, retry cap, needs_input) so parked tasks surface in operator inbox by assignee filter
    - feat: preserve lifecycle phase on cap escalations — trigger-count and retry-count cap no longer overwrite phase to `human_review`; phase stays at the stage where the cap fired
    ```

17. **Run final precommit**

    ```bash
    cd task/controller && make precommit
    ```

    Must exit 0.

</requirements>

<constraints>
- The atomic-write contract from spec 006 (single git writer, gitclient mutex serialization) must not change
- The trigger-cap enforcement order is preserved: cap check runs before the spawn_notification short-circuit
- `containsEscalationSection` idempotency guard is preserved — duplicate sections are still prevented
- Escalation section text (`**Assignee:**` line) must record the agent name that was assigned AT escalation time, before `merged["assignee"] = ""`
- `phase: human_review` is still the correct value for `needs_input` escalations — only trigger cap and retry cap stop setting it
- The cap stickiness test "keeps phase: human_review sticky" MUST still pass: disk `phase: human_review` (legacy tasks) is restored by `existing.Phase()` when section is already present
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`
- Existing tests are extended, not replaced — do NOT delete any `It` block; only change assertions that are now incorrect and add new assertions and new `It` blocks
- Tests use external package `result_test` and Ginkgo/Gomega — do not switch frameworks
- `make precommit` in `task/controller` must exit 0
- Do NOT commit — dark-factory handles git
</constraints>

<verification>

Verify function signatures updated:
```bash
grep -n "applyRetryCounter\|triggerEscalationSection\|escalationSection" task/controller/pkg/result/result_writer.go
```
Expected: `applyRetryCounter` has three params including `existing lib.TaskFrontmatter`; both section functions take `agentName string`.

Verify `merged["phase"] = "human_review"` removed from cap blocks:
```bash
grep -n 'merged\["phase"\] = "human_review"' task/controller/pkg/result/result_writer.go
```
Expected: zero matches (or only if needed for other purposes — check context of any match).

Verify assignee clear is present in cap blocks:
```bash
grep -n 'merged\["assignee"\] = ""' task/controller/pkg/result/result_writer.go
```
Expected: at least two matches (trigger cap block and retry cap block) plus one for needs_input.

Verify existing phase restoration is present:
```bash
grep -n "existing.Phase()" task/controller/pkg/result/result_writer.go
```
Expected: at least two matches (one per cap block).

Verify new tests exist:
```bash
grep -nE "preserves phase: ai_review|preserves phase: in_progress|preserves phase: planning|already-parked|clears assignee when agent emits needs_input|records the agent name" task/controller/pkg/result/result_writer_test.go
```
Expected: multiple matches.

Run tests:
```bash
cd task/controller && make test
```
Expected: all tests pass.

Run coverage:
```bash
cd task/controller && go test -coverprofile=/tmp/result-cover.out -mod=vendor ./pkg/result/... && go tool cover -func=/tmp/result-cover.out | grep "total:"
```
Expected: ≥80%.

Run precommit:
```bash
cd task/controller && make precommit
```
Expected: exit 0.

</verification>
