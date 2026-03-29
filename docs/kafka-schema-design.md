# Kafka Schema Design

Two Kafka schemas power the agent system. Follows the existing cdb-schema-v1 pattern with `{branch}-{group}-{kind}-{version}-{action}` topic naming.

## Schemas

| Schema | Group | Kind | Version |
|--------|-------|------|---------|
| `agent-task-v1` | agent | task | v1 |
| `agent-prompt-v1` | agent | prompt | v1 |

## Topics

```
develop-agent-task-v1-event       task service → controller
develop-agent-task-v1-request     controller → task service
develop-agent-task-v1-result      task service → controller (query responses)

develop-agent-prompt-v1-request   controller → job creator
develop-agent-prompt-v1-event     (future: prompt status tracking)
develop-agent-prompt-v1-result    job creator → controller
```

Primary flow uses 4 topics:

```
agent-task-v1-event         task changed → controller reacts
agent-task-v1-request       controller → update task
agent-prompt-v1-request     controller → execute this prompt
agent-prompt-v1-result      job done → here's the result
```

## Message Formats

### agent-task-v1-event

Published by task service when a task changes.

```json
{
  "id": "task-123",
  "schemaId": {"group": "agent", "kind": "task", "version": "v1"},
  "event": {
    "name": "Backtest ORB USOIL V3",
    "status": "todo",
    "assignee": "backtest-agent",
    "content": "## Request\nStrategy: ORB USOIL.cash V3\nFrom: 2025-01-01\nTo: 2025-12-31",
    "execution_log": []
  }
}
```

### agent-task-v1-request

Published by controller to update a task.

```json
{
  "id": "task-123",
  "schemaId": {"group": "agent", "kind": "task", "version": "v1"},
  "operation": "update",
  "data": {
    "status": "done",
    "append_log": "### Run 3 — 2026-03-25 10:30\nBacktest completed. PF: 1.4, 210 trades\nResults: [link]"
  }
}
```

### agent-prompt-v1-request

Published by controller to trigger job execution.

```json
{
  "id": "prompt-456",
  "schemaId": {"group": "agent", "kind": "prompt", "version": "v1"},
  "data": {
    "task_id": "task-123",
    "assignee": "backtest-agent",
    "instruction": "Backtest ORB USOIL.cash V3",
    "parameters": {
      "strategy": "ORB USOIL.cash V3",
      "from": "2025-01-01",
      "to": "2025-12-31"
    },
    "execution_log": [
      {"run": 1, "timestamp": "2026-03-25T10:00:00Z", "message": "triggered abc-123"},
      {"run": 2, "timestamp": "2026-03-25T10:15:00Z", "message": "still running"}
    ]
  }
}
```

### agent-prompt-v1-result

Published by job creator after job completes.

```json
{
  "id": "result-789",
  "schemaId": {"group": "agent", "kind": "prompt", "version": "v1"},
  "data": {
    "prompt_id": "prompt-456",
    "task_id": "task-123",
    "status": "done",
    "output": "PF: 1.4, Trades: 210, Win Rate: 42%",
    "message": "Backtest completed successfully",
    "links": ["https://trading.example.com/backtest/abc-123"]
  }
}
```

Result status values:

| Status | Meaning | Controller action |
|--------|---------|-------------------|
| `done` | Work completed | Set task done, append results |
| `running` | Still in progress | Keep task in_progress, append log |
| `needs_input` | Missing info | Set task human_review, append question |
| `failed` | Unrecoverable error | Set task human_review, append error |

## Boundary: agent-* vs core-*

```
agent-task-v1       ← agent infrastructure (task management)
agent-prompt-v1     ← agent infrastructure (work execution)

core-backtest-v1    ← trading domain (called BY agent jobs)
core-strategy-v1    ← trading domain (called BY agent jobs)
```

Agent jobs interact with `core-*` topics directly. The agent schemas (`agent-*`) are the orchestration layer — they don't contain domain data, only coordination.
