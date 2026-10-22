---
status: completed
execution_id: agent-cluster-assigner-exclude-env-exec-245-helm-cluster-assigner-exclude-assignees
dark-factory-version: v0.198.7
created: "2026-10-21T18:54:17Z"
queued: "2026-10-21T18:54:17Z"
started: "2026-10-21T18:54:27Z"
completed: "2026-10-21T19:53:10Z"
---
# feat(helm): cluster-assigner passes EXCLUDE_ASSIGNEES

<summary>
- An operator can name agents whose tasks the cluster assigner must never send to the sun cluster, straight from the chart values
- The list reaches the assigner as its `EXCLUDE_ASSIGNEES` setting (agent-cluster-assigner v0.1.3 and later)
- Leaving the list empty changes nothing: existing installs render exactly what they render today
- The values reference, chart version and changelog are updated
</summary>

<objective>
Add an optional `clusterAssigner.excludeAssignees` value (a YAML list of strings, default `[]`) to the Helm chart in `helm/`. When it is non-empty, the cluster-assigner CronJob container gets env `EXCLUDE_ASSIGNEES` set to the entries joined with `,`. When it is empty, no env entry is rendered, so every existing render stays byte-identical apart from the `helm.sh/chart` version label.
</objective>

<context>
- `helm/templates/cluster-assigner-cronjob.yaml` — the CronJob. Its container `env:` list already renders `SUN_ROUTING` and `DRY_RUN` from `$ca` (`{{- $ca := .Values.clusterAssigner }}`). Add the new entry directly after `DRY_RUN`.
- `helm/values.yaml` — the `clusterAssigner:` block (`sunRouting`, `dryRun`, `sun:` …). Add `excludeAssignees: []` after `dryRun`, with a comment in the file's style.
- `helm/README.md` — the `### clusterAssigner (optional)` table, which has a row for `clusterAssigner.sunRouting` / `dryRun`. Add one row after it.
- `helm/Chart.yaml` — currently `version: 0.13.0`.
- `CHANGELOG.md` — no `## Unreleased` section exists yet if the top heading is a version; add one above it.
- `prompts/completed/244-helm-cluster-assigner-sun-secret.md` — previous prompt on this component; its `<verification>` shows the helm install precondition and the `/tmp/ca-base.yaml` render base reused below.
- The binary (bborbe/agent-cluster-assigner) reads `EXCLUDE_ASSIGNEES` as a comma-separated list; tasks of a listed assignee are never newly tagged sun.
</context>

<requirements>
1. **Snapshot first.** Install helm per the `<verification>` precondition, then `rm -rf /tmp/helm-orig && cp -r helm /tmp/helm-orig` before editing. Create `/tmp/ca-base.yaml`:
   ```yaml
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     sentry:
       dsn: dummy
   ```
2. **`helm/values.yaml`.** In the `clusterAssigner:` block, after `dryRun`, add:
   ```yaml
     # Assignees whose tasks are never newly tagged sun; they get their stage
     # default (→ EXCLUDE_ASSIGNEES, comma-joined). Empty renders no env entry.
     # Needs image.tag v0.1.3 or later; older images ignore the variable.
     excludeAssignees: []
   ```
3. **`helm/templates/cluster-assigner-cronjob.yaml`.** Directly after the `DRY_RUN` env entry, add an entry rendered only when the list is non-empty, matching the surrounding indentation exactly:
   ```yaml
                {{- with $ca.excludeAssignees }}
                - name: EXCLUDE_ASSIGNEES
                  value: {{ join "," . | quote }}
                {{- end }}
   ```
4. **`helm/README.md`.** In the `### clusterAssigner (optional)` table, add this exact row directly after the `sunRouting` / `dryRun` row:
   ```markdown
   | `clusterAssigner.excludeAssignees` | `[]` | Assignees never newly tagged sun (→ `EXCLUDE_ASSIGNEES`, comma-joined; needs `image.tag` v0.1.3 or later). Empty renders nothing. |
   ```
5. **`helm/Chart.yaml`.** `version: 0.13.0` → `version: 0.14.0`. Leave `appVersion` unchanged.
6. **`CHANGELOG.md`.** Add a `## Unreleased` section above the top version heading (or reuse an existing `## Unreleased`) with this bullet:
   ```markdown
   - feat(helm): `clusterAssigner.excludeAssignees` (list, default `[]`) renders `EXCLUDE_ASSIGNEES` on the cluster-assigner CronJob, comma-joined; tasks of a listed assignee are never newly tagged sun; an empty list renders nothing; chart 0.13.0→0.14.0
   ```
</requirements>

<constraints>
- Do NOT commit.
- Touch only `helm/values.yaml`, `helm/templates/cluster-assigner-cronjob.yaml`, `helm/README.md`, `helm/Chart.yaml`, `CHANGELOG.md`. No Go code, no mocks, no other template.
- No other new values keys.
- An empty list must render no `EXCLUDE_ASSIGNEES` entry at all, not an empty string.
</constraints>

<verification>
Precondition: the container does not ship helm; install it from the Go proxy. If this fails, report the prompt as FAILED and never skip the render checks:
`command -v helm >/dev/null 2>&1 || { go install helm.sh/helm/v3/cmd/helm@v3.16.4; export PATH="$PATH:$(go env GOPATH)/bin"; }`
`helm version --short | grep -q '^v3\.' || { echo "PRECONDITION FAILED: helm v3 unavailable in container"; exit 1; }`

Run every render check below inside ONE `bash <<'EOS' … EOS` block, because the tool shell is zsh and `$B` relies on word-splitting. Every command must exit 0 and every `diff` must print nothing:
```bash
B="-f /tmp/ca-base.yaml --set clusterAssigner.enabled=true --set clusterAssigner.kafkaBrokers=b:1 --set clusterAssigner.gitRestURL=http://g"
f() { grep -v 'helm.sh/chart:'; }
# disabled: unchanged
diff <(helm template x /tmp/helm-orig -f /tmp/ca-base.yaml | f) <(helm template x helm/ -f /tmp/ca-base.yaml | f)
# enabled, list empty: unchanged, no env entry
diff <(helm template x /tmp/helm-orig $B | f) <(helm template x helm/ $B | f)
! helm template x helm/ $B | grep -q EXCLUDE_ASSIGNEES
# one entry
helm template x helm/ $B --set-json 'clusterAssigner.excludeAssignees=["releaser"]' --show-only templates/cluster-assigner-cronjob.yaml | grep -A1 'name: EXCLUDE_ASSIGNEES$' | grep -qx '                  value: "releaser"'
# two entries, comma-joined
helm template x helm/ $B --set-json 'clusterAssigner.excludeAssignees=["releaser","claude"]' --show-only templates/cluster-assigner-cronjob.yaml | grep -A1 'name: EXCLUDE_ASSIGNEES$' | grep -qx '                  value: "releaser,claude"'
helm lint helm/ -f /tmp/ca-base.yaml
helm lint helm/ $B --set-json 'clusterAssigner.excludeAssignees=["releaser"]'
grep -qx 'version: 0.14.0' helm/Chart.yaml
awk '/^## /{sec=$0} /excludeAssignees/{print sec; exit}' CHANGELOG.md | grep -qx '## Unreleased'
```
Then run `ROOTDIR=/workspace make precommit`. If its ONLY failure is the govulncheck finding GO-2026-6809 (Go standard library crypto/x509 in the container's Go 1.27.2, fixed in 1.27.3), it is an environment issue unrelated to this chart-only change: record it in the summary and report success. Any other precommit failure means FAILED.
</verification>
