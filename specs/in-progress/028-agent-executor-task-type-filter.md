---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-13T20:09:39Z"
generating: "2026-05-13T20:09:39Z"
prompted: "2026-05-13T20:19:13Z"
branch: dark-factory/agent-executor-task-type-filter
---

## Summary

- Today the executor's task-event handler routes a task to an agent based on `task.frontmatter.assignee` alone. The `spec.taskType` field on the Config CRD (added 2026-05-13 via spec 022) is registry metadata only — the executor never reads it.
- This spec enforces type-correctness: before spawning a Job, the executor checks that the task's `task_type` is in the agent's declared types. If not (or if `task_type` is missing entirely), the task is rejected with an operator-readable failure body and `assignee: ""` — the same escalation shape spec 021 uses for retry-cap / trigger-cap escalations.
- Depends on `agent-config-task-types-list.md` (sibling spec that adds `spec.taskTypes` to the CRD). The filter reads from both `spec.taskType` (singular, deprecated) and `spec.taskTypes` (list), unioned into a single effective set.
- No new Kafka topic, no new alert rule. Rejections flow through the existing `assignee: ""` operator-inbox signal that spec 021 made the canonical surface for "task needs human attention". Rejected tasks land at `phase: ai_review` (not `human_review`) — same shape as transient infra failures, because the cause is a routing error the operator can resolve by editing the task or the Config.

## Referenced Specs (one-line glosses)

- **Spec 009** — executor synthesizes a `failed` result when a Job exits non-zero. Introduced the synthetic-failure publish path this spec reuses.
- **Spec 010** — split `failed` (transient, retried) vs `needs_input` (operator-resolvable, NOT retried). Type mismatch follows the `needs_input` counter-semantics.
- **Spec 021** — `assignee: ""` is the canonical operator-inbox signal. Set on every escalation path; this spec adds type mismatch as another such path.
- **Spec 022** — added singular `spec.taskType` to the Config CRD. Today registry metadata only.
- **Spec 024** — weekly OAuth probe. Concrete consumer of this filter (probes route via `task_type: oauth-probe` and need the filter to validate routing).
- **agent-config-task-types-list.md (sibling, draft)** — adds plural `spec.taskTypes` list to the CRD. Must merge before this spec can be approved.
- **agent-task-previous-assignee-frontmatter.md (sibling, approved as spec 027)** — adds `previous_assignee` frontmatter via a single chokepoint in the controller's result writer. This spec's synthetic-failure path inherits the field automatically once 027 ships; if 027 lands first, the previous-assignee bullet in `## Failure` is reinforced by a queryable frontmatter field with no work in this spec.

## Problem

Two specific failure modes happen silently today:

1. **Misrouted task**: an operator creates a task with `assignee: agent-pr-reviewer` and `task_type: oauth-probe`. The executor spawns a `pr-reviewer` Job which then fails inside the agent's prompt parser (e.g. "missing clone_url"). The agent emits a `failed` result; the controller retries. Eventually the retry cap is hit and the task escalates. The operator sees a Failure body that says "missing clone_url" — useful, but the root cause is a routing mistake the executor could have detected before any Job was spawned.
2. **Probe routing breakage**: spec 024's OAuth probe publishes `task_type: oauth-probe` against every agent. Most agents don't accept that type, so probes hit the same misrouted-task fate above — wasted Jobs + meaningless Failure bodies until retry-cap.

A pre-spawn type check eliminates the wasted Jobs and produces a more accurate Failure body ("agent does not accept task_type X") immediately. The operator gets a faster, clearer signal.

## Goal

