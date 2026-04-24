---
tags:
  - dark-factory
  - spec
status: draft
---

## Summary

- Introduce an atomic increment command for task frontmatter fields, decoupled from the fragile "rewrite whole file → git commit" path that fails when the diff is idempotent.
- Replace the executor's current `retry_count` bump with an `IncrementFrontmatter("trigger_count", +1)` command; the new counter unambiguously counts spawn-trigger events, independent of job outcome.
- Add an executor-side filter that refuses to spawn when `trigger_count >= max_triggers` (default 3), preventing indefinite per-minute job-spawn loops.
- Fixes the observed symptom where one pr-reviewer task spawned K8s Jobs every minute for 10+ minutes despite `retry_count: 1` and `max_retries: 3` already set in the task file.
- Addresses three unconfirmed root causes at once (idempotent commit on counter bump, frontmatter merge clobber, missing event publish) by making the counter bump structurally unable to be idempotent.

## Problem

A pr-reviewer-agent task in dev spawned a K8s Job every minute for over 10 minutes even though `retry_count: 1` was persisted in its frontmatter and `max_retries: 3` was the cap. The expected escalation — `retry_count >= max_retries` → `phase: human_review` → executor's phase-allowlist filter rejects — never fired. Manual intervention was needed (editing the task file frontmatter to `status: failed, phase: done`).

Evidence from controller logs showed three vault writes per cycle for the same task:

1. retry_count bump (frontmatter keys=10)
2. spawn_notification annotation (frontmatter keys=11)
3. agent result write (frontmatter keys=2) — reproducibly fails with `git commit failed: nothing to commit, working tree clean: exit status 1`

The agent produces byte-identical failing output every cycle (`Status: failed / gh auth unauthenticated`), so write (3) is idempotent. Kafka offsets on `develop-agent-task-v1-request` advanced by ~3 per minute for this one task, driven by the controller's `sync_loop` republishing on every vault file change.

Root cause is one of three unconfirmed possibilities:

1. The retry_count bump write itself fails due to an idempotent git commit, so the counter never persists.
2. The controller's frontmatter merge clobbers the bumped retry_count with stale values from a subsequent partial-update write.
3. The counter-bump event is not published correctly.

Any fix that relies on "rewrite the file, hope the commit isn't idempotent" is fragile. A counter bump must, by construction, always produce a net state change on disk and in git.

## Goal

The system has a spawn cap that is robust against all three possible root causes and against future frontmatter merge regressions. A task that keeps producing the same failure cannot re-spawn more than a small, configurable number of times. An operator reading a task's frontmatter can tell how many spawn triggers have fired for it, independent of how many of those spawns succeeded, failed, or were cancelled.

## Non-goals

- NOT fixing `gitclient.commitAndPush`'s "nothing to commit" error as the primary mechanism. Atomic increment makes the counter write never idempotent, so the symptom goes away at the counter-write site. Idempotent agent-result writes may still produce benign "nothing to commit" errors — a separate follow-up spec if operational noise justifies it.
- NOT introducing BoltDB or any local counter state in the executor. State stays in the vault file, event-driven through Kafka → controller → vault.
- NOT addressing the sync_loop republish amplification (≈3 Kafka events per cycle for one task). Orthogonal concern, tracked separately if it matters after this fix.
- NOT removing `retry_count` in this spec (see open question 3 for migration approach).
- NOT changing the Kafka topic schema in a breaking way. New command kinds piggyback on the existing `agent-task-v1-request` topic, matching the pattern established by spec 011.

## Desired Behavior

