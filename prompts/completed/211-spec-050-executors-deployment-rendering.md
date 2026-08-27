---
status: completed
spec: [050-executors-per-vault-list]
summary: Modified helm/templates/executor-deployment.yaml to render one per-vault agent-task-executor-<name> Deployment per enabled executors entry (with VAULT_NAME/TOPIC_PREFIX env contract and executor.* fallbacks), gated the legacy single-executor block on executors being empty, and verified all ACs 1/2/3/4/7 including required-error failure modes and byte-identical legacy render
execution_id: agent-exec-211-spec-050-executors-deployment-rendering
dark-factory-version: dev
created: "2026-08-27T10:56:00Z"
queued: "2026-08-27T11:29:20Z"
started: "2026-08-27T11:32:29Z"
completed: "2026-08-27T11:40:00Z"
branch: dark-factory/executors-per-vault-list
---

<summary>
- The executor Deployment template learns a list mode: one Deployment per enabled `executors` entry, named `agent-task-executor-<name>`
- Each per-vault pod carries the shared `app: agent-task-executor` label (so the existing executor Service keeps selecting it) plus a distinct `vault: <name>` label, and the Deployment selector matches both — no two entries claim the same pods
- Each per-vault container receives the full legacy env contract plus two new envs: `VAULT_NAME` (from the entry's `vaultName`, render fails via `required` if empty) and `TOPIC_PREFIX` (from the entry's `topicPrefix`, empty passes through)
- Every optional per-entry field falls back to the existing `executor.*` values block (image, logLevel, sentry.proxy, existingSecret, security contexts, resources)
- The legacy single `agent-task-executor` Deployment still renders unchanged when `executors` is empty and `executor.enabled` is true — same env contract, no `VAULT_NAME`
- List mode is authoritative: when `executors` is non-empty the legacy Deployment is not rendered regardless of `executor.enabled`; an all-disabled list renders no executor Deployment at all
- mTLS stays shared: `executor.kafkaUser.enabled` drives the cert mounts, KafkaUser CR, and `JOB_KAFKA_*` env for every per-vault executor exactly as today
</summary>

<objective>
Make the chart render one per-vault executor Deployment per enabled `executors` entry with the full per-vault env contract, while keeping the default render byte-identical to today's single executor. This is prompt 2 of 3 — the core rendering behavior; it depends on prompt 1 having added the `executors` value to `helm/values.yaml`.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions.

Read these files IN FULL before editing:
- `/workspace/helm/templates/executor-deployment.yaml` — the file you are editing. It is currently a single `{{- if .Values.executor.enabled }}`-gated Deployment `agent-task-executor` (legacy single executor) whose container env (lines ~58-91), probes, securityContext, resources, and shared-`executor.kafkaUser` cert-mount/volume blocks are the per-entry contract you must reproduce with per-entry defaults and the two new envs.
- `/workspace/helm/templates/controller-statefulset.yaml` — the in-repo exemplar for the list-range pattern (lines 1-3: `{{- range $c := .Values.controllers }}` / `{{- if $c.enabled }}` / `---`). It shows the exact root-context access idiom inside a range loop (`$.Values.image.pullPolicy`, `include "agent.namespace" $`, `include "agent.labels" $ | nindent 4`), the per-entry image printf (line 53), the `required "controllers[].kafkaBrokers is required"` / `required "controllers[].vaultName is required"` pattern, and the `($c.image | default dict)` / `($c.sentry | default dict).proxy` nil-safe access idiom.
- `/workspace/helm/templates/_helpers.tpl` — the `agent.executor.image`, `agent.namespace`, `agent.labels`, `agent.kafkaCertVolumeMounts`, `agent.kafkaCertVolumes` helpers (the kafka helpers take their args; namespace/labels read root context — inside the range loop pass `$`, not `.`).
- `/workspace/helm/values.yaml` — the `executor.*` defaults block and the `executors` example added by prompt 1 (precondition below).

