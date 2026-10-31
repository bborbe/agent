---
status: completed
spec: [063-bug-failed-branch-publishes-stale-phase]
summary: Failed results that advanced no phase now omit the phase key, keeping parked phases intact while still publishing in-process advances; specs and changelog updated.
execution_id: agent-phase-regression-exec-261-spec-063-failed-branch-publishes-no-phase
dark-factory-version: v0.198.7
created: "2026-10-31T13:58:50Z"
queued: "2026-10-31T14:47:08Z"
started: "2026-10-31T14:47:09Z"
completed: "2026-10-31T14:52:55Z"
branch: dark-factory/bug-failed-branch-publishes-stale-phase
---

# fix(delivery): a failed result that advanced no phase publishes no phase

<summary>
- A `Failed` result that advanced no phase no longer republishes the Job-start phase, so a task parked at `human_review` while the Job was running stays parked and no second Job starts
- A `Failed` result whose Job did advance still publishes that advanced phase, so a `planning → execution` run that then fails stays at `execution`
- The `Failed` branch now behaves like the other four non-advancing branches (`Done` without `NextPhase`, `needs_input`, `in_progress`, unknown), which already publish no phase
- When a non-empty Job-start phase is dropped, a verbose log line records it, so the drop is visible in the log rather than only in git history
- Five existing specs that asserted the buggy contract are updated in place (not deleted) to assert the phase key is absent
- A new regression spec reproduces the 2026-10-30 shape using the existing Job-start `planning` fixture
- The four source comments that described the old contract are rewritten so the code and its prose agree
- A changelog entry records the behaviour change
</summary>

<objective>
Make `kafkaResultDeliverer.applyResultFrontmatter`'s `AgentStatusFailed` arm omit the `phase` key when the Job advanced no phase, exactly as the other four non-advancing branches already do, while keeping the advanced-phase publication intact. This stops a failed result from overwriting a phase another writer parked while the Job was still running.
</objective>

<context>
Read `CLAUDE.md` for project conventions and `docs/dod.md` for the repo's definition of done (it requires a `## Unreleased` CHANGELOG entry).

Read these files fully before editing:

- `delivery/result-deliverer.go` (441 lines) — the whole file. The change is in `kafkaResultDeliverer.applyResultFrontmatter`; the four comments to rewrite are in the same file.
- `delivery/result-deliverer_test.go` (1319 lines) — read the Kafka deliverer `Describe` block (starts around line 185) and in particular the `Context("phase advanced in-process within one Job", ...)` starting around line 1079, plus the five `Failed` specs at lines 240-261, 504-525, 527-547, 708-728 and 730-750.
- `docs/task-flow-and-failure-semantics.md` lines 33 and 59 — line 33 states the contract this fix restores: "Controller-side failure paths (`needs_input`, `failed`, cap-exhaustion) leave phase unchanged". Line 59 shows the executor allowlist (`planning`, `in_progress`, `ai_review`), which is why a stale `planning` re-spawns the task while a parked `human_review` does not. Do NOT edit this document — it already describes the correct behaviour.
- `CHANGELOG.md` — the top heading is `## v0.135.0`; there is no `## Unreleased` section today.

Coding guidelines (these live in the YOLO container at the paths shown, not in this repo):

- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md` — the dropped-phase log line is debug-shaped, so it must be gated behind `glog.V(2)`, matching the existing `glog.V(2).Infof` calls in `applyResultMetrics`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega style, external test package.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — `## Unreleased` entry format and prefix rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — changed-behaviour coverage.

## Why the change is safe

The published update is merged downstream by `agent-task-controller`'s `MergeFrontmatter`, which copies the on-disk values first and then overlays the incoming ones. A key absent from the payload therefore keeps its on-disk value. This is the same mechanism the four non-advancing branches already rely on (`delete(frontmatter, "phase")` appears in the `Done`-without-`NextPhase`, `needs_input`, `in_progress` and `default` arms of the same function). The `Failed` arm is the only one that does not, and it is the arm that fires on the most common non-advancing outcome: a pod crash before any phase write.

## Cross-repo ordering constraint (do not implement here)

The controller's planning-retry gate (`bborbe/agent-task-controller`, `pkg/command/planning_retry.go`, `matchesRetryGate`) currently reads the phase from the published payload. Once this deliverer stops sending a phase on a non-advancing failure, that gate would skip. The controller-side change — read the on-disk phase instead — is a **separate spec and PR** and is explicitly out of scope here. Do not add it, do not vendor a workaround, and do not change `MergeFrontmatter` (it lives in the controller repo). The controller change must land and deploy before or with this one; mention that ordering in the changelog entry.
</context>

