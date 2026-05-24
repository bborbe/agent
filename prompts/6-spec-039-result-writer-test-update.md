---
status: draft
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `task/controller/pkg/result/result_writer_test.go` updated to assert `phase: unchanged` for `needs_input` instead of `phase: human_review`
- The test `"clears assignee when agent emits needs_input (phase: human_review)"` now asserts the incoming phase is preserved
- Grep audit for `"human_review"` in non-test files returns zero write-side references in `task/controller/pkg/` and `lib/delivery/`
- The result writer's line-180 `human_review` guard (read-side) remains unchanged as the safety net for legitimate `Result.NextPhase` handoffs
</summary>

<objective>
Update the `needs_input` test in `result_writer_test.go` to assert `phase: unchanged` (equal to pre-write phase from existing frontmatter) instead of `phase: human_review`. Also perform the repository-wide grep audit for `"human_review"` write references.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `task/controller/pkg/result/result_writer_test.go` — specifically the `Context("needs_input result")` tests around lines 769-854

The test at line 831 (`"clears assignee when agent emits needs_input (phase: human_review)"`) is the key one that needs updating. Per spec 039, the phase is no longer written to `human_review` on this path — only assignee is cleared.

**Grep audit scope:**
- `task/controller/pkg/` (non-test files)
- `lib/delivery/` (non-test files)

Allowed remaining references:
- The result writer's `merged["phase"] == "human_review"` guard (read-side check)
- `Result.NextPhase`-based comparison or routing
- `// human_review` explanatory comments
</context>

<requirements>

1. **Update the test `"clears assignee when agent emits needs_input (phase: human_review)"`** in `task/controller/pkg/result/result_writer_test.go`:
   - Update the test name to reflect the new behavior: `"clears assignee and preserves phase when agent emits needs_input"`
   - The test writes a task with `phase: ai_review` and the agent result with `phase: human_review`
   - After the write, verify that `phase: ai_review` is preserved (not overwritten to `human_review`)

   **Old test (lines 831-853):**
   ```go
   It("clears assignee when agent emits needs_input (phase: human_review)", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\n---\nOriginal body\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "human_review",
           },
           Content: lib.TaskContent("Needs human input.\n"),
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

   **New test:**
   ```go
   It("clears assignee and preserves phase when agent emits needs_input", func() {
       writeTaskFile(
           "my-task.md",
           "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\n---\nOriginal body\n",
       )
       taskFile = lib.Task{
           TaskIdentifier: identifier,
           Frontmatter: lib.TaskFrontmatter{
               "task_identifier": "test-task-uuid-1234",
               "status":          "in_progress",
               "phase":           "human_review",
           },
           Content: lib.TaskContent("Needs human input.\n"),
       }
       Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
       written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
       s := string(written)
       // Phase is preserved from disk (ai_review), not overwritten to human_review
       Expect(s).To(ContainSubstring("phase: ai_review"))
       Expect(s).NotTo(ContainSubstring("phase: human_review"))
       Expect(s).NotTo(ContainSubstring("\nassignee: claude"))
       Expect(s).NotTo(ContainSubstring("## Retry Escalation"))
       Expect(s).NotTo(ContainSubstring("## Trigger Cap Escalation"))
       Expect(s).To(ContainSubstring("previous_assignee: claude"))
   })
   ```

2. **Update the test `"does not increment retry_count when phase is human_review (needs_input path)"`** at line 769-800:
   - This test is still valid but the assertion comment should be updated
   - The test is testing that when the incoming result has `phase: human_review`, the result writer preserves it (since it's merged from the incoming payload)
   - The test should continue to pass as-is, but verify the comment is updated

3. **Run `make test`** in `task/controller/` to verify the result writer tests pass:
   ```bash
   cd task/controller && make test
   ```

4. **Run the grep audit for `"human_review"` write references**:
   ```bash
   grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v '_test.go'
   ```
   
   For each match, verify it is:
   - A read-side comparison (e.g., `phase == "human_review"`)
   - A `Result.NextPhase` usage
   - An explanatory comment

   Any match matching the pattern `fm["phase"] = "human_review"` or `frontmatter["phase"] = "human_review"` or `SetFrontmatterField(..., "phase", "human_review")` is a FAIL.

5. **Verify the result writer's line-180 `human_review` guard is intact**:
   ```bash
   grep -n 'phase.*==.*human_review' task/controller/pkg/result/result_writer.go
   ```
   Expected: at least one match (the guard). This is a read-side check, not a write.

</requirements>

<constraints>
- Only update the specified test in `result_writer_test.go`
- Do NOT modify the result writer's logic itself — only the test assertions
- The result writer's `human_review` guard at line 180 must remain unchanged
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Test updated
grep -A5 'clears assignee and preserves phase when agent emits needs_input' task/controller/pkg/result/result_writer_test.go
# Expected: match found

# AC2: No phase: human_review assertion for needs_input
grep -n 'phase: human_review' task/controller/pkg/result/result_writer_test.go | grep -v 'ai_review\|execution\|planning\|done'
# Expected: no assertions matching phase: human_review for needs_input path

# AC3: Grep audit - no write-side references
grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go | grep -v '//\|==\|NextPhase'
# Expected: 0 matches (only read-side or comment references)

# AC4: Result writer guard intact
grep -n 'merged\[.*phase.*\].*==.*human_review' task/controller/pkg/result/result_writer.go
# Expected: 1 match (the guard at line 180)

# AC5: All tests pass
cd task/controller && make test
# Expected: exit 0
```
</verification>