---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-04-21T22:18:35Z"
generating: "2026-04-21T22:19:09Z"
prompted: "2026-04-21T22:23:03Z"
verifying: "2026-04-23T21:07:19Z"
branch: dark-factory/agent-concurrency-via-priority-class
---

## Summary

- Add a per-agent concurrency cap by wiring the `Config` CRD to a Kubernetes `PriorityClass` + `ResourceQuota` pair (pattern precedent exists in another repo; template inlined below in §Reference Manifests for self-contained execution)
- The CRD gains an optional `spec.priorityClassName` field; the executor stamps that value onto every spawned Job's PodTemplate
- Concurrency is enforced by K8s natively: `ResourceQuota` scoped to the PriorityClass caps pod count; excess Jobs create but retry pod scheduling until quota frees
- This repo ships the CRD change, the executor change, and the co-located YAML bundle for `agent-claude` (the one prod agent living in this repo)
- Bundles for the other two prod agents (`agent-backtest-agent`, `agent-trade-analysis`) live in the trading repo and are applied by the operator out-of-band — out of dark-factory scope
- No application-level gate, no Kafka defer logic, no new counters — K8s owns the limiting primitive

## Problem

On 2026-04-21 a backfill pushed 18 `trade-analysis` tasks into the queue. The executor spawned all 18 Jobs simultaneously — 8 sat `Pending` on resource contention while 10 ran concurrently against `vault-obsidian-trading` and the trading HTTP APIs, risking API throttling, vault lock contention, and noisy-neighbour symptoms. The executor today has no per-agent concurrency cap; it spawns on every event it receives. The CRD doc lists `maxConcurrentJobs` under "Future Extensions" but no implementation exists. Until a cap is enforced, any bulk enqueue can take down dependent systems or waste cluster capacity on `Pending` pods.

A sibling controller in another repo already solved an analogous problem with a K8s-native pair: a `PriorityClass` tag + a `ResourceQuota` scoped to that class (manifests inlined in §Reference Manifests). Rather than re-invent an application-level gate with Kafka defer semantics, the agent executor should adopt the same pattern.