Coding-plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/k8s-manifest-guide.md` — confirms a `Deployment` is the correct workload kind for the stateless per-vault queue consumer (offsets live in Kafka, dedup in memory; one replica by design).

Precondition (prompt 1 must have landed): `/workspace/helm/values.yaml` MUST already declare `executors: []`. Verify with:
```bash
grep -n '^executors:' /workspace/helm/values.yaml
```
If it returns zero lines, STOP and report a precondition failure — do NOT re-implement the values schema; prompt 1 owns it.
</context>

<requirements>

## 1. Gate the legacy single-executor block on `executors` being empty

In `/workspace/helm/templates/executor-deployment.yaml`, change ONLY the first line of the file:

- From: `{{- if .Values.executor.enabled }}`
- To: `{{- if and .Values.executor.enabled (empty .Values.executors) }}`

Do NOT change any other line of the legacy block (env contract, probes, volumes, labels, annotations all stay byte-identical). When `executors` is empty and `executor.enabled` is true, this block renders exactly today's `agent-task-executor` Deployment with no `VAULT_NAME` env.

## 2. Add the list-mode range block after the legacy block

After the legacy block's closing `{{- end }}` (currently the last line of the file), append a new block that renders one Deployment per enabled `executors` entry. Mirror the controllers structure verbatim:

```
{{- range $e := .Values.executors }}
{{- if $e.enabled }}
---
... (Deployment manifest — see below)
{{- end }}
{{- end }}
```

Inside the loop, `.` is the entry; use `$` for root context everywhere root values are needed (mirror `controller-statefulset.yaml`). Render each Deployment as follows:

- `metadata.name`: `agent-task-executor-{{ $e.name }}`
- `metadata.namespace`: `{{ include "agent.namespace" $ }}`
- `metadata.labels`: `app: agent-task-executor-{{ $e.name }}` plus `{{- include "agent.labels" $ | nindent 4 }}` (mirror the controllers metadata labels; the Deployment's own app label is suffixed so each Deployment is uniquely identifiable — the shared Service selects on pod labels, not Deployment labels)
- `metadata.annotations`: the keel block gated on `$.Values.keel.enabled` (policy/trigger/match-tag/pollSchedule from `$.Values.keel.pollSchedule`, quoted) and the `{{- with $.Values.rolloutNonce }} random: {{ . | quote }} {{- end }}` block — both mirror the legacy block but read via `$.Values`
- `spec.replicas`: `1`
- `spec.progressDeadlineSeconds`: `600`
- `spec.selector.matchLabels`: exactly two keys — `app: agent-task-executor` AND `vault: {{ $e.name }}`
- `spec.template.metadata.annotations`: the prometheus scrape block (`prometheus.io/path: /metrics`, `/port: "9090"`, `/scheme: http`, `/scrape: "true"`) plus the `{{- with $.Values.rolloutNonce }} random ... {{- end }}` block
- `spec.template.metadata.labels`: exactly two keys — `app: agent-task-executor` AND `vault: {{ $e.name }}`. CRITICAL: the pod carries `app: agent-task-executor`, NOT `app: agent-task-executor-<name>` — the shared executor Service (unchanged, selector `app: agent-task-executor`) must keep selecting these pods. The `vault` label makes each entry's selector disjoint so no two entries claim the same pods.

## 3. Per-vault pod spec and container contract

`spec.template.spec`:

- `serviceAccountName`: `agent-task-executor` (the shared ServiceAccount — unchanged RBAC)
- pod-level securityContext: `{{- with ($e.podSecurityContext | default $.Values.executor.podSecurityContext) }}` → `{{- toYaml . | nindent 8 }}`
- affinity: `{{- with $.Values.affinity }}` (shared global affinity) → `{{- toYaml . | nindent 8 }}`

Container (name `service`):

- `image`: `{{ printf "%s/%s:%s" $.Values.image.registry (($e.image | default dict).repository | default $.Values.executor.image.repository) (($e.image | default dict).tag | default $.Values.executor.image.tag | default $.Chart.AppVersion) | quote }}` — mirror `controller-statefulset.yaml` line 53 but default the repository AND tag to `$.Values.executor.image.*` (the values default `bborbe/agent-task-executor` / empty-tag→appVersion; spec DB 1's executor.* fallback)
- `imagePullPolicy`: `{{ $.Values.image.pullPolicy }}`
- container securityContext: `{{- with ($e.securityContext | default $.Values.executor.securityContext) }}` → `{{- toYaml . | nindent 12 }}`
- `args`: `- -v={{ $e.logLevel | default $.Values.executor.logLevel }}`
- `env` — the exact per-vault contract (mirror the legacy env lines 58-91, applying per-entry defaults and adding the two new vars):

  | env | value expression |
  |---|---|
  | `LISTEN` | `':9090'` |
  | `SENTRY_DSN` | `secretKeyRef` — `name: {{ $e.existingSecret | default $.Values.executor.existingSecret | default "agent-task-executor" }}`, `key: sentry-dsn` |
  | `SENTRY_PROXY` | only when set: `{{- with (($e.sentry | default dict).proxy | default $.Values.executor.sentry.proxy) }}` → `value: {{ . | quote }}` (falls back to the global `executor.sentry.proxy` — spec DB 1's "every optional field absent from an entry falls back to the existing executor.* block") |
  | `BRANCH` | `{{ $e.branch | default $.Values.executor.branch | quote }}` |
  | `VAULT_NAME` | `{{ required "executors[].vaultName is required" $e.vaultName | quote }}` (NEW — mandatory; render fails when the entry's `vaultName` is empty) |
  | `TOPIC_PREFIX` | `{{ $e.topicPrefix | default $.Values.executor.topicPrefix | quote }}` (empty passes through as today) |
  | `KAFKA_BROKERS` | `{{ required "executors[].kafkaBrokers is required" ($e.kafkaBrokers | default $.Values.executor.kafkaBrokers) | quote }}` (fires only when BOTH the entry and `executor.kafkaBrokers` are empty) |
  | `NAMESPACE` | `valueFrom` `fieldRef` `fieldPath: metadata.namespace` |
  | `JOB_KAFKA_CLIENT_CERT_SECRET` | only when `$.Values.executor.kafkaUser.enabled` — `value: {{ $ku.clientSecret | default $userName | quote }}` with `{{- $ku := $.Values.executor.kafkaUser }}` and `{{- $userName := $ku.userName | default (printf "%s-agent-task-executor" (include "agent.namespace" $)) }}` (mirror legacy lines 80-91) |
  | `JOB_KAFKA_CA_CERT_SECRET` | only when `$.Values.executor.kafkaUser.enabled` — `value: {{ $ku.caCertSecret | quote }}` |

- `ports`: `containerPort: 9090`, `name: http`
- `livenessProbe` / `readinessProbe`: mirror the legacy probes exactly (httpGet `/healthz` and `/readiness` on port 9090, scheme HTTP, `initialDelaySeconds: 10` / `5`, `timeoutSeconds: 5`, `failureThreshold: 5`, `successThreshold: 1`)
- `resources`: `{{- toYaml ($e.resources | default $.Values.executor.resources) | nindent 12 }}`
- `volumeMounts`: when `$.Values.executor.kafkaUser.enabled`, `{{- include "agent.kafkaCertVolumeMounts" $ | nindent 12 }}`

Pod-level `volumes` (when `$.Values.executor.kafkaUser.enabled`): mirror legacy lines 117-122 reading `$ku := $.Values.executor.kafkaUser` and `$userName := $ku.userName | default (printf "%s-agent-task-executor" (include "agent.namespace" $))`, emitting `{{- include "agent.kafkaCertVolumes" (dict "clientSecret" ($ku.clientSecret | default $userName) "caCertSecret" ($ku.caCertSecret | default "my-cluster-cluster-ca-cert")) | nindent 8 }}`.

Pod-level `imagePullSecrets`: `{{- with $.Values.image.pullSecrets }}` → `{{- toYaml . | nindent 8 }}`.

## 4. Authoritative list mode and legacy fallback

These must hold exactly (they are the spec's Desired Behavior 4 and the AC 3/4 evidence):

- `executors` empty AND `executor.enabled` true → exactly one legacy `agent-task-executor` Deployment, same env contract as today, no `VAULT_NAME`.
- `executors` non-empty → list mode is authoritative: NO Deployment named exactly `agent-task-executor` renders, regardless of `executor.enabled`.
- `executors` non-empty but every entry `enabled: false` → no executor Deployment renders at all.
- `executor.enabled` remains the legacy-path gate only.

## 5. Failure-mode behavior (from the spec table — all render-time, verified in `<verification>`)

- `executors[].vaultName` empty → `helm template` exits non-zero with a `required` error message naming the field (`executors[].vaultName is required`).
- `executors[].kafkaBrokers` empty and not inherited from `executor.kafkaBrokers` → `helm template` exits non-zero with `executors[].kafkaBrokers is required`.
- Two entries sharing a `name` → both render (no dedupe), two same-named Deployment manifests — same property as `controllers[]`; do NOT add a `required` or uniqueness check on `name` (the spec only mandates `required` for `vaultName` and `kafkaBrokers`).
- `executors[].topicPrefix` empty → passes through as empty (unprefixed), no chart error — per-vault isolation failure from a missing prefix is a consumer-values concern, documented in prompt 3's README work.
- `vaultName` violating `^[a-z][a-z0-9-]*$` → renders fine (no chart regex); runtime validation is the executor binary's job.

## 6. Create the verification fixtures

Write these six fixture files (exact contents):

`/tmp/executors-values.yaml` (name == vaultName on both, for AC 1/3/4 evidence):
```yaml
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
executors:
  - name: openclaw
    enabled: true
    vaultName: openclaw
    topicPrefix: develop-openclaw
    kafkaBrokers: kafka:9092
  - name: personal
    enabled: true
    vaultName: personal
    topicPrefix: develop-personal
    kafkaBrokers: kafka:9092
