---
status: completed
summary: Added optional clusterAssigner CronJob + KafkaUser to the Helm chart, with values, README docs, chart 0.11.0 bump, and changelog entry
execution_id: agent-cluster-assigner-chart-exec-242-helm-cluster-assigner-cronjob
dark-factory-version: v0.198.7
created: "2026-10-21T08:23:46Z"
queued: "2026-10-21T08:31:54Z"
started: "2026-10-21T08:32:09Z"
completed: "2026-10-21T08:39:13Z"
---
# feat(helm): add optional cluster-assigner CronJob

<summary>
- The chart can optionally run the cluster assigner every five minutes, which routes every not-yet-started agent task in the OpenClaw vault to a cluster
- The component is off by default; when off, the chart renders exactly what it renders today (only the chart version label changes)
- Overlapping runs are impossible: a new run is skipped while the previous one is still going
- It reuses the git-rest gateway secret the vault's controller already uses, so no new secret has to be provisioned
- Sending tasks to the sun cluster is a separate opt-in; turning it on without naming the secret that holds the sun credentials stops the install with a clear error
- On clusters with Kafka mTLS it gets its own Kafka user and client certificates, the same way the recurring task creator does
- The values reference documents every setting, the chart version is bumped, and the change is in the changelog
</summary>

<objective>
Add an optional `clusterAssigner` component to the Helm chart in `helm/`: a CronJob running `bborbe/agent-cluster-assigner` (v0.1.0, github.com/bborbe/agent-cluster-assigner), which sets `cluster:` frontmatter on every unstarted agent task in the OpenClaw vault by publishing the task controller's UpdateFrontmatterCommand to Kafka. Only nuke prod will enable it; nuke dev and sun leave it disabled, so with `clusterAssigner.enabled: false` (the default) nothing new may render.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single global `CHANGELOG.md` at repo root, no per-module changelog).

This is translation work: every pattern below already exists in the chart. Read these files before editing and follow them; anchor on the names given, not on line numbers:

- `helm/templates/recurring-task-creator-statefulset.yaml` — the closest exemplar for an optional, single-instance component: whole file gated on `{{- if .Values.recurringTaskCreator.enabled }}`, `$ns := include "agent.namespace" .`, `required "... is required"` on `KAFKA_BROKERS`, `DRY_RUN` rendered with `| quote`, `SENTRY_DSN` as a `secretKeyRef` with key `sentry-dsn`, and — most importantly — the Kafka mTLS wiring: `{{- if .Values.recurringTaskCreator.kafkaUser.enabled }}` around `include "agent.kafkaCertVolumeMounts" .` and around `include "agent.kafkaCertVolumes" (dict "clientSecret" ($ku.clientSecret | default $userName) "caCertSecret" ($ku.caCertSecret | default "my-cluster-cluster-ca-cert"))`, with `$userName := $ku.userName | default (printf "%s-recurring-task-creator" (include "agent.namespace" .))`.
- `helm/templates/recurring-task-creator-kafkauser.yaml` — the KafkaUser exemplar (nested `if enabled` / `if kafkaUser.enabled`, `kafka.strimzi.io/v1beta2`, name `userName | default "<namespace>-recurring-task-creator"`, namespace `strimziNamespace | default "strimzi"`, label `strimzi.io/cluster: cluster | default "my-cluster"`, `spec.authentication.type: tls`). Note: neither this file nor `controller-kafkauser.yaml` / `executor-kafkauser.yaml` declares any `spec.authorization` / ACL block — the chart's KafkaUsers are authentication-only.
- `helm/templates/controller-statefulset.yaml` — the `GATEWAY_SECRET` env: `valueFrom.secretKeyRef` with `name: {{ $c.existingSecret | default (printf "agent-task-controller-%s" $c.name) }}` and `key: gateway-secret`. This is the pattern the cluster assigner's `GATEWAY_SECRET` must mirror (same Secret family, same key), so no new Secret is created. Also the global `affinity` (`{{- with $.Values.affinity }}`) and `image.pullSecrets` handling.
- `helm/templates/controller-secret.yaml` — shows the controller Secret `agent-task-controller-<name>` carries both keys `sentry-dsn` and `gateway-secret`.
- `helm/templates/a2a-worker-controller-deployment.yaml` — exemplar for a component versioned independently of the chart: image built inline as `"{{ .Values.image.registry }}/{{ $a.image.repository }}:{{ $a.image.tag }}"` (no `Chart.AppVersion` fallback), and `$a := .Values.a2aWorkerController` as a local alias.
- `helm/templates/_helpers.tpl` — `agent.labels`, `agent.namespace`, `agent.kafkaCertVolumeMounts` (no args, list items at column 0), `agent.kafkaCertVolumes` (dict arg `clientSecret` / `caCertSecret`).
- `helm/values.yaml` — the `recurringTaskCreator:` block (comment style, `kafkaUser:` sub-block with `enabled/cluster/strimziNamespace/userName/clientSecret/caCertSecret`, `resources`) and the `a2aWorkerController:` block (last block in the file; independent image tag comment). The executor block's `securityContext` shows the image-independent hardening defaults.
- `helm/README.md` — `### recurringTaskCreator (optional)` table (style for the new section).
- `helm/Chart.yaml` — currently `version: 0.10.0`, `appVersion: "0.3.1"`, `sources:` list.
- `CHANGELOG.md` — top heading is `## v0.110.0`; there is no `## Unreleased` section yet. Helm entries use `- feat(helm): ... chart 0.9.0→0.10.0. ...`.

