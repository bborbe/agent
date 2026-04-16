---
status: approved
tags:
    - dark-factory
    - spec
approved: "2026-04-16T16:32:02Z"
generating: "2026-04-16T18:11:08Z"
branch: dark-factory/agent-config-crd
---

## Summary

- Agent types become Kubernetes custom resources (`AgentConfig`) instead of a hardcoded slice in executor code
- Adding a new agent is a single `kubectl apply`; no Go change, no rebuild, no redeploy
- The executor watches AgentConfig CRs and keeps an in-memory lookup keyed by assignee
- Implementation follows the established bborbe CRD controller pattern — see `coding/docs/go-kubernetes-crd-controller-guide.md`

## Problem

Every new agent type requires editing `task/executor/main.go`, rebuilding the executor image, and redeploying to every cluster. The hardcoded `agentConfigs` slice couples agent registration to executor releases. Per-agent secrets, volumes, and image references live inside application code rather than in declarative K8s manifests where cluster admins expect them. As the agent catalog grows (hypothesis-agent, youtube-processor, researcher, …) this friction multiplies.

## Goal

After this work the executor discovers agent types dynamically by watching `AgentConfig` custom resources in its namespace. Adding, removing, or updating an agent is a single `kubectl apply` (or `kubectl delete`) operation — the executor picks up the change without restart. The executor binary contains no knowledge of specific agents. Each AgentConfig CR declares image, per-agent env, secret reference, PVC reference, and mount path; the executor merges those with shared env vars (`TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`) and spawns the Job.

## Non-goals

- Changing the Kafka event schema or `lib.Task` structure
- Building a reconciling controller that updates AgentConfig status — the executor is a read-only consumer
- Wiring `spec.resources` into the spawned Job (current `AgentConfiguration` has no Resources field; follow-up)
- Wiring `spec.heartbeat` (controller-side concern, not executor)
- CRD validation webhooks or admission policies
- Auto-scaling, timeouts, or retry configuration (listed under "Future Extensions" in the CRD schema doc)

## Desired Behavior

1. `AgentConfig` (`apiVersion: agents.bborbe.dev/v1`, namespace-scoped) is registered with an OpenAPI v3 schema covering every field in `docs/agent-crd-specification.md`: `assignee`, `image`, `heartbeat`, `resources`, `env`, `secretName`, `volumeClaim`, `volumeMountPath`
2. The executor self-installs the CRD on startup (create if missing, update schema if present). This is an intentional trust concession — see Assumptions for the RBAC implication.
3. When a task event arrives, the executor resolves the agent configuration by `spec.assignee` from its in-memory state
4. Unknown assignees produce a warning log, increment `skipped_unknown_assignee`, and the message is skipped — never errored
5. Image tagging uses `<image>:<branch>` — the resolved agent configuration carries the tagged image
6. Deleting an AgentConfig CR causes subsequent tasks for that assignee to skip with a warning
7. Updating an AgentConfig CR (image, env, secret) is reflected on the next matching task, no restart

## Constraints

**Business / contract**:
- The existing `AgentConfiguration` struct keeps its current shape (image, env, secretName, volumeClaim, volumeMountPath) — it is the conversion target from `v1.AgentConfig`
- The `agent-task-v1-event` Kafka topic and `lib.Task` schema are unchanged
- Task controller behavior is unchanged — it publishes events, unaware of how the executor resolves agents
- Job lifecycle, Job naming, and shared env-var injection are unchanged
- Authoritative CRD schema is `docs/agent-crd-specification.md`

**Implementation**:
- Follow `coding/docs/go-kubernetes-crd-controller-guide.md` — that guide specifies the package layout, interfaces (`K8sConnector`, `EventHandler<Resource>`, store), factory wiring, testing, and all deliberate exclusions (no `Lister`, no `WaitForCacheSync`, no separate CRD YAML manifest)
- CHANGELOG.md entry in agent repo root, format matching existing `v0.X.0 — feat: …`

## Assumptions

