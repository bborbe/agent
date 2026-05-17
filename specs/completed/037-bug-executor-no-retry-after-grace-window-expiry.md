---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-05-17T10:17:26Z"
generating: "2026-05-17T10:18:40Z"
prompted: "2026-05-17T10:39:28Z"
verifying: "2026-05-17T11:17:50Z"
completed: "2026-05-17T13:45:14Z"
branch: dark-factory/bug-executor-no-retry-after-grace-window-expiry
---

## Summary

- Spec 036 introduced a 300s grace window in `task/executor/pkg/handler/task_event_handler.go` `checkActiveCurrentJob`: when `current_job` is set and the k8s Job is inactive but less than 300s has elapsed since `job_started_at`, the executor suppresses respawn to avoid racing the agent's terminal-phase write.
- The gate works correctly for the suppression case, but the executor is purely event-driven: once a suppression decision is made, nothing schedules a follow-up evaluation. If no further task event arrives for that task, the suppressed retry never fires.
- The agent only clears `current_job` on **terminal** phase writes (`done`, `human_review`). Mid-flow transitions (e.g. `planning → in_progress`) keep `current_job` set, so the task event corresponding to the mid-flow transition is the ONLY event during the grace window — and it gets suppressed. The well then runs dry: tasks remain `phase=in_progress` indefinitely with no second pod.
- Concrete prod evidence 2026-05-17: task `cbe79223-b4da-5861-aaa5-1c936fec90b0` (PR Review trading #128). Pod 1 succeeded at 09:33:56Z after the planning phase; the only task event after pod 1 succeeded was suppressed at 09:34:24Z (`elapsed=59s`); grace expired at ~09:38:25Z; >2 hours later the task is still `phase=in_progress` and no second pod has spawned. PR remained in `REVIEW_REQUIRED` with no reviews posted.
- Fix: after the grace window expires, the executor must re-evaluate stuck tasks autonomously rather than waiting for a Kafka event that may never arrive.

## Problem

The respawn grace window introduced by spec 036 assumes that fresh task events continue to flow after a suppression decision. In practice, during the agent's own mid-flow phase transitions (e.g. planning result publish), the task event triggered by the transition arrives DURING the grace window and is suppressed. Because the agent only clears `current_job` on terminal writes, no further events for that task are produced, and the executor — being purely event-driven — never re-checks the task. The task is permanently stuck: pod 1 succeeded, the agent has more work to do (phase is non-terminal), but the executor will not spawn pod 2 until a human edits the task file to trigger a fresh Kafka event.

The 2026-05-17 prod incident on task `cbe79223-...` proves this is not theoretical: a PR review task remained `phase=in_progress` with no second pod for >2 hours after the planning pod succeeded, and the PR was never reviewed.

## Goal

After this fix, when the executor suppresses a respawn due to the grace window, it MUST guarantee a re-evaluation of the same task after the grace period elapses, without depending on a fresh Kafka event arriving. A task whose phase is non-terminal and whose current_job has exited cleanly is retried within a bounded time of pod 1's exit, eliminating the "stuck forever" failure mode demonstrated on 2026-05-17. Spec 036's suppression behavior during the grace window is preserved unchanged.

## Non-goals

- Not changing spec 036's grace duration (300s) or its terminal-phase semantics.
- Not changing the agent's terminal-write protocol or the rule that `current_job` is cleared only on terminal phase writes.
- Not introducing distributed locking, Kafka transactions, or any cross-component coordination.
- Not modifying the spec 035 terminal-phase gate.
- Not changing the `job_watcher`'s existing succeed/fail publish semantics for tasks still present in the `TaskStore`.

## Reproduction

**Triggering incident (verbatim evidence on file):**

- Task UUID: `cbe79223-b4da-5861-aaa5-1c936fec90b0`
- Task file: `~/Documents/Obsidian/OpenClaw/tasks/PR Review github - bborbe-trading - 128 - ff8c8cbf - bump-kafka-topic-reader-v1-6-19-to-v1-6-20.md`
- PR: `bborbe/trading` #128, head SHA `ff8c8cbf`, status `REVIEW_REQUIRED`
- Pod 1: `pr-reviewer-agent-cbe79223-20260517093325` (started 09:33:25Z; planning completed 09:33:54Z writing `phase: in_progress`; pod succeeded 09:33:56Z; `current_job` NOT cleared because phase is non-terminal)
- Spawn check at 09:34:24Z (driven by the planning-result task event):
  `event=respawn_grace_window task=cbe79223-... current_job=pr-reviewer-agent-cbe79223-20260517093325 elapsed=59s` — correctly suppressed per spec 036
- Grace window expired at ~09:38:25Z
- 09:38:25Z onward: ONLY `job_watcher` log lines fire (every ~5 min — informer resync — restating pod 1 succeeded). These do NOT route through `spawnIfNeeded`.
- 11:41Z (≈2h after grace expiry): task still `phase=in_progress`, no second pod ever spawned, PR review never completed, no reviews/comments posted on PR #128.

**Minimal in-process reproduction:**

1. Construct a `lib.Task` with `current_job: pod-A`, `job_started_at: <now - 30s>`, `phase: in_progress` (non-terminal).
2. Configure the fake `JobSpawner` so `IsJobActive(taskID)` returns `false`.
3. Invoke the handler once. Per spec 036, suppression fires and no spawn occurs.
4. Do not deliver any further Kafka event for this task.
5. Advance the clock by `defaultRespawnGracePeriod + 60s`.
6. Today: nothing happens; the task is stuck.
7. Expected after fix: the executor autonomously re-evaluates this task within a bounded interval after grace expiry, observes `IsJobActive=false` and `elapsed > grace`, and proceeds to spawn pod 2.

## Expected vs Actual

**Expected:** After the spec 036 grace window suppresses a respawn, the executor re-evaluates the same task once the grace period elapses, without requiring a fresh Kafka event. A non-terminal task whose pod has exited cleanly is retried within a bounded time of grace expiry.

**Actual (observed 2026-05-17T09:38Z prod):** The executor never re-evaluates. The task remains stuck at `phase=in_progress` indefinitely; the only events for that task during the grace window have already been consumed and suppressed, and no further events are produced because the agent does not clear `current_job` on non-terminal phase transitions.

## Why this is a bug

1. **Liveness.** A task that the agent has not finished with (phase non-terminal) must eventually be picked up again. The current behavior breaks this guarantee for the most common case: an agent that succeeded its current phase and is waiting for the executor to spawn the next phase. There is no operator-visible cue that the task is stuck other than the absence of expected downstream artifacts.
2. **Operator diagnosability.** Logs after grace expiry show only `job_watcher` informer resyncs ("succeeded — agent likely published result already") with no signal that a deferred retry is pending or has failed to fire. The stuck state is silent.
3. **End-user impact.** PR #128 was not reviewed for >2 hours after the planning pod succeeded; the entire pr-reviewer pipeline stopped silently. Any Pattern-B agent with multi-phase work (plan → execute → review) is exposed.

## Desired Behavior

1. When `checkActiveCurrentJob` returns `(suppress=true)` due to the grace window (i.e. not due to `active=true`), the executor MUST guarantee a follow-up evaluation of the same task after grace expiry, without depending on a fresh Kafka task event.
2. The follow-up evaluation runs the same predicate as `spawnIfNeeded` against the latest known frontmatter state of the task at follow-up time. If the predicate still permits spawn (current_job set, job inactive, grace elapsed, phase non-terminal, trigger_count < max_triggers), a new pod is spawned.
3. The follow-up evaluation MUST fire within `defaultRespawnGracePeriod + R` seconds of pod 1's exit, where `R` is a bounded reconciliation overhead (agent decides at impl time; target ≤60s).
4. The follow-up evaluation MUST NOT duplicate spawns: if a fresh Kafka task event arrives in the interim and successfully spawns a new pod, the follow-up evaluation is a no-op (the gate's existing `current_job/IsJobActive/job_started_at` predicate naturally handles this — the new pod will be active or within its own grace window).
5. The follow-up evaluation MUST survive executor restarts: a stuck task created by an executor that subsequently restarted before grace expiry must still be re-evaluated by the post-restart executor. (Agent decides at impl time how to achieve this — e.g. periodic reconciliation that scans current state, or a wake-up triggered by the `job_watcher`'s existing informer events for inactive jobs whose tasks are non-terminal.)
6. A distinct metric label `respawn_after_grace_window` is recorded on the existing `metrics.TaskEventsTotal` vector each time the follow-up evaluation results in a spawn. This is distinguishable from `respawn_grace_window` (suppression) so operators can graph "stuck tasks rescued by the deferred retry" separately from "suppressions in the race window".
7. An info-level structured log line `event=respawn_after_grace_window task=<id> current_job=<job> elapsed=<seconds>` is emitted when the follow-up evaluation spawns a new pod.
8. The fix MUST NOT alter the suppression behavior inside the grace window (spec 036's contract). Existing spec 036 tests pass unchanged.

## Constraints

- The fix lands in the executor service at `task/executor/`. No changes to the agent, the task-controller, the vault-cli library, or any Kafka topic/schema.
- The grace-period constant `defaultRespawnGracePeriod` is NOT changed.
- The new metric label `respawn_after_grace_window` MUST be pre-initialised in `metrics.go` `init()` alongside the existing labels (consistent with spec 035 and 036 patterns).
- The log line MUST be info-level (`glog.Infof`), NOT `glog.V(2)`.
- The clock source MUST remain injectable so unit tests are deterministic. No `time.Now()` directly in any new predicate.
- The fix MUST NOT introduce a per-task in-memory timer that is lost on executor restart unless paired with a restart-safe reconciliation path that catches up after restart (see Desired Behavior #5).
- The fix MUST NOT increase the per-task event-driven spawn rate beyond what the existing predicate allows: any deferred re-evaluation must run the same `current_job`/`IsJobActive`/`job_started_at`/`trigger_count`/phase predicate the event path runs.
- Domain reference: `docs/task-flow-and-failure-semantics.md` defines the phase lifecycle and the `current_job`/`job_started_at` frontmatter contract; this spec extends the "Executor respawn gates" subsection added by spec 036.
- Verification ladder: Rung-1 (`make precommit` in `task/executor/`) covers unit-level correctness. Rung-2 (dev deploy via `agent-dev` worktree) and Rung-3 (prod via `agent-prod` worktree) verify the live behavior: a non-terminal task whose pod exited cleanly results in a second pod spawn within a bounded time of grace expiry.

## Failure Modes

| Trigger | Expected behavior | Detection | Reversibility | Concurrency | Recovery |
|---|---|---|---|---|---|
| Pod 1 succeeds at T+0 in mid-flow phase; only event during grace is suppressed; grace expires at T+300s | Deferred re-evaluation fires within bounded R seconds after T+300s; new pod spawns if predicate still permits; `respawn_after_grace_window` metric + log line emitted | `kubectlquant -n <stage> logs <executor-pod> \| grep respawn_after_grace_window` returns ≥1 line | n/a (spawn is the desired side effect) | Multiple executor replicas: Kafka consumer-group semantics deliver the original event to one; for the follow-up evaluation, agent decides at impl time how to avoid duplicate spawns across replicas (e.g. leader election, k8s informer-driven path that naturally fires once per Job, or rely on the existing `IsJobActive` check being idempotent so duplicate evaluations converge to one spawn) | None |
| Fresh Kafka task event arrives during the grace window and spawns a new pod before grace expiry | Deferred re-evaluation is a no-op when it eventually fires: the new pod's `current_job` differs and `IsJobActive` returns true (or new `job_started_at` is recent) | Logs show the event-driven spawn followed by deferred re-eval reaching the existing `skipped_active_job` or `respawn_grace_window` branch | n/a | n/a | n/a |
| Executor restarts during the grace window | After restart, the deferred re-evaluation MUST still occur for stuck tasks. Agent decides at impl time: either restart-safe reconciliation that scans task state, or accept a one-time event replay from Kafka offset rewind. The acceptance criterion is the observable outcome: a stuck task with grace elapsed gets a second pod within R seconds of executor coming back up | `kubectlquant -n <stage> logs <executor-pod> --since=10m \| grep respawn_after_grace_window` after a forced restart | n/a | n/a | n/a |
| Task at trigger_count >= max_triggers when deferred re-eval fires | Existing `skipped_trigger_cap` path runs; no spawn; existing metric increments. No new pathology. | Existing logs/metrics | n/a | n/a | Operator inspects task; resets trigger_count if appropriate |
| Phase becomes terminal between suppression and deferred re-eval (agent's terminal write finally propagates) | Spec 035 terminal-phase gate fires inside `parseAndFilter`; `spawn_suppressed` log emitted; no spawn | Existing spec 035 diagnostics | n/a | n/a | n/a |
| current_job cleared between suppression and deferred re-eval | Deferred re-eval finds empty current_job; falls through to normal spawn path | Standard spawn log line | n/a | n/a | n/a |
| Clock skew on the executor pod | Deferred re-eval timing may shift by the skew amount (target R ≤60s already includes margin) | n/a | n/a | n/a | n/a |
| Two stuck tasks simultaneously: deferred re-eval must handle both | Each task gets its own evaluation; no head-of-line blocking | Logs show both task ids in respawn_after_grace_window lines within bounded R seconds of each other | n/a | n/a | n/a |
| Permanent infrastructure failure (Kafka offline, vault unreachable) during deferred re-eval | Existing error path increments `error` metric; deferred re-eval may itself fail; next reconciliation cycle (if any) retries; no infinite tight loop | Existing executor error logs | n/a | n/a | Operator restores infrastructure |

## Security / Abuse Cases

Touches no HTTP, no new user input. The deferred re-evaluation reads the same frontmatter fields the event path reads (`current_job`, `job_started_at`, `phase`, `trigger_count`, `max_triggers`). No new trust boundary.

- An operator with vault write access could keep `current_job` set indefinitely on a task to force repeated deferred respawns up to `max_triggers`. This is already the existing trigger-cap design surface and is not a new exposure.
- No new code path can hang or infinite-loop: each deferred evaluation is a single predicate check guarded by `trigger_count < max_triggers`.

## Acceptance Criteria

- [ ] **Deferred re-evaluation exists in the executor** — evidence: `grep -rn 'respawn_after_grace_window' task/executor/pkg/` returns ≥1 line in a non-test file referencing a deferred-evaluation code path (timer, reconciliation tick, job-watcher hook — agent decides at impl time).
- [ ] **The new metric label `respawn_after_grace_window` is pre-initialised** — evidence: `grep -n 'respawn_after_grace_window' task/executor/pkg/metrics/metrics.go` returns ≥1 line in `init()`.
- [ ] **The new metric label is distinct from `respawn_grace_window`** — evidence: `grep -nE 'respawn_grace_window|respawn_after_grace_window' task/executor/pkg/metrics/metrics.go` returns ≥2 distinct lines.
- [ ] **An info-level structured log line is emitted when the deferred re-eval results in a spawn** — evidence: `grep -n 'event=respawn_after_grace_window' task/executor/pkg/` returns ≥1 line in a non-test file; log format includes keys `task`, `current_job`, `elapsed`; emitted via `glog.Infof` not `glog.V(...)`.
- [ ] **Unit test: suppression still fires inside grace window (spec 036 preserved)** — evidence: `cd task/executor && go test ./pkg/handler/... -v -ginkgo.v 2>&1 | grep -E 'grace window'` shows the existing spec 036 PASS rows unchanged.
- [ ] **Unit test: deferred re-eval fires after grace expiry without a fresh Kafka event** — evidence: `cd task/executor && go test ./... -v -ginkgo.v 2>&1 | grep -E 'respawn_after_grace_window|deferred'` shows ≥1 PASS row whose setup explicitly omits delivering a second task event and whose assertion is that a spawn occurs after `defaultRespawnGracePeriod + R` of simulated time advancement via the injected clock.
- [ ] **Unit test: deferred re-eval bound on R ≤60s** — evidence: ≥1 PASS row that advances simulated time to `defaultRespawnGracePeriod + 60s` and asserts spawn count == 1; a paired assertion that at `defaultRespawnGracePeriod - 1s` spawn count == 0. Proves the bound is observable, not just stated.
- [ ] **Unit test: deferred re-eval is idempotent against a concurrent event-driven spawn** — evidence: ≥1 PASS row whose setup delivers a fresh event that spawns a new pod before the deferred re-eval fires, and asserts the deferred re-eval observes the new pod's `current_job/active` and does NOT spawn a duplicate (spawn-counter remains at 1).
- [ ] **Unit test: deferred re-eval respects trigger cap** — evidence: ≥1 PASS row where `trigger_count == max_triggers` at deferred-re-eval time and asserts no spawn (existing `skipped_trigger_cap` metric increments).
- [ ] **Unit test: deferred re-eval respects terminal-phase gate (spec 035)** — evidence: ≥1 PASS row where the task's phase has become terminal between suppression and deferred re-eval, and asserts no spawn (spec 035 gate fires).
- [ ] **Clock is injectable in any new code path** — evidence: `grep -rn 'time.Now()' task/executor/pkg/handler/ task/executor/pkg/job_watcher.go` returns no NEW occurrences attributable to this spec; new code uses the existing `currentDateTime` (or equivalent injected clock) used by spec 036.
- [ ] **`make precommit` exits 0 in `task/executor/`** — evidence: exit code 0.
- [ ] **CHANGELOG entry under `## Unreleased`** — evidence: `grep -nE 'respawn_after_grace_window|deferred respawn|grace window retry' CHANGELOG.md` returns ≥1 line.
- [ ] **`docs/task-flow-and-failure-semantics.md` "Executor respawn gates" subsection updated** — evidence: `grep -n 'respawn_after_grace_window\|deferred re-evaluation\|deferred respawn' docs/task-flow-and-failure-semantics.md` returns ≥1 line under the existing "Executor respawn gates" section; the addition describes (a) why suppression alone is insufficient when the agent does not clear `current_job` on mid-flow phase transitions, (b) the bounded re-evaluation interval after grace expiry, (c) the new metric label and log line, (d) restart-safety expectation.

### Post-Deploy Verification

- [ ] **Post-Deploy (Rung-2):** the fix is live in dev and produces a deferred-respawn signal — evidence: dev executor pod is `1/1 Running` after deploy; within 24h of dev soak, `kubectlquant -n dev logs <executor-pod> --since=24h | grep respawn_after_grace_window` returns ≥1 line referencing a real task id. If organic traffic does not produce the signal within 24h, an operator may force a reproduction by resetting a dev task to `phase=in_progress` with `current_job` set and `job_started_at` recent, waiting `defaultRespawnGracePeriod + 60s`, and observing the signal — the unit tests cover the predicate; the live evidence confirms the wiring.
  - `deploy_check:` `kubectlquant -n dev get deploy/agent-task-executor -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `dev`
- [ ] **Post-Deploy (Rung-3):** the fix is live in prod with no regression — evidence: prod executor pod is `1/1 Running` after deploy; `kubectlquant -n prod logs <executor-pod> --since=24h | grep -E 'respawn_grace_window|respawn_after_grace_window'` returns ≥1 line (either gate firing is acceptable health evidence); spec 035 and 036 gates continue to fire normally.
  - `deploy_check:` `kubectlquant -n prod get deploy/agent-task-executor -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `prod`

## Scenario Coverage

None. The unit tests in `task/executor/pkg/handler/` (and wherever the deferred path lives) cover the predicate and the deferred-eval timing via the injected clock. The Rung-2 live observation covers cross-component restart-safety and informer wiring. No new dark-factory scenario is warranted.

## Verification

```bash
# Rung-1
cd task/executor && make precommit
# Expected: exit 0
```

```bash
# Rung-2 (after dev deploy via agent-dev worktree)
kubectlquant -n dev logs <executor-pod> --since=24h | grep respawn_after_grace_window
# Expected: ≥1 line for at least one task (or operator-forced reproduction)

# Sanity: spec 036 gate still fires
kubectlquant -n dev logs <executor-pod> --since=24h | grep respawn_grace_window
# Expected: ≥0 lines (gate firing organically depends on traffic; non-zero is healthy)
```

```bash
# Rung-3 (prod, after ≥1 day dev soak)
kubectlquant -n prod logs <executor-pod> --since=24h | grep -E 'respawn_grace_window|respawn_after_grace_window'
# Expected: ≥1 line in at least one of the two labels
```

## Do-Nothing Option

Not viable. The 2026-05-17 prod incident on task `cbe79223-...` (PR #128) demonstrates that real production tasks become permanently stuck whenever pod 1 succeeds in a non-terminal phase. Any multi-phase Pattern-B agent (pr-reviewer's plan→implement→review flow, backtest's setup→run→analyze flow, etc.) is exposed every time pod 1 completes within ~300s of pod start — which is the common case for fast phases like planning. Without this fix, operators must manually edit task files to wake stuck tasks, and there is no log signal indicating which tasks are stuck. Spec 036's grace window without deferred re-evaluation trades one race (duplicate spawn) for another failure mode (permanent stall); both need to be closed.

## Notes on the four fix options surfaced in the bug report

The bug report flagged four candidate approaches; this spec deliberately leaves the choice to impl time, but records the trade-offs so the prompt author has context:

- **A. Timer-based retry**: one-shot timer scheduled inside the suppression branch. Simplest code; loses state on executor restart unless paired with a reconciliation tick. Maps cleanly to "agent decides at impl time" if paired with restart-safety per Desired Behavior #5.
- **B. Agent clears `current_job` on every phase transition**: rejected here as it reopens the spec 036 race for mid-flow transitions.
- **C. Periodic reconciliation tick** scanning all tasks with `phase != terminal && current_job set && job inactive` every N seconds. Restart-safe by construction; adds a periodic scan path.
- **D. Wake on `job_watcher` events**: the `job_watcher` informer already observes job-succeeded transitions (see `task/executor/pkg/job_watcher.go:111-120`); routing those observations into `spawnIfNeeded` after grace expiry minimises new code. Restart-safe because the informer re-lists on startup.

Primaries to evaluate: A (cleanest predicate code) and D (lowest code change, restart-safe by virtue of the informer). The acceptance criteria are agnostic to the choice.
