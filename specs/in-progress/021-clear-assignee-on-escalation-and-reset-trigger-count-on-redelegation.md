---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-10T16:25:00Z"
generating: "2026-05-10T16:35:07Z"
prompted: "2026-05-10T16:48:15Z"
branch: dark-factory/clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation
---

## Summary

- When the controller writes an escalation outcome to a task file, it clears `assignee` to empty so parked tasks surface as "unclaimed" on operator boards.
- For genuine "needs human work" escalations (agent emitted `needs_input`), the controller continues to set `phase: human_review` AND clears `assignee`. Today's behavior, plus the new assignee clear.
- For cap-reached escalations (`max_triggers` cap, `max_retries` cap), the controller leaves `phase` at whatever the lifecycle stage was before the failure (typically `planning`, `in_progress`, or `ai_review`) and clears `assignee`. The operator inbox surfaces the task at its correct stage instead of conflating it with "human-style work needed".
- When the operator re-delegates a parked task by setting `assignee` from empty to an agent name, the controller atomically resets `trigger_count: 0` so the agent gets a fresh per-attempt retry budget.
- New escalations only — existing parked tasks in the vault keep whatever shape they have on disk; no migration or backfill.

## Problem

Today the controller writes `phase: human_review` on every escalation regardless of cause and leaves `assignee` pointed at the failed agent. Two consequences operators feel daily:

1. **Stalled tasks are invisible.** An operator querying their board for "tasks I should look at" filters by `assignee == me` (or by team). Tasks parked because the agent hit a cap or genuinely needs human work are still tagged with the agent's name — they don't show up in any human inbox until someone notices the timestamp drift.
2. **`phase: human_review` is ambiguous.** An operator sees `human_review` and cannot tell whether the agent semantically can't proceed (e.g. `needs_input`: humans must do the actual work themselves) or the agent burned its retry budget on infra glitches (e.g. `max_triggers`: a human just needs to fix the underlying flake and re-delegate). The same phase value names two very different operator actions.

The fresh doctrine (see `docs/kafka-schema-design.md` Status Mapping table, and the "Two orthogonal axes" section in `~/Documents/Obsidian/Personal/50 Knowledge Base/Agent Pipeline Concept.md`) splits these into two orthogonal axes: `assignee == ""` is the universal "unclaimed" inbox flag; `phase` describes the kind of work needed at the task's current lifecycle stage. The controller still writes the conflated old shape.

## Goal

After this change, an operator can read a parked task's frontmatter and answer two questions at a glance:

- *"Is anyone working on it?"* — `assignee` is empty when nobody is.
- *"What kind of next step does it need?"* — `phase` says `human_review` only when a human must do the work itself; otherwise it reflects the lifecycle stage the agent stopped at, so a delegate-back is the natural next step.

A task re-delegated by the operator gets a fresh retry budget without any other manual edit.

**Invariant established by this work:** `assignee == ""` is the single canonical inbox signal across all parked-task causes (cap, retries, `needs_input`). Any tooling, board, or notification that surfaces "tasks needing attention" filters on assignee, not on phase.

## Non-goals

- NOT introducing a `failure_class` field on the agent verdict JSON (separate task: [[Auto-Retry Transient Agent Failures Before Human Review]]).
- NOT changing the watcher untrusted-author flow (separate spec lives in the `maintainer` repo).
- NOT changing operator board / task-orch query logic — that filter spec is already approved on the task-orch side and only needs the controller to start writing the new shape.
- NOT migrating or backfilling existing parked tasks. Only escalations written after this change ship use the new shape; existing tasks in the vault keep whatever shape they were saved with.
- NOT changing `AgentStatus` enum values, Kafka topic schemas, or the executor's phase allowlist.
- NOT changing how `trigger_count` or `retry_count` are incremented at spawn time (spec 011, spec 015 still own that path).

## Desired Behavior

1. When the result writer would today escalate to `phase: human_review` because the agent emitted `needs_input` (status went straight to `human_review` on the first result), the writer also sets `assignee: ""`. Phase stays `human_review`.

