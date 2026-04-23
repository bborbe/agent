---
status: generating
tags:
    - dark-factory
    - spec
approved: "2026-04-23T21:07:10Z"
generating: "2026-04-23T21:07:19Z"
branch: dark-factory/configurable-task-triggers
---

## Summary

- Task-executor today spawns a Job only when the task event's phase matches a hardcoded list (planning, in_progress, ai_review) and the status string equals `in_progress`. Both decisions are baked into executor code and are the same for every agent.
- This spec moves the phase allow-list into the agent `Config` CRD as an optional nested `spec.trigger` block with two lists: `phases` and `statuses`. Each agent declares the trigger conditions it wants.
- When a Config omits `trigger` (or the lists inside it are empty), the executor falls back to today's behavior so existing agents keep working byte-for-byte.
- The nested shape (`spec.trigger.*`) leaves room for future trigger dimensions (labels, priorities) without another CRD field reshuffle.
- Implementation stays additive: no Kafka schema change, no verdict contract change.

## Problem

Every agent in the system has the same trigger contract wired into `task_event_handler.go`: only tasks in phase planning / in_progress / ai_review with status `in_progress` are eligible for spawning. New agent types (e.g. a todo-grooming agent, a done-reviewer agent, a backlog-triage agent) need to react to other phase/status combinations, but cannot without a code change to the shared executor. Each new trigger need becomes either a hardcoded special case or a fork, which does not scale as the catalog of agent Configs grows.

## Goal

Each agent Config can declare its own trigger conditions (phases + statuses) in its CRD manifest. The executor reads those conditions at event-handling time and decides whether to spawn based on the per-agent trigger, not a global allow-list. Configs that do not declare a trigger keep the current behavior without any migration.

## Non-goals

- No new trigger dimensions beyond phase and status (labels, priorities, assignee patterns, annotation filters are out of scope for this spec).
- No per-Config override of other executor globals (heartbeat interpretation, reschedule behavior, retry counters).
- No migration tooling. Defaults cover every existing Config.
- No CRD version bump. The new field is an additive optional nested object.
- No change to the Kafka task-event schema, the verdict/result schema, or the spawn-notification schema.
- No updates to agent-Config manifests in consumer repositories (e.g. code-reviewer) beyond, at most, a single demonstration manifest inside this repo.

## Desired Behavior

1. The `agent.benjamin-borbe.de/v1` Config CRD accepts an optional nested `spec.trigger` object. Inside that object, `phases` is an optional list of task phases and `statuses` is an optional list of task statuses. Both lists may be omitted independently.
2. When the executor receives a task event, it resolves the agent Config for the task's assignee and then evaluates trigger conditions against that Config before deciding to spawn.
3. A task event passes the phase check when: the Config's `trigger.phases` is absent or empty and the event phase is in the default set (planning, in_progress, ai_review); OR the Config's `trigger.phases` is non-empty and contains the event phase.
4. A task event passes the status check when: the Config's `trigger.statuses` is absent or empty and the event status equals `in_progress` (today's behavior); OR the Config's `trigger.statuses` is non-empty and contains the event status.
5. Both checks must pass for the event to be eligible for spawning. Every other filter the handler runs today (empty identifier, stage mismatch, empty assignee, active-job guards) stays unchanged and keeps its current order relative to the new check.
6. A Config with an explicit `trigger.phases: []` or `trigger.statuses: []` is treated identically to the field being omitted — there is no way for a Config to express "trigger on nothing".
7. Each entry in `trigger.phases` and `trigger.statuses` is validated on admission/parse against the canonical sets in `github.com/bborbe/vault-cli/pkg/domain` (TaskPhase: todo, planning, in_progress, ai_review, human_review, done; TaskStatus: todo, in_progress, backlog, completed, hold, aborted). An invalid entry rejects the Config just like any other CRD validation failure today.
8. Editing only the `trigger` block of a Config causes the controller to reload that Config; identical Configs (including identical triggers) do not churn the informer.
9. Skip-log lines from the handler remain structured and human-readable when a per-Config trigger filters an event, so an operator can tell which dimension rejected the task.

