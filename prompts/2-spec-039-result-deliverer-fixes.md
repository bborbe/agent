---
status: draft
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `lib/delivery/result-deliverer.go` no longer writes `phase: human_review` on `AgentStatusNeedsInput` or the default/failed branch
- On `AgentStatusNeedsInput`, the deliverer publishes `status: in_progress`, clears `assignee: ""`, and leaves `phase` at the incoming frontmatter value
- On `AgentStatusFailed`/default, the deliverer publishes `status: in_progress`, clears `assignee: ""`, and leaves `phase` unchanged
- Existing tests that assert `phase: human_review` on these paths are updated to assert `phase` unchanged and `assignee: ""`
- New tests verify the new behavior for incoming phases `planning`, `in_progress`, and `ai_review`
</summary>

<objective>
Fix `lib/delivery/result-deliverer.go` so that the `AgentStatusNeedsInput` branch (line 148-150) and the default branch (lines 161-163) publish `status: in_progress`, clear `assignee` to `""`, and leave `phase` unchanged. Only `AgentStatusDone` + `Result.NextPhase: human_review` (via `resolveNextPhase`) may write `phase: human_review`.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `lib/delivery/result-deliverer.go` — specifically lines 115-201, the `kafkaResultDeliverer.DeliverResult` method and the `resolveNextPhase` helper
- `lib/delivery/result-deliverer_test.go` — specifically the tests around lines 148-186 (`AgentStatusNeedsInput` and `AgentStatusFailed`)

**Existing code (lines 148-163):**
```go
case agentlib.AgentStatusNeedsInput:
    frontmatter["status"] = "in_progress"
    frontmatter["phase"] = "human_review"
case agentlib.AgentStatusInProgress:
    // ...
    frontmatter["status"] = "in_progress"
    // phase intentionally not modified — preserves incoming phase
default:
    frontmatter["status"] = "in_progress"
    frontmatter["phase"] = "human_review"
```

**What to change:**
- For `AgentStatusNeedsInput`: remove `frontmatter["phase"] = "human_review"` and add `frontmatter["assignee"] = ""`
- For the `default` branch: remove `frontmatter["phase"] = "human_review"` and add `frontmatter["assignee"] = ""`
- The phase field is already copied from `fmMap` at lines 126-129, so it will be preserved from the incoming frontmatter
</context>

<requirements>

1. **Update the `AgentStatusNeedsInput` case in `lib/delivery/result-deliverer.go`**:
   - Remove the line `frontmatter["phase"] = "human_review"`
   - Add `frontmatter["assignee"] = ""`

   **Old code (lines 148-150):**
   ```go
   case agentlib.AgentStatusNeedsInput:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   ```

   **New code:**
   ```go
   case agentlib.AgentStatusNeedsInput:
       frontmatter["status"] = "in_progress"
       frontmatter["assignee"] = ""
       // phase is preserved from incoming frontmatter (already copied from fmMap above)
   ```

2. **Update the `default` branch in `lib/delivery/result-deliverer.go`**:
   - Remove the line `frontmatter["phase"] = "human_review"`
   - Add `frontmatter["assignee"] = ""`

   **Old code (lines 161-163):**
   ```go
   default:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   ```

   **New code:**
   ```go
   default:
       frontmatter["status"] = "in_progress"
       frontmatter["assignee"] = ""
       // phase is preserved from incoming frontmatter (already copied from fmMap above)
   ```

3. **Update the existing `AgentStatusNeedsInput` test in `lib/delivery/result-deliverer_test.go`**:
   - The test `"publishes needs_input result with phase=human_review"` at line 168-186 currently asserts `Expect(fm["phase"]).To(Equal("human_review"))`
   - Update this test to assert:
     - `fm["phase"]` equals the incoming task's phase (planning/in_progress/ai_review depending on the originalContent)
     - `fm["assignee"]` is empty
     - `fm["status"]` equals `"in_progress"`
     - `fm["phase"]` does NOT equal `"human_review"`

   **Old test (lines 168-186):**
   ```go
   It("publishes needs_input result with phase=human_review", func() {
       generator.GenerateReturns(
           "---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
           nil,
       )
       err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
           Status:  agentlib.AgentStatusNeedsInput,
           Message: "no date range in task",
       })
       Expect(err).NotTo(HaveOccurred())
       Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
       _, cmdObj := sender.SendCommandObjectArgsForCall(0)
       frontmatter, ok := cmdObj.Command.Data["frontmatter"]
       Expect(ok).To(BeTrue())
       fm, ok := frontmatter.(map[string]interface{})
       Expect(ok).To(BeTrue())
       Expect(fm["phase"]).To(Equal("human_review"))
       Expect(fm["status"]).To(Equal("in_progress"))
   })
   ```

   **New test:**
   ```go
   It("publishes needs_input result with phase unchanged from incoming and assignee cleared", func() {
       // originalContent has phase: planning
       generator.GenerateReturns(
           "---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
           nil,
       )
       err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
           Status:  agentlib.AgentStatusNeedsInput,
           Message: "no date range in task",
       })
       Expect(err).NotTo(HaveOccurred())
       Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
       _, cmdObj := sender.SendCommandObjectArgsForCall(0)
       frontmatter, ok := cmdObj.Command.Data["frontmatter"]
       Expect(ok).To(BeTrue())
       fm, ok := frontmatter.(map[string]interface{})
       Expect(ok).To(BeTrue())
       Expect(fm["phase"]).To(Equal("planning"))
       Expect(fm["phase"]).NotTo(Equal("human_review"))
       Expect(fm["status"]).To(Equal("in_progress"))
       Expect(fm["assignee"]).To(BeEmpty())
   })
   ```

