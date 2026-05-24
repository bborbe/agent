---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-24T23:01:12Z"
generating: "2026-05-24T23:01:13Z"
prompted: "2026-05-24T23:05:51Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

## Summary

- Today the controller and `lib/delivery` still write `phase: human_review` on five agent-failure code paths (cap-exhaustion in the atomic increment executor, `needs_input` and `failed`/default branches in both the result deliverer and the content generator).
- That contradicts the spec-021 doctrine, which says escalation = **clear assignee, phase unchanged**. `phase: human_review` must be reserved for `Result.NextPhase` — the agent's own forward-handoff to a human verifier — and never written by the controller's escalation path.
- This spec eliminates those five writes. After the change, every controller-side failure or cap escalation clears `assignee` and leaves `phase` at whatever lifecycle stage the merged frontmatter already held.
- Backward-compat: existing tasks currently parked with `phase: human_review` + a named `assignee` are left on disk untouched. No migration script. Once they're re-delegated or re-escalated, they pick up the new shape.
- Live verification reuses the gh-auth reproducer pattern from spec 021's manual smoke — break the PR-reviewer agent in dev, watch the task surface with empty assignee and unchanged phase.

## Problem

Spec 021 ("Clear Assignee on Escalation") shipped the doctrine and fixed the result-writer cap and retry paths. Five sibling write sites were missed and still produce the old shape:

