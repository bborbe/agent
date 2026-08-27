---
status: completed
spec: [050-executors-per-vault-list]
summary: 'Bumped helm chart version 0.5.2→0.6.0, documented the per-vault executors list in helm/README.md, and added a feat(helm) executors entry to the CHANGELOG ## Unreleased section'
execution_id: agent-exec-212-spec-050-executors-version-and-docs
dark-factory-version: dev
created: "2026-08-27T10:57:00Z"
queued: "2026-08-27T11:29:20Z"
started: "2026-08-27T11:40:01Z"
completed: "2026-08-27T11:43:25Z"
branch: dark-factory/executors-per-vault-list
---

<summary>
- The chart version bumps 0.5.2 → 0.6.0 (additive, backward-compatible minor); `appVersion` stays `0.3.1`
- The README gains an `executors` section documenting the per-vault list, the mandatory `vaultName` (→ `VAULT_NAME`), the per-entry `topicPrefix` → `{prefix}-agent-task-v1-event`/`-request` topic shape, the empty-list legacy fallback, and the default-inheritance from `executor.*`
- Operators can discover the minimal entry shape (`name`, `vaultName`, `topicPrefix`, `kafkaBrokers`) and that executor mTLS stays shared via `executor.kafkaUser`
- The CHANGELOG gains an `## Unreleased` `feat(helm):` entry describing the per-vault `executors` list and the chart minor bump — required so the maintainer release bot can cut the release from the CHANGELOG
- The version bump and docs are the only changes — the rendered chart output is unchanged by this prompt
- The release tagging itself (renaming `## Unreleased`, cutting `vX.Y.Z` + `lib/vX.Y.Z`) remains the operator step, not part of this prompt
</summary>

<objective>
Record the per-vault `executors` capability in the chart's version, user documentation, and changelog so the release is cuttable and operators can discover the new list. This is prompt 3 of 3 — it depends on prompts 1 and 2 having landed (it documents the shipped field names and the rendering behavior).
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (single global `CHANGELOG.md` at repo root; chart changes recorded there with `feat(helm):`/`fix(helm):` prefixes and a `chart X→Y` suffix — see the `## v0.81.1` and `## v0.81.0` helm bullets).

Read these files IN FULL before editing:
- `/workspace/helm/Chart.yaml` — `version: 0.5.2` (line 6) → `0.6.0`; `appVersion: "0.3.1"` (line 8) stays.
- `/workspace/helm/README.md` — the `### executor` section ends before `### controller` (line ~114); the new `### executors` section goes between them, matching the README's existing table style.
- `/workspace/CHANGELOG.md` — top of file: SemVer preamble, then `## v0.82.1` (line 11). There is currently NO `## Unreleased` section.
- `/workspace/docs/kafka-schema-design.md` — the topic naming convention (`{branch}-{group}-{kind}-{version}-{action}`; `agent-task-v1-event` consumed by the executor, `agent-task-v1-request` published by agents) that the README section documents.
- `/workspace/helm/values.yaml` — the `executors` example block added by prompt 1 (the README documents the same field names).

Coding-plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — entry format (`- <prefix>: <what> [context]`, prefix required), verb style, `## Unreleased` placement rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/documentation-guide.md` — how the README values-reference sections are structured.

Preconditions (prompts 1 and 2 must have landed): `grep -n '^executors:' /workspace/helm/values.yaml` returns a line, and `helm template helm/ -f <any executors fixture>` renders per-vault `agent-task-executor-<name>` Deployments. If either is missing, STOP and report a precondition failure — do NOT re-implement the schema or template.
</context>

<requirements>

## 1. Bump the chart version in `/workspace/helm/Chart.yaml`

Change `version: 0.5.2` → `version: 0.6.0`. Change NOTHING else in the file. `appVersion: "0.3.1"` must stay exactly as-is. Do not touch the comment lines or any other field.

## 2. Document the `executors` list in `/workspace/helm/README.md`

Insert a new `### executors (per-vault, values-driven)` section between the end of the `### executor` section and the `### controller` heading (line ~114). Match the README's existing values-reference style (a short intro paragraph, then a field table like the `### executor` section). The section MUST satisfy all of the following:

