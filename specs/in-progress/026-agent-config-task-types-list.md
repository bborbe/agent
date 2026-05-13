---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-13T19:50:24Z"
generating: "2026-05-13T19:50:25Z"
prompted: "2026-05-13T19:55:29Z"
verifying: "2026-05-13T20:16:55Z"
branch: dark-factory/agent-config-task-types-list
---

## Summary

- Spec 022 (shipped 2026-05-13) added a singular `spec.taskType` field to the `agent.benjamin-borbe.de/v1` Config CRD. Each agent declares exactly one task type today.
- Some agents need to handle MORE than one task type. Concrete trigger: spec 024 (OAuth probe) publishes `task_type: oauth-probe` tasks intended for every agent. Today there is no way to make e.g. `maintainer-agent-pr-reviewer` accept both `pr-review` AND `oauth-probe` — the CRD has one slot.
- This spec adds `spec.taskTypes` (list of strings) to the same Config CRD as a **purely additive schema change**. It does NOT change executor routing behavior in this spec.
- The executor's existing assignee-based filter is unchanged here. Enforcing `task.task_type` against the agent's effective set is a follow-up spec (`agent-executor-task-type-filter.md`) that depends on this one.
- Strictly additive — no migration required. CRs that set only `taskType` continue to work byte-for-byte. CRs that set only `taskTypes` are new. CRs that set both are valid.
- `taskType` is marked deprecated in the CRD doc but stays functional indefinitely. A future spec can remove it once no in-cluster CR uses it and the filter spec is shipped.

## Problem

The Config CRD's singular `spec.taskType` field models each agent as handling exactly one task type. Two operational needs are blocked by this:

1. **OAuth probe (spec 024) routing**: the probe wants to publish `task_type: oauth-probe` tasks targeted at every agent's PVC so each Claude credential is exercised weekly. Today there is no field to declare "this agent additionally accepts `oauth-probe`" alongside its primary type.
2. **Future multi-type agents**: an agent that legitimately spans two task types (e.g. `pr-review` + `pr-comment-reply` in one image) has no representation in the CRD.

This is the schema decision spec 022 explicitly deferred. The fix is one additional optional field, ordered so the resolver-side enforcement is decoupled and shippable later.

## Goal

After this spec, a Config CR may declare:
- only `spec.taskType` (existing behavior, no change), OR
- only `spec.taskTypes` (new field, list of strings), OR
- both (additive, no precedence rule needed in this spec).

The CRD admission validator rejects a CR that sets neither field. The CRD's Go types, OpenAPIV3 schema, `Equal` method, `Validate` method, and generated code (`zz_generated.deepcopy.go`, applyconfiguration) are all updated.

The executor's resolver and task-event handler are **NOT modified by this spec**. Existing in-cluster CRs that only set `taskType` continue to be routed exactly as today. Enforcement of `task.task_type` against the agent's declared types is a follow-up spec — see `agent-executor-task-type-filter.md`.

## Non-goals

- **No executor filter behavior change.** The task-event handler continues to route by assignee alone. Enforcement of `task.task_type` against the agent's declared types is `agent-executor-task-type-filter.md` (a sibling spec) and depends on this one.
- **No removal of `taskType` (singular)** in this spec. It stays in the schema, stays functional, gains a `// Deprecated:` doc comment. A separate future spec deprecates-and-removes once nothing relies on it.
- **No migration of existing in-cluster CRs.** Operators may migrate from `taskType` to `taskTypes` at their own pace; nothing in this change forces it.
- **No agent main.go changes.** Each agent's binary still reads `TASK_TYPE` from env (set by the executor based on the routed task) and decides how to handle it. Agents that want to accept multiple types must branch in their own main.go in follow-up work — out of scope here.
- **No Kafka topic or schema change.** The task event payload format is unchanged.
- **No release.** Tagging `vX.Y.Z` + `lib/vX.Y.Z` and bumping downstream consumers are separate sibling work.

## Alternatives Considered

