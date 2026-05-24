---
status: draft
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `lib/delivery/content-generator.go` `applyStatusFrontmatter` no longer writes `phase: human_review` on `AgentStatusNeedsInput` or the default/failed branch
- On `AgentStatusNeedsInput` and `AgentStatusFailed`/default, the function sets `status: in_progress`, clears `assignee`, and preserves the existing phase from the incoming content
- Existing tests that assert `phase: human_review` on these paths are updated to assert `phase` preserved and `assignee` cleared
- New tests verify the new behavior across all three generator types (FallbackContentGenerator, PassthroughContentGenerator, SectionContentGenerator)
</summary>

<objective>
Fix `lib/delivery/content-generator.go` so that `applyStatusFrontmatter` does not write `phase: human_review` on `AgentStatusNeedsInput` or the default/failed branch. The phase field must be preserved from the existing content, and `assignee` must be cleared to `""`.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `lib/delivery/content-generator.go` — specifically the `applyStatusFrontmatter` function at lines 50-75
- `lib/delivery/content-generator_test.go` — tests for `FallbackContentGenerator` and `PassthroughContentGenerator`

**Existing code (lines 50-75):**
```go
func applyStatusFrontmatter(content string, status agentlib.AgentStatus) string {
    switch status {
    case agentlib.AgentStatusDone:
        content = SetFrontmatterField(content, "status", "completed")
        content = SetFrontmatterField(content, "phase", "done")
    case agentlib.AgentStatusNeedsInput:
        // task-level failure: agent ran cleanly but task is impossible/underspecified.
        // Route straight to human_review — retrying a semantically-wrong task wastes compute.
        content = SetFrontmatterField(content, "status", "in_progress")
        content = SetFrontmatterField(content, "phase", "human_review")
    case agentlib.AgentStatusInProgress:
        // Step-level progress save: keep status: in_progress, preserve phase from incoming task.
        content = SetFrontmatterField(content, "status", "in_progress")
        // phase intentionally not modified — preserves the agent's current phase for in-place save
    default:
        // Agent returned status: failed (or unknown). Route to human_review immediately —
        // retry is the controller's job via trigger_count / max_triggers, not a phase loop.
        content = SetFrontmatterField(content, "status", "in_progress")
        content = SetFrontmatterField(content, "phase", "human_review")
    }
    return content
}
```

**What to change:**
- For `AgentStatusNeedsInput`: remove the `phase` write and add `assignee` cleared
- For the `default` branch: remove the `phase` write and add `assignee` cleared
- Update the comments to reflect the new behavior
</context>

<requirements>

1. **Update `applyStatusFrontmatter` in `lib/delivery/content-generator.go`**:
   - For `AgentStatusNeedsInput` branch: remove `content = SetFrontmatterField(content, "phase", "human_review")` and add `content = SetFrontmatterField(content, "assignee", "")`
   - For the `default` branch: remove `content = SetFrontmatterField(content, "phase", "human_review")` and add `content = SetFrontmatterField(content, "assignee", "")`
   - Update comments to reflect that phase is preserved from existing content

   **Old code (lines 56-60):**
   ```go
   case agentlib.AgentStatusNeedsInput:
       // task-level failure: agent ran cleanly but task is impossible/underspecified.
       // Route straight to human_review — retrying a semantically-wrong task wastes compute.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "phase", "human_review")
   ```

   **New code:**
   ```go
   case agentlib.AgentStatusNeedsInput:
       // task-level failure: agent ran cleanly but task is impossible/underspecified.
       // Clear assignee so task surfaces in operator inbox; preserve phase from existing content.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "assignee", "")
       // phase is preserved from existing content — do NOT set to human_review
   ```

   **Old code (lines 67-72):**
   ```go
   default:
       // Agent returned status: failed (or unknown). Route to human_review immediately —
       // retry is the controller's job via trigger_count / max_triggers, not a phase loop.
       // The ## Failure body section carries the reason for the human reviewer.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "phase", "human_review")
   ```

   **New code:**
   ```go
   default:
       // Agent returned status: failed (or unknown). Clear assignee so task surfaces in
       // operator inbox; preserve phase from existing content. Retry is the controller's
       // job via trigger_count / max_triggers, not a phase loop.
       // The ## Failure body section carries the reason for the human reviewer.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "assignee", "")
       // phase is preserved from existing content — do NOT set to human_review
   ```

