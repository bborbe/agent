---
tags:
  - dark-factory
  - spec
status: draft
---

## Summary

- Agent Jobs publish their own result as a task update command to `agent-task-v1-request` Kafka topic before exiting
- task/controller consumes these commands and writes the result back to the task markdown file in the vault git repo
- This closes the feedback loop: human edits task → agent runs → task file updated with outcome
- task/executor stays a pure spawner — no Job watching, no Pod log reading
- The agent-job-interface contract is updated: agents must publish their result, not just print to stdout

## Problem

When an agent Job finishes, its result disappears. The human must manually check Job logs and update the task file. This defeats the purpose of autonomous execution — the loop from "task assigned" to "task updated with result" is broken. Without automated result capture, the system cannot scale beyond a single watched Job.

## Goal

After this work, every agent Job publishes its own result to Kafka before exiting. task/controller consumes the result and updates the originating task file. Successful Jobs mark the task completed. Failed or needs-input Jobs set the task to human_review phase so the human knows to intervene. The task file contains the agent's output text, links, and a timestamp under a "Result" section.

## Non-goals

- Retrying failed Jobs (manual re-trigger via task edit)
- Streaming logs during execution
- Timeout or kill of long-running Jobs (orphan detection is a separate concern)
- Updating anything other than the originating task file
- Job watching by task/executor (agents handle their own output)

## Desired Behavior

1. task/executor passes an additional env var `TASK_ID` (the task identifier) to spawned Jobs, so agents know which task to update
2. Agent Jobs (e.g. agent-backtest) publish a task update command to `agent-task-v1-request` Kafka topic before exiting, containing: task identifier, new status, new phase, output text, links, and timestamp
3. Result status mapping:
   - Agent outcome `done` → task status=completed, phase=done
   - Agent outcome `needs_input` → task status=in_progress, phase=human_review
   - Agent outcome `failed` → task status=in_progress, phase=human_review
4. task/controller consumes commands from `agent-task-v1-request` and writes the update to the task markdown file: frontmatter fields (status, phase) and an appended "Result" section with output, links, and timestamp
5. task/controller commits the change and pushes to the vault git repo so Obsidian sees the update
6. Stdout JSON is still written (for debugging/logging) but is no longer the primary result transport

## Constraints

**Must not change:**
- The `agent-task-v1-event` topic, schema, or existing task/controller git-to-Kafka sync behavior
- task/controller remains the single git writer — no other service touches the vault repo
- The lib.Task struct fields and validation

**Frozen conventions:**
- CQRS CommandObjectSender pattern for publishing to `agent-task-v1-request`
- Consumer group for task/controller request consumption must be distinct from existing groups
- Agent Jobs already have `KAFKA_BROKERS` and `BRANCH` env vars (from agent-job-interface)
- task/executor adds `TASK_ID` to spawned Job env vars (small change to existing JobSpawner)

## Assumptions

- Agents can use the CQRS CommandObjectSender pattern — they already have Kafka wiring for their domain work (e.g. agent-backtest uses `core-backtest-v1`)
- Agent crash (non-zero exit before publishing) means no update reaches task/controller. This is acceptable — orphan detection is a separate future concern.
- The `agent-task-v1-request` topic and schema already exist in lib but are not yet consumed

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Agent crashes before publishing result | No update reaches task/controller; task stays in_progress | Human notices stale task, checks K8s logs, re-triggers |
| Kafka unavailable when agent publishes | Agent retries briefly; if still down, exits without publishing (same as crash) | Human re-triggers |
| Git push conflict in task/controller | Pull, rebase, retry push (same pattern as existing git sync) | Automatic recovery |
| Task file deleted from vault before result arrives | Log warning, skip update, commit offset | Human is aware (they deleted it) |
| Duplicate result command (agent retries after timeout) | task/controller applies idempotently — re-setting same status/phase is a no-op, Result section checks for existing timestamp | No harm |
| Unknown task identifier in command | Log warning, skip, commit offset | Fix agent or task data |

## Security / Abuse Cases

- Result content (output, message, links) is untrusted agent output. Written into markdown as-is. No shell execution or template rendering occurs, so injection risk is limited to markdown formatting.
- The task identifier in the published command must match the one passed via `TASK_ID` env var. No external input can redirect a result to an arbitrary task.
- Git push is authenticated via the existing task/controller credentials. No new trust boundary is crossed.

## Acceptance Criteria

- [ ] task/executor passes `TASK_ID` env var to spawned Jobs (unit test)
- [ ] Agent can publish task update command to `agent-task-v1-request` using CQRS pattern (integration pattern documented)
- [ ] Status mapping is correct for all three cases: done, needs_input, failed (unit test in task/controller)
- [ ] task/controller consumes from `agent-task-v1-request` and updates task file frontmatter (unit test)
- [ ] task/controller appends a "Result" section with output, links, and timestamp (unit test)
- [ ] task/controller commits and pushes after writing (unit test with mock git)
- [ ] Duplicate result commands are handled idempotently (unit test)
- [ ] `docs/agent-job-interface.md` updated with new contract (agents publish result, TASK_ID env var)
- [ ] `make precommit` passes in task/controller/

## Verification

```
cd task/controller && make precommit
```

## Do-Nothing Option

Results stay in K8s Pod logs. Humans must `kubectl logs` each completed Job and manually update the task file. This is tolerable for 1-2 Jobs per day but becomes unworkable as agent usage grows. It also means the vault never reflects what happened — the human has no record of agent output without checking K8s directly.
