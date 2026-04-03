# Task Executor Design (task/executor)

The task executor bridges Kafka and Kubernetes. It consumes task events, filters by status/phase/assignee, resolves the assignee to a container image, and spawns K8s Jobs. It is the only component that talks to the K8s API.

## Inputs / Outputs

| Direction | Source/Target | Purpose |
|-----------|--------------|---------|
| Consumes | `agent-task-v1-event` (Kafka) | Task changed in vault |
| Creates | K8s Job API | Spawn agent container |

## Logic

```
On agent-task-v1-event:
  │
  ├── filter: status must be in_progress
  ├── filter: phase must be planning, in_progress, or ai_review
  ├── filter: assignee must match a known agent type
  ├── deduplicate: skip if same task already processed
  │
  ├── resolve assignee → container image (hardcoded map)
  ├── create K8s Job:
  │     image: resolved from assignee
  │     env: TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH
  │
  └── done — does NOT watch the Job
```

## What task/executor Does NOT Do

- Does NOT watch Jobs for completion
- Does NOT read stdout/logs from Jobs
- Does NOT publish results to Kafka
- Does NOT manage retries or heartbeats

The agent inside the Job publishes its own result directly to `agent-task-v1-request`. See [agent-job-interface.md](agent-job-interface.md) for the full contract.

## Why This Component Exists

Decoupling task/controller from K8s means:
- Controller is pure git + Kafka — testable, simple
- Execution runtime is swappable:

| Today | Tomorrow |
|-------|----------|
| K8s Jobs | Docker containers |
| | Lambda functions |
| | Permanent deployments |
| | Local process |

Swap the executor, everything else stays the same.
