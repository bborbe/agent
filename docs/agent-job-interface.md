# Agent Job Interface

Contract for agents in the task controller system. Three patterns exist.

## Agent Patterns

### Pattern 0: Git-native

Agent reads task files from git, does work, writes result back to git directly. No Kafka, no K8s.

**Examples:** dark-factory, task-watcher

**Contract:** None — agent manages its own git read/write.

### Pattern A: Persistent Service (Deployment/StatefulSet)

Always-running service that consumes `agent-task-v1-event` and publishes `agent-task-v1-request`.

**Examples:** future agent-researcher

**Contract:**
- Consume: `agent-task-v1-event` topic
- Filter: status=in_progress, phase ∈ {planning, in_progress, ai_review}, matching assignee
- Produce: `agent-task-v1-request` topic with updated task content
- Use CQRS CommandObjectSender pattern from `github.com/bborbe/cqrs`

### Pattern B: Ephemeral Job via task/executor

Short-lived K8s Job spawned by task/executor. Agent receives task content via env vars, does work, publishes result to Kafka, exits.

**Examples:** agent-backtest

**Contract:** defined below.

---

## Pattern B: Job Interface (detailed)

### Environment Variables

task/executor passes these env vars to every spawned Job:

| Env Var | Required | Description |
|---------|----------|-------------|
| `TASK_CONTENT` | yes | Raw task markdown from the vault. The agent parses this to extract domain-specific parameters. |
| `TASK_ID` | yes | Task identifier. Agent uses this when publishing `agent-task-v1-request` to reference which task to update. |
| `KAFKA_BROKERS` | yes | Comma-separated Kafka broker addresses. |
| `BRANCH` | yes | Environment branch (`develop`, `master`). |
| `SENTRY_DSN` | no | Sentry DSN for error reporting. |
| `SENTRY_PROXY` | no | Sentry proxy URL. |

### TASK_CONTENT Format

The markdown body of a vault task file **after the frontmatter closing delimiter**. Frontmatter is stripped by task/executor before passing to the agent. The agent receives only the content section.

Example task file in vault:

```markdown
---
status: in_progress
phase: in_progress
assignee: backtest-agent
tags:
  - agent-task
task_identifier: e2e-test-0001-bbr-eurusd-1h
---
Run backtest for strategy `BBR-EURUSD-1H` from 2025-01-01 to 2025-12-31.

## Parameters

- **Strategy:** BBR-EURUSD-1H
- **From:** 2025-01-01
- **Until:** 2025-12-31
```

What the agent receives in `TASK_CONTENT`:

```markdown
Run backtest for strategy `BBR-EURUSD-1H` from 2025-01-01 to 2025-12-31.

## Parameters

- **Strategy:** BBR-EURUSD-1H
- **From:** 2025-01-01
- **Until:** 2025-12-31
```

Each agent type defines what it expects in the task body. The agent does NOT receive frontmatter — it only gets the content after `---`.

### Result Publishing

The agent publishes its result as an `agent-task-v1-request` command to Kafka using the CQRS CommandObjectSender pattern. The command contains:

- Task identifier (from `TASK_ID` env var)
- Updated task content (full markdown with result merged)
- New status/phase based on outcome

### Status Mapping

| Agent Outcome | Task Status | Task Phase |
|---------------|-------------|------------|
| Success | completed | done |
| Needs human input | in_progress | human_review |
| Failed (recoverable) | in_progress | human_review |

### Stdout (optional, debug only)

Agents may write a JSON summary to stdout for debugging/logging. This is NOT the primary result transport — Kafka is.

```json
{
  "status": "done|needs_input|failed",
  "output": "human-readable summary",
  "message": "error message (optional)",
  "links": ["https://example.com/result (optional)"]
}
```

### Exit Code

| Code | Meaning |
|------|---------|
| 0 | Clean exit — result published to Kafka |
| non-zero | Crash — no result published, task stays in_progress |

### Job Lifecycle