```

`/tmp/executors-values-mismatch.yaml` (name != vaultName on entry 1, for AC 2 evidence):
```yaml
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
executors:
  - name: oc
    enabled: true
    vaultName: openclaw
    topicPrefix: develop-openclaw
    kafkaBrokers: kafka:9092
  - name: personal
    enabled: true
    vaultName: personal
    topicPrefix: develop-personal
    kafkaBrokers: kafka:9092
```

`/tmp/executors-disabled.yaml` (all-disabled list renders no Deployment):
```yaml
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
executors:
  - name: openclaw
    enabled: false
    vaultName: openclaw
    topicPrefix: develop-openclaw
    kafkaBrokers: kafka:9092
  - name: personal
    enabled: false
    vaultName: personal
    topicPrefix: develop-personal
    kafkaBrokers: kafka:9092
```

`/tmp/executors-empty-vaultname.yaml` (required-error probe):
```yaml
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
executors:
  - name: oc
    enabled: true
    vaultName: ""
    topicPrefix: develop-openclaw
    kafkaBrokers: kafka:9092
```

`/tmp/executors-empty-brokers.yaml` (required-error probe — no executor.kafkaBrokers to inherit):
```yaml
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: ""
  sentry:
    dsn: dummy
executors:
  - name: oc
    enabled: true
    vaultName: openclaw
    topicPrefix: develop-openclaw
