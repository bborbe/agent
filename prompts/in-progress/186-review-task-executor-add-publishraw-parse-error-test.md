---
status: committing
summary: Added PublishRaw test covering base.ParseEvent error path - publishRaw now has 100% coverage
container: agent-exec-186-review-task-executor-add-publishraw-parse-error-test
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-26T07:02:49Z"
completed: "2026-05-26T07:09:05Z"
---

<summary>
- Adds test for publishRaw base.ParseEvent error path
- Verifies error is wrapped and returned when parse fails
</summary>

<objective>
The publishRaw method in result_publisher.go at line 151 has an untested error path when base.ParseEvent fails. If any command struct fails to serialize, the error path would break silently. After this change, the parse error path is tested.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/result_publisher.go (~line 146, publishRaw)
- task/executor/pkg/result_publisher_test.go
</context>

<requirements>
### 1. Read existing result_publisher_test.go

Understand how the tests mock the command sender and test publish methods.

### 2. Add publishRaw parse error test

Add a test where base.ParseEvent fails (e.g., by having the command struct serialize to invalid JSON that causes parse to fail). Verify the error is wrapped and returned.

### 3. Verify coverage

```bash
cd task/executor && go test -coverprofile=/tmp/cover.out ./pkg/result_publisher.go && go tool cover -func=/tmp/cover.out
```

Target: ≥80% coverage for publishRaw.
</requirements>

<constraints>
- Only change files in `task/executor/pkg/`
- Do NOT commit — dark-factory handles git
- Tests must use Ginkgo/Gomega with Counterfeiter mocks
- External test package
</constraints>

<verification>
cd task/executor && make precommit
</verification>
