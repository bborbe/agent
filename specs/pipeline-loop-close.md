---
tags:
  - dark-factory
  - spec
status: draft
---

## Summary

- task/executor gains Job watching: after spawning a K8s Job, it watches for completion, reads the agent's stdout JSON, and publishes the result to `agent-task-v1-request` Kafka topic
- task/controller gains a request consumer: it consumes `agent-task-v1-request` messages, updates the corresponding task file (frontmatter + appended Result section), and commits/pushes to git
- This closes the end-to-end pipeline loop: human edits task in Obsidian -> agent runs as K8s Job -> task file updated with outcome automatically
- Supersedes the draft spec `agent-result-capture.md` which proposed agents self-publish; this spec keeps agents simple (stdout-only) per the Job interface contract

## Problem

The agent pipeline is broken at the last mile. task/executor spawns K8s Jobs but immediately forgets about them. When an agent finishes, its result sits in Pod logs. Nobody reads it. The task file in Obsidian never learns what happened. Humans must manually check `kubectl logs` and edit the task file. This makes autonomous execution impossible and prevents the system from scaling beyond manually supervised single Jobs.

## Goal

After this work, the pipeline is a closed loop. A task that enters `in_progress` with an assignee triggers a Job, the Job runs, and the task file is automatically updated with the result — including status change, phase change, and a human-readable Result section with output, links, and timestamp. No human intervention required for the happy path.

## Non-goals

- Retry logic for failed Jobs (manual re-trigger via task edit)
- Streaming logs during Job execution
- Timeout or kill of long-running Jobs (orphan detection is separate)
- Changing the agent-job-interface stdout contract (agents still print JSON to stdout)
- Prompt-level schemas (`agent-prompt-v1-*`) — this spec uses only the task-level schema

## Desired Behavior

1. After task/executor creates a K8s Job, it watches that Job until it reaches a terminal state (succeeded or failed)
2. On Job success, task/executor reads the Pod's stdout to extract the agent's JSON result (`{status, output, message, links}`)
3. On Job failure (non-zero exit or no valid JSON), task/executor synthesizes a failed result with the error information from Pod logs
4. task/executor publishes the result as an `agent-task-v1-request` message to Kafka, containing: task identifier, mapped status/phase, output text, links, and timestamp
5. task/controller consumes `agent-task-v1-request` messages from Kafka using a dedicated consumer group
6. task/controller updates the task markdown file: sets frontmatter status/phase per the mapping, and appends a timestamped Result section with the agent's output and links
7. task/controller commits and pushes the change to git, then publishes an `agent-task-v1-event` to confirm the update

## Constraints

**Must not change:**
- The `agent-task-v1-event` topic schema or existing git-to-Kafka sync behavior in task/controller
- task/controller remains the single git writer for the vault repo
- The agent stdout JSON contract defined in `docs/agent-job-interface.md`
- The lib.Task struct fields and validation
- Factory functions remain pure composition (no I/O, no conditionals)

**Frozen conventions:**
- Status mapping: agent `done` -> task status=completed; agent `needs_input` -> phase=human_review; agent `failed` -> phase=human_review
- CQRS pattern for Kafka message publishing/consuming via `github.com/bborbe/cqrs`
- Consumer group for request consumption must be distinct from existing groups
- `TASK_ID` env var must be added to spawned Jobs so the result can be correlated back

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Job succeeds but stdout has no valid JSON | Treat as failed with message "invalid agent output" | Human checks Pod logs, re-triggers |
| Job fails (non-zero exit) | Publish failed result with error from Pod logs | Human investigates, re-triggers |
| Job hangs indefinitely | Not handled in this spec (future: TTL or active deadline) | Human deletes Job manually |
| Pod logs unavailable (evicted before read) | Publish failed result with message "unable to read agent output" | Human checks K8s events |
| Kafka unavailable when publishing result | Retry with backoff; if persistent, log error and skip | Human re-triggers task |
| Git push conflict in task/controller | Pull, rebase, retry push (existing pattern) | Automatic recovery |
| Task file deleted before result arrives | Log warning, skip update, commit Kafka offset | No harm, human is aware |
| Duplicate result for same task | Idempotent: re-applying same status is a no-op, Result section checks timestamp | No harm |
| task/executor restarts mid-watch | Job still exists in K8s; on restart, in-flight Jobs are orphaned for this spec (no recovery) | Human re-triggers; future: reconciliation loop |

## Security / Abuse Cases

- Agent stdout is untrusted input. Written into markdown as-is. No shell execution or template rendering occurs, so injection risk is limited to markdown formatting artifacts.
- Pod log reading is scoped to the namespace task/executor runs in. No cross-namespace access.
- The task identifier in the result must match the `TASK_ID` env var passed to the Job. No external input can redirect results to arbitrary tasks.
- Git push uses existing task/controller credentials. No new trust boundary.
- Pod log size: agents should keep stdout small (single JSON line). If an agent writes excessive stdout, only the last line or first valid JSON object should be parsed. Set a reasonable read limit to prevent memory exhaustion.

## Acceptance Criteria

- [ ] task/executor watches spawned Jobs until terminal state (unit test with fake K8s client)
- [ ] task/executor reads stdout JSON from succeeded Job's Pod logs (unit test)
- [ ] task/executor handles non-zero exit / missing JSON gracefully (unit test)
- [ ] task/executor publishes `agent-task-v1-request` with correct status mapping (unit test)
- [ ] task/executor passes `TASK_ID` env var to spawned Jobs (unit test — verify existing test updated)
- [ ] task/controller consumes `agent-task-v1-request` with dedicated consumer group (unit test)
- [ ] task/controller updates task file frontmatter (status, phase) correctly for all three outcomes (unit test)
- [ ] task/controller appends timestamped Result section with output and links (unit test)
- [ ] task/controller commits and pushes after writing (unit test with mock git client)
- [ ] task/controller publishes `agent-task-v1-event` after successful update (unit test)
- [ ] Duplicate results handled idempotently (unit test)
- [ ] `make precommit` passes in both `task/executor/` and `task/controller/`

## Verification

```
cd task/executor && make precommit
cd task/controller && make precommit
```

## Do-Nothing Option

Results stay in K8s Pod logs. Humans must `kubectl logs` each completed Job and manually update task files. Tolerable for 1-2 Jobs per day but unworkable as agent usage grows. The vault never reflects what happened, making the Obsidian dashboard useless for tracking agent outcomes. The existing draft spec `agent-result-capture.md` proposes an alternative (agents self-publish) but that adds Kafka publishing complexity to every agent, contradicting the simple stdout-only contract.