```

`/tmp/executors-legacy.yaml` (AC 3 default-render probe — legacy single executor, no executors list):
```yaml
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
```

## 7. Scope containment

Edit ONLY `/workspace/helm/templates/executor-deployment.yaml` in this prompt. Do NOT touch `executor-service.yaml`, `executor-rbac.yaml`, `executor-secret.yaml`, `executor-kafkauser.yaml`, `helm/values.yaml`, `helm/README.md`, `helm/Chart.yaml`, `CHANGELOG.md`, or any Go file. The shared single-instance templates keep gating on `executor.enabled` exactly as today — untouched.

## 8. Self-check

Before finishing, re-run the `<verification>` commands below and confirm each passes; walk each acceptance criterion in `/workspace/specs/in-progress/050-executors-per-vault-list.md` that this prompt owns (AC 1, 2, 3, 4, 7) against the rendered output.
</requirements>

<constraints>
- This spec changes exactly five files; this prompt changes ONLY `helm/templates/executor-deployment.yaml`.
- The shared single-instance executor templates are untouched and keep gating on `executor.enabled` exactly as today: `executor-service.yaml` (selector `app: agent-task-executor`), `executor-rbac.yaml` (ServiceAccount/ClusterRole/Role), `executor-secret.yaml` (sentry-dsn), `executor-kafkauser.yaml`. Every per-vault executor pod MUST keep the `app: agent-task-executor` label so the Service selector keeps matching.
- `vaultName` is required at render time via the `required` helper; the slug pattern `^[a-z][a-z0-9-]*$` is validated by the executor binary at startup (v0.7.0) and is NOT the chart's job.
- `topicPrefix` passes through verbatim per entry; the chart does NOT derive or default `{stage}-{vault}`.
- Executor mTLS remains single/shared: `executor.kafkaUser.enabled` drives the cert mounts and KafkaUser CR for all per-vault executors exactly as it does for the single executor today. No per-entry `kafkaUser`.
- `make precommit` must still pass; no Go code is touched.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
cd /workspace
# The container image (bborbe/claude-yolo:v0.15.0) does NOT ship helm, and
# get.helm.sh is 403 from the build network — install helm from the Go proxy
# (pattern from prompts/1-spec-048-egress-proxy-resources.md, verified working).
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi
helm version --short   # must print v3.16

# --- AC 1: list mode renders exactly N per-vault Deployments, one per entry ---
helm template helm/ -f /tmp/executors-values.yaml | grep -E 'name: agent-task-executor-(openclaw|personal)$'
# Must print exactly two lines: agent-task-executor-openclaw and agent-task-executor-personal.
helm template helm/ -f /tmp/executors-values.yaml | grep -cE 'name: agent-task-executor-(openclaw|personal)$'
# Must print 2.

# --- AC 2: VAULT_NAME reads vaultName (not name); TOPIC_PREFIX per entry; distinct envs ---
helm template helm/ -f /tmp/executors-values-mismatch.yaml | grep 'name: agent-task-executor-oc$'
# Must print 1 (Deployment named after `name`, not vaultName).
helm template helm/ -f /tmp/executors-values-mismatch.yaml | grep -A1 'name: VAULT_NAME'
# Must show "value: openclaw" then "value: personal" (openclaw under the agent-task-executor-oc block proves VAULT_NAME reads vaultName, not name).
helm template helm/ -f /tmp/executors-values-mismatch.yaml | grep -A1 'name: TOPIC_PREFIX'
# Must show "value: develop-openclaw" then "value: develop-personal".
helm template helm/ -f /tmp/executors-values-mismatch.yaml | grep -c 'value: oc$'
# Must print 0 (the entry name `oc` appears only in the Deployment/label names, never as a VAULT_NAME value).

# --- AC 3: default render = one legacy Deployment, no VAULT_NAME ---
# (A bare `helm template helm/` exits 1 — namespace/kafkaBrokers/existingSecret are
# required by the chart — so render with the legacy fixture instead.)
helm template helm/ -f /tmp/executors-legacy.yaml | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'
# Must print 1.
helm template helm/ -f /tmp/executors-legacy.yaml | grep -c 'name: VAULT_NAME'
# Must print 0.

# --- AC 4: list mode is authoritative — legacy absent ---
helm template helm/ -f /tmp/executors-values.yaml | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'
# Must print 0.

# --- All-disabled list renders no executor Deployment ---
helm template helm/ -f /tmp/executors-disabled.yaml | grep -c 'kind: Deployment'
# Must print 0.

# --- Failure modes: required errors fire and name the field ---
helm template helm/ -f /tmp/executors-empty-vaultname.yaml 2>&1 | grep -c 'executors\[\.\]vaultName is required'
# Must print >= 1 (render fails).
helm template helm/ -f /tmp/executors-empty-brokers.yaml 2>&1 | grep -c 'executors\[\.\]kafkaBrokers is required'
# Must print >= 1 (render fails).

# --- AC 7: helm lint passes with default values and with an executors-set file ---
helm lint helm/ -f /tmp/executors-legacy.yaml
# Must exit 0 with "0 chart(s) failed".
helm lint helm/ -f /tmp/executors-values.yaml
# Must exit 0 with "0 chart(s) failed".

# --- Repo gate: no Go code changed, so the tree must still pass ---
make precommit
# Must exit 0. If the Makefile's ROOTDIR derivation (`git rev-parse --show-toplevel`)
# fails because .git is masked in the container, re-run as: ROOTDIR=/workspace make precommit
```
</verification>