- Intro (plain language): `executors` is a values-driven list; each enabled entry renders one per-vault Deployment `agent-task-executor-<name>` with `replicas: 1`; one executor per vault means one consumer group, one offset domain, and one dedup state per vault — adding a vault is a values change, not a chart code change. When the list is empty (default) the single legacy `agent-task-executor` Deployment renders unchanged.
- A values-reference table covering every per-entry field, with the default/fallback column stating it falls back to `executor.*`:
  - `executors[].name` — required; Deployment suffix + `vault` pod label.
  - `executors[].enabled` — default `true`.
  - `executors[].vaultName` — required; becomes the `VAULT_NAME` env; slug-constrained `^[a-z][a-z0-9-]*$` validated by the executor binary (>= v0.7.0) at startup, NOT the chart; empty rejected at render time. (The literal string `vaultName` MUST appear in the section for the AC grep.)
  - `executors[].branch` — default `executor.branch`.
  - `executors[].topicPrefix` — default `executor.topicPrefix`; passes through verbatim.
  - `executors[].kafkaBrokers` — required when not inherited; default `executor.kafkaBrokers`.
  - `executors[].image.repository` / `image.tag` — default `executor.image` (empty tag => `appVersion`).
  - `executors[].logLevel` — default `executor.logLevel`.
  - `executors[].sentry.proxy` — default `executor.sentry.proxy`.
  - `executors[].existingSecret` — default `executor.existingSecret`, then the shared `agent-task-executor` Secret (key `sentry-dsn`).
  - `executors[].podSecurityContext` / `securityContext` / `resources` — default `executor.*`.
- A short paragraph on the per-vault topic shape that MUST literally contain `agent-task-v1-event` (for the AC grep): with a per-entry `topicPrefix` of `{stage}-{vault}`, the executor consumes `{prefix}-agent-task-v1-event` and publishes `{prefix}-agent-task-v1-request` (per `docs/kafka-schema-design.md`); the chart passes `TOPIC_PREFIX` through verbatim and does not derive the prefix.
- A short paragraph on the empty-list fallback and authoritative list mode: `executors: []` (default) + `executor.enabled: true` → single legacy `agent-task-executor` Deployment with the same env contract as before (no `VAULT_NAME`); a non-empty list is authoritative — the legacy Deployment is not rendered regardless of `executor.enabled`; an all-disabled list renders no executor Deployment.
- A minimal-entry YAML example (code block) showing just `name`, `vaultName`, `topicPrefix`, `kafkaBrokers`.
- A note that there is no per-entry `kafkaUser` — executor mTLS stays driven by the single shared `executor.kafkaUser` block for all per-vault executors.

## 3. Add the `## Unreleased` CHANGELOG entry in `/workspace/CHANGELOG.md`

Insert a new `## Unreleased` section immediately after the SemVer preamble (`* PATCH version when you make backwards-compatible bug fixes.`) and immediately before the existing `## v0.82.1` heading. There is currently NO `## Unreleased` section — if (unexpectedly) one already exists from another in-flight prompt, append the bullet to it instead of adding a second section.

One bullet, prefix `feat(helm):`, containing the literal word `executors` (for the AC grep). Model the shape on the existing v0.81.0/v0.81.1 helm bullets (describe the behavior + `chart 0.5.2→0.6.0` suffix). Suggested wording (adapt freely, keep the literal `executors` and the chart-version suffix):

```markdown
- feat(helm): add a values-driven `executors` list rendering one per-vault `agent-task-executor-<name>` Deployment per enabled entry — `VAULT_NAME` from `vaultName`, per-entry `TOPIC_PREFIX` selecting the `{prefix}-agent-task-v1-event`/`-request` topics; empty list keeps the single legacy executor unchanged; chart 0.5.2→0.6.0
```

Notes:
- Do NOT rename any version heading. Renaming `## Unreleased` → `## v0.82.0` and cutting the paired `vX.Y.Z` + `lib/vX.Y.Z` tags are the spec's operator step (release), NOT part of this prompt.
- Do NOT create or push any tag.

## 4. Scope containment

