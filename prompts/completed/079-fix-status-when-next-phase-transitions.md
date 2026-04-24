---
status: completed
summary: Updated kafkaResultDeliverer.DeliverResult to keep status:in_progress when NextPhase resolves to a non-terminal phase; updated 3 existing test assertions and added 2 new test cases (ai_review and live dev bug cde7365b); added CHANGELOG entry.
container: agent-079-fix-status-when-next-phase-transitions
dark-factory-version: v0.132.0
created: "2026-04-24T16:01:46Z"
queued: "2026-04-24T16:07:41Z"
started: "2026-04-24T16:09:13Z"
completed: "2026-04-24T16:12:20Z"
---

<summary>
- Agent-returned `status: done` with a `NextPhase` that requests a non-terminal phase (e.g. `in_progress`, `planning`, `ai_review`) now correctly keeps the task at `status: in_progress` so the controller re-triggers on the new phase
- Only `NextPhase: "done"` or empty `NextPhase` sets `status: completed` — matches the original single-phase agent semantics
- Failure and `needs_input` paths unchanged (both still route to `phase: human_review` with `status: in_progress`)
- Without this fix, multi-phase agents (hypothesis v2) stall after planning: kafka deliverer writes `status: completed` + `phase: in_progress`, controller sees "completed" and stops spawning Jobs, the in_progress phase never fires
- Live-observed on 2026-04-24 in dev task `cde7365b-0906-4556-84dc-5fc215d0739a`: planning phase emitted `{status: done, next_phase: in_progress}` → task file shows `phase: in_progress, status: completed` → no in_progress Job spawned
- Tests cover every Status × NextPhase combination, including the new transition-preservation case
</summary>

