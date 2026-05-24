---
status: completed
spec: [039-controller-stop-setting-human-review-on-failure]
summary: Cap-escalation in IncrementFrontmatterExecutor now clears assignee and preserves phase instead of setting phase=human_review
container: agent-exec-143-spec-039-increment-executor-cap-path-fix
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
started: "2026-05-24T23:20:17Z"
completed: "2026-05-24T23:21:59Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- Controller's atomic increment-executor cap path no longer writes `phase: human_review` when `trigger_count >= max_triggers`
- On cap escalation, the atomic write sets `trigger_count` and clears `assignee: ""` while preserving the existing phase
- Stale doc comment above `NewIncrementFrontmatterExecutor` is rewritten to describe the new behavior
- Existing test at line 185 that asserts `phase: human_review` at cap is updated to assert `phase` unchanged and `assignee: ""`; the `It` description is renamed to reflect the new behavior
- New test cases verify the new behavior for pre-write phases `planning`, `in_progress`, and `ai_review`
</summary>

<objective>
Fix `task_increment_frontmatter_executor.go` so that when the atomic increment crosses `trigger_count >= max_triggers`, it writes the new `trigger_count` value, clears `assignee` to `""`, and does NOT modify `phase`. The phase is preserved at whatever lifecycle stage the merged frontmatter already held.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — specifically the doc comment at line 31 and the `buildIncrementModifyFn` function around line 92-117, especially the cap-escalation block at lines 112-114
- `task/controller/pkg/command/task_increment_frontmatter_executor_test.go` — the existing `Context("phase escalation at cap")` test at lines 157-187

**Existing code (line 112-114):**
```go
if cmd.Field == "trigger_count" && newVal >= fm.MaxTriggers() {
    fm["phase"] = "human_review"
}
```

**Existing stale doc comment (line 31, above `NewIncrementFrontmatterExecutor`):**
```go
// If trigger_count reaches max_triggers the phase is escalated to human_review in the same write.
```

**What to change:** The cap escalation block must set `fm["assignee"] = ""` instead of `fm["phase"] = "human_review"`. The phase field must be left untouched. The doc comment must be rewritten to describe the new behavior.
</context>

<requirements>

1. **Update `buildIncrementModifyFn` in `task/controller/pkg/command/task_increment_frontmatter_executor.go`**:
   - In the cap-escalation block (lines 112-114), replace `fm["phase"] = "human_review"` with `fm["assignee"] = ""`
   - The phase field must NOT be modified on cap escalation
   - All other behavior in the function remains unchanged

   **Old code (lines 112-114):**
   ```go
   if cmd.Field == "trigger_count" && newVal >= fm.MaxTriggers() {
       fm["phase"] = "human_review"
   }
   ```

   **New code:**
   ```go
   if cmd.Field == "trigger_count" && newVal >= fm.MaxTriggers() {
       fm["assignee"] = ""
   }
   ```

2. **Rewrite the stale doc comment at line 31** above `NewIncrementFrontmatterExecutor`:

   **Old:**
   ```go
   // If trigger_count reaches max_triggers the phase is escalated to human_review in the same write.
   ```

   **New:**
   ```go
   // If trigger_count reaches max_triggers the assignee is cleared and phase is preserved in the same write.
   ```

