---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-25T21:44:39Z"
generating: "2026-05-25T21:44:40Z"
prompted: "2026-05-25T21:51:06Z"
verifying: "2026-05-25T22:48:54Z"
branch: dark-factory/spawn-notification-early-return-skips-human-review-guard
---

## Summary

- Spec 039 shipped a `human_review` doctrine guard in `result_writer.applyRetryCounter` (line 180): if the merged frontmatter has `phase == "human_review"`, clear `assignee`. The guard is correct, but unreachable when `spawn_notification: true` is also set on the merged frontmatter — an early return at line 171 short-circuits the function before line 180 runs.
- Live incident on 2026-05-25 (~8h after the spec-039 prod deploy) reproduced the bug: a pr-reviewer task transitioned to `phase: human_review` with `assignee: pr-reviewer-agent` still in place and no `previous_assignee` field — proving `clearAssignee()` never executed.
- The bug class is the same as a 2026-04-24 dev incident (task `ba1bad61`), where `spawn_notification`'s early return skipped `applyTriggerCap`. That fix moved `applyTriggerCap` above the early return; the `human_review` guard at line 180 was not also moved, so the next doctrine field added below the early return inherited the same defect.
- Fix: reorder so the line-180 `human_review` guard runs before the `spawn_notification` early return. No semantic change to `spawn_notification`, no refactor of `applyRetryCounter`, no defensive duplication.
- Verifier evidence is concrete: a unit test that reproduces the prod incident, a source-ordering grep over the function body, and operator-driven live verification on the dev cluster after deploy from the `agent-dev` worktree.

## Problem

After spec 039 shipped, a live pr-reviewer task on prod landed at `phase: human_review` with `assignee: pr-reviewer-agent` still set and `previous_assignee` absent. The final mutating commit (`09ed9a55`, 2026-05-25T18:39:53Z) changed only the `phase` field — proving the assignee-clear guard never ran on that write. The guard's source is correct (and verified by spec 039's own ACs); it is unreachable when the merged frontmatter also carries `spawn_notification: true`, because `applyRetryCounter` returns at line 171 before reaching line 180. The operator's inbox filter (`assignee == ""`) misses the task; the operator has to hand-edit the file. This is the spec-039 invariant breaking under a control-flow path the spec did not test against. A 2026-04-24 dev incident with the same shape against a different doctrine field (`applyTriggerCap`) was already fixed by reordering — the same reordering must now be applied to the `human_review` guard before a third instance of this bug surfaces on a fourth doctrine field.

## Goal

After this change, the `human_review` doctrine guard in `applyRetryCounter` is guaranteed to run whenever the merged frontmatter has `phase == "human_review"`, regardless of any other field on the merged frontmatter (including `spawn_notification`). Concretely: the textual order of the body of `applyRetryCounter` is such that no `return` statement appears between the function entry and the `human_review` check on any input where `phase == "human_review"`. The pr-reviewer incident shape — `phase: human_review` + `assignee: <agent>` + `previous_assignee` absent — cannot be produced by this code path.

**Invariant established by this work:** any control-flow path through `applyRetryCounter` that mutates `merged` and then returns the body MUST first traverse the `phase == "human_review"` → `clearAssignee(merged)` check. The check is positioned such that no early return can skip it.

## Assumptions

- The pr-reviewer agent's emission path (`maintainer/agent/pr-reviewer/` → `agentlib.Result{Status: Done, NextPhase: "human_review"}` → `lib/delivery/result-deliverer.go` `AgentStatusDone` branch) is correct as of HEAD. Spec 039's verification already confirmed `AgentStatusDone` writes `phase: human_review` only via `resolveNextPhase`. This spec does not change that path.
- The `task/executor/pkg/result_publisher.go` `PublishFailure` path (K8s pod crash → partial-update with `phase: human_review`) does NOT route through `applyRetryCounter` and is therefore out of scope. (Verified during spec 039: the K8s-crash publisher writes through a separate partial-update primitive.)
- The `spawn_notification` field is set spawn-time by `task/executor/pkg/command/task_update_frontmatter_executor.go` and consumed by `applyRetryCounter` to suppress the retry-counter cap append on the spawn write. Its semantics (when set / how long it lives / what it suppresses) are NOT changed by this spec — only the textual position of the consumer relative to the line-180 guard.
- The 2026-05-25 pr-reviewer incident on task `bborbe-agent #3 (183193c3, add-pi-agent-variant)` is a representative trace of the bug, not a one-off operator edit. Final commit `09ed9a55` changed only the `phase` field, no `assignee` mutation, no `previous_assignee` field appeared — this is the unit-test reproducer's input shape.
- The `result_writer_test.go` Ginkgo suite already has fixtures for `existing` + `incoming` frontmatter maps and a `WriteResult` driver. Adding a new Context for the `spawn_notification + human_review` case does not require new fakes or test infrastructure.
- The completed prompt `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` is the 2026-04-24 fix referenced inline at `result_writer.go:160-165`. It is the documented precedent for the same reorder pattern.

