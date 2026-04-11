---
status: idea
---

## Summary

- Agent types are defined as Kubernetes custom resources instead of hardcoded in executor code
- Adding a new agent requires only applying a CRD manifest — no code change or redeployment
- Each agent definition carries its own container image, environment variables, resource limits, and heartbeat interval
- The executor watches AgentConfig resources and resolves assignee to configuration at runtime
- Per-agent secrets use standard K8s secretKeyRef — no shared API keys across agents
- Existing agents (backtest, trade-analysis, claude) are migrated to CRD manifests

## Problem

Every new agent type requires a code change in `task/executor/main.go` to add an entry to the hardcoded configuration list. The executor must be rebuilt and redeployed just to register a new agent. Per-agent secrets (Gemini key, Anthropic key) are wired through the executor's own deployment env, leaking secrets to a service that doesn't use them. As the number of agents grows (backtest, trade-analysis, hypothesis, youtube-processor), this pattern becomes a deployment bottleneck.

## Goal

After this work, the executor discovers agent types dynamically by watching `AgentConfig` custom resources in its namespace. Adding a new agent means `kubectl apply -f agent-config-foo.yaml` — no executor code change, no rebuild, no redeployment. Each AgentConfig declares its image, per-agent env vars (including secretKeyRef), resource limits, and heartbeat interval. The executor resolves assignee → AgentConfig at runtime via a shared informer.

## Non-goals

- Changing the Kafka event schema or task frontmatter format
- Building a controller/operator that reconciles AgentConfig (executor is the sole consumer)
- Auto-scaling agents based on queue depth
- Agent health monitoring or restart policies beyond K8s Job backoffLimit
- Migrating Pattern A (persistent service) agents to CRD — this covers Pattern B (ephemeral Jobs) only

## Desired Behavior

1. A CRD `AgentConfig` is registered at `agents.bborbe.dev/v1` with fields: assignee, image, env, resources, heartbeat
2. The executor starts a shared informer for AgentConfig in its namespace and maintains a local cache
3. When a task event arrives, the executor queries the cache by assignee name instead of a hardcoded map
4. Unknown assignees (no matching CRD) are skipped with a warning, same as today
5. Per-agent env vars from the CRD spec (including secretKeyRef) are merged with shared env vars (TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH) when spawning the Job
6. Resource requests/limits from the CRD spec are applied to the spawned Job container
7. The executor no longer receives agent-specific secrets (GEMINI_API_KEY, ANTHROPIC_API_KEY) in its own environment
8. Three AgentConfig manifests are created for existing agents: backtest-agent, trade-analysis-agent, claude

## Constraints

**Must not change:**
- The `agent-task-v1-event` Kafka topic and the `lib.Task` schema
- The task/controller behavior (it publishes events, unaware of how executor resolves agents)
- The Job lifecycle (ephemeral, no retry, result via Kafka)
- Shared env vars: TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH always injected
- Job naming convention: `{assignee}-{timestamp}`

**Frozen conventions:**
- Counterfeiter mocks, ginkgo/gomega tests
- `service.Run` for concurrent goroutine lifecycle
- K8s manifests in `task/executor/k8s/`
- `make precommit` as verification gate

**Prerequisite:**
- Prompt `executor-agent-configuration` (AgentConfiguration struct) should be completed first — the CRD spec fields mirror `AgentConfiguration`

## Assumptions

- The number of distinct agent types stays under 50 — an in-memory informer cache is sufficient
- CRD changes (add/remove agent) are infrequent (weekly, not per-minute) — eventual consistency via informer is acceptable
- The CRD API group `agents.bborbe.dev` does not conflict with any existing CRDs in the cluster
- `kubebuilder` or `controller-runtime` is acceptable for CRD scaffolding, OR a hand-written CRD YAML + typed client is sufficient

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| No AgentConfig CRD for assignee | Log warning, skip event, increment `skipped_unknown_assignee` metric | Apply the missing AgentConfig manifest |
| CRD API unavailable at startup | Executor fails to start (informer sync timeout) | Fix K8s API, executor restarts via Deployment |
| CRD deleted while executor running | Informer cache removes entry, next matching task is skipped | Re-apply CRD manifest |
| CRD updated (image change) | Informer cache updates, next spawned Job uses new image | No action needed — working as designed |
| Invalid CRD (missing required field) | K8s API rejects apply | Fix manifest and re-apply |
| secretKeyRef points to missing secret | Job pod fails to start (CreateContainerConfigError) | Create the K8s secret |

## Security / Abuse Cases

- CRD manifests are applied by cluster admins only (RBAC on `agents.bborbe.dev` resources)
- Per-agent secrets stay in the agent's own K8s Secret, not in the executor's environment
- The executor only reads AgentConfig (get/list/watch) — it cannot create/modify CRDs
- Image references in CRD are not validated beyond K8s image pull — malicious images are prevented by registry access control and ImagePullSecrets
- Resource limits in CRD prevent a single agent from consuming unbounded cluster resources

## Acceptance Criteria

- [ ] CRD manifest registered: `kubectl get crd agentconfigs.agents.bborbe.dev`
- [ ] Three AgentConfig resources applied (backtest-agent, trade-analysis-agent, claude)
- [ ] Executor resolves assignee via informer cache (unit test with fake client)
- [ ] Unknown assignee skipped (unit test)
- [ ] Per-agent env vars merged with shared env vars in spawned Job (unit test)
- [ ] Resource limits from CRD applied to spawned Job container (unit test)
- [ ] Executor starts without GEMINI_API_KEY or ANTHROPIC_API_KEY in its own env
- [ ] `make precommit` passes in task/executor/
- [ ] CRD update (image change) reflected in next spawned Job (integration test or manual verification)

## Verification

```
cd task/executor && make precommit
```

## Do-Nothing Option

Keep the hardcoded `AgentConfigurations` slice in `main.go` (from the prerequisite prompt). This works for 3-5 agents but requires a code change + rebuild + redeployment for every new agent. As the agent count grows (hypothesis, youtube-processor, researcher), this becomes a drag on velocity. The CRD approach makes agent registration a kubectl command — zero-downtime, zero-code-change.
