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
  `controller.kafkaBrokers`). mTLS is optional per component via `<component>.kafkaUser.enabled`
  (default off): it emits a Strimzi `KafkaUser` (`type: tls`) and mounts the client cert/key +
  cluster CA at `/client-cert/file`, `/client-key/file`, `/server-cert/file` — use with a
  `tls://…:9093` broker. Secrets are referenced by name only (Strimzi issues them; an external
  syncer must place them in the app namespace).
- **Image pull access** to the images — either the public `docker.io/bborbe/*`
  (no credentials) or an in-cluster mirror (set `image.registry` + `image.pullSecrets`).
- **Sentry DSN** (optional) — supply as a plain Kubernetes Secret (`existingSecret`) or
  let the chart create the Secret from a value you pass with `--set-string`
  (quant resolves this from TeamVault).
- **GitHub App / provider credentials for agents** — each leaf agent needs its provider
  token (e.g. Anthropic/MiniMax) delivered as Secret keys via the agent's `secretEnv`.
- **Executor ≥ v0.18.0 for `type: service` agents** — the `service` Config type and the
  StatefulSet reconcile loop live in the executor, not the chart. The chart's Role grants
  `create`/`get`/`update` on `apps/statefulsets` for that loop, and **deliberately withholds
  `delete`**: the loop's removal path deletes by name, so it is only safe on an executor that
  checks the object's Config controller ownerRef first (**v0.18.1+**), and the chart cannot see
  which executor version it is paired with. Grant `delete` only once that pairing is enforced
  rather than documented — on the quant path the gate is `check-executor-compat` in
  `nuke/agent`, which runs before `mirror`+`upgrade` and can refuse the combination.
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
| `cluster` | `""` | Cluster name rendered as `CLUSTER` on every executor and every controller. Controllers publish only tasks whose `cluster:` frontmatter matches. Requires agent-task-controller ≥ v0.14.0 and agent-task-executor ≥ v0.19.0. Empty = nothing rendered. |
| `publishUntagged` | unset | Rendered as `PUBLISH_UNTAGGED` (quoted `"true"`/`"false"`) on every controller **only when the key is set** — `false` is honoured, absent renders nothing. Set `false` on clusters that must run only explicitly tagged tasks (e.g. sun). Requires agent-task-controller ≥ v0.14.0. |

### sandbox

