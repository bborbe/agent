---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-25T12:48:35Z"
prompted: "2026-05-25T13:21:00Z"
branch: dark-factory/agent-lib-runs-all-phases-in-one-pod
---

## Summary

- Today `agentlib.Agent.Run` dispatches exactly one phase: it looks up the requested phase, runs its `StepRunner`, and returns. When a step publishes `Status: done + NextPhase: <next>`, the pod exits; the executor then spawns a fresh K8s Job for the next phase, gated by a 300s respawn grace window. A clean 3-phase pr-review (planning → execution → ai_review → done) therefore needs 3 pod boots + 2× 300s grace = ~15 min minimum wall-clock even on the happy path, verified live today on bborbe/maintainer#13.
- This spec changes `Agent.Run` to wrap the existing single-phase `StepRunner` call in a loop: after a phase publishes `Done + NextPhase` and that next phase exists on the same `Agent`, the loop looks it up and runs it in the same process. The pod only exits when the chain terminates (NextPhase empty, `done`, `human_review`, unknown to this agent, or status != Done).
- `StepRunner.Run` and `shouldExitStepRunner` stay byte-for-byte unchanged. The loop sits one level above the StepRunner, in `Agent.Run`. Each step still writes the task file + publishes the result before returning, so crash recovery (mid-loop pod kill → executor respawn → next pod resumes via `ShouldRun` guards on the persisted markdown) is preserved with no new code paths.
- A Ginkgo test in `lib/` builds a 3-phase Agent (A→B, B→C, C→done) and asserts one `Agent.Run` call produces three deliveries via a fake `ResultDeliverer`, in order, with the correct `NextPhase` values, and returns the final phase's result.
- Bumps `lib/v0.63.0` with one `feat:` CHANGELOG entry. The downstream `maintainer/agent/pr-reviewer/go.mod` bump + dev deploy verification is intentionally OUT of scope; see Non-goals.

## Problem

`agentlib.Agent.Run` is single-phase by construction: it accepts a `phaseName`, finds the matching `Phase` in `a.phases`, builds a `StepRunner` over that phase's steps, runs it, and returns. The control loop that decides "should the same process run the next phase too?" does not exist. It is implicitly delegated to the executor service, which restarts the pod from scratch — and the executor enforces a 300s `defaultRespawnGracePeriod` between phases to absorb genuine crash-recovery races.

Result: on the happy path, the grace window dominates wall-clock. Each pod also re-installs the claude plugin set (~7s), re-mints the GitHub app IAT (~300ms), and re-runs `gh auth setup-git`. For an agent with three phases, that overhead happens three times, and the operator waits 15 min for a clean PR review that did ~3 min of actual work. Reproduced today on bborbe/maintainer#13: planning ~50s + 5 min grace + execution ~7 min + 5 min grace + ai_review ~3 min ≈ 20 min total measured.

The grace window itself is correct — it absorbs the gap between "previous pod published Done + NextPhase" and "previous pod actually terminates / next Job actually starts" so the executor doesn't double-spawn. It should stay in place for the genuine respawn case (mid-phase crash). It should NOT fire when one pod could just keep going.

## Goal

After this change, when an `agentlib.Agent` configured with N phases is invoked on the happy path:

