# Agent Job Lifecycle

How agent jobs execute within the pipeline. For the interface contract (env vars, stdout format), see [agent-job-interface.md](agent-job-interface.md).

## What a Job Knows

| Knows | Doesn't know |
|-------|-------------|
| Task content (raw markdown from vault) | Controller, task/executor |
| Its own domain APIs (e.g. `core-backtest-v1` via Kafka) | Other agents, CRDs |
| How to produce structured JSON output | Git, vault-cli, task status |

## Job Logic (Backtest Agent Example)

```
Read TASK_CONTENT, TASK_ID → parse strategy, dates from markdown

Missing/invalid params?
  → publish agent-task-v1-request: status=in_progress, phase=human_review
  → exit 0

All params valid:
  → send BacktestQueueCommand via core-backtest-v1 Kafka topic
  → consume core-backtest-v1 event topic, wait for DONE/FAILED
  → DONE → publish agent-task-v1-request: status=completed, phase=done
  → FAILED → publish agent-task-v1-request: status=in_progress, phase=human_review
  → exit 0
```

Note: backtest agent waits for completion in a single run (consumes Kafka events). The agent publishes its result directly to Kafka — task/executor does NOT read stdout or watch Jobs.

## Lifecycle Examples

### Happy Path

```
1. Task created (assignee: backtest-agent, strategy + dates in body)
2. task/executor spawns Job with TASK_CONTENT + TASK_ID
3. Job parses markdown → valid → triggers backtest → waits on event topic
4. Backtest completes → agent publishes agent-task-v1-request (status: completed, phase: done)
5. Job exits 0
6. task/controller consumes request → writes result to task file → git push
```

### Missing Params

```
1. Task created ("backtest ORB" — no dates or ambiguous strategy)
2. Job parses markdown → missing strategy identifier
3. Agent publishes agent-task-v1-request (status: in_progress, phase: human_review)
4. Job exits 0
5. task/controller writes result → task shows human_review in Obsidian
6. Human adds details, sets planning → task/executor re-spawns → this time params valid
```

### Backtest Failure

```
1. Job triggers backtest, waits on event topic
2. Backtest fails: "insufficient data for USOIL 2020-01-01"
3. Agent publishes agent-task-v1-request (status: in_progress, phase: human_review, content includes error)
4. Job exits 0
5. task/controller writes result → task shows human_review with error in Obsidian
```

## Properties

| Property | Description |
|----------|-------------|
| **Stateless** | All context from TASK_CONTENT, no local state |
| **Short-lived** | Runs once, exits. Minutes, not hours |
| **Domain-specific** | Each agent type is a separate container in its domain repo |
| **Kafka for domain + result** | Uses domain topics (core-backtest-v1) AND publishes result to agent-task-v1-request |
| **Stdout = debug** | Optional JSON for logging, Kafka is primary result transport |
