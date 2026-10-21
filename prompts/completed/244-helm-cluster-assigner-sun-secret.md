---
status: completed
execution_id: agent-cluster-assigner-sun-secret-exec-244-helm-cluster-assigner-sun-secret
dark-factory-version: v0.198.7
created: "2026-10-21T18:10:52Z"
queued: "2026-10-21T18:14:55Z"
started: "2026-10-21T18:15:13Z"
completed: "2026-10-21T18:26:21Z"
---
# feat(helm): cluster-assigner renders its own sun Secret

<summary>
- The cluster assigner can now get its sun-cluster credentials straight from the install command, instead of needing a Secret created by hand beforehand
- The token and CA data are handed over at apply time (resolved from TeamVault); they are never written into any committed values file
- When the credentials are supplied inline and no existing Secret is named, the chart creates one small Secret holding both values
- Installs that already name an existing sun Secret render exactly what they render today — no new Secret, same references
- Turning sun routing on without either an existing Secret or the inline credentials still stops the install, with an error naming the missing value
- With sun routing off, or the cluster assigner disabled, nothing changes
- The values reference, chart version and changelog are updated
</summary>

<objective>
Let the optional `clusterAssigner` component of the Helm chart in `helm/` render its own sun Secret: two new values `clusterAssigner.sun.token` / `clusterAssigner.sun.caData` (default `""`, supplied at apply time via `--set-json` from TeamVault) feed a new chart-rendered Secret `cluster-assigner-sun`, which the CronJob's `SUN_TOKEN` / `SUN_CA_DATA` env reference whenever `clusterAssigner.sun.secretName` is empty. Installs that set `sun.secretName` must render byte-identically to today (apart from the `helm.sh/chart` version label).
</objective>

<context>
Read `CLAUDE.md` for project conventions (single global `CHANGELOG.md` at repo root, no per-module changelog).

Read these files before editing; anchor on the names given, not on line numbers:

- `helm/templates/cluster-assigner-cronjob.yaml` — the CronJob added by prompt 242. The sun env block today is:
  ```yaml
                {{- if $ca.sunRouting }}
                {{- $sunSecret := required "clusterAssigner.sun.secretName is required when clusterAssigner.sunRouting is true" $ca.sun.secretName }}
                - name: SUN_TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: {{ $sunSecret }}
                      key: sun-token
                - name: SUN_CA_DATA
                  valueFrom:
                    secretKeyRef:
                      name: {{ $sunSecret }}
                      key: sun-ca-data
                {{- end }}
  ```
- `helm/templates/a2a-worker-controller-secret.yaml` — the exemplar for a chart-rendered Secret built from caller-supplied values: gated with `{{- if and ... (not ...existingSecret) }}`, leading `---`, a comment explaining the values come from TeamVault at apply time and are never committed, `apiVersion: v1` / `kind: Secret` / `type: Opaque`, `namespace: {{ include "agent.namespace" . }}`, labels via `{{- include "agent.labels" . | nindent 4 }}`, and each `data:` key as `{{ required "<value path> is required (or set existingSecret)" $a.<value> | b64enc | quote }}`.
- `helm/values.yaml` — the `clusterAssigner:` block (after `recurringTaskCreator:`): its leading comment block (currently says "the chart creates no Secret for this component"), the `existingSecret` comment (ends "The chart never creates a Secret for this component."), the `sunRouting` comment ("Requires sun.secretName."), and the `sun:` sub-block with `apiURL`, `namespace`, `secretName` and the `secretName` comment.
- `helm/README.md` — `### clusterAssigner (optional)` table; the row `clusterAssigner.sun.apiURL` / `sun.namespace` / `sun.secretName`.
- `helm/Chart.yaml` — currently `version: 0.12.0`.
- `CHANGELOG.md` — top heading is `## v0.113.0`; there is no `## Unreleased` section yet. Helm entries use `- feat(helm): ... chart 0.11.0→0.12.0.`.
- `prompts/completed/242-helm-cluster-assigner-cronjob.md` — the prompt that built the component; its `<verification>` shows the helm-install precondition and the `/tmp/ca-base.yaml` render base reused below.
</context>

<requirements>
1. **Install helm, then snapshot before editing.** Run the helm install from the `<verification>` precondition first (the container does not ship helm). Then copy the unmodified chart: `rm -rf /tmp/helm-orig && cp -r helm /tmp/helm-orig`. Create `/tmp/ca-base.yaml` (minimal values that make the default chart render):
   ```yaml
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     sentry:
       dsn: dummy
   ```

