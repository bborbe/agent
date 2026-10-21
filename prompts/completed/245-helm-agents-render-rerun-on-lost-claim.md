---
status: completed
execution_id: agent-rerun-on-lost-claim-render-exec-245-helm-agents-render-rerun-on-lost-claim
dark-factory-version: v0.198.7
created: "2026-10-21T20:40:00Z"
queued: "2026-10-21T20:31:00Z"
started: "2026-10-21T20:31:10Z"
completed: "2026-10-21T20:39:41Z"
---

# feat(helm): render per-agent rerunOnLostClaim on the agent Config

<summary>
- An agent entry in the chart values can now say `rerunOnLostClaim: false` (or `true`), and the chart renders it onto the agent's Config. It only takes effect once the Config CRD declares the field and the executor honours it, so a runner can then opt out of being re-run after it loses its claim on a task
- `false` is rendered as well as `true`. The neighbouring optional fields drop a `false` value, so this field checks whether the key is present instead of whether it is truthy
- An agent that does not set the key renders exactly as it does today, so no existing agent's Config changes
- The repo gets its first chart render test. `make test` (and so `make precommit` and CI) renders the chart three ways and fails if `false`, `true` or the unset case renders wrongly
- The test fails on the original bug: a truthiness check that drops `false` is caught
- The values example, the chart README, the chart version and the changelog are updated
- No CRD, Go code or agent image change. The CRD schema for the field is owned by a separate change
</summary>

<objective>
Add a `rerunOnLostClaim` passthrough to `helm/templates/agents.yaml` so a values overlay can set `spec.rerunOnLostClaim: false` on chart-rendered runner Configs. Without it, a re-run after a lost claim can start a second claude/pi/goreleaser session. The key must render whenever it is present in the agent entry, including `false`, and a repo test reachable from `make test` must prove the false/true/unset renders.
</objective>

<context>
Read `CLAUDE.md` for project conventions: a single global `CHANGELOG.md` at the repo root, and the root `Makefile` is the only Makefile (there are no per-dir Makefiles). Read `docs/dod.md`: a config change updates the README and adds a CHANGELOG entry under `## Unreleased`.

Files to read before editing (anchor on the names given, not on line numbers):

- `helm/templates/agents.yaml` renders one `Config` CR per enabled `agents` entry under `spec:` at 2-space indent. The relevant existing blocks are:
  ```yaml
    {{- if $agent.maxConcurrentJobs }}
    maxConcurrentJobs: {{ $agent.maxConcurrentJobs }}
    {{- end }}
    {{- if $agent.zombieJobTimeoutSeconds }}
    zombieJobTimeoutSeconds: {{ $agent.zombieJobTimeoutSeconds }}
    {{- end }}
  ```
  Do NOT copy this `{{- if $agent.X }}` shape. `false` is falsy in Go templates, so it would never render `rerunOnLostClaim: false`. The chart's presence-test precedent is `hasKey`. It is used in the same file (`hasKey ($agent.env | default dict) "SENTRY_PROXY"`) and in `helm/templates/executor-deployment.yaml` (`{{- if hasKey $e "runUntagged" }}`).
- `helm/values.yaml` contains the commented `agents:` example (`agents: []` followed by `#` lines). It already documents `zombieJobTimeoutSeconds` directly after the `heartbeat:` line. Mirror that style.
- `helm/README.md`, section `### agents (leaf agents — values-driven)`. Its YAML example documents `zombieJobTimeoutSeconds` after `heartbeat:`, and a prose paragraph about that deadline follows the code block.
- `helm/Chart.yaml` is currently `version: 0.14.0`.
- `CHANGELOG.md` already has a `## Unreleased` section with one `feat(helm):` bullet ending `chart 0.13.0→0.14.0`.
- `helm/crds/config-crd.yaml` does NOT declare `rerunOnLostClaim`. Another change owns the CRD schema. Do not touch it. Until a Config CRD that declares the field is installed, the API server prunes the field. That is expected, and it is not this prompt's concern.
- `Makefile` (root). The `test` target is `.PHONY: test` / `test:` running `go test -mod=mod ...`. `precommit: ensure format generate test check addlicense`. CI (`.github/workflows/ci.yml`) runs `make precommit`.
- `hack/check-build-git-version.sh` is the repo's existing shell-check style: BSD license header comment, `set -euo pipefail`, `cd` to the repo root via `$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)`, `FAIL:` / `OK:` messages, and a non-zero exit on failure. Follow it for the new script.

Helm availability: the YOLO container does NOT ship `helm`, and `get.helm.sh` is blocked. The repo's working precedent (`prompts/completed/227-render-per-agent-job-deadline.md`, `242-helm-cluster-assigner-cronjob.md`) installs it from the Go proxy with `go install helm.sh/helm/v3/cmd/helm@v3.16.4`. Developer machines may have helm v4 on PATH. `helm template <release> <chart> -f <values> --show-only templates/agents.yaml` works in both v3 and v4.

