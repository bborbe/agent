1---
status: idea
---

## Summary

- Controller tracks retry count per task via `retry_count` frontmatter field
- After N failures (default 3), controller sets `phase: human_review` and appends error
- Executor already filters out `human_review` — no executor changes needed
- Prevents infinite respawn loops when agents fail repeatedly

## Problem

When an agent task fails, the result writeback keeps `status: in_progress` and a retryable phase. The executor respawns the job on the next scan cycle. If the failure is persistent (bad input, quota exhausted, broken config), this creates an infinite loop burning K8s jobs, API quota, and cluster resources. Observed: 20+ Gemini API calls in 20 minutes from a single stuck task.

## Goal

After this work, the controller automatically escalates persistently failing tasks to `phase: human_review` after a configurable number of retries. The task file shows the retry count and the escalation reason. A human can fix the root cause, reset `retry_count: 0` and `phase: ai_review`, and the task resumes.

## Non-goals

- Exponential backoff between retries (executor spawns on next scan cycle as today)
- Per-agent retry limits (all agents share the same default)
- Automatic retry reset after a cooldown period
- Changing executor phase filtering logic

## Desired Behavior

1. When the controller writes a non-completed result, it reads `retry_count` from existing frontmatter (default 0) and increments it
2. When `retry_count >= max_retries` (read from frontmatter, default 3), the controller overrides `phase` to `human_review` and appends an error message to the content
3. When the controller writes a completed result, `retry_count` is left as-is (shows how many attempts it took)
4. The executor sees `phase: human_review` and skips — no code change needed
5. To retry: set `retry_count: 0`, `phase: ai_review`, `status: in_progress` in the task file

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

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Agent fails once (transient) | retry_count 0→1, phase stays ai_review, executor respawns | Automatic — next attempt may succeed |
| Agent fails 3 times (persistent) | retry_count→3, phase→human_review, error appended | Fix root cause, reset retry_count=0 + phase=ai_review |
| Agent succeeds after 2 failures | status→completed, phase→done, retry_count stays at 2 | No action needed |
| Task has `max_retries: 1` | Escalates after first failure | Set per-task for expensive operations |
| Task has `max_retries: 10` | Allows 10 attempts before escalation | Set per-task for flaky operations |

## Changes

### Controller: `task/controller/pkg/result/result_writer.go`

In `WriteResult()`, after `mergeFrontmatter()`:
- Read `retry_count` from merged frontmatter (default 0)
- If incoming status != "completed": increment retry_count, write back
- If retry_count >= max_retries: override phase to "human_review", append error to content

### Lib: `lib/agent_task-frontmatter.go`

Add accessors:
- `RetryCount() int` — parse `retry_count`, default 0
- `MaxRetries() int` — parse `max_retries`, default 3

### No executor changes

Executor already filters `allowedPhases = {planning, in_progress, ai_review}`. Setting `phase: human_review` naturally stops spawning.

### No agent changes

Agents don't know about retries. They publish results normally.

## Acceptance Criteria

- [ ] First failure: retry_count 0→1, phase stays ai_review
- [ ] Third failure: retry_count 2→3, phase→human_review, error appended
- [ ] Success after retries: status→completed, retry_count preserved
- [ ] Custom max_retries respected
- [ ] Manual reset (retry_count=0, phase=ai_review) allows fresh retries
- [ ] `make precommit` passes in task/controller/

## Verification

```
cd task/controller && make precommit
```

## Do-Nothing Option

Keep the current behavior where failed tasks retry indefinitely. This works if failures are always transient, but a single persistent failure (bad config, quota, invalid input) burns unlimited resources. The retry counter adds ~30 lines of code to prevent runaway loops.
