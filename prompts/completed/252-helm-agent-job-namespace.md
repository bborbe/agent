---
status: completed
summary: Rendered optional quoted agents[].jobNamespace into the agent Config, documented it in values/README, bumped the chart to 0.22.1, added a CHANGELOG entry and four render test cases
execution_id: agent-jobns-chart-exec-252-helm-agent-job-namespace
dark-factory-version: v0.198.7
created: "2026-10-23T17:36:02Z"
queued: "2026-10-23T17:42:35Z"
started: "2026-10-23T17:42:55Z"
completed: "2026-10-23T17:45:37Z"
---

# feat(helm): render per-agent `spec.jobNamespace` on the agent Config

<summary>
- A per-agent value `agents[].jobNamespace` can now be set in the chart values, and the chart renders it onto that agent's Config as `spec.jobNamespace`
- The executor shipped this field in agent-task-executor v0.31.0, so the chart can now place an agent's Job pods in the sandbox namespace instead of the executor's own namespace
- The field renders only when a value is set. An agent that sets nothing — and an agent that sets an empty string — renders exactly what it renders today, so no existing install changes
- The value is quoted in the rendered YAML, matching the neighbouring string fields
- The values example, the chart README, the chart version and the changelog are updated
- The chart's render test gains four cases: set, unset, empty, and a two-agent render where only one agent sets the value — so a later template edit cannot silently drop the field, start rendering it by default, or read it from the wrong scope
- The bundled Config CRD is deliberately untouched — the executor rewrites that schema at start, and the chart README says which executor version is required
</summary>

<objective>
Let a values overlay set `spec.jobNamespace` on a chart-rendered agent Config, so an install can spawn that agent's Job pods into `<namespace>-agents-sandbox` (the namespace spec 047 renders, with its default-deny egress policy) instead of the executor's own namespace. Today the executor accepts the field but the chart never renders it, so no install can set it. The default render must stay byte-identical.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single root `Makefile`, single global `CHANGELOG.md`) and `docs/dod.md` for the definition of done — a configuration change updates the README and adds a `## Unreleased` CHANGELOG entry.

This is a standalone follow-up, not a prompt generated from spec `047-agent-sandbox-namespace`. That spec is still `in-progress` and owns the sandbox namespace and its egress policy; this change only lets an agent point its Jobs at a namespace that already exists. Do not edit the spec or any file under `specs/`.

The field this prompt renders already exists in the executor. This snippet is the source of truth and is inlined — that repo is not mounted here:

```go
// k8s/apis/agent.benjamin-borbe.de/v1/types.go (agent-task-executor)
// JobNamespace is the namespace the executor creates this Config's Jobs in.
// Empty (the default) means the executor's own namespace, so every Config that
// does not set it behaves exactly as before. A value equal to the executor's
// own namespace is rejected at spawn time: a Config may never opt out of
// isolation by naming the executor namespace. The namespace must already exist
// and already hold this Config's PVCs and Secrets before the field is set.
JobNamespace string `json:"jobNamespace,omitempty"`
```

It shipped in agent-task-executor **v0.31.0** (its `CHANGELOG.md`, top section). The executor's own CRD schema for the field is `{Type: "string", Pattern: crdDNS1123Label, MaxLength: 63}` where `crdDNS1123Label = "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"` (`pkg/k8s_connector.go`) — a valid DNS-1123 label, which is the contract the README must state.

Files to read before editing (anchor on the names below, not on line numbers):

