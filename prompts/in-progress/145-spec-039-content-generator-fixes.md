---
status: approved
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `lib/delivery/content-generator.go` `applyStatusFrontmatter` no longer writes `phase: human_review` on `AgentStatusNeedsInput` or the default/failed branch
- On `AgentStatusNeedsInput` and `AgentStatusFailed`/default, the function sets `status: in_progress`, clears `assignee`, and preserves the existing phase from the incoming content
- Existing tests that assert `phase: human_review` on these paths are updated to assert phase preserved and assignee cleared, using the existing `ParseMarkdownFrontmatter` helper for typed assertions on the YAML map (NOT raw-string `ContainSubstring` matches against indented YAML)
- The existing `DescribeTable` block already exercises `SectionContentGenerator` for `needs_input` and `failed` (entries at lines 354+); this prompt verifies it still passes
- `AgentStatusDone` and `AgentStatusInProgress` test paths are NOT touched
</summary>

<objective>
Fix `lib/delivery/content-generator.go` so that `applyStatusFrontmatter` does not write `phase: human_review` on `AgentStatusNeedsInput` or the default/failed branch. The phase field must be preserved from the existing content, and `assignee` must be cleared.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read BEFORE writing any test assertions (mandatory):**
1. `lib/delivery/content-generator.go` — the `applyStatusFrontmatter` function at lines 50-75.
2. `lib/delivery/markdown.go` — read the `SetFrontmatterField` function in full (lines 15-43). You MUST confirm its exact serialization for empty-string values before writing any test assertion against the emitted YAML. The function uses `lines[i] = key + ": " + value`, so `SetFrontmatterField(content, "assignee", "")` emits literally `assignee: ` (key, colon, space, empty value — no quotes). This means assertions like `ContainSubstring("assignee: \"\"")` will FAIL. Tests must either use `ContainSubstring("assignee: \n")` (line is followed by a newline) or — preferred — parse the frontmatter via `ParseMarkdownFrontmatter` and assert on the returned `map[string]any`.
3. `lib/delivery/markdown.go` — also read `ParseMarkdownFrontmatter` (lines 112-136). This is the existing helper that returns `(map[string]any, body string)`. It is the parser the tests must use for typed assertions. (Note: the spec text refers to this helper as `ExtractFrontmatter` informally; the actual exported function in this codebase is named `ParseMarkdownFrontmatter`.)
4. `lib/delivery/content-generator_test.go` — read lines 1-200 to understand the existing Ginkgo structure and the existing fallback/passthrough test layout. Also read the `DescribeTable` at line 326+ (entries at lines 354+ cover `SectionContentGenerator` for `failed` and `needs_input`).

**Existing code (lines 50-75 of content-generator.go):**
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
        content = SetFrontmatterField(content, "status", "in_progress")
        // phase intentionally not modified
    default:
        // Agent returned status: failed (or unknown). Route to human_review immediately —
        // retry is the controller's job via trigger_count / max_triggers, not a phase loop.
        // The ## Failure body section carries the reason for the human reviewer.
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

   **Old code (lines 56-60, `AgentStatusNeedsInput`):**
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
       // Clear assignee so the task surfaces in the operator inbox; preserve phase from
       // existing content — phase: human_review is reserved for Result.NextPhase handoffs.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "assignee", "")
       // phase is preserved from existing content — do NOT set to human_review
   ```

   **Old code (lines 67-72, default branch):**
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
       // Agent returned status: failed (or unknown). Clear assignee so the task
       // surfaces in the operator inbox; preserve phase from existing content. Retry
       // is the controller's job via trigger_count / max_triggers, not a phase loop.
       // The ## Failure body section carries the reason for the human reviewer.
       content = SetFrontmatterField(content, "status", "in_progress")
       content = SetFrontmatterField(content, "assignee", "")
       // phase is preserved from existing content — do NOT set to human_review
   ```