After this work, every agent `Config` may reference a Kubernetes `PriorityClass` whose namespace-local `ResourceQuota` caps how many pods of that class run at once. When the cap is reached, new Jobs are still created but their pods block on quota until a sibling pod finishes — K8s `Job` controller retries pod creation natively. No executor logic, no event deferral, no retry-count interaction. Adding or tuning the cap is a `kubectl apply` on the `ResourceQuota` — no code change, no restart. The `agent-claude` production agent (owned by this repo) runs at most one Job concurrently until an operator widens the quota. Bundles for `agent-backtest-agent` and `agent-trade-analysis` follow the same inlined template and are applied by the operator out-of-band (they live in a separate repo — out of scope for this spec's execution).

## Non-goals

- Any application-level concurrency gate, queue, or Kafka defer logic (K8s owns the primitive)
- Dynamic reconciliation of PriorityClass/ResourceQuota from CRD fields (operator applies YAML directly; no controller logic)
- New Prometheus counters for queue depth or gate decisions (K8s emits `FailedCreate` events; observability sits on existing event/metric surfaces)
- Dashboards, alerting rules, or SLOs on the new manifests
- Priority-based preemption or fairness between assignees (`preemptionPolicy: Never` everywhere — agents never evict other workloads)
- Changing the `agent-task-v1-event` Kafka schema or `lib.Task` struct
- Changing existing idempotency semantics (`current_job` label guard)

## Desired Behavior

1. The `Config` CRD schema gains an optional `spec.priorityClassName` string. OpenAPIV3Schema allows any DNS-label-compliant name; absent means "no priority class" (Jobs spawn with no `priorityClassName`, unbounded as today).
2. The Go type backing the CRD carries the same field. `make generatek8s` + `make ensure` produce updated clientset/deepcopy. Executor self-installs the schema on startup (spec 007 pattern).
3. `docs/agent-crd-specification.md` documents the field in the authoritative schema section. The "Future Extensions" hint `maxConcurrentJobs` is removed — replaced with a note that quota is enforced via `priorityClassName` + K8s `ResourceQuota`.
4. When the executor spawns a Job for an assignee whose Config has `spec.priorityClassName` set, it copies that value onto `Job.spec.template.spec.priorityClassName`. Unset → field omitted from the PodTemplate.
5. The `agent-claude` bundle is added to this repo at `agent/claude/k8s/`, consisting of four files (shapes inlined in §Reference Manifests):
   - `agentconfig.yaml` — the `Config` CR, with `spec.priorityClassName: agent-claude`
   - `priorityclass.yaml` — cluster-scoped `PriorityClass` named `agent-claude`, mid-tier value (500), `preemptionPolicy: Never`
   - `resource-quota-dev.yaml` — namespace-scoped `ResourceQuota` in `dev` namespace, `pods: "1"`, scoped to PriorityClass `agent-claude`
   - `resource-quota-prod.yaml` — same shape as dev, `namespace: prod` (per-env quota — caps can diverge without touching the PriorityClass)
6. Bundles for `agent-backtest-agent` and `agent-trade-analysis` are NOT created by this spec — they live in a separate repo and are applied by the operator out-of-band using the same inlined template. This spec is complete when the agent-claude path works end-to-end; operator follow-up replicates the pattern for the other two.
7. Every PriorityClass uses `preemptionPolicy: Never`. Agents must not evict other workloads when quota-blocked — they wait, they do not preempt.
8. PriorityClass names are cluster-scoped. The `agent-` prefix reserves the namespace from collisions with non-agent priority classes (including the existing `backtest` class in the trading cluster).
9. When quota is reached, the Job object still creates successfully — only pod creation fails with a `FailedCreate` event (ResourceQuota denies `pods: "2"`). The Job controller retries pod creation automatically; no application handling required.
10. Unit tests (Ginkgo + counterfeiter) verify: (a) executor stamps `priorityClassName` on spawned Jobs when Config has it set, (b) spawned Jobs omit `priorityClassName` when Config leaves it unset, (c) the field round-trips through the CRD schema (decode/encode).
11. Operator-run smoke verification (no automated E2E harness in-repo): after deploy, operator enqueues 5 tasks for `agent-claude` and observes that at most one pod is `Running` at a time, that `FailedCreate` events appear on the other 4 pods, and that all 5 reach a terminal state as the single slot cycles. The commands are listed in §Verification.

## Constraints

**Business / contract**:
- `lib.Task` schema and `agent-task-v1-event` topic are unchanged
- Existing idempotency behaviour (`current_job` label guard described in `docs/task-flow-and-failure-semantics.md`) is untouched — no new logic interacts with it
- `retry_count` semantics from spec 011 are preserved (no new application-level retry path exists)
- Task controller is unaware of the quota — it keeps publishing events as today
- `skipped_unknown_assignee` path (spec 007) is untouched

**Implementation**:
- Testing uses Ginkgo + counterfeiter, matching `job_spawner_test.go` style
- After changing CRD Go types: `make generatek8s` then `make ensure` — not part of precommit; run explicitly
- CHANGELOG.md entry in agent repo root (`v0.X.0 — feat: priorityClassName on Config CRD`) and `lib/CHANGELOG.md` if lib module changes
- Versioning: paired tags `vX.Y.Z` + `lib/vX.Y.Z` at the same commit
- `priorityClassName` value must be DNS-label-safe (enforced by K8s CRD schema validation, matches PriorityClass naming)
- OpenAPIV3Schema in `SetupCustomResourceDefinition` must stay in sync with the Go struct

## Assumptions

- The K8s-native pattern (PriorityClass + ResourceQuota scoped by `PriorityClass` scopeSelector) is preferred over an application-level gate. Rationale: matches the existing `backtest-resource-quota.yaml` precedent, removes a whole class of application bugs (defer loops, offset management, queue metrics), and keeps the executor simple. The trade-off accepted: K8s enforcement is per-namespace (ResourceQuota is namespace-scoped) — sufficient for dev/prod isolation today.
- `ResourceQuota.scopeSelector` supports only `Terminating`, `NotTerminating`, `BestEffort`, `NotBestEffort`, `PriorityClass`, `CrossNamespacePodAffinity`. PriorityClass is the only selector that distinguishes pods by an agent-scoped attribute; label-based selectors are not available for quotas. This forces every capped agent to have a dedicated PriorityClass.
- `preemptionPolicy: Never` is non-negotiable for agent PriorityClasses. Agents are background workloads; they must not displace user-facing or latency-sensitive pods when quota is reached.
- Jobs created while quota is full will backoff on pod creation per the Job controller's internal retry cadence. This is acceptable behaviour — `FailedCreate` events are visible in `kubectl describe job` and on the pod's owning controller events stream.
- Agent count stays below ~100 per cluster (inherited from spec 007). PriorityClass count stays similarly bounded; no administrative concern at this scale.
- Co-locating the 4-file YAML bundle under each agent's existing `k8s/` directory keeps the deployment surface discoverable (mirrors backtest's layout) and means the make-buca flow picks them up without new makefile logic.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `spec.priorityClassName` unset on a Config | Jobs spawn without priorityClassName; no quota applies; pre-spec behaviour | None needed |
| `spec.priorityClassName` references a non-existent PriorityClass | K8s rejects pod creation with "no such PriorityClass"; `FailedCreate` event on the Job | Operator applies the missing PriorityClass manifest |
| ResourceQuota hit; 2nd pod blocked | `FailedCreate` event; Job controller retries pod creation on its own schedule | Automatic when a sibling pod reaches a terminal phase |
| Executor restart while quota-blocked Jobs exist | Nothing to recover — Job controller owns retry; executor has no state in the loop | Automatic |
| Operator changes `ResourceQuota.hard.pods` live | K8s enforces the new limit immediately on the next admission decision | Automatic |
| PriorityClass deleted while Jobs still reference it | Existing pods keep running; new pod admissions fail with "no such PriorityClass" | Operator reapplies the PriorityClass |
| Duplicate event for same TaskIdentifier while a Job is quota-blocked | `current_job` idempotency guard prevents duplicate Job creation | Automatic |
| Non-agent workload applies `priorityClassName: agent-<x>` by mistake | Consumes quota slot; agent starves | Operator audit: `kubectl get pods -A -o jsonpath='{range .items[?(@.spec.priorityClassName=="agent-claude")]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}'` lists every pod claiming the class |