## Non-goals

- NOT touching the semantics, write site, or lifetime of `spawn_notification`. The field continues to be set spawn-time by the executor and deleted by `applyRetryCounter` on the first post-spawn write, exactly as today.
- NOT refactoring `applyRetryCounter` into discrete passes called from `WriteResult` (Option B from the driving task). That refactor is larger, requires test re-organisation, and is deferred until/unless a third recurrence of this bug class surfaces.
- NOT introducing a defensive duplicate of the `human_review` guard inside the `spawn_notification` early-return branch (Option C). Doctrine duplicated across two sites would let the same class of bug recur if a new early-return is added below the duplicate.
- NOT broadening scope to "all early returns in `applyRetryCounter`". The `status == "completed"` early return at line 151 carries its own doctrine (no further mutation on completed tasks) and is unchanged. This spec changes the ordering of the `human_review` guard relative to ONE early return — the `spawn_notification` branch.
- NOT touching `task/executor/pkg/command/task_update_frontmatter_executor.go`. The spawn-time `UpdateFrontmatterCommand` that sets `spawn_notification: true` is the producer; the bug is at the consumer.
- NOT touching `lib/delivery/result-deliverer.go` `AgentStatusDone` branch. It writes `phase: human_review` correctly via `resolveNextPhase` (already verified by spec 039's AC#6).
- NOT migrating the pr-reviewer task already on disk in the 2026-05-25 incident state (`assignee: pr-reviewer-agent` + `phase: human_review`). Operator clears it by hand; no migration script.
- NOT introducing a feature flag, env var, per-agent override, or any opt-out that disables the reordered guard. An escape hatch on the goal is itself a regression; if a future consumer demands variation, that's a separate spec.
- NOT changing the operator's inbox filter or any task-orch query. The filter is `assignee == ""` per spec 021; this spec restores that signal under the `spawn_notification + human_review` path.

## Desired Behavior

1. When `result_writer.applyRetryCounter` receives merged frontmatter with `phase == "human_review"` AND `spawn_notification: true`, it clears `assignee` to `""`, populates `previous_assignee` with the prior assignee value (per the existing `clearAssignee` helper), deletes the `spawn_notification` key, and returns the modified body. Both doctrines fire on the same call.

2. When `applyRetryCounter` receives merged frontmatter with `phase == "human_review"` and NO `spawn_notification`, the existing spec-039 behavior is preserved unchanged: assignee cleared, `previous_assignee` populated, retry-cap append logic runs as before.

3. When `applyRetryCounter` receives merged frontmatter with `spawn_notification: true` and `phase != "human_review"`, the existing 2026-04-24 fix behavior is preserved: `applyTriggerCap` runs, the `spawn_notification` key is deleted, the function returns without invoking `applyRetryCap` or any further mutation. No assignee change.

4. When `applyRetryCounter` receives merged frontmatter with neither `spawn_notification` nor `phase == "human_review"`, the function flow is unchanged from spec 039: `applyTriggerCap` runs, `applyRetryCap` runs, the `human_review` guard is a no-op, body is returned.

5. The textual ordering inside the body of `applyRetryCounter` is such that the `phase == "human_review"` check (and its `clearAssignee` call) appear before the `SpawnNotification()` early return. This ordering is enforceable by source-grep — see Acceptance Criteria AC#5.

6. After deploy of the fix to the dev cluster from the `agent-dev` worktree, a real pr-reviewer agent failure-then-handoff scenario (or the equivalent live reproducer documented in the task) produces an OpenClaw task whose final frontmatter has `assignee: ""`, `previous_assignee` equal to the pre-handoff assignee value, `phase: human_review`, no `spawn_notification` key, and `current_job` cleared per the existing handoff path.

## Constraints

**Must not change:**

- The `Result` struct, `AgentStatus` enum, or Kafka topic schemas for `agent-task-v1-request` / `agent-task-v1-result`.
- The atomic-write contract (spec 006) and single-writer / serialized git invariant.
- The atomic increment / partial-update primitive (spec 015 / 016).
- The `applyTriggerCap` body-section appender and its placement above the `spawn_notification` early return (the 2026-04-24 fix).
- The `applyRetryCap` body-section appender — its placement and behavior are unchanged from spec 039.
- The `clearAssignee` helper — same signature, same behavior (populates `previous_assignee` from `assignee`, then clears `assignee`).
- The `spawn_notification` write site in `task/executor/pkg/command/task_update_frontmatter_executor.go`.
- The `lib/delivery/result-deliverer.go` `AgentStatusDone` branch (spec 039 AC#6).
- The executor's `allowedPhases` allowlist. `human_review` is still outside it; the empty-assignee skip in the scanner remains the load-bearing inbox-park signal.

**Must not regress:**

- Spec 039 (`needs_input` and `failed` paths land with `phase` unchanged + `assignee: ""`).
- Spec 021 (cap escalation paths clear assignee and append the escalation body section).
- Spec 015 cap-stickiness (a task already at cap that receives a stale agent result stays parked, no duplicate section, no assignee revival).
- The 2026-04-24 hotfix (`applyTriggerCap` runs before the `spawn_notification` early return; prompt 075).
- The legitimate `Result.NextPhase: human_review` handoff path: a `Done` result with `NextPhase: "human_review"` and no `spawn_notification` on the merged frontmatter still clears assignee via the line-180 guard (now positioned earlier).

**Relevant docs:**

- `docs/controller-design.md` § "Assignee-Clear on Escalation" table — the row for `Agent emits Result.NextPhase: human_review (legitimate handoff)` must explicitly note that the guard fires regardless of `spawn_notification` state on the merged frontmatter.
- `docs/task-flow-and-failure-semantics.md` — already states the doctrine; no edit required unless the audit pass below identifies wording that implies the guard is gated by other fields.
- `specs/completed/039-controller-stop-setting-human-review-on-failure.md` — predecessor; this spec patches the one unreachable-path defect in the spec-039 guard. Spec 039's other ACs and code paths are not affected.
- `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` — precedent for the reorder pattern; cite in the prompt-side context.
- Driving task: `~/Documents/Obsidian/Personal/24 Tasks/spawn_notification Early Return Skips human_review Guard.md`.
- Live incident artifact: `~/Documents/Obsidian/OpenClaw/tasks/PR Review github - bborbe-agent - 3 - 183193c3 - add-pi-agent-variant.md` (read-only reference; not modified by this spec).

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility | Concurrency |
|---|---|---|---|---|---|
| Agent emits `Done` + `NextPhase: human_review` for a task that was spawned in the same execution window (so executor wrote `spawn_notification: true` on the spawn) | `applyRetryCounter` runs the `human_review` guard first: `assignee` cleared to `""`, `previous_assignee` populated, then the `spawn_notification` early return fires, `spawn_notification` key deleted, body returned. Final on-disk frontmatter has `phase: human_review`, `assignee: ""`, `previous_assignee: <agent>`, no `spawn_notification` key. | None — terminal state until operator verifies and re-delegates or marks done. | Task file frontmatter: `assignee` empty, `previous_assignee` matches the pre-handoff agent name. Operator board inbox filter (`assignee == ""`) surfaces the task within one refresh. | Reversible by operator edit (restore `assignee`, drop `previous_assignee`). | Atomic single write under git-rest mutex; same as today. |
| Agent emits `Done` + `NextPhase: human_review` with NO `spawn_notification` on merged frontmatter (the routine handoff path) | Existing spec-039 behavior preserved: `applyTriggerCap` no-op (no cap), `human_review` guard fires, `assignee` cleared, `previous_assignee` populated, `applyRetryCap` no-op, body returned. | Same as above. | Same as above. | Reversible. | Unchanged. |
| Agent emits `needs_input` or `failed` with `spawn_notification: true` on merged frontmatter | `phase` is NOT `human_review` (per spec 039 the deliverer leaves phase unchanged on these paths). The `human_review` guard is a no-op. `applyTriggerCap` runs, the `spawn_notification` early return fires, `spawn_notification` key deleted, body returned. `assignee` is cleared upstream by the deliverer (spec 039). | Same as today. | Frontmatter: `assignee` empty, `phase` unchanged from pre-failure value, no `spawn_notification`. | Reversible. | Unchanged. |
| Concurrent stale agent result publish arrives for a task already in the post-handoff state (`assignee: ""`, `phase: human_review`, no `spawn_notification`) | Result writer's existing cap-stickiness logic applies. `human_review` guard fires (idempotent — `assignee` already empty, `clearAssignee` is a no-op on empty). Body returned unchanged. No duplicate section, no assignee revival. | None — terminal until operator action. | Section count stays 1, phase stays at `human_review`. | N/A — terminal. | spec 015 stickiness preserved. |
| `applyRetryCounter` receives merged frontmatter with `phase: human_review` and `spawn_notification: true` and ALSO `status: completed` | The `status == "completed"` early return at line 151 fires first (its placement is unchanged by this spec). No mutation. This is the documented invariant: completed tasks are immutable from the controller side. | None. | File untouched. | N/A. | Unchanged. |
| Operator hand-edits a parked task to set `spawn_notification: true` then re-delegates by setting `assignee` | Out of scope for this spec — the executor's spawn-time `UpdateFrontmatterCommand` is the only legitimate writer of `spawn_notification`. If the operator forges the field, the next `applyRetryCounter` call will treat it like a real spawn write: the `human_review` guard runs first (if applicable), then the `spawn_notification` branch deletes the key. No corruption. | Operator can clear the field by re-delegating again. | Frontmatter inspection. | Reversible. | Same as today. |
| Existing on-disk task in the 2026-05-25 incident shape (`phase: human_review`, `assignee: pr-reviewer-agent`, no `previous_assignee`, no `spawn_notification`) | NOT migrated by this spec. File stays as-is until the operator clears `assignee` or a new agent result arrives. On the next result, the new code path emits the correct shape. | Operator hand-edit. | Operator board inbox filter misses it until convergence. | Reversible by operator. | None — quiescent. |

## Security / Abuse Cases

No new attack surface. The `assignee`, `previous_assignee`, `phase`, and `spawn_notification` fields are already operator-controllable in the vault. This spec changes the textual ordering of two checks inside `applyRetryCounter`; it does not introduce new inputs, new write sites, or new external boundaries. The trust boundary at the agent-output → deliverer interface is unchanged. The only narrowed surface is the set of frontmatter states the controller can produce: the `phase: human_review + assignee: <agent> + spawn_notification: true` state is no longer reachable, which is the explicit goal.

## Acceptance Criteria

- [ ] **Unit test reproducing the prod incident (AC#1).** A new Ginkgo Context in `task/controller/pkg/result/result_writer_test.go` (suggested name `when merged has spawn_notification and phase human_review`, agent decides at impl time) feeds `WriteResult` with `existing.spawn_notification = true` + `existing.assignee = pr-reviewer-agent` + `incoming.phase = human_review`. Evidence: the test asserts the resulting on-disk frontmatter has `assignee: ""`, `previous_assignee: pr-reviewer-agent`, `phase: human_review`, and the `spawn_notification` key absent. Test fails on master at HEAD before the fix; test passes after the fix. Evidence shape: Ginkgo run output line `1 Passed | 0 Failed | 0 Pending | 0 Skipped` on the new Context; `git stash && go test ./pkg/result/... -run 'spawn_notification.*human_review' -v` fails on master.

- [ ] **Spec-039 needs_input regression check (AC#2).** Existing Context covering `needs_input` shape (phase unchanged, assignee empty) in `result_writer_test.go` continues to pass unchanged. Evidence shape: Ginkgo run output for the unchanged Context shows it green; no test-file diff under the `needs_input` Context's `It` block.

- [ ] **Spec-039 failed regression check (AC#3).** Existing Context covering `failed` shape (phase unchanged, assignee empty) in `result_writer_test.go` continues to pass unchanged. Evidence shape: same as AC#2 for the `failed` Context.

- [ ] **Cap stickiness regression check (AC#4).** Existing Contexts covering `trigger_count` cap and `retry_count` cap (spec 021's escalation table) continue to pass unchanged. Evidence shape: Ginkgo run output for the cap Contexts shows them green.

- [ ] **Source-ordering AC closes the laziness loophole (AC#5).** Within the body of `applyRetryCounter` only, the `phase == "human_review"` literal appears textually before the `SpawnNotification()` early return. Evidence shape: `awk '/^func \(r \*resultWriter\) applyRetryCounter/,/^}/' task/controller/pkg/result/result_writer.go | grep -nE 'phase == "human_review"|SpawnNotification\(\)'` — the line number of the `human_review` match is strictly LESS than the line number of the `SpawnNotification()` match. Verifier captures both numbers and asserts the inequality.

- [ ] **2026-04-24 hotfix preservation (AC#6).** The relative ordering of `applyTriggerCap` and the `SpawnNotification()` early return is unchanged from prompt 075's fix: `applyTriggerCap` appears before the early return. Evidence shape: the same `awk | grep` over the function body shows `applyTriggerCap(` line number LESS than `SpawnNotification()` line number. (This AC documents an invariant the reorder must preserve; it is failable only if the impl agent over-reorders.)

- [ ] **Legitimate handoff regression check (AC#7).** Existing test in `result-deliverer_test.go` covering `AgentStatusDone` + `NextPhase: human_review` continues to pass unchanged, and the corresponding `result_writer_test.go` Context (where merged has `phase: human_review` and NO `spawn_notification`) also passes unchanged. Evidence shape: Ginkgo run output green for both Contexts; assignee-empty and previous_assignee-populated assertions still pass.

- [ ] **Doctrine doc update (AC#8).** `docs/controller-design.md` § "Assignee-Clear on Escalation" table (or the equivalent section that documents the `Result.NextPhase: human_review` handoff row) gains an explicit note: "the guard fires regardless of `spawn_notification` state on merged frontmatter". Evidence shape: `grep -n 'spawn_notification' docs/controller-design.md` returns at least one line in the assignee-clear section; the line text mentions the doctrine fires irrespective of that field's value.

- [ ] **CHANGELOG entry (AC#9).** `CHANGELOG.md` under `## Unreleased` contains a `fix(controller):` entry naming the bug (spawn_notification early return skipping the spec-039 human_review guard) and referencing spec 039 as the predecessor and the 2026-05-25 prod incident as the trigger. Evidence shape: `grep -A 3 'fix(controller)' CHANGELOG.md | head -20` shows the entry; the entry text mentions both `spec 039` and `spawn_notification`.

- [ ] **make precommit in task/controller (AC#10).** Evidence shape: `cd ~/Documents/workspaces/agent/task/controller && make precommit` exit code 0; final log line contains `ready to commit`.

- [ ] **Post-Deploy (Rung-2) — Live verification on dev cluster (AC#11).** Operator-driven. After the merge commit is deployed to dev via `cd ~/Documents/workspaces/agent-dev && git pull && git merge master && cd task/controller && BRANCH=dev make buca`, the operator verifies that the controller pod image SHA in dev matches the `make buca` output for the merge commit, then triggers a real pr-reviewer agent handoff (or the equivalent live reproducer documented in the driving task). Evidence shape: `deploy_check:` field in the verification record names the controller pod image SHA in dev; `deploy_target: dev`; the resulting OpenClaw task frontmatter has `assignee: ""`, `previous_assignee: pr-reviewer-agent`, `phase: human_review`, and the `spawn_notification` key absent. Verifier captures the task file path and the relevant frontmatter snippet in the verification result.

- [ ] **No new scenario test (AC#12).** The behavior is fully observable in Ginkgo unit tests against the existing fakes (synthetic `existing` + `incoming` frontmatter maps fed to `WriteResult`). No Docker, no `gh`, no live cluster required for automated verification. The dev-cluster live verification at AC#11 is operator-driven evidence captured post-deploy, not an automated scenario. Evidence shape: no new file under `scenarios/`; no Ginkgo `Describe` block added outside `task/controller/pkg/result/result_writer_test.go`.

## Verification

```
cd ~/Documents/workspaces/agent/task/controller && make precommit
```

Source-ordering audit (must show the `human_review` line number less than the `SpawnNotification()` line number, and `applyTriggerCap` less than `SpawnNotification()`):

```
cd ~/Documents/workspaces/agent && awk '/^func \(r \*resultWriter\) applyRetryCounter/,/^}/' task/controller/pkg/result/result_writer.go | grep -nE 'phase == "human_review"|SpawnNotification\(\)|applyTriggerCap\('
```

Reproducer test (must fail on master at HEAD, pass after the fix):

```
cd ~/Documents/workspaces/agent/task/controller && go test ./pkg/result/... -run 'spawn_notification.*human_review' -v
```

Manual smoke on dev (post-deploy from `agent-dev` worktree per the project workflow):

1. Deploy the merge commit to dev: `cd ~/Documents/workspaces/agent-dev && git pull && git merge master && cd task/controller && BRANCH=dev make buca`.
2. Confirm the controller pod image SHA in dev matches the `make buca` output: `kubectlquant -n dev get pod -l app=task-controller -o jsonpath='{.items[0].spec.containers[0].image}'`.
3. Trigger a real pr-reviewer agent handoff on a freshly spawned task (so `spawn_notification: true` is on the merged frontmatter when the result lands).
4. Wait for the agent to run and the controller to write the result.
5. Read the task file frontmatter via the `Read` tool. Confirm: `assignee: ""`, `previous_assignee: pr-reviewer-agent`, `phase: human_review`, no `spawn_notification` key.
6. Confirm the operator's board inbox filter (`assignee == ""`) surfaces the task within one refresh cycle.

## Do-Nothing Option

Cost of leaving this unfixed:

- The 2026-05-25 pr-reviewer incident pattern repeats every time a pr-reviewer (or any agent that emits `Result.NextPhase: human_review` on its first post-spawn write) hands off to a human. Operator board's inbox filter misses the task; operator hand-edits two frontmatter fields per incident.
- The spec-039 invariant is documented as load-bearing in `docs/controller-design.md` and `docs/task-flow-and-failure-semantics.md`, but the code emits a state the invariant forbids. New contributors reading the doc will be misled; new agents added below pr-reviewer will inherit the same defect on their first handoff.
- The 2026-04-24 dev incident was the warning shot; this 2026-05-25 prod incident is the second instance of the same bug class. Deferring increases the probability of a third instance against a fourth doctrine field added below the early return.

Do-nothing is not viable: the driving task captured a live prod incident on 2026-05-25, ~8h after the spec-039 prod deploy. The operator inbox is being relied on.

## References

- `specs/completed/039-controller-stop-setting-human-review-on-failure.md` — predecessor; shipped the line-180 `human_review` guard. This spec patches the one unreachable-path defect in that guard.
- `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` — precedent for the reorder pattern (2026-04-24 fix for the `applyTriggerCap` instance of the same bug class).
- `specs/completed/021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation.md` — established the `assignee: ""` operator-inbox-park signal that the spec-039 guard preserves and this spec restores under the `spawn_notification` path.
- `specs/completed/015-atomic-frontmatter-increment-and-trigger-cap.md` — defines the cap-stickiness behavior preserved by this spec.
- `docs/controller-design.md` — § "Assignee-Clear on Escalation" table to be updated.
- `docs/task-flow-and-failure-semantics.md` — doctrine reference; no edit anticipated.
- `~/Documents/Obsidian/Personal/24 Tasks/spawn_notification Early Return Skips human_review Guard.md` — driving task; option-A rationale and rejected alternatives recorded there.
- `~/Documents/Obsidian/OpenClaw/tasks/PR Review github - bborbe-agent - 3 - 183193c3 - add-pi-agent-variant.md` — live incident evidence (read-only; not modified).
