---
status: completed
spec: [050-executors-per-vault-list]
summary: Added the `executors` values list to helm/values.yaml with a documented two-entry example block (per-entry fields falling back to executor.*, no per-entry kafkaUser), establishing the values schema contract; default render verified byte-identical to pre-change, helm lint clean, and CHANGELOG.md gained an Unreleased entry.
execution_id: agent-exec-210-spec-050-executors-values-schema
dark-factory-version: dev
created: "2026-08-27T10:55:00Z"
queued: "2026-08-27T11:29:20Z"
started: "2026-08-27T11:29:22Z"
completed: "2026-08-27T11:32:27Z"
branch: dark-factory/executors-per-vault-list
---

<summary>
- The chart gains an `executors` values list (default `[]`) that lets an operator declare one executor per vault purely from values
- Each entry carries `name`, `enabled`, `vaultName`, `branch`, `topicPrefix`, `kafkaBrokers`, and optional overrides for `image`, `logLevel`, `sentry.proxy`, `existingSecret`, `podSecurityContext`, `securityContext`, `resources`
- Every optional field missing from an entry falls back to the existing `executor.*` values block, so a minimal entry is just `name`, `vaultName`, `topicPrefix`, `kafkaBrokers`
- The `executor.*` block is untouched and becomes the defaults source — no existing field is renamed or removed
- A documented, fully-commented example block (two entries, mirroring the `controllers` example shape) shows operators exactly what a per-vault entry looks like
- Default values render identically to today — the empty list changes nothing at render time
</summary>

<objective>
Add the `executors` values list to `helm/values.yaml` with a documented example block, establishing the values contract that the follow-on template prompt reads. This is prompt 1 of 3 — it establishes the schema only; no template change happens here, so `helm template` output must be byte-identical to before.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (multi-service mono-repo, dark-factory workflow, helm chart at `helm/`).

Read these files IN FULL before editing:
- `/workspace/helm/values.yaml` — the file you are editing. The `executor:` block ends at the `kafkaUser.caCertSecret: my-cluster-cluster-ca-cert` line (line ~98) and the `controllers:` list starts at line ~107. The `controllers` example block (lines ~100-212) is the shape to mirror for the new `executors` block: a section header comment, a `controllers: []` default, then a fully commented-out two-entry example with per-field inline comments.
- `/workspace/helm/templates/controller-statefulset.yaml` — the template that reads the `controllers` list; it shows which per-entry fields the template actually consumes, so the values example documents real fields only.

No coding-plugin doc is required for this prompt (pure YAML values schema). The chart has no per-vault precedent; the `controllers` list is the in-repo exemplar for the list shape.
</context>

<requirements>

## 1. Insert the `executors` list into `/workspace/helm/values.yaml`

Insert a new top-level `executors` value between the end of the `executor:` block (the `kafkaUser.caCertSecret` line, ~98) and the `controllers:` header comment (line ~100). The block must have:

- A section header comment describing the list, mirroring the tone of the `controllers:` header comment (lines 100-106). Content: values-driven LIST of Deployments, each serving exactly one vault; each enabled entry emits an `agent-task-executor-<name>` Deployment (single replica = one consumer group / offset domain / dedup state per vault); empty by default so a generic install renders the single legacy executor unchanged.
- The default declaration `executors: []`.
- A fully commented-out example (every line prefixed with `# `) with TWO entries (`openclaw` + `personal`), mirroring the `controllers` example shape. Every per-entry field below appears with an inline comment stating what it does and what it falls back to.

## 2. Per-entry fields in the example block

Each entry in the example documents exactly these fields (no more — the spec Non-goals forbid extra per-entry fields):