4. **Update the existing `AgentStatusFailed` test in `lib/delivery/result-deliverer_test.go`**:
   - The test `"publishes failed result with phase=human_review"` at line 148-166 currently asserts `Expect(fm["phase"]).To(Equal("human_review"))`
   - Update this test similarly to verify phase is preserved from incoming frontmatter and assignee is cleared

   **Old test (lines 148-166):**
   ```go
   It("publishes failed result with phase=human_review", func() {
       generator.GenerateReturns(
           "---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
           nil,
       )
       err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
           Status:  agentlib.AgentStatusFailed,
           Message: "task runner failed: timeout",
       })
       Expect(err).NotTo(HaveOccurred())
       Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
       _, cmdObj := sender.SendCommandObjectArgsForCall(0)
       frontmatter, ok := cmdObj.Command.Data["frontmatter"]
       Expect(ok).To(BeTrue())
       fm, ok := frontmatter.(map[string]interface{})
       Expect(ok).To(BeTrue())
       Expect(fm["phase"]).To(Equal("human_review"))
       Expect(fm["status"]).To(Equal("in_progress"))
   })
   ```

   **New test:**
   ```go
   It("publishes failed result with phase unchanged from incoming and assignee cleared", func() {
       // originalContent has phase: planning
       generator.GenerateReturns(
           "---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
           nil,
       )
       err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
           Status:  agentlib.AgentStatusFailed,
           Message: "task runner failed: timeout",
       })
       Expect(err).NotTo(HaveOccurred())
       Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
       _, cmdObj := sender.SendCommandObjectArgsForCall(0)
       frontmatter, ok := cmdObj.Command.Data["frontmatter"]
       Expect(ok).To(BeTrue())
       fm, ok := frontmatter.(map[string]interface{})
       Expect(ok).To(BeTrue())
       Expect(fm["phase"]).To(Equal("planning"))
       Expect(fm["phase"]).NotTo(Equal("human_review"))
       Expect(fm["status"]).To(Equal("in_progress"))
       Expect(fm["assignee"]).To(BeEmpty())
   })
   ```

5. **Add new test cases for incoming phases `in_progress` and `ai_review`** for both `needs_input` and `failed`:
   - Add a Context block for `AgentStatusNeedsInput` with incoming phase `in_progress`
   - Add a Context block for `AgentStatusNeedsInput` with incoming phase `ai_review`
   - Add a Context block for `AgentStatusFailed` with incoming phase `in_progress`
   - Add a Context block for `AgentStatusFailed` with incoming phase `ai_review`

6. **Update the test `"sets phase=human_review when needs_input result requests NextPhase=done (NextPhase ignored)"`** at line 444-465:
   - This test title is now misleading — `NextPhase` is ignored on `needs_input`, and we no longer write `human_review`
   - Update the test to reflect the new behavior: phase should be preserved from incoming frontmatter, assignee should be cleared

7. **Run `make test`** in the lib directory:
   ```bash
   cd lib && make test
   ```
   All tests must pass.

</requirements>

<constraints>
- Only modify the `AgentStatusNeedsInput` case and the `default` branch in `DeliverResult`
- The `AgentStatusDone` branch must remain unchanged — it continues to call `resolveNextPhase`
- The `AgentStatusInProgress` branch must remain unchanged
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: No human_review write in NeedsInput or default branches
grep -A2 'AgentStatusNeedsInput:' lib/delivery/result-deliverer.go | grep -v '//'
# Expected: no "phase.*=.*human_review"

# AC2: Assignee cleared on NeedsInput
grep -A3 'AgentStatusNeedsInput:' lib/delivery/result-deliverer.go
# Expected: assignee set to "" in NeedsInput block

# AC3: Assignee cleared on default branch
grep -A3 'default:' lib/delivery/result-deliverer.go
# Expected: assignee set to "" in default block

# AC4: Tests updated
grep -n 'NotTo(Equal("human_review"))' lib/delivery/result-deliverer_test.go
# Expected: at least 2 matches (one for needs_input, one for failed)

# AC5: Assignee empty in tests
grep -n 'BeEmpty()' lib/delivery/result-deliverer_test.go
# Expected: at least 2 matches

# AC6: All tests pass
cd lib && make test
# Expected: exit 0
```
</verification>
