# Agent Job Interface

Contract for agents in the task controller system. Three patterns exist — this doc covers all three.

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

Short-lived K8s Job spawned by task/executor. This is the simplest agent to build — just read env vars, do work, print JSON.

**Examples:** agent-backtest

**Contract:** defined below.

---

## Pattern B: Job Interface (detailed)

### Environment Variables

task/executor passes these env vars to every spawned Job:

| Env Var | Required | Description |
|---------|----------|-------------|
| `TASK_CONTENT` | yes | Raw task markdown from the vault. The agent parses this to extract domain-specific parameters. |
| `TASK_ID` | yes | Task identifier. Used by task/executor to correlate Job result back to task. |
| `KAFKA_BROKERS` | yes | Comma-separated Kafka broker addresses. |
| `BRANCH` | yes | Environment branch (`develop`, `master`). |
| `SENTRY_DSN` | no | Sentry DSN for error reporting. |
| `SENTRY_PROXY` | no | Sentry proxy URL. |

### TASK_CONTENT Format

Raw markdown body of a vault task file. Example:

```markdown
---
status: in_progress
phase: in_progress
assignee: backtest-agent
tags:
  - agent-task
---
Tags: [[Build Backtest Agent as First Controller Job]]

---

Run backtest for strategy `BBR-EURUSD-1H` from 2025-01-01 to 2025-12-31.

## Parameters

- **Strategy:** BBR-EURUSD-1H
- **From:** 2025-01-01
- **Until:** 2025-12-31
```

Each agent type defines what it expects in the task body. The frontmatter is included but agents should focus on the content section.

### Stdout Contract

The agent **must** write exactly one JSON line to stdout before exiting. task/executor reads this to create the task update request.

```json
{
  "status": "done|needs_input|failed",
  "output": "human-readable summary of what happened",
  "message": "error or status message (optional)",
  "links": ["https://example.com/result (optional)"]
}
```

### Status Values

| Status | Meaning | Task Update (by task/executor) |
|--------|---------|-------------------------------|
| `done` | Agent completed successfully | status → completed, phase → done |
| `needs_input` | Agent needs human clarification | phase → human_review |
| `failed` | Agent failed (recoverable) | phase → human_review |

### Rules

- Write JSON to **stdout**, not stderr. Logs go to stderr via glog.
- Write exactly **one** JSON object. No extra lines before or after.
- The `output` field is shown to the human in the task result section.
- The `message` field explains what went wrong (for `needs_input` and `failed`).
- The `links` field contains URLs to results (dashboards, reports, etc.).

### Exit Code

| Code | Meaning |
|------|---------|
| 0 | Clean exit — stdout JSON determines success/failure |
| non-zero | Crash — task/executor treats as `failed` with message "agent crashed" |

Always exit 0 if you wrote valid JSON to stdout, even for `failed` status.

### Job Lifecycle

1. task/executor creates the Job with env vars
2. Agent starts, reads env vars
3. Agent parses TASK_CONTENT for domain parameters
4. Agent does domain work (API calls, Kafka commands, etc.)
5. Agent writes JSON result to stdout
6. Agent exits with code 0
7. task/executor watches Job completion
8. task/executor reads stdout JSON
9. task/executor publishes `agent-task-v1-request` with mapped result
10. task/controller consumes request, updates task file, git commit+push

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
        return printResult(AgentResult{
            Status:  "needs_input",
            Message: fmt.Sprintf("failed to parse task: %v", err),
        })
    }

    // 2. Do domain work
    result, err := doWork(ctx, params)
    if err != nil {
        return printResult(AgentResult{
            Status:  "failed",
            Message: fmt.Sprintf("work failed: %v", err),
        })
    }

    // 3. Print result and exit 0
    return printResult(result)
}

func printResult(result AgentResult) error {
    data, _ := json.Marshal(result)
    fmt.Println(string(data))
    return nil
}
```

### AgentResult Struct

```go
type AgentResult struct {
    Status  string   `json:"status"`
    Output  string   `json:"output,omitempty"`
    Message string   `json:"message,omitempty"`
    Links   []string `json:"links,omitempty"`
}
```

This struct lives in each agent's own package (not shared). The JSON contract is the interface, not the Go type.

## Checklist for New Agents

### Pattern B (K8s Job)

- [ ] Reads `TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH` from env
- [ ] Parses task markdown to extract domain parameters
- [ ] Returns `needs_input` for missing/invalid parameters (not crash)
- [ ] Writes exactly one JSON line to stdout
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
