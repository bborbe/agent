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
```

## Who Uses the CRD

| Component | Uses | For |
|-----------|------|-----|
| Controller | `spec.assignee`, `spec.heartbeat` | Match tasks, enforce heartbeat |
| Job Creator | `spec.image`, `spec.resources` | Spawn K8s Job with correct image/limits |

## Fields

| Field | Required | Description |
|-------|----------|-------------|
| `spec.assignee` | yes | Matches the `assignee` field in task frontmatter |
| `spec.image` | yes | Docker image for the K8s Job |
| `spec.heartbeat` | yes | Interval between re-spawns for `in_progress` tasks |
| `spec.resources` | no | CPU/memory/storage requests for the job pod |

## Properties

**Declarative** — apply a CRD, system picks it up. Remove it, system stops watching.

**Cheap** — 100 AgentConfig CRDs cost zero resources until a matching task exists.

**Independent** — adding an agent never requires controller or job creator changes.

## Examples

```yaml
apiVersion: agents.bborbe.dev/v1
kind: AgentConfig
metadata:
  name: trade-analyser
spec:
  assignee: trade-analyser
  image: trade-analyser:latest
  heartbeat: 5m
  resources:
    cpu: 1
    memory: 1Gi
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
| `spec.env` | Environment variables for job pods |
| `spec.serviceAccount` | K8s service account for job pods |
