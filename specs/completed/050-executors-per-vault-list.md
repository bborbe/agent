---
status: completed
approved: "2026-08-27T10:52:53Z"
generating: "2026-08-27T10:53:12Z"
prompted: "2026-08-27T11:18:22Z"
verifying: "2026-08-27T11:43:26Z"
completed: "2026-08-27T21:15:23Z"
branch: dark-factory/executors-per-vault-list
---

## Summary

- The chart's single executor Deployment becomes a values-driven `executors` list (mirroring the existing `controllers` list): each enabled entry renders one per-vault executor Deployment `agent-task-executor-<name>`.
- Each per-vault executor serves exactly one vault — it consumes `{stage}-{vault}-agent-task-v1-event`, publishes results to `{stage}-{vault}-agent-task-v1-request`, injects `TOPIC_PREFIX` into spawned Jobs, and keeps its own Kafka offsets and dedup state (one replica per Deployment = one isolation domain per vault).
- The chart passes the new mandatory `VAULT_NAME` env (executor v0.7.0 requires it — Config CRs resolve by the composed `{assignee}-{vaultName}` assignee) from each entry's `vaultName`.
- Backward compatible: when `executors` is empty, the chart renders today's single `agent-task-executor` Deployment unchanged (`executor.enabled` fallback), so generic-cluster installs do not break.
- Chart version bumps 0.5.2 → 0.6.0 and the README documents the list. Consumer values (nuke/agent) migrate in a separate repo. Part of the per-vault topic-isolation design (migration step 2).

## Problem

Today the chart deploys exactly one executor Deployment that consumes one shared Kafka topic stream for every vault in the cluster. Every vault's task events funnel through a single instance: one consumer group, one offset domain, one in-memory dedup table. A job storm or restart in one vault resets shared state for all, and per-vault isolation (topic ownership, offsets, dedup, backpressure) is structurally impossible. Executor v0.7.0 added the machinery to deploy one executor per vault — a mandatory `VAULT_NAME` (Config CRs are resolved by the composed `{assignee}-{vaultName}` assignee) and `TOPIC_PREFIX` propagation into spawned Jobs (`job_spawner.go:495`) — but the chart has no way to express more than one executor: the Deployment template is hard-coded to a single `agent-task-executor` instance gated on one boolean. Per-vault isolation (vault KB: Agent Task Per-Vault Topic Isolation, migration step 2) cannot proceed until the chart can render N executors from values alone.

## Goal

From values alone, the chart renders one independent executor Deployment per vault. Each per-vault executor is a single-replica Deployment scoped to one vault: it consumes only that vault's `-event` topic and publishes only to that vault's `-request` topic (via its per-vault `TOPIC_PREFIX`), injects that same prefix into the Jobs it spawns, and carries its own Kafka offsets and dedup state. The `executors` list is the single source of truth for how many executors run and which vault each serves; adding a vault is a values change, not a chart code change. A generic install that does not use the list (`executors` empty) renders exactly today's single executor, unchanged.

## Non-goals

- Do NOT migrate the consumer values (nuke/agent `values-dev`/`values-prod` and Makefile version pins) — separate repo and spec. This spec only changes the chart.
- Do NOT split the shared single-instance executor resources into per-vault resources: the executor Service, RBAC (ServiceAccount/ClusterRole/Role), Secret, and KafkaUser templates stay single instances, gated on `executor.enabled`, untouched by this spec. Per-vault Services/RBAC/Secrets are a later migration step.
- Do NOT add per-entry `kafkaUser` (or storage, gitRestUrl, taskDir, pollInterval, autoInjectTaskIdentifier, gatewaySecret) to the executor entry schema — those are controller-only or not yet needed; executor mTLS stays driven by the single shared `executor.kafkaUser` block. If a future consumer needs per-vault Kafka credentials, that is a separate spec.
- Do NOT add chart-side regex validation of `vaultName` — slug validation is the executor binary's runtime job (v0.7.0). The chart only rejects an empty value at render time.
- Do NOT synthesize `{stage}-{vault}` as a topic prefix in the chart — `TOPIC_PREFIX` passes through the entry value verbatim; the per-vault prefix is a consumer-values decision.
- No Go code changes; no scenario (chart render is fully reachable by `helm template` + `helm lint`).

## Acceptance Criteria

