---
status: prompted
approved: "2026-10-31T13:54:06Z"
generating: "2026-10-31T13:54:28Z"
prompted: "2026-10-31T14:08:34Z"
branch: dark-factory/bug-failed-branch-publishes-stale-phase
---

## Summary

- The Kafka result deliverer's `AgentStatusFailed` branch republishes the **Job-start** `phase` when the Job advanced no phase, overwriting a phase a prior delivery had already parked (e.g. `human_review`).
- On 2026-10-30 this regressed task `7de48290` from `phase: human_review` to `phase: planning`; the re-driven task started a second Job 3 min 56 s later.
- Every other non-advancing branch (`Done` without `NextPhase`, `needs_input`, `in_progress`, unknown) already publishes no phase so the controller's on-disk-first merge keeps the parked value; only `Failed` does not.
- The fix makes the `Failed` branch behave like the other four: a `Failed` result that advanced nothing must not own the phase.

## Problem

`delivery/result-deliverer.go` `applyResultFrontmatter`'s `AgentStatusFailed` arm sets `status: in_progress` and publishes `phase` only when `d.advancedPhase != ""`. When the Job advanced nothing — the common case, a pod crash before any phase write — it leaves the `phase` the generator produced, and the generator rebuilds from the **Job-start snapshot** (`result.Output` for the passthrough generator). If a prior delivery parked the task at `human_review` after the Job spawned, that Job-start `planning` is stale. The controller's `MergeFrontmatter` merges `phase` incoming-wins — it guards `status`, `cluster`, `assignee`, `previous_assignee` and the counters, **not `phase`** — so the stale value overwrites the park, the task reads as claimable, and a second Job starts.

## Goal

A `Failed` result that advanced no phase publishes **no** `phase` key, so the controller's on-disk-first merge keeps whatever phase is on disk. A `Failed` result whose Job did advance still publishes the advanced phase, so a `planning → execution` run that then fails does not rewind to `planning`.

## Acceptance Criteria

