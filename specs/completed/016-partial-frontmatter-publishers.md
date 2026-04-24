---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-04-24T09:56:21Z"
generating: "2026-04-24T09:56:21Z"
prompted: "2026-04-24T10:01:31Z"
completed: "2026-04-24T11:20:58Z"
branch: dark-factory/partial-frontmatter-publishers
---

## Summary

- The executor still publishes full-frontmatter rewrites from spawn-notification and failure paths, which silently clobber the atomic `trigger_count` increments introduced in spec 015.
- Observed in dev: 14 successful `increment-frontmatter` commands yet `trigger_count` pinned at 1 in the task file, so the trigger cap never fires and jobs spawn indefinitely.
- Migrate every executor publisher that mutates only a few frontmatter fields to the existing `UpdateFrontmatterCommand`, which touches only the keys it names.
- Delete the unused `PublishRetryCountBump` method to eliminate a latent clobber hazard.
- Verify end-to-end with the reproducer task: exactly `max_triggers` spawns, then `phase: human_review`, with `trigger_count` equal to `max_triggers` in the vault file.

## Problem

Spec 015 introduced atomic frontmatter commands (`IncrementFrontmatterCommand`, `UpdateFrontmatterCommand`) so concurrent writers could no longer clobber each other's mutations. The controller side works: the increment executor correctly reads the current file, bumps `trigger_count`, and writes it back. Metrics show 14 such increments succeeded in dev for the reproducer task.

But the trigger cap still did not fire and the executor kept spawning jobs. Investigation found that the executor's spawn-notification publisher (and the failure publisher) still emit a full-frontmatter rewrite derived from the in-memory Kafka task payload captured at consume time. Each cycle: the controller increments `trigger_count` on disk, then the executor's spawn notification rewrites the whole frontmatter using a stale copy, overwriting the just-incremented counter. Net change per cycle is zero. The counter is pinned forever, the cap is unreachable, and the loop runs until an operator intervenes.

This is the same class of bug spec 015 aimed to kill. Spec 015 shipped the primitives; this spec makes the executor actually use them.

## Goal

Every executor-side publisher that mutates a bounded set of frontmatter keys publishes an `UpdateFrontmatterCommand` carrying exactly those keys. Frontmatter fields not named by a publisher — in particular `trigger_count` — are never touched by that publisher. The trigger cap introduced by spec 015 reaches its limit and transitions the task to `phase: human_review` under sustained failure, observable end-to-end on a reproducer task.

## Non-goals

- NOT modifying the controller-side `IncrementFrontmatterExecutor` or `UpdateFrontmatterExecutor`; they work correctly per spec 015.
- NOT changing the agent's own result-write path (`TaskResultExecutor` with full-body content). That path legitimately owns the whole body and remains a full-file rewrite; any future clobber found there is a separate spec.
- NOT changing the Kafka topic schema. Both command kinds already exist on `develop-agent-task-v1-request`.
- NOT reintroducing `retry_count`. Spec 015 replaced it with `trigger_count`.
- NOT adding vault-read capability to the executor. The controller remains the single reader/writer of vault state per spec 008.

## Desired Behavior

1. The spawn-notification publish writes only `current_job`, `job_started_at`, and `spawn_notification` to the task file. No other frontmatter keys are touched.
2. The failure publish writes only `status`, `phase`, `current_job` (plus the existing failure-body mutation, if any) to the task file. No other frontmatter keys are touched. In particular, `trigger_count` is not carried in any executor-originated update.
3. A sequence of atomic increment followed by spawn notification preserves the incremented `trigger_count` on disk. Repeated cycles accumulate monotonically toward the cap.
4. When the reproducer task exceeds `max_triggers`, the controller transitions it to `phase: human_review` and no further jobs spawn. The final on-disk `trigger_count` equals `max_triggers`.
5. The retry-count bump publisher is no longer part of the executor's publisher surface; any stray caller fails to compile.
6. During a mid-deploy rollout, any single in-flight full-frontmatter update from the old executor is accepted by the controller without error. Steady-state behavior is restored on the next cycle.

## Constraints

- The ordering invariant from specs 011 and 015 is preserved: the spawn-notification publish MUST complete synchronously before `SpawnJob` is invoked. The switch from full-frontmatter to `UpdateFrontmatterCommand` goes through the same synchronous Kafka send path and does not change that ordering.
- The executor remains stateless relative to the vault. It publishes commands and does not read task files.
- `UpdateFrontmatterCommand` and `IncrementFrontmatterCommand` are already part of the controller's command set (spec 015). No new command kind is added.
- Existing controller-side no-op-on-empty-updates behavior is relied on for safety; it is not modified.
- See `docs/task-flow-and-failure-semantics.md` and spec `specs/in-progress/015-atomic-frontmatter-increment-and-trigger-cap.md` for the architectural boundary and existing command semantics.