<requirements>
1. **Fix the `AgentStatusFailed` arm of `kafkaResultDeliverer.applyResultFrontmatter`** in `delivery/result-deliverer.go` (the `case agentlib.AgentStatusFailed:` arm, around lines 324-341). The current tail of the arm is:

   ```go
   		frontmatter["status"] = "in_progress"
   		if d.advancedPhase != "" {
   			frontmatter["phase"] = d.advancedPhase
   		}
   ```

   Replace it with:

   ```go
   		frontmatter["status"] = "in_progress"
   		if d.advancedPhase != "" {
   			frontmatter["phase"] = d.advancedPhase
   			break
   		}
   		if phase, ok := frontmatter["phase"].(string); ok && phase != "" {
   			glog.V(2).Infof(
   				"task %s: dropping stale Job-start phase %q on failed result",
   				d.taskID,
   				phase,
   			)
   		}
   		delete(frontmatter, "phase")
   ```

   Behaviour this encodes:
   - `advancedPhase != ""` (the Job advanced in-process): publish that phase, exactly as today. This is the 5d case and must not regress.
   - `advancedPhase == ""` (the common pod-crash case): drop the generated (Job-start) phase and publish no `phase` key, matching the other four non-advancing arms.
   - When a non-empty phase is dropped, emit the `glog.V(2)` line with the exact format string shown. The log fires only for a non-empty string value; a missing or non-string phase is silently deleted. `d.taskID` is an `agentlib.TaskIdentifier` (a string type), so `%s` is correct.
   - Wrap the `Infof` call across lines as shown — the repo's `golines` limit is 100 columns.

   `frontmatter` is an `agentlib.TaskFrontmatter` (`map[string]interface{}`), so the type assertion and `delete` are valid without any import change; `glog` is already imported in this file.

2. **Rewrite the five comments that assert the old contract.** The code and its prose must not disagree after this change. Use the replacement text below verbatim, keeping each comment in the same voice and position as the code it describes.

   a. The `kafkaResultDeliverer` struct field comment above `advancedPhase string` (around lines 181-188). Replace the whole comment block with:

   ```go
   	// advancedPhase is the last non-terminal phase this deliverer published on a
   	// Done + NextPhase result. A deliverer is constructed per Job (with that Job's
   	// originalContent), so this is per-Job state: Agent.Run advances phases
   	// in-process with one deliverer, and the failed branch must publish the phase
   	// this Job advanced to rather than the Job-start phase the generator rebuilt
   	// from. Empty means "no advance yet"; a failed result then publishes no phase
   	// at all, so the controller's on-disk-first merge keeps the phase on disk.
   	advancedPhase string
   ```

   b. The `applyResultFrontmatter` doc comment (around lines 264-267). Replace the final two lines — "Only the AgentStatusDone branch may write phase: human_review, and only when the agent itself requested it via Result.NextPhase." — with:

   ```go
   // A phase is published only when the result owns one: the Done branch with a
   // non-empty NextPhase advances to it, and the failed branch republishes the phase
   // this Job advanced to (advancedPhase). Every other path omits the key, so the
   // controller's on-disk-first merge keeps whatever phase the task holds.
   ```

   c. The comment at the `advancedPhase` recording site inside the `AgentStatusDone` branch (around lines 288-291, "Remember the advance for the rest of this Job. Every other branch now publishes no phase … but the failed branch deliberately still publishes one …"). Replace it with:

   ```go
   		// Remember the advance for the rest of this Job. The failed branch must
   		// publish THIS phase rather than the Job-start phase the generator rebuilt
   		// from — a planning -> execution run that then fails must stay at execution.
   		// Branches that advance nothing omit the phase and let the controller's merge
   		// keep the on-disk value.
   ```

   d. The second paragraph of the `AgentStatusFailed` comment (around lines 332-337, "This branch deliberately keeps publishing its phase: the controller's planning-retry gate reads phase from the published payload, not from disk. …"). Leave the first paragraph (retry-vs-escalate / `applyTriggerCap`) untouched. Replace only the second paragraph with:

   ```go
   		// A failed result owns the phase only when this Job advanced one: publish
   		// advancedPhase so a run that moved planning -> execution and then failed
   		// stays at execution rather than rewinding to planning. When nothing
   		// advanced, publish no phase at all — like the other non-advancing branches,
   		// so the controller's on-disk-first merge keeps whatever phase the task holds
   		// (including a park written while this run was still going). The companion
   		// controller change makes its planning-retry gate read the on-disk phase.
   ```

   e. In `delivery/result-deliverer_test.go`, the 5d comment above the `Expect(publishedFrontmatterAt(1)["phase"]).To(Equal("execution"))` assertion (around lines 1166-1170). Keep the assertion itself unchanged. Replace the comment with:

   ```go
   			// The failed branch still publishes the phase this Job advanced to — not
   			// the Job-start phase the generator rebuilt from. Publishing "planning"
   			// here would rewind an execution-phase task and re-run a planning phase
   			// that already completed. (When nothing advanced, the branch publishes no
   			// phase and the controller's merge keeps the on-disk one.)
   ```