2. **`helm/values.yaml` — `clusterAssigner.sun` gains two keys.** Add `token: ""` and `caData: ""` to the `sun:` sub-block, after `secretName`, with a comment in the file's style: caller-supplied sun API token (→ `SUN_TOKEN`) and CA data (→ `SUN_CA_DATA`, passed through verbatim); used only when `sunRouting: true` and `secretName` is empty, in which case the chart renders Secret `cluster-assigner-sun` from them; supplied at apply time via `--set-json` from TeamVault, never committed to a values file. Update the surrounding comments so they stay true:
   - `secretName` comment: existing Secret holding keys `sun-token` / `sun-ca-data`; read only when `sunRouting: true`; empty = the chart renders `cluster-assigner-sun` from `token` / `caData` (then both required).
   - `sunRouting` comment: requires `sun.secretName`, or `sun.token` + `sun.caData`.
   - Leading block comment and `existingSecret` comment: the chart creates no Secret for the gateway secret / Sentry DSN; the only Secret it may create for this component is the optional sun Secret.
   Add no other keys.

3. **Create `helm/templates/cluster-assigner-sun-secret.yaml`** following `a2a-worker-controller-secret.yaml`. Rendered only when `clusterAssigner.enabled` AND `clusterAssigner.sunRouting` AND `clusterAssigner.sun.secretName` is empty. Shape (verified to render with helm):
   ```yaml
   {{- $ca := .Values.clusterAssigner }}
   {{- if and $ca.enabled $ca.sunRouting (not $ca.sun.secretName) }}
   ---
   # Created only when sunRouting is on and no sun.secretName is referenced. The token
   # and CA data are supplied by the caller at apply time — resolved from TeamVault and
   # passed via --set-json — so no credential is ever committed to a values file.
   apiVersion: v1
   kind: Secret
   type: Opaque
   metadata:
     name: cluster-assigner-sun
     namespace: {{ include "agent.namespace" . }}
     labels:
       {{- include "agent.labels" . | nindent 4 }}
   data:
     sun-token: {{ required "clusterAssigner.sun.token is required when clusterAssigner.sunRouting is true (or set clusterAssigner.sun.secretName)" $ca.sun.token | b64enc | quote }}
     sun-ca-data: {{ required "clusterAssigner.sun.caData is required when clusterAssigner.sunRouting is true (or set clusterAssigner.sun.secretName)" $ca.sun.caData | b64enc | quote }}
   {{- end }}
   ```
   The `required` calls are the failure path: sun routing on with neither `secretName` nor the inline values must fail the render with a message naming the missing value and the `secretName` alternative.

4. **`helm/templates/cluster-assigner-cronjob.yaml` — point the sun env at the right Secret.** Inside `{{- if $ca.sunRouting }}`, replace only the `$sunSecret` line:
   - old: `{{- $sunSecret := required "clusterAssigner.sun.secretName is required when clusterAssigner.sunRouting is true" $ca.sun.secretName }}`
   - new: `{{- $sunSecret := $ca.sun.secretName | default "cluster-assigner-sun" }}`
   The hard `required` on `secretName` is removed so the inline path works (the missing-credentials error now comes from the Secret template). Leave the `SUN_TOKEN` / `SUN_CA_DATA` entries, keys `sun-token` / `sun-ca-data`, indentation, and every other line of the CronJob unchanged, so a `secretName` install renders byte-identically.

5. **`helm/README.md`.** In the `### clusterAssigner (optional)` table, update the `sun.secretName` description (existing Secret with keys `sun-token` / `sun-ca-data`; empty = chart renders `cluster-assigner-sun` from `sun.token` / `sun.caData`) and add one row directly after it for `clusterAssigner.sun.token` / `sun.caData` (default `""` / `""`): sun API token and CA data; with `sunRouting` on and no `sun.secretName`, both are required and rendered into Secret `cluster-assigner-sun`; pass at apply time via `--set-json` from TeamVault, never commit them. Update the intro sentence above the table only if it claims the chart creates no Secret.

6. **`helm/Chart.yaml`.** `version: 0.12.0` → `version: 0.13.0`. Leave `appVersion` and `sources:` unchanged.

7. **`CHANGELOG.md`.** Insert a `## Unreleased` section directly above `## v0.113.0` (blank line before and after, as the file spaces headings) with one bullet:
   `- feat(helm): \`clusterAssigner\` can render its own sun Secret \`cluster-assigner-sun\` (keys \`sun-token\` / \`sun-ca-data\`) from new values \`clusterAssigner.sun.token\` / \`sun.caData\`, supplied at apply time via \`--set-json\` from TeamVault; rendered only when \`sunRouting\` is on and \`sun.secretName\` is empty, and \`SUN_TOKEN\` / \`SUN_CA_DATA\` then reference it; installs setting \`sun.secretName\` render unchanged; sun routing with neither fails the render naming the missing value; chart 0.12.0→0.13.0.`
   (In the file, use plain backticks, not the escaped form above.)

