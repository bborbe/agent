---
status: committing
spec: [039-controller-stop-setting-human-review-on-failure]
summary: 'Updated result_writer_test.go: rewrote needs_input test to post-spec-039 deliverer shape, added legitimate-handoff test, re-anchored two existing tests with new names/comments'
container: agent-exec-148-spec-039-result-writer-test-update
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
started: "2026-05-24T23:28:59Z"
completed: "2026-05-24T23:32:25Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `task/controller/pkg/result/result_writer_test.go` updated so the `needs_input` Context reflects the new doctrine: the deliverer now publishes `phase: ai_review` (or whatever the lifecycle phase was), NOT `phase: human_review`
- A new test covers the new doctrine: incoming `phase: ai_review` with `assignee: ""`, writer preserves both
- The legitimate `Result.NextPhase = human_review` handoff path remains covered by a separate, clearly named test
- The sibling tests at lines 769-800 and 802-829 are re-anchored: each now explicitly covers the legitimate-handoff path and is renamed to reflect that scenario (since the deliverer no longer emits `phase: human_review` on the `needs_input` path post-spec 039)
- Grep audit for `"human_review"` in non-test files returns zero write-side references in `task/controller/pkg/` and `lib/delivery/`
- The result writer's line-180 `human_review` guard (read-side) remains unchanged as the safety net for legitimate `Result.NextPhase` handoffs
</summary>

<objective>
Update the `needs_input` Context in `result_writer_test.go` so it reflects the post-spec-039 deliverer output (phase unchanged, assignee already empty), and split the legitimate `Result.NextPhase = human_review` handoff into its own clearly-named test. Also perform the repository-wide grep audit for `"human_review"` write references.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `task/controller/pkg/result/result_writer_test.go` — specifically the `Context("needs_input result")` block at lines 769-854. Three tests live there today:
  - Line 770: `"does not increment retry_count when phase is human_review (needs_input path)"` — incoming frontmatter has `phase: human_review` and the test asserts the file ends up with `phase: human_review` + `assignee` cleared. Under spec 039 the deliverer no longer publishes `phase: human_review` on the `needs_input` path, so this test no longer represents the `needs_input` path — it now exclusively covers the legitimate `Result.NextPhase = human_review` handoff.
  - Line 802: `"does not increment retry_count when phase is already human_review and retry_count > 0 (terminal guard)"` — same situation. The on-disk file already has `phase: human_review` and the incoming payload also carries `phase: human_review`; under spec 039 this can only happen on the legitimate-handoff path (stale repeat publish after a Done+human_review handoff). The test stays meaningful but its name and context comment must be updated.
  - Line 831: `"clears assignee when agent emits needs_input (phase: human_review)"` — the test fabricates incoming `phase: human_review`, but under spec 039 the deliverer no longer does that for `needs_input`. This test must be rewritten to use the post-spec-039 deliverer shape (incoming `phase: ai_review` with `assignee: ""` already cleared by the deliverer).

- `task/controller/pkg/result/result_writer.go` lines 122 (mergeFrontmatter call), 150-185 (`applyRetryCounter`), 180 (the `phase == "human_review"` guard that clears assignee). Note: the writer merges incoming `req.Frontmatter` over the existing frontmatter; whatever phase the deliverer publishes lands on disk unless the writer's logic overrides it. The line-180 guard only fires when the merged phase is `human_review` — under spec 039's `needs_input` path the merged phase will be the lifecycle phase (e.g. `ai_review`), so the guard does NOT fire on that path; the deliverer is responsible for clearing assignee upstream.

**Grep audit scope:**
- `task/controller/pkg/` (non-test files)
- `lib/delivery/` (non-test files)

Allowed remaining references:
- The result writer's `merged["phase"] == "human_review"` guard (read-side check)
- `Result.NextPhase`-based comparison or routing
- `// human_review` explanatory comments
</context>

<requirements>

1. **Rewrite the test at line 831** (`"clears assignee when agent emits needs_input (phase: human_review)"`) to reflect the post-spec-039 deliverer output.

   Under spec 039, the deliverer's `AgentStatusNeedsInput` branch publishes:
   - `status: in_progress`
   - `phase` unchanged (= whatever the lifecycle phase was — typically `ai_review`)
   - `assignee: ""` (already cleared by the deliverer)

   The result writer then merges this onto the existing frontmatter. The on-disk result must have `phase: ai_review` preserved and `assignee: ""`.

   **Replace** the entire `It("clears assignee when agent emits needs_input (phase: human_review)", ...)` block (lines 831-853) with the following block:

   ```go
   It("preserves phase and keeps assignee cleared when deliverer published needs_input (post-spec-039 shape)", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\n---\nOriginal body\n",
       )
       // Spec 039: the deliverer's needs_input branch publishes phase unchanged
       // (lifecycle stage from the incoming frontmatter snapshot) and clears
       // assignee to "" upstream. The result writer must preserve both.
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "ai_review",
               "assignee":        "",
           },
           Content: lib.TaskContent("Needs human input.\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       // Phase preserved from disk (ai_review), NOT overwritten to human_review.
       Expect(s).To(ContainSubstring("phase: ai_review"))
       Expect(s).NotTo(ContainSubstring("phase: human_review"))
       // Assignee cleared.
       Expect(s).NotTo(ContainSubstring("\nassignee: claude"))
       Expect(s).NotTo(ContainSubstring("## Retry Escalation"))
       Expect(s).NotTo(ContainSubstring("## Trigger Cap Escalation"))
       Expect(s).To(ContainSubstring("previous_assignee: claude"))
   })
   ```

