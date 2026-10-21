---
status: completed
execution_id: agent-helm-stagefilter-exec-240-helm-executor-stage-filter-env
dark-factory-version: v0.198.7
created: "2026-10-21T08:30:00Z"
queued: "2026-10-21T06:14:05Z"
started: "2026-10-21T06:14:10Z"
completed: "2026-10-21T07:37:42Z"
---

# feat(helm): render STAGE_FILTER env on the executor

<summary>
- The chart can tell the executor to stop filtering tasks by their `stage:` field, for clusters whose controller already scopes tasks by cluster
- The setting is optional: when it is not set, every rendered manifest stays exactly as it is today
- Explicitly setting it to `false` is honoured and not mistaken for "unset"
- It can be set once for all executors and overridden per executor in the `executors` list; an entry without its own value inherits the shared one
- The value reference and the README explain what the setting does and which clusters should use it (sun yes, nuke dev/prod leave it unset)
- The chart version is bumped and the change is recorded in the changelog
</summary>

<objective>
Plumb the optional `STAGE_FILTER` environment variable that `agent-task-executor` v0.21.0 now reads through the Helm chart in `helm/`. `STAGE_FILTER=false` makes the executor ignore the task `stage:` field; it is meant for a cluster whose controller already scopes tasks by cluster (sun). Unset must leave every rendered manifest byte-identical (apart from the chart-version label) to today, because the executor's own default is `true`.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single global `CHANGELOG.md`, no per-module changelog).

Read these files before editing; anchor on the names below, not line numbers:
- `helm/templates/executor-deployment.yaml` — two Deployments in one file. The first is guarded by `{{- if and .Values.executor.enabled (empty .Values.executors) }}` (singular executor, uses `.Values`). The second is `{{- range $e := .Values.executors }}` / `{{- if $e.enabled }}` (uses `$e` for the entry and `$.Values` for root values). In both, the `containers[0].env:` block currently ends with the `TASK_GLOB` entry followed by the `{{- with .Values.cluster }}` / `CLUSTER` entry and the `{{- with ... consumerGroup }}` / `CONSUMER_GROUP` entry (12-space indentation for `- name:`).
- `helm/templates/controller-statefulset.yaml` — existing precedent for a presence-tested bool env: `PUBLISH_UNTAGGED` is rendered with `{{- if and (hasKey $.Values "publishUntagged") (kindIs "bool" $.Values.publishUntagged) }}` and `value: {{ $.Values.publishUntagged | quote }}`. Follow exactly this idiom. Do NOT change the controller.
- `helm/values.yaml` — the `executor:` block (note `taskGlob: ""` and `consumerGroup: ""`, each with a comment block); the top-level commented-out `# publishUntagged: false` with its comment block (precedent for a commented-out, intentionally absent key); the commented `executors: []` example and the per-entry field lists (the `#   consumerGroup: ""             # default executor.consumerGroup` lines in both example entries).
- `helm/README.md` — the `### executor` table (`executor.consumerGroup` row) and the `### executors (per-vault, values-driven)` table (`executors[].consumerGroup` row); the `### Global` table `publishUntagged` row for wording style.
- `helm/Chart.yaml` — currently `version: 0.8.0`.
- `CHANGELOG.md` — top heading is `## v0.108.0`; there is currently no `## Unreleased` section. Existing helm entries use the form `- feat(helm): ... chart 0.7.0→0.8.0.`

Note: `helm.sh/chart` label in `_helpers.tpl` (`agent.labels`) renders `agent-<Chart.Version>`, so bumping the chart version changes that label on every resource. That is the only permitted difference against today's default render.
</context>

<requirements>
1. **`helm/values.yaml` — document the new key.** In the `executor:` block, directly after `consumerGroup: ""`, add a **commented-out** entry (no real key — a default would make "unset" and `false` indistinguishable):
   ```yaml
   # stageFilter: rendered as the `STAGE_FILTER` env (quoted "true"/"false") on the
   # executor only when the key is set — `false` is honoured, unset renders
   # nothing (the executor's own default `true` applies). `false` makes the
   # executor ignore the task `stage:` field; set it only on a cluster whose
   # controller already scopes tasks by cluster (e.g. sun). nuke dev/prod leave
   # it unset. Requires agent-task-executor >= v0.21.0.
   # stageFilter: false
   ```
   Use two-space indentation inside `executor:` like the neighbouring entries. In the commented `executors:` example, add `#   stageFilter: false            # default executor.stageFilter; renders only when set` to both example entries, directly after their `consumerGroup` line, matching that line's comment style.