- `helm/templates/agents.yaml` — one `Config` CR per enabled `agents` entry, rendered under `spec:` at 2-space indent. The conditionals to copy in style are `{{- with $agent.configMapName }}` / `{{- with $agent.configMapMountPath }}` (string fields, rendered with `| quote`) and `{{- if hasKey $agent "rerunOnLostClaim" }}` (bool, rendered bare). The `rerunOnLostClaim` block sits directly before the `volumeMountPath` block — that is the insertion point.
- `helm/values.yaml` — the commented `agents:` example (`agents: []` followed by `#` lines). The `rerunOnLostClaim` comment block is the neighbour to mirror.
- `helm/README.md`, section `### agents (leaf agents — values-driven)` — a live YAML example (real values YAML, not comments) plus prose paragraphs below it, including one about `rerunOnLostClaim` and one about `configMapName`.
- `helm/Chart.yaml` — `version: 0.22.0`.
- `CHANGELOG.md` — the top section is `## v0.124.0`; there is no `## Unreleased` section today.
- `hack/test-helm-render.sh` — the render-test harness, run by `make test` via the `test-helm` target. `write_values <file> <rerunOnLostClaim line or empty> [extra agent field(s)]` writes the base values; its third argument appends extra agent fields one per line at 4-space indent. The three `run_case` invocations (`false`, `true`, `unset`) come before the `# doc_has <file> <kind> <name> <namespace("" for cluster-scoped)>` block. `$tmpdir/unset.yaml` is the no-extra-fields values file.

Verified render shapes (helm v4.3.1, current chart): a string field renders quoted at 2-space indent — `configMapName: easy-agent-goreleaser` in the values renders as

```yaml
  configMapName: "easy-agent-goreleaser"
```

and an empty value for the same field renders no line at all. The `agents[]` base entry used by the harness (`rr-agent`) produces exactly one `kind: Config` document under `--show-only templates/agents.yaml`, and each Config's `metadata.name` is the agent's `name` at 2-space indent.

Out of scope, stated so it is not "fixed" here: `helm/crds/config-crd.yaml` does not declare `jobNamespace` and this prompt does not add it. The executor rewrites that schema on every start (`configSpecProperties()` in agent-task-executor `pkg/k8s_connector.go` already carries `jobNamespace` with the DNS-1123 pattern above), and Helm never upgrades files under `crds/` on an existing install, so a chart-side edit there would change nothing for a deployed release. Documenting the executor version requirement in the README is the part this prompt owns.
</context>

<requirements>
1. **Template: `helm/templates/agents.yaml`.** Insert this block immediately after the `{{- end }}` that closes `{{- if hasKey $agent "rerunOnLostClaim" }}`, and before `{{- if $agent.volumeMountPath }}`, at the same 2-space `spec` indent:

   ```yaml
     {{- with $agent.jobNamespace }}
     jobNamespace: {{ . | quote }}
     {{- end }}
   ```

   - `with` (not `if`) matches the neighbouring string fields, and it skips both an absent key and an empty string — so `jobNamespace: ""` and an entry with no key both render nothing.
   - `| quote` matches `configMapName` / `configMapMountPath`: a namespace is a string and renders quoted.
   - Read the value from the loop variable (`$agent.jobNamespace`), never from `.Values` — the loop is over `.Values.agents`, and a `.Values` read would render the same value onto every agent.
   - No chart-side default and no fallback to the sandbox namespace. An agent without the key renders no `jobNamespace` line, which is the byte-identical requirement.
   - Do not change any other block in the file.

2. **Values docs: `helm/values.yaml`.** In the commented `agents:` example, add a commented entry directly after the `rerunOnLostClaim` comment block (which ends with the `# executor default.` line) and before the `#   type: service` comment lines. Use the surrounding style — `  #   key: value` for the first line and `  #` plus spaces to the same comment column for continuation lines:

   ```
     #   jobNamespace: dev-agents-sandbox   # OPTIONAL: Config.spec.jobNamespace. Runs this
     #                                 # agent's Jobs in that namespace instead of the
     #                                 # executor's own. It must already exist and already
     #                                 # hold this agent's PVC and Secret: either place them
     #                                 # there yourself, or point this at the chart's own
     #                                 # sandbox namespace (`sandbox.namespace`, default
     #                                 # `<namespace>-agents-sandbox`) and set
     #                                 # `sandbox.renderAgentStorage: true`, which renders
     #                                 # the chart-managed copies into THAT namespace.
     #                                 # renderAgentStorage never follows jobNamespace to a
     #                                 # different name. Must be a valid DNS-1123 label
     #                                 # (lowercase alphanumerics and `-`, at most 63
     #                                 # characters) — the executor's Config CRD schema
     #                                 # rejects anything else at apply time — and must
     #                                 # differ from the executor's namespace, which the
     #                                 # executor rejects by spawning no Job. Omit to keep
     #                                 # the executor-namespace default. Requires
     #                                 # agent-task-executor >= v0.31.0.
   ```

   `agents` stays `[]`. Add no uncommented key, and no top-level or `sandbox.*` default for this field.

