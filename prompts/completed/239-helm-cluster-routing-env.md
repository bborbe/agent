---
status: completed
summary: Added optional cluster-routing values (cluster, publishUntagged, consumerGroup) to the Helm chart, templates, README and changelog; chart bumped 0.7.0→0.8.0 with default renders unchanged.
execution_id: agent-helm-cluster-exec-239-helm-cluster-routing-env
dark-factory-version: v0.198.7
created: "2026-10-20T22:00:00Z"
queued: "2026-10-20T21:30:09Z"
started: "2026-10-20T21:30:15Z"
completed: "2026-10-20T21:36:16Z"
---

# feat(helm): render CLUSTER, PUBLISH_UNTAGGED and CONSUMER_GROUP env for cluster-scoped task routing

<summary>
- The chart can name the cluster it is installed on, so each cluster's controllers and executors know which tasks are theirs
- Controllers can be told to publish only tasks explicitly tagged for their cluster, which a cluster such as sun needs to avoid picking up untagged work
- Each executor can be given its own Kafka consumer group, so several executors on different clusters do not share offsets
- All three settings are optional; an install that sets none of them renders exactly the manifests it renders today
- Setting the untagged-publish switch to `false` is honoured, not mistaken for "unset"
- The chart's value reference and the README document what each setting does and when to use it
- The chart version is bumped and the change is recorded in the changelog
</summary>

<objective>
Plumb the optional cluster-routing environment variables that `agent-task-executor` (v0.19+) and `agent-task-controller` (v0.14.0+) now read, through the Helm chart in `helm/`. This lets one chart install on several clusters (nukedev, nukeprod, sun) while each cluster's controllers publish, and its executors consume, only the tasks addressed to it. Unset values must leave every rendered manifest byte-identical (apart from the chart-version label) to today.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single global `CHANGELOG.md`, no per-module changelog).

Read these files before editing; anchor on the names below, not line numbers:
- `helm/templates/executor-deployment.yaml` — two Deployments in one file. The first is guarded by `{{- if and .Values.executor.enabled (empty .Values.executors) }}` (singular executor, uses `.Values`). The second is a `range` over `.Values.executors` with `$e` as the entry and `$.Values` for root values. In each, the `containers[0].env:` block ends with the `TASK_GLOB` entry (`- name: TASK_GLOB`). Existing optional-env idiom: `{{- with ... }}` around `- name: SENTRY_PROXY`.
- `helm/templates/controller-statefulset.yaml` — `range $c := .Values.controllers` with `$c.enabled` guard; the `env:` block starts at `- name: LISTEN` and ends with the `NAMESPACE` fieldRef entry. Root values are reached via `$.Values`.
- `helm/values.yaml` — top-level `sentry:` block (documents a root-level, optional, fleet-wide value with a comment block); `executor:` block (note the `taskGlob: ""` entry with its comment); the commented `executors: []` example with per-entry fields and the `# - name: openclaw` example entry.
- `helm/README.md` — the `### Global` table, the `### executor` table (`executor.taskGlob` row), and the `### executors (per-vault, values-driven)` table (`executors[].*` rows).
- `helm/Chart.yaml` — `version: 0.7.0`.
- `CHANGELOG.md` — top entry is `## v0.106.0`; there is currently no `## Unreleased` section. Existing helm entries use the form `feat(helm): ... chart 0.6.8→0.6.9.`

Note: `helm.sh/chart` label in `_helpers.tpl` (`agent.labels`) renders `agent-<Chart.Version>`, so bumping the chart version changes that label on every resource. That is the only permitted difference against today's default render.
</context>

