---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-14T12:04:04Z"
generating: "2026-05-14T12:04:23Z"
prompted: "2026-05-14T12:08:32Z"
branch: dark-factory/executor-inject-task-type-env
---

## Summary

- The executor spawns Kubernetes Jobs for agents and injects a fixed set of env vars (`TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`, `PHASE`) plus any per-agent config env. The task's `task_type` frontmatter field is read elsewhere (executor filter, spec 028) but is NOT forwarded to the spawned Job.
- The three agent binaries in this repo (`agent/claude`, `agent/code`, `agent/gemini`) already declare a `TASK_TYPE` env field on their application struct with default `"unknown"` (added in spec 029 for per-agent Prometheus metrics). Without the executor injecting the real value, every Job is grouped as `task_type="unknown"` on PushGateway — making per-task-type metric breakdowns impossible.
- This spec injects `TASK_TYPE` into every spawned Job's env, sourced from the task's frontmatter `task_type` field. Empty/absent frontmatter yields an empty string — the agent binary keeps its `"unknown"` default behavior because envconfig only overrides the default when the env is set to a non-empty value.
- Foundation work: once `TASK_TYPE` reaches every agent container, follow-up specs can branch on `a.TaskType` in main.go for per-task-type dispatch (e.g. healthcheck vs domain handler) without re-parsing `TASK_CONTENT` frontmatter inside the binary.
- Pattern is identical to existing `PHASE` injection: a typed accessor on `lib.TaskFrontmatter`, a value-coercion helper next to `taskPhaseString`, and one new `envBuilder.Add` line in `buildJobEnvBuilder`.
- **Named `lib.TaskType` type** introduced alongside the accessor. Every other well-known frontmatter field has a named type (`TaskIdentifier`, `TaskAssignee`, `domain.TaskPhase`, `domain.TaskStatus`); `task_type` joins that pattern. Includes validation (regex matching the existing CRD-side `validateTaskTypeValue` rule), well-known constants for current task type values, and an `oauth-probe` constant marked deprecated to prepare the upcoming `healthcheck` rename.

## Referenced Specs

- **Spec 022** — adds `task_type` field to `AgentConfig` CRD. Establishes `task_type` as a first-class concept on the agent side.
- **Spec 026** — `AgentConfig` carries a list of supported `task_types`. Source of truth for which values an agent will accept.
- **Spec 028** — executor filters spawn candidates by `task_type` match against `AgentConfig.task_types`. The executor already reads `task.Frontmatter["task_type"]` for filtering; this spec forwards the same value to the Job's env.
- **Spec 029** — per-agent Prometheus metrics. Adds the `TASK_TYPE` env field to each agent binary's `application` struct (default `"unknown"`) and uses it as a pusher `Grouping()` dimension. Explicitly defers "executor passing the task's actual `task_type` field to the spawned Job's `TASK_TYPE` env" to a follow-up spec — this is that spec.

## Problem

Three agent binaries declare a `TaskType` field expecting a `TASK_TYPE` env var, but no producer in the system sets it. On PushGateway every Job is grouped as `task_type="unknown"`, collapsing the dimension and defeating the reason it was added. Operators cannot answer "what's the failure rate of `claude-agent` runs of task_type `healthcheck` vs `domain-handler`". Without executor-side injection, every consumer (metrics today, dispatch tomorrow) must either accept `"unknown"` or re-parse `TASK_CONTENT` frontmatter — duplicating logic the executor already performs for filtering.

## Goal

After this spec ships, every K8s Job spawned by the executor carries a `TASK_TYPE` env var whose value equals the task's `task_type` frontmatter field (empty string when absent or non-string). No agent binary code changes; no CRD changes; no rename of existing handlers.

## Dependencies

- **Spec 029 (per-agent job metrics) must merge before this spec ships.** Spec 029 introduces the `TASK_TYPE` env field with default `"unknown"` on each agent binary's `application` struct — without that consumer, the env injected by this spec has no reader and silently no-ops.

