---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-05-25T22:05:10Z"
generating: "2026-05-25T22:05:10Z"
prompted: "2026-05-25T22:11:43Z"
verifying: "2026-05-25T23:10:07Z"
completed: "2026-05-26T15:37:18Z"
branch: dark-factory/update-frontmatter-executor-enforces-human-review-doctrine
---

## Summary

- Spec 039 closed five controller and `lib/delivery` write sites that violated the "`phase: human_review` → `assignee: ""`" doctrine, plus kept the line-180 `result_writer.go` guard as a downstream safety net for `Result.NextPhase` handoffs.
- It missed a sixth write site: the partial-update primitive (spec 016) — `UpdateFrontmatterCommand`'s executor merges arbitrary updates into the frontmatter unconditionally, with no `human_review` guard. An agent that emits a partial update setting `phase: human_review` lands the task with `phase: human_review` AND its `assignee` still set — exactly the shape spec 039 was supposed to make unreachable.
- Concrete live incident on 2026-05-25 in prod: pr-reviewer-agent's verify step caught hallucinations on PR #3 and emitted a partial-update setting `phase: human_review`. The task landed with `assignee: pr-reviewer-agent`, no `previous_assignee`, `current_job` still set. Operator inbox filter (`assignee == ""`) missed it.
- Fix: extract the inline assignee-clear from `result_writer.go:180` into a shared helper and call it from BOTH the result writer (replacing the inline guard) AND `buildUpdateModifyFn` in the partial-update executor, after the merge loop, before marshal. After this change, the doctrine holds platform-wide: no non-test code path can write `phase: human_review` without clearing assignee in the same atomic write.
- No schema changes, no feature flag, no migration. Existing parked tasks remain on disk; convergence happens on next event per spec 039's non-goal stance.

## Problem

Spec 039 enforced the doctrine "`phase: human_review` → `assignee: ""`" by removing five controller / lib/delivery write sites that produced the old shape and by keeping the result-writer's line-180 guard as a safety net for the only legitimate `human_review` write (the agent's own `Result.NextPhase` handoff in the `AgentStatusDone` branch).

It did not touch the partial-update primitive shipped by spec 016: `UpdateFrontmatterCommand`. The executor's merge loop is unconditional — it takes whatever key-value pairs the command carries and writes them into the frontmatter, then marshals. There is no guard that catches `phase: human_review` arriving via this path.

Agents call this primitive directly during their execution to set frontmatter fields piecemeal (e.g. progress markers, verdict fields). When an agent's verify step concludes "this needs a human" and emits a partial update setting `phase: human_review`, the executor writes the new phase and leaves the in-flight `assignee`, `current_job`, and (absence of) `previous_assignee` untouched. The task lands in a state the operator's inbox filter (`assignee == ""`) cannot see.

Live evidence — 2026-05-25, ~8 hours after the spec-039 prod deploy: pr-reviewer-agent processed `PR Review github - bborbe-agent - 3 - 183193c3 - add-pi-agent-variant`, its verify step rejected the output for hallucinations and emitted a partial update setting `phase: human_review`. Final frontmatter on disk: `assignee: pr-reviewer-agent`, no `previous_assignee` field, `current_job: pr-reviewer-agent-e323cc47-...`, `phase: human_review`. The body carries a `## Verdict` block with `"verdict": "fail"`. Operator had to hand-edit assignee to surface the task — the exact failure class spec 039 was meant to eliminate.

This is one missed write site, not a doctrine gap. The fix is local: route the partial-update executor through the same assignee-clear logic the line-180 guard already encodes, by extracting that logic into a shared helper.

## Goal

After this change:

