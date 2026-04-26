---
status: completed
summary: Added AgentStatusInProgress enum value to lib/delivery/status.go, added handling cases in applyStatusFrontmatter and kafkaResultDeliverer.DeliverResult preserving phase from incoming task, added tests for both delivery paths, and updated CHANGELOG.md.
container: agent-080-add-agent-status-in-progress
dark-factory-version: v0.135.19-1-gc08c946
created: "2026-04-26T18:00:47Z"
queued: "2026-04-26T18:00:47Z"
started: "2026-04-26T18:01:01Z"
completed: "2026-04-26T18:07:43Z"
---

<summary>
- Adds `AgentStatusInProgress AgentStatus = "in_progress"` to `lib/delivery/status.go` — the fourth value in the `AgentStatus` enum.
- Semantic: "step completed, save progress, do NOT advance the phase". Used by multi-step phase handlers to commit intermediate state during a single phase invocation.
- Updates `applyStatusFrontmatter` (file delivery path) to handle the new status: set `status: in_progress`, leave `phase` unchanged.
- Updates `kafkaResultDeliverer.DeliverResult` switch (kafka delivery path) to handle the new status: set `status: in_progress`, leave `phase` unchanged from incoming task frontmatter.
- Tests cover both delivery paths × the new status × phase preservation.
- No changes to existing statuses (`done`, `failed`, `needs_input`) — fully additive.
- `NextPhase` is ignored on `AgentStatusInProgress` (in-place save by definition).
</summary>

<objective>
Introduce a first-class "save without advancing" agent status so multi-step phase handlers can commit step-level progress without conflating with `Status: done`. After this change, an agent that has completed step N of M within a phase can emit `Status: in_progress` to save its work; the controller writes the update without changing `phase`. Phase-unaware agents continue to work unchanged because they never emit this status.

Concrete motivation: `agent/backtest` `in_progress` phase needs to save `## Plan` after synthesis, then save `frontmatter.backtest_id` after triggering, then save `## Result` after fetching metrics — all within the same phase, all writing different parts of the task. Today this requires the convention "Status: done with empty NextPhase = in-place save", which is fragile (`done` semantically suggests terminal). A first-class status makes intent explicit.
</objective>

<context>
Read `CLAUDE.md` at repo root for project conventions.

Read these guides before writing code (in coding plugin docs):
- `go-error-wrapping-guide.md` — `github.com/bborbe/errors`, never `fmt.Errorf`
- `go-testing-guide.md` — Ginkgo/Gomega, external `_test` package

Note: existing `AgentStatus` enum predates `go-enum-type-pattern.md` and lacks `Available*`, `Validate`, `Contains` methods. This prompt is purely additive — do NOT retroactively add those methods (out of scope; would be a separate refactor).

**Files to read in full before editing:**

- `lib/delivery/status.go` — defines the `AgentStatus` enum and `AgentResultInfo` struct. Add the new constant here (step 1).
- `lib/delivery/content-generator.go` — `applyStatusFrontmatter` switch handles each status. Add a case for the new status (step 2). Note: existing default case routes anything-not-explicit to `human_review` — the new case must come BEFORE the default.
- `lib/delivery/result-deliverer.go` — `kafkaResultDeliverer.DeliverResult` has its own switch on `result.Status` (around lines 136–155 — anchor by method name, not line numbers). Add a case for the new status (step 3).
- `lib/delivery/content-generator_test.go` — extend, don't create parallel files.
- `lib/delivery/result-deliverer_test.go` — extend.

**Design contract for `AgentStatusInProgress`:**

| Path | `status` after | `phase` after | `NextPhase` |
|---|---|---|---|
| File delivery (`applyStatusFrontmatter`) | `in_progress` | unchanged from incoming | ignored |
| Kafka delivery (`kafkaResultDeliverer`) | `in_progress` | unchanged from incoming | ignored |

**Why phase is preserved unchanged:**
- The agent is mid-phase. Phase advance happens only on `AgentStatusDone` with `NextPhase` set.
- This enables in-place section commits (e.g. `## Plan` written during `phase: in_progress` synthesize-and-execute path) without controller-loop risk.

**Why NextPhase is ignored:**
- Semantic clarity: `in_progress` means "this phase isn't done yet". Phase advance is contradictory.
- If an agent wrongly sets both `Status: in_progress` AND `NextPhase`, log a warning and ignore `NextPhase` (do not error — agents should not crash for misuse).

Grep before editing:
```bash
grep -n "AgentStatus" lib/delivery/*.go | grep -v _test.go
grep -n "applyStatusFrontmatter\|switch result.Status" lib/delivery/*.go | grep -v _test.go
```
</context>

<requirements>

## 1. Add `AgentStatusInProgress` to `lib/delivery/status.go`

Append to the `const` block:

```go
// AgentStatusInProgress indicates the agent has completed a step within the current phase
// and saved partial state, but the phase is not yet complete. The controller writes the update
// without advancing the phase. Used by multi-step phase handlers for in-place progress saves.
// NextPhase is ignored on this status.
AgentStatusInProgress AgentStatus = "in_progress"
```

Place it after `AgentStatusNeedsInput` to keep the enum order coherent (terminal → escalation → in-flight).

## 2. Update `applyStatusFrontmatter` in `lib/delivery/content-generator.go`

Add a case BEFORE the default:

```go
case AgentStatusInProgress:
    // Step-level progress save: keep status: in_progress, preserve phase from incoming task.
    // Multi-step phase handlers use this to commit ## Plan / ## Result / etc. mid-phase
    // without triggering a phase transition.
    content = SetFrontmatterField(content, "status", "in_progress")
    // phase intentionally not modified — preserves the agent's current phase for in-place save
```

