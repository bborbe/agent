---
status: draft
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
---

<summary>
- Add agent-step_test.go covering NewAgentStep, Name, ShouldRun, Run
- Add missing claude-runner_test.go branches: buildCommand error path, appendTail branch
- Add task-runner_test.go branches: deliver error path, stepString branch
- Add expand-tilde_test.go for ~ prefix expansion
- Add result-deliverer_test.go for NewNoopResultDeliverer and NewKafkaResultDeliverer
</summary>

<objective>
Increase test coverage in lib/claude package from 76.6% to above 80%. Several functions are below the 80% threshold: buildCommand (61.9%), deliver (66.7%), stepString (50.0%), and NewAgentStep/agent-step.go (0%). After this change, all functions in lib/claude meet the 80% coverage requirement.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` for Ginkgo/Gomega patterns.

Files to read before making changes (read ALL first):
- lib/claude/agent-step.go
- lib/claude/claude-runner.go (~line 68 buildCommand, ~line 139 appendTail)
- lib/claude/task-runner.go (~line 103 deliver, ~line 169 stepString)
- lib/claude/expand-tilde.go
- lib/claude/result-deliverer.go (~line 43 NewNoopResultDeliverer, ~line 72 NewKafkaResultDeliverer)
- lib/claude/claude-runner_test.go (existing tests for reference)
- lib/claude/task-runner_test.go (existing tests for reference)
</context>

<requirements>
1. Create `lib/claude/agent-step_test.go`:
   - Test `NewAgentStep` with valid config
   - Test `Name` returns `"AgentStep"`
   - Test `ShouldRun`: returns `true` always
   - Test `Run`: success path, error path (`ClaudeRunner.Run` error)
   - Use Counterfeiter mock for `ClaudeRunner`

2. Expand `lib/claude/claude-runner_test.go`:
   - Test `buildCommand`: tool allowlist generation (branch at ~line 68)
   - Test `appendTail`: error case (branch at ~line 139)

3. Expand `lib/claude/task-runner_test.go`:
   - Test `deliver`: error path (file write failure)
   - Test `stepString`: empty/whitespace steps (branch at ~line 169)

4. Create `lib/claude/expand-tilde_test.go`:
   - Test `expandTilde`: `~` prefix (current 90%, missing `~` expansion)
   - Test `~/path` expansion
   - Test `~user/path` (should return unchanged)

5. Expand `lib/claude/result-deliverer_test.go` or create if needed:
   - Test `NewNoopResultDeliverer` (0% currently)
   - Test `NewKafkaResultDeliverer` (0% currently)

All tests: use Ginkgo/Gomega, Counterfeiter mocks (not manual mocks), external test package (`_test` suffix). Coverage target: ≥80% on new code; cover all branches for modified code.
</requirements>

<constraints>
- Only change files in `lib/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/` — error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`), context propagation, factory pattern, time injection
- Tests must use Ginkgo v2 / Gomega with Counterfeiter mocks
- External test packages (package name ending in _test)
</constraints>

<verification>
cd lib && make precommit
</verification>
