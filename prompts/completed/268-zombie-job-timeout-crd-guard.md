---
status: completed
summary: Added a render-time guard rejecting agents[].zombieJobTimeoutSeconds below the Config CRD minimum (read from the CRD), with four render test cases, chart bump to 0.23.25, docs and changelog updates
execution_id: agent-zombie-job-timeout-guard-exec-268-zombie-job-timeout-crd-guard
dark-factory-version: v0.199.2
created: "2026-11-26T19:30:00Z"
queued: "2026-11-26T18:47:01Z"
started: "2026-11-26T18:47:13Z"
completed: "2026-11-26T18:54:36Z"
---

# Render-time guard for zombieJobTimeoutSeconds against the CRD minimum

<summary>
- A values overlay that sets `zombieJobTimeoutSeconds` below the Config CRD's declared minimum now fails at `helm template` time, not at apply time.
- The failure message names the agent, the field and the offending value.
- A valid value renders verbatim, unchanged.
- The guard's minimum is read from the CRD at render time, so it cannot drift from the CRD.
- `zombieJobTimeoutSeconds: 0` is rejected instead of being silently dropped as if unset.
- The new render cases run in CI as part of the existing chart render test suite.
- The values example, the chart README, the chart version and the changelog are updated.
</summary>

<objective>
Make the Helm chart reject an out-of-range `zombieJobTimeoutSeconds` at render time with a message pointing at the offending values entry, instead of letting the value reach the cluster and fail later as a CRD admission error far from the values file.
</objective>

<context>
Read `CLAUDE.md` for repo conventions: a single global `CHANGELOG.md` at the repo root, and the root `Makefile` is the build entry point (it includes `Makefile.docker`).
Read `docs/dod.md` — the repo's Definition of Done (the daemon's `validationPrompt`); it requires a README update when usage/configuration changes and a `CHANGELOG.md` entry under `## Unreleased`.
Read `specs/completed/043-executor-zombie-job-detection.md` — the authority for the `30` floor; this prompt only moves that failure earlier.
Read `helm/templates/agents.yaml` — the `agents` range renders `ConfigSpec` fields. `rerunOnLostClaim` uses `hasKey $agent "rerunOnLostClaim"`, the pattern to copy. `zombieJobTimeoutSeconds` currently uses a truthiness test `{{- if $agent.zombieJobTimeoutSeconds }}`, which is falsy for `0` and drops the field before any nested guard could see it.
Read `helm/crds/config-crd.yaml` — `spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.zombieJobTimeoutSeconds` declares `{type: integer, minimum: 30}`.
Read `hack/test-helm-render.sh` — the render harness. `write_values` and `domains_failfast` show the existing patterns; note `run_case` is hardcoded to assert `rerunOnLostClaim` lines and cannot be reused as-is.
Read `helm/values.yaml` and `helm/README.md` — both document `zombieJobTimeoutSeconds` (the values comment and the `### agents` YAML example, plus a prose paragraph), the places the DoD's README rule applies.
</context>

<requirements>
1. In `helm/templates/agents.yaml`, replace the `zombieJobTimeoutSeconds` passthrough so the guard sits OUTSIDE any truthiness test. Use this verified shape:
   - Place these three lines at the top of the file, before `{{- range $agent := .Values.agents }}`:
     ```
     {{- $crd := .Files.Get "crds/config-crd.yaml" | fromYaml }}
     {{- $v := index $crd.spec.versions 0 }}
     {{- $min := int $v.schema.openAPIV3Schema.properties.spec.properties.zombieJobTimeoutSeconds.minimum }}
     ```
     `.Files.Get` on `crds/` is verified to work. The `int` cast is required: `fromYaml` yields `float64`, and `lt` fails with "incompatible types for comparison: int and float64" without it.
   - In the agent body, gate on `hasKey $agent "zombieJobTimeoutSeconds"` (NOT truthiness) so `0` is seen, then fail below the minimum:
     ```
     {{- if hasKey $agent "zombieJobTimeoutSeconds" }}
     {{- if lt (int $agent.zombieJobTimeoutSeconds) $min }}
     {{- fail (printf "agents[%s].zombieJobTimeoutSeconds=%v is below the CRD minimum %d" $agent.name $agent.zombieJobTimeoutSeconds $min) }}
     {{- end }}
     zombieJobTimeoutSeconds: {{ $agent.zombieJobTimeoutSeconds }}
     {{- end }}
     ```
