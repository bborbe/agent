---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-03-28T11:25:45Z"
prompted: "2026-03-28T11:38:11Z"
verifying: "2026-03-28T12:05:06Z"
branch: dark-factory/task-to-prompt-consumer
---

## Summary

- Bridge the gap between task management and agent execution: task events in, prompt events out
- Tasks in progress (status=in_progress) automatically become executable prompts — no manual intervention needed
- Completes the second link in the agent pipeline: vault → task events → **prompt events** → execution
- Restart-safe: duplicate prompts are suppressed in-memory; downstream consumers are idempotent
- Runs alongside the existing HTTP server with graceful shutdown

## Problem

prompt/controller is currently a skeleton HTTP server with no data pipeline. Without a consumer, task events published by task/controller sit in Kafka unprocessed. No prompts are generated, so prompt/executor has nothing to execute. The entire agent pipeline from task to prompt to execution is broken at the task-to-prompt boundary.

## Goal

After this work, prompt/controller continuously converts qualifying task events into prompt events. Any task with status=in_progress and a non-empty assignee produces exactly one prompt event. The service consumes from agent-task-v1-event and publishes to agent-prompt-v1-event, bridging the gap between task management and agent execution.

## Non-goals

- Prompt result handling (consuming execution outcomes)
- Task status write-back (updating tasks based on prompt results)
- Persistent duplicate tracking (BoltDB, Redis) -- in-memory map is sufficient for MVP
- Batch/windowed processing -- one event at a time is fine
- Prompt parameter generation from task metadata -- Instruction only for now
- Re-prompting on task content changes (same TaskIdentifier = skip)

## Desired Behavior

1. On startup, the service begins consuming from the task event topic determined by branch.
2. For each consumed task event, the service checks whether status is "in_progress" and assignee is non-empty. Events that do not match are acknowledged and skipped.
3. The service checks whether a prompt was already generated for this task's TaskIdentifier. If so, the event is acknowledged and skipped.
4. For qualifying events, the service constructs a Prompt with: a new PromptIdentifier (UUID), the task's TaskIdentifier, the task's Assignee, and Instruction derived from the task's Content field.
5. The Prompt is published to the prompt event topic.
6. After successful publish, the TaskIdentifier is recorded to prevent duplicates.
7. The consumer and HTTP server run concurrently. Context cancellation stops both with graceful shutdown.

## Constraints

- Existing HTTP server endpoints (healthz, readiness, metrics) must continue working unchanged
- `lib.Task`, `lib.Prompt`, `lib.TaskV1SchemaID`, `lib.PromptV1SchemaID` are frozen -- do not modify
- Use the same EventObjectSender pattern as task/controller (SyncProducer -> JSONSender -> EventObjectSender)
- Use the same consumer pattern as trading notification/controller (OffsetConsumerHighwaterMarks with offset store)
- CLI flags: kafka-brokers, branch, listen (plus existing sentry flags). Use same struct tag conventions as task/controller.
- Factory functions remain pure composition -- no conditionals, no I/O, no context creation
- All new interfaces must have generated mocks; all tests use project test framework
- Consumer must respect context cancellation for graceful shutdown

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Kafka broker unreachable on startup | Service blocks on consumer creation, retries per libkafka defaults | Operator checks broker connectivity |
| Malformed task event (cannot deserialize) | Log warning, skip message, advance offset | Event is lost; producer must republish if needed |
| Task event missing required fields (no TaskIdentifier) | Log warning, skip message | Re-evaluated if task/controller republishes |
| Kafka producer fails when publishing prompt | Log error, do not mark TaskIdentifier as processed | Event will be retried on next consumer cycle |
| Service restarts (in-memory map lost) | All qualifying tasks re-processed; duplicate prompts published | Downstream consumers must be idempotent |
| Task event with status != in_progress | Silently skipped, offset advanced | No action needed |
| Task event with empty assignee | Silently skipped, offset advanced | No action needed |

## Security / Abuse Cases

- Kafka topics are derived from frozen schema IDs and operator-configured branch -- no injection risk
- Task content flows into Prompt Instruction as-is -- no sanitization needed because both are internal system events, not user-facing HTTP input
- No HTTP endpoints accept external input beyond healthz/readiness/metrics
- Consumer group ID should be deterministic and service-specific to prevent cross-service offset conflicts

## Acceptance Criteria

- [ ] prompt/controller accepts kafka-brokers and branch CLI flags
- [ ] Service consumes from agent-task-v1-event topic
- [ ] Task events with status=in_progress and non-empty assignee produce a prompt event
- [ ] Task events with status != in_progress or empty assignee are skipped
- [ ] Duplicate TaskIdentifiers do not produce additional prompt events (within same process lifetime)
- [ ] Prompt events are published to agent-prompt-v1-event topic via EventObjectSender
- [ ] Published Prompt contains valid PromptIdentifier (UUID), TaskIdentifier, Assignee, and Instruction
- [ ] HTTP server (healthz, readiness, metrics) runs concurrently with consumer
- [ ] Graceful shutdown: context cancellation stops both consumer and HTTP server
## Assumptions

- Kafka topics for task and prompt events already exist (created by infrastructure)
- Task events conform to the `lib.Task` schema
- Downstream prompt consumers are idempotent (safe to receive duplicate prompts after restart)
- In-memory duplicate tracking is sufficient for MVP (no persistent store needed)

## Verification

```
cd prompt/controller && make precommit
```

## Do-Nothing Option

Without this consumer, task events accumulate in Kafka with no downstream effect. prompt/executor has no prompts to execute, and the agent system cannot act on tasks. The pipeline remains broken at the task-to-prompt boundary. No interim workaround exists — manual prompt creation would defeat the purpose of automation. Doing nothing blocks all agent automation.
