---
status: committing
spec: [040-agent-lib-runs-all-phases-in-one-pod]
summary: Added phase loop to Agent.Run with ctx-cancellation check between phases, created 9 Ginkgo tests covering all exit conditions, and added v0.63.0 CHANGELOG entry
container: agent-exec-150-spec-040-agent-multi-phase-loop
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-25T00:00:00Z"
queued: "2026-05-25T13:57:05Z"
started: "2026-05-25T13:57:20Z"
completed: "2026-05-25T14:00:40Z"
---

<summary>
- An agent configured with multiple phases now runs all of them inside one process on the happy path, instead of exiting after every phase and waiting for the executor to spawn a new pod.
- The per-phase publish sequence is unchanged: every step still writes the task file and publishes its result before the loop advances, so crash recovery via persisted markdown still works.
- The change is contained to `Agent.Run` — the inner `StepRunner` and `shouldExitStepRunner` are untouched.
- Exit conditions are explicit and total six: `result == nil`, non-Done status, empty NextPhase, NextPhase equal to `"done"` or `"human_review"`, NextPhase not in this Agent's phase set, plus the orthogonal ctx-cancelled check between iterations.
- A new Ginkgo test exercises a 3-phase chain end-to-end and asserts the deliverer received the three publishes in order; additional tests cover ctx-cancel-between-phases, unknown NextPhase, and an empty-Steps middle phase.
- The `CHANGELOG.md` gains a `## v0.63.0` section above `## v0.62.29` with a single `feat:` line naming the loop and the consequence ("one pod boot per agent on the happy path").
</summary>

<objective>
Change `lib/agent_agent.go`'s `Agent.Run` so it loops over phases after a `Done + NextPhase` publish, instead of returning after the first phase. The loop sits one level above the existing `StepRunner.Run` call; the StepRunner itself is unchanged. Add Ginkgo tests in `lib/agent_agent_test.go` (create the file if it does not yet exist), and add the corresponding `## v0.63.0` `feat:` entry to the repo-root `CHANGELOG.md`.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

