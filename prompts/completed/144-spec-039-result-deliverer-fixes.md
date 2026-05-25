---
status: completed
spec: [039-controller-stop-setting-human-review-on-failure]
summary: 'Updated result-deliverer to clear assignee and preserve phase on AgentStatusNeedsInput/default branches instead of writing phase: human_review'
container: agent-exec-144-spec-039-result-deliverer-fixes
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
started: "2026-05-24T23:22:02Z"
completed: "2026-05-24T23:24:34Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `lib/delivery/result-deliverer.go` no longer writes `phase: human_review` on `AgentStatusNeedsInput` or the default/failed branch
- On `AgentStatusNeedsInput`, the deliverer publishes `status: in_progress`, clears `assignee: ""`, and leaves `phase` at the incoming frontmatter value
- On `AgentStatusFailed`/default, the deliverer publishes `status: in_progress`, clears `assignee: ""`, and leaves `phase` unchanged
- Stale block comment at lines 131-134 is updated to describe the new behavior
- Existing tests that assert `phase: human_review` on these paths are updated to assert `phase` unchanged and `assignee: ""`
- New tests verify the new behavior for incoming phases `in_progress` and `ai_review`
</summary>

<objective>
Fix `lib/delivery/result-deliverer.go` so that the `AgentStatusNeedsInput` branch (line 148-150) and the default branch (lines 161-163) publish `status: in_progress`, clear `assignee` to `""`, and leave `phase` unchanged. Only `AgentStatusDone` + `Result.NextPhase: human_review` (via `resolveNextPhase`) may write `phase: human_review`.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `lib/delivery/result-deliverer.go` — specifically lines 115-201, the `kafkaResultDeliverer.DeliverResult` method (including the stale comment block at lines 131-134) and the `resolveNextPhase` helper
- `lib/delivery/result-deliverer_test.go` — specifically the tests around lines 148-186 (`AgentStatusNeedsInput` and `AgentStatusFailed`) and the test at lines 444-465 that exercises `NextPhase` on `needs_input`

**Existing code (lines 131-163, including the stale comment block to rewrite):**
```go
// Set status/phase from result.Status directly. The content generator may not
// have frontmatter to update (TASK_CONTENT is body-only), so we set it explicitly.
// Failed tasks route to human_review — retry is the controller's responsibility
// via trigger_count / max_triggers, not a phase loop.
switch result.Status {
case agentlib.AgentStatusDone:
    // ... unchanged ...
case agentlib.AgentStatusNeedsInput:
    frontmatter["status"] = "in_progress"
    frontmatter["phase"] = "human_review"
case agentlib.AgentStatusInProgress:
    // ... unchanged ...
default:
    frontmatter["status"] = "in_progress"
    frontmatter["phase"] = "human_review"
}
```

**What to change:**
- Rewrite the comment block at lines 131-134 to describe the new doctrine (no controller-side `human_review` write; clear assignee instead)
- For `AgentStatusNeedsInput`: remove `frontmatter["phase"] = "human_review"` and add `frontmatter["assignee"] = ""`
- For the `default` branch: remove `frontmatter["phase"] = "human_review"` and add `frontmatter["assignee"] = ""`
- The phase field is already copied from `fmMap` at lines 126-129, so it will be preserved from the incoming frontmatter
</context>

<requirements>

1. **Rewrite the stale comment block at lines 131-134** of `lib/delivery/result-deliverer.go`:

   **Old:**
   ```go
   // Set status/phase from result.Status directly. The content generator may not
   // have frontmatter to update (TASK_CONTENT is body-only), so we set it explicitly.
   // Failed tasks route to human_review — retry is the controller's responsibility
   // via trigger_count / max_triggers, not a phase loop.
   ```

   **New:**
   ```go
   // Set status/assignee from result.Status directly. The content generator may not
   // have frontmatter to update (TASK_CONTENT is body-only), so we set it explicitly.
   // Failed and needs_input results clear assignee (operator inbox surfaces empty-assignee
   // tasks) and leave phase unchanged. Only the AgentStatusDone branch may write
   // phase: human_review, and only when the agent itself requested it via Result.NextPhase.
   // Retry of failed tasks is the controller's responsibility via trigger_count /
   // max_triggers, not a phase loop.
   ```