There is no CronJob template in this chart yet; use standard `batch/v1` `CronJob` fields. Everything inside the pod template (container, env, mounts, volumes, affinity, pull secrets) follows the exemplars above.

Binary contract (agent-cluster-assigner v0.1.0 `main.go` env tags, verified by the requester): `KAFKA_BROKERS`, `TOPIC_PREFIX`, `GIT_REST_URL`, `GATEWAY_SECRET`, `TASK_GLOB`, `VAULT`, `DEFAULT_CLUSTER_PROD`, `DEFAULT_CLUSTER_DEV`, `SUN_ROUTING`, `DRY_RUN`, `SUN_API_URL`, `SUN_NAMESPACE`, `SUN_TOKEN`, `SUN_CA_DATA`, `SENTRY_DSN`, `SENTRY_PROXY`, `SUN_TIMEOUT`. `SENTRY_PROXY` renders only when the fleet-wide `sentry.proxy` value is set (see requirement 3); `SUN_TIMEOUT` is deliberately left unset by the chart. The binary publishes to the `<topicPrefix>-agent-task-v1-request` topic.
</context>

<requirements>
1. **Install helm, then snapshot before editing.** First run the helm install from the `<verification>` precondition (the container does not ship helm). Then copy the unmodified chart: `rm -rf /tmp/helm-orig && cp -r helm /tmp/helm-orig`. Create `/tmp/ca-base.yaml` (the minimal values that make the default chart render; verified to render today):
   ```yaml
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     sentry:
       dsn: dummy
   ```

2. **`helm/values.yaml` — add a `clusterAssigner:` block** after the `recurringTaskCreator:` block (before `a2aWorkerController:`), with a leading comment block in the file's style explaining: what it does (CronJob that routes every unstarted agent task in the vault by setting `cluster:` frontmatter via the controller's UpdateFrontmatterCommand on Kafka), that it is off by default and only nuke prod enables it, and that when `enabled: false` nothing renders. Keys and defaults:
   - `enabled: false`
   - `image.repository: bborbe/agent-cluster-assigner`, `image.tag: v0.1.0` — comment: versioned independently of the chart (like `a2aWorkerController`), so no `appVersion` fallback.
   - `logLevel: "2"`
   - `schedule: "*/5 * * * *"`, `successfulJobsHistoryLimit: 1`, `failedJobsHistoryLimit: 3`
   - `kafkaBrokers: ""` (required when enabled; `tls://…:9093` with `kafkaUser`), `topicPrefix: ""` (comment: the binary publishes to `<topicPrefix>-agent-task-v1-request`; nuke prod sets `master`)
   - `gitRestURL: ""` (required when enabled; git-rest base URL of the vault)
   - `existingSecret: ""` — comment: name of the Secret providing key `gateway-secret` (→ `GATEWAY_SECRET`) and optionally `sentry-dsn` (→ `SENTRY_DSN`); empty = the vault's controller Secret `agent-task-controller-<vault>`, which assumes the controller instance is named after the vault; the chart never creates a Secret for this component.
   - `vault: openclaw`, `taskGlob: "tasks/*.md"`, `defaultClusterProd: nukeprod`, `defaultClusterDev: nukedev`
   - `sunRouting: false`, `dryRun: false` (both YAML bools)
   - `sun:` with `apiURL: ""`, `namespace: a2a`, `secretName: ""` — comment on `secretName`: Secret holding keys `sun-token` (→ `SUN_TOKEN`) and `sun-ca-data` (→ `SUN_CA_DATA`); read only when `sunRouting: true`, and required then.
   - `securityContext:` — copy the executor block's container `securityContext` defaults verbatim (`allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: false`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault`).
   - `resources:` requests `cpu: 20m`, `memory: 20Mi`; limits `cpu: 200m`, `memory: 100Mi`.
   - `kafkaUser:` mirroring `recurringTaskCreator.kafkaUser` exactly (`enabled: false`, `cluster: my-cluster`, `strimziNamespace: strimzi`, `userName: ""` with comment "Empty = derived `<namespace>-cluster-assigner`", `clientSecret: ""`, `caCertSecret: my-cluster-cluster-ca-cert`) with an adapted copy of its comment block.

