---
kind: bug
tags:
  - agent
  - spec
status: draft
---

## Summary

- Spec 035's terminal-phase gate (deployed 2026-05-16T18:26Z) prevents respawn ONCE a task has reached a terminal phase, but does NOT prevent respawn during the race window between (a) the agent's k8s Job exiting cleanly and (b) the agent's terminal-phase write reaching the task event the executor consumes.
- Verified live on prod 2026-05-16T20:25:15Z: pod 1 (`pr-reviewer-agent-22fda7e7-20260516201916`) succeeded at 20:24:28Z; at 20:25:15Z `task_event_handler.go:319` logged `task ...: current_job ... no longer active, proceeding to spawn`; pod 2 (`pr-reviewer-agent-22fda7e7-20260516202515`) spawned 1 second later. The agent's terminal phase write (`phase: human_review`) arrived at the executor's next event ~4 minutes after that — too late: pod 2 was already running.
- Root cause is in `spawnIfNeeded` at `task/executor/pkg/handler/task_event_handler.go:300-322`. When `current_job` is set and `IsJobActive` returns false, the code unconditionally falls through to spawn. It interprets job-inactivity as "agent finished cleanly without writing terminal phase, must be a crashed run, retry" — but does not distinguish that case from "agent finished and terminal phase write is in transit".
- The spec 035 gate at the top of `parseAndFilter` is necessary but not sufficient: it gates on the latest *observed* phase. During the race window, the latest observed phase is still `in_progress` (or whatever the agent set pre-exit), so the gate sees nothing terminal and lets the event through to `spawnIfNeeded`.
- Fix: in `spawnIfNeeded`, when `current_job != "" && !active`, require either (a) the task frontmatter's `current_job` field to be cleared (by the agent itself on clean exit, or by a downstream observer), OR (b) a configurable grace period to have elapsed since `job_started_at`, before allowing respawn. Inside that window, suppress and emit a structured log line + metric label.
- This is the second of two gates needed to close the duplicate-spawn class. Spec 035 handles "stale event arrives at terminal task"; this spec handles "fresh event arrives at clean-exit task during the agent's terminal-write propagation window".

## Problem

The executor today treats k8s Job inactivity as a proxy for "the agent is done with this task; if no terminal phase is set, the agent crashed, retry it". This proxy is wrong for the post-completion pre-terminal-write window: the agent finishes successfully, the Job becomes inactive seconds later, but the agent's terminal-phase write (committed to the vault repo, picked up by obsidian-git, propagated through the task-controller, published as a task event) takes additional seconds-to-minutes to land at the executor. During that window, any task event the executor processes will see phase=in_progress + current_job inactive and (correctly per current code) decide to spawn a retry.

The 2026-05-16 prod run produced concrete evidence: pod 1 succeeded at 20:24:28Z; pod 2 spawned at 20:25:16Z; the terminal-phase write only reached the executor at 20:29:15Z (the spec 035 gate then correctly suppressed any further spawns). The 5-minute terminal-write propagation window is the bug's surface area.

## Goal

After this fix, when `current_job` is set in the task frontmatter and the k8s Job is no longer active, the executor MUST NOT spawn a new pod unless either (a) the `current_job` field has been cleared from the frontmatter — proving the prior agent wrote its terminal state — or (b) the configured grace period has elapsed since `job_started_at`. Inside the suppression window, the executor emits a structured info-level log line and increments a dedicated metric label so operators can diagnose suppressed retries from logs alone. The change closes the duplicate-spawn race demonstrated in prod on 2026-05-16T20:25Z.

## Non-goals

- Not modifying the spec 035 terminal-phase gate at the top of `parseAndFilter` — it remains in place, gates on terminal phase, and is necessary.
- Not changing the agent's terminal-phase write protocol (the agent still writes `phase: human_review` or `phase: done` on clean exit and updates `current_job` as today).
- Not removing the existing retry-on-crash behavior — agents that genuinely crash before any phase write still get retried after the grace period.
- Not introducing distributed locking, Kafka transactions, or any cross-component coordination beyond reading two fields on the task frontmatter.
- Not making the grace period dynamic — a single fixed default with optional per-agent override in `AgentConfiguration` is sufficient.

## Reproduction

**Triggering incident (verbatim evidence on file):**

