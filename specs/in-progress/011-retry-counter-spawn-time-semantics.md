---
status: verifying
approved: "2026-04-19T17:14:36Z"
generating: "2026-04-19T17:21:52Z"
prompted: "2026-04-19T17:26:55Z"
verifying: "2026-04-20T17:04:23Z"
branch: dark-factory/retry-counter-spawn-time-semantics
---

## Summary

- Move `retry_count` increment from the controller's result writer (failure-time) to the executor's spawn path (spawn-time)
- `retry_count` becomes "number of agent invocations attempted for this task", not "number of failure events observed for this task"
- **Supersedes spec 010's result-writer retry guard** — the guard becomes dead code because the writer no longer increments at all
- User-initiated job cleanup (`kubectl delete job`) no longer inflates the counter
- Synthetic failures from the executor's Job informer (OOM, eviction, deletion) no longer inflate the counter

## Problem

Today `task/controller/pkg/result/result_writer.go` increments `retry_count` whenever an `AgentStatusFailed` arrives on Kafka. Failures have three sources:

1. **Agent-published** — the agent ran, hit a real problem (parse error, network), emitted `failed`. Legitimate retry.
2. **Executor-synthesised** (spec 009) — Job terminal state `Failed` with no agent result (OOM, eviction, backoffLimit). Legitimate retry.
3. **User-synthesised** — an operator runs `kubectl delete job …` on a Running Job. Executor Job informer sees it as terminal → synthesises `failed`. NOT a retry.

Source 3 is indistinguishable from sources 1 and 2 inside the controller. It should be, because the operator is the human — they are managing the cluster, not invoking the retry ladder.

Observed: smoke test task `94884aa4` reached `retry_count: 5` through normal escalation, then the operator ran `kubectl delete job --all` on four Running zombies. Controller received four synthesised `failed` events and bumped `retry_count` to `9`. Max was 5. The counter exceeded max-retries after escalation had already fired, because the result writer's "skip increment when phase already human_review" guard (spec 010) only fires when the **incoming** result carries `phase: human_review`. Synthesised failures carry `phase: ai_review`, so the guard misses them.

Spec 010's guard is a bandage. The underlying design is wrong: the controller infers attempts from observed failure events, and failure events are a noisy signal. The executor knows for certain when it launches an attempt.

## Goal

`retry_count` is incremented by the only component that unambiguously knows an attempt was made: the executor, at the moment it creates a K8s Job. The controller stops inferring attempts from failure events.

A human reading an escalated task's `retry_count` can trust that it equals the number of agent invocations attempted, not the number of times K8s emitted a Job-failure event.

## Non-goals

- Not changing `max_retries` semantics or default value
- Not changing escalation target (`phase: human_review` after `retry_count >= max_retries` stays)
- Not changing `AgentStatus` enum or Kafka topic schemas
- Not removing the executor's synthetic-failure publisher — it still delivers `failed` results so the controller knows work happened
- Not moving `retry_count` out of the task file frontmatter

## Desired Behavior

1. When the executor decides a task warrants a spawn, it publishes an `agent-task-v1-request` "update" command that increments `retry_count` by 1, then creates the K8s Job.
2. The controller's result writer no longer increments `retry_count` on `AgentStatusFailed`. It only writes status, phase, and appends Result.
3. Escalation logic (`retry_count >= max_retries` → `phase: human_review`) stays in the controller but reads the counter the executor maintains; the writer applies escalation on the result-write cycle without bumping.
4. `kubectl delete job …` on a Running Job produces a synthesised `failed` Result in the task file but does NOT advance `retry_count`. The task either gets re-spawned (counter bumped at spawn time) or stays idle if phase has already left the executor allowlist.
5. Spec 010's "skip increment when incoming phase is human_review" guard is removed from the result writer — the writer no longer increments, so the guard is dead code.

## Constraints