- `name` — instance name; becomes the Deployment suffix `agent-task-executor-<name>` and the pod label `vault: <name>`. Required. No chart-side validation; two entries sharing a `name` render two same-named manifests (last-write-wins on apply — same property as `controllers[]`).
- `enabled` — default `true`; `false` skips the entry.
- `vaultName` — REQUIRED; becomes the `VAULT_NAME` env. Empty is rejected at render time. The slug pattern `^[a-z][a-z0-9-]*$` is validated by the executor binary (>= v0.7.0) at startup, NOT by the chart — say so in the comment.
- `branch` — default `executor.branch`.
- `topicPrefix` — default `executor.topicPrefix`. Passes through verbatim; the chart does NOT derive `{stage}-{vault}`. Comment must say per-vault isolation requires setting this to `{stage}-{vault}` so the executor consumes `{prefix}-agent-task-v1-event` and publishes `{prefix}-agent-task-v1-request`.
- `kafkaBrokers` — required when not inherited; default `executor.kafkaBrokers`. Use `tls://…:9093` with the shared `executor.kafkaUser` block.
- `image.repository` / `image.tag` — default `executor.image` (empty tag => `Chart.appVersion`).
- `logLevel` — default `executor.logLevel`.
- `sentry.proxy` — default `executor.sentry.proxy`. NOTE: `sentry.dsn` is NOT a per-entry field — the chart never creates per-vault Secrets; `SENTRY_DSN` is always read from a Secret (see `existingSecret`).
- `existingSecret` — default `executor.existingSecret`, then the shared `agent-task-executor` Secret (key `sentry-dsn`).
- `podSecurityContext` — default `executor.podSecurityContext`.
- `securityContext` — default `executor.securityContext`.
- `resources` — default `executor.resources`.

Close the example with a comment stating there is NO per-entry `kafkaUser` — executor mTLS is driven by the single shared `executor.kafkaUser` block for all per-vault executors (spec Non-goal; per-vault Kafka credentials are a future separate spec).

## 3. Do NOT modify anything else in the file

- The `executor.*` block stays byte-identical — every field name and default is preserved; it becomes the defaults source for per-entry optional fields.
- Do NOT touch the `controllers`, `agents`, `recurringTaskCreator`, or any other block.
- Do NOT add any other new top-level value.

## 4. Self-check

Before finishing, re-run the `<verification>` commands below and confirm each passes; walk each acceptance criterion in `/workspace/specs/in-progress/050-executors-per-vault-list.md` that this prompt owns (Desired Behavior 1 only — schema).
</requirements>

<constraints>
- This spec changes exactly five files: `helm/templates/executor-deployment.yaml`, `helm/values.yaml`, `helm/README.md`, `helm/Chart.yaml`, `CHANGELOG.md`. This prompt changes ONLY `helm/values.yaml`.
- The legacy `executor.*` values block remains and keeps its exact field names; nothing in it is removed or renamed.
- Do NOT add per-entry `kafkaUser` (or `storage`, `gitRestUrl`, `taskDir`, `pollInterval`, `autoInjectTaskIdentifier`, `gatewaySecret`) — controller-only or not yet needed (spec Non-goal).
- Do NOT add chart-side regex validation of `vaultName` — slug validation is the executor binary's runtime job.
- Do NOT synthesize `{stage}-{vault}` as a topic prefix — `TOPIC_PREFIX` passes through the entry value verbatim.
- No Go code changes; `make precommit` is NOT required for this prompt (no Go file changes — run the helm-based verification below instead).
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

# A bare `helm template helm/` exits 1 (namespace/kafkaBrokers/existingSecret are
# required by the chart) — always render with these values set.
RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'

# The executors default exists in values.yaml.
grep -n '^executors:' helm/values.yaml
# Must return at least one line.

# Default render is byte-equivalent to pre-change: exactly one legacy Deployment,
# no VAULT_NAME anywhere.
eval "$RENDER" | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'
# Must print 1.
eval "$RENDER" | grep -c 'name: VAULT_NAME'
# Must print 0.

# The example block must stay fully commented so the list remains empty (the
# byte-identical default render depends on it):
grep -c '^  # - name:' helm/values.yaml
# Must print 2.

# Chart still lints clean with default values.
helm lint helm/
# Must exit 0 with "0 chart(s) failed".
```
</verification>
