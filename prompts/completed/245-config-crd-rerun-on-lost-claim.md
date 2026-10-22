---
status: completed
execution_id: agent-chart-rerun-on-lost-claim-crd-exec-245-config-crd-rerun-on-lost-claim
dark-factory-version: v0.198.7
created: "2026-10-21T20:49:08Z"
queued: "2026-10-21T20:49:08Z"
started: "2026-10-21T20:49:32Z"
completed: "2026-10-21T20:53:01Z"
---
# feat(helm): Config CRD declares rerunOnLostClaim

<summary>
- The Config CRD the chart ships now declares `spec.rerunOnLostClaim`, so a fresh install starts with the field instead of pruning it — effective once the executor release carrying the field ships, since the current executor release would prune it again at startup
- The field is a plain on/off switch with no value baked into the schema — the executor program is the authority on its shape and re-applies that shape whenever it starts
- Leaving the field out keeps today's behaviour (run the task again), so no existing agent changes
- Existing installs are unchanged — the cluster's copy of the schema is written by the executor at startup, and a chart upgrade never rewrites it
- Chart version and changelog are updated
</summary>

<objective>
Add `spec.rerunOnLostClaim` to the Config CRD schema in `helm/crds/config-crd.yaml` as `{type: boolean}` with no schema-level `default:`, matching `configSpecProperties()` in `agent-task-executor` (`pkg/k8s_connector.go`) byte for byte in shape. This keeps the chart's copy of the CRD identical to the schema the executor binary installs, so a fresh `helm install` starts correct and the two definitions cannot drift.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single global `CHANGELOG.md` at repo root, no per-module changelog).

Read these files before editing; anchor on the names given, not on line numbers:

- `helm/crds/config-crd.yaml` — the Config CRD. Under `spec.versions[0].schema.openAPIV3Schema.properties.spec.properties`, the tail of the property list today is `priorityClassName`, `imagePullSecret`, `zombieSweeperIntervalSeconds`, `zombieJobTimeoutSeconds`, `maxConcurrentJobs`, then `trigger:`. The file already carries this comment above `type:`:
  ```
  # Absent means "job", so existing Configs are unaffected. Keep in sync
  # with configSpecProperties() in agent-task-executor/pkg/k8s_connector.go
  # — that binary overwrites this schema on every executor start.
  ```
  `preserveUnknownFields: false` is set at the `spec:` level, which is why an undeclared property is pruned.
- `helm/Chart.yaml` — `version: 0.14.0`.
- `CHANGELOG.md` — **there is no `## Unreleased` section**; the top heading is the most recent release (`## v0.115.0`). Helm entries use `- feat(helm): ... chart X→Y.`.
- `prompts/completed/244-helm-cluster-assigner-sun-secret.md` — the previous chart prompt, and the source of the render-check and helm-install patterns reused below.
- Provenance: `agent-task-executor` spec `010-config-rerun-on-lost-claim` (in progress on `feat/claim-client`) lists the Helm chart CRD as a non-goal — "Helm chart CRD in `bborbe/agent` is another repo → follow-up named \"sync rerunOnLostClaim into bborbe/agent helm/crds/config-crd.yaml\"" — and the executor prompt generated from it adds the constraint "Do NOT modify the Helm chart CRD (it lives in another repo)". Its Failure Modes table gives the done-condition: `grep -n rerunOnLostClaim helm/crds/config-crd.yaml` in `bborbe/agent` returns ≥ 1 line. That repo is not mounted here — do not search for it.
- Merge-order note: a sibling prompt in another worktree (`feat/chart-rerun-on-lost-claim-render`, templates/values/README) already bumps `helm/Chart.yaml` 0.14.0 → 0.14.1 and adds a `## Unreleased` bullet. Whichever merges second reconciles the version and the `## Unreleased` section — if that one lands first, this prompt's bump becomes 0.14.1 → 0.15.0 (and requirement 4's existing-heading branch applies).
</context>

<requirements>
1. **Install helm, snapshot before editing, and write the render base.** The container does not ship helm. Install it, then copy the unmodified chart and write the minimal values file the chart needs to render at all — `namespace` is empty by default and `executor.enabled: true` makes `executor.kafkaBrokers` / `executor.sentry.dsn` required, so without it every `helm template` in `<verification>` exits 1 and emits no CRD:

   `rm -rf /tmp/crd-orig && cp -r helm /tmp/crd-orig`

   ```yaml
   # /tmp/crd-base.yaml
   namespace: test
   executor:
     kafkaBrokers: kafka:9092
     sentry:
       dsn: dummy
   ```

2. **`helm/crds/config-crd.yaml` — add the property.** Insert exactly these two lines (the comment and the property) directly after the `maxConcurrentJobs` entry and before `trigger:`, preserving the file's existing indentation and its inline-flow style:
   ```yaml
   # Optional; unset means true. false parks a taken-over task at human_review.
   rerunOnLostClaim: {type: boolean}
   ```
   Add no `default:`, no `nullable:`, no `description:` key and no other property. The comment wording mirrors the executor's own comment on the same field.

3. **`helm/Chart.yaml`.** `version: 0.14.0` → `version: 0.15.0`. Leave `appVersion` and `sources:` unchanged.

4. **`CHANGELOG.md`.** There is no `## Unreleased` heading today — create one directly above the top released `## v...` section (blank line before and after, as the file spaces headings), then add one bullet under it. If a release lands before this runs and the heading already exists, add the bullet to it instead. Never create a second `## Unreleased`; never touch a released section:
   `- feat(helm): the Config CRD in \`helm/crds/config-crd.yaml\` now declares \`spec.rerunOnLostClaim\` as \`{type: boolean}\` with no schema default, matching the executor binary's \`configSpecProperties()\` — unset means true, so existing Configs are unaffected; keeps a fresh install's CRD identical to the one the executor installs; chart 0.14.0→0.15.0.`
   (In the file, use plain backticks, not the escaped form above.)