- Task: `22fda7e7-9f20-5c65-8173-0352f3bd2735` (PR Review github - bborbe-maintainer - 5 - d04d349a)
- Pod 1: `pr-reviewer-agent-22fda7e7-20260516201916` (started 20:19:16Z; succeeded 20:24:28Z per `job_watcher.go:117` log)
- Spawn decision: `task_event_handler.go:319` log line at 20:25:15.000Z: `task 22fda7e7-...: current_job pr-reviewer-agent-22fda7e7-20260516201916 no longer active, proceeding to spawn`
- Pod 2: `pr-reviewer-agent-22fda7e7-20260516202515` (started 20:25:16.041Z, ~1 second after the decision)
- Terminal write observed at executor: `task_event_handler.go:66` log line at 20:29:15.001Z: `event=spawn_suppressed phase=human_review task=22fda7e7-...` (the spec 035 gate firing on the third event, after pod 2 was already running and had finished)
- Time between pod 1 success → pod 2 spawn: ~47 seconds. Time between pod 2 spawn → terminal write reaching executor: ~4 minutes.

**Predicate location:**

- File: `task/executor/pkg/handler/task_event_handler.go`
- Function: `spawnIfNeeded`
- Lines 300-322: the `if currentJob := task.Frontmatter.CurrentJob(); currentJob != ""` block. Today the inactive-job branch (line 319-322) unconditionally proceeds to spawn.

**Minimal in-process reproduction:**

1. Construct a `lib.Task` with `current_job: pod-A`, `job_started_at: <now - 30s>`, `phase: in_progress`, `status: in_progress`.
2. Configure the `FakeJobSpawner` so `IsJobActive(taskID)` returns `false`.
3. Call the handler. Observe today: spawn proceeds; one new pod is created.
4. Expected after fix: spawn is suppressed; a `respawn_grace_window` (or similar) metric label increments; a structured log line records the suppression with task id + current_job + elapsed time.

**Cross-cycle reproduction (race-closing variant):**

1. Same as above but `job_started_at: <now - 6 minutes>` (past the grace period default).
2. Call the handler.
3. Expected: spawn proceeds (graceful fall-through; the agent genuinely crashed without writing terminal phase, retry is correct).

## Expected vs Actual

**Expected:** When `current_job` is set and the k8s Job is inactive, the executor distinguishes "agent finished cleanly; terminal write in flight" (suppress for grace period) from "agent crashed; never wrote anything" (retry after grace period elapses).

**Actual (observed 2026-05-16T20:25Z prod):** the executor treats job-inactive as sufficient to spawn immediately. The race window between job-exit and terminal-write-propagation is the bug's surface — any event the executor processes during that window produces a duplicate pod.

## Why this is a bug

- **Idempotency.** The same task should produce one pr-reviewer pod per agent lifecycle, not two. The agent's clean exit + terminal write IS the contract; the executor's window between those two events is an implementation artifact the agent has no way to close.
- **Operator diagnosability.** Today the duplicate-spawn produces no executor-side log signal indicating "I knew the prior pod finished but didn't have its terminal phase yet". The 20:25:15 line says "proceeding to spawn" — operationally indistinguishable from a legitimate retry-after-crash.
- **Downstream amplification.** Pr-reviewer-agent produces external side effects (GitHub review API calls). Two pods racing produces dismiss-and-replace behavior the human reviewer didn't request. This is exactly the failure mode the 3-bug chain (maintainer specs 030, 031, agent spec 035) was meant to close; this fourth piece reopens it for clean-exit tasks.

## Desired Behavior

1. In `spawnIfNeeded`, when `currentJob != "" && !active`, the executor MUST check two additional conditions before proceeding to spawn:
   - **Condition A (clean-exit signal):** is `current_job` still present in the task frontmatter? If the agent's terminal write cleared the field, the prior run is durably finished and a new spawn is allowed (the next agent run, if needed, starts fresh).
   - **Condition B (grace period):** has the configured grace period (default: 300s) elapsed since `job_started_at`?
2. If `current_job` is still set AND the grace period has NOT elapsed, suppress the spawn, emit info-level `event=respawn_grace_window task=<id> current_job=<job> elapsed=<seconds>` log line, increment `metrics.TaskEventsTotal.WithLabelValues("respawn_grace_window")`, and return without error. The next event for the same task will re-evaluate.
3. If `current_job` is still set AND the grace period HAS elapsed, proceed to spawn (current behavior; this is the genuine crash-retry path). Emit the existing `task ...: current_job ... no longer active, proceeding to spawn` log line so operator diagnostics for crashed-agent retries are unchanged.
4. If `current_job` is empty (cleared), proceed to spawn unchanged. This is a new task or a task whose prior run completed cleanly and the controller updated the frontmatter.
5. The grace period is a single package-level constant `defaultRespawnGracePeriod = 300 * time.Second`. No per-agent override on `AgentConfiguration` — defer until a real need surfaces. If a future agent type needs a different value, add the override then.
6. The spec 035 terminal-phase gate remains untouched. The new check runs inside `spawnIfNeeded` AFTER the terminal-phase gate has let the event through but BEFORE the existing inactive-job-proceed-to-spawn fall-through.
7. The check uses the task's `job_started_at` field (already present in frontmatter; written by spawn). If absent (legacy tasks), the grace period is treated as elapsed (preserves existing retry behavior; do not break old tasks).

