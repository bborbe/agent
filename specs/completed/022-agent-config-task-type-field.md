---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-05-10T20:34:40Z"
generating: "2026-05-10T20:34:41Z"
prompted: "2026-05-10T20:40:24Z"
verifying: "2026-05-10T21:00:02Z"
completed: "2026-05-12T22:38:02Z"
branch: dark-factory/agent-config-task-type-field
---

## Summary

- Today the executor maps a task to its handling agent through a hardcoded `task_type → agent` registry shortcut. Agents themselves do not declare which task type they handle; the binding lives in code.
- This spec adds a required `spec.taskType` string field to the `agent.benjamin-borbe.de/v1` Config CRD. Each Config declares the one `task_type` value it consumes (matching the frontmatter key on tasks).
- `taskType` is non-empty, alphanumeric-plus-hyphen (DNS-label-ish, e.g. `pr-review`, `trade-analysis`, `bug-fix`). Validation rejects anything else at admission/parse.
- Multiple Configs may share an `Image` (the same container can serve different task types) but `(metadata.name, spec.taskType)` must differ between Configs.
- This spec is purely additive on the schema and on `Validate`/`Equal`. It also migrates every existing in-repo Config manifest to carry an explicit `taskType`. No consumer code reads the field yet — routing remains untouched and is the next spec's job.

## Problem

The executor's "task assignee → agent Config" lookup is built on a hardcoded `task_type → agent` map sitting in code. Adding a new agent type today requires editing that map, recompiling, and redeploying — even though every other facet of an agent (image, env, secret, trigger, priority class) lives declaratively on the Config CRD. Worse, the registry is opaque to operators: there is no `kubectl get` that reveals which task types the cluster will route, only the executor binary knows. Group A of the [[Eliminate Agent Task Rot]] effort cannot remove that registry until Configs themselves carry the binding.

## Goal

After this spec, every agent Config CRD declares — as a required, validated field on its spec — the single `task_type` it handles. The field is visible to operators (`kubectl get config -o yaml`), survives round-trips through the typed clientset and applyconfiguration, and participates in `Equal`/`Validate` so the controller informer reacts correctly to changes. Existing manifests are migrated. Routing code is unchanged in this spec — but the data needed to delete the hardcoded registry is now on the CRs.

## Non-goals

- No consumer wiring. No code reads `spec.TaskType` for routing decisions in this spec. The hardcoded `task_type → agent` registry shortcut is **not deleted yet** — that lives in the follow-up [[Delegate to Default Agent Button in Task-Orchestrator]] work.
- No A/B selection or fan-out when multiple Configs declare the same `taskType`. Future concern; out of scope here.
- No rename of existing `metadata.name` or `spec.assignee` values. Those stay byte-for-byte identical.
- No addition of `task_type` to task frontmatter / `TaskFrontmatter` Go type. That is the watcher's responsibility, tracked separately as spec 022.
- No `failure_class` field on the agent verdict JSON. Different task entirely.
- No CRD apiVersion bump. The new field is additive on `v1`. (See Constraints for the breaking-vs-additive call.)
- No migration tooling for Configs deployed in repos outside this one (e.g. `agent-trade-analysis`, `agent-hypothesis`, `agent-backtest-agent` all live in the trading repo; `pr-reviewer` lives in the maintainer repo). This spec migrates only manifests under `~/Documents/workspaces/agent`. Owners of sibling repos must add `taskType` before the next executor that requires the field is rolled out — flagged in §Out-of-repo Follow-up.
- No admission webhook. Uniqueness of `(namespace, taskType)` and pattern enforcement live in `ConfigSpec.Validate` and at parse time only — operators can still apply two Configs with the same `taskType` if they bypass the executor's controller. That risk is accepted.

## Desired Behavior