1. `pkg/command/task_increment_frontmatter_executor.go` line 113 — the atomic `increment-frontmatter` command, when `trigger_count >= max_triggers` fires inside the increment itself (a different write path from the result writer's cap), sets `phase = "human_review"`.
2. `lib/delivery/result-deliverer.go` line 150 — when the agent returns `AgentStatusNeedsInput`, the deliverer sets `phase: human_review` before publishing the result onto Kafka. The result writer downstream then clears assignee (correct per spec 021), but the upstream write that put `human_review` on the frontmatter at all should not have happened.
3. `lib/delivery/result-deliverer.go` line 163 — the `default` branch (covers `AgentStatusFailed` and unknown statuses) writes `phase: human_review`.
4. `lib/delivery/content-generator.go` line 60 — `AgentStatusNeedsInput` branch writes `phase: human_review` directly into the task body content sent back to the controller.
5. `lib/delivery/content-generator.go` line 72 — `default` branch (= `failed`/unknown) writes `phase: human_review`.

Live evidence (2026-05-24, captured in the driving Obsidian task): the PR-reviewer agent failed on a real task; the resulting OpenClaw task landed with `phase: human_review` AND `assignee: pr-reviewer-agent` — failing both halves of the spec-021 doctrine. The operator's inbox filter (`assignee == ""`) missed it; the operator had to hand-edit both fields. The increment-executor and lib/delivery code paths are responsible.

The doctrine consequence: `phase: human_review` currently means at least three different things at once ("agent semantically blocked", "agent crashed", "agent finished a phase and wants a human to verify"). Any consumer that branches on phase has to also read the task body to disambiguate. This spec removes the first two meanings, leaving only the third (which is the only meaning the agent itself can emit via `Result.NextPhase`).

## Goal

After this change:

- No controller-side or `lib/delivery`-side code path writes `phase: human_review` on a failure or cap-exhaustion outcome. Every failure outcome clears `assignee` and leaves `phase` at whatever the merged frontmatter held just before the failure decision.
- `phase: human_review` is written exactly one way: by honoring an agent's own `Result.NextPhase == "human_review"` (the explicit "I'm done with my phase, please have a human verify" handoff). This is the `AgentStatusDone` branch in `result-deliverer.go`, which already routes through `resolveNextPhase`.
- A grep for `human_review` literal across `task/controller/pkg/` and `lib/delivery/` returns either (a) `Result.NextPhase` handling, (b) the result writer's `assignee`-clear guard that fires when the agent itself emitted `human_review`, or (c) tests that assert `human_review` is NOT written on failure paths. Zero failure-path writes remain.

**Invariant established by this work:** Controller-side and deliverer-side code MUST NOT write the literal `"human_review"` into the `phase` field. Only the agent's own `Result.NextPhase` can produce that value, and the only honoring path is the `AgentStatusDone` → `resolveNextPhase` branch.

## Assumptions

- No parked task currently relies on `phase: human_review` for routing logic outside the result writer's line-180 assignee-clear guard. (Verified by code search: the only consumer of `phase == "human_review"` in non-test code is that guard plus the `Result.NextPhase` resolution path.)
- The operator's inbox filter is `assignee == ""` and nothing else. Phase is not part of the filter predicate. (Anchored by spec 021 doctrine.)
- The `lib/` directory has a `precommit` Makefile target whose coverage includes the `lib/delivery` subpackage (confirmed: `lib/Makefile` exists at repo root of the lib module).
- The synthetic K8s-crash failure publisher referenced in the Failure Modes table routes through one of the five enumerated bug sites (specifically `content-generator.go:72` default branch, per the existing inline comment `status: failed — symmetric with PublishFailure's K8s-crash failure path.`). No separate sixth call site exists; AC#9's grep is the safety net if this assumption is wrong.
- Downstream consumers (operator board, notification UIs) will adapt to the narrower `human_review` doctrine without coordination. None are known to branch on `phase: human_review` to mean "agent failed" today.

## Non-goals

- NOT migrating existing parked tasks that hold `phase: human_review + assignee: <agent>` from the old doctrine. They stay on disk untouched until natural re-delegation or re-escalation rewrites them. No vault-cli sweep, no one-shot migration script.
- NOT changing the `AgentStatus` enum, Kafka topic schemas, or the `Result` struct shape. The `AgentStatusNeedsInput` value still exists; only its frontmatter mapping changes.
- NOT changing how the controller surfaces failure body content (`## Failure` section, `## Trigger Cap Escalation` section). Those body writes continue exactly as today — only the `phase` frontmatter field stops being touched on failure paths.
- NOT changing the operator's inbox filter or any task-orch query. The inbox filter is already `assignee == ""` per spec 021; this spec is the upstream fix that finally makes every failure path emit that shape.
- Do NOT introduce a feature flag, env var, or per-agent override that conditionally re-enables the old `phase: human_review` write. The doctrine is platform-wide and final; an escape hatch on the goal is itself a regression.
- Do NOT add a new "escalation phase" value (e.g. `phase: blocked`, `phase: escalated`). Phase stays at the lifecycle stage; the empty-assignee signal carries escalation semantics.

## Desired Behavior

1. When the atomic `increment-frontmatter` executor processes a `trigger_count` increment that crosses the `max_triggers` cap, it writes the new `trigger_count` value and clears `assignee` to `""` in the same atomic write. It does NOT modify `phase`. (Body-section append for `## Trigger Cap Escalation` is owned by the result writer path, not the increment executor — unchanged from today.)

2. When the result deliverer (in `lib/delivery`) handles an `AgentStatusNeedsInput` result, the published frontmatter sets `status: in_progress` and leaves `phase` at whatever value the deliverer's incoming frontmatter snapshot held. It does NOT write `phase: human_review`. The downstream result writer's assignee-clear guard (which fires when `merged["phase"] == "human_review"`) is consequently NOT triggered by this path anymore — instead, the deliverer itself clears `assignee` to `""` so the operator-inbox shape lands the same way.

3. When the result deliverer handles an `AgentStatusFailed` (or any unknown/default status), the published frontmatter sets `status: in_progress`, leaves `phase` unchanged, and clears `assignee` to `""`. The `## Failure` body section is still rendered exactly as today.

4. When `applyStatusFrontmatter` in `content-generator.go` handles `AgentStatusNeedsInput`, it sets `status: in_progress`, clears `assignee`, and does NOT modify `phase`.

5. When `applyStatusFrontmatter` handles the default branch (`AgentStatusFailed`/unknown), it sets `status: in_progress`, clears `assignee`, and does NOT modify `phase`.

6. The `AgentStatusDone` branch in the deliverer is unchanged: it continues to call `resolveNextPhase(taskID, result.NextPhase)` and write whatever phase value the agent requested. This is the **only** way `phase: human_review` can be written, and only when an agent explicitly handed off via `Result.NextPhase = "human_review"`.

7. The result writer's existing `human_review` guard (line 180, `if phase == "human_review" → clearAssignee`) stays in place as a safety net for the `AgentStatusDone` → `Result.NextPhase: human_review` handoff. It is no longer the load-bearing assignee-clear for failure paths (those now clear it upstream), but the guard remains correct for the Done+handoff case.

8. Existing tasks on disk with `phase: human_review + assignee: <agent>` (old doctrine) are not touched by this change. No migration. On the next escalation or re-delegation of such a task, the new code paths emit the new shape and the task converges.

## Constraints

**Must not change:**

- The `AgentStatus` enum (`AgentStatusDone`, `AgentStatusNeedsInput`, `AgentStatusFailed`, `AgentStatusInProgress`). Same identifiers, same numeric/string values.
- The Kafka topic schemas for `agent-task-v1-request` and `agent-task-v1-result`.
- The `Result.NextPhase` field — `human_review` remains a legal value for that field.
- The atomic-write contract (spec 006), single-writer / serialized git invariant.
- The atomic increment / partial-update primitive (spec 015 / 016) — the cap-detection logic inside the increment runs at the same point, only the mutation it emits changes.
- The result writer's `applyTriggerCap` and `applyRetryCap` body-section appenders (spec 021's escalation sections). These already clear assignee correctly; they stay as-is.
- The result writer's `human_review` → clearAssignee guard (line 180). It now fires only for the legitimate `Result.NextPhase: human_review` case.
- The executor's `allowedPhases` allowlist. `human_review` is still outside it; the `assignee == ""` skip in the scanner is what keeps parked tasks from re-spawning.
- The `## Failure` body section, the `## Trigger Cap Escalation` section, the `## Retry Escalation` section — body content writes are unchanged.