2. **Update the `AgentStatusNeedsInput` case in `lib/delivery/result-deliverer.go`**:

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

3. **Update the `default` branch in `lib/delivery/result-deliverer.go`**:

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

4. **Update the existing `AgentStatusNeedsInput` test in `lib/delivery/result-deliverer_test.go`** (around lines 168-186):

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
       // generator stub returns content whose frontmatter has phase: planning
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
       Expect(fm["assignee"]).To(Equal(""))
   })
   ```

   Note: the deliverer copies the incoming generator-frontmatter map into the published frontmatter, so the test verifies the phase value originally returned by the generator stub still appears (i.e. the deliverer did NOT overwrite it with `human_review`).

5. **Update the existing `AgentStatusFailed` test in `lib/delivery/result-deliverer_test.go`** (around lines 148-166):

   **Old test:**
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
       ...
       Expect(fm["phase"]).To(Equal("human_review"))
       Expect(fm["status"]).To(Equal("in_progress"))
   })
   ```

   **New test:**
   ```go
   It("publishes failed result with phase unchanged from incoming and assignee cleared", func() {
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
       Expect(fm["assignee"]).To(Equal(""))
   })
   ```

6. **Add four new test Context blocks** for incoming phases `in_progress` and `ai_review` covering both `needs_input` and `failed`. Use this exact template for each (substituting `<STATUS_CONST>`, `<PHASE>`, `<MESSAGE>`, and `<BODY_SECTION>` for each entry):

   **Template (place these inside the same `Describe` block as the tests above):**
   ```go
   Context("AgentStatusNeedsInput with incoming phase: in_progress", func() {
       It("preserves phase: in_progress and clears assignee", func() {
           generator.GenerateReturns(
               "---\nstatus: in_progress\nphase: in_progress\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
               nil,
           )
           err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
               Status:  agentlib.AgentStatusNeedsInput,
               Message: "needs info",
           })
           Expect(err).NotTo(HaveOccurred())
           _, cmdObj := sender.SendCommandObjectArgsForCall(0)
           fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
           Expect(ok).To(BeTrue())
           Expect(fm["phase"]).To(Equal("in_progress"))
           Expect(fm["phase"]).NotTo(Equal("human_review"))
           Expect(fm["status"]).To(Equal("in_progress"))
           Expect(fm["assignee"]).To(Equal(""))
       })
   })
   ```

   Produce four such Context blocks total. The four entries (substitute into the template above):
   - `AgentStatusNeedsInput` + phase `in_progress` + Result body
   - `AgentStatusNeedsInput` + phase `ai_review` + Result body
   - `AgentStatusFailed` + phase `in_progress` + `## Failure` body
   - `AgentStatusFailed` + phase `ai_review` + `## Failure` body