5. **Render checks — run every one yourself; they are the tests for this change.** Scratch files under `/tmp` only. The exact commands and expected outcomes are in `<verification>`. They cover: the property is present in the rendered CRD with the right type; no `default:` sits under it; the rest of the rendered output is unchanged apart from the `helm.sh/chart` label; `helm lint` passes.
   If any check fails, fix the chart and re-run all checks; never weaken a check.

6. Self-check: before finishing, re-run every command in `<verification>`, confirm each passes, and walk requirements 2-4 against the changed files.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- The property is exactly `rerunOnLostClaim`, type `boolean`, with **no** `default:` key. A schema-level `default: true` is wrong here: the Go field is `*bool` with nil ⇒ true, and the executor binary rewrites the cluster CRD from its own schema, so a chart-side default would be dropped on the next executor start and the two schemas would diverge.
- Do NOT touch any other CRD property, `helm/templates/`, `helm/values.yaml`, `_helpers.tpl`, or `appVersion`. This change is the CRD file, the chart version and the changelog — nothing else.
- Do NOT create or edit any `values-*.yaml` overlay — cluster overlays live in another repo.
- Do NOT edit Go code.
- `helm/README.md` documents chart values only, not Config CR fields — no README change is required for this schema-only addition.
- All paths in this prompt are repo-relative; scratch files go in `/tmp` only, never inside the repo.
</constraints>

<verification>
Precondition — the container image does not ship helm; install it from the Go proxy (repo pattern, see `prompts/completed/244-helm-cluster-assigner-sun-secret.md`; `get.helm.sh` is 403 from the build network). A failure here means the prompt FAILED (report it as failed in the final summary; never skip the render checks or claim they passed):
`command -v helm >/dev/null 2>&1 || { timeout 300 go install helm.sh/helm/v3/cmd/helm@v3.16.4; export PATH="$PATH:$(go env GOPATH)/bin"; }`
`helm version --short | grep -q '^v3\.' || { echo "PRECONDITION FAILED: helm v3 unavailable in container"; exit 1; }`
Each new shell must re-export `PATH="$PATH:$(go env GOPATH)/bin"` (or call `$(go env GOPATH)/bin/helm`). Every command below must succeed (exit 0) — except the CRD `diff`, which is expected to print exactly the two added lines and nothing else. Run the render and diff commands under bash: they use process substitution (`<(...)`), which `/bin/sh` does not support.

The chart does not render without `namespace` and the executor's required values, so define a base values file first (verified to render at exit 0):
```bash
printf 'namespace: test\nexecutor:\n  kafkaBrokers: kafka:9092\n  sentry:\n    dsn: dummy\n' > /tmp/crd-base.yaml
```
Every render command spells `-f /tmp/crd-base.yaml` out in full. Do **not** put it in a shell variable and rely on unquoted expansion: zsh does not word-split, so `$B` reaches helm as one argument and fails with `open  /tmp/crd-base.yaml: no such file or directory`.

The CRD is rendered by `--include-crds` (a plain `helm template` omits `crds/`):
- `helm template x helm/ -f /tmp/crd-base.yaml --include-crds > /tmp/crd-new.yaml`
- `helm template x /tmp/crd-orig -f /tmp/crd-base.yaml --include-crds > /tmp/crd-old.yaml`

Property present, right type, no default:
- `grep -qx '                rerunOnLostClaim: {type: boolean}' /tmp/crd-new.yaml`
- `grep -q '# Optional; unset means true. false parks a taken-over task at human_review.' /tmp/crd-new.yaml`
- `! grep -A1 'rerunOnLostClaim' /tmp/crd-new.yaml | grep -q 'default:'`
- `[ "$(grep -c 'rerunOnLostClaim' /tmp/crd-new.yaml)" = 1 ]`

Nothing else changed in the CRD apart from the chart label:
- `diff <(grep -v 'helm.sh/chart:' /tmp/crd-old.yaml) <(grep -v 'helm.sh/chart:' /tmp/crd-new.yaml) > /tmp/crd.diff; [ "$(grep -c '^>' /tmp/crd.diff)" = 2 ] && ! grep -q '^<' /tmp/crd.diff` — exactly two added lines (the comment and the `rerunOnLostClaim` line), no removed or changed lines. The raw diff also prints its hunk header (e.g. `80a81,82`); the assertions ignore it. An empty diff means the snapshot was taken after the edit, not before.

Lint:
- `helm lint helm/ -f /tmp/crd-base.yaml | grep -q '0 chart(s) failed'`
- `helm lint helm/ -f /tmp/crd-base.yaml --set executor.kafkaBrokers=kafka:9092 | grep -q '0 chart(s) failed'`

Metadata:
- `grep -q '^version: 0.15.0$' helm/Chart.yaml`
- `awk '/^## /{sec=$0} /feat\(helm\): .*rerunOnLostClaim/{print sec}' CHANGELOG.md` prints `## Unreleased`
- `[ "$(grep -c '^## Unreleased$' CHANGELOG.md)" = 1 ]`
- `! grep -rn 'default:' helm/crds/config-crd.yaml` (the file carries no schema default anywhere)

`ROOTDIR=/workspace make precommit` — must pass (it does not cover the chart; it guards that nothing else regressed). The Makefile derives `ROOTDIR` from `git rev-parse --show-toplevel`; a masked `.git` (the daemon runs with `--set hideGit=true`) leaves it empty, so supply it explicitly — the same form every recent prompt in this repo uses (see `prompts/completed/244-helm-cluster-assigner-sun-secret.md`).
</verification>