- One pod boot runs all phases in order, in the same OS process. The `Agent.Run` call returns only after the chain terminates (either a step publishes a terminal result — `Failed`, `NeedsInput`, `Done` without NextPhase advancement — or the publish hops to a NextPhase outside this agent's known phase set).
- Each per-step publish (write task file + deliver result via the deliverer) still happens before the loop advances to the next phase. The persisted markdown after step K is byte-identical to today's persisted markdown after step K on the same input. The Kafka publish sequence is byte-identical to today's per-phase pod sequence: same `AgentResultInfo` values, in the same order, just emitted from one process instead of N.
- A mid-loop pod kill (e.g. SIGKILL between phases) leaves the system in a state where the executor's existing respawn path takes over, and the next pod resumes at the correct phase via the existing `ShouldRun` guards on persisted markdown. No new recovery code paths.
- The `StepRunner.Run` function and `shouldExitStepRunner` helper are unchanged. The loop wraps the StepRunner call, it does not push into it.
- `lib/v0.63.0` is tagged with a `feat:` CHANGELOG entry naming the new loop behavior.

**Invariant established by this work:** Phase iteration on the happy path lives inside `Agent.Run`. The executor's per-phase respawn path is reserved for genuine crashes (pod died mid-phase) and for inter-agent hops (NextPhase belongs to a different agent than the one currently running).

## Non-goals

- NOT bumping `maintainer/agent/pr-reviewer/go.mod` to consume `lib/v0.63.0`. That is a downstream consumer change in a different repo; it will land as a follow-up spec after `lib/v0.63.0` is tagged.
- NOT deploying the change to dev/prod or running the live wall-clock measurement on a real PR. The Definition-of-Done "behavioral" criteria from the driving Obsidian task (one pod boot, < 5 min wall-clock) are deferred to the follow-up spec that lands the go.mod bump in the consumer repo and exercises the new code path against bborbe/quant.
- NOT touching `StepRunner.Run`, `shouldExitStepRunner`, or any per-step delivery code. The loop sits strictly above the StepRunner in `Agent.Run`.
- NOT removing or shortening `defaultRespawnGracePeriod` in `task/executor`. The grace window is still correct for the genuine respawn case (pod died mid-phase); this spec just stops triggering that path on healthy phase advancement.
- NOT refactoring `Phase`, `Step`, `Result`, `AgentResultInfo`, `AgentStatus`, or any deliverer type. Same surface shapes.
- NOT introducing a feature flag, env var, or per-agent opt-out that re-enables the old "exit after every phase" behavior. The loop is platform-wide and final; an escape hatch on the goal is itself a regression. If a future agent legitimately needs per-phase pod isolation, it would emit `NextPhase` outside its own `a.phases` set, which the loop already treats as a hop and exits — that is the supported variation.
- NOT adding cross-agent NextPhase resolution. If `Result.NextPhase` names a phase that is not in this Agent's `a.phases`, the loop exits (the executor handles the cross-agent hop the same way it does today).
- NOT changing the meaning of `AgentStatusInProgress` mid-phase saves (those continue to be handled inside the `StepRunner` and never reach the new outer loop).

## Desired Behavior

1. `Agent.Run` walks phases in a loop. The first iteration uses the `phaseName` argument; each subsequent iteration uses the previous iteration's published `NextPhase` to look up the next `Phase` in `a.phases`.

2. After each phase's `StepRunner.Run` returns, `Agent.Run` inspects the last delivered `Result`. The loop continues to a next iteration if and only if ALL of the following hold:
   - `Result.Status == AgentStatusDone`
   - `Result.NextPhase != ""`
   - `Result.NextPhase` is not the literal `"done"`
   - `Result.NextPhase` is not the literal `"human_review"`
   - `Result.NextPhase` resolves to a Phase present in `a.phases` (lookup by exact name match, same comparison as today's `findPhase`)
   - `ctx.Err() == nil`

   On any "no", the loop breaks and `Agent.Run` returns the last result and last error (same return shape as today).

3. Between iterations, `Agent.Run` checks `ctx.Done()`. If the context has been cancelled, the loop returns `ctx.Err()` wrapped via the same `errors.Wrapf` pattern used elsewhere in the file. No further phases are started.

4. The first iteration's behavior on unknown initial `phaseName` is unchanged: `Agent.Run` still routes through `unsupportedPhase` and publishes a Failed result via the deliverer, with no loop iterations. The new loop only activates after at least one phase has run successfully and published `Done + NextPhase`.

5. The markdown parsed at the top of `Agent.Run` (`ParseMarkdown(ctx, taskContent)`) is parsed exactly once and reused across loop iterations. Subsequent phases see the markdown as the previous phase's steps mutated it (this is already how `StepRunner.Run` operates within a phase; the loop extends that property across phases).

6. Each loop iteration constructs a fresh `StepRunner` for the next phase via `NewStepRunner(deliverer, p.Steps...)` — same constructor call shape as today. The deliverer instance is reused across iterations (passed once to `Agent.Run`).

7. `StepRunner.Run` is not modified. `shouldExitStepRunner` is not modified. The new loop logic lives entirely in `Agent.Run` (or a small unexported helper in the same file if `Agent.Run` would otherwise exceed reasonable function length — agent decides at impl time).

8. The docstring on `Agent.Run` is updated to describe the loop behavior, the exit conditions, and the contract change ("`Done + NextPhase != ""` no longer means exit pod — it means the Agent decides whether to advance or hand off"). A short comment block near the loop names the four exit conditions explicitly.

9. The CHANGELOG entry under `lib/v0.63.0` reads as one `feat:` line, naming the loop and the consequence (one pod boot per agent on the happy path), in the same style as existing `lib/`-scoped CHANGELOG lines (terse, no marketing).

## Constraints

- `lib/agent_runner.go` is not modified. Verified by `git diff lib/v0.62.29..HEAD -- lib/agent_runner.go` returning empty after the change.
- Public types `Agent`, `Phase`, `Step`, `Result`, `AgentStatus`, `AgentResultInfo`, `ResultDeliverer` keep their current field sets and method signatures. No additions, no removals, no field-tag changes.
- `Agent.Run`'s function signature is unchanged: `Run(ctx, phaseName, taskContent, deliverer) (*Result, error)`. Callers in `agent/claude`, `agent/code`, `agent/gemini`, `agent/pr-reviewer` (consumers) compile against the new version without source edits.
- The order and content of `deliverer.DeliverResult` calls for a 3-phase happy path with the new lib must equal the union of the per-phase call sequences observed under the old lib (i.e. if old behavior was: pod1 emits {A_step1, A_step2_done_nextB}; pod2 emits {B_step1_done_nextC}; pod3 emits {C_step1_done_nextDone} — new behavior emits the concatenation in a single process, in the same order).
- The `lib/` Go module's `precommit` Makefile target continues to pass. No new direct dependencies added to `lib/go.mod`.
- `defaultRespawnGracePeriod` in `task/executor/pkg/handler/task_event_handler.go` is not edited.

Domain reference: `docs/task-flow-and-failure-semantics.md` (phase taxonomy, NextPhase semantics, terminal-phase rules).

## Failure Modes

| Trigger | Expected behavior | Detection | Reversibility | Recovery |
|---------|-------------------|-----------|---------------|----------|
| Step publishes `Status: Done` with `NextPhase: ""` | Loop breaks after this iteration. `Agent.Run` returns the last result. | Test assertion: deliverer received N calls, loop exited after Nth. | n/a (normal terminal) | None needed. |
| Step publishes `Status: Done` with `NextPhase: "done"` | Loop breaks. Same as empty NextPhase. | Test assertion: NextPhase value preserved on the returned Result. | n/a | None. |
| Step publishes `Status: Done` with `NextPhase: "human_review"` | Loop breaks. Pod exits. Existing result-writer assignee-clear guard handles the human_review handoff downstream (unchanged). | Test assertion: deliverer call N has `NextPhase: "human_review"`; no call N+1. | n/a | None. |
| Step publishes `Status: Done` with `NextPhase: "<unknown-to-this-agent>"` | Loop breaks. Pod exits. Executor's existing inter-agent routing spawns a new Job for the receiving agent. | Test assertion: a 2-phase Agent given a `Done + NextPhase: "extra"` result on the second phase returns after that publish; no third iteration attempted. | n/a (matches today's cross-agent hop behavior) | None — same code path as today's cross-agent handoff. |
| Step publishes `Status: Failed` or `Status: NeedsInput` | Loop breaks (matches `shouldExitStepRunner`'s exit decision; the outer loop respects the StepRunner's return). Pod exits via the executor's standard failure flow. | Test assertion: deliverer received the Failed/NeedsInput delivery; no further phase started. | n/a | Executor retry path (unchanged from today). |
| `ctx.Done()` fires between iterations | Loop returns `ctx.Err()` (wrapped). Last publish for the just-completed phase already happened before the check (StepRunner already published it). | Test: cancel ctx after first phase's deliver, assert `Agent.Run` returns ctx.Err and the second phase's StepRunner was never invoked. | Reversible: executor respawn at the persisted phase. | Executor respawn picks up at the persisted phase via `ShouldRun` guards. |
| Pod SIGKILLed mid-phase (between step K's publish and step K+1's start, or mid-StepRunner) | Pod dies. Executor's existing respawn timer fires; next pod boots, parses the persisted markdown, `ShouldRun` guards skip completed steps and resume at the next outstanding step. | Operator observation: kubectl shows the pod terminated; a new pod boots after the grace window; persisted markdown shows the last completed step's output. | Reversible (same crash recovery as today). | No new code; existing executor respawn + `ShouldRun`-based resume. |
| Two phases in `a.phases` with the same name | Caught by `Agent.validate(ctx)` at the top of `Agent.Run` (unchanged). Returns the duplicate-phase error. | Test: existing duplicate-phase test still passes. | n/a (programmer error) | Fix the agent composition. |
| `Result.NextPhase` resolves to a Phase whose `Steps` slice is empty | Loop iteration runs StepRunner with zero steps; StepRunner returns `(nil, nil)`. Loop sees a nil last-result and breaks. `Agent.Run` returns the previous iteration's result. | Test: 3-phase chain where middle phase has zero steps — `Agent.Run` returns the first phase's result with no panic; deliverer received only the first phase's publishes. | n/a (programmer error, but loop must not panic) | Fix the agent composition; observable failure mode is "loop terminates early without explanation," surfaced by the empty-Steps test. |
| Deliverer publish fails between phases (step published, deliverer returned err) | StepRunner already returns the err from the inner `DeliverResult` call. The outer loop receives `(lastResult, err)` and returns immediately without starting the next phase. Same exit shape as today. | Test: deliverer mocked to fail on call 2 — assert `Agent.Run` returns the err; phase 2 of a 3-phase chain never starts. | Reversible via executor retry. | Executor retry (unchanged). |

## Security / Abuse Cases

Not applicable for this spec. The change is internal to `Agent.Run`'s control flow inside a process that already has the same trust boundary as today (executor-spawned Job pod running the agent binary). No new network I/O, file I/O, user input, or trust-boundary crossing is introduced. The publish-to-Kafka and write-task-file paths are unchanged; the loop only reorders when they run within a single process.

## Files Touched

- Modified: `lib/agent_agent.go` (phase loop lives here), `lib/CHANGELOG.md`, `lib/agent_agent_test.go` (new tests; create if absent).
- **NOT modified**: `lib/agent_runner.go` (StepRunner stays single-phase), `task/executor/**` (no executor change), `task/controller/**`, anything outside `lib/`.

## Acceptance Criteria

- [ ] `Agent.Run` in `lib/agent_agent.go` contains a loop that iterates phases on `Done + NextPhase` advancement. Evidence: `grep -nE 'for ' lib/agent_agent.go` returns at least one new for-loop inside `Run`; reading the surrounding 30 lines shows the four documented exit conditions in code (Status != Done, NextPhase == "" or `"done"` or `"human_review"`, NextPhase not in `a.phases`, ctx cancelled).
- [ ] `lib/agent_runner.go` is unchanged. Evidence: `git diff lib/v0.62.29 HEAD -- lib/agent_runner.go` exit 0 with empty output.
- [ ] `shouldExitStepRunner` is unchanged. Evidence: same `git diff` covers it (same file).
- [ ] `Agent.Run`'s function signature is unchanged. Evidence: `git diff lib/v0.62.29 HEAD -- lib/agent_agent.go | grep -E '^[-+]func \(a \*Agent\) Run'` shows no `-`/`+` pair that changes the signature line.
- [ ] A new Ginkgo test exists in `lib/` covering the 3-phase happy-path chain. Evidence: `grep -rn 'It("' lib/agent_agent_test.go` (or wherever the test lands) returns at least one `It` whose description names the multi-phase loop (e.g. "runs A then B then C in one call"). The test uses the existing `lib/mocks` fakes for `ResultDeliverer` and `Step`, builds three phases A→B, B→C, C→done, calls `Agent.Run(ctx, "A", "<minimal task body>", fakeDeliverer)`, and asserts: (a) `fakeDeliverer.DeliverResult` was invoked exactly 3 times (one per phase, assuming each phase has one step); (b) the `NextPhase` value on the three invocations was `"B"`, `"C"`, `"done"` in order; (c) the returned `Result.NextPhase == "done"`; (d) the returned error is nil.
- [ ] A test covers the ctx-cancel-between-phases case. Evidence: `grep -n 'ctx.*cancel\|Cancel(' lib/agent_agent_test.go` returns at least one match in a test that runs a 2+ phase Agent, cancels ctx after the first phase publishes, and asserts the second phase's step `Run` was never invoked (via the fake step's call counter) and `Agent.Run` returned a non-nil error whose `errors.Is` chain includes `context.Canceled`.
- [ ] A test covers the NextPhase-outside-this-agent case. Evidence: a test where phase B publishes `NextPhase: "unknown"` and the loop terminates after B; the deliverer's call count equals the per-phase publish count up to and including B; no third phase's step is invoked.
- [ ] `cd lib && make precommit` exits 0. Evidence: exit code.
- [ ] `lib/v0.63.0` tag is set on the commit that lands this change. Evidence: `git tag --list 'lib/v0.63.0'` returns `lib/v0.63.0`.
- [ ] `CHANGELOG.md` has a new top-level section `## v0.63.0` immediately above `## v0.62.29` containing a single `feat:` line that names the loop and the consequence ("one pod boot per agent on the happy path"). Evidence: both `grep -n '^## v0.63.0' CHANGELOG.md` and `grep -n '^## v0.62.29' CHANGELOG.md` return a line; the former's line number is strictly less than the latter's; `grep -A1 '^## v0.63.0' CHANGELOG.md` shows a line starting with `- feat`.

