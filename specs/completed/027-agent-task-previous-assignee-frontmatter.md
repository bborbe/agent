---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-05-13T20:00:57Z"
generating: "2026-05-13T20:00:57Z"
prompted: "2026-05-13T20:05:48Z"
verifying: "2026-05-13T20:24:33Z"
completed: "2026-05-13T21:18:22Z"
branch: dark-factory/agent-task-previous-assignee-frontmatter
---

## Summary

- Today, when the controller clears `assignee: ""` on any escalation path, the previous assignee is preserved only inside the body of the appended escalation section as a markdown bullet (`- **Assignee:** <name>`). This makes operator-inbox queries body-grep-only: there is no queryable frontmatter source of truth for "which agent parked this task".
- This spec adds a `previous_assignee` frontmatter field to the task file, written by the controller's result writer on every assignee-clear operation, alongside the existing body-side bullet. The body bullet is NOT removed — it stays for human readers; the new frontmatter is the machine-queryable source of truth.
- Purely additive. Existing operator-inbox queries that filter on `assignee == ""` continue to work unchanged. New queries can filter or group by `previous_assignee == "<agent-name>"`.
- Scope: every assignee-clear path in `task/controller/pkg/result/result_writer.go` that exists at merge time (today: retry-cap, trigger-cap, `needs_input` — three paths; type-mismatch from sibling spec `agent-executor-task-type-filter.md` inherits this field automatically if/when it lands).
- Implementation centralizes the new field write at the single chokepoint that already clears assignee. If the four call sites do not currently share that chokepoint, the centralization refactor is in scope for this spec — it is the only way to guarantee future escalation paths inherit the field without per-site duplication.

## Problem

Spec 021 made `assignee: ""` the canonical operator-inbox signal: any task with empty assignee surfaces in the operator's dashboard. That solved visibility. But triage requires a follow-up question: **which agent parked this task?** Today's answer requires either:

- Reading the markdown body for the `## Retry Escalation` / `## Trigger Cap Escalation` / `## Failure` bullet, OR
- Inspecting the most-recent git commit history on the file, OR
- Looking at the `current_job` frontmatter (which is a K8s Job name with embedded agent prefix — fragile).

None of these are queryable. An operator who wants to answer "show me every task `pr-reviewer-agent` parked in the last week" must walk the vault directory and grep body content. Dashboards built on the vault's index can filter by assignee (empty / not empty) but cannot group by parked-by-agent.

The fix is one frontmatter field. The controller already writes the assignee value into the body; mirroring it into frontmatter is a 5-line change in one writer file, but unlocks query patterns operators clearly need.

## Goal

After this spec, every assignee-clear path in the controller's result writer (today: retry-cap, trigger-cap, `needs_input`) writes the previous assignee name into the `previous_assignee` frontmatter field of the same file. Future paths that also clear assignee inherit the field for free via the shared write chokepoint. The body-side bullet is preserved for human readers. The field is stable across re-deliveries (idempotency contract from spec 021 still holds). Operator-inbox queries gain the ability to filter or group by parked-by-agent without parsing body content.

## Non-goals

- **No new escalation path is introduced.** This spec only adds frontmatter to existing paths.
- **No removal of the body-side bullets.** They stay for human readability; the frontmatter is additive.
- **No frontmatter field for the CURRENT assignee history.** Only the most-recent previous assignee is captured. Multi-step history would require an array; out of scope here.
- **No change to operator-inbox query infrastructure.** This spec writes the field; consumers (dashboards, vault-cli queries) can add filters as they see fit.
- **No retroactive backfill** of `previous_assignee` on existing parked tasks. Existing tasks keep their body-side bullet; new escalations going forward get both.
- **No release.** Tag policy + downstream consumer bumps are sibling work.

## Referenced Specs

- **Spec 010** — defines `needs_input` semantics (assignee cleared, phase becomes `human_review`).
- **Spec 021** — `assignee: ""` is the canonical operator-inbox signal. Defined the assignee-clear pattern on all escalation paths. This spec extends those write paths with the new frontmatter field.
- **agent-executor-task-type-filter.md (sibling, draft)** — adds type-mismatch as a fourth assignee-clear path. NOT a hard prerequisite for this spec; the filter spec's body bullet alone is sufficient for its MVP. If it ships before this one, the type-mismatch path inherits the frontmatter field automatically via the shared chokepoint.

## Desired Behavior