3. **Create `helm/templates/cluster-assigner-cronjob.yaml`.** Whole file gated on `{{- if .Values.clusterAssigner.enabled }}`; alias `$ca := .Values.clusterAssigner` and `$ns := include "agent.namespace" .`. Emit exactly one `batch/v1` `CronJob`:
   - `metadata`: `name: cluster-assigner`, `namespace: {{ $ns }}`, labels `app: cluster-assigner` + `agent.labels`.
   - `spec`: `schedule`, `successfulJobsHistoryLimit`, `failedJobsHistoryLimit` from values (`schedule` quoted); `concurrencyPolicy: Forbid` hard-coded in the template (NOT a value — overlapping runs must be impossible). `jobTemplate.spec.backoffLimit: 0` (hard-coded, with a one-line comment: the next scheduled run is the retry; a retrying failed pod would otherwise make `Forbid` skip the next tick). Pod template labels `app: cluster-assigner`; `restartPolicy: Never`; global `affinity` and global `image.pullSecrets` via `with`, as in `controller-statefulset.yaml`.
   - One container `name: service`; image built inline like `a2a-worker-controller-deployment.yaml` from `.Values.image.registry`, `$ca.image.repository`, `$ca.image.tag`, quoted; `imagePullPolicy: {{ .Values.image.pullPolicy }}`; `args: [-v=<logLevel>]` as in the recurring-task-creator; `securityContext` via `with $ca.securityContext`; `resources` via `toYaml`.
   - `env`, exact names, all plain values `| quote`d:
     - `KAFKA_BROKERS` ← `required "clusterAssigner.kafkaBrokers is required" $ca.kafkaBrokers`
     - `TOPIC_PREFIX` ← `topicPrefix`
     - `GIT_REST_URL` ← `required "clusterAssigner.gitRestURL is required" $ca.gitRestURL`
     - `GATEWAY_SECRET` ← `secretKeyRef` `key: gateway-secret`, `name: {{ $ca.existingSecret | default (printf "agent-task-controller-%s" $ca.vault) }}` — mirrors the controller's `GATEWAY_SECRET` block.
     - `SENTRY_DSN` ← `secretKeyRef` on the same Secret name, `key: sentry-dsn`, `optional: true` (the Sentry DSN is optional for this component; a Secret without the key must not block pod start).
     - `TASK_GLOB` ← `taskGlob`, `VAULT` ← `vault`, `DEFAULT_CLUSTER_PROD` ← `defaultClusterProd`, `DEFAULT_CLUSTER_DEV` ← `defaultClusterDev`
     - `SUN_ROUTING` ← `sunRouting`, `DRY_RUN` ← `dryRun` (bools quoted → `"false"` / `"true"`)
     - `SUN_API_URL` ← `sun.apiURL`, `SUN_NAMESPACE` ← `sun.namespace`
     - Only inside `{{- if $ca.sunRouting }}`: bind `$sunSecret := required "clusterAssigner.sun.secretName is required when clusterAssigner.sunRouting is true" $ca.sun.secretName`, then `SUN_TOKEN` ← `secretKeyRef` `{name: $sunSecret, key: sun-token}` and `SUN_CA_DATA` ← `secretKeyRef` `{name: $sunSecret, key: sun-ca-data}`.
     - Render `SENTRY_PROXY` from the fleet-wide `.Values.sentry.proxy` with `{{- with .Values.sentry.proxy }}`, using the same `with` pattern as the recurring-task-creator / executor (but reading the root key, since requirement 2 adds no per-component `sentry:` block) (a CHANGELOG entry records Sentry events lost when a component omitted it). Do NOT render `SUN_TIMEOUT`.
   - Kafka mTLS: only when `$ca.kafkaUser.enabled`, render `volumeMounts:` with `include "agent.kafkaCertVolumeMounts" .` and a pod-level `volumes:` with `include "agent.kafkaCertVolumes" (dict "clientSecret" ($ku.clientSecret | default $userName) "caCertSecret" ($ku.caCertSecret | default "my-cluster-cluster-ca-cert"))`, where `$ku := $ca.kafkaUser` and `$userName := $ku.userName | default (printf "%s-cluster-assigner" $ns)`. When `kafkaUser.enabled` is false, neither the `volumeMounts:` nor the `volumes:` key is rendered (the component needs no other volume). The CronJob nests the pod spec deeper than the StatefulSet exemplar, so the `nindent` values differ from the recurring-task-creator's: with `containers:` at column 10, the mount list items sit at column 16 and the volume list items at column 12 — confirm by rendering, not by copying the exemplar's numbers.
   - No ServiceAccount, RBAC, Service, probes, Prometheus annotations, keel or rolloutNonce annotations (a CronJob run is short-lived and talks only to Kafka and git-rest).