3. **README: `helm/README.md`.** In `### agents (leaf agents — values-driven)`:
   - In the YAML example (live values YAML, not a comment block), add these lines directly after the `rerunOnLostClaim` entry, aligned with it:
     ```
         jobNamespace: dev-agents-sandbox   # OPTIONAL: Config.spec.jobNamespace — where this
                                           # agent's Jobs run. Omit for the executor namespace.
     ```
   - Add one prose paragraph after the existing `rerunOnLostClaim` paragraph below the code block. It must say:
     - the field places that agent's Job pods in the named namespace;
     - the namespace must already exist and already hold the agent's PVC and Secret — either place them there yourself, or point `jobNamespace` at the chart's own sandbox namespace (`sandbox.namespace`, default `<namespace>-agents-sandbox`) and set `sandbox.renderAgentStorage: true`, which renders the chart-managed copies into that namespace. `renderAgentStorage` never follows `jobNamespace` to a different name, so a `jobNamespace` that is not the chart's sandbox namespace needs the PVC and Secret placed by hand;
     - the value must be a valid DNS-1123 label (lowercase alphanumerics and `-`, at most 63 characters), because the executor's Config CRD schema rejects anything else at apply time;
     - a value equal to the executor's own namespace is rejected by the executor and no Job is spawned;
     - the field needs agent-task-executor >= v0.31.0, because an older executor ignores the field and an older Config CRD prunes it.
   - Do not restructure other README content, and do not touch `## Generic cluster (not quant)`.

4. **Chart version: `helm/Chart.yaml`.** `version: 0.22.0` → `version: 0.22.1`. Patch bump for one newly rendered optional per-agent field, matching the `rerunOnLostClaim` precedent. Leave `appVersion` unchanged.

5. **`CHANGELOG.md`.** Add a new `## Unreleased` section at the top, above `## v0.124.0`, separated by a blank line, with this one bullet copied verbatim (it is a markdown file — the inner backticks are real, so copy them, do not escape them):

   ```text
   - feat(helm): render `spec.jobNamespace` from a new optional `agents[].jobNamespace` value, quoted, so an install can spawn an agent's Jobs into its sandbox namespace instead of the executor's own. Nothing renders when the key is absent or empty, so no existing agent's Config changes; the namespace must already exist and hold the agent's PVC/Secret (point it at the chart's `sandbox.namespace` and set `sandbox.renderAgentStorage`, or place them yourself), it must be a DNS-1123 label, and a value equal to the executor namespace is rejected by the executor. Requires agent-task-executor >= v0.31.0 (older executors ignore the field and an older Config CRD prunes it). Chart 0.22.0→0.22.1.
   ```

   Do not add a second `## Unreleased` heading.