3. **Update the five existing `Failed` specs in `delivery/result-deliverer_test.go` — update, never delete.** Each currently asserts the buggy contract. For each, replace its `phase`-equality assertion with `Expect(fm).NotTo(HaveKey("phase"))`, keep its remaining `status` / `assignee` / `previous_assignee` assertions, and keep its fixture unchanged.

   a. Lines 240-261, "preserves assignee on failed result below the trigger cap (retry stays routable)": remove `Expect(fm["phase"]).To(Equal("planning"))` (line 256) **and** the now-vacuous companion `Expect(fm["phase"]).NotTo(Equal("human_review"))` (line 257). Add `Expect(fm).NotTo(HaveKey("phase"))`. Keep the `status`, `assignee` and `previous_assignee` assertions.

   b. Lines 504-525: remove `Expect(fm["phase"]).To(Equal("human_review"))` (line 522), add `Expect(fm).NotTo(HaveKey("phase"))`, keep the `status` assertion. Rename the `It` from "sets phase=human_review when failed result requests NextPhase=in_progress (NextPhase ignored)" to "publishes no phase and ignores NextPhase when a failed result requests NextPhase=in_progress" — the old name states the behaviour this change removes.

   c. Lines 527-547, "emits ## Failure body section when failed result has NextPhase set (body shape from 077 unchanged)": remove `Expect(fm["phase"]).To(Equal("human_review"))` (line 545) and add `Expect(fm).NotTo(HaveKey("phase"))`. This spec's name promises a body assertion it does not make today; add it rather than renaming. After the existing frontmatter assertions, assert the published body carries the failure section:

   ```go
   			Expect(cmdObj.Command.Data["content"]).To(ContainSubstring("## Failure"))
   ```

   This is valid: `DeliverResult` puts the generated markdown body into `agentlib.Task.Content`, and `base.ParseEvent` round-trips the task through JSON, so `cmdObj.Command.Data["content"]` is a plain `string` (verified). Keep the existing `It` name.

   d. Lines 708-728, `Context("AgentStatusFailed with incoming phase: in_progress")`: remove `Expect(fm["phase"]).To(Equal("in_progress"))` (line 722) and `Expect(fm["phase"]).NotTo(Equal("human_review"))` (line 723), add `Expect(fm).NotTo(HaveKey("phase"))`. Keep the `status`, `assignee` and `previous_assignee` assertions. Rename the `It` from "preserves phase: in_progress and assignee below the trigger cap" to "publishes no phase and keeps the assignee below the trigger cap".

   e. Lines 730-750, `Context("AgentStatusFailed with incoming phase: ai_review")`: remove `Expect(fm["phase"]).To(Equal("ai_review"))` (line 744) and `Expect(fm["phase"]).NotTo(Equal("human_review"))` (line 745), add `Expect(fm).NotTo(HaveKey("phase"))`. Keep the `status`, `assignee` and `previous_assignee` assertions. Rename the `It` from "preserves phase: ai_review and assignee below the trigger cap" to "publishes no phase and keeps the assignee below the trigger cap".

   After these five edits, `grep -c 'Expect(fm\["phase"\]).To(Equal' delivery/result-deliverer_test.go` must print **13** (it prints 18 before the change).