1. The `agent.benjamin-borbe.de/v1` Config CRD accepts a required string field `spec.taskType` on `ConfigSpec`. The JSON tag is `taskType`. The Go field name is `TaskType`. No `omitempty` — absent or empty is a validation error.
2. `ConfigSpec.Validate` rejects a Config when `taskType` is empty and when `taskType` contains any character outside `[a-z0-9-]`. Uppercase, underscore, dot, slash, whitespace, and any other character all fail. Leading and trailing hyphens are permitted (the CRD already accepts kebab-case-ish strings elsewhere; tightening to "no leading/trailing hyphen" is out of scope).
3. `ConfigSpec.Equal` returns false when two specs differ only by `TaskType` (covers empty vs non-empty, "a" vs "b", same value).
4. The generated typed clientset and applyconfiguration code under `task/executor/k8s/client/...` carries the new field. Round-tripping a `Config` through `Marshal`/`Unmarshal` and through the typed client preserves `taskType`.
5. Every agent Config manifest under `~/Documents/workspaces/agent` is updated to include an explicit `spec.taskType` matching the existing assignee semantics. Concretely: `agent/claude/k8s/agent-claude.yaml` gains `taskType: <chosen-value>` (see Open Question 1 — the claude-agent is a generic runtime, so the chosen value must be decided in audit).
6. Editing only the `taskType` field of a Config causes the controller informer to detect the diff via `Equal` returning false, exactly the way every other ConfigSpec field already behaves.
7. `docs/agent-crd-specification.md` is updated: a new required row `spec.taskType` is added to the Fields table with the validation rule and a one-line example. The example block at the top of the doc gets the field too.
8. The repo's `CHANGELOG.md` (single global file at root) gets an entry under a new `## vX.Y.Z` header. Both `vX.Y.Z` and `lib/vX.Y.Z` tags are produced at the same commit per project tag policy.

## Constraints

- **Required, not optional.** Earlier CRD additions (`priorityClassName` in spec 013, `trigger` in spec 014) were optional with backward-compat defaults. This field intentionally is not. Reason: the whole point is to remove a hardcoded registry — leaving the field optional means the registry has to stay around to handle Configs that omit it. Required-from-day-one is cheaper than a two-step deprecation. The cost is paid up front: every existing in-repo manifest must be migrated atomically with the type change.
- The field name `taskType` (camelCase JSON, `TaskType` Go) is fixed. Do not rename to `task_type`, `kind`, `type`, `handles`, `consumes`, etc. The frontmatter key on tasks is `task_type` (snake_case per YAML convention) but on the CRD spec the convention is camelCase, matching every other field on `ConfigSpec`.
- Validation regex is `^[a-z0-9-]+$`. Lowercase only. No empty string. Max length **63 characters** (matches K8s label-value cap so the value can safely flow into a label/annotation in future consumers). The pattern matches DNS-label-ish naming used elsewhere in the project without imposing the full DNS-label rule (which would forbid leading/trailing hyphens and a leading digit).
- **No reserved values.** Strings like `default`, `none`, `*`, `all` are valid `taskType` values. Future routing sentinels that need a distinct shape MUST use a character outside `[a-z0-9-]` (which the regex rejects), so they cannot collide with a real `taskType`.
- **Uniqueness across Configs is not enforced.** Two Configs in the same namespace can declare the same `taskType`. Routing code in a future spec decides resolution (A/B, primary preference, etc.). The bijection wording in §Summary is aspirational for the v1 use case; explicitly NOT a runtime invariant.
- `ConfigSpec.Equal` and `ConfigSpec.Validate` must both be updated in the same commit as the type change. Either alone breaks the controller informer or admission flow.
- The existing required fields (`assignee`, `image`, `heartbeat`) keep their current order and validation messages; the new check is appended without reshuffling.
- `make generatek8s` (which runs `hack/update-codegen.sh`) is the only sanctioned way to regenerate typed client + applyconfiguration code. No hand-edits to `zz_generated*.go` or `applyconfiguration/`.
- `make precommit` runs in `task/executor` and must stay green. `git diff --exit-code` after `make generate` and `make generatek8s` must be empty (no codegen drift left uncommitted).
- The single global `CHANGELOG.md` and paired `vX.Y.Z` + `lib/vX.Y.Z` tags rule from `CLAUDE.md` applies. Both tag numbers must equal the new CHANGELOG header.
- Existing Config manifests in sibling repos (trading, maintainer) MUST NOT be edited from within this repo's pipeline. Cross-repo coordination is out of scope; see §Out-of-repo Follow-up.
- The CRD's OpenAPIV3Schema (defined in `SetupCustomResourceDefinition` per spec 007 pattern) gains a `taskType` property marked `required` with `pattern: ^[a-z0-9-]+$`. Schema and Go struct stay in sync.

## Assumptions