2. **Update the existing failed-result test for the FallbackContentGenerator** at lines 43-60 of `lib/delivery/content-generator_test.go`. Use `ParseMarkdownFrontmatter` to parse the result and assert on the map — NOT `ContainSubstring` against the raw YAML (which is brittle to whitespace/quoting variations).

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
           fm, body := delivery.ParseMarkdownFrontmatter(generated)
           Expect(fm["status"]).To(Equal("in_progress"))
           Expect(fm["phase"]).To(Equal("planning"))
           Expect(fm["phase"]).NotTo(Equal("human_review"))
           // SetFrontmatterField emits `assignee: ` (key, colon, space, empty value, newline).
           // ParseMarkdownFrontmatter's underlying yaml.Unmarshal decodes that key with no
           // scalar into a Go nil; ParseMarkdownFrontmatter then omits nil values from the
           // returned map. So an "empty assignee" surfaces as a missing key in fm.
           _, assigneePresent := fm["assignee"]
           Expect(assigneePresent).To(BeFalse(), "assignee should be cleared (parsed as missing key when empty)")
           Expect(body).To(ContainSubstring("## Failure"))
           Expect(body).To(ContainSubstring("claude CLI failed: exit status 1"))
           Expect(body).NotTo(ContainSubstring("## Result"))
       },
   )
   ```

   Note: the `delivery.ParseMarkdownFrontmatter` reference assumes the test file imports the package as `delivery`. If the existing test file already uses `delivery.NewFallbackContentGenerator()` or similar, the import alias is already in scope — use it. If the file is internal to package `delivery` and there is no alias, call `ParseMarkdownFrontmatter` unqualified.

3. **Update the `needs_input` tests for the FallbackContentGenerator** at lines 62-88 of `lib/delivery/content-generator_test.go`. Two tests live there; both must be updated using the same `ParseMarkdownFrontmatter` pattern.

   **Worked template** (apply to BOTH the "keeps status..." test at lines 62-76 AND the "sets status..." test at lines 78-88):
   ```go
   It("clears assignee and preserves phase for needs_input result, keeps ## Result section", func() {
       original := "---\ntitle: My Task\nstatus: in_progress\nphase: ai_review\nassignee: some-agent\n---\n\n## Task\n\nRun a backtest.\n"
       generated, err := generator.Generate(ctx, original, agentlib.AgentResultInfo{
           Status:  agentlib.AgentStatusNeedsInput,
           Message: "no date range in task",
       })
       Expect(err).NotTo(HaveOccurred())
       fm, body := delivery.ParseMarkdownFrontmatter(generated)
       Expect(fm["status"]).To(Equal("in_progress"))
       Expect(fm["phase"]).To(Equal("ai_review"))
       Expect(fm["phase"]).NotTo(Equal("human_review"))
       _, assigneePresent := fm["assignee"]
       Expect(assigneePresent).To(BeFalse(), "assignee should be cleared")
       Expect(body).To(ContainSubstring("## Result"))
       Expect(body).NotTo(ContainSubstring("## Failure"))
   })
   ```

   For the second test (lines 78-88, which originally used `original := "---\ntitle: My Task\n---\n..."` with no `phase`), use a similar shape but with a different incoming phase (e.g. `phase: planning`) and assert `fm["phase"]` equals `planning`. If the original test had no phase in the original content, add one so the "preserves" claim is observable.

4. **Update PassthroughContentGenerator test at lines 262-280** (`"writes ## Failure with result.Message on AgentStatusFailed when Output has frontmatter"`):
   - Use the same `ParseMarkdownFrontmatter`-based assertions
   - Add `phase: planning` and `assignee: some-agent` to the input frontmatter so phase preservation and assignee clearing are observable
   - Assert `fm["phase"]` equals `planning`, NOT `human_review`, and the `assignee` key is absent (per the empty-serialization reasoning in requirement 2)

5. **DO NOT modify any test that exercises `AgentStatusDone` or `AgentStatusInProgress`.** Specifically:
   - The existing test at line 184 (`Expect(generated).NotTo(ContainSubstring("phase: human_review"))`) in an `AgentStatusInProgress` Context is already correct under the new doctrine — leave it untouched.
   - Do not touch the `AgentStatusDone` happy-path tests; they continue to assert `phase: done` / `status: completed`.

6. **Verify the `DescribeTable` block at line 326+** still passes after the code changes. This table has entries for `SectionContentGenerator` + `failed` (line ~354) and `SectionContentGenerator` + `needs_input` (line ~360+). These entries only assert that `result.Message` surfaces in the body — they do NOT assert on phase, so they require no edits. After the code change in requirement 1, the table must still pass; if it does not, investigate before adding new tests.

7. **Run `make test`** in the lib directory:
   ```bash
   cd lib && make test
   ```
   All tests must pass — including the unchanged `AgentStatusDone`, `AgentStatusInProgress`, and `DescribeTable` cases.

</requirements>

<constraints>
- Only modify `applyStatusFrontmatter` in `lib/delivery/content-generator.go`
- The `AgentStatusDone` and `AgentStatusInProgress` branches must remain unchanged
- Tests covering `AgentStatusDone` MUST continue to pass unchanged — do not touch them
- Tests covering `AgentStatusInProgress` (including the negative `NotTo(ContainSubstring("phase: human_review"))` at content-generator_test.go:184) MUST continue to pass unchanged — do not touch them
- All test assertions on frontmatter values MUST go through `ParseMarkdownFrontmatter`. Do NOT use `ContainSubstring` against indented YAML emitted by `SetFrontmatterField` — the emitted form for empty values is `key: ` (no quotes), which is brittle to whitespace assumptions.
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: No human_review write in NeedsInput or default branches
[ "$(grep -c 'SetFrontmatterField.*"phase".*"human_review"' lib/delivery/content-generator.go)" -eq 0 ]
# Expected: exit 0

# AC2: Assignee cleared in NeedsInput and default (2 SetFrontmatterField assignee writes total)
[ "$(grep -c 'SetFrontmatterField.*"assignee".*""' lib/delivery/content-generator.go)" -ge 2 ]
# Expected: exit 0

# AC3: No phase write inside the NeedsInput case body
! grep -A6 'case agentlib.AgentStatusNeedsInput:' lib/delivery/content-generator.go | grep -q 'SetFrontmatterField.*"phase"'
# Expected: exit 0

# AC4: Tests no longer assert phase: human_review for failed/needs_input paths (only the existing
# NotTo(ContainSubstring("phase: human_review")) lines remain — there should be at least 4 such NotTo
# assertions: fallback-failed, fallback-needs-input (x2), passthrough-failed)
[ "$(grep -c 'NotTo.*phase.*human_review\|NotTo(Equal("human_review"))' lib/delivery/content-generator_test.go)" -ge 4 ]
# Expected: exit 0

# AC5: Tests use ParseMarkdownFrontmatter for typed assertions on the new paths
grep -q 'ParseMarkdownFrontmatter' lib/delivery/content-generator_test.go
# Expected: exit 0

# AC6: All tests pass
cd lib && make test
# Expected: exit 0
```
</verification>