4. **Create `helm/templates/cluster-assigner-kafkauser.yaml`** mirroring `recurring-task-creator-kafkauser.yaml` line for line: nested `{{- if .Values.clusterAssigner.enabled }}` / `{{- if .Values.clusterAssigner.kafkaUser.enabled }}`; name `userName | default (printf "%s-cluster-assigner" (include "agent.namespace" .))` (must equal the `$userName` derived in the CronJob so the client Secret name lines up); namespace `strimziNamespace | default "strimzi"`; label `strimzi.io/cluster`; `spec.authentication.type: tls`. Keep the chart's authentication-only shape — add no `spec.authorization` block, because no KafkaUser in this chart declares ACLs and a `simple` authorization block on a cluster without an authorizer leaves the KafkaUser NotReady. Instead, state in the file's leading comment that this user writes the `<topicPrefix>-agent-task-v1-request` topic only.

5. **`helm/README.md`.** Mention the optional cluster assigner in the opening paragraph's component list next to the recurring-task-creator. Add a `### clusterAssigner (optional)` section directly after the `### recurringTaskCreator (optional)` table, with one sentence on what it does / who enables it (nuke prod only) and a table in the same style covering: `enabled`, `image.repository` / `image.tag`, `schedule` / history limits, `kafkaBrokers` (required), `topicPrefix`, `gitRestURL` (required), `existingSecret` (default = `agent-task-controller-<vault>`, keys `gateway-secret` + optional `sentry-dsn`), `vault` / `taskGlob`, `defaultClusterProd` / `defaultClusterDev`, `sunRouting` / `dryRun`, `sun.apiURL` / `sun.namespace` / `sun.secretName` (keys `sun-token` / `sun-ca-data`, required when `sunRouting`), `kafkaUser.*` (as `recurringTaskCreator.kafkaUser`, derived user name `<namespace>-cluster-assigner`), `resources` / `securityContext`.

6. **`helm/Chart.yaml`.** `version: 0.10.0` → `version: 0.11.0`. Append `- https://github.com/bborbe/agent-cluster-assigner` to `sources:`. Leave `appVersion` unchanged.

7. **`CHANGELOG.md`.** Insert a `## Unreleased` section directly above `## v0.110.0` (blank line before and after, as the file spaces headings) with one bullet:
   `- feat(helm): add optional \`clusterAssigner\` CronJob (\`bborbe/agent-cluster-assigner\` v0.1.0, every 5 min, \`concurrencyPolicy: Forbid\`) that routes every unstarted agent task in the OpenClaw vault by setting \`cluster:\` frontmatter via the controller's UpdateFrontmatterCommand on Kafka; reuses the controller's \`gateway-secret\` Secret, renders \`SUN_TOKEN\`/\`SUN_CA_DATA\` only when \`sunRouting\` is on (\`sun.secretName\` then required), optional Strimzi \`KafkaUser\` + mTLS mounts via \`clusterAssigner.kafkaUser.enabled\`; off by default (nothing renders), only nuke prod enables it; chart 0.10.0→0.11.0.`
   (In the file, use plain backticks, not the escaped form above.)