4. **Add the regression spec** inside the existing `Context("phase advanced in-process within one Job", ...)` (starts around line 1079), immediately after the 5d spec's closing `})` (line 1173). Reuse that Context's `jobStartContent` fixture (it seeds `phase: planning` and `assignee: hypothesis-agent`) and its `publishedFrontmatterAt` accessor. Add:

   ```go
   		It("5d2: publishes no phase on a failed result that advanced nothing", func() {
   			generator.GenerateReturns(jobStartContent, nil)

   			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
   				Status:  agentlib.AgentStatusFailed,
   				Message: "pod crash",
   			})).To(Succeed())

   			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
   			Expect(publishedFrontmatterAt(0)).NotTo(HaveKey("phase"))
   			Expect(publishedFrontmatterAt(0)["status"]).To(Equal("in_progress"))
   			Expect(publishedFrontmatterAt(0)["assignee"]).To(Equal("hypothesis-agent"))
   		})
   ```

   Reusing the `phase: planning` fixture is what makes the assertion non-vacuous: add this spec and run `go test ./delivery/ -count=1` BEFORE applying requirement 1; confirm the failure is Gomega's "not to have key", then apply the fix and re-run to green. This is the container-executable half of the spec's AC7; the pre-fix worktree reproduction itself is host-side (see constraints).

5. **Leave the 5d assertion unchanged.** `Expect(publishedFrontmatterAt(1)["phase"]).To(Equal("execution"))` must still pass. It proves the advanced phase is still published, so the suite cannot pass by simply never publishing a phase on a failure.

6. **Confirm the other four non-advancing branches are untouched.** The `Done`-without-`NextPhase`, `needs_input`, `in_progress` and `default` arms already call `delete(frontmatter, "phase")`; their existing assertions at lines ~283, ~307, ~335, ~358 and ~382 must still pass. Do not edit those branches.

7. **Add a `## Unreleased` section to `CHANGELOG.md`.** The file's top heading is `## v0.135.0`; insert `## Unreleased` directly above it (blank line before and after, matching the file's heading spacing) and add one bullet, in the style of the surrounding bullets, that says:
   - a `failed` result that advanced no phase now omits `phase` from the published payload, so a phase parked while the Job was running (e.g. `human_review`) survives the controller's merge and the task is not re-spawned;
   - a `failed` result after an in-process advance still publishes that advanced phase;
   - a `glog.V(2)` line records the dropped Job-start phase;
   - the controller's planning-retry gate must read the on-disk phase — that companion change (`bborbe/agent-task-controller`, separate PR) must land and deploy before or with this one.
   Use the `fix:` prefix. Leave every historical entry untouched (the `v0.132.5` entry's final sentence describes the pre-change contract and is a record of that release, not a current statement).

8. **Self-check before finishing.** Re-read the `AgentStatusFailed` arm and confirm the advanced-phase path is byte-for-byte equivalent to today's behaviour. Confirm every one of the five updated specs keeps at least one non-phase assertion. Confirm no spec still asserts a phase value on a `Failed` result that advanced nothing.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- The `Failed` branch MUST still publish `d.advancedPhase` when it is non-empty. Removing that would rewind an execution-phase task that failed and re-run a completed planning phase.
- The five existing `Failed` specs are UPDATED in place, never deleted.
- Do NOT touch `MergeFrontmatter`'s phase handling — it lives in `bborbe/agent-task-controller`, not this repo.
- Do NOT implement the companion controller change (`planning_retry.go` / `matchesRetryGate`) in this prompt. It is a separate spec and PR.
- Do NOT change `delivery/content-generator.go`, the `DeliverResult` signatures, the `fileResultDeliverer` (its `preserveDiskPhase` already keeps the on-disk phase on a failed result), or any error path.
- Do NOT change how `status` is written on the `Failed` branch — it stays `"in_progress"`.
- Do NOT attempt the pre-fix reproduction with `git worktree add /tmp/prefix v0.135.0`: `.git` is masked inside this container, so git commands cannot run. That evidence (AC7 in the spec) is host-side and outside this prompt's scope; the spec's Verification section owns it. The in-container regression evidence is the new spec failing before the fix and passing after; the `test "$(grep -c …)" = "13"` count is the in-container evidence for the five updates.
- Scope: `delivery/result-deliverer.go`, `delivery/result-deliverer_test.go`, `CHANGELOG.md`. Nothing else.
- Do not add a new dependency; `glog`, `ParseMarkdownFrontmatter`, `SetFrontmatterField` and the deliverer types already exist.
- This is a single-module repo (`go.mod` at the repo root); `make` runs at the repo root here, with `ROOTDIR=/workspace` because the masked `.git` leaves the Makefile's `git rev-parse` empty.
</constraints>

<verification>
ROOTDIR=/workspace make precommit
go test ./delivery/ -count=1
test "$(grep -c 'Expect(fm\["phase"\]).To(Equal' delivery/result-deliverer_test.go)" = "13"
</verification>
