---
status: completed
summary: Added context cancellation checks to EnsureInstalled loop in claude-plugin-installer.go and StepRunner.Run loop in agent_runner.go
container: agent-exec-153-review-lib-add-context-cancellation-checks
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T21:10:00Z"
completed: "2026-05-25T21:15:37Z"
---

<summary>
- Add non-blocking select with ctx.Done() check to outer plugin install loop
- Add non-blocking select with ctx.Done() check to StepRunner.Run outer loop
- Add context cancellation check before returning lastResult at end of StepRunner.Run
</summary>

<objective>
Add context cancellation checks to two long-running loops in lib/: the EnsureInstalled outer loop over specs in claude-plugin-installer.go, and the StepRunner.Run step loop in agent_runner.go. Both loops can run for extended periods (multiple subprocess calls or sequential step executions) and should respect context cancellation to allow graceful shutdown.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md` for the context cancellation pattern.

Files to read before making changes (read ALL first):
- lib/claude/claude-plugin-installer.go (~line 80, EnsureInstalled method)
- lib/agent_runner.go (~line 38, Run method, StepRunner struct)
- lib/agent_runner_test.go (if exists)
</context>

<requirements>
1. Fix `lib/claude/claude-plugin-installer.go` (~line 80-90) — outer loop over specs in `EnsureInstalled`:
   - Add non-blocking select with `ctx.Done()` check at top of each iteration
   - Pattern: `select { case <-ctx.Done(): return errors.Wrap(ctx, ctx.Err(), "context cancelled during EnsureInstalled"); default: }`
   - The err from `ensureOne` is already wrapped by `ensureOne` itself

2. Fix `lib/agent_runner.go` (~line 38-80) — `StepRunner.Run` step loop:
   - Add select with `ctx.Done()` check at top of each `for` loop iteration
   - At the final return (~line 80), add `ctx.Err()` check before returning `lastResult`
   - Pattern: `if ctx.Err() != nil { return nil, ctx.Err() }` before returning
</requirements>

<constraints>
- Only change files in `lib/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/` — error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`), context propagation, factory pattern, time injection
</constraints>

<verification>
cd lib && make precommit
</verification>