- **Replace `taskType` entirely with `taskTypes`** (breaking change). Forces every in-cluster CR to migrate atomically with the deploy. Operationally painful for a feature that is purely additive in behavior. Rejected.
- **`taskTypes` wins, `taskType` silently ignored when both set.** Cleaner long-term but creates a surprise mode where an operator who adds `taskTypes` without removing `taskType` thinks both are honored. Rejected in favor of union semantics.
- **CRD admission rejects CRs with both set.** Forces clean migration but breaks atomic adds (operator can't add `taskTypes` first and remove `taskType` second on the same CR). Rejected.
- **Encode multi-type as a comma-separated string in `taskType`.** Stringly-typed, requires every consumer to parse, breaks CRD validation patterns. Rejected.

## Desired Behavior

1. The `agent.benjamin-borbe.de/v1` Config CRD schema gains a new optional field `spec.taskTypes` of type `[]string`. Each element must match `^[a-z0-9-]+$` and be at most 63 characters (mirrors the existing `spec.taskType` constraint).
2. The existing `spec.taskType` field stays in the schema. Its Go doc gains a `// Deprecated: prefer spec.taskTypes (list).` comment.
3. CRD admission validation requires **at least one of** `spec.taskType` (non-empty) or `spec.taskTypes` (non-empty list) to be present. A Config CR with neither is rejected.
4. A Config CR that sets BOTH `taskType: pr-review` and `taskTypes: [oauth-probe, pr-review]` is accepted. No precedence rule, no warning, no error — both fields coexist.
5. The executor's existing task-event handler is unchanged: it continues to look up the Config CR by `task.frontmatter.assignee` and to route accordingly. It does NOT read `taskTypes` from the resolved Config in this spec.
6. Two Config CRs that differ only in their `taskTypes` are not equal (covered by the existing `ConfigSpec.Equal` contract — extended to include the new field).

## Constraints

- Change is confined to: `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` (Go struct), `task/executor/pkg/k8s_connector.go` (OpenAPIV3Schema + validation), generated code (`zz_generated.deepcopy.go`, applyconfiguration), tests, `docs/agent-crd-specification.md`, and root `CHANGELOG.md`. **The executor's task-event handler is NOT modified.** No file in `lib/*`, `task/controller/*`, `agent/*`, `prompt/*` is modified, no downstream `go.mod` is bumped.
- The existing `spec.taskType` field signature is unchanged. No JSON tag rename, no type change. Only adds a `// Deprecated:` doc comment.
- The `ConfigSpec.Equal` method is updated to compare the new `TaskTypes` slice (order-sensitive, matching existing field comparison style — see `Equal` for `taskType` as the template).
- The `ConfigSpec.Validate` method is updated to enforce the at-least-one-of rule and the per-element pattern.
- Codegen drift is zero after a round-trip: `make precommit` must include `make generate` + `make test` and exit 0.
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks per project convention.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required.
- Project tag policy from `CLAUDE.md` still applies if a release is cut from the merge: paired `vX.Y.Z` + `lib/vX.Y.Z` tags. Cutting that release is NOT in scope.

## Assumptions

- The OpenAPIV3Schema's `x-kubernetes-validations` (CEL) or `oneOf`/`anyOf` constructs are available in the cluster's k8s API server version (spec 022 used schema-level pattern + maxLength constraints; spec 015 used `oneOf` elsewhere).
- No downstream consumer of the Config CR struct relies on `taskType` being the only field naming a type. The executor's resolver continues to read `taskType` only; `taskTypes` is stored but not yet consumed (filter is the sibling spec's job).
- Existing in-cluster Config CRs all set `taskType`, so the "at least one of" admission rule does not reject any current CR on schema upgrade.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Config CR has neither `taskType` nor `taskTypes` | Admission rejects with a clear message naming both fields | Operator sets at least one |
| Config CR has `taskTypes: []` (empty list) but `taskType: foo` set | Accepted | None — by design |
| Config CR has `taskType: ""` (empty string) and `taskTypes: [foo, bar]` | Accepted | None — by design |
| Config CR has both `taskType: foo` AND `taskTypes: [foo, bar]` (overlap) | Accepted; both fields stored as-is, no precedence rule applied in this spec | None — by design; follow-up filter spec defines semantics |
| Config CR has `taskTypes: [Foo-Bar]` (uppercase or invalid chars) | Admission rejects per the `^[a-z0-9-]+$` pattern on each element | Operator fixes the value |
| Existing CR loaded after schema bump | Loads cleanly — `taskTypes` is optional, defaults to empty slice / nil. Resolver and handler behave identically to today. | None |
| `make precommit` flags drift after `make generate` | Implementation regenerates and commits the drift | None — caught at the verification rung |