| Key | Default | Description |
|---|---|---|
| `sandbox.namespace` | `""` | Explicit sandbox namespace. Empty falls back to `<namespace>-agents-sandbox`, which every current install renders. Must differ from `namespace` — the render fails otherwise, since equal values emit a second `Namespace` object for the control namespace. Also relocates the egress proxy, its allowlist and the sandbox RBAC. |
| `sandbox.domains` | `{}` | Map of sandbox namespace name to its egress peer set. Non-empty renders one `Namespace` and one `<name>-egress` default-deny policy per entry, and the egress proxy once in `proxyNamespace`. Empty renders today's single sandbox unchanged. Mutually exclusive with `sandbox.namespace` — declaring both fails the render. Each entry takes an optional `peers` list; see **Multiple sandbox domains** below. |
| `sandbox.domains.<name>.peers` | unset | Destinations this domain may reach, from the fixed catalog `dns`, `router`, `pushgateway`, `sentry-proxy`, `vault-personal`, `vault-openclaw`, `vault-trading`, `frontend-gateway`, `frontend-strategy`, `vault-api`, `kafka`, `proxy`. An unknown name fails the render. `dns` and `proxy` are added unconditionally. **Omitting `peers` renders the full union** — deliberately, so the first apply of a domains install narrows nothing; `peers: []` renders only DNS and the proxy. |
| `sandbox.proxyNamespace` | `""` | Namespace hosting the single egress proxy (Deployment, Service, PDB, allowlist ConfigMap and both its policies). Must be one of the declared domains; required when more than one domain is declared, defaults to the only domain when exactly one is. Setting it with no `domains` fails the render. |
| `sandbox.peerNamespace` | `""` | Namespace the sandbox policy's **non-agent** peers select: sentry-proxy, the three `vault-obsidian-*` services, `frontend-gateway`, `frontend-strategy` and the optional vault-API app. Empty falls back to the control namespace, so every current install renders unchanged. Set it when the control plane moves to its own namespace but those services stay behind — otherwise the peers name a namespace holding none of them and the policy silently denies every sandboxed Job its vault and its Sentry proxy. **The router is not covered**: it is a control-plane component and follows `namespace`. |
| `sandbox.kafka.namespace` | `""` | Select Kafka by namespace (in-cluster Service) instead of the built-in nodeport addresses. Takes precedence over them. |
| `sandbox.kafka.podLabels` | `{}` | Pod selector narrowing the Kafka peer inside `sandbox.kafka.namespace`. Empty opens **every** pod in that namespace on `servicePort` — set it unless the namespace holds only brokers. |
| `sandbox.kafka.servicePort` | `0` | The broker pods' **container** port — not the Service port, since a NetworkPolicy port matches the destination pod. Required when `sandbox.kafka.namespace` is set, and must be a positive integer — the render fails on `0` or on a non-numeric value, either of which would emit a policy that matches nothing. |
| `sandbox.kafka.nodePorts` | `[32159]` | Nodeports the built-in nodeport branch admits, one `ports:` entry each. A NetworkPolicy port matches the destination pod, and a Kafka client bootstraps on one nodeport then follows the broker's advertised metadata to another — so admitting only the bootstrap port rejects the connection the client actually uses. Empty or unset falls back to `[32159]`; every element must be a positive integer or the render fails. Ignored when `sandbox.kafka.namespace` is set. |
| `sandbox.routerInstance` | `""` | `app.kubernetes.io/instance` the router peer selects. Empty = `claude-code-router-<namespace>`. |
| `sandbox.vaultApiApp` | `""` | When set, render an extra egress peer selecting this `app` label on port 9090 (e.g. `vault-api-openclaw`). |
| `sandbox.renderAgentStorage` | `false` | Also render each chart-rendered agent Secret and PVC into the sandbox namespace (gated on `executor.enabled`). With `domains` declared the copy follows each agent's `jobNamespace` and renders only when that names a declared domain; without domains it targets the single sandbox namespace regardless of `jobNamespace`. Only chart-rendered objects are copied, and a copied PVC is a new, empty volume — not a view of the control-namespace claim. |

### Multiple sandbox domains

Declare `sandbox.domains` when one install must host agents in more than one
namespace — for example a general `agent-sandbox` and a narrower
`trading-sandbox` whose Jobs may only reach trading services:

```yaml
sandbox:
  proxyNamespace: agent-sandbox
  domains:
    agent-sandbox: {}          # omitting peers renders the full union
    trading-sandbox:
      peers: [router, pushgateway, frontend-gateway, frontend-strategy]
```

The chart renders one `Namespace` per entry, carrying
`helm.sh/resource-policy: keep` so `helm uninstall` does not delete it, and one
`<name>-egress` `NetworkPolicy` with `podSelector app: agent`, `policyTypes:
[Egress]` and no ingress rules. Each domain's egress is exactly its `peers`
list, plus `dns` and `proxy`, which are unconditional: DNS is how a pod resolves
anything and the proxy is the only route to the public internet, so making
either opt-in would be an off-switch for egress that fails silently.

The egress proxy renders once, in `proxyNamespace`, and `egress-proxy-ingress`
admits `app: agent` pods from every rendered domain — a newly declared domain
reaches the proxy without a second edit. `proxyNamespace` is required when more
than one domain is declared, must name a declared domain, and defaults to that
domain when exactly one is declared.

