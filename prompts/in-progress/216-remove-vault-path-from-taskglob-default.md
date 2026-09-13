---
status: approved
created: "2026-09-13T13:05:00Z"
queued: "2026-09-13T14:06:50Z"
---

# Remove the vault task path from the taskGlob default

<summary>
- The chart no longer names a specific vault directory in its task-glob default
- Consumers supply the glob explicitly, exactly as production already does
- The values default and both template fallbacks are neutralised together
- Existing installs are unaffected provided they set the glob explicitly — reviewer check before merging: confirm every consumer config sets it
- A vault rename can no longer silently stop dispatch for consumers that set the glob; an unset glob is caught loudly by the executor, which must ship before this default is safe to inherit
</summary>

<objective>
Remove the hardcoded `24 Tasks/*.md` default from the `executor.taskGlob` chain so the chart stops baking one vault's folder layout into a released artifact. When the vault renumbered `24 Tasks/` to `25 Tasks/` on 2026-09-13, every executor that inherited this default silently evaluated zero task files — `evaluated=0`, no error, indistinguishable from a quiet fleet. The chart cannot know a consumer's vault layout, so it must stop asserting one.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

The value flows through three sites in this repo, and fixing any one alone leaves the others live:

- `helm/values.yaml` — the `executor.taskGlob` default; search for `taskGlob`
- `helm/templates/executor-deployment.yaml` — two fallbacks, one in the singular executor deployment's `TASK_GLOB` env block and one in the per-vault fan-out loop that renders `agent-task-executor-<name>` deployments; search for `TASK_GLOB`

The sibling `bborbe/agent-task-executor` repo carries its own `default:"24 Tasks/*.md"` on the `TASK_GLOB` flag, and this chart deploys that image. That repo is a separate dark-factory project with its own worktree and its own prompt — it is not reachable from this container. Do not attempt to edit it here.
</context>

<requirements>
1. In `helm/values.yaml`, change the `executor.taskGlob` default from `"24 Tasks/*.md"` to `""`. Update the comment above it to say the glob is **consumer-supplied** and that an empty value means no glob is configured. Do not let the comment name any vault directory, not even as an example.

2. In `helm/templates/executor-deployment.yaml`, drop the `default "24 Tasks/*.md"` fallback from **both** `TASK_GLOB` env blocks:
   - singular executor deployment: `{{ .Values.executor.taskGlob | default "24 Tasks/*.md" | quote }}` becomes `{{ .Values.executor.taskGlob | quote }}`
   - per-vault fan-out loop: `{{ $e.taskGlob | default $.Values.executor.taskGlob | default "24 Tasks/*.md" | quote }}` becomes `{{ $e.taskGlob | default $.Values.executor.taskGlob | quote }}`
   Keep the `| quote` filter in both — dropping it renders a null `value:` once the default is empty. Leave both blocks otherwise unchanged, including the comment above `GITREST_GATEWAY_SECRET`.

3. Verify no path-shaped literal survives anywhere under `helm/` — see `<verification>` for the exact form.

4. Do **not** substitute a new sentinel value, and do **not** implement fail-loud behaviour here. Making an empty glob loud is the executor binary's job and ships in its own repo. The chart's only responsibility is to stop asserting a directory. In particular a replacement default of `25 Tasks/*.md` is explicitly wrong: it re-bakes the current layout and breaks again at the next rename.

5. Add a `## Unreleased` heading at the top of `CHANGELOG.md` — it is currently **absent**; the file opens at `## v0.88.0` — with a bullet describing the default removal, **and bump the chart version in `helm/Chart.yaml` from `0.6.3` to `0.6.4`**. The inline comment on that field reads "bumped on chart changes", every recent chart commit bumped it (`0.5.2`→`0.6.0`→`0.6.1`→`0.6.2`→`0.6.3`), and the release commit does not touch `helm/Chart.yaml` — so nothing else in the pipeline performs this bump, and skipping it ships an altered chart still labelled `0.6.3`. Mention the bump in the `## Unreleased` entry. Do **not** touch the root module version or `.claude-plugin/`.