## Non-goals

- **No per-agent dispatch logic.** Branching on `a.TaskType` inside any agent binary's `main.go` or `Run()` is a follow-up spec.
- **No shared `agent/lib/healthcheck` package.** The "Per-Agent task_type Dispatch" task tracks this separately.
- **No rename of `oauth-probe` to `healthcheck`.** Probe publisher, HTTP route, and cron env name are untouched.
- **No new agent binary fields or default changes.** The `TASK_TYPE` env field already exists with default `"unknown"` (spec 029). This spec does not modify the field, its default, or any consumer of `a.TaskType`.
- **No CRD changes.** `AgentConfig.task_type` and `task_types` (specs 022, 026) are unchanged. The CRD-side `validateTaskTypeValue` regex check in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` stays as-is; refactoring it to delegate to `lib.TaskType.Validate` is a follow-up.
- **No change to `ConfigSpec.TaskType string` / `ConfigSpec.TaskTypes []string` field types** in the CRD types.go. Generated deepcopy/applyconfiguration touching means bigger blast radius — separate spec.
- **No change to `pkg/task_type_filter.go:EffectiveTaskTypes(string, []string)` signature.** The handler refactor switches its single call site to pass `string(f.TaskType())`. Migrating the filter helper to `[]lib.TaskType` is a follow-up.
- **No validation that `task_type` is in `AgentConfig.task_types`.** That filter is already enforced upstream of spawn by spec 028; this spec only forwards the value.
- **No default value or fallback.** If `task_type` is absent from frontmatter, `TASK_TYPE` is set to the empty string — envconfig then preserves the binary's existing `"unknown"` default. The executor does not synthesize a value.

## Desired Behavior

1. A new `lib.TaskType` named type exists in `agent/lib/agent_task-type.go`, defined as `type TaskType string` with methods `String() string`, `Bytes() []byte`, `Ptr() *TaskType`, `Validate(ctx context.Context) error` (mirrors the shape of `lib.TaskIdentifier` in `agent_task-identifier.go`).
2. `lib.TaskType.Validate` enforces the same rule as the existing CRD-side `validateTaskTypeValue` in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`: non-empty, matches `^[a-z0-9-]+$`, max 63 characters. Empty value returns a validation error wrapped with `errors.Wrap(ctx, validation.Error, ...)`.
3. Well-known `TaskType` constants exist in `agent_task-type.go` for every value currently in use across the 5 in-cluster Config CRs: `TaskTypeClaude`, `TaskTypePRReview`, `TaskTypeBacktest`, `TaskTypeHypothesis`, `TaskTypeTradeAnalysis`, `TaskTypeOAuthProbe`. The `oauth-probe` constant carries a `// Deprecated: use TaskTypeHealthcheck` GoDoc with no replacement constant yet (the rename spec adds `TaskTypeHealthcheck`).
4. `lib.TaskFrontmatter` exposes a `TaskType()` accessor that returns `lib.TaskType` (the named type, not bare `string`) — the value of the `task_type` key, or `TaskType("")` when the key is absent or holds a non-string value.
5. The executor's `buildJobEnvBuilder` adds a `TASK_TYPE` env var to every spawned Job's container, whose value is `task.Frontmatter.TaskType().String()`. A package-private helper `taskTypeString(f lib.TaskFrontmatter) string` mirrors `taskPhaseString` for the coercion.
6. The injection happens on every spawn path that currently injects `PHASE` — no separate code path, no opt-in, no agent-config gating.
7. The existing call site in `task/executor/pkg/handler/task_event_handler.go:204` — `taskType, _ := task.Frontmatter.String("task_type")` inside `taskTypeMismatchReason` — switches to `taskType := task.Frontmatter.TaskType()`. The local variable becomes `lib.TaskType`; the call to `pkg.TaskTypeInSet(taskType, effectiveTypes)` passes `string(taskType)` (filter signature unchanged per non-goals).
8. When the task's frontmatter has no `task_type` field, the Job's `TASK_TYPE` env is set to an empty string. Downstream agent binaries that use envconfig retain their `"unknown"` default for unset/empty envs.
9. The `task_type` value flows verbatim into the Job env — no normalization (no lowercasing, no whitespace trimming, no allowlisting against `AgentConfig.task_types`). Validation (`TaskType.Validate`) exists as a method but is not invoked by the spawner; callers opt in.