## Constraints

- The new check lives entirely in `task/executor/pkg/handler/task_event_handler.go`. No changes to the agent, the task-controller, the vault-cli library, or any external interface.
- `AgentConfiguration` is NOT modified — the grace period is a package-level constant with no override. Adding an override is deferred to a follow-up spec if a future agent type needs a different value.
- The grace-period clock source MUST be injectable (the existing handler likely already has a clock; if not, add one) so unit tests are deterministic. No `time.Now()` directly in the predicate.
- The new metric label `respawn_grace_window` MUST be pre-initialised in `metrics.go` `init()` alongside the existing labels (consistent with spec 035's pattern).
- The log line MUST be info-level (`glog.Infof`), NOT `glog.V(2)`. This is a respawn-suppression signal operators must see at default verbosity.
- The fix MUST NOT modify the spec 035 terminal-phase gate or its tests. Both gates coexist: terminal gate runs first in `parseAndFilter`, grace gate runs later in `spawnIfNeeded`.
- Verification ladder: Rung-1 (`make precommit` in `task/executor/`) is the primary correctness gate; Rung-2 (deploy to dev, reset PR #5 task to a state with `current_job` still set + `job_started_at` recent + phase non-terminal, observe spawn suppression) is required because the race is timing-dependent and unit-deterministic only via the injected clock; Rung-3 (prod) after ≥1 day dev soak.
- `glog.SetOutput` is NOT available in `glog v1.2.x`. Log-line observation in tests uses the spawn-counter + metric-counter proxy (path b from spec 035).
- Domain reference: `docs/task-flow-and-failure-semantics.md` defines the phase lifecycle and the `current_job`/`job_started_at` frontmatter contract.

## Failure Modes

| Trigger | Expected behavior | Detection | Reversibility | Concurrency | Recovery |
|---|---|---|---|---|---|
| Pod finishes cleanly at T+0; executor event arrives at T+10s with phase still `in_progress` | Spawn suppressed; `respawn_grace_window` metric increments; log line emitted | `kubectlquant -n <stage> logs <executor-pod> \| grep respawn_grace_window` shows the task id | n/a (no spawn) | Multiple executor replicas independently reach the same decision per event | None — next event re-evaluates |
| Pod finishes cleanly at T+0; agent's terminal-write propagates at T+30s; executor event arrives at T+45s | Terminal-phase gate (spec 035) fires; existing `spawn_suppressed_terminal_phase` log + metric | Existing spec 035 diagnostics | n/a | n/a | None |
| Pod crashes at T+0 without writing terminal phase; grace period (300s) elapses; event arrives at T+310s | Spawn proceeds; existing `proceeding to spawn` log line | Existing executor logs | Reversible (the new pod runs and may itself succeed) | n/a | Operator inspects the crashed pod's logs to understand the failure |
| Pod runs longer than the grace period legitimately (slow PR with large diff) | If `job_started_at` is OLDER than grace period AND job is still active, today's `active` branch returns "still active" — grace check is irrelevant. If the job becomes inactive AFTER the grace window, retry proceeds as legitimate-crash recovery | Existing `skipped_active_job` metric while running; spawn after exit | n/a | n/a | n/a |
| Task frontmatter missing `job_started_at` (legacy task) | Grace period treated as elapsed; spawn proceeds (preserves existing behavior) | Existing logs | n/a | n/a | n/a |
| Two executor replicas race on the same event during the grace window | Kafka consumer-group semantics deliver to one; the other sees nothing. The chosen replica suppresses. No duplicate spawn. | Per-replica logs | n/a | n/a | n/a |
| Clock skew on the executor pod | Grace window may be off by the skew amount. For 300s default + typical skew <10s, irrelevant. | n/a | n/a | n/a | n/a |
| `current_job` field cleared mid-window (controller observed terminal write) | Spawn proceeds — clean-exit path. New agent run starts fresh. | Executor logs show the fall-through | n/a | n/a | n/a |

## Security / Abuse Cases

Touches no HTTP, no user input. The two new reads (`CurrentJob`, `JobStartedAt`) are existing frontmatter fields the executor already parses. No new trust boundary.

## Acceptance Criteria

- [ ] `task/executor/pkg/handler/task_event_handler.go` contains a grace-window check between the existing `current_job` inactivity branch and the fall-through spawn — evidence: `grep -n 'respawn_grace_window' task/executor/pkg/handler/task_event_handler.go` returns ≥1 line in a non-test context; the check reads `task.Frontmatter.JobStartedAt()` and compares against an injected clock.
- [ ] The `defaultRespawnGracePeriod` constant is named and set to 300 seconds — evidence: `grep -n 'defaultRespawnGracePeriod' task/executor/pkg/handler/` returns ≥1 line with the value `300 * time.Second`.
- [ ] No per-agent override is added — evidence: `grep -rn 'RespawnGracePeriod\|GracePeriod' task/executor/pkg/handler/ task/executor/pkg/metrics/` shows the constant only, no `AgentConfiguration` field; the constant is a single `time.Duration` package-level var.
- [ ] An info-level structured log line is emitted on every suppression — evidence: `grep -n 'event=respawn_grace_window' task/executor/pkg/handler/task_event_handler.go` returns ≥1 line; format includes keys `task`, `current_job`, `elapsed`.
- [ ] The new metric label `respawn_grace_window` is pre-initialised — evidence: `grep -n 'respawn_grace_window' task/executor/pkg/metrics/metrics.go` returns ≥1 line in `init()`.
- [ ] Ginkgo tests cover the three branches — evidence: `cd task/executor && go test ./pkg/handler/... -v -ginkgo.v 2>&1 | grep -E 'grace window|respawn'` returns ≥3 PASS rows with row names matching: `current_job set, job inactive, within grace => suppress`, `current_job set, job inactive, past grace => spawn`, `current_job empty, job inactive => spawn`.
- [ ] The grace-window test uses the injected clock — evidence: a Counterfeiter fake clock is wired into the handler under test; the test sets `Now()` deterministically to simulate elapsed-vs-not-elapsed.
- [ ] The spec 035 terminal-phase gate tests still pass unchanged — evidence: `cd task/executor && go test ./pkg/handler/...` exits 0 and the spec 035 row names from `task_event_handler_test.go` still appear in the output.
- [ ] `make precommit` exits 0 in `task/executor/` — evidence: exit code 0.
- [ ] CHANGELOG entry under `## Unreleased` — evidence: `grep -n 'respawn_grace_window\|grace window' CHANGELOG.md` returns ≥1 line.

### Post-Deploy Verification

- [ ] **Post-Deploy (Rung-2):** after dev deploy, reset PR #5 task to a state simulating the race (`current_job: <new-job-id>`, `job_started_at: <now>`, phase=in_progress, status=in_progress) without actually starting a pod for that id; on the next event publish, the executor suppresses and logs the grace-window line — evidence: `kubectlquant -n dev logs <executor-pod> --since=10m | grep respawn_grace_window` returns ≥1 line referencing the task id.
  - `deploy_check:` `kubectlquant -n dev get deploy/agent-task-executor -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `dev`
- [ ] **Post-Deploy (Rung-3):** same observation in prod after ≥1 day dev soak — evidence: within 7 days of dev-soak start, the next pr-reviewer task that exhibits the clean-exit-pre-terminal-write window produces a `respawn_grace_window` log line in prod AND `kubectlquant -n prod get jobs | grep <task-uuid> | wc -l` returns `1` exactly for that task lifecycle.
  - `deploy_check:` `kubectlquant -n prod get deploy/agent-task-executor -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `prod`

## Verification

```bash
# Rung-1
cd task/executor && make precommit
# Expected: exit 0
```

```bash
# Rung-2 (after dev deploy via agent-dev worktree)
kubectlquant -n dev logs <executor-pod> --since=10m | grep respawn_grace_window
# Expected: ≥1 line for the test task

kubectlquant -n dev get jobs | grep <task-uuid> | wc -l
# Expected: 1 (or 0 if the simulated reset never triggered a real pod — the suppression is the evidence)
```

```bash
# Rung-3 (prod, after ≥1 day dev soak)
kubectlquant -n prod logs <executor-pod> --since=24h | grep respawn_grace_window
# Expected: ≥1 line for at least one task in the past 24h
```

## Do-Nothing Option

Not viable. The 2026-05-16T20:25Z prod incident proves the race produces duplicate spawns under normal load. Each duplicate is a wasted pr-reviewer invocation (cost + GitHub API quota + risk of dismissing a prior pod's review). The spec 035 fix alone leaves the race open; closing it requires this complementary gate.