6. **Render tests: `hack/test-helm-render.sh`.** Add four cases, in a block placed directly after the three existing `run_case` invocations and before the `# doc_has <file> …` comment. Use the existing `write_values` third argument for the per-agent field and the existing `fail=1` accumulation; do not touch `run_case`, `write_values`, `doc_has` or any existing case.

   ```bash
   # agents[].jobNamespace: rendered (quoted) when set, absent when the key is empty
   # or unset — an install that sets nothing must render exactly as before.
   write_values "$tmpdir/jobns.yaml" "" "jobNamespace: dev-agents-sandbox"
   write_values "$tmpdir/jobns-empty.yaml" "" 'jobNamespace: ""'

   jobns_out="$tmpdir/jobns-set.out"
   "$helm_bin" template x "$CHART_DIR" -f "$tmpdir/jobns.yaml" --show-only templates/agents.yaml >"$jobns_out"
   if ! grep -qx '  jobNamespace: "dev-agents-sandbox"' "$jobns_out"; then
   	echo 'FAIL: job-namespace: missing the quoted jobNamespace line'
   	fail=1
   else
   	echo "OK: job-namespace"
   fi

   jobns_empty_out="$tmpdir/jobns-empty.out"
   "$helm_bin" template x "$CHART_DIR" -f "$tmpdir/jobns-empty.yaml" --show-only templates/agents.yaml >"$jobns_empty_out"
   jobns_empty_count="$(grep -c '^kind: Config$' "$jobns_empty_out" || true)"
   if [ "$jobns_empty_count" -ne 1 ]; then
   	echo "FAIL: job-namespace-empty: expected exactly 1 Config document, got $jobns_empty_count"
   	fail=1
   elif grep -q 'jobNamespace' "$jobns_empty_out"; then
   	echo "FAIL: job-namespace-empty: an empty jobNamespace still rendered a line"
   	fail=1
   else
   	echo "OK: job-namespace-empty"
   fi

   jobns_unset_out="$tmpdir/jobns-unset.out"
   "$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" --show-only templates/agents.yaml >"$jobns_unset_out"
   jobns_unset_count="$(grep -c '^kind: Config$' "$jobns_unset_out" || true)"
   if [ "$jobns_unset_count" -ne 1 ]; then
   	echo "FAIL: job-namespace-unset: expected exactly 1 Config document, got $jobns_unset_count"
   	fail=1
   elif grep -q 'jobNamespace' "$jobns_unset_out"; then
   	echo "FAIL: job-namespace-unset: unexpected jobNamespace line"
   	fail=1
   else
   	echo "OK: job-namespace-unset"
   fi

   # Two agents, only ONE setting jobNamespace: exactly one line, on that agent's
   # Config. This is the case that catches a template reading .Values instead of
   # $agent — such a read renders the value onto both Configs (or onto neither).
   cat >"$tmpdir/jobns-mixed.yaml" <<'EOF'
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     sentry:
       dsn: dummy
   agents:
     - name: ns-agent
       enabled: true
       assignee: ns
       image: bborbe/agent-claude
       heartbeat: 5m
       taskTypes: [llm]
       triggerPhases: [planning]
       triggerStatuses: [in_progress]
       jobNamespace: dev-agents-sandbox
     - name: plain-agent
       enabled: true
       assignee: plain
       image: bborbe/agent-claude
       heartbeat: 5m
       taskTypes: [llm]
       triggerPhases: [planning]
       triggerStatuses: [in_progress]
   EOF
   jobns_mixed_out="$tmpdir/jobns-mixed.out"
   "$helm_bin" template x "$CHART_DIR" -f "$tmpdir/jobns-mixed.yaml" --show-only templates/agents.yaml >"$jobns_mixed_out"
   jobns_mixed_docs="$(grep -c '^kind: Config$' "$jobns_mixed_out" || true)"
   jobns_mixed_lines="$(grep -c 'jobNamespace:' "$jobns_mixed_out" || true)"
   if [ "$jobns_mixed_docs" -ne 2 ]; then
   	echo "FAIL: job-namespace-mixed: expected 2 Config documents, got $jobns_mixed_docs"
   	fail=1
   elif [ "$jobns_mixed_lines" -ne 1 ]; then
   	echo "FAIL: job-namespace-mixed: expected exactly 1 jobNamespace line, got $jobns_mixed_lines"
   	fail=1
   elif ! awk 'BEGIN{RS="---"} index($0,"kind: Config") && index($0,"  name: ns-agent\n") && index($0,"  jobNamespace: \"dev-agents-sandbox\"\n"){found=1} END{exit(found?0:1)}' "$jobns_mixed_out"; then
   	echo "FAIL: job-namespace-mixed: the jobNamespace line is not on ns-agent's Config"
   	fail=1
   elif awk 'BEGIN{RS="---"} index($0,"kind: Config") && index($0,"  name: plain-agent\n") && index($0,"jobNamespace"){found=1} END{exit(found?0:1)}' "$jobns_mixed_out"; then
   	echo "FAIL: job-namespace-mixed: plain-agent's Config carries a jobNamespace line"
   	fail=1
   else
   	echo "OK: job-namespace-mixed"
   fi
   ```

   The heredoc body above is shown indented for readability inside this prompt — in the script the `cat >…<<'EOF'` terminator and every values line start in column 1, exactly like the existing `cat >"$tmpdir/kafka-incluster.yaml" <<'EOF'` blocks.

   The `kind: Config` count guard in both negative cases is load-bearing: an empty or failed render would otherwise satisfy "no `jobNamespace` line" vacuously. Recipe lines use TABs; `set -euo pipefail` is already in force, so a failing `helm template` must abort the run (do not mask it with `|| true` or a pipe).