**Must not regress:**

- Spec 010 (`needs_input` vs `failed` semantics): `needs_input` still routes to "task is parked, operator must look at it"; `failed` still routes to controller-retry-or-cap. The difference between the two is preserved via the body content (`## Failure` rendered on `failed`, not on `needs_input`) and via the deliverer's status field. The convergence on `phase`-unchanged + `assignee: ""` is intentional and matches spec 021's escalation table.
- Spec 015 cap-stickiness: a task already at cap (`assignee: ""`, escalation section present) that receives another stale agent result publish stays parked, no duplicate section, no assignee revival.
- Spec 021 escalation table: the three rows (`trigger_count` cap, `retry_count` cap, `needs_input`) all continue to land with `assignee: ""`. This spec changes the third row's `phase` column from `human_review` to "unchanged", aligning it with the first two rows.

**Relevant docs:**

- `docs/controller-design.md` — § "On agent-task-v1-request" (line 42 — currently says `if agent emits needs_input → set phase: human_review, clear assignee`; must be updated to: `if agent emits needs_input → clear assignee, leave phase unchanged`). § "Assignee-Clear on Escalation" table (line 67 — currently shows `Agent emits needs_input | human_review | ""`; must be updated to `Agent emits needs_input | unchanged | ""`). § "increment-frontmatter" flow (line 97 — currently says `set phase = "human_review" in the same write`; must be updated to `clear assignee in the same write, leave phase unchanged`).
- `docs/task-flow-and-failure-semantics.md` — must reflect that `phase: human_review` is reserved for `Result.NextPhase` handoffs and never controller-emitted.
- Operator-side cross-reference (NOT modified by this spec, lives outside `/workspace`): `~/Documents/Obsidian/Personal/50 Knowledge Base/Agent Pipeline Concept.md` — "Two orthogonal axes" / "Failure flow" sections. Hand-updated by operator post-merge if they still imply `human_review` for `needs_input`. The dark-factory prompt MUST NOT attempt to read or modify this path.
- `specs/completed/021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation.md` — Desired Behavior item 1 of spec 021 said `needs_input` writes `phase: human_review` + `assignee: ""`. This spec supersedes that one bullet of 021. The rest of 021 (cap paths, reset on re-delegation) stands unchanged. Reference this supersession explicitly in the body.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection | Reversibility | Concurrency |
|---|---|---|---|---|---|
| Atomic increment crosses `trigger_count >= max_triggers` | `trigger_count` written to new value, `assignee` cleared to `""`, `phase` unchanged. No `human_review` write. | Operator re-delegates via assignee edit; scanner resets counters (spec 021). | Task file frontmatter inspection — phase matches pre-cap value, assignee empty. | Reversible by operator edit. | Atomic single write under git-rest mutex; concurrent stale agent result publish to result-writer path independently clears assignee (idempotent). |
| `AgentStatusNeedsInput` from agent | Deliverer publishes result with `status: in_progress`, `phase` unchanged, `assignee: ""`. Result writer downstream merges and persists. No `human_review` write anywhere. | Operator inspects task body (no `## Failure` section because needs_input ≠ failed), decides next step, re-delegates. | Task file frontmatter: phase = pre-failure value, assignee empty. | Reversible by operator. | Deliverer → Kafka → result writer is the existing sequence; no new race introduced. |
| `AgentStatusFailed` from agent | Deliverer publishes result with `status: in_progress`, `phase` unchanged, `assignee: ""`. `## Failure` body section rendered. Result writer persists. No `human_review` write. | Controller may retry per `retry_count < max_retries`. At cap, retry escalation path (spec 021) fires; same shape. | Task body shows `## Failure` block; frontmatter shows assignee empty, phase unchanged. | Reversible by operator. | Same sequencing as today — only the frontmatter mutation differs. |
| Agent crashes mid-job (K8s pod failure, no Result returned at all) | The K8s-side failure publisher in `lib/delivery` (the `PublishFailure` path) emits a synthetic failure result. Per this spec, that synthetic result also lands with phase unchanged + assignee empty. | Same as `AgentStatusFailed`. | Failure body section present + frontmatter shape. | Reversible. | Synthetic publish and any concurrent real publish are deduplicated by the existing Kafka offset / result-writer idempotency (unchanged). |
| Agent emits `Result.NextPhase = "human_review"` legitimately (Done + human-verify handoff) | Deliverer's `AgentStatusDone` branch routes through `resolveNextPhase`, writes `phase: human_review`. Result writer's existing line-180 guard fires, clears `assignee`. | Operator verifies the agent's output and either marks done or kicks back. | Frontmatter: phase = `human_review`, assignee empty, status = `in_progress`. | Reversible. | Unchanged from today. |
| Stale agent result arrives at a task already parked (assignee `""`, escalation section present) | Result writer's existing cap-stickiness logic restores the on-disk phase, keeps assignee empty, no duplicate section. Unchanged from today. | None — terminal state until operator re-delegates. | Section count stays 1, phase stays at cap-fired stage. | N/A — terminal. | spec 021's stickiness behavior preserved. |
| Existing task on disk: `phase: human_review + assignee: backtest-agent` (pre-spec) | No migration. File untouched until the next event (re-delegation, new agent result, escalation) hits it. At that point the new code paths emit the new shape and the task converges. | Operator can manually clear assignee + revert phase at any time (manual override is the only migration path). | Operator board's inbox filter (`assignee == ""`) won't surface it until convergence. | Reversible by operator edit. | None — quiescent state. |
| Result writer's line-180 `human_review` guard fires on a task that just had `phase: human_review` written by the legitimate `Result.NextPhase` path | Assignee is cleared (correct — that's a handoff to a human verifier, which is an inbox-eligible state per spec 021). | Operator verifies. | Same as the legitimate-handoff row above. | Reversible. | Unchanged. |