After this spec, the executor reads the agent's effective task-type set (`{cfg.TaskType} ∪ set(cfg.TaskTypes)`, skipping empty `TaskType`) on each task-event evaluation. If the task's `task_type` is not in that set, the executor synthesizes a failure (same shape as spec 009's Job-failure synthesis) carrying an operator-readable Reason explaining the type mismatch. The controller writes that failure back: `## Failure` body section appended, `assignee: ""` cleared, phase preserved at its current stage. No Job is spawned. The operator sees the rejected task in the existing assignee-empty operator inbox surface and re-delegates after correcting either the task's `task_type` or the agent's `taskTypes` declaration.

## Non-goals

- **No CRD schema change.** Sibling spec adds the field; this spec only enforces.
- **Singular `taskType` is NOT removed.** It still contributes to the effective set (unioned with `taskTypes`). Deprecation is the sibling spec's concern; a separate future spec deprecates-and-removes once nothing relies on it.
- **No new metric, no new alert rule.** Rejections surface via the existing `assignee: ""` operator-inbox query and the existing failure-body content generator.
- **No automatic migration of legacy tasks.** Tasks created before this spec that lack a `task_type` field will be rejected on first event re-delivery — this is the chosen strict semantics. The operator is responsible for adding `task_type` to legacy task templates and to in-flight tasks before this spec deploys. The CHANGELOG bullet calls this out explicitly so it cannot ship as a surprise.
- **No release.** Tag policy + downstream consumer bumps are sibling work.

## Alternatives Considered

- **Permissive: empty `task.task_type` bypasses the filter.** Tasks lacking `task_type` continue to route by assignee only. Backwards-compat with legacy tasks; new probe-style tasks gain protection. **Rejected** because the user explicitly chose strict (every task must declare a type; legacy tasks without one are routing errors that should surface). The migration path is to add `task_type` to legacy task templates, not to silently route them.
- **Strict, but log-and-skip** (no synthetic failure write-back). Lighter footprint but invisible — operator doesn't see the rejection in the vault. Loses the audit-trail and operator-inbox surface that spec 021 built. **Rejected.**
- **Default `task.task_type` to the agent's `taskType` when empty.** Clever but hides misconfiguration. **Rejected.**
- **Add the filter inside each agent's main.go instead of the executor.** Distributed enforcement, every agent re-implements the check. **Rejected** — single chokepoint in the executor is simpler.

## Desired Behavior

1. After the executor resolves the Config CR by the task's assignee (the existing first-stage filter), it computes the **effective task-type set** as the union of the agent's singular `taskType` (if non-empty) and the elements of its `taskTypes` list.
2. The executor reads the task's `task_type` frontmatter value. The value may be empty or missing.
3. The task is **type-correct** iff its `task_type` is in the effective set. **Empty `task_type` is never in any effective set** — strict semantics, no opt-out.
4. **If type-correct**: the existing status/phase/stage checks proceed unchanged; the Job spawns as today.
5. **If not type-correct**: the executor synthesizes a failure result reusing the same publish path spec 009 built for synthetic Job-failure detection. The synthetic failure writes:
   - `status: in_progress` (unchanged)
   - `phase: ai_review` (same as spec 010 — task surfaces in operator inbox via the assignee-empty signal, not via `human_review` which is reserved for genuinely human-only work)
   - `assignee: ""` (operator-inbox signal per spec 021)
   - Body: original task content + appended `## Failure` section containing:
     - **Reason** — naming the mismatched type and the agent's effective types (or, for the empty case, that the task has no `task_type`)
     - **Assignee** — the previous assignee name (the agent that rejected the task), preserved as a bullet so operators can identify which agent without git-log. Mirrors the existing `## Retry Escalation` / `## Trigger Cap Escalation` body shape from spec 021.
6. Type-mismatch failures do NOT bump `trigger_count` or `retry_count` — operator-resolvable per spec 010's `needs_input` semantics.
7. The handler short-circuits after the synthetic-failure publish: no Job is spawned, no further checks run, the task event is ACKed.

## Constraints

- **Sibling-spec dependency**: `agent-config-task-types-list.md` MUST be merged and shipped before this spec's prompt is approved. Without `cfg.TaskTypes` available on the resolved Config, the filter cannot read what it needs. The dark-factory daemon should not be allowed to schedule this spec in parallel with the sibling.
- Change is confined to the `task/executor` module: the handler chokepoint, the existing synthetic-failure publisher (must be reused — no parallel failure path), generated mocks, tests, and root `CHANGELOG.md`. No file in `lib/*`, `task/controller/*`, `agent/*`, `prompt/*` is modified.
- The CRD schema is NOT modified by this spec.
- The effective-set computation is a pure helper, testable independently. No business logic in the factory.
- The handler retains existing status/phase/stage filters when the task IS type-correct. The type check is an ADDITIONAL filter layered before them.
- Type-mismatch failures must NOT bump `trigger_count` or `retry_count` (per spec 010 `needs_input` semantics).
- The CHANGELOG bullet for this spec MUST flag the strict-empty-task_type behavior so the deploy doesn't surprise operators with legacy tasks lacking the field.
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks per project convention.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required.
- Project tag policy from `CLAUDE.md` still applies if a release is cut from the merge: paired `vX.Y.Z` + `lib/vX.Y.Z` tags. Cutting that release is NOT in scope.

## Assumptions

- The existing synthetic-failure publisher introduced by spec 009 is reachable from the task-event handler and can be invoked with a custom Reason string. Verification of the exact entry point is part of the implementation prompt.
- The task's `task_type` frontmatter value is accessible via the existing `lib.TaskFrontmatter` accessor pattern (same shape as other frontmatter reads like `Status()` and `Assignee()`).
- The operator-inbox query surfacing `assignee == ""` (per spec 021) is already in production. This spec inherits that surface.
- Existing in-cluster Config CRs all declare a single `taskType` today. (Constraint: operator must ensure at least one of `taskType` or `taskTypes` is set per CR — enforced by the sibling spec's admission rule. If a CR slips through with both empty, every routed task mismatches and escalates; this surfaces as a high-volume escalation storm in the operator inbox, not silent failure.)

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Task `task_type` not in agent's effective set | Synthetic failure: Reason names the mismatched type and the effective set. `assignee: ""`. No Job. | Operator either adds the type to the agent's `taskTypes` or changes the task's `task_type` to a matching value, then re-delegates by setting assignee. |
| Task has no `task_type` at all | Synthetic failure: Reason states the task has no `task_type` and lists the agent's effective types. `assignee: ""`. No Job. | Operator adds `task_type` to the task frontmatter, then re-delegates. |
| Resolver returns ErrConfigNotFound for `task.assignee` (existing case) | Existing behavior: `skip task <id>: unknown assignee` log line, no Job spawned. Type-filter not reached. | Operator fixes assignee. |
| `task.frontmatter` deserialization fails (existing case) | Existing behavior: task event skipped with warning. | None |
| Same task event re-delivered after a synthetic-failure write (Kafka at-least-once) | Idempotent: the synthetic-failure write cleared `assignee` to `""`, so the existing empty-assignee filter catches the re-delivery before reaching the type filter. No duplicate `## Failure` section. Inherits spec 021's idempotency contract. | None |
| `make precommit` flags drift after `make generate` | Implementation regenerates and commits the drift. | None — caught at the verification rung. |

## Security / Abuse Cases

- The type filter narrows the surface of what tasks reach an agent's Job. Strictly defensive — no new trust boundary.
- The Reason string contains the agent name and effective types, both of which are operator-known cluster state. No secret exposure.
- A malicious operator who controls the Config CR could add unintended types to `taskTypes` and route arbitrary tasks. This is an existing cluster-RBAC concern, unchanged by this spec.
- DoS: a flood of misrouted tasks now produces synthetic failures instead of wasted Jobs — strictly less work. Net reduction in load.

## Acceptance Criteria

- [ ] The task-event handler computes the effective task-type set after resolving the Config CR and BEFORE the existing status/phase/stage checks.
- [ ] When the task's `task_type` is missing or not in the effective set, the handler publishes a synthetic failure (via the spec 009 publish path) with the Reason shape from Desired Behavior #5, and returns without spawning a Job.
- [ ] When the task's `task_type` IS in the effective set, the handler proceeds to existing checks unchanged. A CR with `taskType: pr-review` plus a task with `task_type: pr-review` produces byte-identical behavior to today.
- [ ] The synthetic-failure publish path on type mismatch does NOT increment `trigger_count` or `retry_count`.
- [ ] The effective-set computation is implemented as a pure helper and is unit-tested independently.
- [ ] Five behavior matrix branches are covered by handler tests: singular-only match, list-only match, overlap match, mismatch (both fields), missing task_type. Each asserts the correct outcome (Job spawn vs synthetic-failure publish).
- [ ] Existing handler tests for the assignee-only routing path continue to pass.
- [ ] `docs/agent-crd-specification.md` (and/or `task-flow-and-failure-semantics.md`) gains a row documenting type-mismatch as an escalation cause alongside trigger-cap and retry-cap.
- [ ] CHANGELOG bullet under `## Unreleased` describes the new type-filter enforcement AND explicitly flags the strict-empty-task_type behavior so operators are warned.
- [ ] `make precommit` is clean in `task/executor`.

## Scenario Coverage

**No new scenario.** This is a single-chokepoint filter addition + reuse of the existing synthetic-failure publish path. All five behavior matrices (matches singular / matches list / overlap / mismatch / missing) are reachable via unit tests against the handler with counterfeiter fakes for `ResultPublisher` and `Resolver`. End-to-end behavior is already covered transitively by spec 021 (assignee-clear surfaces in operator inbox) and spec 009 (synthetic-failure body contains the Reason). Cluster smoke verification happens at operator deploy time on dev (set a Config's `taskTypes`, send a mismatched task, observe the failure body and empty assignee in the vault file).

## Verification

```
cd task/executor
make precommit
```

Manual smoke check (operator-driven post-deploy, gated on a future release that ships this change AND `agent-config-task-types-list.md` to dev):

- Operator constrains an agent's accepted types via `kubectlquant edit` on the Config CR.
- Operator writes a task with a mismatched `task_type` into the vault via the normal git-rest path.
- Expected within seconds: no Job is spawned, the task's `assignee` is cleared to `""`, the `## Failure` body section is appended with a Reason naming the mismatch.

Acceptance for THIS spec stands on `make precommit` + the unit-test matrix; the smoke check is a sanity belt for post-deploy.

## Do-Nothing Option

Ship nothing. Misrouted tasks continue to be spawned as Jobs and to fail inside each agent's prompt parser with parser-specific error messages. Operators see Failure bodies that explain the parsing failure (e.g. "missing clone_url") but not the routing cause. Retry counters increment uselessly. Probes built on `task_type: oauth-probe` (spec 024) route to every agent and most of them park as misrouted-task escalations after retry-cap.

The cost of THIS spec is small (one new filter in one handler file, one helper function, reuse of an existing publisher, a few unit tests, one CHANGELOG bullet) and is confined to the executor module. It pays back every time a task is misrouted — operator gets a clear Reason immediately instead of a parser-specific diagnostic after several wasted Jobs.
