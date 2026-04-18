---
status: prompted
approved: "2026-04-18T19:07:02Z"
generating: "2026-04-18T19:07:02Z"
prompted: "2026-04-18T19:21:43Z"
branch: dark-factory/executor-job-failure-detection
---

## Summary

- Executor records the spawned Job's name and start timestamp into task frontmatter
- Executor watches `batch/v1 Jobs` in its namespace and reacts to terminal states
- On `Job.Failed` (OOM, backoffLimit exceeded, evict), executor publishes a synthetic failure result to Kafka
- The controller's existing retry counter (spec 008) picks up the failure and escalates after N attempts
- Unifies silent failures (OOM, SIGKILL, node eviction) with agent-published failures under one retry path

## Problem

Spec 008 added a retry counter that fires only when the controller receives a failure result from the agent via Kafka. Silent failures — OOMKilled, node eviction, `backoffLimit` exceeded, image pull failure — never produce a Kafka result because the process is SIGKILL'd or never starts. The controller's retry counter stays at 0 and the task oscillates between "spawn blocked by existing Job" and "phase: ai_review" forever.

Concretely in dev: claude-agent pods OOM, the Job stays in a failed state, the executor's next event for the same task logs "job already exists, treating as success", and no retry/escalation ever happens. Observed with tasks `73556f72-a25f-473c-877b-52d98bc82a88` and `bf97b8c3-2130-4159-8d69-60e2af093d4a` after controller v0.38.0 deployment.

## Goal

After this work, any terminal Job failure — whether the agent published a result or not — counts as one retry attempt and flows through the controller's escalation logic. A task whose Job OOMs three times reaches `phase: human_review` just like one whose agent explicitly reports failure three times. The executor's behaviour is self-consistent: it cleans up its own completed/failed Jobs and never serves stale "already exists" responses.

## Non-goals

- Building a full reconciling controller on Jobs — a shared informer with a handful of event handlers is enough
- Replacing the controller's retry counter — we feed it, we don't duplicate it
- Distinguishing failure causes (OOM vs evict vs backoffLimit) in the escalation section — all treated as "job failed"
- Changing Job spec fields (resources, backoffLimit, ttlSecondsAfterFinished) — out of scope; retry protection is independent of Job tuning
- Restarting or resubmitting failed Jobs directly — the controller's existing respawn-on-next-event path is reused
- Migrating Pattern A (persistent service) agents — this is for Pattern B (ephemeral Job) agents only
- Detecting or killing Jobs stuck in `Running` forever (no failure, no publish) — timeout handling is a separate follow-up spec

## Desired Behavior

1. When the executor spawns a Job for a task, the task frontmatter gains `current_job: <job-name>` and `job_started_at: <ISO8601>`, and `status`/`phase` remain unchanged.
2. The executor watches `batch/v1 Jobs` with a shared informer in its own namespace and maintains a Job-to-task lookup keyed by a label (e.g. `agent.benjamin-borbe.de/task-id`).
3. When a Job reaches terminal state, the executor reacts:
   - `Succeeded` without the agent having already published a result → publish synthetic failure (`status: in_progress`, phase stays `ai_review`, message: "job completed without publishing result") so the counter increments.
   - `Failed` → publish synthetic failure with the Job's failure reason in the message.
4. If a subsequent task event arrives while the task's `current_job` still exists and is running, the executor skips spawning (idempotent) and logs a warning.
5. The synthetic failure result flows through the same Kafka topic the agent uses; the controller's retry counter handles it identically.
6. Jobs that reach terminal state are deleted by the executor after publishing the synthetic result, so future events spawn fresh Jobs.
7. In the rare race where the agent publishes a result and the executor also emits a synthetic failure, both writes are processed (last-write-wins on frontmatter, counter increments twice) — accepted as a mild false positive rather than suppressed.

## Constraints

**Must not change:**
- The controller's retry counter logic from `specs/in-progress/008-task-retry-protection.md` or its Kafka consumer
- The `lib.Task` schema or the `agent-task-v1-request` / `agent-task-v1-event` topic schemas (see `docs/agent-job-interface.md`)
- Agent code — agents keep publishing results the same way; a published result takes precedence over the synthetic failure
- Phase filtering (`allowedPhases = {planning, in_progress, ai_review}` — see `docs/controller-design.md` and `docs/agent-job-lifecycle.md`)

**Scope additions:**
- Executor RBAC gains `delete` on `jobs.batch` in its own namespace (minimum required for terminal-Job cleanup)

**Frozen conventions:**
- Counterfeiter mocks, Ginkgo/Gomega tests
- `github.com/bborbe/errors.Wrapf(ctx, err, ...)` for error wrapping
- `service.Run` for goroutine lifecycle
- `libtime.CurrentDateTimeGetter` for time injection — never `time.Now()`
- `make precommit` as verification gate