2. Add the four cases to `hack/test-helm-render.sh`. `run_case` is hardcoded to `rerunOnLostClaim`, so add two helpers in the same style: `run_render_ok <case> <values-file> <expected-line>` (render exits 0 and `grep -qx` the line, keeping the `grep -c '^kind: Config$' == 1` guard) and `run_render_fails <case> <values-file> <message-fragment>` (render exits non-zero and stderr contains the fragment, like `domains_failfast`). Cases:
   - `zombieJobTimeoutSeconds: 10` → render exits non-zero and stderr contains `agents[<name>].zombieJobTimeoutSeconds=10 is below the CRD minimum 30`, so the agent/value interpolation is covered.
   - `zombieJobTimeoutSeconds: 60` → render succeeds and emits `  zombieJobTimeoutSeconds: 60` (two leading spaces, the rendered `spec` indent; have `run_render_ok` match the indented line, as `run_case` does with `grep -qx '  rerunOnLostClaim: false'`).
   - `zombieJobTimeoutSeconds: 0` → render exits non-zero with the guard message. Against the current chart this case exits 0 and emits no key at all, so this is the regression lock for the falsy trap.
   - CRD-tracking probe: copy `helm/` to `$tmpdir/chart-crd` (the script's `mktemp -d` + `trap`, never inside the repo), rewrite the copy's `crds/config-crd.yaml` `minimum: 30` → `minimum: 45` with a non-`-i` `sed` (redirect to a temp file, then move — `sed -i` differs between BSD and GNU), then render the copy with `zombieJobTimeoutSeconds: 40` — must fail with the guard message (min 45); render `35` against the unmodified `helm/` — must succeed. Render both with `--show-only templates/agents.yaml`. This proves the guard reads the CRD rather than a copied constant.
3. Release record (repo Definition of Done — `docs/dod.md`):
   - `helm/Chart.yaml`: `version: 0.23.24` → `version: 0.23.25`. Leave `appVersion` unchanged. Add a `# 0.23.25 …` line to the version-history comment block directly above `version:` (the convention the 0.23.24 bump followed), naming the render-time `zombieJobTimeoutSeconds` guard.
   - `helm/values.yaml` and `helm/README.md`: in the `zombieJobTimeoutSeconds` comment, the `### agents` YAML example and the README prose paragraph, state that a value below `30` (the Config CRD minimum) now fails at `helm template` time with a message naming the agent and value, and that `0` is rejected rather than treated as unset.
   - `CHANGELOG.md`: there is no `## Unreleased` section today (the head is `## v0.148.3` at `origin/master` `705554a`; use whatever the current top released heading is). Insert `## Unreleased` directly above that heading with a blank line after it, then this bullet:
     - feat(helm): reject a `zombieJobTimeoutSeconds` below the Config CRD minimum (30) at render time, naming the agent and value, instead of failing later at admission; the minimum is read from `helm/crds/config-crd.yaml` so it cannot drift; render cases in `hack/test-helm-render.sh` cover below-min, valid, `0`, and CRD-tracking; chart 0.23.24→0.23.25.
4. Scope: only `zombieJobTimeoutSeconds` is guarded. `zombieSweeperIntervalSeconds` (min 10) is not rendered by this chart, and `maxConcurrentJobs` / `runBudgetSeconds` / `tokenCeiling` (min 0) drop `0` harmlessly because `0` equals their floor. Do not change those blocks.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Do NOT add a chart-side default for `zombieJobTimeoutSeconds`.
- Do NOT touch `helm/crds/config-crd.yaml` — the `30` floor is owned by spec 043.
- Existing render tests must still pass.
- Cite template anchors by content/name; line numbers only as hints.
- Repo-relative paths only.
</constraints>

<verification>
ROOTDIR=/workspace make test
</verification>
