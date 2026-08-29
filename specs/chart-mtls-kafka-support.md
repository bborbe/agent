---
status: draft
---

## Summary

- The `agent` Helm chart wires only plaintext Kafka for the executor and controllers; only recurring-task-creator can emit a Strimzi `KafkaUser`, and even that never mounts the resulting client cert.
- The company Octopus install needs mTLS Kafka (`type: tls` KafkaUser + client cert/key + cluster CA mounted at fixed paths) per stage cluster; today that forces an Octopus-only post-render patch, defeating the reusable-chart design.
- This spec adds an opt-in `kafkaUser` values block to the executor and to each controller (mirroring recurring's existing block, plus secret-name fields), and mounts three cert volumes at fixed paths when enabled.
- Recurring-task-creator gains the cert mounts it currently lacks (its KafkaUser CR template already exists, unchanged).
- Default is OFF: plaintext installs (quant) render byte-identical to today. This is a chart-only change — no Go code.

## Problem

The `bborbe/agent` chart supports only PLAINTEXT Kafka for the executor and controllers: their Deployment/StatefulSet templates set only the `KAFKA_BROKERS` env, no TLS cert volumes. Only recurring-task-creator has a `kafkaUser.enabled` flag, and it emits solely the Strimzi `KafkaUser` custom resource — it never mounts the issued client cert into the pod. That is fine for quant (a single plaintext Strimzi cluster on `:9092`), but it blocks the company Octopus install, whose per-stage Strimzi clusters require mTLS on `:9093`: a `KafkaUser` of `type: tls` plus the client cert/key and cluster CA mounted into each pod at the fixed paths the `github.com/bborbe/kafka` library auto-detects (`/client-cert/file`, `/client-key/file`, `/server-cert/file`). Without chart support, Octopus must post-render-patch every workload, defeating the point of a reusable chart. The chart must support BOTH plaintext (default, quant unchanged) AND mTLS (opt-in, Octopus).

## Goal

The chart supports mTLS Kafka as an opt-in, per-component feature. When a component's `kafkaUser.enabled` is `true`, the chart both emits a Strimzi `KafkaUser` (`type: tls`) in the Strimzi operator's namespace AND mounts the client cert, client key, and cluster CA into that component's pod at the three fixed paths the kafka library reads. When `kafkaUser.enabled` is `false` or absent (the default), the chart emits no KafkaUser and no cert volumes, and the rendered manifests are identical to the current chart output. The feature is available on the executor, on every entry in the `controllers[]` list, and on recurring-task-creator (which currently emits the CR but not the mounts). The chart never rewrites `KAFKA_BROKERS`; the operator sets the `tls://` scheme in values.

## Non-goals

- No changes to the `maintainer` chart (watchers) — that is a separate spec using the same pattern.
- No changes to the `seibert-data/agent` values files that consume this chart.
- No cross-namespace secret syncing — the chart references the client and CA secrets by name only; Strimzi issues the user secret and an external secret-syncer copies it plus the CA into the app namespace. The chart assumes they exist.
- No executor/controller/recurring Go code — mTLS is entirely a mount concern; the kafka library already auto-detects `tls://` when the certs are present.
- No change to `KAFKA_BROKERS` scheme rewriting — the flag controls the cert mount and the CR only, never the brokers value.
- No change to any topic/prefix or other non-Kafka behavior.
- Do NOT add a chart-managed copy/sync of the client or CA secrets — invariant; the chart references by name only. If a future consumer needs the chart to create those secrets, that is a separate spec.

## Acceptance Criteria

- [ ] With default values (`kafkaUser.enabled` absent/false for every component), `helm template helm/` renders NO object of `kind: KafkaUser` for the executor or any controller, and NO volume or volumeMount named `client-cert`, `client-key`, or `server-cert` on any executor/controller/recurring pod — evidence: `helm template helm/ | grep -c 'kind: KafkaUser'` returns `0`; `helm template helm/ | grep -cE 'name: (client-cert|client-key|server-cert)'` returns `0`.
- [ ] The default-values render is byte-identical to the pre-change chart for the Kafka-cert concern — evidence: `helm template helm/ > /tmp/after.yaml`, compared against the same command on `HEAD` (`git stash` or worktree), `diff /tmp/before.yaml /tmp/after.yaml` is empty.
- [ ] With `executor.kafkaUser.enabled=true`, `helm template` renders exactly one `kind: KafkaUser` named `<namespace>-agent-task-executor` (or the `userName` override) in namespace `strimzi` (or `strimziNamespace` override) with `spec.authentication.type: tls` and label `strimzi.io/cluster: my-cluster` (or `cluster` override) — evidence: `helm template helm/ --set executor.kafkaUser.enabled=true | grep -A8 'kind: KafkaUser'` shows the name, namespace, cluster label, and `type: tls`.
- [ ] With `executor.kafkaUser.enabled=true`, the executor Deployment pod spec has three secret volumes (`client-cert`, `client-key`, `server-cert`, each `defaultMode: 420`) and three volumeMounts at `/client-cert`, `/client-key`, `/server-cert`, with `items` mapping `user.crt`/`user.key`/`ca.crt` respectively to path `file` — evidence: `helm template helm/ --set executor.kafkaUser.enabled=true` output matches the shape of `~/Documents/workspaces/sm-octopus/agent/task/executor/agent-task-executor-deploy.yaml` lines 89-119 (`grep -A2 'mountPath: /client-cert'` and the three `secret:` blocks with `key: user.crt path: file` etc.).
- [ ] With a controller entry setting `kafkaUser.enabled: true`, `helm template` renders one `kind: KafkaUser` named `<namespace>-agent-task-controller-<name>` in `strimzi` and the same three cert volumes/mounts on that controller's StatefulSet pod — evidence: `helm template helm/ -f <controller-with-kafkauser-values>.yaml | grep -A8 'kind: KafkaUser'` and the three mount paths appear once per enabled controller.
- [ ] With `recurringTaskCreator.kafkaUser.enabled=true`, the recurring StatefulSet pod gains the three cert volumes/mounts (in addition to the existing `tmp` emptyDir), and the pre-existing recurring KafkaUser CR still renders unchanged — evidence: `helm template helm/ --set recurringTaskCreator.enabled=true --set recurringTaskCreator.kafkaUser.enabled=true | grep -E 'name: (client-cert|client-key|server-cert|tmp)'` lists all four volumes; the recurring KafkaUser CR is present.
- [ ] `helm lint helm/` passes clean both with default values and with `executor.kafkaUser.enabled=true` — evidence: `helm lint helm/` exits 0 (`0 chart(s) failed`).
- [ ] `helm/README.md` values reference documents the new `executor.kafkaUser.*` and `controllers[].kafkaUser.*` keys (`enabled`, `cluster`, `strimziNamespace`, `clientSecret`, `caCertSecret`, `userName`) with defaults — evidence: `grep -c 'kafkaUser' helm/README.md` increases; `grep -n 'executor.kafkaUser.enabled' helm/README.md` returns ≥1.
- [ ] `CHANGELOG.md` has an `## Unreleased` (or new top) entry describing the mTLS cert-mount addition and the chart version minor bump, and `helm/Chart.yaml` `version` is bumped by a minor (0.3.1 → 0.4.0), `appVersion` unchanged unless the operator states otherwise — evidence: `grep -n 'kafkaUser\|mTLS\|cert' CHANGELOG.md` returns the new entry; `grep 'version:' helm/Chart.yaml` shows `0.4.0`.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

```
make test
# Targeted asserts that the change landed (helm may be absent in-container):
grep -n 'kafkaUser.enabled' helm/templates/executor-deployment.yaml
grep -n 'client-cert' helm/templates/executor-deployment.yaml
grep -n 'kind: KafkaUser' helm/templates/executor-kafkauser.yaml helm/templates/controller-kafkauser.yaml
grep -n 'executor.kafkaUser' helm/README.md
grep 'version:' helm/Chart.yaml
```

### Operator-executable (runs on the host — requires the `helm` CLI)

```
# Default render is unchanged (byte diff on the Kafka-cert concern):
git stash && helm template helm/ > /tmp/before.yaml && git stash pop
helm template helm/ > /tmp/after.yaml
diff /tmp/before.yaml /tmp/after.yaml            # empty

# Feature ON renders CR + three mounts:
helm template helm/ --set executor.kafkaUser.enabled=true | grep -A8 'kind: KafkaUser'
helm template helm/ --set executor.kafkaUser.enabled=true | grep -E 'mountPath: /(client-cert|client-key|server-cert)'

helm lint helm/
```

## Desired Behavior

1. The per-component `kafkaUser` values block is extended and added to `executor` and to each `controllers[]` entry (today it exists only on `recurringTaskCreator`). Fields: `enabled` (default `false`), `cluster` (default `my-cluster`), `strimziNamespace` (default `strimzi`), plus three new fields on all three components — `userName` (override for the KafkaUser CR name; default derived per component), `clientSecret` (name of the k8s Secret holding `user.crt`/`user.key`; default derived from the KafkaUser name), and `caCertSecret` (name of the cluster CA secret holding `ca.crt`; default `my-cluster-cluster-ca-cert`).
2. A new template emits a Strimzi `KafkaUser` for the executor when `executor.kafkaUser.enabled` is `true`, mirroring the existing recurring-task-creator KafkaUser template exactly (`apiVersion: kafka.strimzi.io/v1beta2`, `kind: KafkaUser`, `spec.authentication.type: tls`, label `strimzi.io/cluster: <cluster>`, in `strimziNamespace`). Name = `userName` or derived `<namespace>-agent-task-executor`.
3. A new template iterates `controllers[]` and emits one Strimzi `KafkaUser` per entry whose `kafkaUser.enabled` is `true`. Name = `userName` or derived `<namespace>-agent-task-controller-<name>`.
4. The executor Deployment, when `executor.kafkaUser.enabled` is `true`, mounts three secret volumes (`defaultMode: 420`) at fixed paths: `/client-cert/file` from `clientSecret` key `user.crt` → path `file`; `/client-key/file` from `clientSecret` key `user.key` → path `file`; `/server-cert/file` from `caCertSecret` key `ca.crt` → path `file`.
5. Each controller StatefulSet, when its `kafkaUser.enabled` is `true`, mounts the same three secret volumes at the same fixed paths, using that controller's `clientSecret`/`caCertSecret`.
6. The recurring-task-creator StatefulSet, when its `kafkaUser.enabled` is `true`, mounts the same three secret volumes (it currently mounts only the `tmp` emptyDir); its existing KafkaUser CR template is unchanged.
7. All cert volumes, mounts, and KafkaUser CRs are conditional on `enabled`, so plaintext installs (default, quant) render byte-identical to the current chart. `helm/README.md` documents the new keys and `CHANGELOG.md` + `helm/Chart.yaml` record an additive minor version bump.

## Constraints

- Quant renders unchanged. The hard requirement: with default values, the rendered output for the Kafka-cert concern is byte-identical to the current chart (no KafkaUser, no cert volumes/mounts). This is the load-bearing backward-compat invariant.
- The existing `recurring-task-creator-kafkauser.yaml` template and the `recurringTaskCreator.kafkaUser` block's existing fields (`enabled`, `cluster`, `strimziNamespace`) keep their current behavior; only the new fields and the cert mount are additive.
- Fixed cert paths `/client-cert/file`, `/client-key/file`, `/server-cert/file` are frozen — the `github.com/bborbe/kafka` library reads exactly these. Volume item `path` is `file`; secret keys are `user.crt`, `user.key`, `ca.crt`. These match the Octopus reference manifest `~/Documents/workspaces/sm-octopus/agent/task/executor/agent-task-executor-deploy.yaml` and MUST NOT be renamed.
- The chart does not create or sync the client or CA secrets; it references them by name. It does not rewrite `KAFKA_BROKERS`.
- Version bump is additive (minor): the change is backward-compatible; no BREAKING marker.
- Repo is dark-factory `workflow: direct` (commits land on master in-container, no PR gate); `autoRelease` is driven by `.maintainer.yaml` (maintainer bot cuts the tag from the CHANGELOG). Do not assume a PR review gate.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection |
|---------|-------------------|----------|-----------|
| `kafkaUser.enabled: true` but the referenced `clientSecret`/`caCertSecret` does not exist in the app namespace | Chart renders the mounts (it only references by name); the pod fails to start (volume mount references missing secret) | Operator creates/syncs the secret (Strimzi user secret + CA), then pod starts | `kubectl describe pod` shows `MountVolume.SetUp failed ... secret "<name>" not found` |
| Strimzi runs in a namespace other than the default `strimzi` and `strimziNamespace` is not overridden | KafkaUser CR is created in the wrong namespace; Strimzi never issues the cert; mTLS auth fails silently | Operator sets `kafkaUser.strimziNamespace` to the real Strimzi namespace and re-applies | `kubectl get kafkausers -n <real-strimzi-ns>` does not list the user; broker rejects the client |
| `enabled: true` on a plaintext cluster (`KAFKA_BROKERS` not `tls://`) | Certs mount but the kafka library stays plaintext (scheme-driven); no functional change beyond an unused mount | Operator either sets `tls://` brokers or disables `kafkaUser` | Kafka connects plaintext; certs unused (no error) |
| `userName`/`clientSecret` collide across two controllers on the same cluster | Two KafkaUser CRs / mounts reference the same name; last-writer-wins on the shared secret | Operator gives each controller a distinct `userName`/`clientSecret` | Two StatefulSets mount the same secret; per-component derived defaults (`<ns>-agent-task-controller-<name>`) avoid this unless overridden to collide |
| Chart version not bumped | Release tooling (`autoRelease` from CHANGELOG) has no new version to cut | Operator adds the CHANGELOG entry + bumps `Chart.yaml` `version` | `git diff helm/Chart.yaml` shows no version change; CHANGELOG lacks a new entry |

## Suggested Decomposition

Single code layer (Helm templates), but the components are independent seams. Generate prompts in this order:

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Extend the `kafkaUser` values block (add `userName`/`clientSecret`/`caCertSecret`; add block to `executor` + `controllers[]` docs in `values.yaml`) + README values reference + CHANGELOG + Chart.yaml minor bump | 1, 7 | 8, 9 | — |
| 2 | Executor: new `executor-kafkauser.yaml` CR template + cert mounts in `executor-deployment.yaml` (conditional on `enabled`) | 2, 4 | 3, 4 | prompt 1 |
| 3 | Controllers: new `controller-kafkauser.yaml` (ranged) CR template + cert mounts in `controller-statefulset.yaml` per entry | 3, 5 | 5 | prompt 1 |
| 4 | Recurring: add the three cert mounts to `recurring-task-creator-statefulset.yaml` (CR template unchanged) | 6 | 6 | prompt 1 |
| 5 | Default-off invariant + lint verification (byte-diff render, `helm lint` both ways) | 7 | 1, 2, 7 | prompts 2, 3, 4 |

Rationale: prompt 1 establishes the values contract + docs that all component templates read; prompts 2/3/4 are independent per-component template edits that can run in any order after 1; prompt 5 is the final invariant/lint gate that needs all mounts in place. No cycles — every component depends only on the values block.

## Do-Nothing Option

If not done, the Octopus install must post-render-patch every executor/controller/recurring workload to inject the KafkaUser and cert mounts, forking the manifests away from the shared chart. That defeats the reusable-chart design and means every future chart change must be re-reconciled by hand in Octopus. The current chart stays fine for quant (plaintext) but permanently blocks the company mTLS install from consuming the chart as-is. Not acceptable if Octopus is to adopt the chart.