## Constraints

- **Existing env var names and values are frozen.** `TASK_CONTENT`, `TASK_ID`, `KAFKA_BROKERS`, `BRANCH`, `PHASE` keep their current keys, sources, and emptiness semantics.
- **Pattern parity with `PHASE`.** The new helper must follow the shape of `taskPhaseString` (package-private, single-arg `lib.TaskFrontmatter`, returns string, returns `""` when absent). No generic reflection, no map-of-keys table. `TASK_TYPE` is injected immediately after `PHASE` and before per-agent `config.Env` entries.
- **Spawner does not call `TaskType.Validate`.** The validation method exists on the named type for downstream consumers; the executor forwards the value opaquely.
- **Accessor parity with existing typed accessors.** `TaskType()` lives next to `Phase()`, `Stage()`, `Assignee()`, `CurrentJob()` in `lib/agent_task-frontmatter.go` and follows the same `f["key"].(string)` pattern. Non-string values yield empty string, not panic.
- **No change to the `EnvBuilder` interface or k8s package.**
- **No new dependency.** The accessor is a one-line map lookup; the helper is a one-line wrapper.
- **`make precommit` must pass in `lib/` and `task/executor/` service dirs.**

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Task frontmatter has no `task_type` key | `TaskType()` returns `""`; `TASK_TYPE` env is set to `""`; agent binary defaults `a.TaskType` to `"unknown"` via envconfig | None — designed behavior |
| Task frontmatter has `task_type: 42` (non-string) | `TaskType()` returns `""` (type assertion fails silently); same as absent case | None — designed behavior |
| Task frontmatter has `task_type: ""` (empty string) | `TaskType()` returns `""`; same as absent case | None — designed behavior |
| Task frontmatter has `task_type: "healthcheck"` | `TaskType()` returns `"healthcheck"`; Job's `TASK_TYPE` env is `"healthcheck"`; agent binary's `a.TaskType == "healthcheck"` | None — designed behavior |

### Interactions (not failures)

- **Per-agent `config.Env` entry named `TASK_TYPE`**: the per-agent entry is added after the executor's `TASK_TYPE` and overwrites it in the container env (existing `EnvBuilder.Add` last-wins semantics). Operators avoid colliding with reserved env names; this is unchanged behavior, not introduced by this spec.

## Security / Abuse Cases

`task_type` originates in user-authored vault frontmatter, flows through Kafka, and is injected into a container env var. The value never crosses a shell, exec, or file-path boundary inside the executor — it is handed to the Kubernetes API as an `EnvVar.Value` string and consumed inside the agent container as a plain env. Threat model:

- **Attacker controls** the frontmatter value (anyone who can write to the vault repo).
- **Boundary crossed** is vault → Kafka → K8s API → container env. None of these interpret the value as code.
- **Length / character classes**: Kubernetes accepts arbitrary UTF-8 in `EnvVar.Value`. No size validation is added; existing vault-to-Kafka pipeline already bounds frontmatter field sizes.
- **Hang/retry risk**: none — the injection is a synchronous map lookup.
- **Race**: none — frontmatter is read once at spawn time, identical to `PHASE` today.

No new validation is required. Downstream consumers (metric labels, future dispatch) are responsible for their own validation if they treat the value as anything other than an opaque string.

## Acceptance Criteria

