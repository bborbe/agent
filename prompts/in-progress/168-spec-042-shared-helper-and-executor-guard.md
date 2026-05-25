---
status: executing
spec: [042-update-frontmatter-executor-enforces-human-review-doctrine]
container: agent-exec-168-spec-042-shared-helper-and-executor-guard
dark-factory-version: v0.173.0
created: "2026-05-26T00:00:00Z"
queued: "2026-05-25T22:35:21Z"
started: "2026-05-25T23:06:30Z"
completed: "2026-05-25T22:54:00Z"
branch: dark-factory/update-frontmatter-executor-enforces-human-review-doctrine
lastFailReason: 'validate completion report: completion report status: partial'
cancelled: "2026-05-25T23:04:01Z"
---

<summary>
- Extracts the `phase == "human_review"` assignee-clear inline guard from the result writer into a shared helper inside the `result` package
- Wires the partial-update executor (`UpdateFrontmatterCommand`) through that helper so any partial update producing `phase: human_review` clears assignee in the same atomic write
- Replaces the result writer's inline `phase == "human_review"` check with a call to the new helper — observable behavior identical
- Adds Ginkgo unit tests on the partial-update executor covering: phase-flip to human_review clears assignee, non-phase updates preserve assignee, idempotent re-clear on already-parked tasks, combined frontmatter+body verdict path
- Closes the sixth `phase: human_review` write site identified in spec 042's live 2026-05-25 prod incident
</summary>

<objective>
Make the partial-update executor enforce the spec-039 doctrine (`phase: human_review` → `assignee: ""`) inside the same atomic write that performs the merge. Centralize the doctrine in a single shared helper that both the result writer and the partial-update executor route through, eliminating the inbox-filter bypass that pr-reviewer-agent triggered in prod on 2026-05-25.
</objective>

<context>
Read `CLAUDE.md` for project conventions. Read these guides:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`

Read these project files before editing:
- `task/controller/pkg/result/result_writer.go` — owns `clearAssignee` (lines 228-235) and the inline `phase == "human_review"` guard (lines 179-182). The helper extraction target.
- `task/controller/pkg/command/task_update_frontmatter_executor.go` — `buildUpdateModifyFn` (lines 95-121). Add the helper call here after the merge loop and after the optional `bodySection` append, before `marshalFileContent`.
- `task/controller/pkg/command/task_update_frontmatter_executor_test.go` — existing Ginkgo suite. Mirror the `writeTaskFile` / `parseFrontmatter` / `buildCmdObj` patterns; new tests go inside the existing `Describe("HandleCommand", ...)` block.
- `task/controller/pkg/result/result_writer_test.go` — the existing line-180 guard tests live in `Context("needs_input result", ...)` (around lines 769+) and the spec 041 `Context("spawn_notification + human_review handoff ...")` (added by `1-spec-041-reorder-human-review-guard.md` if it has run; harmless if absent). All must keep passing after the helper extraction with zero behavioral change.
- `specs/in-progress/042-update-frontmatter-executor-enforces-human-review-doctrine.md` — full ACs.

**Live incident shape (the reproducer the new tests must cover):** On disk before: `assignee: pr-reviewer-agent`, `phase: planning`, `current_job: pr-reviewer-agent-…`, no `previous_assignee`. Agent emits `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}, Body: <## Verdict block>}`. Required post-write shape: `assignee: ""`, `previous_assignee: pr-reviewer-agent`, `phase: human_review`, `current_job` unchanged, body contains `## Verdict` block.
</context>

<requirements>

