---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-03-29T12:51:51Z"
prompted: "2026-03-29T12:57:19Z"
verifying: "2026-03-29T19:53:33Z"
completed: "2026-03-29T19:54:52Z"
branch: dark-factory/task-executor-service
---

## Summary

- Eliminate the Prompt layer by building `task/executor`, a new service that directly consumes task events from Kafka and spawns K8s Jobs
- Pipeline simplifies from task/controller -> prompt/controller -> prompt/executor -> agent job to task/controller -> task/executor -> agent job
- Reuses the existing filtering logic and in-memory deduplication from prompt/controller
- Resolves assignee to container image via a static lookup; unknown assignees are skipped
- Ships Job spawning only; result capture deferred to a future spec

## Problem

The agent pipeline has an unnecessary intermediate layer. Task events flow through prompt/controller (which creates prompt events) and then prompt/executor (which spawns Jobs from prompts). The Prompt abstraction adds no value -- task content IS the prompt. This extra hop increases latency, adds a Kafka topic, and doubles the code surface to maintain.

## Goal

After this work, a single `task/executor` service consumes task events from Kafka and directly spawns K8s Jobs for qualifying tasks. The prompt/controller and prompt/executor services become obsolete (removal is a separate follow-up). The new service follows the same skeleton, conventions, and dependency patterns as prompt/controller.

## Non-goals

- Removing prompt/controller or prompt/executor (separate cleanup)
- Capturing Job stdout/results back to Kafka
- Dynamic assignee-to-image resolution via CRD or config
- Changing the task event schema or Kafka topic
- Adding retry/requeue logic for failed Jobs

## Desired Behavior

1. Service starts a Kafka consumer on `agent-task-v1-event` topic and an HTTP server with /healthz, /readiness, /metrics, /setloglevel endpoints
2. Each consumed task event is filtered: skip unless status=in_progress, phase is one of {planning, in_progress, ai_review}, and assignee is non-empty
3. An in-memory deduplication tracker prevents spawning a second Job for a task ID that already has one in this process lifetime
4. Assignee is resolved to a container image; unknown assignees log a warning and are skipped
5. A K8s batch/v1 Job is created in the service's namespace with the resolved image, task content, broker connection info, and branch context. Jobs do not retry on failure
6. K8s RBAC resources (ServiceAccount, Role, RoleBinding) grant the service permission to create/get/list/watch/delete batch/v1 Jobs
7. Service gracefully shuts down on SIGTERM: stops Kafka consumer, waits for in-flight handler, then stops HTTP server

## Constraints

**Must not change:**
- The `agent-task-v1-event` Kafka topic and the lib.Task schema
- Consumer group must be distinct from prompt/controller's group

**Frozen conventions (follow prompt/controller patterns):**
- Per-service go.mod with `replace ../../lib`, Makefile with standard targets, Dockerfile
- `service.Run` for concurrent goroutine lifecycle, `service.Main` for signal handling + Sentry
- Counterfeiter mocks, ginkgo/gomega tests
- Consumer group name: `agent-task-executor`
- CLI flags: `--listen`, `--kafka-brokers`, `--branch`, `--sentry-dsn`, `--sentry-proxy`
- K8s manifests in `task/executor/k8s/` following prompt/controller/k8s/ pattern
- Job name format: `agent-{taskID-short}`
- Env vars on spawned Job: `TASK_CONTENT`, `KAFKA_BROKERS`, `BRANCH`
- `restartPolicy=Never`, `backoffLimit=0`

## Assumptions

- Task content IS the prompt — no transformation needed between task and agent input
- In-memory deduplication is sufficient; reset on restart is acceptable (K8s AlreadyExists guards against duplicates after restart)
- K8s env var size limit (~1MB) is sufficient for task content
- Existing prompt/controller and prompt/executor continue running in parallel until explicit cleanup

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Unknown assignee in task event | Log warning, skip event, commit offset | Add assignee to hardcoded map and redeploy |
| K8s API unreachable | Return error from handler, Kafka retries message | Consumer retries on next poll cycle |
| Job name collision (duplicate task ID) | K8s returns AlreadyExists error | Log and treat as success (job already running), mark as processed in dedup tracker |
| Malformed task JSON | Log warning, skip, commit offset | Fix upstream producer |
| Job creation quota exceeded | Return error, Kafka retries | Increase namespace quota or wait for existing jobs to complete |

## Security / Abuse Cases

- Task content is passed as an env var to the spawned Job; content size is bounded by Kafka message size limit and K8s env var limit (~1MB). No file-system write needed.
- The service only creates Jobs in its own namespace via RBAC Role (not ClusterRole). It cannot affect other namespaces.
- Assignee-to-image map is hardcoded in Go; no external input can influence which images are run.
- The dedup tracker is in-memory and resets on restart. After restart, duplicate Jobs are prevented by K8s Job name collision (AlreadyExists).

## Acceptance Criteria

- [ ] `make precommit` passes in task/executor/
- [ ] Service binary starts, connects to Kafka, and consumes from agent-task-v1-event topic
- [ ] Task events with wrong status, missing phase, or empty assignee are skipped (unit test)
- [ ] Duplicate task IDs do not spawn a second Job (unit test)
- [ ] Unknown assignee logs warning and skips (unit test)
- [ ] Valid task event triggers K8s Job creation with correct name, image, env vars (unit test with mock K8s client)
- [ ] K8s AlreadyExists error is handled gracefully (unit test)
- [ ] K8s manifests include Deployment, Service, Secret, and RBAC resources
- [ ] All external dependencies (K8s client, event handler) are injected and mockable

## Verification

```
cd task/executor && make precommit
```

## Do-Nothing Option

Keep the current three-hop pipeline (task/controller -> prompt/controller -> prompt/executor). It works but adds unnecessary latency, an extra Kafka topic, and more code to maintain. The Prompt abstraction provides no value since task content is passed through unchanged. Acceptable short-term but increases operational burden as more agent types are added.
