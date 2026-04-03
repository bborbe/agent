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
develop-agent-task-v1-result      (future: query responses)
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
  → publishes agent-task-v1-event (confirms change)
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