- [ ] With `executors` set to N enabled entries (e.g. one with `name: openclaw`, one with `name: personal`), `helm template helm/ -f <values>` renders exactly N Deployments, one per entry, each `metadata.name` = `agent-task-executor-<name>` — evidence: `helm template helm/ -f <values> | grep -E 'name: agent-task-executor-(openclaw|personal)$'` returns one line per entry and `helm template helm/ -f <values> | grep -cE 'name: agent-task-executor-(openclaw|personal)$'` returns `2`.
- [ ] Each per-vault Deployment's container env sets `VAULT_NAME` to that entry's `vaultName` field (not its `name` field) and `TOPIC_PREFIX` to that entry's `topicPrefix`; two entries with distinct values render distinct env — evidence: with a values file where `name` and `vaultName` differ (e.g. entry `name: oc, vaultName: openclaw` renders Deployment `agent-task-executor-oc`), the rendered block shows `- name: VAULT_NAME` followed by `value: openclaw` and `- name: TOPIC_PREFIX` followed by that entry's prefix (`grep -A1 'name: VAULT_NAME'` and `grep -A1 'name: TOPIC_PREFIX'` on the rendered output); a second entry with a different `vaultName`/`topicPrefix` renders different values in its own Deployment block.
- [ ] With default values (`executors: []`, `executor.enabled: true`), `helm template helm/` (with the required values set — the chart requires `namespace`, `executor.kafkaBrokers`, `executor.existingSecret`; a bare `helm template helm/` exits 1) renders exactly one Deployment named `agent-task-executor` (the legacy single executor) with the same env contract as today, and NO Deployment container env contains `VAULT_NAME` — evidence: `RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'; eval "$RENDER" | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'` returns `1`; `eval "$RENDER" | grep -c 'name: VAULT_NAME'` returns `0`.
- [ ] With `executors` non-empty, `helm template helm/ -f <values>` renders NO Deployment named exactly `agent-task-executor` (list mode is authoritative; the legacy single executor is not rendered alongside) — evidence: `helm template helm/ -f <values> | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'` returns `0`.
- [ ] `helm/Chart.yaml` `version` is `0.6.0` and `appVersion` is unchanged (`0.3.1`) — evidence: `grep -n '^version:' helm/Chart.yaml` returns `version: 0.6.0`; `grep -n '^appVersion:' helm/Chart.yaml` returns `appVersion: "0.3.1"`.
- [ ] `helm/README.md` documents the `executors` list: a values-reference entry describing the list, the mandatory per-entry `vaultName` (→ `VAULT_NAME`, slug-constrained `^[a-z][a-z0-9-]*$`), the per-entry `topicPrefix` → per-vault `{prefix}-agent-task-v1-event`/`-request` topics, and the empty-list legacy fallback — evidence: `grep -n 'agent-task-v1-event' helm/README.md` returns ≥1 line and `grep -n 'vaultName' helm/README.md` returns ≥1 line within the executors section.
- [ ] `helm lint helm/` passes with default values and with an `executors`-set values file — evidence: `helm lint helm/` exits 0 in both cases (`0 chart(s) failed`).
- [ ] `CHANGELOG.md` gains an `## Unreleased` entry describing the per-vault `executors` list and the chart minor bump — evidence: `grep -n 'executors' CHANGELOG.md` returns ≥1 line in the top (Unreleased) section. Required so the `autoRelease: true` maintainer bot can cut the release from the CHANGELOG.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

```
cd /workspace
# The container image (bborbe/claude-yolo:v0.15.0) does NOT ship helm, and
# get.helm.sh is 403 from the build network — install helm from the Go proxy
# (pattern from prompts/1-spec-048-egress-proxy-resources.md, verified working).
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi
helm version --short   # must print v3.16

# A bare `helm template helm/` exits 1 (namespace/kafkaBrokers/existingSecret are
# required by the chart) — always render with these values set.
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'

make precommit                       # repo gate — must still pass (no Go changes expected)
helm lint helm/                      # exit 0

# Default render = one legacy Deployment, no VAULT_NAME:
eval "$RENDER" | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'   # 1
eval "$RENDER" | grep -c 'name: VAULT_NAME'                                               # 0

# List mode = N per-vault Deployments with per-entry env.
# Fixture /tmp/executors-values.yaml (two entries; name != vaultName on one to prove VAULT_NAME reads vaultName):
#   namespace: test
#   executors:
#     - name: oc
#       vaultName: openclaw
#       topicPrefix: develop-openclaw
#       kafkaBrokers: kafka:9092
#     - name: personal
#       vaultName: personal
#       topicPrefix: develop-personal
#       kafkaBrokers: kafka:9092
helm template helm/ -f /tmp/executors-values.yaml | grep -A2 'kind: Deployment' | grep -E 'name: agent-task-executor-'
# per-entry VAULT_NAME / TOPIC_PREFIX (use a values file where name != vaultName on one entry):
helm template helm/ -f /tmp/executors-values.yaml | grep -A1 'name: VAULT_NAME'
helm template helm/ -f /tmp/executors-values.yaml | grep -A1 'name: TOPIC_PREFIX'

# Legacy absent in list mode:
helm template helm/ -f /tmp/executors-values.yaml | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'   # 0
```

