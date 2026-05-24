---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Adds test coverage for PublishIncrementTriggerCount method
- Tests happy path: command sent correctly
- Tests error path: sender fails
- Tests edge case: empty task identifier
</summary>

<objective>
PublishIncrementTriggerCount in result_publisher.go has 0% test coverage. This method is called from spawnIfNeeded before every SpawnJob. If it fails, spawning is aborted. After this change, the method has comprehensive tests covering happy path, error path, and edge cases.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/result_publisher.go (~line 109, PublishIncrementTriggerCount)
- task/executor/pkg/result_publisher_test.go (existing test patterns)
- task/executor/pkg/handler/task_event_handler_test.go (mock usage patterns)
</context>

<requirements>
### 1. Read existing result_publisher_test.go

Understand the existing test patterns and mock setup for ResultPublisher.

### 2. Add PublishIncrementTriggerCount tests

Add test cases to result_publisher_test.go:

**Happy path test:**
- Create a mock ResultPublisher
- Call PublishIncrementTriggerCount with a valid task
- Verify the command was sent with correct fields

**Error path test:**
- Mock the underlying sender to return an error
- Call PublishIncrementTriggerCount
- Verify the error propagates

**Edge case: empty task identifier:**
- Verify behavior when task identifier is empty

### 3. Run make test to verify coverage

```bash
cd task/executor && go test -coverprofile=/tmp/cover.out ./pkg/result_publisher.go && go tool cover -func=/tmp/cover.out
```

Target: ≥80% coverage for PublishIncrementTriggerCount.
</requirements>

<constraints>
- Only change files in `task/executor/pkg/`
- Do NOT commit — dark-factory handles git
- Tests must use Ginkgo/Gomega with Counterfeiter mocks
- External test package (package_test suffix)
</constraints>

<verification>
cd task/executor && make precommit
</verification>