## Assumptions

- At most one Job exists per task at a time — enforced by checking `current_job` in frontmatter before spawn
- Agent Jobs either publish a result within a bounded time OR reach a terminal Job state — the informer sees every terminal transition
- Jobs are labelled consistently at spawn time with the task identifier — this is a small extension to the existing job spawner
- The executor's namespace scope is sufficient — all agent Jobs live in the same namespace as the executor
- The agent's Kafka result, if published, arrives before the executor observes terminal Job state in practice; a late-arriving agent result after synthetic failure is acceptable (controller's mergeFrontmatter handles both, last-write-wins on frontmatter)
- Job deletion by the executor does not race with other actors — no other controller reconciles these Jobs

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Pod OOMKilled, Job reaches `Failed` | Executor publishes synthetic failure, deletes Job; controller increments retry_count | Automatic — next scan respawns |
| Node eviction, Job reaches `Failed` | Same as OOM | Automatic |
| Agent publishes failure, Job then reaches `Succeeded` (exit 0 after publish) | Both results flow through Kafka; controller counter increments twice (per Desired Behavior #7) | Accepted as mild false positive — no suppression logic |
| Agent publishes success, Job reaches `Succeeded` | Controller already wrote completed; executor skips synthetic emission | No action needed |
| Job stuck in `Running` forever (no OOM, no publish) | Out of scope for this spec — add a timeout label in follow-up | Manual: `kubectl delete job` |
| `current_job` set but Job no longer exists in K8s | Executor clears `current_job` on next event and spawns a fresh Job | Automatic |
| Informer desynced at startup | Executor re-lists on connect; terminal Jobs observed on first list produce synthetic failures | Automatic |
| Synthetic failure publish fails (Kafka down) | Job stays until next informer event; retry with backoff or on next informer resync | Kafka recovery |
| Executor restarts mid-OOM | On restart, informer lists all Jobs, sees the Failed one, publishes synthetic failure once | Automatic |
| Two executor replicas (future HA) | Leader election required before enabling — flagged as follow-up | Run single replica for now |

## Security / Abuse Cases

- The executor already has RBAC on Jobs in its namespace (`get/list/watch`); this spec requires adding `delete` for terminal-state cleanup — narrow, namespace-scoped
- Label-based task-ID lookup is tamper-resistant because agent pods cannot modify their own Job labels
- No new secrets or external endpoints — Kafka producer is the existing one
- A malicious or buggy agent that publishes `status: completed` prematurely is out of scope; spec 008's counter already trusts published results

## Acceptance Criteria

- [ ] After a Job is spawned for a task, the task file contains `current_job: <name>` and `job_started_at: <ISO8601>` in frontmatter
- [ ] After a Job fails (OOMKilled), the task file shows `retry_count: 1` and `phase: ai_review`
- [ ] After three consecutive OOMs on the same task, the task file shows `retry_count: 3`, `phase: human_review`, and a `## Retry Escalation` section
- [ ] A Job that `Succeeded` with an agent-published `status: completed` leaves the task at `phase: done` and no synthetic failure is emitted
- [ ] A second task event arriving while `current_job` is still running does not spawn a second Job (log line documents the skip)
- [ ] No stale failed Jobs for agent tasks remain in the namespace (`kubectlquant -n dev get jobs` is clean)
- [ ] A rare agent-vs-executor race producing both results is not suppressed (may double-increment the counter, acceptable per Desired Behavior #7)
- [ ] `cd task/executor && make precommit` passes

## Verification

```
cd task/executor && make precommit
```

Manual dev-cluster verification (timing: within ~2 scan cycles per step, ≈2 min):

1. Pick a task known to OOM (e.g. `73556f72-a25f-473c-877b-52d98bc82a88`)
2. Ensure fresh state: remove `retry_count`, `current_job`, `job_started_at` from frontmatter; push
3. Confirm `current_job` and `job_started_at` appear in frontmatter after Job is spawned
4. After the Job OOMs (usual ~30-60s for agent-claude), confirm `retry_count: 1`, `phase: ai_review`
5. Let the cycle run through naturally; after three OOMs expect `phase: human_review` and a `## Retry Escalation` section containing timestamp, attempts=3, assignee
6. Confirm no stale `Failed` Jobs remain: `kubectlquant -n dev get jobs | grep claude-agent`

## Do-Nothing Option

Keep spec 008 as-is. Works for agents that publish clean failure results; silently fails for OOM, evict, and any SIGKILL path. Every new silent-failure mode (disk pressure, image pull fail, CrashLoopBackOff before first publish) requires its own investigation because nothing tracks it. As the agent fleet grows (hypothesis, youtube-processor, researcher), silent failures become the dominant cause of stuck tasks. The executor is the only component that reliably observes Job terminal state — not wiring it up leaves the retry counter half-armed.