2. **Add a new sibling test** in the same `Context("needs_input result", ...)` block immediately after the one rewritten in req #1. This test covers the legitimate `Result.NextPhase = human_review` handoff (the only path that can legitimately write `phase: human_review`):

   ```go
   It("clears assignee via line-180 guard when agent legitimately handed off via Result.NextPhase=human_review", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\n---\nOriginal body\n",
       )
       // Legitimate handoff: agent returned Done + NextPhase=human_review.
       // The deliverer resolveNextPhase wrote phase: human_review onto the
       // result. The writer's line-180 guard must then clear assignee.
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "human_review",
           },
           Content: lib.TaskContent("Please verify the strategy output.\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       Expect(s).To(ContainSubstring("phase: human_review"))
       Expect(s).NotTo(ContainSubstring("\nassignee: claude"))
       Expect(s).NotTo(ContainSubstring("## Retry Escalation"))
       Expect(s).NotTo(ContainSubstring("## Trigger Cap Escalation"))
       Expect(s).To(ContainSubstring("previous_assignee: claude"))
   })
   ```

3. **Re-anchor the test at line 770** (`"does not increment retry_count when phase is human_review (needs_input path)"`).

   The test fixture has incoming `phase: human_review`. Under spec 039 the deliverer no longer publishes that on the `needs_input` path — so this test now exclusively documents the **legitimate `Result.NextPhase = human_review` handoff path**. The fixture stays correct; only the name and comment must reflect the new anchoring.

   - Rename to: `"does not increment retry_count on Result.NextPhase=human_review handoff (legitimate handoff path)"`.
   - Update the inline comment that currently says `// phase must stay human_review — not overwritten by escalation logic` to instead say `// Legitimate handoff: agent set Result.NextPhase=human_review; phase stays as merged.`.
   - Update the inline comment that says `// no escalation section — needs_input is not an infra failure` to: `// no escalation section — this is a Done+human_review handoff, not a retry/cap path.`
   - Do NOT change the fixture frontmatter values or the assertions.

4. **Re-anchor the test at line 802** (`"does not increment retry_count when phase is already human_review and retry_count > 0 (terminal guard)"`).

   Same situation: the on-disk file already has `phase: human_review`, and the incoming payload also carries it. Under spec 039 this can only occur as a stale repeat publish after a legitimate Done+human_review handoff. The test stays valid; only the name and inline comment must be tightened.

   - Rename to: `"does not increment retry_count on stale repeat publish to a task already parked at human_review (legitimate handoff stickiness)"`.
   - Add an inline comment at the top of the It block: `// Stale repeat publish path: task already parked at human_review from a prior Result.NextPhase handoff; this publish must not bump retry_count or duplicate sections.`
   - Do NOT change fixture values or assertions.

5. **Run `make test`** in `task/controller/` to confirm the rewritten + new + re-anchored tests pass:
   ```bash
   cd task/controller && make test
   ```
   Exit code MUST be 0. This is the load-bearing safety net for the prompt — if the test command fails, the prompt is not done; fix the tests and re-run until they pass.

6. **Run the grep audit for `"human_review"` write references**:
   ```bash
   grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v '_test.go'
   ```

   For each match, verify it is:
   - A read-side comparison (e.g., `phase == "human_review"`)
   - A `Result.NextPhase` usage
   - An explanatory comment

   Any match matching the pattern `fm["phase"] = "human_review"` or `frontmatter["phase"] = "human_review"` or `SetFrontmatterField(..., "phase", "human_review")` is a FAIL.

7. **Verify the result writer's line-180 `human_review` guard is intact**:
   ```bash
   grep -n 'phase.*==.*human_review' task/controller/pkg/result/result_writer.go
   ```
   Expected: at least one match (the guard). This is a read-side check, not a write.

</requirements>

<constraints>
- Only update tests in `result_writer_test.go` per the four requirements above
- Do NOT modify the result writer's logic itself — only the test code
- The result writer's `human_review` guard at line 180 must remain unchanged
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Rewritten needs_input test present with the new doctrine wording
grep -n 'preserves phase and keeps assignee cleared when deliverer published needs_input (post-spec-039 shape)' task/controller/pkg/result/result_writer_test.go
# Expected: 1 match

# AC2: New legitimate-handoff test added
grep -n 'clears assignee via line-180 guard when agent legitimately handed off via Result.NextPhase=human_review' task/controller/pkg/result/result_writer_test.go
# Expected: 1 match

# AC3: Tests at line 770 / line 802 re-anchored
grep -n 'does not increment retry_count on Result.NextPhase=human_review handoff (legitimate handoff path)' task/controller/pkg/result/result_writer_test.go
# Expected: 1 match
grep -n 'does not increment retry_count on stale repeat publish to a task already parked at human_review (legitimate handoff stickiness)' task/controller/pkg/result/result_writer_test.go
# Expected: 1 match

# AC4: Remaining `phase: human_review` assertions in the test file are intentional and only in legitimate-handoff guard tests
grep -nE 'ContainSubstring\("phase: human_review"\)' task/controller/pkg/result/result_writer_test.go
# Expected: exactly 2 matches — one in the re-anchored test at line ~770 and one in the new legitimate-handoff test added in req #2.
# If any matches sit inside a Context/It block that mentions "needs_input" without "Result.NextPhase" handoff context, that's a FAIL.

# AC5: Grep audit - no write-side references in non-test code
grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go | grep -v '//\|==\|NextPhase'
# Expected: 0 matches (only read-side or comment references)

# AC6: Result writer guard intact
grep -n 'merged\[.*phase.*\].*==.*human_review' task/controller/pkg/result/result_writer.go
# Expected: 1 match (the guard at line 180)

# AC7: All tests pass — this is load-bearing for the prompt
cd task/controller && make test
# Expected: exit 0
```
</verification>