**Scenario coverage: NO new scenario.** The change is internal to `lib/` and fully exercised by unit + integration tests at the lib level. The live behavioral verification (one pod boot on real PR, < 5 min wall-clock) is explicitly deferred to the follow-up spec that bumps `pr-reviewer/go.mod` and deploys to dev — that spec, not this one, will own the scenario AC if one is needed at all.

## Verification

```
cd ~/Documents/workspaces/agent/lib
make precommit
git diff lib/v0.62.29 HEAD -- lib/agent_runner.go    # must be empty
git tag --list 'lib/v0.63.0'                          # must print lib/v0.63.0
grep -n '^## v0.63.0' CHANGELOG.md
```

Expected: `make precommit` exits 0. The `git diff` on `agent_runner.go` produces no output. The tag exists. The CHANGELOG line is present.

## Do-Nothing Option

If we don't do this: every multi-phase agent (pr-reviewer today, every future agent with a planning/execution/review shape) keeps paying 5 min × (N-1) grace windows on the happy path. For the 3-phase pr-reviewer that is 10 min of wall-clock added on top of ~3 min of actual work, every PR review. The bug-fix verification loop on this very repo runs against pr-reviewer-agent output, so the slowness compounds during incident response: every fix-and-retest cycle costs 15 min minimum before the operator knows whether the fix worked. Status quo is acceptable only if multi-phase agents are accepted as a rarely-used edge case, which contradicts the parent goal ([[Build Generic Claude Agent]]) that explicitly targets multi-phase agents as the default shape going forward. Reject do-nothing.