1. **Add the shared helper to `task/controller/pkg/result/result_writer.go`.**

   Add a new exported function in the same file, immediately AFTER the existing `clearAssignee` function (currently at lines 228-235). The helper's contract from spec AC#1:

   ```go
   // ClearAssigneeIfHumanReview enforces the spec-039 / spec-042 doctrine:
   // when merged frontmatter has phase == "human_review", clear assignee via
   // clearAssignee (which captures the prior assignee into previous_assignee
   // if non-empty, then sets assignee to ""). Returns the prior assignee
   // name (empty string when no clear happened OR when the prior assignee
   // was already empty). This is the single chokepoint for the human_review
   // assignee-clear invariant; both the result writer and the partial-update
   // executor (spec 042) route through here.
   func ClearAssigneeIfHumanReview(merged lib.TaskFrontmatter) string {
       if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
           return clearAssignee(merged)
       }
       return ""
   }
   ```

   Notes:
   - Exported (capital `C`) because the executor package imports it from `result`.
   - Body delegates to the existing `clearAssignee` (unexported) — do NOT inline the assignee-clear logic; the spec requires the doctrine to live in exactly one place.
   - Return value mirrors `clearAssignee`'s signature so future call sites can render escalation body sections; the spec-042 executor call site discards it.
   - The phase-key existence check (`phase, ok := merged["phase"].(string)`) preserves the existing line-180 behavior exactly: only fire when the key is present AND the value is the string `"human_review"`. Numeric/nil/missing → no-op.

2. **Replace the inline guard in `applyRetryCounter` with a call to the new helper.**

   In `task/controller/pkg/result/result_writer.go`, in `applyRetryCounter` (currently lines 150-185), find the block:

   ```go
   // needs_input: agent explicitly requested human review — clear assignee so task surfaces in operator inbox
   if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
       clearAssignee(merged) // sets previous_assignee and clears assignee
   }
   ```

   Replace it with:

   ```go
   // needs_input: agent explicitly requested human review — clear assignee so task surfaces in operator inbox.
   // Routes through ClearAssigneeIfHumanReview (spec 042) so the partial-update executor shares the same chokepoint.
   ClearAssigneeIfHumanReview(merged)
   ```

   **NOTE — coordinate with spec 041's reorder prompt:** if `1-spec-041-reorder-human-review-guard.md` has already merged, the inline `phase == "human_review"` block was MOVED to a position ABOVE the `SpawnNotification()` early return (and gained a long comment about the 2026-04-24 / 2026-05-25 incidents). Anchor by the observable code shape, NOT by a guessed line number: replace whichever single inline `if phase, ok := merged["phase"].(string); ok && phase == "human_review"` block exists in `applyRetryCounter`, in whatever position it lives. There must be exactly ONE such inline block before this prompt and ZERO after. Do NOT delete the surrounding comment about the 2026-04-24 / 2026-05-25 incidents — preserve it verbatim above the new helper call.

   Verify with: `grep -n 'phase == "human_review"' task/controller/pkg/result/result_writer.go` returns ZERO matches in the production file after the edit (matches inside test files are fine).