## Security / Abuse Cases

- The `taskTypes` field is constrained by the same pattern (`^[a-z0-9-]+$`, maxLength 63) as the existing `taskType` field. No injection surface beyond what already exists.
- Adding a list field does not change the trust model: only cluster operators can write Config CRs (via standard k8s RBAC).
- A misconfigured Config CR that overlaps task types with another agent does not violate isolation — it only affects routing within the executor. Existing log lines (`skip task <id>: unknown task_type ...`) surface mismatches.

## Acceptance Criteria

- [ ] `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` `ConfigSpec` struct has a new field `TaskTypes []string` with JSON tag `taskTypes,omitempty`.
- [ ] The existing `TaskType` field gains a `// Deprecated: prefer TaskTypes (list).` doc comment.
- [ ] `ConfigSpec.Equal` compares `TaskTypes` slice (order-sensitive).
- [ ] `ConfigSpec.Validate` enforces (a) at least one of `TaskType` non-empty or `TaskTypes` non-empty list, (b) per-element pattern `^[a-z0-9-]+$` on `TaskTypes`, (c) per-element maxLength 63 on `TaskTypes`.
- [ ] `OpenAPIV3Schema` in `task/executor/pkg/k8s_connector.go` exposes `taskTypes` array property with item pattern + maxLength, and includes a schema-level at-least-one-of rule.
- [ ] Generated code (`zz_generated.deepcopy.go`, applyconfiguration) is regenerated and round-trips cleanly via `make generate`.
- [ ] **Executor task-event handler is unchanged.** The existing assignee-based routing still works byte-for-byte for CRs that set only `taskType`.
- [ ] Existing tests for the singular `taskType` path continue to pass.
- [ ] New table-driven tests on `Validate`: only `taskType` set → accept, only `taskTypes` set → accept, both set → accept, neither set → reject, invalid char in `taskTypes` element → reject.
- [ ] New tests on `Equal`: differing `taskTypes` slices return false; identical slices return true.
- [ ] `docs/agent-crd-specification.md` adds `taskTypes` to the Fields table, flags `taskType` as deprecated (with the at-least-one-of contract documented), and shows an example with both set. Doc notes that filtering on `taskTypes` is a follow-up spec.
- [ ] A bullet under `## Unreleased` in root `CHANGELOG.md` describes the additive schema change.
- [ ] `make precommit` is clean in `task/executor`.

## Scenario Coverage

**No new scenario.** This is a CRD schema extension + a one-line resolver change. The failure modes are all reachable via unit tests against `ConfigSpec.Validate` and the resolver. The schema-level admission rule is reachable via the existing CRD round-trip tests (spec 022 established the pattern). End-to-end behavior — does the cluster's k8s API server accept the schema, do existing CRs still load — is verified by deploying to dev and confirming all 5 existing dev agents still pick up real tasks. That verification is part of acceptance, not a scenario.

## Verification

```
cd task/executor
make precommit
```

`make precommit` runs lint, license, gosec, trivy, generate-drift, and go test.

Manual smoke check (gated on a future release that ships this change to dev):

```
# Deploy executor with new schema
# Confirm all 5 dev Config CRs still load (no admission rejection):
kubectlquant -n dev get configs.agent.benjamin-borbe.de
# Expect: 5 rows, none in error state

# Add a taskTypes list to one CR and confirm acceptance:
kubectlquant -n dev edit config.agent.benjamin-borbe.de agent-claude
# add: spec.taskTypes: [oauth-probe]
# Save. kubectl apply should succeed.

# Existing real tasks (pr-review, etc.) continue to route normally — assignee-based
# matching is unchanged. taskTypes is stored on the Config but not yet enforced.
```

Filter-side enforcement is verified in the follow-up spec `agent-executor-task-type-filter.md`. Acceptance for THIS spec stands on `make precommit` + the unit tests on `Validate`/`Equal`.

## Do-Nothing Option

Ship nothing. The CRD continues to model "one agent = one task type" forever. Future multi-type agents are impossible without re-opening this design. The OAuth probe and any similar cross-cutting probe/maintenance task type cannot be cleanly modeled.

The cost of THIS spec is small (one new field, one validation rule, a few tests, one CHANGELOG bullet) and is confined to the CRD module. It pays back on every future change — without the schema field, no follow-up filter spec can be written.
