---
status: draft
---

## Summary

- Controller tracks retry count per task via `retry_count` frontmatter field
- After N failures (default 3), controller sets `phase: human_review` and appends error
- Executor already filters out `human_review` — no executor changes needed
- Prevents infinite respawn loops when agents fail repeatedly

## Problem

When an agent task fails, the result writeback keeps `status: in_progress` and a retryable phase. The executor respawns the job on the next scan cycle. If the failure is persistent (bad input, quota exhausted, broken config), this creates an infinite loop burning K8s jobs, API quota, and cluster resources. Observed: 20+ Gemini API calls in 20 minutes from a single stuck task.

## Goal

After this work, the controller automatically escalates persistently failing tasks to `phase: human_review` after a configurable number of retries. The task file shows the retry count and the escalation reason. Recovery is a documented single-step manual reset (see Recovery Procedure).

## Non-goals

- Exponential backoff between retries (executor spawns on next scan cycle as today)
- Per-agent retry limits (all agents share the same default)
- Automatic retry reset after a cooldown period
- Changing executor phase filtering logic

## Desired Behavior

1. When the controller writes a non-completed result, it reads `retry_count` from existing frontmatter (default 0) and increments it
2. When `retry_count >= max_retries` (read from frontmatter, default 3), the controller overrides `phase` to `human_review` and appends a structured error message to the content (see Error Message Format)
3. When the controller writes a completed result, `retry_count` is left as-is (shows how many attempts it took)
4. The executor sees `phase: human_review` and skips — no code change needed
5. The kafka result-deliverer (`lib/delivery/result-deliverer.go`) is the canonical path and already writes `phase: ai_review` on failure — compatible. The fallback content generator currently writes `phase: human_review` directly on failure and must be reconciled so the retry counter is not bypassed on the fallback path.

## Error Message Format

When escalating to `human_review`, the appended error section contains:

- Timestamp of escalation (ISO 8601)
- Total attempt count (matches final `retry_count`)
- Agent/assignee name
- Last failure message from the agent result

Rendered as a markdown section (e.g. `## Retry Escalation`) so a human operator sees it inline when opening the task.

## Recovery Procedure

To retry a task escalated to `human_review`:

1. Fix the root cause (config, input, quota)
2. Reset frontmatter: `retry_count: 0`, `phase: ai_review`, `status: in_progress`
3. Executor respawns on the next scan cycle

## Constraints

**Must not change:**
- Executor code or phase filtering logic (`allowedPhases = {planning, in_progress, ai_review}`)
- Agent result publishing (agents don't know about retries)
- Kafka event/request schema
- Task frontmatter schema (new fields are additive)

**Frozen conventions:**
- Counterfeiter mocks, ginkgo/gomega tests
- `make precommit` as verification gate

## Assumptions

- Most transient failures resolve within 3 retries (network blips, brief API outages)
- Persistent failures (bad input, missing config, quota) will fail all 3 attempts
- Tasks without `max_retries` field use the controller's default (3)
- `retry_count` is an integer stored as YAML in frontmatter
- At most one result writer executes per task at a time (controller processes results sequentially per task identifier) — no cross-writer race on `retry_count`

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Agent fails once (transient) | retry_count 0→1, phase stays ai_review, executor respawns | Automatic — next attempt may succeed |
| Agent fails 3 times (persistent) | retry_count→3, phase→human_review, error appended | See Recovery Procedure |
| Agent succeeds after 2 failures | status→completed, phase→done, retry_count stays at 2 | No action needed |
| Task has `max_retries: 0` | Escalates on first failure (retry_count 0→1, 1 >= 0 triggers escalation) | See Recovery Procedure |
| Task has `max_retries: 1` | Escalates after first failure | Set per-task for expensive operations |
| Task has `max_retries: 10` | Allows 10 attempts before escalation | Set per-task for flaky operations |

## Implementation Sketch (non-binding)

Hints for the implementer. Final structure is at the implementer's discretion provided acceptance criteria pass.

- Controller result writer: after merging frontmatter, read `retry_count`, increment on non-completed status, compare against `max_retries`, override `phase` and append error section when threshold reached.
- Frontmatter lib: add typed accessors alongside existing `Status()` / `Phase()` (`RetryCount()`, `MaxRetries()`).
- Fallback content generator: align with the retry flow (either set `ai_review` like the kafka path, or leave phase untouched so the controller decides).
- No executor changes: `allowedPhases` already excludes `human_review`.
- No agent changes: agents keep publishing results unchanged.

## Acceptance Criteria

- [ ] After one failure, the task file shows `retry_count: 1` and `phase: ai_review`
- [ ] After three failures (default), the task file shows `retry_count: 3`, `phase: human_review`, and a retry escalation section in the body containing timestamp, attempt count, assignee, and last error
- [ ] A completed result leaves `retry_count` at its prior value and sets `phase: done`
- [ ] A task with `max_retries: N` escalates once `retry_count >= N`
- [ ] `max_retries: 0` escalates on the first failure
- [ ] Resetting `retry_count: 0` + `phase: ai_review` allows the executor to respawn the task
- [ ] Fallback content generator path does not bypass the retry counter
- [ ] `make precommit` passes in task/controller/ and lib/

## Verification

```
cd task/controller && make precommit
cd lib && make precommit
```

## Do-Nothing Option

Keep the current behavior where failed tasks retry indefinitely. This works if failures are always transient, but a single persistent failure (bad config, quota, invalid input) burns unlimited resources. The retry counter adds ~30 lines of code to prevent runaway loops.