## Security / Abuse Cases

No new attack surface. The `phase` and `assignee` fields are already operator-controllable in the vault. This spec removes controller-side writes that previously produced a confusing-but-not-dangerous state. The remaining `human_review` write (legitimate `Result.NextPhase` handoff) is gated by the same agent-output trust boundary as today.

One observation: the bug fix narrows the meaning of `phase: human_review` and so narrows the surface that downstream consumers can branch on. Any tool that currently keys off `phase: human_review` to mean "agent failed" will need to switch to keying off `assignee: ""` (or the body section presence). That is the correct doctrine; consumers are expected to follow.

## Acceptance Criteria

- [ ] `task_increment_frontmatter_executor.go` cap path: when `trigger_count >= max_triggers`, the atomic write sets `trigger_count` to its new value, sets `assignee: ""`, and does NOT modify `phase`. Evidence: unit test in `pkg/command/task_increment_frontmatter_executor_test.go` (Ginkgo Context: `trigger_count cap escalation`) asserts the post-write frontmatter has `assignee: ""`, `phase` equal to the pre-write value (run with three pre-write phase values: `planning`, `in_progress`, `ai_review`), and `phase != "human_review"`. Test fails on current main, passes after this change.

- [ ] `lib/delivery/result-deliverer.go` `AgentStatusNeedsInput` branch (currently line 148-150): publishes `status: in_progress`, leaves `phase` at the incoming frontmatter value, sets `assignee: ""`. Evidence: unit test in `lib/delivery/result-deliverer_test.go` asserts the published `lib.Task.Frontmatter` map has `phase` equal to the input task's phase (verify for `planning`, `in_progress`, `ai_review` inputs), `assignee: ""`, and `phase != "human_review"`.

- [ ] `lib/delivery/result-deliverer.go` default branch (currently line 161-163, covers `AgentStatusFailed` and unknown): same shape as the `needs_input` AC above — `phase` unchanged, `assignee: ""`, `phase != "human_review"`. Evidence: unit test in the same file, separate Context, covering `AgentStatusFailed` explicitly and a synthetic unknown status value.