<objective>
Fix `kafkaResultDeliverer.DeliverResult` so a requested in-progress phase transition preserves `status: in_progress`. A task is only `status: completed` when its terminal phase (`done`) is reached — NOT when the agent is mid-multi-phase workflow. This unblocks every phase-dispatched agent (hypothesis v2, future orchestrators) from the post-planning stall observed in dev.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`

**Key files to read in full before editing:**

- `lib/delivery/result-deliverer.go` — contains `kafkaResultDeliverer.DeliverResult`. The relevant switch (currently around line 137 — anchor by method name, not line number):
  ```go
  switch result.Status {
  case AgentStatusDone:
      frontmatter["status"] = "completed"
      frontmatter["phase"] = resolveNextPhase(ctx, d.taskID, result.NextPhase)
  case AgentStatusNeedsInput:
      frontmatter["status"] = "in_progress"
      frontmatter["phase"] = "human_review"
  default:
      frontmatter["status"] = "in_progress"
      frontmatter["phase"] = "human_review"
  }
  ```
  Bug: the `AgentStatusDone` arm sets `status: completed` unconditionally. When `NextPhase` is `"in_progress"`, `"planning"`, `"ai_review"`, etc., the task is mid-workflow, not complete. `status` must stay `in_progress` so the controller keeps triggering as the phase changes.

- `lib/delivery/result-deliverer_test.go` — the test cases from prompt 078 (`"sets phase=in_progress when done result requests NextPhase=in_progress"`, etc.) currently assert on `phase` only, NOT on `status`. Extend them to assert `status: in_progress` for the transition cases.

- `lib/delivery/status.go` — `AgentStatusDone`, `AgentStatusFailed`, `AgentStatusNeedsInput` constants. No changes. Note: `AgentStatusFailed` does NOT have its own `case` arm in the switch — it falls through to `default:`, which correctly sets `phase: human_review` + `status: in_progress` per prompts 074/077. Do NOT add a separate `case AgentStatusFailed:` arm.

- `github.com/bborbe/vault-cli/pkg/domain.TaskPhase` — enum (`TaskPhasePlanning`, `TaskPhaseInProgress`, `TaskPhaseAIReview`, `TaskPhaseHumanReview`, `TaskPhaseDone`, `TaskPhaseTodo`). Only `TaskPhaseDone` is considered terminal for agent-requested transitions.

**Design contract (updated from prompt 078):**

| Agent `Status` | Agent `NextPhase` | Task `status:` | Task `phase:` |
|---|---|---|---|
| `done` | empty | `completed` | `done` (legacy default, unchanged) |
| `done` | `"done"` | `completed` | `done` (explicit, unchanged) |
| `done` | `"planning"` | **`in_progress`** (was: completed) | `planning` |
| `done` | `"in_progress"` | **`in_progress`** (was: completed) | `in_progress` |
| `done` | `"ai_review"` | **`in_progress`** (was: completed) | `ai_review` |
| `done` | `"human_review"` | **`in_progress`** (was: completed) | `human_review` |
| `done` | any invalid value | `completed` | `done` (invalid logged + fallback, unchanged) |
| `needs_input` | any | `in_progress` (unchanged) | `human_review` (unchanged) |
| `failed` | any | `in_progress` (unchanged) | `human_review` (unchanged) |

**Rationale:**
- A task is "completed" only when there is no more work to trigger. An agent requesting `NextPhase: in_progress` is declaring "I finished my part, now run the next phase" — that's not completion.
- The `human_review` transition IS still `status: in_progress` because it's awaiting human action (existing behavior for needs_input + failure — matches semantically).

Grep before editing:
```bash
grep -nA1 "case AgentStatusDone:" lib/delivery/result-deliverer.go
grep -n "status: completed\|frontmatter\[\"status\"\]" lib/delivery/result-deliverer.go
grep -n "frontmatter\[\"status\"\]\|NextPhase.*in_progress\|NextPhase.*planning" lib/delivery/result-deliverer_test.go | head -20
```
</context>

<requirements>

1. **Update the `AgentStatusDone` arm in `lib/delivery/result-deliverer.go` `kafkaResultDeliverer.DeliverResult`**

   Replace:
   ```go
   case AgentStatusDone:
       frontmatter["status"] = "completed"
       frontmatter["phase"] = resolveNextPhase(ctx, d.taskID, result.NextPhase)
   ```

   With:
   ```go
   case AgentStatusDone:
       resolvedPhase := resolveNextPhase(ctx, d.taskID, result.NextPhase)
       frontmatter["phase"] = resolvedPhase
       // Only mark the task completed when the resolved phase is terminal (done).
       // Requested transitions to planning/in_progress/ai_review/human_review keep
       // the task at status: in_progress so the controller re-triggers on the
       // new phase. Without this, multi-phase agents stall after their first phase.
       if resolvedPhase == "done" {
           frontmatter["status"] = "completed"
       } else {
           frontmatter["status"] = "in_progress"
       }
   ```

   Logic:
   - `resolveNextPhase` already returns `"done"` for empty `NextPhase`, `"done"` for invalid values, and the validated phase string otherwise.
   - The switch on `resolvedPhase == "done"` handles both the legacy empty-NextPhase case (status stays `completed`) and the new transition cases (status stays `in_progress`).

2. **Do NOT modify the `AgentStatusNeedsInput` or `default` arms** — they correctly set `status: in_progress` and `phase: human_review`.

3. **Do NOT modify `resolveNextPhase`** — its existing empty/invalid/valid fallback behavior is correct.

4. **Update existing tests in `lib/delivery/result-deliverer_test.go`**

   Find the test cases added by prompt 078 for NextPhase transitions (names like `"sets phase=in_progress when done result requests NextPhase=in_progress"`). For each such case that currently only asserts `frontmatter["phase"]`, add an assertion on `frontmatter["status"]`:

   - `NextPhase: "in_progress"` → `status: in_progress`
   - `NextPhase: "planning"` → `status: in_progress`
   - `NextPhase: "ai_review"` → `status: in_progress`
   - `NextPhase: "human_review"` → `status: in_progress`
   - `NextPhase: "done"` → `status: completed`
   - `NextPhase: ""` (empty) → `status: completed`
   - `NextPhase: "invalid-value"` → `status: completed` (falls back to `done`)

5. **Add one new test case** explicitly named after the live-observed bug:

   Existing tests in this file (e.g. the `"publishes failed result with phase=human_review"` case at ~line 148, and the `"sets phase=done when done result has empty NextPhase"` case at ~line 188) follow this idiom: construct a deliverer with a fake generator, call `DeliverResult`, then read `fm := <captured frontmatter map>` and assert with `Expect(fm["phase"]).To(Equal(...))` / `Expect(fm["status"]).To(Equal(...))`. Mirror that exact shape:

   ```go
   It("keeps status=in_progress when done result requests NextPhase=in_progress (live dev bug cde7365b)", func() {
       generator.GenerateReturns(
           "---\nstatus: in_progress\nphase: in_progress\n---\n\nBody.\n\n## Plan\n\n[plan content]\n",
           nil,
       )
       // (build the deliverer with the same fake producer + fake time setup the other tests use)
       err := deliverer.DeliverResult(ctx, AgentResultInfo{
           Status:    AgentStatusDone,
           Message:   "plan extracted",
           NextPhase: "in_progress",
       })
       Expect(err).NotTo(HaveOccurred())
       // Assert on the captured frontmatter map (same fm variable name as sibling tests)
       Expect(fm["phase"]).To(Equal("in_progress"))
       Expect(fm["status"]).To(Equal("in_progress"))   // ← the fix: was "completed" before
   })
   ```

6. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased`:

   ```markdown
   - fix(lib): `kafkaResultDeliverer` now keeps `status: in_progress` when an agent returns `status: done` with a `NextPhase` that requests a non-terminal phase (planning/in_progress/ai_review/human_review); only `NextPhase: done` or empty sets `status: completed` — unblocks multi-phase agents from the post-phase-1 stall (live dev bug observed on hypothesis agent task `cde7365b` 2026-04-24)
   ```

