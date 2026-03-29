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
Read TASK_CONTENT → parse strategy, dates from markdown

Missing/invalid params?
  → stdout: {status: needs_input, message: "missing: strategy name"}
  → exit 0

All params valid:
  → send BacktestQueueCommand via core-backtest-v1 Kafka topic
  → consume core-backtest-v1 event topic, wait for DONE/FAILED
  → DONE → stdout: {status: done, output: "backtest completed for BBR-EURUSD-1H"}
  → FAILED → stdout: {status: failed, message: "insufficient data"}
  → exit 0
```

Note: backtest agent waits for completion in a single run (consumes Kafka events). Heartbeat re-spawning applies to agents that can't wait synchronously.

## Lifecycle Examples

### Happy Path

```
1. Task created (assignee: backtest-agent, strategy + dates in body)
2. task/executor spawns Job with TASK_CONTENT
3. Job parses markdown → valid → triggers backtest → waits on event topic
4. Backtest completes → stdout: {status: done, output: "PF: 1.4, 210 trades"}
5. Job exits 0
6. task/executor reads stdout → publishes result → task updated to done
```

### Missing Params

```
1. Task created ("backtest ORB" — no dates or ambiguous strategy)
2. Job parses markdown → missing strategy identifier
3. stdout: {status: needs_input, message: "missing: strategy identifier"}
4. Job exits 0
5. Task updated to human_review, human adds details, sets planning
6. Controller re-triggers → new job → this time params valid → proceeds
```

### Backtest Failure

```
1. Job triggers backtest, waits on event topic
2. Backtest fails: "insufficient data for USOIL 2020-01-01"
3. stdout: {status: failed, message: "backtest failed: insufficient data"}
4. Job exits 0
5. Task updated to human_review with error message
```

## Properties

| Property | Description |
|----------|-------------|
| **Stateless** | All context from TASK_CONTENT, no local state |
| **Short-lived** | Runs once, exits. Minutes, not hours |
| **Domain-specific** | Each agent type is a separate container in its domain repo |
| **Kafka for domain only** | Uses domain topics (core-backtest-v1), not agent-* topics |
| **Stdout = result** | Single JSON line, read by task/executor |