### Operator-executable (runs on the host after PR merge — publishing the chart is the release step)

```
helm package helm/ -d /tmp/agent-chart
ls /tmp/agent-chart/agent-0.6.0.tgz
helm push /tmp/agent-chart/agent-0.6.0.tgz oci://registry-1.docker.io/bborbe/agent   # exits 0 on success
```

## Desired Behavior

1. The chart gains an `executors` list value (default `[]`). Each entry declares `name` (required — Deployment suffix `agent-task-executor-<name>`), `enabled` (default `true`), `vaultName` (required — becomes `VAULT_NAME`), `branch`, `topicPrefix`, `kafkaBrokers` (required at render when not inherited), and optional overrides for `image`, `logLevel`, `sentry`, `existingSecret`, `podSecurityContext`, `securityContext`, `resources`. Every optional field absent from an entry falls back to the existing `executor.*` block, so an entry is minimal: `name`, `vaultName`, `topicPrefix`, `kafkaBrokers`.
2. Each enabled entry renders one Deployment named `agent-task-executor-<name>` in the chart namespace, `replicas: 1`, using the shared `agent-task-executor` ServiceAccount. The pod carries the `app: agent-task-executor` label (so the existing executor Service keeps selecting executor pods — that Service file is unchanged) plus a per-vault label `vault: <name>` (key `vault`, value the entry name); the Deployment selector matches both `app: agent-task-executor` and `vault: <name>`, so no two entries claim the same pods.
3. Each per-vault container receives the same contract as today's executor (LISTEN, SENTRY_DSN from the entry's `existingSecret` defaulting to the shared `agent-task-executor` Secret, BRANCH, KAFKA_BROKERS, NAMESPACE, probes, securityContext, resources, shared `executor.kafkaUser` cert mounts) plus the two new envs: `VAULT_NAME` = the entry's `vaultName` (render fails via `required` if empty) and `TOPIC_PREFIX` = the entry's `topicPrefix` (empty passes through as today). With a per-vault `topicPrefix` of `{stage}-{vault}`, the executor consumes `{stage}-{vault}-agent-task-v1-event` and publishes `{stage}-{vault}-agent-task-v1-request` (topic naming per `docs/kafka-schema-design.md`); the executor already injects that `TOPIC_PREFIX` into spawned Jobs (executor code, shipped).
4. Legacy fallback and authoritative list mode: when `executors` is empty AND `executor.enabled` is `true`, the chart renders exactly today's single `agent-task-executor` Deployment (unchanged env contract, no `VAULT_NAME`). When `executors` is non-empty, list mode is authoritative — the legacy single Deployment is not rendered regardless of `executor.enabled`; an all-disabled list renders no executor Deployment at all. The `executor.enabled` flag remains the legacy-path gate only.
5. `helm/Chart.yaml` `version` becomes `0.6.0`; `appVersion` stays `0.3.1`.
6. `helm/values.yaml` gains a documented `executors` example block (mirroring the `controllers` example shape) and `helm/README.md` documents the list, the `vaultName` requirement, the per-vault topic shape, and the empty-list fallback.

## Constraints