These minimal base values were verified to render the chart today, with one agent producing exactly one `kind: Config` document under `--show-only templates/agents.yaml`:
```yaml
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
agents:
  - name: rr-agent
    enabled: true
    assignee: rr
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
```
</context>

<requirements>
1. **Template: `helm/templates/agents.yaml`.** Insert this immediately after the existing `zombieJobTimeoutSeconds` block (after its `{{- end }}`), at the same 2-space `spec` indent:
   ```yaml
     {{- if hasKey $agent "rerunOnLostClaim" }}
     rerunOnLostClaim: {{ $agent.rerunOnLostClaim }}
     {{- end }}
   ```
   - The value renders bare (no `| quote`), so a YAML bool `false`/`true` renders as `rerunOnLostClaim: false` / `rerunOnLostClaim: true`.
   - Do not add a chart-side default. An agent without the key renders no `rerunOnLostClaim` line at all.
   - Do not change any other block in the file, including the existing `maxConcurrentJobs` / `zombieJobTimeoutSeconds` truthiness checks.

2. **Render test script: create `hack/test-helm-render.sh`** (mode `0755`, bash, the license header and structure of `hack/check-build-git-version.sh`). Contract:
   - `set -euo pipefail`, then `cd` to the repo root the same way the existing hack script does.
   - Chart dir: `CHART_DIR="${CHART_DIR:-helm}"`. Verification uses the override to run the test against a mutated scratch copy of the chart.
   - Helm resolution: if `helm` is on `PATH`, use it (v3 or v4 both fine). Otherwise run `go install helm.sh/helm/v3/cmd/helm@v3.16.4` and use `"$(go env GOPATH)/bin/helm"`. Keep the version in a script-local variable `HELM_VERSION=v3.16.4`. Do NOT add it to `tools.env`, because that file is a canonical cross-repo copy. If the install fails, the script fails (no silent skip).
   - Scratch files go in `tmpdir="$(mktemp -d)"` with `trap 'rm -rf "$tmpdir"' EXIT`. Never write inside the repo.
   - Write three values files: the base values from `<context>` with one agent entry, differing only in the agent's `rerunOnLostClaim` key:
     - `false.yaml`: `rerunOnLostClaim: false`
     - `true.yaml`: `rerunOnLostClaim: true`
     - `unset.yaml`: no `rerunOnLostClaim` key
   - For each, run `"$helm" template x "$CHART_DIR" -f <file> --show-only templates/agents.yaml > <out>`. A non-zero render exit fails the script (guaranteed by `set -e`; do not mask it with `|| true` or a pipe).
   - Assertions. Each failure prints `FAIL: <case>: <what>` and makes the script exit non-zero at the end. Each pass prints `OK: <case>`.
     - In every case, the output contains exactly one Config document: `grep -c '^kind: Config$'` equals `1`. This guards against an empty render passing the unset case.
     - false case: `grep -qx '  rerunOnLostClaim: false' <out>`
     - true case: `grep -qx '  rerunOnLostClaim: true' <out>`
     - unset case: `! grep -q 'rerunOnLostClaim' <out>`
   - Use `grep -q` / `grep -qx` with `if` / `!` for the assertions. Never treat `grep -c` printing `0` as a pass, because `grep -c` exits 1 on zero matches under `set -e`. Count with `grep -c ... || true` captured into a variable when you need the number.
   - End with `OK: helm render tests passed` on success.

