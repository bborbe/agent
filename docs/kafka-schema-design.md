# Kafka Schema Design

One Kafka schema powers the agent system. Follows the existing cdb-schema-v1 pattern with `{branch}-{group}-{kind}-{version}-{action}` topic naming.

## Schema

| Schema | Group | Kind | Version |
|--------|-------|------|---------|
| `agent-task-v1` | agent | task | v1 |

## Topics

```
develop-agent-task-v1-event       task/controller publishes when task files change
develop-agent-task-v1-request     agents publish commands (update task with results)
develop-agent-task-v1-result      CQRS command results (success/failure confirmation)
```

## Data Flow

```
Human edits task in Obsidian
  → obsidian-git pushes
  → task/controller detects change
  → publishes agent-task-v1-event

task/executor consumes event
  → spawns K8s Job with TASK_CONTENT + TASK_ID

Agent (K8s Job) does work
  → publishes agent-task-v1-request (operation: "update")

task/controller consumes request
  → writes result to task file
  → git commit + push
  → publishes agent-task-v1-result (success/failure confirmation)
  → publishes agent-task-v1-event (confirms file change)
```

## Message Formats

### agent-task-v1-event

Published by task/controller when a task file changes in git.

```json
{
  "id": "uuid",
  "schemaId": {"group": "agent", "kind": "task", "version": "v1"},
  "event": {
    "taskIdentifier": "4aa57614-8835-4992-87e9-ca483ddbeae6",
    "frontmatter": {
      "status": "in_progress",
      "phase": "in_progress",
      "assignee": "backtest-agent"
    },
    "content": "Run backtest for strategy BBR-EURUSD-1H..."
  }
}
```

### agent-task-v1-request

Published by agents to update a task. Consumed by task/controller.

```json
{
  "id": "uuid",
  "schemaId": {"group": "agent", "kind": "task", "version": "v1"},
  "operation": "update",
  "data": {
    "taskIdentifier": "4aa57614-8835-4992-87e9-ca483ddbeae6",
    "frontmatter": {
      "status": "completed",
      "phase": "done",
      "task_identifier": "4aa57614-8835-4992-87e9-ca483ddbeae6"
    },
    "content": "Original task content...\n\n## Result\n\nBacktest completed. PF: 1.4, 210 trades."
  }
}
```

The `content` field replaces the entire markdown body below the frontmatter. Include original task text + appended `## Result` section.

### agent-task-v1-result

Published automatically by the CQRS framework when the task/controller processes a command. Confirms whether the command was accepted or rejected. The command sender can correlate via `requestID`.

```json
{
  "requestID": "original-request-uuid",
  "success": true,
  "eventID": "e2e-test-0001-bbr-eurusd-1h",
  "event": { "...serialized task..." }
}
```

On failure:
```json
{
  "requestID": "original-request-uuid",
  "success": false,
  "message": "write result for task e2e-test-0001-bbr-eurusd-1h: commit and push failed: ..."
}
```

## CQRS Command Result Pattern

Every `CommandObjectExecutorTxFunc` has a `sendResultEnabled` flag (second parameter). This controls whether the CQRS framework automatically publishes to the `-result` topic.

### sendResultEnabled = true (recommended)

The framework handles result publishing:

| Executor returns | Result topic message |
|------------------|---------------------|
| `eventID, event, nil` | Success result with eventID + event |
| `nil, nil, err` | Failure result with error message |
| `nil, nil, ErrCommandObjectSkipped` | Nothing (silently skipped) |

The command sender receives confirmation that processing succeeded or failed. All trading command executors use this mode.

### sendResultEnabled = false (manual results)

The framework only publishes failure results (non-skipped errors). On success, no result is sent. Use this only when the executor needs to send results through a different channel (e.g., a custom topic or external system).

If `sendResultEnabled = false`, the executor is responsible for confirming command receipt through its own mechanism. Otherwise the command sender has no way to know if the command was processed.

### ErrCommandObjectSkipped

Wrapping an error with `cdb.ErrCommandObjectSkipped` tells the framework to silently drop the command — no result is published, no error is logged at the handler level. Use this for expected conditions like malformed payloads or validation failures that don't warrant a result message.

**Important**: The framework logs only "result returned skipped error => skip" at V3 — the wrapped reason is not visible. Add Warning-level logging before returning `ErrCommandObjectSkipped` if debugging visibility is needed.

### Status Mapping

| Agent Outcome | Task Status | Task Phase |
|---------------|-------------|------------|
| Success | completed | done |
| Needs human input | in_progress | human_review |
| Failed (recoverable) | in_progress | human_review |

## Content Sanitization

Bare `---` lines in agent content are escaped to `\-\-\-` by task/controller's ResultWriter to prevent YAML frontmatter corruption.

## Boundary: agent-* vs core-*

```
agent-task-v1       ← agent infrastructure (task management + orchestration)

core-backtest-v1    ← trading domain (called BY agent jobs)
core-strategy-v1    ← trading domain (called BY agent jobs)
```

Agent jobs interact with `core-*` topics directly. The `agent-task-v1` schema is the orchestration layer — it doesn't contain domain data, only coordination.