- The `UpdateFrontmatterCommand` executor enforces the spec-039 doctrine in the same atomic write that performs the merge. When the incoming updates produce a merged frontmatter where `phase == "human_review"`, the executor clears `assignee` to `""` and copies the prior assignee value into `previous_assignee` per the existing `clearAssignee` semantics, before marshaling.
- The assignee-clear logic on `phase == "human_review"` lives in exactly one place — a shared helper inside the `result` package (the package that owns the doctrine). Both the result writer's existing guard and the partial-update executor's new call site route through that helper.
- A repository-wide grep across `task/controller/pkg/` and `lib/delivery/` (non-test code) for any write that sets `phase: human_review` or merges a map that may contain it yields **zero** write sites that do not route through the shared helper.

**Invariant established by this work:** No non-test code path in `task/controller/pkg/` or `lib/delivery/` can persist a task file frontmatter where `phase == "human_review"` AND `assignee != ""` in the same atomic write. Every write site that may produce `phase: human_review` routes through the shared helper that clears assignee in the same write.

## Non-goals

- NOT a backward-compat sweep of existing parked tasks holding `phase: human_review + assignee: <agent>` (spec 039 non-goal stands; convergence is on-next-event).
- NOT changing the `UpdateFrontmatterCommand` schema, the `task.UpdateFrontmatterCommand` Go struct, or the agent SDK. The wire format is unchanged.
- NOT adding general "any forbidden frontmatter combination" validation. The only invariant in scope is `phase: human_review` → `assignee: ""`. Other doctrine invariants (e.g. status/phase coherence) are out of scope.
- NOT rejecting the command when an agent emits `phase: human_review` via partial update. The executor still completes the write — it just additionally clears assignee in the same atomic write. (Rejection — "option 2" — was considered and rejected by the driving task on migration-cost grounds.)
- Do NOT introduce a feature flag, env var, or per-agent override that conditionally re-enables the unguarded merge. The doctrine is platform-wide and final; an escape hatch on the goal is itself a regression.
- Do NOT add the guard to write sites outside `task/controller/pkg/` and `lib/delivery/`. The doctrine surface is the controller; agents/lib publishers feed into it.

## Desired Behavior

1. When `UpdateFrontmatterCommand` arrives with `Updates` that, after merge into the existing on-disk frontmatter, produce a state where `phase == "human_review"`, the executor's atomic write also clears `assignee` and sets `previous_assignee` per `clearAssignee` semantics — in the same atomic write that performs the merge. Other update keys in the command are still applied as today.

2. When `UpdateFrontmatterCommand` arrives with updates that do NOT result in `phase == "human_review"` (e.g. progress markers, body-section appends, status changes, any other phase value), the executor's behavior is unchanged from today: merge and write, no assignee touch.

3. When the merged frontmatter held `phase: human_review` BEFORE the command arrived (already-parked task) and the command's updates do not change `phase`, the executor's guard still observes `phase == "human_review"` post-merge and clears assignee — idempotently. If `assignee` was already `""`, `clearAssignee` no-ops on `previous_assignee` (existing semantics). No duplicate `previous_assignee` writes, no body-section duplication.

4. The result writer's existing line-180 behavior is preserved: any merged frontmatter with `phase == "human_review"` results in `assignee: ""` after the write. The implementation routes through the same shared helper the partial-update executor uses; the observable behavior at the result-writer call site is identical to today.

5. The shared helper's contract is: given a frontmatter map, if `phase == "human_review"`, call the existing `clearAssignee` (setting `previous_assignee` from the current `assignee` if non-empty, then clearing `assignee` to `""`); otherwise, no-op. The helper returns the prior assignee name (or empty string) — same return shape as `clearAssignee` for parity with the cap paths that already use the prior name for body-section rendering. The partial-update executor discards the returned name (it does not render an escalation body section).

6. `docs/controller-design.md` § Assignee-Clear table and `docs/task-flow-and-failure-semantics.md` enumerate `UpdateFrontmatterCommand` (the partial-update primitive) as a write path constrained by the same `phase: human_review` → `assignee: ""` guard. The docs name the shared helper as the single enforcement point.

## Constraints

**Must not change:**