## Security / Abuse Cases

- The feature adds one optional string field on a cluster-scoped-applied CR. Trust boundary matches spec 007: whoever applies `Config` CRs already has full control of the executor's behaviour.
- PriorityClass is a cluster-scoped object; applying one requires cluster-admin-equivalent RBAC. Creation is operator-gated, not agent-gated.
- `preemptionPolicy: Never` ensures an agent quota or priority class cannot be weaponised to evict other workloads.
- A misapplied `ResourceQuota` with an unrealistically low `pods:` count degrades an agent to "never runs" — visible via `FailedCreate` events; not a data-integrity risk.
- No new HTTP surfaces, no new Kafka topics, no new RBAC on the executor (the existing service account already watches Configs and creates Jobs).

## Acceptance Criteria

- [ ] `Config` CRD schema includes `spec.priorityClassName` (optional string, DNS-label pattern); OpenAPIV3Schema updated in `SetupCustomResourceDefinition`; generated clientset + deepcopy committed
- [ ] `docs/agent-crd-specification.md` documents `priorityClassName` in the authoritative schema section; the `maxConcurrentJobs` "Future Extension" row is removed and replaced with the K8s-native pattern description
- [ ] Executor stamps `Job.spec.template.spec.priorityClassName` when `spec.priorityClassName` is set; omits it otherwise
- [ ] `agent-claude` YAML bundle exists at `agent/claude/k8s/` with four files: `agentconfig.yaml` (sets `spec.priorityClassName: agent-claude`), `priorityclass.yaml`, `resource-quota-dev.yaml`, `resource-quota-prod.yaml` — all inline-matching §Reference Manifests
- [ ] Bundles for `agent-backtest-agent` and `agent-trade-analysis` in their separate repo are explicitly out of scope — operator follow-up note added to `~/Documents/Obsidian/Personal/24 Tasks/Add per-agent parallel job limit to AgentConfig CRD.md`
- [ ] Every agent PriorityClass has `preemptionPolicy: Never` and an `agent-`-prefixed name
- [ ] Every agent ResourceQuota has `pods: "1"` and `scopeSelector` matching its PriorityClass
- [ ] Unit tests (Ginkgo + counterfeiter) cover: priorityClassName applied when set, omitted when unset, round-trip through CRD schema
- [ ] Operator smoke verification documented in §Verification passes: 5 tasks enqueued for `agent-claude` → at most one pod Running at any time, `FailedCreate` events on the 4 blocked pods, all 5 reach terminal state (no automated E2E harness)
- [ ] `make precommit` clean in every changed module; paired `vX.Y.Z` + `lib/vX.Y.Z` tags (if lib changes)