- AgentConfig is namespace-scoped (matches executor scope, simplifies RBAC)
- Agent count stays below ~100 per cluster — in-memory state is trivially sufficient
- CR changes are infrequent (daily at most) — eventual consistency is acceptable
- The `agents.bborbe.dev` API group is not already claimed in the cluster
- The executor's ServiceAccount can be granted cluster-scoped RBAC for `customresourcedefinitions` (required because self-install touches a cluster-scoped resource)

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| No matching AgentConfig for assignee | Warn, increment `skipped_unknown_assignee`, skip message | Apply missing CR |
| CRD missing at startup | Executor creates it | Automatic |
| CRD exists with older schema | Executor updates it | Automatic |
| API server unavailable at startup | Executor exits non-zero | K8s recovery + pod restart |
| Informer not yet synced when first task arrives | Lookup misses, task skipped with warning | Next task after sync succeeds |
| AgentConfig CR deleted while executor running | Next matching task skips with warning | Re-apply CR |
| AgentConfig CR updated | Next matching task uses new values | No action needed |
| `volumeClaim` set but `volumeMountPath` missing | Spawner returns error (existing behavior) | Fix the CR |
| Secret referenced by `spec.secretName` does not exist | Job pod fails with `CreateContainerConfigError` (existing behavior) | Create the Secret |
| RBAC missing | Executor exits non-zero on Listen | Apply RBAC manifest |

## Security / Abuse Cases

- AgentConfig CRs are applied by cluster admins only — RBAC on `agentconfigs.agents.bborbe.dev` restricts who can create/update them
- The executor's service account has only read verbs (`get/list/watch`) on AgentConfig — a compromised executor cannot mutate its own agent catalog
- Self-install requires write verbs on `customresourcedefinitions` (cluster-scoped) — a real trust concession; accepted, matches `sm-octopus/k8s-controller` and `alert-controller` norm
- Per-agent secrets stay in the agent's own K8s Secret (referenced by `spec.secretName`); blast radius is contained to the Job pod
- Image references are not validated beyond K8s image-pull — registry ACLs and ImagePullSecrets remain the defense
- The informer only watches the executor's own namespace — cross-namespace PVC mounts are impossible by construction
- A malicious AgentConfig CR could point to a public registry image — mitigated by cluster-level image policy (out of scope) and by RBAC restricting who can apply CRs

## Acceptance Criteria

- [ ] `cd task/executor && make precommit` passes
- [ ] Executor binary has no compiled-in agent catalog — the agent list is discovered at runtime
- [ ] CRD self-installs on executor startup — `kubectlquant -n dev get crd agentconfigs.agents.bborbe.dev` returns the CRD without any manual apply
- [ ] Three AgentConfig CRs applied in dev matching today's hardcoded configuration (including secretName, volumeClaim, volumeMountPath on trade-analysis)
- [ ] Executor logs show each CR arriving via the informer after startup
- [ ] Task event for `backtest-agent` in dev spawns a Job with image `…/agent-backtest:dev`
- [ ] Unknown assignee task event → warning logged, `skipped_unknown_assignee` incremented, task skipped (not errored)
- [ ] `kubectlquant -n dev delete agentconfig agent-trade-analysis` → next task for `trade-analysis-agent` skips with warning
- [ ] `kubectlquant -n dev apply -f <new-agent>.yaml` → executor picks it up within seconds (no restart) and next matching task spawns a Job
- [ ] Executor ServiceAccount has the minimum RBAC required to self-install the CRD and watch AgentConfig resources
- [ ] CHANGELOG.md entry added

## Verification

```
cd ~/Documents/workspaces/agent
cd task/executor && make precommit
```

Manual dev-cluster verification:

1. Deploy executor; confirm CRD self-installed:
   ```
   kubectlquant -n dev get crd agentconfigs.agents.bborbe.dev
   ```
2. Apply AgentConfig CRs for each agent:
   ```
   kubectlquant -n dev apply -f task/executor/k8s/agent-claude.yaml
   kubectlquant -n dev apply -f task/executor/k8s/agent-backtest-agent.yaml
   kubectlquant -n dev apply -f task/executor/k8s/agent-trade-analysis.yaml
   kubectlquant -n dev get agentconfigs
   ```
3. Publish a task event for `backtest-agent` and confirm a Job is spawned with image `.../agent-backtest:dev`.
4. Delete `agent-trade-analysis` and publish a task for `trade-analysis-agent`; confirm warning log and `skipped_unknown_assignee` metric increment.

## Do-Nothing Option

Keep the hardcoded `agentConfigs` slice. Every new agent triggers an executor code change, rebuild, PR review, release, and redeploy. Tolerable today with three agents and a single contributor, but the CRD approach is a one-time cost that removes the per-agent deployment tax permanently.