6. In `helm/README.md`, add a row to the `### executor` values table documenting that the glob is now consumer-supplied — this change makes `executor.taskGlob` mandatory for consumers, and the project's own `docs/dod.md` requires the README to be updated when a change affects configuration. Use:

   `| \`executor.taskGlob\` | \`""\` | git-rest single-level glob selecting the vault task files the reconcile loop evaluates. Consumer-supplied — the chart asserts no vault folder layout; empty = no glob configured. |`

7. Run `grep -nE '([0-9]{2} )?(Tasks|Goals|Knowledge Base)/' helm/values.yaml` and report any other hardcoded vault path in your final summary. Do **not** change them — out of scope. (The same defect class exists in `commands/launch-agent.md` and was broken by the same vault rename; note it as a follow-up, do not fix it here.)
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Repo-relative paths only; never absolute or home-relative paths
- Do not edit `bborbe/agent-task-executor` — separate repo, separate worktree, separate prompt
- Do not introduce a replacement default value anywhere in this change
- The only version string you may change is `helm/Chart.yaml` (`0.6.3` → `0.6.4`); do not touch the root module version or `.claude-plugin/`
- This chart change and the executor's fail-loud-on-empty-glob change (sibling repo `bborbe/agent-task-executor`) must land together. An empty default inherited *before* fail-loud exists reproduces the silent `evaluated=0` failure this prompt removes — keep them sequenced in the same release window
- `helm/` is YAML and Go templating only — no Go source changes in this prompt
</constraints>

<verification>
Run `make precommit` -- must pass.

Then confirm the absence directly. Use `! grep -q`, not `grep -c` — `grep -c` exits 1 when the count is 0, which would fail the very step meant to pass:

- `! grep -rq '2[0-9] Tasks/' helm/` -- must succeed (the literal is gone from the chart)

Then confirm the templates still render, since both edited blocks sit inside `{{ }}` expressions and a malformed one fails only at render time.

⚠️ **`helm` is NOT installed in this container image**, and `get.helm.sh` is blocked by the egress allowlist. Install it from the Go proxy first — the pattern is already verified in `prompts/completed/210-spec-050-executors-values-schema.md`:

⚠️ **Run every command below in ONE shell invocation.** `export PATH` does not survive into a new shell, so if the install and the renders are split across separate calls, `helm` is not found and the renders silently produce nothing — and the daemon does not check `<verification>` exit codes.

- `command -v helm >/dev/null 2>&1 || { go install helm.sh/helm/v3/cmd/helm@v3.16.4; export PATH="$PATH:$(go env GOPATH)/bin"; }` -- then `helm version --short` must print `v3.16`
- A bare `helm template helm/` exits 1 — the chart requires `namespace`, `executor.kafkaBrokers` and `executor.existingSecret`. Render with all three set:
  `helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret > /tmp/rendered.yaml` -- must exit 0 with no stderr
- `grep -A1 'name: TASK_GLOB' /tmp/rendered.yaml` -- must print `value: ""` on the line *after* the name
- `! grep -q '2[0-9] Tasks/' /tmp/rendered.yaml` -- must succeed
- `helm lint helm/` -- must report 0 chart(s) failed

⚠️ **The name and the value render on separate lines.** A pattern like `'TASK_GLOB.*Tasks/'` is single-line scoped and therefore *never matches* — it passes vacuously on the unchanged chart too, so it cannot tell before from after. Assert on the `value:` line via `grep -A1`, and keep the pre-change tree handy to confirm the check actually flips.

Then render the **fan-out** path, which the default render never exercises (`executors: []` means only the singular block renders, leaving the second edited site unverified):

- `printf 'executors:\n  - name: openclaw\n    enabled: true\n    vaultName: openclaw\n    topicPrefix: develop-openclaw\n    kafkaBrokers: kafka:9092\n' > /tmp/executors-fixture.yaml`
- `helm template helm/ -f /tmp/executors-fixture.yaml --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret | grep -A1 'name: TASK_GLOB'` -- must print `value: ""` from the `agent-task-executor-openclaw` Deployment

⚠️ The fixture **must** set `enabled: true` — the template gates on `{{- if $e.enabled }}`, which treats an absent key as false, so a fixture omitting it renders no Deployment at all and silently re-creates the vacuous-check problem.

Before finishing, re-run `<verification>` and confirm it passes; walk each acceptance criterion against the change.
</verification>
