---
status: idea
tags:
  - dark-factory
  - spec
  - controller
  - executor
---

## Summary

- Agent Jobs get a hard runtime ceiling (`activeDeadlineSeconds` in the K8s Job spec)
- The executor sets this when spawning each Job; once exceeded, K8s kills the Pod and the Job moves to Failed
- Default value applies to every agent (sane upper bound, e.g. 30 min)
- Per-agent override via a new `AgentConfig` field for slow agents (e.g. trade-analysis on a 100-trade sweep) or fast agents (e.g. heartbeat ping)
- Closes the "agent runs forever" failure mode where a hung Claude session, infinite loop, or overshooting plan accumulates compute and blocks downstream queue progress

## Problem

Today an agent Job runs until it completes or the cluster reschedules it — there is no upper bound. We've seen single trade-analysis Phase 2 runs go past 35 minutes (live observation 2026-04-28, task `81f0affd`) when the prompt asked for too many trades. If Claude hangs on a tool call, gets stuck in a retry loop, or hits a degenerate plan, the Job consumes a Pod slot, an OAuth-PVC mount, and Claude API quota indefinitely until a human notices.

Symptoms today:
- No surface signal that an agent is "running too long" — operators learn from `kubectl get jobs` runtime
- Trigger-cap escalation logic (3 retries → human_review) only fires when the Job *exits* with a non-success status; a perpetually-running Job never exits and never escalates
- Resource leaks under sustained load: imagine a controller bug that re-spawns the same task every minute — without a deadline, every spawn lingers

## Goal

After this work, **every agent Job has a deadline**, and operators can tune that deadline per agent:

1. Executor stamps `spec.activeDeadlineSeconds` on every Job it creates from an `AgentConfig`
2. Default deadline is the cluster-wide default (e.g. `1800` = 30 min) shipped in code
3. `AgentConfig.spec.jobActiveDeadlineSeconds` (new optional field) overrides the default per agent
4. When the deadline fires, the Pod is killed with `DeadlineExceeded`. The existing executor-side completion handler (which already routes Job failures into the trigger-cap escalation path) sees this as a failure and increments `trigger_count` on the next pickup
5. After 3 deadline-exceeded retries, the trigger-cap escalation routes to `human_review` — same path as crash-loops today

## Non-goals

- Soft graceful-shutdown with a SIGTERM grace period (K8s already handles this via `terminationGracePeriodSeconds`; we're not changing that)
- Cluster-wide policy enforcement (e.g. via OPA/Gatekeeper) — the executor is the single point that creates Jobs, that's where the policy lives
- Different deadlines per task (a fast smoke vs. slow nightly sweep) — same agent always gets the same deadline; if a specific task needs longer, split into a separate `AgentConfig` (sibling to today's per-stage Configs)
- Mid-run deadline extension — the Job is killed and retried; no "snooze"
- Backporting an `activeDeadlineSeconds` field to old running Jobs — the field is immutable on the K8s Job spec; the change applies to NEW Jobs only
- Per-step or per-phase deadlines (the deadline is the whole Job, which today is one phase but in framework agents wraps a single phase too)

## Desired Behavior

### Default

When `AgentConfig.spec.jobActiveDeadlineSeconds` is unset, the executor uses a hardcoded default — pick something operators can live with. Suggested: **`1800` (30 min)**. This is generous for current agents (claude/code/gemini/hypothesis ~5min, trade-analysis ~15min, backtest variable) and tight enough that a hung Job is detected within an hour.

### Per-agent override

```yaml
apiVersion: agent.example.com/v1
kind: AgentConfig
metadata:
  name: trade-analysis-agent
  namespace: dev
spec:
  assignee: trade-analysis-agent
  image: docker.example.com/agent-trade-analysis:dev
  # NEW: override the cluster default. Optional; omit to inherit default.
  jobActiveDeadlineSeconds: 3600   # 1 hour for slow sweeps
```

```yaml
spec:
  assignee: heartbeat-agent
  jobActiveDeadlineSeconds: 60     # tight bound; should never exceed
```

### Stamping on Job creation

In `task/executor`, where the Job is built from the AgentConfig (search `corev1.PodSpec` / `batchv1.JobSpec`), add:

```go
deadline := defaultJobActiveDeadlineSeconds // const, e.g. 1800
if cfg.Spec.JobActiveDeadlineSeconds != nil && *cfg.Spec.JobActiveDeadlineSeconds > 0 {
    deadline = *cfg.Spec.JobActiveDeadlineSeconds
}
job.Spec.ActiveDeadlineSeconds = &deadline
```

`*int64` (pointer) on the CRD field so we distinguish "unset → use default" from "explicitly 0 → no deadline" (we MUST reject `0` at validation; K8s treats 0 as "kill immediately"). Either reject in CRD validation or coerce to default.

### Failure semantics

K8s emits a `Failed` Job condition with reason `DeadlineExceeded` when the deadline fires. The executor's existing failure handler (which today catches Pod crashes) sees this as a failure and:
1. Increments `trigger_count` on the task file
2. If `trigger_count >= max_triggers` (3), escalates to `human_review`
3. Otherwise the controller reschedules and the next Job runs with the same deadline

No new code path for the deadline-exceeded case — it reuses the existing failure handling.

### Observability

- Log line at Job creation time: `creating job assignee=X deadline=Ys`
- Log line at Job failure with reason: `job failed assignee=X reason=DeadlineExceeded`
- Existing Prometheus metric `agent_task_failed_total` covers it; consider adding a `reason` label so deadline-exceeded is distinguishable from crash

## Open Questions

- **Default value**: 30 min suggested. Survey current agents' p99 runtime first; adjust before shipping.
- **Where to define the default**: code constant, env var on the executor, or cluster ConfigMap? Code constant is simplest; env var lets ops tune without redeploy. Recommend env var with a code default.
- **Trade-analysis specifically**: today's task `81f0affd` ran 35+ min on 38 trades (~50s/trade). With the 30-min default it would have been killed mid-run. Either bump trade-analysis to `3600` in its AgentConfig, or scope tasks tighter (per [[Trade Analysis Agent Guide]] § E2E Testing tier-2 = 1 day = ~2 min). Both fixes apply, the spec covers the mechanism.
- **Interaction with manual `kubectl exec` debug**: deadline applies to the Job's main container too. If we attach to a long-lived debug session, the Job dies. Acceptable — debug Jobs are a separate concern; if needed, set `jobActiveDeadlineSeconds: 86400` per-agent for debug variants.

## Out of Scope

- Soft "warning" thresholds at 50% / 75% of deadline (could add a metric later)
- Per-task overrides via task frontmatter — keep deadlines at the AgentConfig level
- Replacing `activeDeadlineSeconds` with a custom watchdog inside the agent binary

## Related

- [[Agent Task Controller Architecture]] — full pipeline architecture
- `task/executor/pkg/job/builder.go` (or similar) — where Jobs are constructed today
- AgentConfig CRD definition in the controller repo
- Existing trigger-cap escalation logic — we lean on it; no new escalation path needed