1. The controller exposes an **atomic increment** operation on frontmatter fields. When applied, it reads the current value, adds a delta, and writes the new value — all inside the gitclient mutex — so concurrent events cannot lose increments and the written value always differs from the read value (no idempotent-commit failure path for the counter).
2. The controller exposes an **atomic partial-update** operation that sets specific frontmatter keys without touching other keys. This replaces the current whole-file-rewrite path for small frontmatter edits and removes the clobber risk from interleaved writes.
3. The executor publishes an increment command for a `trigger_count` field before calling the K8s Job spawner. The increment MUST be durable (controller ACK / Kafka commit) before `SpawnJob` is called.
4. Before publishing the increment, the executor checks the current `trigger_count` against `max_triggers` (default 3). If `trigger_count >= max_triggers`, the executor skips the spawn, does not publish the increment, and records a `skipped_trigger_cap` metric.
5. `trigger_count` semantics: it counts spawn-trigger events. It is incremented exactly once per (attempted) spawn, independent of whether the Job succeeded, failed, was synthesised-failed by the informer, or was manually deleted. This is distinct in name and meaning from `retry_count`.
6. `max_triggers` is a frontmatter field with a default of 3 when absent. Humans can raise it by editing the task file (same pattern as today's `max_retries`).
7. The cap-reached terminal state is explicit and operator-visible: when `trigger_count` reaches `max_triggers`, the controller atomically sets `phase: human_review` (mirroring the current `applyRetryCounter` escalation). The executor's existing phase-allowlist filter then rejects any further events for that task, even if the `sync_loop` republishes it.
8. Existing E2E tests that rely on `retry_count` escalation continue to pass through a silent-deprecation migration: `retry_count` stays readable in frontmatter but is no longer bumped; it is removed in a subsequent release after one release cycle of coexistence.

## Constraints

- Controller remains the single writer to the vault git repo. Executor never writes git directly; it publishes commands on `agent-task-v1-request`.
- Atomic increment and atomic partial-update MUST run inside the gitclient mutex. No new lock.
- Ordering invariant: executor publishes `IncrementFrontmatter(trigger_count, +1)` BEFORE `kubectl create Job`. If the increment publish fails, no Job is created. (Same invariant spec 011 established for the current retry bump.)
- Over-count tolerance: if Job creation fails after the increment is durable, counter stays bumped by 1. Over-count per spawn attempt is bounded by 1; `max_triggers` absorbs it. Documented tradeoff, not observable behaviour.
- No breaking Kafka schema change. New command kinds extend `agent-task-v1-request`.
- Default `max_triggers = 3` matches today's `max_retries` default.
- Existing atomic-write guarantees from spec 011 are preserved. This spec strengthens them; it does not weaken them.
- Relevant existing docs: `docs/task-flow-and-failure-semantics.md`, `docs/controller-design.md`. Both must be updated to document the new commands and the counter semantics.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Executor publishes IncrementFrontmatter, Kafka broker unreachable | Executor logs error, does NOT spawn Job. Next event cycle retries. | No stuck state; task stays in executor allowlist until publish succeeds. |
| Publish OK, K8s create Job fails | Counter bumped by 1, no Job runs. Next cycle may bump again. | Counter reaches `max_triggers` → task leaves executor allowlist (per question 4). Correct signal. |
| Two executor events for same task arrive close together | Existing `IsJobActive` idempotency prevents duplicate spawn. Only one increment + spawn pair runs. | Same as today. |
| Agent produces byte-identical failing output every cycle (reproducer of original bug) | Counter still increments (atomic, never idempotent). After `max_triggers` cycles, executor skips further spawns. | Bounded loop; no manual intervention needed. |
| Controller crashes between applying increment and publishing ACK/event | On restart, controller re-scans vault, sees bumped `trigger_count`, re-emits event. Executor sees no active Job, re-enters spawn path, checks cap. | Max over-count = 1 per crash. Acceptable. |
| Two concurrent IncrementFrontmatter commands for same task | Gitclient mutex serialises them. Both increments apply. | Monotonic counter; no lost increment. |
| Legacy task with `retry_count` set but no `trigger_count` | Treated as `trigger_count = 0`. First spawn bumps to 1. | Migration behaviour per question 3. |
| Human edits task file to `trigger_count: 999` | Executor skips spawn (cap reached). Task stays where the human parked it. | Same escape hatch as today's manual edit. |

## Security / Abuse Cases

No new attack surface. The executor already publishes on `agent-task-v1-request`; this adds one command kind on an existing authenticated channel. An attacker with Kafka produce access to that topic could already forge any task update; this spec does not widen that. The cap is strictly an internal safety rail; a bypassing attacker is already past a stronger trust boundary.

## Design Decisions

Decisions on previously-open questions, recorded with rationale so the spec is self-contained at approve time.

1. **Cap enforcement order — Decision: check-then-increment.** Under the single-executor + single-Kafka-partition constraint (line 61), there is no concurrency on the check, and check-first avoids publishing a useless increment command when the cap is already reached. Cheaper on hot-path and on Kafka traffic.
2. **Reset semantics for `trigger_count` — Decision: reset only on human rewrite.** A human editing the task file already can set any frontmatter value. Auto-reset on status/phase transitions would silently re-arm the loop for tasks that the system itself transitioned (e.g. a controller reopening an aborted task). Keeping the counter sticky across automated transitions preserves the "this task has already burned N triggers" signal — matching the counter's stated independence from job outcome (line 54).
3. **Migration / coexistence with `retry_count` — Decision: silent deprecation over one release.** This release: controller still reads `retry_count` for escalation compatibility but stops bumping it; executor bumps only `trigger_count`. Next release: remove `retry_count` handling. Existing vault tasks are not rewritten; they accumulate `trigger_count` from first spawn. No migration script needed.
4. **Cap-reached behaviour — Decision: atomically escalate phase to `human_review`.** Matches the existing `result_writer.applyRetryCounter` pattern from spec 011, stays operator-visible in the vault, and uses the already-established phase-allowlist filter as the hard stop. The executor-side cap filter is the first line of defence; the phase escalation is the persistent terminal state.
5. **Atomic read-increment-write placement — Decision: new command handler in the controller, taking the gitclient mutex explicitly.** Keeps the gitclient layer git-generic (no frontmatter-semantic creep). The frontmatter-aware read/parse/increment/marshal/write lives with the rest of the controller's frontmatter logic. Gitclient exposes the mutex / an `UnderLock(func())` helper; the handler uses it.

## Acceptance Criteria

- [ ] Unit tests: atomic `IncrementFrontmatter` under the controller's lock produces a monotonic sequence across sequential command events with no lost increments.
- [ ] Unit tests: concurrent `IncrementFrontmatter` commands for the same task produce exactly `N` net increments for `N` commands (no lost updates).
- [ ] Unit tests: atomic `UpdateFrontmatter(key → value)` changes only the named keys; other frontmatter keys are byte-identical before and after.
- [ ] Unit tests: executor skips spawn when `trigger_count >= max_triggers`. Metrics label `skipped_trigger_cap` increments. No `IncrementFrontmatter` is published. `SpawnJob` is not called.
- [ ] Unit tests: executor publishes `IncrementFrontmatter(trigger_count, +1)` strictly before `SpawnJob`. If publish fails, `SpawnJob` is not called.
- [ ] Integration test: a deterministically-failing task spawns exactly `max_triggers` times. All subsequent Kafka events for that task are filtered (no further spawns). Task ends with `phase: human_review` (frontmatter-only change, no body/content mutation).
- [ ] Integration test: a task with the reproducer pattern from the original bug (byte-identical agent output every cycle) terminates within `max_triggers` spawns.
- [ ] `docs/task-flow-and-failure-semantics.md` updated: new counter, new commands, cap behaviour.
- [ ] `docs/controller-design.md` updated: atomic frontmatter commands are documented as part of the writer contract.
- [ ] `CHANGELOG.md` under `## Unreleased` notes the operator-visible change: new `trigger_count` / `max_triggers` fields; retry loop is now bounded.
- [ ] Pre-existing E2E tests still pass. Tests that assert `retry_count` bumping are migrated to assert `trigger_count`; tests that assert `retry_count >= max_retries` escalation are migrated to assert `trigger_count >= max_triggers` escalation. `retry_count` is still readable in frontmatter during this release (silent deprecation).

## Verification

```bash
cd task/controller && make precommit
cd task/executor && make precommit
cd lib && make precommit
```

Manual / E2E on dev:

1. Deploy controller + executor with this change.
2. Create a task that deterministically fails the same way every cycle (e.g. reuse the pr-reviewer `gh auth unauthenticated` scenario).
3. Wait ~10 minutes.
4. Confirm exactly `max_triggers` Jobs were created for the task (check K8s Job history by `task-id` label).
5. Confirm the task's frontmatter shows `phase: human_review` and `trigger_count == max_triggers`.
6. Confirm no further Jobs spawn over a 24h window.
7. Confirm `skipped_trigger_cap` metric is non-zero for this task.

## Do-Nothing Option

Cost of leaving this unfixed:

- Any task that hits a reproducibly-idempotent agent-result write (misauth, missing env, deterministic parse error) becomes a per-minute K8s Job spawn loop until a human notices. The pr-reviewer incident already demonstrates this in dev.
- The three candidate root causes (idempotent commit, frontmatter merge clobber, missing publish) all map to "counter didn't persist" and are individually hard to isolate. Adding more logging without changing the write semantics costs investigation time on every recurrence.
- `retry_count` as currently named and used mixes "attempts made" and "failure events observed" semantics across spec 008/010/011 history. A second counter with unambiguous semantics closes that category of confusion instead of paying it down field-by-field.

Not doing this is reasonable if pr-reviewer-style idempotent failures are believed to be once-a-year events. The on-call cost of 10+ Jobs/minute is bounded (cluster survives; operator edits the file). But the fix is small (one atomic primitive, one counter, one filter) and shuts down a whole class of spawn-loop bugs.

## References

- `specs/completed/008-task-retry-protection.md` — original retry counter design
- `specs/in-progress/010-failure-vs-needs-input-semantics.md` — earlier escalation guard work
- `specs/completed/011-retry-counter-spawn-time-semantics.md` — moved `retry_count` bump to executor spawn path (this spec extends that model with a more robust primitive)
- `docs/task-flow-and-failure-semantics.md` — must be updated
- `docs/controller-design.md` — must be updated
- Obsidian task: `~/Documents/Obsidian/Personal/24 Tasks/Fix agent-task-executor indefinite retry loop and result_writer idempotent commit failure.md`
- Evidence task: `~/Documents/Obsidian/OpenClaw/tasks/e2e-test-pr-reviewer-gh-auth-v12-20260423.md`
- pr-reviewer incident task id: `edef6449-dea1-4df1-b0d2-96a4789ba32c` (dev)
