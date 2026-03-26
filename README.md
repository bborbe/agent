# agent

Event-driven agent orchestration system. Generic controller + pluggable jobs via Kubernetes.

## Components

| Component | Description |
|-----------|-------------|
| `lib/` | Shared types: agent-task-v1, agent-prompt-v1 schemas, AgentConfig CRD |
| `vault-service/` | Task CQRS over Kafka, vault-cli internally |
| `controller/` | Consumes task events, produces prompts, heartbeat logic |
| `job-creator/` | Bridges Kafka prompts to K8s Jobs |

## Architecture

See [Agent Task Controller Architecture](https://github.com/bborbe/obsidian-personal/blob/master/50%20Knowledge%20Base/Agent%20Task%20Controller%20Architecture.md) for full design.

## License

BSD-2-Clause