**Files to read before implementing:**
- `lib/agent_agent.go` — the entire file; the `Agent.Run` method is what changes.
- `lib/agent_runner.go` — the `StepRunner.Run` and `shouldExitStepRunner` functions. DO NOT modify. Read so you understand what return shape the loop receives.
- `lib/agent_phase.go` — `Phase` struct and `NewPhase` constructor.
- `lib/agent_step.go` — `Step` interface and `Result` struct. Note `Result.NextPhase` is `string` (not `domain.TaskPhase`).
- `lib/agent_status.go` — `AgentStatus` constants, especially `AgentStatusDone`.
- `lib/agent_result-deliverer.go` — the `ResultDeliverer` interface.
- `lib/agent_markdown.go` — `ParseMarkdown` signature.
- `lib/mocks/agent-result-deliverer.go` — fake `AgentResultDeliverer` with `DeliverResultCallCount`, `DeliverResultArgsForCall(i)`, `DeliverResultStub`, `DeliverResultReturnsOnCall(i, err)`.
- `lib/mocks/agent-step.go` — fake `AgentStep` with `NameReturns`, `ShouldRunReturns`, `RunStub`, `RunCallCount`, `RunReturnsOnCall(i, *Result, err)`.
- `lib/agent_task_test.go` — example of the Ginkgo style used in this package (Describe / Context / It, `_ = Describe(...)`, package is `lib_test`).
- `lib/lib_suite_test.go` — confirms Ginkgo suite wiring is already in place; the new test file just adds more `Describe` blocks.
- `CHANGELOG.md` (repo root) — single CHANGELOG for the whole repo; entries are scoped via the `feat(lib): ...` / `fix(lib/delivery): ...` prefix convention. The top of file currently begins with `## v0.62.29`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md` — context cancellation idioms for loops.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `errors.Wrapf(ctx, err, "...")` style used throughout this codebase.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry style.

**Key facts verified from source**

`Agent.Run` current signature:
```go
func (a *Agent) Run(
    ctx context.Context,
    phaseName domain.TaskPhase,
    taskContent string,
    deliverer ResultDeliverer,
) (*Result, error)
```

`Result.NextPhase` is `string` (see `lib/agent_step.go`).

`Phase.Name` is `domain.TaskPhase` (see `lib/agent_phase.go`).

`findPhase` already exists and looks up by exact `domain.TaskPhase` match:
```go
func (a *Agent) findPhase(name domain.TaskPhase) (Phase, bool)
```

`StepRunner.Run` returns `(*Result, error)`. It returns `(nil, nil)` when no step actually executed (e.g. all `ShouldRun` guards returned false, or the phase has zero steps). The loop must handle a nil lastResult without panicking — when nil, the loop breaks and returns whatever the previous iteration produced.

`AgentStatusDone` is the constant `"done"` of type `AgentStatus`. The two terminal-NextPhase literals are the strings `"done"` and `"human_review"` (matching `domain.TaskPhase` enum string values).
</context>

<requirements>

1. **Modify `lib/agent_agent.go`'s `Agent.Run` to loop over phases.** Keep the function signature exactly:
   ```go
   func (a *Agent) Run(
       ctx context.Context,
       phaseName domain.TaskPhase,
       taskContent string,
       deliverer ResultDeliverer,
   ) (*Result, error)
   ```

   The new control flow:

   - `a.validate(ctx)` — keep at the very top, unchanged.
   - `a.findPhase(phaseName)` — keep as the first lookup. On miss, dispatch to `a.unsupportedPhase` exactly as today and return. The loop is NOT entered on the unsupported-phase path.
   - `ParseMarkdown(ctx, taskContent)` — keep, called exactly once before the loop. The parsed `*Markdown` is reused across iterations.
   - Introduce a `for` loop that starts with the resolved first phase already in hand and walks subsequent phases by inspecting the last delivered `*Result`.
   - Each iteration:
     1. Construct `runner := NewStepRunner(deliverer, p.Steps...)` — same constructor call shape as today.
     2. `result, err := runner.Run(ctx, md)`.
     3. If `err != nil`, return `(result, err)` immediately. Do not start the next iteration.
     4. Determine whether to continue (see step 2 below for the six conditions). If any condition says "stop", return `(result, nil)`.
     5. Otherwise, look up the next phase via `a.findPhase(domain.TaskPhase(result.NextPhase))`. If the lookup fails, return `(result, nil)`. (This is the "NextPhase outside this agent" exit; it intentionally collapses with the explicit `"done"` / `"human_review"` checks because those are not in `a.phases` for typical agents — but the explicit checks in step 2 still happen first so the diagnostics in the test/log message remain clear.)
     6. Update the iteration's current phase to the found phase and continue.

2. **Encode the six exit conditions explicitly and in this exact order.** After each `runner.Run` returns, the loop must break (and `Agent.Run` returns the last result + nil error) when ANY of:
   - `result == nil` — StepRunner produced no delivery this iteration (empty Steps or all ShouldRun=false). Break and return whatever lastResult was before this iteration (which may itself be nil on the very first iteration; that case is acceptable — same as today's behavior when the only phase has no steps that fire).
   - `result.Status != AgentStatusDone` — Failed, NeedsInput, or InProgress. StepRunner already handled the per-step publish; the loop just stops.
   - `result.NextPhase == ""` — terminal in-place save.
   - `result.NextPhase == "done"` — terminal literal.
   - `result.NextPhase == "human_review"` — escalation literal.
   - `ctx.Err() != nil` — context cancelled (see step 3). Wrap and return.

   Otherwise, attempt the `findPhase(domain.TaskPhase(result.NextPhase))` lookup. If it returns `ok == false`, also break and return `(result, nil)`.

   A short comment block immediately above the loop must name these six exit conditions in plain English so a future reader can verify against the test cases.

3. **Context cancellation check between iterations.** After a successful iteration's StepRunner.Run returns and BEFORE attempting the next phase lookup, check `ctx.Err()`. If non-nil, return:
   ```go
   return result, errors.Wrapf(ctx, ctx.Err(), "agent run cancelled between phases")
   ```
   This uses the same `errors.Wrapf(ctx, err, "...")` pattern already used in this file (see the existing `parse markdown` and `deliver unsupported-phase` wraps).

4. **Update the `Agent.Run` docstring** to describe the new loop behavior. The doc block on the method must:
   - State that `Run` walks phases sequentially in the same process on the happy path.
   - List the six exit conditions (`result == nil`, `Status != Done`, `NextPhase == ""`, `NextPhase == "done"`, `NextPhase == "human_review"`, `NextPhase not in a.phases`, `ctx cancelled`).
   - State the contract change in one sentence: `"Done + NextPhase != ""` no longer means "exit pod" — it means "the Agent decides whether to advance internally or hand off to the executor".`
   - Keep the existing notes about `phaseName` routing through `unsupportedPhase` on miss and about `taskContent` being parsed once.