3. **Update the existing test in `task/controller/pkg/command/task_increment_frontmatter_executor_test.go`** (keep the Context name `"phase escalation at cap"` unchanged — spec reference is descriptive, not literal):
   - Rename the inner `It(...)` description on line 158 from `"sets phase=human_review when trigger_count reaches max_triggers"` to `"clears assignee and preserves phase when trigger_count reaches max_triggers"`
   - The existing test asserts `Expect(fm["phase"]).To(Equal("human_review"))` at line 185 — change this to `Expect(fm["phase"]).NotTo(Equal("human_review"))`
   - Add assertion `Expect(fm["assignee"]).To(BeEmpty())` to verify assignee is cleared

   **Old test (lines 158, 183-185):**
   ```go
   It("sets phase=human_review when trigger_count reaches max_triggers", func() {
       ...
       fm = parseFrontmatter(taskFile)
       Expect(fm["trigger_count"]).To(BeNumerically("==", 2))
       Expect(fm["phase"]).To(Equal("human_review"))
   ```

   **New test:**
   ```go
   It("clears assignee and preserves phase when trigger_count reaches max_triggers", func() {
       ...
       fm = parseFrontmatter(taskFile)
       Expect(fm["trigger_count"]).To(BeNumerically("==", 2))
       Expect(fm["phase"]).NotTo(Equal("human_review"))
       Expect(fm["assignee"]).To(BeEmpty())
   ```

4. **Add new test cases for pre-write phase preservation** in the same `Context("phase escalation at cap")` block, after the existing escalation test:
   - Add a test where the pre-write phase is `planning`
   - Add a test where the pre-write phase is `in_progress`
   - Add a test where the pre-write phase is `ai_review`
   - Each test should verify that after cap escalation, the phase remains unchanged and assignee is cleared

   **Example new test structure (for `planning`; repeat for `in_progress` and `ai_review`):**
   ```go
   It("preserves phase: planning when trigger_count reaches max_triggers", func() {
       taskFile := writeTaskFile(
           "cap-planning.md",
           "---\ntask_identifier: cap-planning-uuid\ntrigger_count: 1\nmax_triggers: 2\nphase: planning\nassignee: some-agent\n---\nbody\n",
       )
       cmd := buildCmdObj(task.IncrementFrontmatterCommand{
           TaskIdentifier: lib.TaskIdentifier("cap-planning-uuid"),
           Field:          "trigger_count",
           Delta:          1,
       })
       _, _, err := executor.HandleCommand(ctx, nil, cmd)
       Expect(err).NotTo(HaveOccurred())
       fm := parseFrontmatter(taskFile)
       Expect(fm["trigger_count"]).To(BeNumerically("==", 2))
       Expect(fm["phase"]).To(Equal("planning"))
       Expect(fm["phase"]).NotTo(Equal("human_review"))
       Expect(fm["assignee"]).To(BeEmpty())
   })
   ```

5. **Run `make test`** in `task/controller/` to verify the tests pass:
   ```bash
   cd task/controller && make test
   ```
   All tests must pass, especially the updated `NewIncrementFrontmatterExecutor` suite.

</requirements>

<constraints>
- Only modify the cap-escalation block in `buildIncrementModifyFn` and the doc comment at line 31 — no other behavioral changes to the function
- The phase field must NOT be written on cap escalation — only assignee cleared
- All existing tests that do NOT involve cap escalation must continue to pass unchanged
- Do NOT modify the `buildIncrementModifyFn` function signature
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: No human_review literal anywhere in the file (the stale line-31 doc comment is gone too)
grep -n 'human_review' task/controller/pkg/command/task_increment_frontmatter_executor.go
# Expected: 0 matches

# AC2: Assignee cleared on cap
grep -n 'assignee.*=.*""' task/controller/pkg/command/task_increment_frontmatter_executor.go
# Expected: at least 1 match in the cap block

# AC3: Test It description renamed
grep -n 'clears assignee and preserves phase when trigger_count reaches max_triggers' task/controller/pkg/command/task_increment_frontmatter_executor_test.go
# Expected: 1 match

# AC4: Test asserts NotTo human_review
grep -n 'NotTo(Equal("human_review"))' task/controller/pkg/command/task_increment_frontmatter_executor_test.go
# Expected: at least 1 match

# AC5: Assignee empty assertion added
grep -n 'BeEmpty()' task/controller/pkg/command/task_increment_frontmatter_executor_test.go | grep -i assign
# Expected: at least 1 match

# AC6: All tests pass
cd task/controller && make test
# Expected: exit 0
```
</verification>
