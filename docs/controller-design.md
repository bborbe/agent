# Controller Design (prompt/controller)

The controller is a pure Kafka consumer/producer that orchestrates agent work. It consumes task events, creates prompts, and processes results. It has no K8s API access and no git access.

## Inputs / Outputs

| Direction | Topic | Purpose |
|-----------|-------|---------|
| Consumes | `agent-task-v1-event` | Task created or status changed |
| Consumes | `agent-prompt-v1-result` | Job finished, here's the result |
| Produces | `agent-prompt-v1-request` | Execute this prompt |
| Produces | `agent-task-v1-request` | Update task with results |

## Core Logic

```
On agent-task-v1-event:
  │
  ├── find AgentConfig CRD where spec.assignee == event.assignee
  ├── no match → ignore
  │
  ├── status: todo/planning → convert task → prompt, publish agent-prompt-v1-request
  ├── status: in_progress   → check heartbeat
  │     ├── last prompt > heartbeat ago → convert task → prompt, publish
  │     └── within heartbeat → skip
  ├── status: human_review  → do nothing
  └── status: done/failed   → do nothing

On agent-prompt-v1-result:
  │
  ├── result.status: done
  │   → publish agent-task-v1-request (set done, append results to log)
  │
  ├── result.status: needs_input
  │   → publish agent-task-v1-request (set human_review, append message to log)
  │
  └── result.status: failed
      → publish agent-task-v1-request (set human_review, append error to log)
```

## Task → Prompt Conversion

The controller strips task metadata and creates a self-contained prompt:

```
Task (agent-task-v1):               Prompt (agent-prompt-v1):
─────────────────────               ────────────────────────
status: in_progress                 assignee: backtest-agent
assignee: backtest-agent            instruction: "Backtest ORB USOIL V3"
goals: [...]                        parameters:
themes: [...]                         strategy: ORB USOIL.cash V3
related links: [...]                  from: 2025-01-01
                                      to: 2025-12-31
Request:                            execution_log:
  Backtest ORB USOIL V3              - "Run 1: triggered abc-123"
  From: 2025-01-01                    - "Run 2: still running"
  To: 2025-12-31

Execution Log:
  Run 1: triggered abc-123
  Run 2: still running
```

The prompt contains everything the job needs — nothing more.

## Heartbeat Tracking

The controller tracks per task:
- Last prompt sent timestamp
- Prompt ID (to correlate results)

Heartbeat interval comes from the AgentConfig CRD:

| Agent Type | Heartbeat | Reason |
|------------|-----------|--------|
| backtest-agent | 15m | Backtests take 5-30min |
| trade-analyser | 5m | Analysis is faster |
| youtube-processor | 1m | Quick processing |

## What the Controller Does NOT Do

- No K8s API calls (job creator handles that)
- No git operations (task service handles that)
- No domain logic (doesn't know what a backtest is)
- No job management (doesn't know about pods)