Storage copies follow each agent's `jobNamespace` in domains mode only: with
`sandbox.renderAgentStorage: true` an agent whose `jobNamespace` names a
declared domain gets its Secret and PVC copied there; an agent with no
`jobNamespace`, or one naming an undeclared namespace, gets no sandbox copy.

`sandbox.namespace` and `sandbox.domains` are mutually exclusive. Installs that
declare no domains keep the legacy single sandbox and its policy name
`agent-sandbox-egress`.

### executor

| Key | Default | Description |
|---|---|---|
| `executor.enabled` | `true` | Deploy the executor. |
| `executor.image.repository` | `bborbe/agent-task-executor` | Image repo (under `image.registry`). |
| `executor.image.tag` | `""` | Image tag; defaults to chart `appVersion`. |
| `executor.kafkaBrokers` | `""` (**required**) | Kafka bootstrap brokers. |
| `executor.topicPrefix` | `""` | Kafka topic prefix. Empty = unprefixed (per-stage-cluster / Octopus). Quant: `develop`/`master`. |
| `executor.branch` | `""` | Stage label forwarded as `BRANCH`. |
| `executor.taskGlob` | `""` | git-rest single-level glob selecting the vault task files the reconcile loop evaluates. Consumer-supplied — the chart asserts no vault folder layout; empty = no glob configured. |
| `executor.consumerGroup` | `""` | Rendered as `CONSUMER_GROUP` on the executor. Gives each Kafka consumer its own consumer group (e.g. `agent-task-executor-sun`) so executors on different clusters do not share offsets. Empty = nothing rendered. |
| `executor.stageFilter` | unset | Rendered as `STAGE_FILTER` (quoted `"true"`/`"false"`) on the executor **only when the key is set** — `false` is honoured, absent renders nothing (the executor's own default `true` applies). `false` makes the executor ignore the task `stage:` field; set it only on a cluster whose controller already scopes tasks by cluster (e.g. sun); nuke dev/prod leave it unset. Requires agent-task-executor ≥ v0.21.0. |
| `executor.runUntagged` | unset | Rendered as `RUN_UNTAGGED` (quoted `"true"`/`"false"`) on the executor **only when the key is set** — `false` is honoured, absent renders nothing (the executor's own default `true` applies). `false` makes the executor skip every task without a `cluster:` field; set it only on a cluster whose controller publishes no untagged tasks (e.g. sun); nuke dev/prod leave it unset. Requires agent-task-executor ≥ v0.22.0. |
| `executor.sentry.proxy` | `""` | Sentry proxy URL (optional). |
| `executor.sentry.dsn` | `""` | Sentry DSN; when set the chart creates the Secret. |
| `executor.existingSecret` | `""` | Name of a pre-existing Secret with key `sentry-dsn`; when set the chart creates no Secret. |
| `executor.kafkaUser.enabled` | `false` | Emit a Strimzi `KafkaUser` + mount mTLS client certs. Off = plaintext, no CR, no volumes. |
| `executor.kafkaUser.cluster` | `my-cluster` | Strimzi Kafka CR name (KafkaUser `strimzi.io/cluster` label). |
| `executor.kafkaUser.strimziNamespace` | `strimzi` | Namespace the KafkaUser CR is created in (the Strimzi operator's, not the app ns). |
| `executor.kafkaUser.userName` | `""` | KafkaUser CR name. Empty = `<namespace>-agent-task-executor`. |
| `executor.kafkaUser.clientSecret` | `""` | Secret (app ns) with `user.crt`/`user.key`. Empty = same as the KafkaUser name. |
| `executor.kafkaUser.caCertSecret` | `my-cluster-cluster-ca-cert` | Secret (app ns) with the cluster CA as `ca.crt`. |
| `executor.podSecurityContext` / `securityContext` | `{}` / hardened | Pod/container security contexts. |
| `executor.resources` | 20m/20Mi → 500m/50Mi | Requests/limits. |

### executors (per-vault, values-driven)

`executors` is a values-driven list: each **enabled** entry renders one per-vault
Deployment `agent-task-executor-<name>` with `replicas: 1`. One executor per vault
means one consumer group, one offset domain, and one dedup state per vault — adding
a vault is a values change, not a chart code change. When the list is empty (the
default) the single legacy `agent-task-executor` Deployment renders unchanged.

| Key | Default | Description |
|---|---|---|
| `executors[].name` | — (**required**) | Deployment suffix → `agent-task-executor-<name>` + `vault: <name>` pod label. |
| `executors[].enabled` | `true` | `false` skips the entry. |
| `executors[].vaultName` | — (**required**) | Becomes the `VAULT_NAME` env. Slug `^[a-z][a-z0-9-]*$` is validated by the executor binary (≥ v0.7.0) at startup, NOT the chart; empty is rejected at render time. |
| `executors[].branch` | `executor.branch` | Stage label forwarded as `BRANCH`. |
| `executors[].topicPrefix` | `executor.topicPrefix` | Kafka topic prefix; passes through verbatim (see per-vault topics below). |
| `executors[].kafkaBrokers` | `executor.kafkaBrokers` | Kafka bootstrap brokers. Required when not inherited. |
| `executors[].image.repository` / `image.tag` | `executor.image` | Image repo/tag; empty tag → chart `appVersion`. |
| `executors[].logLevel` | `executor.logLevel` | Log verbosity. |
| `executors[].consumerGroup` | `executor.consumerGroup` | Rendered as `CONSUMER_GROUP` on that executor; per-entry value wins, empty = nothing rendered. |
| `executors[].stageFilter` | `executor.stageFilter` | Rendered as `STAGE_FILTER` on that executor; a per-entry value (including `false`) wins, otherwise `executor.stageFilter` is inherited, neither set = nothing rendered. |
| `executors[].runUntagged` | `executor.runUntagged` | Rendered as `RUN_UNTAGGED` on that executor; a per-entry value (including `false`) wins, otherwise `executor.runUntagged` is inherited, neither set = nothing rendered. |
| `executors[].sentry.proxy` | `executor.sentry.proxy` | Sentry proxy URL. |
| `executors[].existingSecret` | `executor.existingSecret`, then shared `agent-task-executor` | Pre-existing Secret with key `sentry-dsn`. |
| `executors[].podSecurityContext` / `securityContext` / `resources` | `executor.*` | Pod/container security contexts + resources. |

**Per-vault topics.** With a per-entry `topicPrefix` of `{stage}-{vault}`, the executor
consumes `{prefix}-agent-task-v1-event` and publishes `{prefix}-agent-task-v1-request`
(topic naming per `docs/kafka-schema-design.md`); the chart passes `TOPIC_PREFIX`
through verbatim and does not derive the prefix.

**Empty-list fallback and authoritative list mode.** `executors: []` (the default)
plus `executor.enabled: true` renders the single legacy `agent-task-executor`
Deployment with the same env contract as before (no `VAULT_NAME`). A non-empty list
is authoritative — the legacy Deployment is not rendered regardless of
`executor.enabled`; an all-disabled list renders no executor Deployment.

A minimal entry:

```yaml
executors:
  - name: openclaw
    vaultName: openclaw
    topicPrefix: develop-openclaw
    kafkaBrokers: kafka:9092
```

There is no per-entry `kafkaUser` — executor mTLS stays driven by the single shared
`executor.kafkaUser` block for all per-vault executors.

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
| `controller.kafkaUser.*` | off | Per-controller mTLS `KafkaUser` + cert mounts — same fields as `executor.kafkaUser` (`enabled`/`cluster`/`strimziNamespace`/`userName`/`clientSecret`/`caCertSecret`). Derived user name = `<namespace>-agent-task-controller-<name>`. |
| `controller.storage.size` | `1Gi` | BoltDB PVC size. |
| `controller.storage.storageClassName` | `""` | Empty = cluster default StorageClass (portable). Set explicitly only for a specific class (quant uses `local-path`, which needs the Local Path Provisioner). |
| `controller.sentry.*` / `existingSecret` | `""` | Sentry DSN, same pattern as executor. |
| `controller.gatewaySecret` | `""` | Gateway secret value (chart-created Secret key `gateway-secret`). |
| `controller.resources` | 20m/20Mi → 500m/50Mi | Requests/limits. |

### agents (leaf agents — values-driven)

`agents` is a list; each **enabled** entry emits a `Config` CR + PriorityClass
(cluster-scoped) + ResourceQuota (concurrency cap), plus a Secret only when
`secretEnv` is set and a PVC only when `volumeMountPath` is set (and the agent is
not `type: service`). Nothing is hard-coded — add/remove agents purely in values.

```yaml
agents:
  - name: agent-claude
    enabled: true
    assignee: claude-agent
    image: bborbe/agent-claude       # under image.registry
    tag: v0.1.1                       # defaults to appVersion when empty
    heartbeat: 5m
    zombieJobTimeoutSeconds: 5400     # OPTIONAL: Job deadline (seconds) — must exceed the
                                      # agent's own internal wait, or its Job dies first.
                                      # Omit for the executor default (1800s).
    rerunOnLostClaim: false           # OPTIONAL: `false` = never re-run a task after this
                                      # agent's claim is lost. Rendered whenever set
                                      # (including false); omit for the executor default.
    jobNamespace: dev-agents-sandbox   # OPTIONAL: Config.spec.jobNamespace — where this
                                      # agent's Jobs run. Omit for the executor namespace.
    taskTypes: [llm, healthcheck]
    triggerPhases: [planning, execution, ai_review]
    triggerStatuses: [in_progress]
    volumeMountPath: /home/claude/.claude
    storageSize: 1Gi
    configMapName: easy-agent-goreleaser   # OPTIONAL: ConfigMap mounted into the Job
    configMapMountPath: /agent             # OPTIONAL: where it is mounted
    configMapItems:                        # OPTIONAL: key → path projection
      - key: prompt.md
        path: prompt.md
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

An agent that waits on sibling tasks must set this deadline above its own wait timeout,
because the executor stamps it onto the Job's `activeDeadlineSeconds` and the Job is
killed when it elapses.

Interactive or session-holding runners (claude, pi, goreleaser) should set
`rerunOnLostClaim: false`, so that a lost claim cannot start a second session; the field
needs a Config CRD that declares `spec.rerunOnLostClaim`, because older CRDs prune it.

`jobNamespace` places that agent's Job pods in the named namespace. The namespace must
already exist and already hold the agent's PVC and Secret — either place them there
yourself, or point `jobNamespace` at the chart's own sandbox namespace
(`sandbox.namespace`, default `<namespace>-agents-sandbox`) and set
`sandbox.renderAgentStorage: true`, which renders the chart-managed copies into that
namespace. In domains mode (`sandbox.domains` declared) `renderAgentStorage` copies the
Secret and PVC into the sandbox that `jobNamespace` names, and only when that name is a
declared domain; with no domains the copy still targets the single `sandbox.namespace`.
A `jobNamespace` naming an undeclared namespace needs the PVC and Secret placed by hand. The value must be a valid DNS-1123 label (lowercase alphanumerics and `-`, at most
63 characters), because the executor's Config CRD schema rejects anything else at apply
time. A value equal to the executor's own namespace is rejected by the executor and no Job
is spawned. The field needs agent-task-executor >= v0.31.0, because an older executor
ignores the field and an older Config CRD prunes it.

`configMapName` / `configMapMountPath` / `configMapItems` render a **reference** onto the
Config CR; the chart does not create the ConfigMap itself, so the object must already exist
in the namespace. This is what lets an agent be parameterised by mounted markdown — a
`prompt.md` plus its guardrails — instead of a bespoke image.

### recurringTaskCreator (optional)

| Key | Default | Description |
|---|---|---|
| `recurringTaskCreator.enabled` | `false` | Deploy the recurring-task-creator StatefulSet + RBAC. |
| `recurringTaskCreator.image.repository` | `bborbe/recurring-task-creator` | Image repo. |
| `recurringTaskCreator.kafkaBrokers` | `""` | Kafka bootstrap brokers. |
| `recurringTaskCreator.stage` / `topicPrefix` / `dryRun` | `""` / `""` / `"false"` | Runtime config. |
| `recurringTaskCreator.sentry.*` | `""` | Sentry DSN, same pattern. |
| `recurringTaskCreator.kafkaUser.enabled` | `false` | Emit a Strimzi `KafkaUser` + mount mTLS client certs (mTLS clusters only). |
| `recurringTaskCreator.kafkaUser.cluster` / `strimziNamespace` | `my-cluster` / `strimzi` | Strimzi cluster + namespace. |
| `recurringTaskCreator.kafkaUser.userName` / `clientSecret` / `caCertSecret` | `""` / `""` / `my-cluster-cluster-ca-cert` | As `executor.kafkaUser`. Derived user name = `<namespace>-recurring-task-creator`. |

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
- **Network-isolated sandbox** — the chart renders the sandbox namespace
  (`sandbox.namespace`, default `<namespace>-agents-sandbox`) with a
  default-deny egress NetworkPolicy (kube-dns, sentry-proxy, the router, pushgateway, the three
  vault-obsidian services, frontend-gateway, frontend-strategy, Kafka and the egress proxy —
  nothing else) and cross-namespace RBAC for
  the executor. Agent Jobs land there; it is inert until the executor ships
  `ConfigSpec.JobNamespace` (spawns Jobs into the sandbox). The namespace also hosts a
  two-replica tinyproxy egress proxy (`egress-proxy`, port 8888) and its allowlist
  ConfigMap — the only path from a sandbox pod to the public internet; changing the
  allowlist is a ConfigMap edit, never an image rebuild.
  Kafka is selected either by the built-in nuke nodeport `/32`s on the ports in
  `sandbox.kafka.nodePorts` (default `[32159]`) or, when `sandbox.kafka.namespace`
  + `sandbox.kafka.servicePort` are set, by an in-cluster namespace + optional
  `sandbox.kafka.podLabels` selector. A Kafka client bootstraps on one nodeport
  and then follows the broker's advertised metadata to another, so list every
  nodeport the brokers advertise — the bootstrap port alone is not enough.
  `pushgateway` remains pinned to the `monitoring` namespace.
- **Egress proxy** — the chart renders a domain-allowlisting tinyproxy (`egress-proxy`,
  port 8888) in the sandbox namespace (`sandbox.namespace`, default
  `<namespace>-agents-sandbox`). The allowlist lives in the
  `egress-proxy-allowlist` ConfigMap — change it there and restart the proxy, no image
  rebuild. Direct internet egress from agent pods is denied except through the proxy.
  ⚠️ The proxy env is **not** injected by default: set `egressProxy.injectAgentEnv: true`
  to add `HTTP_PROXY`/`HTTPS_PROXY`/`no_proxy` to Job agents' Configs. It stays off until
  the executor spawns Jobs into the sandbox (`ConfigSpec.JobNamespace`, spec 002) — while
  Jobs run in the control namespace, `egress-proxy-ingress` does not admit them and the
  injected proxy would deny every outbound call. `type: service` agents never receive it.
  The injected `NO_PROXY` keeps in-cluster traffic direct: `.svc.cluster.local`, the
  Kafka broker hosts, and the `<service>.<namespace>` short form of every Service peer in
  the sandbox catalog (`pushgateway.monitoring`, `sentry-proxy.<peerNamespace>`, the
  vaults, the trading frontends, the router) — derived from the catalog, never a bare
  namespace suffix like `.prod`, which would let public hosts bypass the proxy.

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
