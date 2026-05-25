---
status: draft
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
---

<summary>
- Adds error path tests for RunDeferredRespawnLoop
- Tests error from evalDeferredRespawns during initial evaluation
- Tests error from evalDeferredRespawns during ticker tick
- Ensures errors propagate correctly to caller
</summary>

<objective>
RunDeferredRespawnLoop at task_event_handler.go:558 has 70% coverage. The error path from evalDeferredRespawns is untested. If evalDeferredRespawns returns an error during the initial reconciliation pass or during a ticker tick, it should propagate correctly. After this change, the error path is tested.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/handler/task_event_handler.go (~line 558, RunDeferredRespawnLoop)
- task/executor/pkg/handler/task_event_handler_test.go (existing test patterns)
</context>

<requirements>
### 1. Read existing task_event_handler_test.go

Understand the existing test patterns for RunDeferredRespawnLoop tests. Look for tests around line 1264 that test startup seed behavior.

### 2. Add error path tests

Add test cases to task_event_handler_test.go:

**Initial eval error:**
- Mock the store to return tasks
- Mock evalDeferredRespawns to return an error on first call
- Call RunDeferredRespawnLoop
- Verify the error propagates

**Ticker tick error:**
- Setup with tasks in store
- First evalDeferredRespawns succeeds
- Second evalDeferredRespawns returns an error
- Call RunDeferredRespawnLoop with a short timeout context
- Verify the error propagates

### 3. Verify coverage

```bash
cd task/executor && go test -coverprofile=/tmp/cover.out ./pkg/handler/... && go tool cover -func=/tmp/cover.out
```

Target: ≥80% coverage for RunDeferredRespawnLoop.
</requirements>

<constraints>
- Only change files in `task/executor/pkg/handler/`
- Do NOT commit — dark-factory handles git
- Tests must use Ginkgo/Gomega with Counterfeiter mocks
- External test package (package_test suffix)
</constraints>

<verification>
cd task/executor && make precommit
</verification>