2. **Update existing tests in `lib/delivery/content-generator_test.go`**:
   - The test `"sets status=in_progress and phase=human_review for failed result with ## Failure section"` at lines 43-60 currently asserts `Expect(generated).To(ContainSubstring("phase: human_review"))`
   - Update this test to verify phase is NOT `human_review` and assignee is empty

   **Old test (lines 43-60):**
   ```go
   It(
       "sets status=in_progress and phase=human_review for failed result with ## Failure section",
       func() {
           original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Task\n\nRun a backtest.\n"
           generated, err := generator.Generate(ctx, original, agentlib.AgentResultInfo{
               Status:  agentlib.AgentStatusFailed,
               Message: "claude CLI failed: exit status 1",
           })
           Expect(err).NotTo(HaveOccurred())
           Expect(generated).To(ContainSubstring("status: in_progress"))
           Expect(generated).To(ContainSubstring("phase: human_review"))
           Expect(generated).NotTo(ContainSubstring("phase: ai_review"))
           Expect(generated).To(ContainSubstring("## Failure"))
           Expect(generated).To(ContainSubstring("claude CLI failed: exit status 1"))
           Expect(generated).To(ContainSubstring("```\n"))
           Expect(generated).NotTo(ContainSubstring("## Result"))
       },
   )
   ```

   **New test:**
   ```go
   It(
       "sets status=in_progress, clears assignee, preserves phase for failed result with ## Failure section",
       func() {
           original := "---\ntitle: My Task\nstatus: in_progress\nphase: planning\nassignee: some-agent\n---\n\n## Task\n\nRun a backtest.\n"
           generated, err := generator.Generate(ctx, original, agentlib.AgentResultInfo{
               Status:  agentlib.AgentStatusFailed,
               Message: "claude CLI failed: exit status 1",
           })
           Expect(err).NotTo(HaveOccurred())
           Expect(generated).To(ContainSubstring("status: in_progress"))
           Expect(generated).To(ContainSubstring("assignee: \"\""))
           Expect(generated).To(ContainSubstring("phase: planning"))
           Expect(generated).NotTo(ContainSubstring("phase: human_review"))
           Expect(generated).To(ContainSubstring("## Failure"))
           Expect(generated).To(ContainSubstring("claude CLI failed: exit status 1"))
           Expect(generated).To(ContainSubstring("```\n"))
           Expect(generated).NotTo(ContainSubstring("## Result"))
       },
   )
   ```

3. **Update the needs_input tests in `lib/delivery/content-generator_test.go`**:
   - The test `"keeps status=in_progress, phase=human_review, ## Result section for needs_input"` at lines 62-76
   - The test `"sets status=in_progress and phase=human_review for needs_input result"` at lines 78-88
   - Update both to verify phase is preserved from input and assignee is cleared

4. **Update tests in PassthroughContentGenerator section**:
   - The test at lines 262-280 (`"writes ## Failure with result.Message on AgentStatusFailed when Output has frontmatter"`) asserts `Expect(generated).To(ContainSubstring("phase: human_review"))`
   - Update this to verify phase is NOT `human_review` and assignee is empty

5. **Run `make test`** in the lib directory:
   ```bash
   cd lib && make test
   ```
   All tests must pass.

</requirements>

<constraints>
- Only modify `applyStatusFrontmatter` function in `lib/delivery/content-generator.go`
- The `AgentStatusDone` and `AgentStatusInProgress` branches must remain unchanged
- The `default` branch behavior for `AgentStatusFailed`/unknown should match `AgentStatusNeedsInput`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: No human_review write in NeedsInput or default branches
grep -n 'SetFrontmatterField.*phase.*human_review' lib/delivery/content-generator.go
# Expected: 0 matches

# AC2: Assignee cleared on NeedsInput and default
grep -n 'SetFrontmatterField.*assignee' lib/delivery/content-generator.go
# Expected: at least 2 matches (one for needs_input, one for default)

# AC3: Phase preserved (no phase write in NeedsInput/default)
grep -A10 'AgentStatusNeedsInput:' lib/delivery/content-generator.go | grep 'SetFrontmatterField.*phase'
# Expected: 0 matches

# AC4: Tests updated
grep -n 'phase: human_review' lib/delivery/content-generator_test.go | grep -v 'NotTo'
# Expected: 0 matches for non-Done branches

# AC5: Assignee cleared in tests
grep -n 'assignee: ""' lib/delivery/content-generator_test.go
# Expected: at least 4 matches (failed+needs_input for fallback and passthrough)

# AC6: All tests pass
cd lib && make test
# Expected: exit 0
```
</verification>