7. **Verification commands** (repo-relative)

   Must exit 0:
   ```bash
   cd lib && make precommit
   ```

   Spot checks:
   ```bash
   grep -nA5 'case AgentStatusDone:' lib/delivery/result-deliverer.go
   ```
   Must show the new branch with `if resolvedPhase == "done"` + `else` setting `status: in_progress`.

   ```bash
   grep -n 'status.*in_progress\|NextPhase.*in_progress' lib/delivery/result-deliverer_test.go | head -10
   ```
   Must show the new status assertions in the transition test cases.

</requirements>

<constraints>
- Only edit these files:
  - `lib/delivery/result-deliverer.go` (AgentStatusDone arm only)
  - `lib/delivery/result-deliverer_test.go` (extend existing tests + add one new case)
  - `CHANGELOG.md`
- Do NOT modify `resolveNextPhase` — its current semantics are correct.
- Do NOT modify the `AgentStatusNeedsInput` or `default` arms.
- Do NOT modify `content-generator.go`, `status.go`, `markdown.go`, `print.go`.
- Do NOT modify `lib/claude/` — this is a delivery-layer fix.
- Do NOT modify `task/executor/` or `task/controller/` — the controller's behavior is correct; the bug is the deliverer telling it the task is completed.
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this prompt introduces none).
- Ginkgo v2 only. External test package matches existing file conventions.
- Backward-compatible: single-phase agents (empty NextPhase) still get `status: completed` + `phase: done`.
- All existing tests must pass after the change. Tests that asserted the old `status: completed` on transition cases will fail — update them, don't delete them.
- Do NOT commit — dark-factory handles git.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify the `AgentStatusDone` arm has the new branching:
```bash
grep -nB1 -A8 'case AgentStatusDone:' lib/delivery/result-deliverer.go
```
Must show `resolvedPhase := resolveNextPhase(...)`, `frontmatter["phase"] = resolvedPhase`, and an `if resolvedPhase == "done"` ... `else` block.

Verify no lingering unconditional `status: completed` on done:
```bash
grep -nB2 -A2 '"status"\] = "completed"' lib/delivery/result-deliverer.go
```
The only `"completed"` assignment must be inside the new `if resolvedPhase == "done"` branch.

Verify tests cover status assertions:
```bash
grep -nC1 'NextPhase: *"in_progress"' lib/delivery/result-deliverer_test.go
```
Must show nearby `"in_progress"` status assertion for the transition cases.

Run focused tests (the Go test function is `TestDelivery`, not `TestSuite` — confirmed from `delivery_suite_test.go:17`):
```bash
cd lib && go test -v ./delivery/... -run TestDelivery
```
Must exit 0. PASS lines must include the new "keeps status=in_progress when done result requests NextPhase=in_progress" case.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n 'cde7365b\|keeps status\|status: in_progress when' CHANGELOG.md
```
Must show the new Unreleased entry.

Post-merge live verification (NOT part of this prompt's execution — documented for the human):
1. Tag `lib/vX.Y.Z` at the release commit (dark-factory auto-releases root only; lib submodule needs manual tag).
2. Bump trading/agent/hypothesis dependency to that version + `make buca`.
3. Repost a hypothesis task with `phase: planning` → expect planning Job to complete AND in_progress Job to spawn automatically.
</verification>