## Design Decisions

1. Delete `PublishRetryCountBump` now rather than leaving it as silently deprecated. An interface method with no callers but clobber-capable behavior is a loaded foot-gun; removing it forces reviewers to think before reintroducing retry-count writes.
2. Both `PublishSpawnNotification` and `PublishFailure` use `UpdateFrontmatterCommand`. Both are partial mutations; neither legitimately touches `trigger_count`.
3. The executor does not read the current file frontmatter. It publishes only the keys it is responsible for. The controller merges on write.
4. The migration is not coordinated with a schema change. Any in-flight full-frontmatter message produced by the old executor binary is consumed normally by the existing `TaskResultExecutor` and applied once. One-time transient, deploy-bounded.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Rolling deploy has one executor still running old full-frontmatter publisher | Controller consumes that message via existing `TaskResultExecutor`; at most one clobber per task per deploy | Next cycle's increment restores monotonic progress toward cap |
| `UpdateFrontmatterCommand` for spawn notification fails to deliver between the increment and the job spawn | Spawn notification lost; `current_job` not recorded; job informer later reports job done and synthesizes a failure; executor re-spawns on the next cycle | Same behavior as today when spawn-notification delivery fails; no new regression |
| Publisher is invoked with an empty Updates map | Controller's update executor is a no-op for empty maps per spec 015 | No harmful write; emit the existing no-op metric |
| Reproducer task already has `trigger_count` above `max_triggers` at deploy time | Controller transitions to `phase: human_review` on the next controller tick; no further spawns | Operator resets `trigger_count: 0` and `phase: in_progress` if re-verification is needed |

## Security / Abuse Cases

Not directly applicable. No new trust boundary, no new user-controlled input. The change narrows — does not widen — the set of frontmatter keys an executor publish can mutate, which reduces blast radius.

## Acceptance Criteria

- [ ] Spawn-notification publish emits an `UpdateFrontmatterCommand` whose Updates map contains exactly `current_job`, `job_started_at`, and `spawn_notification`. Unit test asserts the published operation kind and the exact key set (no `trigger_count`, no `status`, no `phase`).
- [ ] Failure publish emits an `UpdateFrontmatterCommand` whose Updates map contains exactly `status`, `phase`, `current_job`. Unit test asserts the exact key set and that `trigger_count` is absent from the Updates map. Body content is NOT mutated by this publisher — any body writes remain the responsibility of the agent's own result publish through `TaskResultExecutor`.
- [ ] `PublishRetryCountBump` is removed from the publisher interface and implementation. A reintroduced caller fails to build.
- [ ] Integration test: publish `IncrementFrontmatterCommand` followed by a spawn-notification `UpdateFrontmatterCommand` against the controller; assert `trigger_count` on disk equals the post-increment value.
- [ ] Scenario (end-to-end): run the reproducer task that fails every cycle (e.g. the `gh-auth-unauth` pattern from `e2e-test-pr-reviewer-trigger-cap-20260424.md`). Observe exactly `max_triggers` spawns, then `phase: human_review` with `trigger_count == max_triggers` in the on-disk task file. No further jobs spawn.
- [ ] `cd task/executor && make precommit` passes.
- [ ] `docs/task-flow-and-failure-semantics.md` updated to list, for each executor publisher, which command kind it emits.
- [ ] `CHANGELOG.md` entry references this spec and the spec 015 foundation.

## Verification

```
cd task/executor && make precommit
```

End-to-end verification on the reproducer task, after deploy:

1. Reset the task file to `phase: in_progress`, `status: todo`, `trigger_count: 0`.
2. Let the controller process it.
3. Observe controller metrics: `agent_task_controller_frontmatter_commands_total{operation="increment-frontmatter",outcome="success"}` increments each cycle; `trigger_count` on disk increments 1:1.
4. After `max_triggers` cycles: task transitions to `phase: human_review`; executor publishes no further spawn notifications.
5. Confirm `trigger_count == max_triggers` in the vault file.

## Do-Nothing Option

Ship nothing: spec 015's primitives remain unused by two of the three partial-update publishers, the trigger-cap loop remains unreachable, and any task whose agent fails deterministically spawns jobs indefinitely until an operator disables it. This defeats the entire point of spec 015 and has already reproduced in dev. Not acceptable.