Verify the existing default-case behavior is preserved for `AgentStatusFailed` and any unknown status.

## 3. Update `kafkaResultDeliverer.DeliverResult` switch in `lib/delivery/result-deliverer.go`

Add a case BEFORE the default in the `switch result.Status` block:

```go
case AgentStatusInProgress:
    // Step-level progress save: keep status: in_progress, preserve phase from incoming
    // task frontmatter (already copied from fmMap above). NextPhase ignored on this status —
    // log a warning if the agent set both.
    if result.NextPhase != "" {
        glog.Warningf("task %s: ignoring NextPhase %q on Status: in_progress (in-place save)",
            d.taskID, result.NextPhase)
    }
    frontmatter["status"] = "in_progress"
    // phase intentionally not modified — preserves incoming phase
```

Verify the existing `AgentStatusDone`, `AgentStatusNeedsInput`, and `default` (failed/unknown) cases are unchanged.

## 4. Tests

In `lib/delivery/content-generator_test.go`, add cases asserting:

- `applyStatusFrontmatter(content, AgentStatusInProgress)` returns content with `status: in_progress` and `phase` unchanged from the input.
- **Test fixture MUST have a non-default phase in the input** (e.g. `phase: planning` — NOT `phase: human_review`). Use `phase: planning` so the assertion "preserved" is distinguishable from "default-routed-to-human_review" (the default-case behavior).
- Concrete fixture:
  ```
  ---
  status: in_progress
  phase: planning
  ---
  body content
  ```
  After `applyStatusFrontmatter(input, AgentStatusInProgress)`: `phase: planning` MUST remain unchanged. Assert via `SetFrontmatterField(...)` round-trip OR `ParseMarkdownFrontmatter(...)` to extract and compare.

In `lib/delivery/result-deliverer_test.go`, add cases asserting:

- `kafkaResultDeliverer.DeliverResult` with `Status: AgentStatusInProgress` produces a CQRS update where `frontmatter[status] == "in_progress"` and `frontmatter[phase]` equals the incoming task's phase (e.g. set up the test with originalContent containing `phase: planning`, then assert the published frontmatter has `phase: planning` after delivery — NOT overwritten to `human_review` or `done`).
- `kafkaResultDeliverer.DeliverResult` with `Status: AgentStatusInProgress` AND `NextPhase: "ai_review"` produces the SAME frontmatter result as without NextPhase (NextPhase ignored — assert `frontmatter[phase]` still equals the incoming task's phase, NOT `ai_review`). The warning log itself does not need to be asserted (Ginkgo doesn't capture glog by default — observable behavior is `phase` unchanged).
- Tests for the existing three statuses (`done`, `failed`, `needs_input`) still pass — no regression.

Use existing test fixtures and helpers — extend, don't duplicate.

## 5. CHANGELOG

Add an entry under `## Unreleased` (or create the section if it doesn't exist):

```
- feat(lib): add AgentStatusInProgress for step-level in-place saves; preserves phase frontmatter, ignores NextPhase. Enables multi-step phase handlers to commit intermediate state without triggering phase advance.
```

## 6. Verify

```bash
make test
```

Exit code 0; new test cases visible in output.

```bash
make precommit
```

Exit code 0.

</requirements>

<constraints>
- Status enum is a `string` type — the new constant value MUST be `"in_progress"` exactly (matches the existing TaskPhase value of the same name in vault-cli, for symmetry)
- **Namespace clarification**: the Go enum value `AgentStatusInProgress = "in_progress"` is intentionally identical to the frontmatter status string `"in_progress"` already written by NeedsInput / Failed / default / non-terminal-Done paths. The two are distinct namespaces (Go enum on the wire from agent → controller; YAML frontmatter on disk). Do NOT refactor existing paths to use the new enum. The existing paths produce `frontmatter[status] = "in_progress"` from a different semantic origin (escalation / mid-flight tracking); collapsing them would change behavior.
- `AgentStatusInProgress` case must come BEFORE the default case in both switch statements (default catches `AgentStatusFailed` + any unknown)
- Phase is NOT modified on `AgentStatusInProgress` — the incoming phase from task frontmatter survives untouched
- `NextPhase` is ignored on `AgentStatusInProgress` — log a warning, don't error
- No changes to existing `AgentStatusDone`, `AgentStatusFailed`, `AgentStatusNeedsInput` semantics
- No changes to the `AgentResultInfo` struct (no new fields)
- Errors via `github.com/bborbe/errors` — never `fmt.Errorf`
- No `time.Now()` or `context.Background()` in business logic
- Do NOT modify `lib/claude/` (this change is delivery-layer only)
- Do NOT commit — dark-factory handles git
- All existing tests must still pass
</constraints>

<verification>
```bash
make test
```
Must exit 0.

Check the new constant:
```bash
grep -n "AgentStatusInProgress" lib/delivery/status.go
```
Must show exactly one definition with value `"in_progress"`.

Check both switch statements have the new case:
```bash
grep -n "AgentStatusInProgress" lib/delivery/content-generator.go lib/delivery/result-deliverer.go
```
Must show usage in both files (case clauses).

Check tests pass:
```bash
go test -v ./lib/delivery/... 2>&1 | tail -30
```
All test cases pass; new in_progress cases visible in output.

Check no regression in claude package:
```bash
go test ./lib/claude/... 2>&1 | tail -10
```
Exit 0.

```bash
make precommit
```
Must exit 0.
</verification>