<requirements>
1. **`helm/values.yaml` — new top-level values.** Add, near the existing top-level `sentry:` block (after it, before `executor:`), with comments:
   - `cluster: ""` — comment: names the cluster this release is installed on (e.g. `nukedev`, `nukeprod`, `sun`). When non-empty it is rendered as env `CLUSTER` on every executor and every controller. Controllers publish only tasks whose `cluster:` frontmatter matches this value. Requires agent-task-controller >= v0.14.0 and agent-task-executor >= v0.19.0; empty (default) renders nothing.
   - `# publishUntagged: false` — **commented out** (unset by default, a key must not exist in the defaults so "unset" is distinguishable from `false`). Comment: rendered as env `PUBLISH_UNTAGGED` (quoted string `"true"`/`"false"`) on every controller only when the key is set; set it to `false` on clusters that must run only explicitly tagged tasks (e.g. sun) so tasks without a `cluster:` frontmatter are not published there. Unset = nothing rendered (controller's own default applies). Requires agent-task-controller >= v0.14.0.
2. **`helm/values.yaml` — consumer group.** In the `executor:` block, directly after `taskGlob: ""` and its comment, add `consumerGroup: ""` with a comment: rendered as env `CONSUMER_GROUP` on the executor when non-empty; gives each Kafka consumer its own consumer group (e.g. `agent-task-executor-sun`) so executors on different clusters do not share offsets; empty = nothing rendered. In the commented `executors:` example, document the per-entry override `consumerGroup: ""  # default executor.consumerGroup` next to the other per-entry fields (add it to both example entries only where the fields are listed, matching their style).
3. **`helm/templates/executor-deployment.yaml` — singular executor Deployment.** After the `TASK_GLOB` env entry in the `{{- if and .Values.executor.enabled (empty .Values.executors) }}` Deployment, render, each guarded so an empty value renders nothing and leaves no blank/stray line:
   - `CLUSTER` with `{{ . | quote }}` using `{{- with .Values.cluster }}`.
   - `CONSUMER_GROUP` with `{{- with .Values.executor.consumerGroup }}`.
   Keep the exact 12-space indentation of the neighbouring `- name:` entries.
4. **`helm/templates/executor-deployment.yaml` — `executors[]` Deployments.** After the `TASK_GLOB` env entry inside the `range` Deployment, render:
   - `CLUSTER` using `{{- with $.Values.cluster }}` (the list entries have no per-entry `cluster`; root value only).
   - `CONSUMER_GROUP` using `{{- with ($e.consumerGroup | default $.Values.executor.consumerGroup) }}` — per-entry value wins, falls back to `executor.consumerGroup`, empty renders nothing.
5. **`helm/templates/controller-statefulset.yaml`.** After the `AUTO_INJECT_TASK_IDENTIFIER` env entry and before the `NAMESPACE` fieldRef entry (or after `NAMESPACE`; keep it inside the `env:` list either way), render for every enabled controller:
   - `CLUSTER` using `{{- with $.Values.cluster }}` and `{{ . | quote }}`.
   - `PUBLISH_UNTAGGED` using a presence test, NOT truthiness: `{{- if and (hasKey $.Values "publishUntagged") (kindIs "bool" $.Values.publishUntagged) }}` ... `value: {{ $.Values.publishUntagged | quote }}`. Result: `true` renders `"true"`, `false` renders `"false"`, key absent renders nothing. A non-bool value (e.g. a string passed via `--set-string`) renders nothing; do not add extra handling for it.
6. **`helm/README.md`.** Document the new values with the same table style as neighbouring rows:
   - `### Global` table: rows for `cluster` (default `""`) and `publishUntagged` (default unset; renders only when set, `false` is honoured).
   - `### executor` table: row for `executor.consumerGroup` (default `""`).
   - `### executors` table: row for `executors[].consumerGroup` (default `executor.consumerGroup`).
   Each description states which env var it renders, on which workloads, and the minimum component version from requirement 1.
7. **`helm/Chart.yaml`.** Bump `version: 0.7.0` to `version: 0.8.0` (minor: new optional values, backwards-compatible). Leave `appVersion` unchanged.
8. **`CHANGELOG.md`.** Insert a `## Unreleased` section directly above `## v0.106.0` (blank line before and after, matching the file's heading spacing) containing one bullet in the existing helm style:
   `- feat(helm): render optional cluster-routing env — \`cluster\` → \`CLUSTER\` on the singular executor, every \`executors[]\` entry and every controller; \`publishUntagged\` → \`PUBLISH_UNTAGGED\` on every controller (rendered whenever the key is set, so \`false\` renders); \`executor.consumerGroup\` / \`executors[].consumerGroup\` → \`CONSUMER_GROUP\` on that executor; chart 0.7.0→0.8.0. All three are absent by default, so an install that sets none of them renders the same manifests as before (only the \`helm.sh/chart\` label changes). Consumed by agent-task-controller >= v0.14.0 and agent-task-executor >= v0.19.0; older binaries ignore the env.` (Write it as plain markdown; the backslashes above only escape the backticks for this prompt file — the bullet in the changelog must contain normal backticks.)
9. **Render checks (run these yourself; this is the test for the template changes).** Create scratch files under `/tmp` only, never inside the repo. Create `/tmp/helm-cluster-values.yaml`:
   ```yaml
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     vaultName: personal
   controllers:
     - name: personal
       vaultName: personal
       kafkaBrokers: kafka:9092
       resources: {}
   ```
   and `/tmp/helm-cluster-values-list.yaml` identical but adding top-level `executors:` with two entries — `- {name: personal, vaultName: personal}` and `- {name: other, vaultName: other, consumerGroup: per-entry-group}`.
   - Default render: `helm template t helm -f /tmp/helm-cluster-values.yaml > /tmp/helm-default.yaml` and the same with `-f /tmp/helm-cluster-values-list.yaml > /tmp/helm-default-list.yaml`; assert none of `CLUSTER`, `PUBLISH_UNTAGGED`, `CONSUMER_GROUP` appear (`! grep -q`).
   - Enabled render: `helm template t helm -f /tmp/helm-cluster-values.yaml --set cluster=sun --set publishUntagged=false --set executor.consumerGroup=agent-task-executor-sun > /tmp/helm-sun.yaml`. Assert `CLUSTER` + value `"sun"` appear on both the `agent-task-executor` Deployment and the `agent-task-controller-personal` StatefulSet, `PUBLISH_UNTAGGED` + `"false"` appears only on the StatefulSet, and `CONSUMER_GROUP` + `"agent-task-executor-sun"` appears only on the Deployment. Prefer checking per-document (split on `---`, e.g. with `awk`) so a match on the wrong resource fails.
   - List render: same flags with `-f /tmp/helm-cluster-values-list.yaml`; assert `CLUSTER` `"sun"` on both `agent-task-executor-personal` and `agent-task-executor-other`, `CONSUMER_GROUP` `"agent-task-executor-sun"` on `-personal` (inherited) and `"per-entry-group"` on `-other`.
   - `--set publishUntagged=true` renders `"true"` on the StatefulSet.
   - Unchanged-by-default: render the pre-change chart (from a copy of the original `helm/templates`, `helm/values.yaml`, `helm/Chart.yaml` taken **before** you edit anything, stored under `/tmp`) with the same default values and confirm `diff` between old and new default renders shows only `helm.sh/chart:` label lines (filter them with `grep -v 'helm.sh/chart:'` on both sides, then `diff` must be empty).
10. Run `helm lint helm` (informational `icon is recommended` is acceptable; zero failed charts required).
11. Self-check: before finishing, re-run every command in `<verification>` and walk requirements 1-8 against the diff.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass (`make test`).
- Optional means absent: with `cluster` empty, `publishUntagged` unset and no `consumerGroup`, no env entry, comment, blank line or key may be added to any rendered manifest (only the `helm.sh/chart` label changes via the version bump).
- `publishUntagged` MUST be tested with `hasKey` + `kindIs "bool"`, never by truthiness — `false` must render `"false"`. Do not add a default for it in `helm/values.yaml` (a default would make "unset" and `false` indistinguishable); the commented-out example is the only mention.
- Do NOT add per-controller or per-executor `cluster` / `publishUntagged` overrides, validation of cluster names, or `required` checks — the spec for this change is the three values only.
- Do NOT change the Chart `appVersion`, image tags, CRDs or any other template.
- Do NOT edit Go code; this change is chart-only (plus CHANGELOG).
- All paths in this prompt are repo-relative; scratch files go in `/tmp` only.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` — must pass (the chart is not covered by it; it guards that nothing else regressed).

Run `helm lint helm` — must report `0 chart(s) failed`.

Run the render checks from requirement 9 and confirm each assertion; in particular:
- `! grep -qE 'CLUSTER|PUBLISH_UNTAGGED|CONSUMER_GROUP' /tmp/helm-default.yaml`
- `! grep -qE 'CLUSTER|PUBLISH_UNTAGGED|CONSUMER_GROUP' /tmp/helm-default-list.yaml`
- `grep -A1 'name: CLUSTER' /tmp/helm-sun.yaml` shows `value: "sun"` (two occurrences: Deployment and StatefulSet)
- `grep -A1 'name: PUBLISH_UNTAGGED' /tmp/helm-sun.yaml` shows `value: "false"` (one occurrence)
- `grep -A1 'name: CONSUMER_GROUP' /tmp/helm-sun.yaml` shows `value: "agent-task-executor-sun"` (one occurrence)
- `grep -q '^version: 0.8.0$' helm/Chart.yaml`
- `awk '/^## /{sec=$0} /feat\(helm\): render optional cluster-routing env/{print sec}' CHANGELOG.md` prints `## Unreleased`

If `helm` is not available on PATH in the container, state that explicitly in your final summary and still complete every file edit; do not claim the render checks passed.
</verification>