8. **Render checks — run every one yourself; they are the tests for this change.** All values are dummies (`dummy-token`, `dummy-ca-data`, Secret name `sun-creds`); never use real credentials. Scratch files under `/tmp` only. Run under bash. The exact commands and expected outcomes are listed in `<verification>`; they cover:
   - Disabled (default) and disabled-with-`sunRouting=true`: output identical to the snapshot apart from the `helm.sh/chart` label; no `cluster-assigner` object.
   - Enabled, `sunRouting` false, inline token/caData supplied: identical to the snapshot apart from the label; no `cluster-assigner-sun` anywhere.
   - Case 1 — `sunRouting` true + `sun.secretName=sun-creds`: identical to the snapshot apart from the label (also with inline values additionally set); no `cluster-assigner-sun` Secret; both env `secretKeyRef`s name `sun-creds`.
   - Case 2 — `sunRouting` true + inline `--set-json` token/caData, no `secretName`: Secret `cluster-assigner-sun` in namespace `test` with `sun-token: "ZHVtbXktdG9rZW4="` (b64 of `dummy-token`) and `sun-ca-data: "ZHVtbXktY2EtZGF0YQ=="` (b64 of `dummy-ca-data`); both env `secretKeyRef`s name `cluster-assigner-sun`.
   - Case 3 — `sunRouting` true with neither: render exits non-zero, stderr names `clusterAssigner.sun.token is required` and `clusterAssigner.sun.secretName`; with only the token: stderr names `clusterAssigner.sun.caData is required`.
   If any check fails, fix the chart and re-run all checks; never weaken a check.

9. Self-check: before finishing, re-run every command in `<verification>`, confirm each passes, and walk requirements 2-7 against the changed files.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- No real credential may appear anywhere — not in values files, templates, README, CHANGELOG, or scratch files. Use only the dummy values given. `clusterAssigner.sun.token` / `caData` default to `""` and are supplied at apply time via `--set-json`.
- With `clusterAssigner.enabled: false`, with `sunRouting: false`, and with `sun.secretName` set, the rendered chart must be identical to today apart from the `helm.sh/chart` label (version bump).
- The Secret is named exactly `cluster-assigner-sun` with keys exactly `sun-token` and `sun-ca-data`; values are `b64enc`'d.
- Do NOT touch any other component's template or values (executor, executors, controllers, agents, recurringTaskCreator, a2aWorkerController), the CRDs, `_helpers.tpl`, `helm/templates/cluster-assigner-kafkauser.yaml`, or `appVersion`.
- Do NOT create or edit any `values-*.yaml` overlay — cluster overlays live in another repo.
- Add no values beyond `clusterAssigner.sun.token` and `clusterAssigner.sun.caData`.
- Do NOT edit Go code; this change is chart-only plus `CHANGELOG.md`.
- All paths in this prompt are repo-relative; scratch files go in `/tmp` only, never inside the repo.
</constraints>

<verification>
Precondition — the container image does not ship helm; install it from the Go proxy (repo pattern, see `prompts/completed/242-helm-cluster-assigner-cronjob.md`; `get.helm.sh` is 403 from the build network). A failure here means the prompt FAILED (report it as failed in the final summary; never skip the render checks or claim they passed):
`command -v helm >/dev/null 2>&1 || { go install helm.sh/helm/v3/cmd/helm@v3.16.4; export PATH="$PATH:$(go env GOPATH)/bin"; }`
`helm version --short | grep -q '^v3\.' || { echo "PRECONDITION FAILED: helm v3 unavailable in container"; exit 1; }`
Each new shell must re-export `PATH="$PATH:$(go env GOPATH)/bin"` (or call `$(go env GOPATH)/bin/helm`). Run all render commands under bash in one shell that first defines:
```bash
B="-f /tmp/ca-base.yaml --set clusterAssigner.enabled=true --set clusterAssigner.kafkaBrokers=b:1 --set clusterAssigner.gitRestURL=http://g"
INLINE=(--set-json 'clusterAssigner.sun.token="dummy-token"' --set-json 'clusterAssigner.sun.caData="dummy-ca-data"')
```
(`$B` relies on word-splitting, so leave it unquoted; `"${INLINE[@]}"` stays quoted.) Every command below must succeed (exit 0); every `diff` must print nothing.

Disabled — nothing changes:
- `diff <(helm template x /tmp/helm-orig -f /tmp/ca-base.yaml | grep -v 'helm.sh/chart:') <(helm template x helm/ -f /tmp/ca-base.yaml | grep -v 'helm.sh/chart:')`
- `helm template x helm/ -f /tmp/ca-base.yaml --set clusterAssigner.sunRouting=true > /tmp/ca-off.yaml`
- `! grep -q cluster-assigner /tmp/ca-off.yaml`