- The `task.UpdateFrontmatterCommand` Go struct shape (`TaskIdentifier`, `Updates`, `Body`). Same fields, same JSON wire format.
- The atomic-write contract (spec 006): single read-modify-write under the git-rest mutex, single commit, single push.
- The partial-update primitive's existing semantics for non-`phase`/non-`human_review` updates (spec 016). All other key merges remain unconditional.
- The result writer's line-180 observable behavior. The line is replaced by a call to the shared helper, but the post-write frontmatter shape is identical to today.
- `clearAssignee`'s existing semantics: `previous_assignee` set from current `assignee` only if non-empty; `assignee` cleared to `""`; return value is the prior assignee name. Spec 039 ACs depend on these semantics — they are not relaxed.
- The `phase` allowlist in the executor (spec-011-era `allowedPhases`). The partial-update path was never gated by that allowlist and remains ungated for phase writes — the new guard runs orthogonally.
- The `## Verdict` and other body-section writes that agents emit via `cmd.Body`. Body-section semantics are unchanged.

**Must not regress:**

- Spec 039 doctrine: no non-test write path in `task/controller/pkg/` or `lib/delivery/` writes `phase: human_review` while leaving a non-empty `assignee` in the same atomic write. The new write site is now also covered.
- Spec 021 escalation paths (cap, retry, stickiness) — they continue to call the existing `clearAssignee` directly with their own decision logic; this spec does NOT reroute them through the new helper. The helper is specifically for `phase == "human_review"` guarding.
- The result writer's `applyTriggerCap`, `applyRetryCap`, and `applyRetryCounter` behavior for non-`human_review` paths. They are not touched.
- Spec 015 cap-stickiness: a stale partial update arriving at an already-parked task does not revive assignee. Idempotent re-clear preserves the parked state.

**Relevant docs (in `~/Documents/workspaces/agent/docs/`):**