- Controller stays the single writer to the vault git repo. Executor must use the existing `agent-task-v1-request` update command to bump `retry_count`; it never writes git directly.
- No new Kafka topic. Piggyback on `agent-task-v1-request` by publishing a task update whose frontmatter diff is limited to `retry_count` (and optionally `current_job`).
- Ordering: executor MUST publish the bump request before calling `kubectl create Job`.
- If Job creation fails after the bump is published, the counter stays incremented. Over-count is capped at 1 per spawn attempt; max_retries absorbs it. This is a design tradeoff, not an observable behavior.
- The executor already tracks per-task state. It can compute `retry_count + 1` locally from the task it received in the event.
- `retry_count` is authoritative in the vault file. The executor's in-memory view is derived; after the controller writes back, the corrected value is re-emitted and the executor's local view catches up.
- Existing spec 008 retry-counter semantics stay: counter starts at 0 on task creation, escalation at `max_retries`.
- Synthetic-failure Result body/format (spec 009) is unchanged — only its side-effect on the counter changes.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Executor publishes bump request, Kafka broker unreachable | Executor logs error, does NOT spawn K8s Job. Next event cycle retries publish. | No stuck state; task stays in executor allowlist until publish succeeds. |
| Executor publishes bump request, Kafka OK, K8s create Job fails | Counter is incremented by 1, no Job runs. Next event cycle will try again. | If Job creation keeps failing, counter reaches max_retries and task escalates to human_review — correct signal: something is wrong. |
| Two events for same task arrive close together (spawn collision) | Executor idempotency check (`IsJobActive` on task-id label) prevents duplicate spawn. Only one publish-then-spawn runs. | Existing behavior. |
| Operator deletes Running Job | Executor Job informer synthesises `failed` Result → controller writes Result section, status=in_progress, phase=ai_review, retry_count UNCHANGED. Next event cycle: executor decides to re-spawn → bump → spawn. | Counter equals attempts, not deletions. |
| Controller crashes between bump-write and next cycle | On restart, controller re-scans vault, sees `retry_count: N+1`, emits event. Executor sees no active Job for this task → spawns again, bumps to N+2. Over-counts by 1. | Acceptable — max_retries absorbs it. |
| Result writer sees `AgentStatusFailed` at `retry_count == max_retries` | Writer sets `phase: human_review`, appends `## Retry Escalation`, does NOT increment. | Escalation still fires correctly. |
| Legacy task with elevated `retry_count` from failure-time accounting | Controller does not retroactively rewrite. New attempts bump from current value. | Operator may manually reset if desired; migration is opt-in. |

## Do-Nothing Option

Cost of keeping failure-time increment:

- Every operator-initiated job cleanup inflates `retry_count` by the number of Running jobs deleted. Small teams with frequent manual intervention see counter drift regularly.
- Spec 010's escalation-skip guard remains load-bearing and must be audited every time result-writer logic changes.
- New failure event sources (e.g. future pod-disruption-budget evictions) silently feed the counter without author review. Design entropy.

Benefit: zero dev time. But the semantic bug compounds — the counter is trusted to mean "attempts" and quietly means "failure events". Every downstream human decision built on that number is slightly wrong.

Not doing this is reasonable if we decide `retry_count` is a soft signal, not an attempt count. But the variable name, the max_retries escalation rule, and the spec 008 problem statement all assume "attempt count". The code disagrees. Fixing the code is cheaper than redefining the meaning.

## Security / Abuse

No new attack surface. The executor already publishes `agent-task-v1-request` messages for other flows; this adds one more call site in an existing authenticated channel. An attacker with Kafka produce access to `agent-task-v1-request` could already forge any task update.

## Acceptance Criteria

- [ ] Controller's result writer does not modify `retry_count` on any incoming `AgentStatus`. It writes status, phase, and appends the Result section only.
- [ ] Executor's spawn path publishes an `agent-task-v1-request` update that sets `retry_count = current + 1` **before** creating the K8s Job.
- [ ] Integration test: spawn → Job fails → counter advances by exactly 1 (the spawn-time bump); the synthesised failure adds nothing.
- [ ] Integration test: spawn → Job succeeds → counter advances by exactly 1; `done` result adds nothing.
- [ ] Integration test: operator deletes a Running Job → controller appends Result section, `retry_count` is unchanged from before the delete; the next spawn cycle produces exactly one bump + one new Job.
- [ ] Unit test (result writer): `AgentStatusFailed` arrival with `retry_count=3, max=5` → writer does not modify the field and sets `phase=ai_review`.
- [ ] Unit test (result writer): `AgentStatusFailed` arrival with `retry_count=5, max=5` → writer does not modify the field and sets `phase=human_review`, appends `## Retry Escalation`.
- [ ] Spec 010's "skip increment when incoming phase=human_review" branch is removed from the controller's result writer — no grep hit for the guarded branch remains.
- [ ] `docs/task-flow-and-failure-semantics.md` updated to reflect new increment site.
- [ ] CHANGELOG.md under `## Unreleased` notes the behavior change (operator-visible: retry_count no longer drifts on manual job cleanup).

## Verification

```bash
cd task/controller && make precommit
cd task/executor && make precommit
cd lib && make precommit
```

Manual smoke test on dev:

1. Create a task that will deterministically fail (e.g. malformed prompt causing parse error at agent)
2. Let it retry normally to `retry_count: 3`
3. `kubectlquant -n dev delete jobs -l agent.benjamin-borbe.de/task-id=<id>` while Job is Running
4. Wait one poll cycle
5. Verify task file: `retry_count` did NOT jump; a new `## Result` section was appended for the synthesised failure; next spawn will bump to 4

## References

- `specs/completed/008-task-retry-protection.md` — original retry counter design (failure-time)
- `specs/in-progress/010-failure-vs-needs-input-semantics.md` — the bandage being removed
- `specs/completed/009-executor-job-failure-detection.md` — synthetic failure publisher stays; only its effect on the counter changes
- `docs/task-flow-and-failure-semantics.md` — must be updated
- `docs/controller-design.md` — writer section needs an update