7. **Update the test at lines 444-465** (`"sets phase=human_review when needs_input result requests NextPhase=done (NextPhase ignored)"`):
   - CRITICAL: line 448 currently has `phase: human_review` in the generator stub's returned content. Under the new doctrine, the deliverer no longer overwrites phase, so whatever the generator returns will pass through. Change the stub's content from `phase: human_review` to `phase: planning`.
   - Rename the `It(...)` description to: `"preserves phase from incoming content and clears assignee when needs_input result requests NextPhase=done (NextPhase ignored)"`
   - Update assertions: `Expect(fm["phase"]).To(Equal("planning"))`, `Expect(fm["phase"]).NotTo(Equal("human_review"))`, `Expect(fm["assignee"]).To(Equal(""))`

   **Old test (lines 444-465):**
   ```go
   It(
       "sets phase=human_review when needs_input result requests NextPhase=done (NextPhase ignored)",
       func() {
           generator.GenerateReturns(
               "---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n",
               nil,
           )
           err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
               Status:    agentlib.AgentStatusNeedsInput,
               Message:   "missing date range",
               NextPhase: "done",
           })
           Expect(err).NotTo(HaveOccurred())
           _, cmdObj := sender.SendCommandObjectArgsForCall(0)
           frontmatter, ok := cmdObj.Command.Data["frontmatter"]
           Expect(ok).To(BeTrue())
           fm, ok := frontmatter.(map[string]interface{})
           Expect(ok).To(BeTrue())
           Expect(fm["phase"]).To(Equal("human_review"))
           Expect(fm["status"]).To(Equal("in_progress"))
       },
   )
   ```

   **New test:**
   ```go
   It(
       "preserves phase from incoming content and clears assignee when needs_input result requests NextPhase=done (NextPhase ignored)",
       func() {
           generator.GenerateReturns(
               "---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n",
               nil,
           )
           err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
               Status:    agentlib.AgentStatusNeedsInput,
               Message:   "missing date range",
               NextPhase: "done",
           })
           Expect(err).NotTo(HaveOccurred())
           _, cmdObj := sender.SendCommandObjectArgsForCall(0)
           frontmatter, ok := cmdObj.Command.Data["frontmatter"]
           Expect(ok).To(BeTrue())
           fm, ok := frontmatter.(map[string]interface{})
           Expect(ok).To(BeTrue())
           Expect(fm["phase"]).To(Equal("planning"))
           Expect(fm["phase"]).NotTo(Equal("human_review"))
           Expect(fm["status"]).To(Equal("in_progress"))
           Expect(fm["assignee"]).To(Equal(""))
       },
   )
   ```

8. **Run `make test`** in the lib directory:
   ```bash
   cd lib && make test
   ```
   All tests must pass.

</requirements>

<constraints>
- Only modify the `AgentStatusNeedsInput` case, the `default` branch, and the comment block at lines 131-134 in `DeliverResult`
- The `AgentStatusDone` branch must remain unchanged — it continues to call `resolveNextPhase`
- The `AgentStatusInProgress` branch must remain unchanged
- Existing `AgentStatusDone` tests must continue to pass unchanged — do NOT modify them
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: No human_review write in NeedsInput branch (assertion: must exit 0 i.e. no match found)
! grep -A3 'AgentStatusNeedsInput:' lib/delivery/result-deliverer.go | grep -q 'phase.*=.*"human_review"'
# Expected: exit 0 (assertion holds — no match)

# AC2: No human_review write in default branch
! grep -A3 '^	default:' lib/delivery/result-deliverer.go | grep -q 'phase.*=.*"human_review"'
# Expected: exit 0 (assertion holds — no match)

# AC3: Assignee cleared in NeedsInput branch
grep -A3 'AgentStatusNeedsInput:' lib/delivery/result-deliverer.go | grep -q 'frontmatter\["assignee"\] = ""'
# Expected: exit 0

# AC4: Assignee cleared in default branch
grep -A3 '^	default:' lib/delivery/result-deliverer.go | grep -q 'frontmatter\["assignee"\] = ""'
# Expected: exit 0

# AC5: Tests updated — at least 3 NotTo human_review (needs_input, failed, NextPhase-ignored)
[ "$(grep -c 'NotTo(Equal("human_review"))' lib/delivery/result-deliverer_test.go)" -ge 3 ]
# Expected: exit 0

# AC6: Tests assert assignee empty — at least 3
[ "$(grep -c 'fm\["assignee"\]' lib/delivery/result-deliverer_test.go)" -ge 3 ]
# Expected: exit 0

# AC7: All tests pass
cd lib && make test
# Expected: exit 0
```
</verification>