- The legacy `executor.*` values block remains and keeps its exact field names; it becomes the defaults source for per-entry optional fields. Nothing in the `executor.*` block is removed or renamed.
- The shared single-instance executor templates are untouched and keep gating on `executor.enabled` exactly as today: `executor-service.yaml` (selector `app: agent-task-executor`), `executor-rbac.yaml` (ServiceAccount/ClusterRole/Role), `executor-secret.yaml` (sentry-dsn), `executor-kafkauser.yaml`. Every per-vault executor pod must keep the `app: agent-task-executor` label so that Service selector keeps matching.
- `vaultName` is required at render time (`required` helper); the slug pattern `^[a-z][a-z0-9-]*$` is validated by the executor binary at startup (v0.7.0) and is NOT the chart's job.
- `topicPrefix` passes through verbatim per entry; the chart does not derive or default `{stage}-{vault}`. Per-vault isolation (the Goal) depends on the consumer setting a per-vault `topicPrefix`; the consumer-values migration is the separate repo.
- Executor mTLS remains single/shared: `executor.kafkaUser.enabled` drives the cert mounts and KafkaUser CR for all per-vault executors exactly as it does for the single executor today. No per-entry `kafkaUser`.
- This spec changes exactly five files: `helm/templates/executor-deployment.yaml`, `helm/values.yaml`, `helm/README.md`, `helm/Chart.yaml`, `CHANGELOG.md`.
- Chart version is `0.6.0` (additive minor — the change is backward-compatible); `appVersion` unchanged.
- `make precommit` must still pass; no Go code is touched.

## Failure Modes

All failures here are render-time or operator-time; a chart render writes no cluster state, so every row is fully reversible (fix values or revert the change, re-render).

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `executors[].vaultName` empty | `helm template` fails with a `required` error naming the field | Operator sets `vaultName` per entry, re-renders |
| `executors[].kafkaBrokers` empty (and not inherited) | `helm template` fails with a `required` error naming the field | Operator sets `kafkaBrokers` per entry, re-renders |
| Two entries share the same `name` | Chart renders two same-named Deployment manifests (no dedupe — same property as `controllers[]`); `kubectl apply` last-write-wins | Operator keeps entry names unique within the list |
| `executors[].topicPrefix` empty on a multi-vault install | Executors consume unprefixed topics — cross-vault events race and per-vault isolation silently fails (the Goal is not met) | Operator sets per-vault `topicPrefix` = `{stage}-{vault}` in consumer values, re-renders |
| `vaultName` violates the slug pattern | Chart renders fine; the executor pod exits at startup with the v0.7.0 validation error | Operator fixes `vaultName`, re-applies |
| Default-values render drifts from pre-change behavior (backward-compat regression) | Legacy single executor no longer renders, or renders with a new env | Operator diffs `helm template helm/` output against the pre-change chart and reverts |
| Consumer still pins the old chart version | The new `executors` capability is unreachable until the pin bumps (separate repo) | Operator bumps the version pin in the consumer values repo, re-renders |

## Security / Abuse Cases

- The chart renders operator-supplied values into container env verbatim — the same passthrough pattern the `controllers` list already uses; no new input parsing or trust boundary is introduced.
- The only new mandatory input, `vaultName`, is non-secret and validated at runtime by the executor's slug regex; empty values are rejected at render time by `required`.
- `SENTRY_DSN` remains a `secretKeyRef` to the shared Secret — never inlined into the manifest; the new per-entry fields carry no secret material.
- Values flow from the operator's values file to the Deployment manifest only; nothing is logged or echoed by the chart.

## Suggested Decomposition

Multi-layer chart change (values schema + template + version/docs). Generate prompts in this order:

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | `helm/values.yaml`: add the `executors` list + per-entry schema with `executor.*` default inheritance + documented example block | 1 | — (schema only) | — |
| 2 | `helm/templates/executor-deployment.yaml`: range over `executors`, per-entry env (`VAULT_NAME`, `TOPIC_PREFIX`, …), legacy fallback + authoritative list mode | 2, 3, 4 | 1, 2, 3, 4, 7 | prompt 1 |
| 3 | `helm/Chart.yaml` version → 0.6.0 + `helm/README.md` executors documentation + `CHANGELOG.md` Unreleased entry | 5, 6 | 5, 6, 8 | prompts 1, 2 (documents final field names) |

Rationale: prompt 1 establishes the values contract the template reads; prompt 2 is the core rendering behavior and the backward-compat fallback; prompt 3 records version + docs last so the README matches the shipped field names. No cycles — each prompt depends only on its predecessors.

## Do-Nothing Option

If not done, the chart continues to deploy one shared executor for all vaults, so the per-vault topic-isolation design stays stuck at step 1: every vault shares one consumer group, one offset domain, and one dedup table, and a storm or restart in any vault disrupts all vaults. Executor v0.7.0's `VAULT_NAME`/`TOPIC_PREFIX` machinery (already shipped) sits unusable because the chart cannot express more than one executor. The current chart remains functional for single-vault clusters, so this is a blocking-gap rather than a regression — acceptable only if per-vault isolation is abandoned.
