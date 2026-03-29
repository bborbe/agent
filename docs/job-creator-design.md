# Job Creator Design (task/executor)

The job creator bridges Kafka and Kubernetes. It consumes prompt requests, spawns K8s Jobs, watches their completion, and publishes results. The controller never touches K8s — this component is the only one that does.

## Inputs / Outputs

| Direction | Source/Target | Purpose |
|-----------|--------------|---------|
| Consumes | `agent-prompt-v1-request` (Kafka) | Prompt to execute |
| Watches | K8s Job API | Job status (running, succeeded, failed) |
| Produces | `agent-prompt-v1-result` (Kafka) | Execution result |

## Logic

```
On agent-prompt-v1-request:
  │
  ├── read AgentConfig CRD for assignee → get image, resources
  ├── create K8s Job:
  │     image: CRD.spec.image
  │     env/args: prompt content (instruction, parameters, execution_log)
  │     resources: CRD.spec.resources
  │
  └── watch Job until completion
        │
        ├── succeeded → read Job output (stdout/logs)
        │   → publish agent-prompt-v1-result (status from output)
        │
        └── failed → read error from logs
            → publish agent-prompt-v1-result (status: failed, message: error)
```

## Job Output Contract

Jobs write their result to stdout as structured output (JSON). See [agent-job-interface.md](agent-job-interface.md) for the full contract.

```json
{
  "status": "done",
  "output": "PF: 1.4, Trades: 210",
  "message": "Backtest completed successfully",
  "links": ["https://..."]
}
```

The job creator reads this from the Job's logs and wraps it in an `agent-prompt-v1-result` message.

## Why This Component Exists

Decoupling the controller from K8s means:
- Controller is pure Kafka — testable, simple
- Execution runtime is swappable:

| Today | Tomorrow |
|-------|----------|
| K8s Jobs | Docker containers |
| | Lambda functions |
| | Permanent deployments |
| | Local process |

Swap the job creator, everything else stays the same.