3. **Wire the helper into `buildUpdateModifyFn` in `task/controller/pkg/command/task_update_frontmatter_executor.go`.**

   The current function body (lines 100-120) returns the closure:

   ```go
   return func(current []byte) ([]byte, error) {
       frontmatterStr, err := result.ExtractFrontmatter(ctx, current)
       if err != nil { ... }
       body, err := result.ExtractBody(ctx, current)
       if err != nil { ... }
       fm, err := parseTaskFrontmatter(frontmatterStr)
       if err != nil { ... }
       for k, v := range updates {
           fm[k] = v
       }
       if bodySection != nil {
           body = delivery.ReplaceOrAppendSection(body, bodySection.Heading, bodySection.Section)
       }
       return marshalFileContent(ctx, fm, body)
   }
   ```

   Insert ONE new line AFTER `if bodySection != nil { ... }` and BEFORE `return marshalFileContent(...)`:

   ```go
       // spec 042: enforce phase: human_review → assignee: "" doctrine on the
       // merged frontmatter in the same atomic write. No-op when the merge
       // does not produce phase: human_review.
       result.ClearAssigneeIfHumanReview(fm)
   ```

   `fm` is the local `lib.TaskFrontmatter` map produced by `parseTaskFrontmatter`; the helper mutates it in place. Discard the returned name — the partial-update executor does not render an escalation body section (per spec Desired Behavior #5).

   Verify the `result` package import is already present at the top of the file (it is — line 23: `"github.com/bborbe/agent/task/controller/pkg/result"`). No new imports required.

4. **Add four new Ginkgo `Context` blocks to `task/controller/pkg/command/task_update_frontmatter_executor_test.go`.**

   Place all four inside the existing `Describe("HandleCommand", ...)` block, after the existing `Context("Body nil leaves body untouched", ...)`. Use the existing `writeTaskFile`, `parseFrontmatter`, `buildCmdObj`, `executor`, and `fakeGit` helpers from the suite's `BeforeEach`.

   **4a. spec 042 AC#4 — phase flip to human_review clears assignee:**

   ```go
   Context("spec 042: phase flip to human_review clears assignee", func() {
       It("sets previous_assignee and clears assignee when Updates flips phase to human_review", func() {
           taskFile := writeTaskFile(
               "task.md",
               "---\ntask_identifier: spec-042-flip-uuid\nstatus: in_progress\nphase: planning\nassignee: pr-reviewer-agent\ncurrent_job: pr-reviewer-agent-e323cc47\n---\nbody\n",
           )
           cmd := buildCmdObj(task.UpdateFrontmatterCommand{
               TaskIdentifier: lib.TaskIdentifier("spec-042-flip-uuid"),
               Updates: lib.TaskFrontmatter{
                   "phase": "human_review",
               },
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmd)
           Expect(err).NotTo(HaveOccurred())
           fm := parseFrontmatter(taskFile)
           Expect(fm["phase"]).To(Equal("human_review"))
           Expect(fm["assignee"]).To(Equal(""))
           Expect(fm["previous_assignee"]).To(Equal("pr-reviewer-agent"))
           Expect(fm["current_job"]).To(Equal("pr-reviewer-agent-e323cc47"))
           Expect(fm["status"]).To(Equal("in_progress"))
       })
   })
   ```

   **4b. spec 042 AC#5 — non-phase updates preserve assignee:**

   ```go
   Context("spec 042: non-phase updates leave assignee untouched", func() {
       It("does not clear assignee and does not add previous_assignee when Updates contains no phase key", func() {
           taskFile := writeTaskFile(
               "task.md",
               "---\ntask_identifier: spec-042-nonphase-uuid\nstatus: in_progress\nphase: in_progress\nassignee: backtest-agent\n---\nbody\n",
           )
           cmd := buildCmdObj(task.UpdateFrontmatterCommand{
               TaskIdentifier: lib.TaskIdentifier("spec-042-nonphase-uuid"),
               Updates: lib.TaskFrontmatter{
                   "progress": "50%",
               },
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmd)
           Expect(err).NotTo(HaveOccurred())
           fm := parseFrontmatter(taskFile)
           Expect(fm["assignee"]).To(Equal("backtest-agent"))
           Expect(fm["phase"]).To(Equal("in_progress"))
           Expect(fm["progress"]).To(Equal("50%"))
           _, hasPrev := fm["previous_assignee"]
           Expect(hasPrev).To(BeFalse(), "non-phase update must not add previous_assignee")
       })
   })
   ```

   **4c. spec 042 AC#6 — idempotent re-clear on already-parked task:**

   ```go
   Context("spec 042: idempotent re-clear on already-parked task", func() {
       It("preserves previous_assignee on a parked task when Updates is a non-phase key with Body", func() {
           taskFile := writeTaskFile(
               "task.md",
               "---\ntask_identifier: spec-042-parked-uuid\nstatus: in_progress\nphase: human_review\nassignee: \"\"\nprevious_assignee: pr-reviewer-agent\n---\n## Result\n\nok\n",
           )
           cmd := buildCmdObj(task.UpdateFrontmatterCommand{
               TaskIdentifier: lib.TaskIdentifier("spec-042-parked-uuid"),
               Updates: lib.TaskFrontmatter{
                   "verdict": "fail",
               },
               Body: &task.BodySection{
                   Heading: "## Verdict",
                   Section: "## Verdict\n\n- **Result:** fail\n",
               },
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmd)
           Expect(err).NotTo(HaveOccurred())
           fm := parseFrontmatter(taskFile)
           Expect(fm["phase"]).To(Equal("human_review"))
           Expect(fm["assignee"]).To(Equal(""))
           // previous_assignee must NOT be overwritten with empty string —
           // clearAssignee only writes previous_assignee when current assignee is non-empty.
           Expect(fm["previous_assignee"]).To(Equal("pr-reviewer-agent"))
           Expect(fm["verdict"]).To(Equal("fail"))
           content, err := os.ReadFile(taskFile) // #nosec G304 -- test helper
           Expect(err).NotTo(HaveOccurred())
           Expect(string(content)).To(ContainSubstring("## Verdict"))
           Expect(string(content)).To(ContainSubstring("## Result"))
       })
   })
   ```

   **4d. spec 042 AC#7 — combined frontmatter+body verdict path (the live 2026-05-25 prod reproducer):**

   ```go
   Context("spec 042: prod incident reproducer (phase: human_review + Body Verdict section)", func() {
       It(
           "clears assignee, captures previous_assignee, and appends the ## Verdict body section in a single atomic write",
           func() {
               taskFile := writeTaskFile(
                   "task.md",
                   "---\ntask_identifier: spec-042-incident-uuid\nstatus: in_progress\nphase: planning\nassignee: pr-reviewer-agent\ncurrent_job: pr-reviewer-agent-e323cc47\n---\nbody\n",
               )
               verdictSection := "## Verdict\n\n- **Verdict:** fail\n- **Reason:** hallucination detected in PR diff\n"
               cmd := buildCmdObj(task.UpdateFrontmatterCommand{
                   TaskIdentifier: lib.TaskIdentifier("spec-042-incident-uuid"),
                   Updates: lib.TaskFrontmatter{
                       "phase": "human_review",
                   },
                   Body: &task.BodySection{
                       Heading: "## Verdict",
                       Section: verdictSection,
                   },
               })
               _, _, err := executor.HandleCommand(ctx, nil, cmd)
               Expect(err).NotTo(HaveOccurred())

               fm := parseFrontmatter(taskFile)
               Expect(fm["phase"]).To(Equal("human_review"))
               Expect(fm["assignee"]).To(Equal(""))
               Expect(fm["previous_assignee"]).To(Equal("pr-reviewer-agent"))

               content, err := os.ReadFile(taskFile) // #nosec G304 -- test helper
               Expect(err).NotTo(HaveOccurred())
               body := string(content)
               Expect(body).To(ContainSubstring("## Verdict"))
               Expect(body).To(ContainSubstring("hallucination detected"))
           },
       )
   })
   ```

5. **Run the controller precommit and the grep audit.**

   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0. All Ginkgo suites green (the four new partial-update tests pass; the existing result_writer line-180 / needs_input tests still pass unchanged).

   Then verify the spec-042 AC#9 grep audit from the repository root:

   ```bash
   cd ~/Documents/workspaces/agent && grep -rn 'phase.*human_review\|"phase".*human_review' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go
   ```

   Expected output: every match is one of (a) a read-side comparison (e.g. `existing.Phase() == "human_review"`), (b) a comment, (c) the body of `ClearAssigneeIfHumanReview` itself, or (d) a call site `result.ClearAssigneeIfHumanReview(...)`. No assignment-side match (`merged["phase"] = "human_review"`, `Updates["phase"] = "human_review"`, etc.) may remain in non-test code outside the helper. Enumerate matches in your verification report classifying each.

6. **Verify the controller package still vets and lints clean** (precommit already runs these; this is a fast targeted check during iteration):

   ```bash
   cd task/controller && go test ./pkg/result/... ./pkg/command/... -count=1
   ```
   Both packages green.
</requirements>

<constraints>
- Do NOT change the `task.UpdateFrontmatterCommand` Go struct shape (`TaskIdentifier`, `Updates`, `Body`) — wire format must stay identical (spec Non-goal).
- Do NOT change `clearAssignee`'s semantics — `previous_assignee` is set ONLY when current `assignee` is non-empty; spec 039 / spec 021 ACs depend on this (spec Constraint).
- Do NOT add the helper to call sites outside `task/controller/pkg/` and `lib/delivery/`. Spec 042 is doctrine-completion, not expansion (spec Non-goal).
- Do NOT introduce a feature flag, env var, or per-agent override that conditionally re-enables the unguarded merge (spec Non-goal — escape hatch on the goal is a regression).
- Do NOT reject the command when `phase: human_review` arrives via partial update; the executor must still complete the write, only adding the assignee-clear (spec Non-goal — "option 2" rejected).
- Do NOT touch `applyTriggerCap`, `applyRetryCap`, `applyRetryCounter`'s structure beyond the ONE-line guard replacement, or any escalation body-section helpers. Spec 021 escalation paths continue to call `clearAssignee` directly (spec Must-not-regress).
- Do NOT touch the `phase` allowlist in the executor (`allowedPhases`); the partial-update path was never gated by it and stays ungated (spec Constraint).
- Do NOT commit — dark-factory handles git.
- Follow project error-handling conventions (`errors.Wrapf(ctx, ...)` from `github.com/bborbe/errors`). This prompt introduces no new error paths.
- Existing tests must still pass after the change. General rule: do not delete, weaken, or rewrite assertions that remain valid under the new doctrine.
- **EXCEPTION: tests that encode pre-spec-042 behavior MUST be updated to match the new doctrine.** Spec 042 changes observable behavior of `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}}` — pre-spec-042 the executor preserved `assignee`; post-spec-042 the executor clears it via the helper. Any existing assertion that encodes the pre-spec-042 expectation (e.g. asserts `assignee` is preserved when `phase: human_review` is written) MUST be updated to the post-spec-042 expectation (`assignee: ""`, `previous_assignee: <prior>`). Known case: the `only named keys change` test in `task_update_frontmatter_executor_test.go` (around lines 129-147) asserts `fm["assignee"] == "claude"` after a `phase: human_review` update — update its `assignee` assertion from `"claude"` to `""` and add a `previous_assignee == "claude"` assertion. This is REQUIRED by AC#1, not optional.
- Context for the `Context("Body field appends a new section", ...)` test in the same file: it asserts `fm["phase"] == "human_review"` on a write where the input frontmatter has NO `assignee` key originally. After the helper runs, `clearAssignee` no-ops on `previous_assignee` (no prior value) and sets `assignee: ""`. The existing `phase` assertion holds; add a sanity assertion for the new `assignee` shape if helpful, but the existing test's intent is unchanged.
</constraints>

<verification>
```bash
# AC#1, AC#2 — helper exists and inline guard removed
grep -n 'func ClearAssigneeIfHumanReview' task/controller/pkg/result/result_writer.go
grep -n 'phase == "human_review"' task/controller/pkg/result/result_writer.go

# AC#3 — executor call site wired
grep -n 'ClearAssigneeIfHumanReview\|human_review' task/controller/pkg/command/task_update_frontmatter_executor.go

# AC#4-#7 — new tests pass
cd task/controller && go test ./pkg/command/... -run 'spec 042' -v -count=1

# AC#8 — existing result writer tests still pass
cd task/controller && go test ./pkg/result/... -count=1

# AC#9 — repo-wide grep audit
cd ~/Documents/workspaces/agent && grep -rn 'phase.*human_review\|"phase".*human_review' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go

# AC#13 — full precommit
cd task/controller && make precommit
```

Expected:
- First grep returns exactly one match (the helper declaration line).
- Second grep returns ZERO matches (inline production guard removed).
- Third grep returns at least one match — the `result.ClearAssigneeIfHumanReview(fm)` call inside `buildUpdateModifyFn`.
- Test runs are green. Grep audit shows only read-side, comments, helper-body, or helper-call matches. `make precommit` exits 0 with `ready to commit`.
</verification>