- [ ] `lib/delivery/content-generator.go` `applyStatusFrontmatter` for `AgentStatusNeedsInput`: returns content whose frontmatter has `phase` equal to the input frontmatter's `phase` (preserved from `existing` content), `assignee: ""`, and no `human_review` literal in the phase position. Evidence: unit test in `lib/delivery/content-generator_test.go` exercising the function with input content carrying various phase values; output parsed via the existing `ExtractFrontmatter` helper.

- [ ] `lib/delivery/content-generator.go` `applyStatusFrontmatter` default branch (failed/unknown): same shape as above. Evidence: unit test, separate Context, covering `AgentStatusFailed` and an unknown status value.

- [ ] No regression in the legitimate `Result.NextPhase: human_review` handoff path. Evidence: existing tests in `result-deliverer_test.go` covering `AgentStatusDone` + `NextPhase: human_review` continue to pass unchanged. Output frontmatter has `phase: human_review` (this is the only allowed write of that literal) and the downstream result writer's line-180 guard clears `assignee` to `""`.

- [ ] No regression in spec 021's escalation behavior in the result writer. Evidence: existing Ginkgo Contexts in `task/controller/pkg/result/result_writer_test.go` (the cap paths, the retry paths, the stickiness paths) all pass unchanged. Specifically: `writes assignee: empty and preserves phase: {ai_review,in_progress,planning} at trigger cap` still passes; `clears assignee when agent emits needs_input` continues to assert assignee empty (but its phase assertion changes — see updated-test AC below).

- [ ] Existing `needs_input` test in `result_writer_test.go` that asserted `phase: human_review` is updated to assert `phase: unchanged` (i.e. equal to the pre-write phase from the existing frontmatter). Evidence: test file diff shows the assertion changed; test passes after this change.

- [ ] A repository-wide grep for the literal string `"human_review"` (case-sensitive, double-quoted) inside `task/controller/pkg/` and `lib/delivery/` (excluding `_test.go` files) returns ONLY read-side or comment-side references — zero write-side references. Specifically: no `fm["phase"] = "human_review"`, no `SetFrontmatterField(..., "phase", "human_review")`, no `frontmatter["phase"] = "human_review"`, no `task.Frontmatter["phase"] = "human_review"` writes remain in non-test code. The allowed remaining references are: (a) the result writer's `merged["phase"] == "human_review"` guard (read), (b) any `Result.NextPhase`-based comparison or routing (read), (c) `// human_review` explanatory comments. Evidence: the verifier runs `grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go` and enumerates each remaining match by `file:line`, then asserts each one is a read or comment (NOT an assignment to a phase field). Any line matching the pattern `\["phase"\]\s*=\s*"human_review"` or `phase.*"human_review"` on the LHS of an assignment FAILS this AC.

- [ ] `docs/controller-design.md` updated: line ~42 (the `if agent emits needs_input` bullet in the request-flow pseudocode), line ~67 (the Assignee-Clear table row for `needs_input`), and line ~97 (the increment-frontmatter cap escalation step) all reflect the new shape: phase unchanged, assignee cleared. Evidence: file diff under git review; `grep -n 'human_review' docs/controller-design.md` shows only the supersession note and the legitimate `Result.NextPhase` handoff reference.

- [ ] `docs/task-flow-and-failure-semantics.md` updated to state explicitly: "`phase: human_review` is reserved for agent-emitted `Result.NextPhase` handoffs. The controller and the result deliverer never write this phase on failure or cap-exhaustion paths." Evidence: grep of the doc returns this sentence (or equivalent).

- [ ] `CHANGELOG.md` under `## Unreleased`: an operator-visible entry naming the doctrine narrowing — controller no longer writes `phase: human_review` on `needs_input` / `failed` / cap; phase now strictly reflects the lifecycle stage on parked tasks. Evidence: changelog file diff shows the entry; the entry names spec 021 as the predecessor and this spec as the completion of that doctrine.

- [ ] `make precommit` in `task/controller` passes — gosec 0 issues, trivy 0 vuln, all Ginkgo suites green. Evidence: `make precommit` exit code 0, "ready to commit" line in output.

- [ ] `make precommit` in `~/Documents/workspaces/agent/lib` passes (confirmed: `lib/Makefile` exists and its `precommit` target covers the `lib/delivery` subpackage via `go test ./...`). Evidence: `cd ~/Documents/workspaces/agent/lib && make precommit` exit code 0.

