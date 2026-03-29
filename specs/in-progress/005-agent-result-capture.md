---
status: approved
tags:
    - dark-factory
    - spec
approved: "2026-03-29T19:50:57Z"
branch: dark-factory/agent-result-capture
---

## Summary

- task/controller consumes `agent-task-v1-request` commands and updates task files in the vault git repo
- task/executor passes `TASK_ID` env var to spawned Jobs so agents know which task to update
- Agents publish their own result to `agent-task-v1-request` using CQRS — task/executor does not watch Jobs or read stdout
- Closes the feedback loop: human edits task → agent runs → task file updated with outcome

## Problem

When an agent Job finishes, its result disappears. The human must manually check Job logs and update the task file. The loop from "task assigned" to "task updated with result" is broken. Without automated result capture, the system cannot scale beyond manual monitoring.

## Goal

After this work, agents publish their result to Kafka and task/controller writes it back to the task file. The human sees the result in Obsidian without touching kubectl. task/executor remains a simple Job launcher.

## Non-goals

- Detecting crashed agents (no result published) — human notices stale tasks
- Retrying failed Jobs
- Streaming logs during execution
- Updating anything other than the originating task file
- Changing agent-backtest (separate repo, separate work)

## Desired Behavior

1. task/executor passes `TASK_ID` (task identifier from the event) as an env var to every spawned K8s Job, alongside existing TASK_CONTENT, KAFKA_BROKERS, BRANCH
2. task/controller consumes commands from `agent-task-v1-request` topic using a new consumer group
3. Each consumed command contains: task identifier, new status, new phase, result output text, and optional links
4. task/controller updates the task markdown file: sets frontmatter status and phase, appends a `## Result` section with output, links, and ISO timestamp
5. task/controller commits the change and pushes to the vault git repo
6. Duplicate result commands for the same task are handled idempotently — re-setting the same status/phase is a no-op, Result section checks for existing content before appending

## Constraints

**Must not change:**
- The `agent-task-v1-event` topic or existing task/controller git-to-Kafka sync behavior
- task/controller remains the single git writer
- task/executor stays a Job launcher — no Job watching, no stdout reading

**Frozen conventions:**
- CQRS CommandObjectSender pattern for request consumption
- Consumer group for request consumption must be distinct from event-producing group
- Task file format follows vault-cli conventions (YAML frontmatter + markdown body)
- `TASK_ID` env var name matches `docs/agent-job-interface.md`

## Assumptions

- The `agent-task-v1-request` Kafka topic already exists (confirmed in cluster)
- The lib.Task struct and TaskV1 schema already support request/result topics
- Agents handle their own Kafka publishing — task/executor is not involved in the result path
- Git conflicts during push are resolved by pull+rebase+retry (existing pattern)

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Agent crashes before publishing result | No update reaches task/controller; task stays in_progress | Human notices stale task, re-triggers |
| Git push conflict | Pull, rebase, retry push | Automatic recovery |
| Task file deleted before result arrives | Log warning, skip, commit offset | Human is aware (they deleted it) |
| Duplicate result command | Idempotent — re-set same values, don't duplicate Result section | No harm |
| Malformed command (missing task ID) | Log warning, skip, commit offset | Fix upstream agent |
| Unknown task ID (file not found) | Log warning, skip, commit offset | Fix task data |
| Result content contains YAML frontmatter delimiters (---) | Strip or escape delimiters before writing to prevent file corruption | Automatic sanitization |

## Security / Abuse Cases

- Result content (output, links) is untrusted agent output. Written into markdown as-is. No shell execution or template rendering — injection risk limited to markdown formatting.
- Git push uses existing task/controller credentials. No new trust boundary.
- Task ID in command must match a file in the vault. Unknown IDs are logged and skipped.

## Acceptance Criteria

- [ ] task/executor passes `TASK_ID` env var to spawned Jobs (unit test)
- [ ] task/controller consumes from `agent-task-v1-request` (unit test with mock consumer)
- [ ] Task file frontmatter is updated with new status and phase (unit test)
- [ ] Result section is appended with output, links, and timestamp (unit test)
- [ ] Duplicate results don't create duplicate Result sections (unit test)
- [ ] Unknown task IDs are logged and skipped (unit test)
- [ ] task/controller commits and pushes after writing (unit test with mock git)
- [ ] `docs/agent-job-interface.md` reflects agent-publishes-result pattern
- [ ] `make precommit` passes in task/executor/ and task/controller/

## Verification

```
cd task/executor && make precommit
cd task/controller && make precommit
```

## Do-Nothing Option

Results stay in K8s Pod logs. Humans must `kubectl logs` each completed Job and manually update the task file. Tolerable for 1-2 Jobs per day but unworkable as agent usage grows. The vault never reflects what happened without manual intervention.