8. **Render checks — run every one yourself; they are the tests for this change.** Scratch files under `/tmp` only. Use `E="--set clusterAssigner.enabled=true --set clusterAssigner.kafkaUser.enabled=true --set clusterAssigner.kafkaBrokers=b:1 --set clusterAssigner.topicPrefix=master --set clusterAssigner.gitRestURL=http://g"` and `helm template x helm/ -f /tmp/ca-base.yaml $E` as the "enabled" render (save to `/tmp/ca-on.yaml`).
   - Disabled is a no-op: `! helm template x helm/ -f /tmp/ca-base.yaml | grep -q cluster-assigner`, and `diff <(helm template x /tmp/helm-orig -f /tmp/ca-base.yaml | grep -v 'helm.sh/chart:') <(helm template x helm/ -f /tmp/ca-base.yaml | grep -v 'helm.sh/chart:')` is empty.
   - Enabled: `grep -c '^kind: CronJob$' /tmp/ca-on.yaml` prints `1`; `grep -c '^kind: KafkaUser$' /tmp/ca-on.yaml` prints `1` (no other component enables a KafkaUser with these values); `grep -c 'name: test-cluster-assigner$' /tmp/ca-on.yaml` is at least `1`; the CronJob document contains `schedule: "*/5 * * * *"` and `concurrencyPolicy: Forbid`; `grep -A1 'name: SUN_ROUTING' /tmp/ca-on.yaml` shows `value: "false"`; `! grep -q 'SUN_TOKEN\|SUN_CA_DATA\|SUN_TIMEOUT' /tmp/ca-on.yaml`; `GATEWAY_SECRET` references Secret `agent-task-controller-openclaw` key `gateway-secret`; the three cert mounts `/client-cert`, `/client-key`, `/server-cert` and volumes with `secretName: test-cluster-assigner` and `secretName: my-cluster-cluster-ca-cert` are present.
   - All twelve always-on env names render: for each of `KAFKA_BROKERS TOPIC_PREFIX GIT_REST_URL GATEWAY_SECRET TASK_GLOB VAULT DEFAULT_CLUSTER_PROD DEFAULT_CLUSTER_DEV SUN_ROUTING DRY_RUN SUN_API_URL SUN_NAMESPACE`, `grep -q "name: $v$" /tmp/ca-on.yaml`.
   - Sun routing on: `helm template x helm/ -f /tmp/ca-base.yaml $E --set clusterAssigner.sunRouting=true --set clusterAssigner.sun.secretName=s` contains `name: SUN_TOKEN` and `name: SUN_CA_DATA`, each a `secretKeyRef` with `name: s` (keys `sun-token` / `sun-ca-data`), and `SUN_ROUTING` value `"true"`.
   - Sun routing without secret fails: `! helm template x helm/ -f /tmp/ca-base.yaml $E --set clusterAssigner.sunRouting=true >/dev/null 2>&1`, and its stderr names `clusterAssigner.sun.secretName`.
   - mTLS off: `helm template x helm/ -f /tmp/ca-base.yaml $E --set clusterAssigner.kafkaUser.enabled=false` renders the CronJob, no `kind: KafkaUser`, and no `client-cert` mount/volume.
   - Structure (PyYAML is not in the container): `helm template x helm/ -f /tmp/ca-base.yaml $E --show-only templates/cluster-assigner-cronjob.yaml > /tmp/ca-cj.yaml`, then each must succeed: `grep -qx '  concurrencyPolicy: Forbid' /tmp/ca-cj.yaml`, `grep -qx '      backoffLimit: 0' /tmp/ca-cj.yaml`, `grep -qx '          restartPolicy: Never' /tmp/ca-cj.yaml`, `grep -qx '          containers:' /tmp/ca-cj.yaml`, `grep -qx '          volumes:' /tmp/ca-cj.yaml`, `grep -qx '              volumeMounts:' /tmp/ca-cj.yaml`. `helm lint` (below) covers YAML validity.

9. Self-check: before finishing, re-run every command in `<verification>`, confirm each passes, and walk requirements 2-7 against the changed files.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- With `clusterAssigner.enabled: false` (the default) the rendered chart must be identical to today apart from the `helm.sh/chart` label (version bump).
- Do NOT touch any other component's template or values (executor, executors, controllers, agents, recurringTaskCreator, a2aWorkerController), the CRDs, `_helpers.tpl`, or `appVersion`.
- Do NOT create or edit any `values-*.yaml` overlay — cluster overlays live in another repo.
- Do NOT create a Secret for this component; `GATEWAY_SECRET` mirrors the controller's `gateway-secret` `secretKeyRef` pattern, and sun credentials come from the pre-existing `sun.secretName` Secret.
- `SUN_TOKEN` / `SUN_CA_DATA` render only when `sunRouting` is true; `SUN_TIMEOUT` is never rendered; `SENTRY_PROXY` renders only when `sentry.proxy` is set.
- Add no values beyond those listed in requirement 2 (no service account, probes, keel, extra tunables).
- Do NOT edit Go code; this change is chart-only plus `CHANGELOG.md`.
- All paths in this prompt are repo-relative; scratch files go in `/tmp` only, never inside the repo.
</constraints>