## Reference Manifests

These are the canonical shapes for the four-file bundle. Prompts must produce byte-compatible equivalents (formatting/comments may vary). The pattern is lifted from an existing sibling controller; inlined here so execution never requires reading outside this repo.

```yaml
# agent/claude/k8s/priorityclass.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: agent-claude
value: 500
globalDefault: false
preemptionPolicy: Never
description: "Agent claude — namespace-local concurrency via matching ResourceQuota. Never preempts."
```

```yaml
# agent/claude/k8s/resource-quota-dev.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: agent-claude
  namespace: dev
spec:
  hard:
    pods: "1"
  scopeSelector:
    matchExpressions:
      - scopeName: PriorityClass
        operator: In
        values: ["agent-claude"]
```

```yaml
# agent/claude/k8s/resource-quota-prod.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: agent-claude
  namespace: prod
spec:
  hard:
    pods: "1"
  scopeSelector:
    matchExpressions:
      - scopeName: PriorityClass
        operator: In
        values: ["agent-claude"]
```

```yaml
# agent/claude/k8s/agentconfig.yaml (excerpt — full Config CR has more fields)
apiVersion: agent.benjamin-borbe.de/v1
kind: Config
metadata:
  name: agent-claude
  namespace: dev
spec:
  assignee: claude-agent
  priorityClassName: agent-claude     # NEW — references the PriorityClass above
  # ...existing fields: image, heartbeat, resources, secretName, etc.
```

Notes:
- `value: 500` is mid-tier; safe (non-system, not zero), arbitrary
- `preemptionPolicy: Never` is mandatory — agents never evict
- `pods: "1"` is the initial conservative default; operators raise it per-agent when load is understood
- Dev and prod each have their own `resource-quota-<env>.yaml` — caps can diverge per environment without touching the PriorityClass

## Verification

```
# Agent repo (CRD types, executor, agent-claude manifest bundle)
cd task/executor && make precommit
cd ../.. && make generatek8s && git diff --exit-code        # no drift

# After deploy: inspect the CR, PriorityClass, and ResourceQuota
kubectlquant -n dev get configs.agent.benjamin-borbe.de agent-claude -o yaml | grep priorityClassName
kubectlquant get priorityclass agent-claude
kubectlquant -n dev get resourcequota agent-claude -o yaml

# Enqueue 5 tasks for one assignee, watch pods and events
watch -n1 'kubectlquant -n dev get pods -l agent.benjamin-borbe.de/assignee=claude-agent'
# expected: at most one pod Running at any time; others Pending or absent (Job exists, pod not yet created)

kubectlquant -n dev get events --field-selector reason=FailedCreate | grep agent-claude
# expected: FailedCreate events citing "exceeded quota" on Jobs waiting for a slot
```

## Do-Nothing Option

Without this change, any bulk enqueue (backfill, recovery-from-outage, fan-out from an upstream job) spawns an unbounded number of Jobs for a single agent, risking: (a) API throttling or lock contention on downstream services like `vault-obsidian-trading` and the trading HTTP APIs, (b) `Pending` pods starving on cluster resources, (c) noisy-neighbour impact on unrelated workloads. The alternative to the K8s-native pattern — building an application-level gate with Kafka defer semantics — carries meaningful complexity (offset management, queue-depth metrics, stuck-gate detection, hot-loop pacing) that a sibling controller already avoided by using PriorityClass + ResourceQuota. Accepting the status quo means every bulk operation carries an outage risk that scales with fleet size, and the next incident requires an ad-hoc manual quota workaround every time.