- The closest precedents are spec 014 (configurable triggers) and spec 013 (priority class) — both added a field to `ConfigSpec` with `Validate`/`Equal` updates and a regenerate-clientset step. The exact same shape applies here, modulo "required" vs "optional".
- `make generatek8s` regenerates deepcopy + clientset + applyconfiguration in one shot. No additional generator config is required for a primitive `string` field.
- Ginkgo + Gomega + counterfeiter are the test stack (per `CLAUDE.md`). Existing tests in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go` extend cleanly.
- The only in-repo Config manifest is `agent/claude/k8s/agent-claude.yaml`. Other repos (trading, maintainer) carry their own manifests and migrate independently.
- The `claude-agent` Config currently has no natural single `taskType` (the container is a generic Claude Code runner). The audit step must pick a value; candidates: `claude`, `general`, `default`. See Open Question 1.
- **Cross-spec coupling:** the `taskType` chosen for a Config MUST equal the `task_type` value any task emitter writes for tasks targeting that Config's assignee. Spec 022 emits `task_type: pr-review` for `pr-reviewer-agent` (matches `taskType: pr-review` here). For `agent-claude`, no Go watcher emits — tasks come via slash command / manual creation, so the convention is operator-enforced: any task assigned to `claude-agent` must set `task_type: <claude-config's value>` in its frontmatter.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Manifest omits `spec.taskType` | CRD validation rejects with `taskType is empty`; controller refuses to apply | Operator adds the field and re-applies |
| Manifest sets `spec.taskType: ""` (explicit empty) | Same as omitted — `taskType is empty` | Operator picks a non-empty value |
| Manifest sets `spec.taskType: PR-Review` (uppercase) | Validation rejects with `taskType must match ^[a-z0-9-]+$` | Operator lowercases the value |
| Manifest sets `spec.taskType: pr_review` (underscore) | Validation rejects with the same pattern message | Operator switches to a hyphen |
| Two Configs in the same namespace declare `taskType: pr-review` | Both apply (no admission webhook). Routing code in a future spec must decide what to do; today nothing reads the field. | Out of scope for this spec |
| Existing Config manifest in this repo is rolled out without migration | Executor crash on Config parse; informer cannot list Configs | Operator rolls back to previous executor build, then applies migrated manifest |
| Sibling repo (trading, maintainer) ships an old Config manifest after this executor is deployed | That Config fails validation in their cluster; their executor logs `taskType is empty` and skips | Sibling repo owner updates their manifest (see §Out-of-repo Follow-up) |
| `make generatek8s` produces a diff that's not committed | `git diff --exit-code` fails in CI / precommit | Re-run `make generatek8s` and commit the generated files |

## Security / Abuse Cases

- The new field is a short string under operator control on a namespaced K8s CR — same trust boundary as every other ConfigSpec field. No new attack surface.
- The validation pattern `^[a-z0-9-]+$` excludes shell metacharacters, path separators, and quote characters by construction. Even when a future spec consumes `taskType` for filename or shell composition, no escaping is required at the consumption site.
- An operator with RBAC to write Configs in a namespace can already shape executor behavior arbitrarily (image, env, secret mount). Adding `taskType` does not widen that.
- No HTTP, file, or user-input ingress crosses into this code path. Validation runs in-memory on a typed struct.

## Acceptance Criteria

- [ ] `ConfigSpec` carries a required `TaskType string \`json:"taskType"\`` field, declared after the existing required fields and before the optional ones.
- [ ] `ConfigSpec.Validate` returns a wrapped `validation.Error` with message `taskType is empty` when `TaskType == ""`.
- [ ] `ConfigSpec.Validate` returns a wrapped `validation.Error` referencing the pattern when `TaskType` contains any character outside `[a-z0-9-]`.
- [ ] `ConfigSpec.Validate` returns nil for a spec with `TaskType: "pr-review"` (and other valid values like `claude`, `trade-analysis`, `bug-fix`, `a`, `2fa-setup`).
- [ ] `ConfigSpec.Validate` returns a wrapped `validation.Error` when `TaskType` exceeds 63 characters (e.g. 64 `a`s).
- [ ] `ConfigSpec.Equal` returns false when two specs differ only by `TaskType`; returns true when both equal.
- [ ] Generated `applyconfiguration/agent.benjamin-borbe.de/v1/configspec.go` exposes a `WithTaskType(string)` builder method, matching the pattern of other primitive fields.
- [ ] `agent/claude/k8s/agent-claude.yaml` includes an explicit `spec.taskType` set to the value chosen during audit (Open Question 1).
- [ ] The CRD's OpenAPIV3Schema (`SetupCustomResourceDefinition`) declares `taskType` with `type: string`, `pattern: ^[a-z0-9-]+$`, and includes it in the `required` list alongside `assignee`, `image`, `heartbeat`.
- [ ] `docs/agent-crd-specification.md` lists `spec.taskType` in the Fields table as required, with a one-line description and an example value; the top-level YAML example block carries the field.
- [ ] `CHANGELOG.md` has a new `## vX.Y.Z` header naming the field; matching `vX.Y.Z` and `lib/vX.Y.Z` tags exist at the release commit.
- [ ] `make precommit` clean in `task/executor`. `make generate` and `make generatek8s` produce no uncommitted diff.