3. **Wire into `make test`: root `Makefile`.** Add a target and make `test` depend on it, so `make test` (dark-factory's `testCommand`), `make precommit` and CI all run it:
   ```make
   .PHONY: test
   test: test-helm
   	go test -mod=mod -p=$${GO_TEST_PARALLEL:-1} -cover -race $(shell go list -mod=mod ./... | grep -v /vendor/)

   .PHONY: test-helm
   test-helm:
   	bash hack/test-helm-render.sh
   ```
   Keep the existing `go test` recipe line byte-identical. Only add the `test-helm` prerequisite and the new target. Recipe lines use a TAB.

4. **Values docs: `helm/values.yaml`.** In the commented `agents:` example, add a commented entry directly after the `zombieJobTimeoutSeconds` comment lines, in the same column style:
   ```
     #   rerunOnLostClaim: false       # OPTIONAL: Config.spec.rerunOnLostClaim. `false` stops
     #                                 # the executor re-running a task after this agent's
     #                                 # claim is lost (no second session). Rendered whenever
     #                                 # the key is set, `false` included; omit for the
     #                                 # executor default.
   ```
   `agents` stays `[]`. Add no uncommented key.

5. **README: `helm/README.md`.** In `### agents (leaf agents — values-driven)`, add this to the YAML example directly after the three `zombieJobTimeoutSeconds` lines:
   ```
       rerunOnLostClaim: false           # OPTIONAL: `false` = never re-run a task after this
                                         # agent's claim is lost. Rendered whenever set
                                         # (including false); omit for the executor default.
   ```
   Then add one sentence after the existing deadline paragraph below the code block. It should say that interactive or session-holding runners (claude, pi, goreleaser) should set `rerunOnLostClaim: false`, so that a lost claim cannot start a second session. It should also say that the field needs a Config CRD that declares `spec.rerunOnLostClaim`, because older CRDs prune it. Do not restructure other README content.

6. **Chart version: `helm/Chart.yaml`.** `version: 0.14.0` → `version: 0.14.1`. This is a patch bump for a newly rendered optional per-agent field, matching the `maxConcurrentJobs` / `zombieJobTimeoutSeconds` precedents. Leave `appVersion` unchanged.

7. **`CHANGELOG.md`.** Append one bullet to the EXISTING `## Unreleased` section, after its current bullet and before `## v0.114.0`. Do not add a second `## Unreleased` heading. Keep a blank line before `## v0.114.0`. Use plain backticks:
   `- feat(helm): render \`rerunOnLostClaim\` on the agent Config whenever \`agents[].rerunOnLostClaim\` is set, \`false\` included (presence test via \`hasKey\`, not truthiness), as a bare bool; nothing renders when unset, so no existing agent's Config changes; new \`hack/test-helm-render.sh\`, run by \`make test\`, asserts the false/true/unset renders; chart 0.14.0→0.14.1. Takes effect only with a Config CRD that declares \`spec.rerunOnLostClaim\` (older CRDs prune the field) and an agent-task-executor that reads it; older executors ignore the field.`

8. Self-check: before finishing, run every command in `<verification>` and confirm each result matches its annotation. Then walk requirements 1–7 and every `<constraints>` bullet against the changed files.
</requirements>

<constraints>
- Do NOT commit. dark-factory handles git.
- Existing tests must still pass.
- Do NOT edit anything under `helm/crds/`. The Config CRD schema for `rerunOnLostClaim` is owned by a separate change.
- The key renders whenever it is PRESENT in the agent entry, including `false`. Use `hasKey $agent "rerunOnLostClaim"`, never `{{ if $agent.rerunOnLostClaim }}`. The value renders as a bare bool, never quoted.
- An agent that does not set the key must render exactly as today (no `rerunOnLostClaim` line, no chart-side default).
- Do NOT change any other template, other `agents.yaml` block, Go code, agent image, or `tools.env`.
- Do NOT create or edit any `values-*.yaml` overlay. Cluster overlays live in another repo.
- The new test must be reachable from `make test`, must fail loudly when helm cannot be obtained or a render fails, and must write scratch files only to a `mktemp -d` dir.
- All paths in this prompt are repo-relative. Scratch files go in `/tmp` only.
</constraints>

<verification>
Run under bash, in ONE shell invocation:

```bash
set -o pipefail; trap 'echo "VERIFY FAIL at line $LINENO"' ERR

# 1. the new test passes on the edited chart
bash hack/test-helm-render.sh                                     # must exit 0 and print "OK: helm render tests passed"

# 2. the test catches the original bug: mutate a scratch copy to the truthiness pattern; the test must FAIL
rm -rf /tmp/rr-mut && cp -r helm /tmp/rr-mut
sed -i 's/if hasKey \$agent "rerunOnLostClaim"/if $agent.rerunOnLostClaim/' /tmp/rr-mut/templates/agents.yaml
grep -q 'if \$agent.rerunOnLostClaim }}' /tmp/rr-mut/templates/agents.yaml   # must succeed: the mutation was applied
out="$(CHART_DIR=/tmp/rr-mut bash hack/test-helm-render.sh 2>&1)" && echo "MUTATION NOT CAUGHT"; printf '%s\n' "$out" | grep -q '^FAIL: false'   # must succeed: the false case fails on the mutated chart (and nothing printed MUTATION NOT CAUGHT)
rm -rf /tmp/rr-mut

# 3. template uses the presence test, not truthiness
grep -q 'if hasKey \$agent "rerunOnLostClaim"' helm/templates/agents.yaml
! grep -q 'if \$agent.rerunOnLostClaim' helm/templates/agents.yaml

# 4. make test runs the helm test
grep -qE '^test: test-helm' Makefile
grep -q 'bash hack/test-helm-render.sh' Makefile

# 5. CRDs untouched (no rerunOnLostClaim added by this prompt)
! grep -q 'rerunOnLostClaim' helm/crds/config-crd.yaml

# 6. bookkeeping
grep -q '^version: 0.14.1$' helm/Chart.yaml
awk '/^## /{sec=$0} /rerunOnLostClaim/{print sec}' CHANGELOG.md   # must print exactly one line: "## Unreleased"
test "$(grep -c '^## Unreleased$' CHANGELOG.md)" = 1
grep -q 'rerunOnLostClaim' helm/README.md
grep -q 'rerunOnLostClaim' helm/values.yaml
grep -q '^agents: \[\]$' helm/values.yaml
```

Then run `ROOTDIR=/workspace make precommit`. It must pass, and its output must include `OK: helm render tests passed` (the `test` target now runs the render test).
</verification>