<verification>
Precondition — the container image does not ship helm; install it from the Go proxy (repo pattern, see `prompts/completed/227-render-per-agent-job-deadline.md`; `get.helm.sh` is 403 from the build network). A failure here means the prompt FAILED (report it as failed in the final summary; never skip the render checks or claim they passed):
`command -v helm >/dev/null 2>&1 || { go install helm.sh/helm/v3/cmd/helm@v3.16.4; export PATH="$PATH:$(go env GOPATH)/bin"; }`
`helm version --short | grep -q '^v3\.16' || { echo "PRECONDITION FAILED: helm unavailable in container"; exit 1; }`
Each new shell must re-export `PATH="$PATH:$(go env GOPATH)/bin"` (or call `$(go env GOPATH)/bin/helm`) — the export does not persist across separate command invocations. Run all render commands under bash (`$E` relies on word-splitting).

Render checks (requirement 8), in particular:
- `! helm template x helm/ -f /tmp/ca-base.yaml | grep -q cluster-assigner`
- `diff <(helm template x /tmp/helm-orig -f /tmp/ca-base.yaml | grep -v 'helm.sh/chart:') <(helm template x helm/ -f /tmp/ca-base.yaml | grep -v 'helm.sh/chart:')` — empty
- `helm template x helm/ -f /tmp/ca-base.yaml --set clusterAssigner.enabled=true --set clusterAssigner.kafkaUser.enabled=true --set clusterAssigner.kafkaBrokers=b:1 --set clusterAssigner.topicPrefix=master --set clusterAssigner.gitRestURL=http://g > /tmp/ca-on.yaml` — exits 0
- `grep -c '^kind: CronJob$' /tmp/ca-on.yaml` prints `1`; `grep -c '^kind: KafkaUser$' /tmp/ca-on.yaml` prints `1`
- `grep -q 'schedule: "\*/5 \* \* \* \*"' /tmp/ca-on.yaml && grep -q 'concurrencyPolicy: Forbid' /tmp/ca-on.yaml`
- `helm template x helm/ -f /tmp/ca-base.yaml $E --show-only templates/cluster-assigner-cronjob.yaml > /tmp/ca-cj.yaml && grep -qx '  concurrencyPolicy: Forbid' /tmp/ca-cj.yaml && grep -qx '      backoffLimit: 0' /tmp/ca-cj.yaml && grep -qx '          restartPolicy: Never' /tmp/ca-cj.yaml && grep -qx '          containers:' /tmp/ca-cj.yaml && grep -qx '          volumes:' /tmp/ca-cj.yaml && grep -qx '              volumeMounts:' /tmp/ca-cj.yaml`
- `grep -A1 'name: SUN_ROUTING' /tmp/ca-on.yaml | grep -q 'value: "false"'`
- `! grep -q 'SUN_TOKEN\|SUN_CA_DATA\|SUN_TIMEOUT' /tmp/ca-on.yaml`
- the same render plus `--set clusterAssigner.sunRouting=true --set clusterAssigner.sun.secretName=s` contains `name: SUN_TOKEN` and `name: SUN_CA_DATA` (secretKeyRefs to `s`)
- the same render plus only `--set clusterAssigner.sunRouting=true` exits non-zero mentioning `clusterAssigner.sun.secretName`

`helm lint helm/` and `helm lint helm/ -f /tmp/ca-base.yaml --set clusterAssigner.enabled=true --set clusterAssigner.kafkaBrokers=b:1 --set clusterAssigner.gitRestURL=http://g` — both report `0 chart(s) failed`.

`grep -q '^version: 0.11.0$' helm/Chart.yaml`

`awk '/^## /{sec=$0} /feat\(helm\): add optional .clusterAssigner. CronJob/{print sec}' CHANGELOG.md` prints `## Unreleased`

`ROOTDIR=/workspace make precommit` — must pass (it does not cover the chart; it guards that nothing else regressed).
</verification>