5. **Do NOT modify `lib/agent_runner.go`.** Specifically: `StepRunner.Run` and `shouldExitStepRunner` are unchanged byte-for-byte. Verify with `git diff -- lib/agent_runner.go` (must produce no output relative to the start of this prompt's working tree).

6. **Do NOT add, remove, or rename any field on `Agent`, `Phase`, `Step`, `Result`, `AgentStatus`, `AgentResultInfo`, or `ResultDeliverer`.** No new methods on these types. The loop logic lives in `Agent.Run` (or, if and only if it would otherwise exceed reasonable readability, in a small unexported helper in `lib/agent_agent.go` like `func (a *Agent) shouldAdvance(result *Result) (Phase, bool)`). No new exported names in package `lib`.

7. **Create `lib/agent_agent_test.go` (or extend it if it already exists) with Ginkgo tests covering:**

   a. **Happy-path 3-phase chain.** Build a 3-phase Agent A→B, B→C, C→done. Each phase has one step (the `mocks.AgentStep` fake). Configure:
      - Step A: `NameReturns("step-a")`, `ShouldRunReturns(true, nil)`, `RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil)`.
      - Step B: `NameReturns("step-b")`, `ShouldRunReturns(true, nil)`, `RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "C"}, nil)`.
      - Step C: `NameReturns("step-c")`, `ShouldRunReturns(true, nil)`, `RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "done"}, nil)`.
      - Deliverer: `mocks.AgentResultDeliverer{}`, no stub (default: returns nil).

      Call `agent.Run(ctx, domain.TaskPhase("A"), "<minimal-markdown-task-body>", deliverer)`.

      Assert:
      - Returned error is `nil`.
      - Returned `*Result` is non-nil and `result.Status == lib.AgentStatusDone`.
      - Returned `result.NextPhase == "done"`.
      - `deliverer.DeliverResultCallCount() == 3`.
      - The three `DeliverResultArgsForCall(i)` invocations have `NextPhase` values `"B"`, `"C"`, `"done"` in that order.
      - `stepA.RunCallCount() == 1`, `stepB.RunCallCount() == 1`, `stepC.RunCallCount() == 1`.

      For the minimal-markdown body, use the smallest valid input accepted by `ParseMarkdown` — read `lib/agent_markdown.go` to confirm. A simple `"# Task\n"` or `"---\nphase: A\n---\n# Task\n"` works; pick whichever ParseMarkdown accepts cleanly. Do NOT invent frontmatter fields.

   b. **Ctx-cancel between phases.** Build a 2-phase Agent A→B. Step A's `RunStub` (note: use `RunStub`, not `RunReturns`, so the cancel happens during/after step A): inside the stub, set the result to `Done + NextPhase: "B"` AND call `cancel()` on a cancellable context created via `context.WithCancel(context.Background())`. Step B uses default mock values (so if it ever runs, the test can detect it via `RunCallCount > 0`).

      **This test asserts the between-iterations ctx check** added in requirement 3 (the new code paid for by this prompt) — NOT cancellation observed inside StepRunner during step A's Run. Step A's Run completes normally (its stub doesn't return ctx.Err); the loop then checks ctx.Err() before looking up phase B and bails out there.

      Assert:
      - `Agent.Run` returns a non-nil error.
      - `errors.Is(err, context.Canceled)` returns `true`.
      - `stepB.RunCallCount() == 0` (the second phase never started — verifies the between-iterations check ran).
      - `deliverer.DeliverResultCallCount() == 1` (only step A's publish happened).

   c. **NextPhase outside this agent.** Build a 2-phase Agent A→B. Step B publishes `Done + NextPhase: "unknown-to-this-agent"`.

      Assert:
      - Returned error is `nil`.
      - Returned `result.NextPhase == "unknown-to-this-agent"`.
      - `deliverer.DeliverResultCallCount() == 2` (one for A, one for B; no third).
      - There is no step C invocation (only A and B are in the agent).

   d. **NextPhase == "human_review" literal.** Build a 2-phase Agent A→B. Step B publishes `Done + NextPhase: "human_review"`.

      Assert:
      - Returned error is `nil`.
      - `result.NextPhase == "human_review"`.
      - `deliverer.DeliverResultCallCount() == 2`.

   e. **Empty-Steps middle phase (must not panic).** Build a 3-phase Agent A→B, B has zero steps, C is never reached. Step A publishes `Done + NextPhase: "B"`. Phase B is constructed with `lib.NewPhase(domain.TaskPhase("B"))` (no steps). Phase C has one step that would fail the test if invoked.

      Assert:
      - The call does NOT panic.
      - Returned error is `nil`.
      - `deliverer.DeliverResultCallCount() == 1` (only A published; B's empty StepRunner returned `(nil, nil)`).
      - `stepC.RunCallCount() == 0`.

   f. **Deliverer publish failure stops the loop.** Build a 3-phase Agent A→B→C. Configure `deliverer.DeliverResultReturnsOnCall(1, errors.New("kafka down"))` (second call fails). Step A publishes `Done + NextPhase: "B"`, step B publishes `Done + NextPhase: "C"`. The error happens during step B's publish (the StepRunner returns the error up).

      Assert:
      - Returned error is non-nil.
      - `stepC.RunCallCount() == 0`.
      - `deliverer.DeliverResultCallCount() == 2` (A succeeded, B's call returned the error).

   g. **Failed status stops the loop.** Build a 2-phase Agent A→B. Step A publishes `Status: Failed, NextPhase: ""`.

      Assert:
      - Returned error is `nil`.
      - `result.Status == lib.AgentStatusFailed`.
      - `stepB.RunCallCount() == 0`.

   h. **NeedsInput status stops the loop.** Same as (g) but with `AgentStatusNeedsInput`.

      Assert:
      - `result.Status == lib.AgentStatusNeedsInput`.
      - `stepB.RunCallCount() == 0`.

   i. **Unsupported initial phase still routes through `unsupportedPhase`.** Build a 2-phase Agent A→B and call `agent.Run(ctx, domain.TaskPhase("Z"), ..., deliverer)`.

      Assert:
      - Returned `result.Status == lib.AgentStatusFailed`.
      - `deliverer.DeliverResultCallCount() == 1` (the unsupported-phase publish; loop never started).
      - `stepA.RunCallCount() == 0`, `stepB.RunCallCount() == 0`.

   All tests live in package `lib_test` (matching the existing `agent_task_test.go` style). Import mocks via:
   ```go
   import "github.com/bborbe/agent/lib/mocks"
   ```
   And reference fakes as `&mocks.AgentResultDeliverer{}`, `&mocks.AgentStep{}`.

8. **Add the `## v0.63.0` section to the repo-root `CHANGELOG.md`** immediately above the current `## v0.62.29` line. Format:
   ```markdown
   ## v0.63.0

   - feat(lib): `agentlib.Agent.Run` now loops over phases in one process — when a step publishes `Done + NextPhase` and that phase exists on the same Agent, the loop runs it in-process instead of returning. The pod only exits on terminal status, terminal NextPhase (`"done"`/`"human_review"`/empty/unknown-to-this-agent), or ctx cancel. Consequence: one pod boot per agent on the happy path; the executor's 300s respawn grace window now only fires on genuine crashes and cross-agent hops.
   ```

   Use a single `feat(lib): ...` bullet. Do NOT use `feat:` (unscoped) — the existing CHANGELOG style is `feat(scope): ...` / `fix(scope): ...`.

   Anchor the insertion by the regex `^## v[0-9]+\.[0-9]+\.[0-9]+` (first match). Do NOT hard-code `## v0.62.29` as the anchor — the auto-committing repo means the version below may have drifted by the time this prompt runs. If a `## Unreleased` section already exists above the first version header, insert `## v0.63.0` after `## Unreleased`'s content but above the first `## v...` line. (If the auditor sees `## Unreleased` already populated with the v0.63.0 feat line, that is acceptable — surface it as an open question rather than duplicating.)

9. **Run `cd lib && make precommit`** from the repo root. It must exit 0. If `go generate` regenerates mocks (the `generate` target in `lib/Makefile` does `rm -rf mocks && go generate`), the regenerated `lib/mocks/agent-step.go` and `lib/mocks/agent-result-deliverer.go` files are expected — commit them with the rest.

10. **Verify the `lib/agent_runner.go` is byte-identical** to its state at the start of this prompt:
    ```bash
    git diff -- lib/agent_runner.go
    ```
    Must produce no output.

</requirements>

<constraints>
- `lib/agent_runner.go` MUST NOT be modified. `StepRunner.Run` and `shouldExitStepRunner` stay byte-for-byte unchanged.
- `Agent.Run`'s function signature is unchanged: `Run(ctx, phaseName, taskContent, deliverer) (*Result, error)`.
- Public types `Agent`, `Phase`, `Step`, `Result`, `AgentStatus`, `AgentResultInfo`, `ResultDeliverer` keep their current field sets and method signatures. No additions, no removals.
- No new direct dependencies added to `lib/go.mod`.
- The Kafka publish sequence for a 3-phase happy path must be byte-identical to today's per-phase pod sequence (same `AgentResultInfo` values, same order).
- `defaultRespawnGracePeriod` in `task/executor/pkg/handler/task_event_handler.go` is NOT edited.
- No feature flag, env var, or per-agent opt-out is added. The loop is platform-wide and final.
- No cross-agent NextPhase resolution. If `Result.NextPhase` names a phase not in this Agent's `a.phases`, the loop exits and lets the executor handle the cross-agent hop (same as today).
- Do NOT commit — dark-factory handles git. Do NOT tag — the auto-tagger handles `lib/v0.63.0`.
- Existing tests must still pass — specifically `lib/agent_task_test.go` and any other Ginkgo file under `lib/`.
- Use `errors.Wrapf(ctx, err, "...")` from `github.com/bborbe/errors` for any new error wrapping. Do NOT use `fmt.Errorf` or stdlib `errors.New` for wrapping.
- Tests live in `package lib_test`, not `package lib`. Use the existing mock fakes (`lib/mocks/`) — do NOT hand-roll new fakes.

</constraints>

<verification>
```bash
# AC1: precommit passes
cd lib && make precommit
# Expected: exit 0

# AC2: agent_runner.go untouched
git -C .. diff -- lib/agent_runner.go
# Expected: empty output

# AC3: Agent.Run signature unchanged (no signature-line edit)
git -C .. diff -- lib/agent_agent.go | grep -E '^[-+]func \(a \*Agent\) Run'
# Expected: no -/+ pair that changes the signature line; either both absent
# (signature line is unchanged in the diff context) or both present and
# byte-identical.

# AC4: loop exists in agent_agent.go
grep -nE '^\s*for ' lib/agent_agent.go
# Expected: at least one match inside the Run method body

# AC5: CHANGELOG entry present
grep -n '^## v0.63.0' CHANGELOG.md
# Expected: 1 match

grep -n '^## v0.62' CHANGELOG.md | head -1
# Expected: line number strictly greater than the v0.63.0 line number

awk '/^## v0.63.0/{flag=1; next} /^## v/{flag=0} flag' CHANGELOG.md | grep -E '^- feat\(lib\):'
# Expected: 1 match

# AC6: Ginkgo test file has the multi-phase It
grep -nE 'It\("' lib/agent_agent_test.go
# Expected: at least one It whose description names the multi-phase loop
# (e.g. "runs A then B then C in one call", "loops phases on Done+NextPhase",
# "advances to the next phase in the same process")

# AC7: Ginkgo test covers ctx-cancel-between-phases
grep -nE 'context\.WithCancel|cancel\(\)' lib/agent_agent_test.go
# Expected: at least one match in a test that runs a 2+ phase Agent and
# asserts step B's RunCallCount == 0 and errors.Is(err, context.Canceled)

# AC8: Ginkgo test covers unknown NextPhase (cross-agent hop)
grep -nE '"unknown' lib/agent_agent_test.go
# Expected: at least one match (the test's NextPhase literal)

# AC9: test file compiles and runs
cd lib && go test -mod=mod -run . -v ./... 2>&1 | tail -40
# Expected: all tests pass; no panics on the empty-Steps middle phase test

# AC10: lib module has no new direct dependencies
git -C .. diff -- lib/go.mod
# Expected: empty output, OR only mock-generation-related noise (no new
# `require (...)` entries pointing to packages not already present)
```
</verification>