- [ ] Live verification on dev cluster: trigger a real PR-reviewer agent failure (use the `gh auth` reproducer pattern referenced in spec 015's verification, or the runbook "Create PR Review Agent Task" § Manual override). After the agent fails and the controller writes the result, inspect the resulting OpenClaw task file. Evidence: task frontmatter shows `assignee: ""`, `phase` equal to whatever the pre-failure phase was (e.g. `planning` or `in_progress`, NOT `human_review`); task body contains a `## Failure` section with the agent's stderr; operator-board inbox filter (`assignee == ""`) surfaces the task within one refresh. Capture the file path and the relevant frontmatter snippet in the verification result.

- [ ] No new scenario test. The behavior is fully observable in Ginkgo unit tests against the existing fakes (fake gitclient, synthetic frontmatter/body inputs to the deliverer and content-generator, in-memory parse-and-assert). No Docker, `gh`, or live cluster required for verification. The dev-cluster smoke above is operator-driven evidence captured during deploy, not an automated scenario.

## Verification

```
cd ~/Documents/workspaces/agent/task/controller && make precommit
cd ~/Documents/workspaces/agent/lib && make precommit
```

Failure-path grep audit (must return empty or only the allowed read-side references documented in the grep AC):

```
cd ~/Documents/workspaces/agent && grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go
```

Manual smoke on dev (post-deploy from `agent-dev` worktree per the project workflow):

1. Create or identify a PR-reviewer task that will deterministically fail (broken `gh auth`, missing token, or other reproducer).
2. Wait for the agent to run and the controller to write the result.
3. `cat` (via `Read` tool, not raw cat) the task file frontmatter. Confirm: `assignee: ""`, `phase` equal to the pre-failure lifecycle stage (typically `planning` or `in_progress`), and no `human_review` anywhere in the frontmatter.
4. Confirm the task body contains a `## Failure` section with the agent's stderr.
5. Confirm the operator's board inbox filter (`assignee == ""`) surfaces the task within one refresh cycle.
6. Re-delegate by setting `assignee` back to `pr-reviewer-agent`; confirm spec 021's reset path fires (`trigger_count` reset to 0) and the executor re-spawns the agent on the next scan.

## Do-Nothing Option

Cost of leaving this unfixed:

- The 2026-05-24 incident pattern repeats every time an agent fails: operator board's inbox filter misses the failed task; operator hand-edits two frontmatter fields per incident; doctrine drifts from documented invariant.
- `phase: human_review` continues to mean three things at once. Any new operator UI, board, or notification that branches on phase will encode the ambiguity into its query logic, which then becomes hard to unwind.
- Spec 021 is incomplete on disk. The escalation table in `docs/controller-design.md` says one thing (the new doctrine) and the code emits the old one for two of the three rows. New contributors reading the doc will be misled.

It is reasonable to defer only if the operator inbox is not being relied on day-to-day. The driving Obsidian task captured a live incident on 2026-05-24, which means the inbox IS being relied on; the do-nothing option is no longer viable.

## References

- `specs/completed/021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation.md` — predecessor; this spec completes the doctrine for the two rows 021 partially covered (`needs_input` row) and one row 021 missed entirely (the atomic-increment cap path) plus the two `lib/delivery` write sites that 021 did not touch.
- `specs/completed/010-failure-vs-needs-input-semantics.md` — the `needs_input` vs `failed` distinction is preserved here (body content differs; frontmatter shape now converges).
- `specs/completed/011-retry-counter-spawn-time-semantics.md` — retry-counter ownership stays at the executor; this spec does not touch it.
- `specs/completed/015-atomic-frontmatter-increment-and-trigger-cap.md` — defines the atomic-increment path whose cap-mutation we change in bug-site 1.
- `specs/completed/016-partial-frontmatter-publishers.md` — the partial-update primitive used by both the deliverer and the increment executor; unchanged.
- `docs/controller-design.md` — must be updated (see Acceptance Criteria).
- `docs/task-flow-and-failure-semantics.md` — must be updated.
- `~/Documents/Obsidian/Personal/24 Tasks/Controller Stop Setting human_review on Agent Failure.md` — driving task with live incident evidence.
- `~/Documents/Obsidian/Personal/50 Knowledge Base/Agent Pipeline Concept.md` — doctrine canonical reference.
- Sibling task: [[Capture gh auth setup-git stderr on pr-reviewer auth failure]] — surfaced the 2026-05-24 incident.
- Superseded: [[Enforce trigger_count cap escalation sticky human_review in TaskResultExecutor]] — enforced the old (now wrong) doctrine.