No new scenario test. The behavior is covered fully by unit tests in `types_test.go` (Validate, Equal, JSON round-trip) plus the existing Config-controller integration tests (which exercise informer + apply through the typed client).

## Verification

```
cd task/executor
make generatek8s
git diff --exit-code         # no codegen drift
make precommit
go test ./k8s/apis/agent.benjamin-borbe.de/v1/...
```

After deploy (manual, operator-run):

```
kubectlquant -n dev get configs.agent.benjamin-borbe.de agent-claude -o yaml | grep taskType
kubectlquant -n dev apply -f agent/claude/k8s/agent-claude.yaml    # round-trip applies cleanly
```

Negative check (manual):

```
# Edit agent-claude.yaml to remove spec.taskType, attempt to apply — expect rejection
kubectlquant -n dev apply --dry-run=server -f /tmp/no-tasktype.yaml
# expected: error citing "taskType is empty" or schema validation on the required field
```

## Out-of-repo Follow-up

Once this spec lands and the executor enforces `taskType`, every sibling repo that ships an `agent.benjamin-borbe.de/v1 Config` manifest must add the field before the new executor reaches their cluster. Known affected manifests:

- `~/Documents/workspaces/trading/agent/trade-analysis/k8s/agent-trade-analysis.yaml` → `taskType: trade-analysis`
- `~/Documents/workspaces/trading/agent/hypothesis/k8s/agent-hypothesis.yaml` → `taskType: hypothesis`
- `~/Documents/workspaces/trading/agent/backtest/k8s/agent-backtest-agent.yaml` → `taskType: backtest`
- `~/Documents/workspaces/maintainer/agent/pr-reviewer/k8s/maintainer-agent-pr-reviewer.yaml` → `taskType: pr-review`

These migrations are operator-coordinated, not part of this spec's PR. The companion task in [[Eliminate Agent Task Rot]] tracks them.

## Sizing Recommendation

Single prompt is the right size. The change is mechanically narrow:

1. Add field + tag to `ConfigSpec` in `types.go`.
2. Extend `Validate` (two new checks) and `Equal` (one extra equality term).
3. Extend OpenAPIV3Schema in `SetupCustomResourceDefinition`.
4. Run `make generatek8s` and commit the regenerated `applyconfiguration` and deepcopy artifacts.
5. Update `agent/claude/k8s/agent-claude.yaml`.
6. Update `docs/agent-crd-specification.md` (Fields table + top example).
7. Add Ginkgo cases to `types_test.go` for the new Validate paths and Equal diff.
8. Bump `CHANGELOG.md` and produce paired tags.

All eight steps fit in one PR, and splitting them risks landing the type without the manifest migration (which would crash the executor on first apply). Recommend keeping it as one prompt unless audit reveals codegen surface area large enough to push it over a reasonable diff size.

## Do-Nothing Option

Keep the hardcoded `task_type → agent` registry in executor code. Every new agent type requires (a) a code change in the executor, (b) a recompile, (c) a paired tag/release, (d) an operator deploy, all gated on a single team's bandwidth. The registry is invisible to operators, untestable from CRs alone, and grows unbounded as the agent catalog expands. Group A of [[Eliminate Agent Task Rot]] cannot ship without this field — refusing to add it means deferring that whole effort indefinitely. The status quo is acceptable only as long as the agent catalog stays at its current size and turns over slowly; the moment a fifth or sixth agent type lands, the cost of the registry compounds.

## Open Questions

1. What `taskType` value should `agent-claude` declare? The container is a generic Claude Code runner — it does not handle one specific frontmatter `task_type`. Candidates: `claude` (matches assignee root), `general`, `default`, `claude-general`. Decide during audit. **Default recommendation: `claude`** — shortest, matches the assignee prefix, and leaves room for future per-task-type Claude Configs (`claude-pr-review`, `claude-bug-fix`) to coexist without colliding.
2. Should `taskType` permit a leading digit (e.g. `2fa-setup`)? The proposed pattern `^[a-z0-9-]+$` allows it. DNS-label-strict would not. Recommend allowing — looser is fine here, the field is not used as a hostname.
3. Should the validator forbid leading/trailing hyphens? Out of scope — flagged for completeness; spec assumes "no". If audit decides yes, tighten to `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`.