- [ ] New file `agent/lib/agent_task-type.go` defines `type TaskType string` with methods `String()`, `Bytes()`, `Ptr()`, `Validate(ctx)` matching the shape of `agent_task-identifier.go`.
- [ ] `lib.TaskType.Validate` returns an error wrapped with `validation.Error` for: empty string, value not matching `^[a-z0-9-]+$`, value longer than 63 characters. Returns nil for valid values.
- [ ] Well-known constants exist in `agent_task-type.go`: `TaskTypeClaude` (`"claude"`), `TaskTypePRReview` (`"pr-review"`), `TaskTypeBacktest` (`"backtest"`), `TaskTypeHypothesis` (`"hypothesis"`), `TaskTypeTradeAnalysis` (`"trade-analysis"`), `TaskTypeOAuthProbe` (`"oauth-probe"`).
- [ ] `TaskTypeOAuthProbe` carries a GoDoc comment `// Deprecated: use TaskTypeHealthcheck once introduced by the oauth-probe rename spec.`.
- [ ] Unit tests in `agent_task-type_test.go` (Ginkgo) cover: each valid constant passes `Validate`, empty string fails, value with uppercase fails, value with underscore fails, 64-character value fails, 63-character value passes, `String()` and `Bytes()` round-trip correctly.
- [ ] `lib.TaskFrontmatter` has a `TaskType() lib.TaskType` method (named-type return) in `agent_task-frontmatter.go` that returns the `task_type` frontmatter value, or `TaskType("")` when absent or non-string.
- [ ] Unit tests in `agent_task-frontmatter_test.go` cover three cases for `TaskType()`: present string value returned verbatim as `TaskType`, key absent returns `TaskType("")`, non-string value returns `TaskType("")`.
- [ ] `task/executor/pkg/handler/task_event_handler.go:taskTypeMismatchReason` no longer calls `task.Frontmatter.String("task_type")` — uses `task.Frontmatter.TaskType()` instead. The call to `pkg.TaskTypeInSet` passes `string(taskType)` to keep the filter signature stable per non-goals.
- [ ] Existing tests in `pkg/handler/task_event_handler_test.go` covering type-mismatch and empty-task-type paths still pass without semantic changes.
- [ ] `task/executor/pkg/spawner/job_spawner.go` defines a package-private `taskTypeString(f lib.TaskFrontmatter) string` helper mirroring `taskPhaseString`. Returns `f.TaskType().String()`.
- [ ] `buildJobEnvBuilder` calls `envBuilder.Add("TASK_TYPE", taskTypeString(task.Frontmatter))` (insertion order is enforced by Constraints, not duplicated here).
- [ ] Spawner test asserts that for a task with `task_type: "healthcheck"` in frontmatter, the spawned Job's container env contains `TASK_TYPE=healthcheck`.
- [ ] Spawner test asserts that for a task with no `task_type` frontmatter key, the spawned Job's container env contains `TASK_TYPE` set to `""`.
- [ ] `make precommit` passes in `lib/`.
- [ ] `make precommit` passes in `task/executor/`.
- [ ] `CHANGELOG.md` has an `Unreleased` entry under `feat(lib): add TaskType named type with validation and well-known constants` plus a second bullet `feat(task/executor): inject TASK_TYPE env into every spawned Job` referencing this spec.
- [ ] **No new scenario.** Unit tests at the type, accessor, and spawner layers fully cover the behavior; no E2E test is added.

## Verification

```
cd ~/Documents/workspaces/agent/lib && make precommit
cd ~/Documents/workspaces/agent/task/executor && make precommit
```

Both must exit zero. The spawner test output must include the new `TASK_TYPE` env assertions.

## Do-Nothing Option

If we skip this, the `TASK_TYPE` env field declared by every agent binary (spec 029) silently stays at its `"unknown"` default forever. Per-task-type Prometheus grouping never works in production. Any future per-agent dispatch logic is forced to re-parse `TASK_CONTENT` markdown inside `main.go` — duplicating frontmatter parsing the executor already performs, and re-introducing the same drift risk that justified the typed accessor pattern in the first place. Not acceptable: the cost is two new lines in the executor and one new accessor in `lib/`, against an indefinite tax on every future task-type-aware feature.