---

## Open Questions (audit-time only — not actionable by the executor)

- **Deployment metadata labels are not specified by the spec.** The spec pins the pod labels (`app: agent-task-executor` + `vault: <name>`) and the selector, but is silent on the Deployment's own `metadata.labels`. This prompt chooses `app: agent-task-executor-<name>` (mirroring the controllers' `app: agent-task-controller-<name>` on the StatefulSet metadata), so each per-vault Deployment is uniquely identifiable by its own app label. If the reviewer prefers `app: agent-task-executor` on the Deployment metadata too (to group all executor Deployments under one `-l app=` query), adjust requirement 2 — the pod labels and selector must NOT change either way (the shared Service depends on them).
- **SENTRY_DSN secret-name default chain.** The spec's Desired Behavior 3 says "SENTRY_DSN from the entry's `existingSecret` defaulting to the shared `agent-task-executor` Secret". This prompt uses the three-hop chain `$e.existingSecret -> $.Values.executor.existingSecret -> "agent-task-executor"` — the middle hop (inheriting `executor.existingSecret`) is inferred from Desired Behavior 1 ("every optional field absent from an entry falls back to the existing `executor.*` block"). If the reviewer intends the entry to fall back DIRECTLY to `"agent-task-executor"` (skipping `executor.existingSecret`), drop the middle hop.
- **`name` is not `required`-gated.** The spec describes `name` as required (Desired Behavior 1) but only mandates the `required` helper for `vaultName` (Desired Behavior 3) and `kafkaBrokers`; the Failure Modes table treats duplicate names as "same property as `controllers[]`" (no dedupe). To mirror `controllers[]` exactly, this prompt adds no `required` on `name` — an empty `name` renders `agent-task-executor-`. If the reviewer wants a `required` on `name`, it must be added to the spec's Failure Modes table first.