sunRouting false — no Secret even with inline values:
- `helm template x helm/ $B "${INLINE[@]}" > /tmp/ca-nosun.yaml`
- `diff <(helm template x /tmp/helm-orig $B | grep -v 'helm.sh/chart:') <(grep -v 'helm.sh/chart:' /tmp/ca-nosun.yaml)`
- `! grep -q 'cluster-assigner-sun' /tmp/ca-nosun.yaml`

Case 1 — sunRouting true + secretName set → no chart Secret, env refs point at secretName, render unchanged:
- `helm template x helm/ $B --set clusterAssigner.sunRouting=true --set clusterAssigner.sun.secretName=sun-creds > /tmp/ca-sun-ref.yaml`
- `diff <(helm template x /tmp/helm-orig $B --set clusterAssigner.sunRouting=true --set clusterAssigner.sun.secretName=sun-creds | grep -v 'helm.sh/chart:') <(grep -v 'helm.sh/chart:' /tmp/ca-sun-ref.yaml)`
- `! grep -q 'cluster-assigner-sun' /tmp/ca-sun-ref.yaml`
- `grep -A3 'name: SUN_TOKEN$' /tmp/ca-sun-ref.yaml | grep -q 'name: sun-creds$'`
- `grep -A3 'name: SUN_CA_DATA$' /tmp/ca-sun-ref.yaml | grep -q 'name: sun-creds$'`
- `helm template x helm/ $B --set clusterAssigner.sunRouting=true --set clusterAssigner.sun.secretName=sun-creds "${INLINE[@]}" > /tmp/ca-sun-both.yaml && diff /tmp/ca-sun-ref.yaml /tmp/ca-sun-both.yaml` (secretName wins)

Case 2 — sunRouting true + inline token/caData, no secretName → Secret rendered with both keys, env refs point at `cluster-assigner-sun`:
- `helm template x helm/ $B --set clusterAssigner.sunRouting=true "${INLINE[@]}" > /tmp/ca-sun-inline.yaml`
- `helm template x helm/ $B --set clusterAssigner.sunRouting=true "${INLINE[@]}" --show-only templates/cluster-assigner-sun-secret.yaml > /tmp/ca-sun-secret.yaml`
- `grep -qx 'kind: Secret' /tmp/ca-sun-secret.yaml && grep -qx '  name: cluster-assigner-sun' /tmp/ca-sun-secret.yaml && grep -qx '  namespace: test' /tmp/ca-sun-secret.yaml`
- `grep -qx '  sun-token: "ZHVtbXktdG9rZW4="' /tmp/ca-sun-secret.yaml && grep -qx '  sun-ca-data: "ZHVtbXktY2EtZGF0YQ=="' /tmp/ca-sun-secret.yaml`
- `grep -A3 'name: SUN_TOKEN$' /tmp/ca-sun-inline.yaml | grep -q 'name: cluster-assigner-sun$'`
- `grep -A3 'name: SUN_CA_DATA$' /tmp/ca-sun-inline.yaml | grep -q 'name: cluster-assigner-sun$'`
- `[ "$(grep -c 'name: cluster-assigner-sun$' /tmp/ca-sun-inline.yaml)" = 3 ]` (Secret metadata + two env refs)

Case 3 — sunRouting true with neither → render fails with the required message:
- `! helm template x helm/ $B --set clusterAssigner.sunRouting=true >/dev/null 2>/tmp/ca-sun-none.err`
- `grep -q 'clusterAssigner.sun.token is required' /tmp/ca-sun-none.err && grep -q 'clusterAssigner.sun.secretName' /tmp/ca-sun-none.err`
- `! helm template x helm/ $B --set clusterAssigner.sunRouting=true --set-json 'clusterAssigner.sun.token="dummy-token"' >/dev/null 2>/tmp/ca-sun-noca.err`
- `grep -q 'clusterAssigner.sun.caData is required' /tmp/ca-sun-noca.err`

Lint:
- `helm lint helm/ | grep -q '0 chart(s) failed'`
- `helm lint helm/ $B --set clusterAssigner.sunRouting=true "${INLINE[@]}" | grep -q '0 chart(s) failed'`

Metadata:
- `grep -q '^version: 0.13.0$' helm/Chart.yaml`
- `awk '/^## /{sec=$0} /feat\(helm\): .clusterAssigner. can render its own sun Secret/{print sec}' CHANGELOG.md` prints `## Unreleased`
- `! grep -rn 'dummy-token\|dummy-ca-data\|sun-creds' helm/ CHANGELOG.md` (no dummy or test value leaked into the repo)

`ROOTDIR=/workspace make precommit` — must pass (it does not cover the chart; it guards that nothing else regressed).
</verification>