2. When the result writer would today escalate to `phase: human_review` because `trigger_count >= max_triggers`, the writer instead leaves `phase` unchanged from what the merged frontmatter held just before the cap check (typically `planning` / `in_progress` / `ai_review`) AND sets `assignee: ""`. The Trigger Cap Escalation section is still appended exactly once, the same as today.

3. When the result writer would today escalate to `phase: human_review` because `retry_count >= max_retries`, the writer instead leaves `phase` unchanged from what the merged frontmatter held just before the retry check AND sets `assignee: ""`. The Retry Escalation section is still appended exactly once, the same as today.

4. The cap-stickiness invariant from spec 015 is preserved: once a task is parked at cap (`assignee: ""`, escalation section present), a subsequent stale agent result publish that arrives with `assignee` set or `phase` non-empty does NOT revive `assignee` and does NOT change `phase` away from the stage at which the cap fired. The escalation section is not duplicated. (Mirrors today's "phase: human_review sticky at cap" behavior, generalized.)

5. When the controller observes a task whose on-disk `assignee` transitions from empty (or absent) to a non-empty agent name — typically because the operator edited the file — it atomically resets `trigger_count` to `0` for that task before the next spawn fires. `retry_count` is also reset to `0` by the same atomic write so the per-attempt retry budget refills together with the spawn-trigger budget.

6. The reset in (5) happens exactly once per empty-to-named transition: it does not fire on every scanner pass, it does not fire when an already-named assignee is changed to a different agent name, it does not fire when the assignee goes named → empty.

7. Operator-visible escalation sections (`## Trigger Cap Escalation`, `## Retry Escalation`) keep recording the agent name that was assigned at the moment of escalation, so the body still answers "who burned the budget?" even after `assignee` is cleared in frontmatter.

## Constraints

**Must not change:**

- The atomic-write contract from spec 006 (single git writer, gitclient mutex serialization).
- The atomic increment / partial-update command shape from spec 015 / 016.
- The trigger-cap enforcement order: cap check runs before the spawn-notification short-circuit in the result writer (spec 015 completion notes documented this is load-bearing; this spec preserves it).
- The escalation-section idempotency guard (`containsEscalationSection`) — a repeated write at cap must not duplicate the section.
- The "spawn_notification consumed by current_job branch" path that deletes the marker after writing.
- `phase: human_review` is still outside the executor's `allowedPhases` — the executor still won't spawn for it. The cap-reached lifecycle stages (`planning` / `in_progress` / `ai_review`) ARE inside the allowlist; the empty-`assignee` skip filter in `vault_scanner.go` (currently the assignee-emptiness check inside `vault_scanner.skipReason` / equivalent) is what stops re-spawn for those, not the phase filter.
- Existing test-coverage shape (`pkg/result/result_writer_test.go` Ginkgo Contexts: `retry counter`, `needs_input result`, `trigger_count cap escalation`) — tests are extended, not replaced.

**Must not regress:**

- Spec 010: `needs_input` still routes to `human_review` on the first result, retry counter is not incremented.
- Spec 011: executor still bumps `retry_count` at spawn time; controller does not bump it.
- Spec 015: `trigger_count >= max_triggers` still hard-stops the spawn loop (executor's cap filter is the first line; the empty-`assignee` write here is the persistent terminal state).
- A task already parked (escalation section present, `assignee: ""`) that receives another stale agent result publish stays parked: phase unchanged, assignee stays empty, no duplicate section.

**Relevant docs:**

- `docs/kafka-schema-design.md` (Status Mapping table — already updated 2026-05-10; this spec is the controller-side implementation of that table).
- `docs/task-flow-and-failure-semantics.md` — must be updated: empty assignee is the inbox signal; `phase: human_review` semantics narrowed.
- `docs/controller-design.md` — must document the assignee-clear on escalation and the empty-to-named reset.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Agent emits `needs_input` (first result) | `phase: human_review`, `assignee: ""`, no retry counter bump | Operator re-delegates by setting `assignee` to an agent name; controller resets `trigger_count: 0`. |
| `trigger_count` reaches `max_triggers` | `phase` unchanged (lifecycle stage preserved), `assignee: ""`, `## Trigger Cap Escalation` appended once | Operator fixes underlying flake, re-delegates; controller resets `trigger_count: 0`. |
| `retry_count` reaches `max_retries` | `phase` unchanged, `assignee: ""`, `## Retry Escalation` appended once | Same as above. |
| Stale agent result arrives at a task already at cap (assignee already `""`) | Phase stays at the cap-fired stage; assignee stays `""`; escalation section count stays 1 | None — terminal state. |
| Stale agent result arrives with `assignee: <agent>` field set after escalation | Result writer ignores the agent's `assignee` field and re-clears it to `""` (cap stickiness mirrors today's phase stickiness) | None — terminal state. |
| Operator edits parked task: `assignee` empty → `claude` | Controller resets `trigger_count: 0` and `retry_count: 0` exactly once; executor sees fresh budget on next spawn | None — designed path. |
| Operator edits parked task: `assignee: claudeA` → `assignee: claudeB` (named → named) | No reset. Counters carry over. | If operator wants a fresh budget they clear assignee first then set the new name. (Documented escape hatch.) |
| Operator clears assignee manually on a non-parked task | No reset (named → empty is not the trigger). On next operator re-delegation the empty → named path resets counters. | None. |
| Two scanner passes observe the same empty → named transition | Reset emitted only on the first observation. Idempotent on the second pass via the same mechanism scanner uses for hash-based change detection today. | None. |
| Vault file written with `assignee` absent (no key) instead of `assignee: ""` | Treated as empty — same as `""`. Both forms are inbox-eligible. | None. |
| Race A: agent result publish writes first, then operator re-delegate | Escalation section appended, assignee re-cleared (cap stickiness). Operator's re-delegation then triggers reset on next scanner pass. | Bounded; converges to fresh-budget post-edit state. |
| Race B: operator edit lands first, then in-flight agent result | Controller resets counters; executor spawns. The in-flight agent result writes against a non-cap state and goes through the normal result path. | Bounded; converges to whatever the agent reports next. |

## Security / Abuse Cases

No new attack surface. The `assignee` field has always been operator-controlled in the vault. This spec adds:

- Controller-emitted writes that clear `assignee` (only on escalation paths the controller already owns).
- Controller-emitted writes that reset `trigger_count` / `retry_count` to 0 (only when scanner observes empty → named transition; bounded to one reset per transition).

An attacker with vault write access could already set any frontmatter field to any value — that trust boundary is unchanged. The reset is bounded by the empty-to-named guard, so an attacker can't trigger an unbounded reset loop just by editing assignee back and forth.

## Acceptance Criteria

- [ ] Unit test: `needs_input` result writes `phase: human_review` AND `assignee: ""`. (Extends the existing `needs_input result` Context.)
- [ ] Unit test: `trigger_count >= max_triggers` writes `assignee: ""` and leaves `phase` at the value the merged frontmatter held before the cap check (verified for `phase: ai_review`, `phase: in_progress`, `phase: planning` cases).
- [ ] Unit test: `retry_count >= max_retries` writes `assignee: ""` and leaves `phase` at the pre-check value (same three phase variants).
- [ ] Unit test: stale agent result at a task already parked (escalation section present, `assignee: ""`) keeps assignee empty, phase unchanged, escalation count stays at 1, even if the incoming `req.Frontmatter` carries `assignee: claude` and a different `phase`.
- [ ] Unit test: escalation section text still records the assignee that was active at escalation time, not the cleared value.
- [ ] Unit test: operator edit `assignee: ""` → `assignee: claude` triggers exactly one frontmatter reset of `trigger_count: 0` and `retry_count: 0`.
- [ ] Unit test: assignee `claude-a` → `claude-b` (named → named) does NOT trigger reset.
- [ ] Unit test: assignee `claude` → `""` (named → empty) does NOT trigger reset.
- [ ] Unit test: scanner observing the empty → named transition twice (e.g. due to hash change replay) emits the reset only once.
- [ ] Existing escalation-section idempotency test (single-section append at cap) still passes unchanged in `result_writer_test.go`.
- [ ] Existing retry-counter sticky-behavior test ("does not increment retry_count when phase is human_review") still passes unchanged.
- [ ] Existing `phase: human_review` stickiness test at cap still passes (renamed/extended to also assert `assignee: ""`, not replaced).
- [ ] `docs/task-flow-and-failure-semantics.md` updated: empty `assignee` is the inbox flag; `phase: human_review` reserved for genuine human-style work.
- [ ] `docs/controller-design.md` updated: escalation writes clear assignee; empty → named transition resets counters.
- [ ] `CHANGELOG.md` under `## Unreleased`: operator-visible behavior change — parked tasks now surface with empty assignee; phase no longer flips to `human_review` on cap escalations.
- [ ] `make precommit` passes in `task/controller`.
- [ ] No new scenario test (existing unit + integration coverage in the Ginkgo suite is sufficient — assignee/phase/counter behavior is fully observable in `result_writer_test.go` and `vault_scanner_test.go` against fake gitclient and synthetic vault files; no Docker, `gh`, or live cluster needed).

## Verification

```
cd task/controller && make precommit
```

Manual smoke on dev (post-deploy):

1. Find or create a deterministically-failing task (reuse pr-reviewer `gh auth` reproducer pattern from spec 015).
2. Wait for `trigger_count` to reach `max_triggers`.
3. Confirm task file frontmatter shows `assignee: ""` (or empty) and `phase` is still at the lifecycle stage from before failure (NOT `human_review`).
4. Confirm `## Trigger Cap Escalation` section is appended once with the agent name recorded.
5. Edit the task file: set `assignee` back to the agent name. Push.
6. Confirm controller writes `trigger_count: 0` (and `retry_count: 0`) within one scan cycle. Confirm executor then spawns a fresh job.

## Do-Nothing Option

Cost of leaving this unfixed:

- Operator boards keep showing parked tasks under the failed agent's name, indefinitely. The morning-coffee Human-Review board (north-star vision) is not achievable with the current frontmatter shape because there's no consistent "unclaimed" signal.
- `phase: human_review` continues to mean four different things at once (cap, retries, needs_input, untrusted-author at creation), so any tooling that branches on phase has to also read the body for context — fragile and error-prone.
- Re-delegation requires manual counter reset (or the operator must edit `trigger_count: 0` themselves), which is friction in the recovery path that drives the do-nothing option to: most parked tasks just die quietly.

This is reasonable to defer if the operator inbox is not yet a primary workflow. It is not reasonable to defer once any operator UI starts depending on the new doctrine — the task-orch filter spec is already approved on that side, so the controller is the bottleneck.

## References

- `docs/kafka-schema-design.md` — Status Mapping table (canonical for the new shape, updated 2026-05-10)
- `~/Documents/Obsidian/Personal/50 Knowledge Base/Agent Pipeline Concept.md` — "Two orthogonal axes" + "Before / after" sections
- `~/Documents/Obsidian/Personal/24 Tasks/Make Stalled PR-Reviewer Tasks Visible to Operator.md` — driving operator need
- `specs/completed/006-result-writer-conflict-resolution.md` — single-writer / serialized git invariant
- `specs/completed/010-failure-vs-needs-input-semantics.md` — `needs_input` vs `failed` precedent (must be respected)
- `specs/completed/011-retry-counter-spawn-time-semantics.md` — executor owns `retry_count` bump
- `specs/completed/015-atomic-frontmatter-increment-and-trigger-cap.md` — `trigger_count` / `max_triggers` and cap-stickiness invariant
- `specs/completed/016-partial-frontmatter-publishers.md` — atomic partial-update primitive used by the reset path
- Sibling task-orch operator-inbox filter spec (already approved, separate repo) — depends on this controller-side change to land
- Out-of-scope: [[Auto-Retry Transient Agent Failures Before Human Review]] (`failure_class` field), maintainer-repo untrusted-author spec