1. task/executor consumes `agent-task-v1-event`, spawns K8s Job with env vars
2. Agent starts, reads `TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`
3. Agent parses TASK_CONTENT for domain parameters
4. Agent does domain work (API calls, Kafka commands, etc.)
5. Agent publishes `agent-task-v1-request` with updated task content
6. Agent exits 0
7. task/controller consumes request, updates task file, git commit+push
8. Obsidian shows updated task

### task/executor Role

task/executor is a **Job launcher only**:
- Consumes `agent-task-v1-event`
- Filters (status/phase/assignee)
- Resolves assignee → image
- Spawns K8s Job with env vars
- Does NOT watch Jobs, read stdout, or publish results

The agent handles its own result publishing. If the agent crashes without publishing, the task stays in_progress — the human notices and re-triggers.

### Implementation Pattern (Go)

```go
type application struct {
    SentryDSN    string           `required:"false" arg:"sentry-dsn"    env:"SENTRY_DSN"`
    SentryProxy  string           `required:"false" arg:"sentry-proxy"  env:"SENTRY_PROXY"`
    KafkaBrokers libkafka.Brokers `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS"`
    Branch       base.Branch      `required:"true"  arg:"branch"        env:"BRANCH"`
    TaskContent  string           `required:"true"  arg:"task-content"  env:"TASK_CONTENT"`
    TaskID       string           `required:"true"  arg:"task-id"       env:"TASK_ID"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
    // 1. Parse task content (agent-specific logic)
    params, err := parseTaskContent(a.TaskContent)
    if err != nil {
        return publishTaskUpdate(ctx, a, "needs_input", fmt.Sprintf("failed to parse task: %v", err))
    }

    // 2. Do domain work
    result, err := doWork(ctx, params)
    if err != nil {
        return publishTaskUpdate(ctx, a, "failed", fmt.Sprintf("work failed: %v", err))
    }

    // 3. Publish result and exit 0
    return publishTaskUpdate(ctx, a, "done", result.Summary)
}

func publishTaskUpdate(ctx context.Context, a *application, status, message string) error {
    // Create CQRS command sender
    syncProducer, _ := createSyncProducer(ctx, a.KafkaBrokers)
    defer syncProducer.Close()

    sender := createTaskUpdateCommandSender(ctx, syncProducer, a.Branch)

    // Publish agent-task-v1-request with updated task
    return sender.Send(ctx, TaskUpdateCommand{
        TaskID:  a.TaskID,
        Status:  status,
        Message: message,
    })
}
```

## Checklist for New Agents

### Pattern B (K8s Job)

- [ ] Reads `TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH` from env
- [ ] Parses task markdown to extract domain parameters
- [ ] Publishes result to `agent-task-v1-request` using CQRS pattern
- [ ] Publishes `needs_input` for missing/invalid parameters (not crash)
- [ ] Exits 0 on all handled cases (done, needs_input, failed)
- [ ] Logs to stderr (glog), never to stdout
- [ ] Uses `service.Main` for signal handling and Sentry
- [ ] Has Dockerfile, Makefile, go.mod (standalone module)
- [ ] Has `main_test.go` with ginkgo compile test

### Pattern A (Persistent Service)

- [ ] Consumes `agent-task-v1-event` with own consumer group
- [ ] Filters: status=in_progress, qualifying phase, matching assignee
- [ ] Deduplicates (don't process same task twice)
- [ ] Publishes result to `agent-task-v1-request` using CQRS pattern
- [ ] HTTP server with /healthz, /readiness, /metrics
- [ ] Uses `service.Run` for concurrent Kafka + HTTP

## Differences: Pattern A vs Pattern B

| | Pattern A | Pattern B |
|---|-----------|-----------|
| **Lifecycle** | Permanent Deployment | Ephemeral K8s Job |
| **Event consumption** | Direct from Kafka | task/executor spawns it |
| **Result publishing** | Direct to Kafka | Direct to Kafka |
| **Kafka dependency** | Yes | Yes |
| **When to use** | Long-running, always listening | Short-lived, scales to zero |
