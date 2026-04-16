# AgentConfig CRD Specification

AgentConfig is a Kubernetes Custom Resource Definition that declares an agent type. Both the controller and job creator read these to know which agents exist and how to handle them.

## CRD Definition

```yaml
apiVersion: agents.bborbe.dev/v1
kind: AgentConfig
metadata:
  name: backtest-agent
  namespace: dev
spec:
  assignee: backtest-agent        # matches task assignee field
  image: backtest-agent:latest    # container image for K8s Job
  heartbeat: 15m                  # re-spawn interval for in_progress tasks
  resources:
    cpu: 500m
    memory: 512Mi
    ephemeral-storage: 1Gi
  env:                            # per-agent env vars (merged with shared)
    LOG_LEVEL: info
  secretName: agent-backtest      # K8s Secret mounted via envFrom
```

## Who Uses the CRD

| Component | Uses | For |
|-----------|------|-----|
| Controller | `spec.assignee`, `spec.heartbeat` | Match tasks, enforce heartbeat |
| Job Creator | `spec.image`, `spec.resources`, `spec.env`, `spec.secretName`, `spec.volumeClaim`, `spec.volumeMountPath` | Spawn K8s Job with correct image/limits/env/secret/volume |

## Fields

| Field | Required | Description |
|-------|----------|-------------|
| `spec.assignee` | yes | Matches the `assignee` field in task frontmatter |
| `spec.image` | yes | Docker image for the K8s Job (tag appended at runtime from branch) |
| `spec.heartbeat` | yes | Interval between re-spawns for `in_progress` tasks |
| `spec.resources` | no | CPU/memory/storage requests for the job pod |
| `spec.env` | no | Per-agent environment variables, merged with shared vars (`TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`) |
| `spec.secretName` | no | Name of an existing K8s Secret mounted on the container via `envFrom` |
| `spec.volumeClaim` | no | Name of an existing PVC mounted into the container |
| `spec.volumeMountPath` | conditional | Container path for `volumeClaim` mount — required when `volumeClaim` is set |

## Properties

**Declarative** — apply a CRD, system picks it up. Remove it, system stops watching.

**Cheap** — 100 AgentConfig CRDs cost zero resources until a matching task exists.

**Independent** — adding an agent never requires controller or job creator changes.

## Examples

```yaml
apiVersion: agents.bborbe.dev/v1
kind: AgentConfig
metadata:
  name: trade-analysis-agent
spec:
  assignee: trade-analysis-agent
  image: docker.quant.benjamin-borbe.de:443/agent-trade-analysis
  heartbeat: 5m
  resources:
    cpu: 1
    memory: 1Gi
  secretName: agent-trade-analysis
  volumeClaim: agent-trade-analysis
  volumeMountPath: /home/claude/.claude
```

```yaml
apiVersion: agents.bborbe.dev/v1
kind: AgentConfig
metadata:
  name: youtube-processor
spec:
  assignee: youtube-processor
  image: youtube-processor:latest
  heartbeat: 1m
  resources:
    cpu: 2
    memory: 2Gi
```

## Future Extensions

| Field | Purpose |
|-------|---------|
| `spec.maxConcurrentJobs` | Limit parallel jobs per agent type |
| `spec.timeout` | Max runtime before job is killed |
| `spec.retries` | Auto-retry count before human_review |
| `spec.serviceAccount` | K8s service account for job pods |