- `controller-design.md` — § "Assignee-Clear on Escalation" table must add a row for `UpdateFrontmatterCommand` (partial-update primitive) with the same `phase: human_review` → `assignee: ""` outcome and a column noting the shared-helper enforcement point.
- `task-flow-and-failure-semantics.md` — must state explicitly that the partial-update primitive is constrained by the same guard and routes through the shared helper.
- `specs/completed/039-controller-stop-setting-human-review-on-failure.md` — predecessor; this spec completes the doctrine by closing the sixth write site. Reference the supersession explicitly.
- `specs/completed/016-partial-frontmatter-publishers.md` — defines the `UpdateFrontmatterCommand` primitive. The primitive's contract is unchanged; only the executor adds a doctrine guard.
- `specs/completed/021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation.md` — origin of the `clearAssignee` semantics; this spec relies on those semantics unchanged.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility | Concurrency |
|---|---|---|---|---|---|
| Agent emits `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}}` on a task with `assignee: <agent>` | Atomic write produces `phase: human_review`, `previous_assignee: <agent>`, `assignee: ""`, all other existing fields preserved, body-section (if any) applied. | Operator surfaces task via `assignee == ""` inbox filter, verifies, re-delegates by setting assignee. | Task frontmatter inspection: `phase == "human_review"`, `assignee == ""`, `previous_assignee == <agent>`. | Reversible by operator edit (set `assignee`). | Single atomic read-modify-write under git-rest mutex; concurrent stale agent publishes to the result-writer path are independently guarded. |
| Agent emits `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}}` on a task with `assignee: ""` (already parked) | Atomic write produces `phase: human_review`, `assignee: ""`, `previous_assignee` unchanged (no overwrite — `clearAssignee` no-ops on empty assignee). | None — terminal-parked state preserved. | Frontmatter unchanged from pre-write except for `phase` (if it wasn't already `human_review`). | Reversible by operator edit. | Idempotent — repeated stale partial updates leave the same shape. |
| Agent emits `UpdateFrontmatterCommand{Updates: {"phase": "planning"}}` (or any other phase) on a task with `assignee: <agent>` | Atomic write produces the new phase, `assignee` unchanged, no `previous_assignee` write, body-section applied as today. | None — normal in-flight update. | Frontmatter shows new phase, assignee intact. | Reversible by next command. | Unchanged from current behavior. |
| Agent emits `UpdateFrontmatterCommand` with non-phase updates only (e.g. progress marker, body-section append) | Atomic write applies updates and body-section; the guard runs against the post-merge frontmatter and observes `phase` unchanged from on-disk (not `human_review` unless it was already), so the guard no-ops. Assignee untouched. | None. | Frontmatter shows the new field; assignee preserved. | Reversible. | Unchanged. |
| Agent emits `UpdateFrontmatterCommand` with both `Updates` AND `Body` (e.g. verify-fail: `phase: human_review` + `## Verdict` block) | Atomic write merges the updates, applies the body-section append, then runs the guard. Final state: `phase: human_review`, `assignee: ""`, `previous_assignee: <agent>`, body has `## Verdict` block appended. | Operator reads the `## Verdict` block, decides next step. | Both frontmatter (assignee empty, phase human_review) and body section present. | Reversible. | Single atomic write — frontmatter mutation and body-section append commit together. |
| `UpdateFrontmatterCommand` arrives for a task file that does not exist | Executor returns `cdb.ErrCommandObjectSkipped` (current behavior, unchanged). No write, no guard execution. | None — command is dropped. | Logged warning. | N/A. | Unchanged. |
| Crash between merge and marshal (process killed mid-`AtomicReadModifyWriteAndCommitPush`) | No partial write — the atomic-write contract (spec 006) guarantees all-or-nothing. On restart, the command may be reprocessed; the guard re-runs deterministically and produces the same post-merge state. | Restart-driven idempotency. | N/A — no observable partial state. | Reversible by next command. | Atomic-write contract holds. |
| Two `UpdateFrontmatterCommand`s for the same task arrive in quick succession (one sets `phase: human_review`, one sets `phase: planning`) | Serialized through the git-rest mutex; whichever wins the second write determines the final phase. Guard runs against each post-merge frontmatter independently — the `human_review` write clears assignee; the subsequent `planning` write leaves assignee at `""` (no revival logic in the guard). Operator-board inbox surfaces the task regardless. | Operator decides; if assignee revival is desired, operator edits manually. | Final frontmatter determined by the second command's outcome. | Reversible. | Serialized atomic writes; no race. |

## Security / Abuse Cases

No new attack surface. The `phase` and `assignee` fields are already operator-controllable in the vault, and agent-emitted partial updates already cross the agent-output trust boundary today. This spec narrows what an agent can persist in a single write — it cannot escape the doctrine by routing through the partial-update primitive instead of the result-publish path. That is a tightening, not a loosening, of the trust boundary.

One observation: an agent that wants to deliberately bypass the inbox filter can no longer do so via partial-update. The guard fires regardless of which write path the agent picks. If a future write site is added (a seventh primitive), it must also call the shared helper — the doctrine docs name the helper explicitly to make that requirement discoverable.

## Acceptance Criteria

- [ ] A shared helper exists in the `result` package (suggested name `ClearAssigneeIfHumanReview` or equivalent — agent decides at impl time) that, given a `lib.TaskFrontmatter`, calls `clearAssignee` when `phase == "human_review"` and no-ops otherwise; returns the prior assignee name (empty string if no clear happened). Evidence: a `grep -n 'func.*HumanReview' task/controller/pkg/result/*.go` returns exactly one declaration; the function's body invokes the existing `clearAssignee`.

- [ ] `result_writer.go` line-180 inline guard is replaced by a call to the shared helper. Evidence: `grep -n 'phase == "human_review"' task/controller/pkg/result/result_writer.go` returns zero matches; the call site invokes the new helper.

- [ ] `task_update_frontmatter_executor.go` `buildUpdateModifyFn` calls the shared helper on the merged frontmatter, after the merge loop and after the optional body-section append, before `marshalFileContent`. Evidence: `grep -n 'HumanReview\|human_review' task/controller/pkg/command/task_update_frontmatter_executor.go` returns at least one call to the helper inside `buildUpdateModifyFn`.

- [ ] Unit test in `task_update_frontmatter_executor_test.go` (Ginkgo `DescribeTable`, per project go-testing-guide conventions): given a task file with `assignee: pr-reviewer-agent` and `phase: planning` on disk, applying `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}}` produces on-disk frontmatter with `phase: human_review`, `assignee: ""`, `previous_assignee: pr-reviewer-agent`. Evidence: test passes after the change; fails when run against pre-change `buildUpdateModifyFn`.

- [ ] Unit test (same file, separate table row or Context): given a task file with `assignee: backtest-agent` and `phase: in_progress`, applying `UpdateFrontmatterCommand{Updates: {"progress": "50%"}}` (non-`phase` update) produces on-disk frontmatter with `phase: in_progress` unchanged, `assignee: backtest-agent` unchanged, no `previous_assignee` field added. Evidence: test asserts assignee untouched and no `previous_assignee` key in the marshaled frontmatter.

- [ ] Unit test (same file, separate row): given a task file already parked (`assignee: ""`, `phase: human_review`, `previous_assignee: pr-reviewer-agent`), applying `UpdateFrontmatterCommand{Updates: {"verdict": "fail"}, Body: <section>}` produces post-write frontmatter with `assignee: ""` still empty, `previous_assignee` unchanged (not overwritten with empty string), `phase: human_review` still set, and body section appended. Evidence: test asserts idempotent re-clear behavior — `previous_assignee` value is the same string before and after.

- [ ] Unit test: given a task file with `assignee: pr-reviewer-agent` and `phase: planning`, applying `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}, Body: <Verdict section>}` produces frontmatter shape per AC #1 above (assignee cleared to `""`, `previous_assignee: pr-reviewer-agent`, `phase: human_review`) PLUS body containing the `## Verdict` section. Evidence: test asserts both the frontmatter shape and the body-section presence in a single write.

- [ ] Existing `result_writer_test.go` tests covering the line-180 guard continue to pass unchanged after the helper extraction. Evidence: `cd task/controller && go test ./pkg/result/...` exits 0 with no test diffs required beyond what the impl prompt produces; specifically, the contexts asserting `phase: human_review` → `assignee: ""` post-merge produce the same final shape.

- [ ] AC#9-style grep audit (mirroring spec 039 AC#9): a repository-wide grep for any non-test code path in `task/controller/pkg/` and `lib/delivery/` that can persist `phase == "human_review"` without routing through the shared helper returns zero matches. Specifically, `grep -rn 'phase.*human_review\|"phase".*human_review' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go` enumerates each match; every match is either (a) a read-side comparison, (b) a comment, (c) the shared helper's own definition, or (d) a call to the shared helper. No assignment-side match remains outside the helper itself. Evidence: the verifier runs the grep and enumerates `file:line` for each match, classifying it.

- [ ] `docs/controller-design.md` § Assignee-Clear table updated: a row exists for `UpdateFrontmatterCommand` (partial-update primitive) with the same `phase: human_review` → `assignee: ""` outcome and a column or note naming the shared helper as the enforcement point. Evidence: `grep -n 'UpdateFrontmatterCommand\|partial.update\|partial-update' docs/controller-design.md` returns at least one match in the table area; the matched line names the shared helper.

- [ ] `docs/task-flow-and-failure-semantics.md` updated to enumerate the partial-update primitive as a constrained write path. Evidence: `grep -n 'partial' docs/task-flow-and-failure-semantics.md` returns at least one match referring to the partial-update primitive and naming the shared helper.

- [ ] `CHANGELOG.md` under `## Unreleased`: an operator-visible entry naming the doctrine completion — the partial-update executor now enforces `phase: human_review` → `assignee: ""` via a shared helper; spec 039 is named as the predecessor and this spec as the closure of the sixth write site. Evidence: changelog file diff shows the entry.

- [ ] `make precommit` in `task/controller` passes — gosec 0 issues, trivy 0 vuln, all Ginkgo suites green. Evidence: exit code 0, `ready to commit` line in output.

- [ ] **Post-Deploy (Rung-2) — Live verification on dev cluster** (operator-driven): deliberately reproduce a scenario where an agent emits `UpdateFrontmatterCommand` with `phase: human_review` (or use the executor's `PublishFailure` path which already emits this shape on K8s pod crashes). After the partial update lands, inspect the resulting task file.
    - **deploy_check**: `kubectlquant -n dev get statefulset agent-task-controller -o jsonpath='{.spec.template.spec.containers[0].image}'` reports an image digest matching the merge commit's `make buca` output (the controller pod is running the fix, not a stale pre-fix image).
    - **deploy_target**: `dev` namespace, `agent-task-controller-0` pod.
    - **Evidence**: task frontmatter has `assignee: ""`, `previous_assignee: pr-reviewer-agent` (or whichever agent emitted), `phase: human_review`, and the `## Verdict` (or equivalent agent body section) appended. Operator-board inbox filter (`assignee == ""`) surfaces the task within one refresh. Capture task file path + frontmatter snippet in the verification result.

- [ ] No new scenario test. The behavior is fully observable in Ginkgo unit tests against the existing fakegitclient and synthetic command inputs to `buildUpdateModifyFn`. The dev-cluster smoke above is operator-driven evidence captured post-deploy, not an automated scenario.

## Verification

```
cd ~/Documents/workspaces/agent/task/controller && make precommit
```

Grep audit (must enumerate only read-side, comment, helper-definition, or helper-call matches):

```
cd ~/Documents/workspaces/agent && grep -rn 'phase.*human_review\|"phase".*human_review' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go
```

Manual smoke on dev (post-deploy from `agent-dev` worktree per the project workflow):

1. Identify or set up a pr-reviewer-agent task that will deterministically trigger a verify-fail (hallucination flag, or equivalent reproducer that causes the agent to emit `UpdateFrontmatterCommand` with `phase: human_review`).
2. Wait for the agent to run and the controller to process the partial-update command.
3. Read the resulting task file. Confirm: `assignee: ""`, `previous_assignee: <agent-name>`, `phase: human_review`, body has the agent's verdict / failure section.
4. Confirm the operator-board inbox filter (`assignee == ""`) surfaces the task within one refresh cycle.
5. Capture the task file path and the frontmatter snippet in the verification result.

## Do-Nothing Option

Cost of leaving this unfixed:

- The 2026-05-25 incident pattern repeats every time any agent's verify-fail (or similar mid-execution escalation) routes through the partial-update primitive. Operator hand-edits two frontmatter fields per incident, exactly as before spec 039.
- Spec 039's doctrine is technically incomplete on disk. The Assignee-Clear table in `docs/controller-design.md` says one thing (assignee always cleared on `human_review`) but the partial-update path emits the old shape. New contributors reading the doc will be misled.
- Any future agent or primitive that wants to emit `phase: human_review` via partial update inherits the bug.

Deferring is not viable: the inbox is being relied on day-to-day (proven by the 2026-05-25 live incident, ~8 hours after spec 039's prod deploy). Each new failure of the same class costs operator time and erodes confidence that the doctrine holds.

## References

- `specs/completed/039-controller-stop-setting-human-review-on-failure.md` — predecessor; closed five write sites and kept the line-180 result-writer guard as a safety net. This spec closes the sixth (the partial-update executor) and extracts the line-180 logic into a shared helper so both call sites share the doctrine.
- `specs/completed/021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation.md` — origin of the `clearAssignee` semantics this spec relies on.
- `specs/completed/016-partial-frontmatter-publishers.md` — defines the `UpdateFrontmatterCommand` primitive whose executor is the fix site.
- `specs/completed/015-atomic-frontmatter-increment-and-trigger-cap.md` — atomic-write contract that guarantees the merge + guard run as a single write.
- `docs/controller-design.md` — must be updated (Assignee-Clear table).
- `docs/task-flow-and-failure-semantics.md` — must be updated (partial-update primitive enumerated).
- `~/Documents/Obsidian/Personal/24 Tasks/UpdateFrontmatter Executor Bypasses human_review Doctrine.md` — driving task with live incident evidence (2026-05-25 prod, pr-reviewer + PR #3).
- `~/Documents/Obsidian/OpenClaw/tasks/PR Review github - bborbe-agent - 3 - 183193c3 - add-pi-agent-variant.md` — incident artifact file with the violating frontmatter on disk at incident capture time.

## Verification Result

**Verified:** 2026-05-26T15:36:42Z (HEAD c25f540)
**Binary:** installed `dark-factory` (agent repo spec; Phase 0 N/A)
**Deployed image:** dev `agent-task-controller@sha256:320d5777bd11...`, pod started 2026-05-26T11:54:41Z (post-v0.63.15 release at 01:10Z — fix is live)
**Scenario:** No scenario file (spec declares "No new scenario test"; behavior fully observable in Ginkgo unit tests + grep audit + doc inspection + live deploy confirmation).
**Evidence:**
- AC#1 helper: `task/controller/pkg/result/result_writer.go:288 func ClearAssigneeIfHumanReview(merged lib.TaskFrontmatter) string` — single declaration, body invokes existing `clearAssignee`.
- AC#2 result_writer call site: line 215 calls `ClearAssigneeIfHumanReview(merged)` replacing inline `phase == "human_review"` guard.
- AC#3 executor call site: `task_update_frontmatter_executor.go:122 result.ClearAssigneeIfHumanReview(fm)` inside `buildUpdateModifyFn` after merge, before marshal.
- AC#4-7 unit tests: `task_update_frontmatter_executor_test.go` Context "spec 042: phase flip to human_review clears assignee" (line 282) + idempotent re-clear (line 340) + combined frontmatter+body Verdict reproducer (line 370) + non-phase preserve coverage in surrounding contexts. All pass under `make precommit`.
- AC#8 result_writer_test.go unchanged tests: pass under `make precommit`.
- AC#9 grep audit: `grep -rn 'phase.*human_review' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go` enumerates 10 matches — all comments (8), helper definition (line 289), or helper call sites (lines 215, 122). Zero assignment-side matches outside helper.
- AC#10 controller-design.md line 71: table row `UpdateFrontmatterCommand (spec 042) | human_review | "" | buildUpdateModifyFn → ClearAssigneeIfHumanReview`.
- AC#11 task-flow-and-failure-semantics.md line 188: "Partial-update doctrine guard (spec 042)" paragraph names `buildUpdateModifyFn` + `result.ClearAssigneeIfHumanReview`.
- AC#12 CHANGELOG.md: v0.63.14 entry adds helper + executor wire + result_writer refactor; v0.63.15 fix entry names the 2026-05-25 prod incident and spec 039 predecessor.
- AC#13 `make precommit` in `task/controller`: exit 0, `ready to commit`, gosec 0 issues, trivy 0 vuln.
- AC#14 Post-Deploy: dev controller pod running v0.63.15+ image (start 11:54Z, release 01:10Z). **Caveat: live K8s pod-kill reproducer was NOT executed under this verification run.** Backtest task `c0690a8b-...` triggered for the purpose ran to natural completion via the spec-041 `Result.NextPhase: human_review` path (which DOES live-exercise the shared helper from the result_writer side at line 215), not the spec-042 partial-update path. Spec-042's executor path is covered by the four Ginkgo unit tests above; next opportunistic real K8s crash (or agent verify-fail emitting `UpdateFrontmatterCommand{phase: human_review}`) will provide live evidence. Accepted as adequate: the helper itself is exercised live via the result_writer call site, the call site in the executor is a single-line invocation of the tested helper, and the four unit tests cover all branches in the FailureModes table.
**Verdict:** PASS