2. **`helm/templates/executor-deployment.yaml` — singular executor Deployment.** After the `CONSUMER_GROUP` block of the `{{- if and .Values.executor.enabled (empty .Values.executors) }}` Deployment, render:
   ```
               {{- if and (hasKey .Values.executor "stageFilter") (kindIs "bool" .Values.executor.stageFilter) }}
               - name: STAGE_FILTER
                 value: {{ .Values.executor.stageFilter | quote }}
               {{- end }}
   ```
   (the `- name:` line keeps the 12-space indentation of its neighbours). Result: `true` renders `"true"`, `false` renders `"false"`, key absent renders nothing and leaves no blank or stray line. A non-bool value renders nothing; add no extra handling.
3. **`helm/templates/executor-deployment.yaml` — `executors[]` Deployments.** After the `CONSUMER_GROUP` block inside the `range` Deployment, resolve the effective value with per-entry precedence, then render only when it is a bool:
   ```
               {{- $stageFilter := $.Values.executor.stageFilter }}
               {{- if hasKey $e "stageFilter" }}
               {{- $stageFilter = $e.stageFilter }}
               {{- end }}
               {{- if kindIs "bool" $stageFilter }}
               - name: STAGE_FILTER
                 value: {{ $stageFilter | quote }}
               {{- end }}
   ```
   Per-entry key present (including `false`) wins; otherwise `executor.stageFilter` is inherited; neither set renders nothing. Do not use `default` for this — it treats `false` as empty and would drop it. Confirm the variable assignment (`=`, not `:=`, for the reassignment) renders no stray blank lines in the env list.
4. **`helm/README.md`.** Document with the same table style as neighbouring rows:
   - `### executor` table: after the `executor.consumerGroup` row add `executor.stageFilter` — default `unset`; description: rendered as `STAGE_FILTER` (quoted `"true"`/`"false"`) on the executor **only when the key is set**, so `false` is honoured and unset renders nothing (executor default `true` applies). `false` makes the executor ignore the task `stage:` field; use only on a cluster whose controller already scopes tasks by cluster (sun); nuke dev/prod leave it unset. Requires agent-task-executor ≥ v0.21.0.
   - `### executors` table: after the `executors[].consumerGroup` row add `executors[].stageFilter` — default `executor.stageFilter`; description: rendered as `STAGE_FILTER` on that executor; a per-entry value (including `false`) wins, otherwise `executor.stageFilter` is inherited, neither set = nothing rendered.