## Constraints

- The Config CRD schema must remain backward compatible: Configs serialized without `trigger` must round-trip and produce identical spawn decisions to today.
- The verdict/result schema (`{status, message, output}`) is unchanged.
- The Kafka task-event envelope is unchanged.
- Generated code (deepcopy, clientset) must be regenerated via the existing `make generatek8s` target in `task/executor`. No hand-written deepcopy.
- `ConfigSpec.Equal` and `ConfigSpec.Validate` must be updated together with the new field; omitting either breaks the controller informer logic or admission.
- Default phases list stays exactly `{planning, in_progress, ai_review}`. Default status stays exactly `{in_progress}`. These defaults are the contract — do not rename, reorder, or drop values during this work.
- Existing metric labels `skipped_phase` and `skipped_status` stay the counters incremented when the new per-Config trigger rejects an event. No new metric label is introduced.
- `docs/agent-crd-specification.md` is updated to describe the new `spec.trigger` field (phases + statuses + default-fallback semantics) so institutional memory of the contract outlives this spec.
- The handler's existing filter order (empty message → malformed JSON → empty identifier → completed-task taskstore cleanup → status → phase → stage → assignee) must be preserved. The only change is that the status and phase predicates consult the resolved Config instead of a hardcoded list.
- Resolving the Config earlier (before the status/phase filter) is allowed and expected. `ErrConfigNotFound` must still produce the existing `skipped_unknown_assignee` metric + warning log and must not cause an error return.
- Domain lookup table `TaskStatuses.Contains` already exists in `vault-cli/pkg/domain/task.go` — reuse it, do not reimplement.

## Assumptions

- The Kafka task-event payload exposes both `phase` and `status` on `task.Frontmatter` today (verified: existing handler reads both via `task.Frontmatter.Phase()` and `task.Frontmatter.Status()`).
- The `domain.TaskPhase.Validate` and `domain.TaskStatus.Validate` functions already exist and return a wrapped validation error for unknown values.
- The task-executor `make generatek8s` target (which runs `hack/update-codegen.sh`) regenerates deepcopy + clientset for all `v1` types in one shot. No additional generator config changes are required for a new nested struct.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Config manifest sets `trigger.phases: [bogus_phase]` | CRD validation rejects the Config with a wrapped validation error naming the invalid value. | Operator fixes the manifest and re-applies. |
| Config manifest sets `trigger.statuses: [bogus_status]` | CRD validation rejects the Config with a wrapped validation error naming the invalid value. | Operator fixes the manifest and re-applies. |
| Config manifest omits `trigger` entirely | Executor uses default phases (planning, in_progress, ai_review) and default status (in_progress) for that assignee. No warning. | None needed. |
| Event arrives for an assignee with no matching Config | Executor logs the existing "unknown assignee" warning, increments `skipped_unknown_assignee`, and does not spawn. | Operator creates the Config if the assignee is expected. |
| Event phase passes per-Config trigger but status does not | Executor increments `skipped_status`, logs a skip line naming the task and the status, and does not spawn. | None — working as intended. |
| Event status passes per-Config trigger but phase does not | Executor increments `skipped_phase`, logs a skip line naming the task and the phase, and does not spawn. | None — working as intended. |
| Config manifest sets `trigger: {}` (empty object, both lists absent) | Treated identically to `trigger` being omitted: defaults apply for both dimensions. | None — working as intended. |
| Two Configs share the same assignee | Existing resolver behavior (first match wins) is unchanged; only one Config's trigger list is consulted. | Out of scope for this spec. |

## Security / Abuse Cases

- The Config CRD is cluster-scoped/namespace-scoped k8s state, only writable by operators with RBAC. No new trust boundary is introduced.
- The event payload's `phase` and `status` strings are already parsed by the current handler and must continue to be treated as untrusted input: unknown/empty values must simply fail the trigger check, never panic and never loop.
- A pathological Config with an enormous `phases` or `statuses` list is bounded by the fixed domain vocabulary (6 phases, 6 statuses max) — CRD validation should reject any unknown value, so the list can be at most O(12) in total. No DoS surface is added.
- No file paths, URLs, or shell-substituted strings are introduced; the new field is a pure enum list.