7. **Self-check before finishing.** Re-read requirements 1–6 against the changed files, then run every command in `<verification>` and confirm each result matches its annotation.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- An install that sets no `jobNamespace` — and one that sets `jobNamespace: ""` — must render byte-identically to today. No `jobNamespace` line, no chart-side default, no fallback to `sandbox.namespace` or `<namespace>-agents-sandbox`.
- The field renders only when a non-empty per-agent value is set, read from the loop variable (`$agent`). Do not add a top-level `jobNamespace` value, a `sandbox.jobNamespace` value, or any fleet-wide default.
- Render the value quoted (`{{ . | quote }}`), matching `configMapName` / `configMapMountPath`.
- Do NOT edit `helm/crds/config-crd.yaml` or any other file under `helm/crds/`. The executor rewrites that schema at start; the chart's bundled copy is not this change's concern.
- Do NOT edit `specs/`, including `specs/in-progress/047-agent-sandbox-namespace.md`.
- Do NOT change any other template, any other `agents.yaml` block, any Go code, `tools.env`, `skills/`, `docs/`, or any `values-*.yaml` overlay (cluster overlays live in another repo).
- Existing tests must still pass, including every case already in `hack/test-helm-render.sh`.
- All actionable paths in this prompt are repo-relative. The agent-task-executor paths in `<context>` are citations only — that repo is not mounted; the snippet is inlined above. Scratch files go in `/tmp` only.
</constraints>

<verification>
Run `make test` from the repo root. It runs the Go tests and `bash hack/test-helm-render.sh`; it must exit 0, print `OK: job-namespace`, `OK: job-namespace-empty`, `OK: job-namespace-unset` and `OK: job-namespace-mixed` exactly once each, and end with `OK: helm render tests passed`.

Do NOT run `make precommit` for this change: it additionally pulls `trivy`, `osv-scanner` and `golangci-lint` over an unchanged Go tree, which adds no signal for a chart-only edit and can fail on tooling availability rather than on the change.

Then confirm the bookkeeping:

```bash
grep -q 'with \$agent.jobNamespace' helm/templates/agents.yaml
grep -q 'jobNamespace: {{ . | quote }}' helm/templates/agents.yaml
grep -q '^version: 0.22.1$' helm/Chart.yaml
grep -q 'jobNamespace' helm/values.yaml
grep -q 'jobNamespace' helm/README.md
grep -q 'v0.31.0' helm/README.md
grep -q 'DNS-1123' helm/README.md
grep -q 'renderAgentStorage' helm/README.md
grep -q 'jobNamespace' hack/test-helm-render.sh
test "$(grep -c '^## Unreleased$' CHANGELOG.md)" = 1
awk '/^## /{sec=$0} /jobNamespace/{print sec; exit}' CHANGELOG.md   # must print exactly: ## Unreleased
! grep -q 'jobNamespace' helm/crds/config-crd.yaml
```
</verification>
