# agent — Helm chart

Event-driven, Kafka-based, Kubernetes-native task platform: a **controller** that turns
Obsidian/git task files into Kafka events, an **executor** that materialises agent
`Config` custom resources into Kubernetes Jobs, and any number of **leaf agents**
(claude / code / gemini / pi …) declared purely as values. Optionally a
**recurring-task-creator** that emits scheduled tasks.

This chart is layer 2 of a three-layer design:

| Layer | Where | What |
|---|---|---|
| Image | source repos (`agent-task-executor`, `agent-task-controller`, `agent-*`) | public semver images on `docker.io/bborbe/*` |
| Chart | **here** (`oci://registry-1.docker.io/bborbe/agent`) | *how* the components run |
| Values | your cluster config repo | *your* namespace, brokers, secrets, overrides |

One chart installs on any cluster; everything that differs between clusters is a value.

---

## Prerequisites

- **Kubernetes** ≥ 1.25 (the Config CRD uses `x-kubernetes-validations` CEL rules).
- **Kafka** reachable from the namespace. On quant/Octopus this is Strimzi; the chart
  only needs the bootstrap broker address (`executor.kafkaBrokers`,
  `controller.kafkaBrokers`). mTLS/`KafkaUser` is optional (see recurring-task-creator).
- **Image pull access** to the images — either the public `docker.io/bborbe/*`
  (no credentials) or an in-cluster mirror (set `image.registry` + `image.pullSecrets`).
- **Sentry DSN** (optional) — supply as a plain Kubernetes Secret (`existingSecret`) or
  let the chart create the Secret from a value you pass with `--set-string`
  (quant resolves this from TeamVault).
- **GitHub App / provider credentials for agents** — each leaf agent needs its provider
  token (e.g. Anthropic/MiniMax) delivered as Secret keys via the agent's `secretEnv`.
- **Helm** ≥ 3.8 (OCI registry support).

The `configs.agent.benjamin-borbe.de` CRD ships in the chart's `crds/` directory and is
installed automatically on first `helm install`. (The executor also reconciles the CRD
schema at runtime, so it stays current across upgrades — `crds/` only guarantees the CRD
exists before the leaf `Config` resources apply.)

---

## Install

```bash
helm install agent oci://registry-1.docker.io/bborbe/agent \
  --version 0.2.0 \
  --namespace <your-namespace> \
  --values values.yaml
```

Upgrade by bumping `--version` / image tags and re-running with `helm upgrade --install`.

A minimal `values.yaml` (executor only, public images, generic cluster):

```yaml
namespace: my-agents
image:
  registry: docker.io
executor:
  kafkaBrokers: "my-kafka-bootstrap:9092"
  sentry:
    existingSecret: agent-sentry   # a Secret you created with key `sentry-dsn`
controller:
  enabled: false
recurringTaskCreator:
  enabled: false
agents: []
```

---

## Values reference

### Global

| Key | Default | Description |
|---|---|---|
| `namespace` | `""` (**required**) | Namespace all components deploy into. Fails loudly if unset. |
| `image.registry` | `docker.io` | Registry prefix for every component image. Point at an in-cluster mirror to avoid Docker Hub. |
| `image.pullPolicy` | `IfNotPresent` | Container image pull policy. |
| `image.pullSecrets` | `[]` | Pull secret(s), e.g. `[{name: docker}]`. Empty for public `docker.io`. |
| `keel.enabled` | `false` | Emit keel auto-redeploy annotations (registry poll). Off = versioned deploys. |
| `keel.pollSchedule` | `@every 1m` | keel poll interval when enabled. |
| `affinity` | `{}` | Node affinity object applied to controller/executor/recurring pods. Empty = none. |
| `rolloutNonce` | `""` | Value of a `random` pod annotation to force a rollout. Empty = omit. |

### executor

| Key | Default | Description |
|---|---|---|
| `executor.enabled` | `true` | Deploy the executor. |
| `executor.image.repository` | `bborbe/agent-task-executor` | Image repo (under `image.registry`). |
| `executor.image.tag` | `""` | Image tag; defaults to chart `appVersion`. |
| `executor.kafkaBrokers` | `""` (**required**) | Kafka bootstrap brokers. |
| `executor.topicPrefix` | `""` | Kafka topic prefix. Empty = unprefixed (per-stage-cluster / Octopus). Quant: `develop`/`master`. |
| `executor.branch` | `""` | Stage label forwarded as `BRANCH`. |
| `executor.sentry.proxy` | `""` | Sentry proxy URL (optional). |
| `executor.sentry.dsn` | `""` | Sentry DSN; when set the chart creates the Secret. |
| `executor.existingSecret` | `""` | Name of a pre-existing Secret with key `sentry-dsn`; when set the chart creates no Secret. |
| `executor.podSecurityContext` / `securityContext` | `{}` / hardened | Pod/container security contexts. |
| `executor.resources` | 20m/20Mi → 500m/50Mi | Requests/limits. |

### controller