## Acceptance Criteria

- [ ] The Config CRD exposes an optional `spec.trigger` object with optional `phases` and `statuses` lists, each validated against the canonical task phase/status vocabulary.
- [ ] `ConfigSpec.Equal` returns false when two specs differ only by the new `Trigger` field (covers nil vs non-nil, same list vs different list, same list vs reordered list).
- [ ] `ConfigSpec.Validate` rejects a Config whose `Trigger.Phases` or `Trigger.Statuses` contains a value outside the domain vocabulary, and accepts a Config with a nil or empty `Trigger`.
- [ ] The executor consults the per-Config `Trigger` on every task event before deciding whether to spawn. When `Trigger` (or the relevant sub-list) is nil or empty, the hardcoded default (phases: planning/in_progress/ai_review; statuses: in_progress) is applied.
- [ ] When the event phase is in the Config's trigger phases and the event status is in the Config's trigger statuses, and all other existing filters pass, the Job is spawned and `JobsSpawnedTotal` + `TaskEventsTotal{result=spawned}` are incremented.
- [ ] When the trigger filters reject an event, the correct label (`skipped_phase` or `skipped_status`) is incremented and no Job is spawned.
- [ ] Regenerated deepcopy code compiles and passes `make test` inside `task/executor`.
- [ ] Handler-level tests cover at least the seven scenarios enumerated in Verification below.
- [ ] Existing Config manifests in the repo (including `agent/claude/k8s/agent-claude.yaml`) continue to apply unchanged and produce identical spawn decisions.
- [ ] `docs/agent-crd-specification.md` documents the new `spec.trigger` field, its `phases`/`statuses` lists, and the default-fallback semantics (omitted / empty list / empty object → defaults).

## Verification

Run inside `task/executor`:

```
make generatek8s
make precommit
```

Test suites that must pass:

```
go test ./k8s/apis/agent.benjamin-borbe.de/v1/...
go test ./pkg/handler/...
```

Behavioral scenarios that must be added to `pkg/handler/task_event_handler_test.go`:

1. Resolved Config has `Trigger == nil` → event with phase=in_progress and status=in_progress spawns (default behavior preserved).
2. Resolved Config has `Trigger.Phases = [todo]` and `Trigger.Statuses = nil` → event with phase=todo and status=in_progress spawns.
3. Resolved Config has `Trigger.Phases = nil` and `Trigger.Statuses = [completed]` → event with phase=in_progress and status=completed spawns.
4. Resolved Config has `Trigger.Phases = [done]` and `Trigger.Statuses = [completed]` → matching event spawns; non-matching event does not.
5. Event phase matches Config trigger but status does not → `skipped_status` metric incremented, no spawn.
6. Event status matches Config trigger but phase does not → `skipped_phase` metric incremented, no spawn.
7. Config has `Trigger = &Trigger{Phases: []TaskPhase{}, Statuses: []TaskStatus{}}` (both lists empty) → behaves exactly like scenario 1 (defaults apply).

CRD-level scenarios that must be added to `types_test.go`:

- `Equal` returns false when only `Trigger` differs (nil vs set, set vs different, set vs reordered).
- `Validate` passes with nil `Trigger`, with empty-list `Trigger`, and with any combination of valid entries.
- `Validate` fails with a wrapped validation error when any entry is outside the canonical domain set.

## Do-Nothing Option

Keep the hardcoded global allow-list and either (a) grow it with the union of every future agent's needs — making every agent react to every phase/status, which breaks least-surprise — or (b) fork the executor per agent type, which duplicates spawner, controller, and informer infrastructure. Neither path scales as new agent Configs are added. The current approach is acceptable only as long as every agent really does want the exact same trigger contract; once a single agent needs a different one, this spec is the minimum viable move.