5. **`helm/Chart.yaml`.** Bump `version: 0.8.0` to `version: 0.9.0` (minor: new optional value, backwards-compatible). Leave `appVersion` unchanged.
6. **`CHANGELOG.md`.** Insert a `## Unreleased` section directly above `## v0.108.0` (blank line before and after, matching the file's heading spacing) containing one bullet in the existing helm style (normal markdown backticks in the file):
   `- feat(helm): render optional \`STAGE_FILTER\` env on the singular executor and on each \`executors[]\` entry — \`executor.stageFilter\` / \`executors[].stageFilter\` (per-entry wins, else inherits \`executor.stageFilter\`); rendered whenever the key is set so \`false\` renders, nothing when unset; \`false\` makes the executor ignore the task \`stage:\` field, for clusters whose controller already scopes tasks by cluster (sun; nuke dev/prod leave it unset); chart 0.8.0→0.9.0. Requires agent-task-executor >= v0.21.0; older binaries ignore the env.`
7. **Render checks (run these yourself; this is the test for the template changes).** Scratch files go under `/tmp` only, never inside the repo. Before editing anything, copy `helm/` to `/tmp/helm-orig` for the unchanged-by-default comparison. Create `/tmp/helm-stage-values.yaml`:
   ```yaml
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     vaultName: personal
     image:
       tag: "0.21.0"
     sentry:
       dsn: dummy
   ```
   and `/tmp/helm-stage-values-list.yaml` identical but adding a top-level `executors:` list with two entries: `- {name: personal, vaultName: personal, enabled: true}` and `- {name: other, vaultName: other, enabled: true}`.
   - Default render (singular and list, no stageFilter): `! grep -q STAGE_FILTER` on both outputs.
   - Singular, `--set executor.stageFilter=false`: per-document check (split on `---`, e.g. with `awk`) that the `agent-task-executor` Deployment contains `name: STAGE_FILTER` followed by `value: "false"`. Also `--set executor.stageFilter=true` renders `"true"`.
   - List, `--set 'executors[1].stageFilter=false'` (no `executor.stageFilter`): `STAGE_FILTER` `"false"` appears only in the `agent-task-executor-other` document, not in `agent-task-executor-personal`.
   - List inheritance, `--set executor.stageFilter=false`: `"false"` appears on both `-personal` and `-other`. With `--set executor.stageFilter=false --set 'executors[1].stageFilter=true'`: `-personal` is `"false"` (inherited), `-other` is `"true"` (per-entry wins).
   - Unchanged-by-default: render `/tmp/helm-orig` and the edited chart with `/tmp/helm-stage-values.yaml` and with `/tmp/helm-stage-values-list.yaml`; after removing lines matching `helm.sh/chart:` from both sides, `diff` must be empty.
8. Run `helm lint helm -f /tmp/helm-stage-values.yaml` (informational `icon is recommended` is acceptable; zero failed charts required).
9. Self-check: before finishing, re-run every command in `<verification>` and walk requirements 1-6 against the changed files.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass (`make test`).
- Optional means absent: with `stageFilter` unset at both levels, no env entry, comment, blank line or key may be added to any rendered manifest (only the `helm.sh/chart` label changes via the version bump).
- `stageFilter` MUST be tested with `hasKey` + `kindIs "bool"`, never by truthiness or `default` — `false` must render `"false"`. Do NOT add a default for it in `helm/values.yaml`; the commented-out example is the only mention.
- STAGE_FILTER applies to executors only (singular Deployment and `executors[]` entries). Do NOT touch the controller StatefulSet, recurring-task-creator, a2a-worker-controller or any other template.
- Do NOT add validation, `required` checks, cluster-name coupling (e.g. deriving the value from `cluster`), or any other new value — `stageFilter` is the only addition.
- Do NOT change the Chart `appVersion`, image tags, CRDs or any other template.
- Do NOT edit Go code; this change is chart-only (plus CHANGELOG).
- All paths in this prompt are repo-relative; scratch files go in `/tmp` only.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` — must pass (the chart is not covered by it; it guards that nothing else regressed).

Run `helm lint helm -f /tmp/helm-stage-values.yaml` — must report `0 chart(s) failed`.

Run the render checks from requirement 7 and confirm each assertion; in particular:
- `! helm template t helm -f /tmp/helm-stage-values.yaml | grep -q STAGE_FILTER`
- `! helm template t helm -f /tmp/helm-stage-values-list.yaml | grep -q STAGE_FILTER`
- `helm template t helm -f /tmp/helm-stage-values.yaml --set executor.stageFilter=false | grep -A1 'name: STAGE_FILTER'` shows `value: "false"` (one occurrence)
- `helm template t helm -f /tmp/helm-stage-values-list.yaml --set 'executors[1].stageFilter=false' | grep -c 'name: STAGE_FILTER'` prints `1`
- `helm template t helm -f /tmp/helm-stage-values-list.yaml --set executor.stageFilter=false | grep -c 'name: STAGE_FILTER'` prints `2`
- `grep -q '^version: 0.9.0$' helm/Chart.yaml`
- `awk '/^## /{sec=$0} /feat\(helm\): render optional .STAGE_FILTER. env/{print sec}' CHANGELOG.md` prints `## Unreleased`

If `helm` is not available on PATH in the container, state that explicitly in your final summary and still complete every file edit; do not claim the render checks passed.
Before the STAGE_FILTER list assertions, confirm the list render actually produces both executor Deployments (`helm template t helm -f /tmp/helm-stage-values-list.yaml | grep -c 'name: agent-task-executor-\(personal\|other\)$'` is at least 2); a 0 means the test values are wrong, not that the template passes.
</verification>