| Key | Default | Description |
|---|---|---|
| `controller.enabled` | `true` | Deploy the controller StatefulSet (1Gi BoltDB PVC). |
| `controller.image.repository` | `bborbe/agent-task-controller` | Image repo. |
| `controller.vaultName` | `""` (**required when enabled**) | Names the workload + derives the git-rest URL + `VAULT_NAME`. |
| `controller.gitRestUrl` | `""` | git-rest (Obsidian API) URL. Empty = `http://vault-obsidian-<vaultName>:9090`. |
| `controller.kafkaBrokers` | `""` (**required when enabled**) | Kafka bootstrap brokers. |
| `controller.topicPrefix` / `branch` | `""` | As executor. |
| `controller.taskDir` | `""` | Task directory inside the vault. |
| `controller.autoInjectTaskIdentifier` | `"false"` | Auto-inject a task identifier. |
| `controller.pollInterval` | `"60s"` | Vault poll interval. |
| `controller.storage.size` | `1Gi` | BoltDB PVC size. |
| `controller.storage.storageClassName` | `""` | Empty = cluster default StorageClass (portable). Set explicitly only for a specific class (quant uses `local-path`, which needs the Local Path Provisioner). |
| `controller.sentry.*` / `existingSecret` | `""` | Sentry DSN, same pattern as executor. |
| `controller.gatewaySecret` | `""` | Gateway secret value (chart-created Secret key `gateway-secret`). |
| `controller.resources` | 20m/20Mi → 500m/50Mi | Requests/limits. |

### agents (leaf agents — values-driven)

`agents` is a list; each **enabled** entry emits a `Config` CR + Secret + PVC +
PriorityClass (cluster-scoped) + ResourceQuota (concurrency cap). Nothing is
hard-coded — add/remove agents purely in values.

```yaml
agents:
  - name: agent-claude
    enabled: true
    assignee: claude-agent
    image: bborbe/agent-claude       # under image.registry
    tag: v0.1.1                       # defaults to appVersion when empty
    heartbeat: 5m
    taskTypes: [llm, healthcheck]
    triggerPhases: [planning, execution, ai_review]
    triggerStatuses: [in_progress]
    volumeMountPath: /home/claude/.claude
    storageSize: 1Gi
    env:
      ALLOWED_TOOLS: WebSearch,WebFetch,Read,Grep
      ANTHROPIC_BASE_URL: https://api.minimax.io/anthropic
      ANTHROPIC_MODEL: MiniMax-M2.7-highspeed
    secretEnv:                        # → Secret keys (base64-encoded by the chart)
      SENTRY_DSN: ""                  # supply via --set-string or a values overlay
      ANTHROPIC_AUTH_TOKEN: ""
    resources:
      requests: {cpu: 500m, memory: 1Gi, ephemeral-storage: 2Gi}
      limits:   {cpu: 500m, memory: 1Gi, ephemeral-storage: 2Gi}
    priorityValue: 500                # PriorityClass value (non-preempting)
    concurrency: 1                    # ResourceQuota pods cap for this agent
```

The four reference agents (`agent-claude`, `agent-code`, `agent-gemini`, `agent-pi`)
follow this exact shape; `agent-pi` uses `PROVIDER_API_KEY` instead of
`ANTHROPIC_AUTH_TOKEN` in `secretEnv` and `MODEL` instead of the `ANTHROPIC_*` env.

### recurringTaskCreator (optional)

| Key | Default | Description |
|---|---|---|
| `recurringTaskCreator.enabled` | `false` | Deploy the recurring-task-creator StatefulSet + RBAC. |
| `recurringTaskCreator.image.repository` | `bborbe/recurring-task-creator` | Image repo. |
| `recurringTaskCreator.kafkaBrokers` | `""` | Kafka bootstrap brokers. |
| `recurringTaskCreator.stage` / `topicPrefix` / `dryRun` | `""` / `""` / `"false"` | Runtime config. |
| `recurringTaskCreator.sentry.*` | `""` | Sentry DSN, same pattern. |
| `recurringTaskCreator.kafkaUser.enabled` | `false` | Emit a Strimzi `KafkaUser` (mTLS clusters only). |
| `recurringTaskCreator.kafkaUser.cluster` / `strimziNamespace` | `my-cluster` / `strimzi` | Strimzi cluster + namespace. |

---

## Generic cluster (not quant)

Quant is opinionated: an in-cluster mirror, keel polling, node affinity, TeamVault
secrets, Strimzi mTLS. **None of that is required.** A vanilla cluster:

- **Public images** — leave `image.registry: docker.io` and `image.pullSecrets: []`.
  No mirror needed.
- **No keel** — leave `keel.enabled: false`; deploy new versions by bumping image tags
  and re-running `helm upgrade`.
- **No node affinity** — leave `affinity: {}`.
- **Plain Kubernetes Secrets** — instead of TeamVault, create Secrets yourself and point
  `executor.existingSecret` / `controller.existingSecret` at them (key `sentry-dsn`), and
  put agent tokens into each agent's `secretEnv` (delivered via a private values overlay
  or `--set-string agents[N].secretEnv.ANTHROPIC_AUTH_TOKEN=...`).
- **No Strimzi mTLS** — leave `recurringTaskCreator.kafkaUser.enabled: false`; point
  `*.kafkaBrokers` at your plaintext/SASL bootstrap.
- **Your own namespace** — set `namespace` to anything; cluster-scoped objects
  (PriorityClass, per-namespace ClusterRoles) are named so multiple namespaces coexist.

---

## Two-chart story

This is the **core** chart: controller + executor + Config CRD + leaf agents +
optional recurring-task-creator. The **maintainer** application (PR-review bot, GitHub
releaser, the four watchers) ships as a *separate* `bborbe/maintainer` chart that depends
on this core. Install core first, then maintainer on top. *(The maintainer chart is
forthcoming; until then its components deploy via their existing manifests.)*

---

## See also

- Ops runbook (quant): **Agent Platform - Helm Deploy** — steady-state `make apply`,
  the one-time kubectl→Helm crossover, rollback.
- Architecture: **Source Chart Config Separation**.