Edit ONLY `/workspace/helm/Chart.yaml`, `/workspace/helm/README.md`, and `/workspace/CHANGELOG.md`. Do NOT touch any helm template, `helm/values.yaml`, or any Go file.

## 5. Self-check

Before finishing, re-run the `<verification>` commands below and confirm each passes; walk each acceptance criterion in `/workspace/specs/in-progress/050-executors-per-vault-list.md` that this prompt owns (AC 5, 6, 8) against the final files. After `make precommit`, confirm `git status --porcelain` shows ONLY the three intended files (`helm/Chart.yaml`, `helm/README.md`, `CHANGELOG.md`) — if precommit modified any other file (e.g. a Go file via gofmt), revert the drift and report it; the change must not touch anything beyond scope.
</requirements>

<constraints>
- Chart version is `0.6.0` (additive minor — the change is backward-compatible); `appVersion` unchanged (`0.3.1`).
- This spec changes exactly five files; this prompt changes ONLY `helm/Chart.yaml`, `helm/README.md`, and `CHANGELOG.md`.
- Do NOT add per-entry `kafkaUser` (or `storage`, `gitRestUrl`, `taskDir`, `pollInterval`, `autoInjectTaskIdentifier`, `gatewaySecret`) to the documented schema — controller-only or not yet needed.
- Do NOT document chart-side regex validation of `vaultName` — slug validation is the executor binary's runtime job; the chart only rejects an empty value at render time.
- Do NOT document a chart-derived `{stage}-{vault}` topic prefix — `TOPIC_PREFIX` passes through verbatim.
- No Go code changes; `make precommit` must still pass.
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

# --- AC 5: chart version ---
grep -n '^version:' helm/Chart.yaml
# Must print: version: 0.6.0
grep -n '^appVersion:' helm/Chart.yaml
# Must print: appVersion: "0.3.1"

# --- AC 6: README documents the list ---
grep -n 'agent-task-v1-event' helm/README.md
# Must print >= 1 line, inside the executors section.
grep -n 'vaultName' helm/README.md
# Must print >= 1 line, inside the executors section.
grep -n '^### executors' helm/README.md
# Must print 1 line (the section heading exists).

# --- AC 8: CHANGELOG Unreleased entry ---
grep -n 'executors' CHANGELOG.md
# Must print >= 1 line with a line number strictly less than the '## v0.82.1' line (i.e. inside the top Unreleased section).
sed -n '1,/^## v0.82.1/p' CHANGELOG.md
# Must show "# Changelog" ... "## Unreleased" as the top version section (no released heading above it).

# --- Render unchanged by version/docs edits (smoke): default still one legacy Deployment ---
# (A bare `helm template helm/` exits 1 — namespace/kafkaBrokers/existingSecret are
# required by the chart — so render with a legacy fixture instead.)
cat > /tmp/executors-legacy-smoke.yaml <<'EOF'
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
EOF
helm template helm/ -f /tmp/executors-legacy-smoke.yaml | grep -B1 -A2 'kind: Deployment' | grep -c 'name: agent-task-executor$'
# Must print 1.

# --- AC 7 (smoke): helm lint passes with default values and with an executors-set file ---
cat > /tmp/executors-lint.yaml <<'EOF'
namespace: test
image:
  registry: docker.io
executor:
  enabled: true
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
executors:
  - name: openclaw
    enabled: true
    vaultName: openclaw
    topicPrefix: develop-openclaw
    kafkaBrokers: kafka:9092
  - name: personal
    enabled: true
    vaultName: personal
    topicPrefix: develop-personal
    kafkaBrokers: kafka:9092
EOF
helm lint helm/ -f /tmp/executors-legacy-smoke.yaml
# Must exit 0 with "0 chart(s) failed".
helm lint helm/ -f /tmp/executors-lint.yaml
# Must exit 0 with "0 chart(s) failed".

# --- Repo gate: no Go code changed, so the tree must still pass ---
make precommit
# Must exit 0. If the Makefile's ROOTDIR derivation (`git rev-parse --show-toplevel`)
# fails because .git is masked in the container, re-run as: ROOTDIR=/workspace make precommit
```
</verification>