1. On every assignee-clear operation by the controller's result writer (today: retry-cap, trigger-cap, `needs_input`), the writer also sets `previous_assignee: <name>` in the task's frontmatter where `<name>` is the assignee value the task carried BEFORE the clear.
2. The body-side bullet inside the escalation section (`- **Assignee:** <name>`) continues to be written unchanged. Both the body bullet AND the frontmatter field carry the same value.
3. When an operator re-delegates a parked task by setting `assignee: <new-name>`, the controller's empty-to-named transition handler (per spec 021) does NOT clear `previous_assignee`. The field persists across re-delegation so operators can see "this task was previously parked by <X>; I re-delegated to <Y>".
4. Re-delivered escalation events (Kafka at-least-once) write the same `previous_assignee` value idempotently; the value is recomputed from the pre-clear assignee on every delivery, so re-deliveries are byte-identical. (Inherits spec 021's idempotency contract.)

## Constraints

- Change is confined to `task/controller/pkg/result/result_writer.go` (the writer holding the assignee-clear call sites + the escalation body builder), generated mocks, tests, and root `CHANGELOG.md`. `docs/agent-crd-specification.md` and `docs/task-flow-and-failure-semantics.md` get one-line updates documenting the new field. No file in `lib/*`, `task/executor/*`, `agent/*`, `prompt/*` is modified.
- The frontmatter field name is `previous_assignee` (snake_case, matches existing `current_job`, `task_identifier`, `task_type` conventions).
- The field is written by the writer ONLY on assignee-clear paths. It is never written when assignee transitions to a non-empty value (that would mask history).
- The writer never reads `previous_assignee` to decide what to write. The value always comes from the pre-clear assignee at the moment of the clear.
- The field is read by no one in this spec. Consumers (vault-cli, dashboards) add their own filters in follow-up work.
- Implementation centralizes the new field write at the chokepoint that already clears assignee. If the existing assignee-clear call sites do not currently share a single helper, refactoring them to share one is in-scope here — otherwise the four-site duplication risk is real and a future fifth path would silently miss the field.
- Existing tests covering the assignee-clear paths must still pass and must be EXTENDED to assert the new frontmatter value (not added in parallel as separate test files).
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks per project convention.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required.
- Project tag policy from `CLAUDE.md` still applies if a release is cut from the merge: paired `vX.Y.Z` + `lib/vX.Y.Z` tags. Cutting that release is NOT in scope.

## Assumptions

- All four assignee-clear paths route through a single helper (or small set of helpers) in `result_writer.go`. The implementation prompt verifies and centralizes the new field write in that helper rather than duplicating the assignment at four call sites.
- The task's pre-clear assignee value is available to the writer at the moment it decides to clear (it must be — the writer reads the existing file before the merge). Implementation prompt confirms the access pattern.
- No downstream consumer relies on the absence of `previous_assignee` to decide whether a task is "fresh" — that role belongs to `assignee` (empty vs non-empty) and `task_identifier`.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Re-delivered escalation event after the field is already set | Idempotent — value is recomputed from the same pre-clear assignee, so the result is byte-identical | None |
| Operator re-delegates a parked task by setting `assignee: <new>` | Writer does NOT touch `previous_assignee`. The field persists across re-delegation. | None |
| Pre-clear assignee is empty (defensive: should never happen, but possible if upstream produces malformed state) | Implementation-prompt decision: either write `previous_assignee: ""` or omit the field entirely. The spec does not pin this; both are acceptable as long as the chosen behavior is documented in a test. | None |
| Existing parked task on disk lacks `previous_assignee` (created before this spec) | No backfill. Field stays absent. New escalations on the same task DO set the field. | None — by design |

(The happy-path rows for each escalation source — retry-cap, trigger-cap, `needs_input` — are covered in Desired Behavior #1 rather than duplicated here.)

## Security / Abuse Cases

- `previous_assignee` carries an agent name — same trust level as the current `assignee` field. No new exposure.
- No secret data flows through this field.
- DoS: no new write per task event; the field is set in the same atomic write that clears assignee. Net writes-per-event unchanged.

## Acceptance Criteria

- [ ] The controller's result writer sets `previous_assignee: <pre-clear-name>` in frontmatter on every code path that also sets `assignee: ""`. (Today: retry-cap, trigger-cap, `needs_input`.)
- [ ] All assignee-clear paths present in `result_writer.go` at merge time inherit the field via a shared helper write — not duplicated at each call site. If the shared helper does not exist today, the implementation centralizes the paths into one as part of this work.
- [ ] The body-side `- **Assignee:** <name>` bullet inside escalation sections is unchanged.
- [ ] Operator re-delegation (empty → named assignee) does NOT touch `previous_assignee`. The field persists.
- [ ] Idempotent re-delivery produces byte-identical content for the parked file (no flapping of `previous_assignee`).
- [ ] Existing tests for the three current escalation paths are EXTENDED to assert the new field value (not added as parallel tests). Each path's test verifies `previous_assignee` matches the expected pre-clear name.
- [ ] A new test covers the operator-re-delegation persistence case: after assignee-clear sets the field, a second writer cycle that sets a non-empty assignee leaves `previous_assignee` untouched.
- [ ] `docs/agent-crd-specification.md` or `docs/task-flow-and-failure-semantics.md` documents the new field with the contract (set on assignee-clear, persists across re-delegation, no backfill).
- [ ] CHANGELOG bullet under `## Unreleased`.
- [ ] `make precommit` is clean in `task/controller` — covers lint, license, gosec, trivy, generate-drift, and go test in one invocation.

## Scenario Coverage

**No new scenario.** The new field is reachable via unit tests against the writer for each of the four escalation paths. The body-side bullets are already tested today; the new test extends those same tests with an additional frontmatter assertion. End-to-end behavior (operator queries the parked-by-agent surface in a dashboard) is consumer-side and out of scope. Cluster verification happens at operator deploy time on dev (see Verification).

## Verification

```
cd task/controller
make precommit
```

Manual smoke check (operator-driven post-deploy, gated on a future release that ships this change to dev):

- Deliberately trigger one of the four escalation paths on dev (e.g. set `max_triggers: 1` on a probe task and let the executor over-trigger).
- After the controller writes back, inspect the vault file: `previous_assignee: <agent-name>` is present in frontmatter, the body bullet is also present.
- Set the task's `assignee` back to the agent name. Confirm `previous_assignee` is unchanged.

Acceptance for THIS spec stands on `make precommit` + the extended unit tests on the four writer paths.

## Do-Nothing Option

Ship nothing. Operator-inbox queries that want to group parked tasks by parked-by-agent continue to require body-content parsing. Dashboards that surface this view either implement the grep client-side (fragile, slow, no index) or rely on `current_job` parsing (fragile — that field is a K8s Job name string with embedded agent prefix, format changes break the parser).

The cost of THIS spec is small and confined to one writer file (plus tests, docs, CHANGELOG). It pays back every time an operator wants to triage parked tasks by which agent failed them — a query that grows in usefulness as the agent fleet grows.