- [ ] A `Failed` result with `advancedPhase == ""` publishes frontmatter **without** a `phase` key — evidence: a new spec in the `result-deliverer_test.go:1079` Context ("phase advanced in-process within one Job") reuses its existing `jobStartContent` fixture (which seeds `phase: planning`) and asserts `Expect(publishedFrontmatterAt(0)).NotTo(HaveKey("phase"))` (the Context's accessor). Reusing the seeded fixture makes the assertion non-vacuous by construction.
- [ ] A `Failed` result after a `Done + NextPhase: execution` still publishes `phase: execution` — evidence: the existing spec "keeps the retry routable on the advanced phase after a failure" (`result-deliverer_test.go:1151`) still passes with `fm["phase"] == "execution"`.
- [ ] The `Done`-without-`NextPhase`, `needs_input`, `in_progress` and unknown branches still publish no `phase` — evidence: their existing assertions `Expect(fm).NotTo(HaveKey("phase"))` at `result-deliverer_test.go:283 / :307 / :335 / :358 / :382` still pass.
- [ ] The **five existing `Failed` specs that encode the buggy contract are updated, not deleted** — each replaces its `phase`-equality with `Expect(fm).NotTo(HaveKey("phase"))` while keeping its remaining assertions (`status` / `assignee` / `previous_assignee` where its fixture has them); the 5d advanced-phase case is untouched. The five: `result-deliverer_test.go:240-261` (`:256`), `:504-525` (`:522`), `:527-547` (`:545`), `:708-728` (`:722`), `:730-750` (`:744`). ⚠️ `:527-547` has no assignee and no body assertion today — either add the `## Failure` body assertion its name promises or rename it. — evidence: those five specs pass; `grep -c 'Expect(fm\["phase"\]).To(Equal' delivery/result-deliverer_test.go` returns **13** (18 before the change); and `git diff -U0 -- delivery/result-deliverer_test.go` shows five removed `Expect(fm["phase"]).To(Equal(…))` lines and five added `Expect(fm).NotTo(HaveKey("phase"))` lines. 5d's assertion (`:1171`, `publishedFrontmatterAt(1)["phase"]`) is unchanged and does not use the `fm` map.
- [ ] `make precommit` exits 0.
- [ ] `go test ./delivery/ -count=1` exits 0.
- [ ] The new regression test fails on the pre-fix tree — evidence: in a `v0.135.0` worktree (`git worktree add /tmp/prefix v0.135.0`), copy the new spec added under AC1 into `result-deliverer_test.go` and run `go test ./delivery/ -run TestDelivery -count=1`; expect a Gomega failure containing the fragment `not to have key`. Record the output in the PR body.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit`
- `go test ./delivery/ -count=1`

### Operator-executable (host, after PR merge)

None in this repo — the deliverer is a library. The runtime proof is the deployed-cluster verification owned by vault task `A Phase Regression Written Behind the Deliverer Starts a Second Job` in `private-personal` (dev / prod / sun windows). This spec stays `verifying` until that task records the dev/prod observation — tests passing is not the bug fixed.

## Desired Behavior

1. A `Failed` result whose Job advanced no phase publishes `status: in_progress` and **no** `phase`.
2. A `Failed` result whose Job advanced to phase P publishes `phase: P`.
3. Every non-advancing branch (Done-without-NextPhase, needs_input, in_progress, unknown) keeps publishing no phase.
4. A regression test reproduces the 2026-10-30 shape: a `Failed` result over a Job-start `planning` snapshot publishes no `phase`, so a prior `human_review` park survives the merge.

## Constraints

- **The `Failed` branch must still publish the advanced phase.** Removing that would rewind an execution-phase task that failed, re-running a completed planning phase (the exact reason `advancedPhase` exists).
- **The five existing `Failed` specs are updated, not deleted** (see AC4). They currently assert the buggy contract; each keeps its remaining assertions and swaps only the `phase`-equality for the absence assertion.
- **Four source comments encode the buggy contract and must be updated in the same change** — the repo must not keep asserting the old rule in prose after the code changes:
  - `delivery/result-deliverer.go:184-187` — "the failed branch — the only branch that still publishes a phase … Empty means 'no advance yet', in which case the generated (Job-start) phase is the best available answer." After the fix, empty means *publish nothing*.
  - `result-deliverer.go:332-337` — "This branch deliberately keeps publishing its phase: the controller's planning-retry gate reads phase from the published payload, not from disk." The first half becomes false; the second half becomes false once the companion lands (the gate reads disk).
  - `result-deliverer.go:266-267` — "Only the `AgentStatusDone` branch may write phase" (already stale).
  - `result-deliverer_test.go:1166-1170` — the 5d comment "The failed branch still publishes its phase (the controller's planning-retry gate reads it from the payload)". The 5d *assertion* stays; the comment must change.
- **Companion change required in `bborbe/agent-task-controller` (separate spec/PR):** the planning-retry gate (`pkg/command/planning_retry.go` `matchesRetryGate`) reads `req.Frontmatter.Phase()` — the payload. With this change a failed planning run's payload carries no phase, so the gate would skip. The controller must read the **on-disk** phase (its `FindTaskFilePath` already fetches the frontmatter at `:87`). That change is backward-compatible (works whether the payload carries phase or not) and **must land and deploy before or with this one**; this change is not backward-compatible with a payload-reading gate.
- **Rejected alternative — do not guard `phase` in the controller's `MergeFrontmatter`** the way `status` is pinned (`result_writer.go`). A legitimate `Done` advance must write `phase`; a blanket on-disk-first guard would block it. The deliverer must omit the phase it does not own.
- Do not touch `MergeFrontmatter`'s phase handling here — it lives in the controller.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| A pod crashes before any phase write; a prior delivery parked the task at `human_review` | The `Failed` result publishes no phase; the park survives; no second Job starts | — |
| A Job advances `planning → execution` then fails | The `Failed` result publishes `execution`; the task stays at execution | — |
| The controller gate still reads the payload (companion not yet deployed) | The deliverer publishes no phase; the gate skips; a failed pr-review planning run is not retried controller-side | Deploy the controller change; `grep 'phase is not planning'` on the controller log shows the skip; the executor's trigger-cap retry remains a fallback |
| **Detection:** the Failed branch drops a non-empty Job-start phase | A `glog.V(2)` line records the dropped phase (`task %s: dropping stale Job-start phase %q on failed result`), so the regression is observable in the controller/executor log rather than only from vault git history two days later | — |

## Reproduction

**Environment:** `bborbe/agent` at `v0.135.0`; the deployed `pr-reviewer-agent` image carries it.

**Triggering incident:** task `7de48290` (vault `private-agent`, `tasks/PR Review github - bborbe-vault-cli - 341 …`). Job `pr-reviewer-agent-7de48290-20261030102802` started `10:28:03Z`; a delivery parked the task at `phase: human_review` at `10:39:49Z`; the Job body carries `## Failure — pod_crash_no_stdout` (10:33:20Z); commit `8b744afa1e` at `10:41:37Z` moved `phase: human_review → planning` (and `assignee: "" → pr-reviewer-agent`); a second Job started `10:45:33Z`.

**Minimal in-process reproduction:** construct the deliverer with `generator.GenerateReturns` returning frontmatter `status: in_progress, phase: planning, assignee: pr-reviewer-agent` (the Job-start snapshot); call `DeliverResult` with `Status: failed`; today the published `fm` carries `phase: planning`; expected: no `phase` key.

## Expected vs Actual

**Expected** (per `docs/task-flow-and-failure-semantics.md:33` — "Controller-side failure paths (`needs_input`, `failed`, cap-exhaustion) leave phase unchanged" — and per the other four non-advancing branches): a `Failed` result that advanced nothing publishes no phase, so the controller keeps the on-disk phase.

**Actual** (observed 2026-10-30): it publishes the Job-start `planning`, which the controller's incoming-wins merge applies over the `human_review` park, regressing the task and starting a second Job.

## Why this is a bug

`phase` is the resume cursor — the controller re-drives whatever reads it. A delivery that did not advance the phase must not own it; the other four non-advancing branches already encode that rule (`delete(frontmatter, "phase")` at `:283 / :314 / :323 / :350`), and `docs/task-flow-and-failure-semantics.md:33` states the contract this violates. The `Failed` branch is the sole exception, and it is the one that fires on a pod crash — the most common non-advancing outcome. The executor's spawn allowlist excludes `human_review` (`docs/task-flow-and-failure-semantics.md:59`), so a stale `planning` is exactly what turns the park into a second Job.
