---
status: draft
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
---

<summary>
- Adds missing test for checkActiveCurrentJob parse error path
- Adds missing test for spawnIfNeeded spawn notification failure path
- Adds missing test for IsJobActive K8s list error path
- Adds missing test for applyTaskIDLabel nil map branch
</summary>

<objective>
Multiple error paths and edge cases in task/executor are untested: checkActiveCurrentJob graceful parse error (treating malformed job_started_at as elapsed), spawnIfNeeded spawn notification failure (best-effort, logs warning), IsJobActive K8s list error path, and applyTaskIDLabel nil map initialization. After this change, all these paths have test coverage.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/handler/task_event_handler.go (~line 330, checkActiveCurrentJob; ~line 449, spawnIfNeeded)
- task/executor/pkg/spawner/job_spawner.go (~line 160, IsJobActive; ~line 270, applyTaskIDLabel)
- task/executor/pkg/handler/task_event_handler_test.go
- task/executor/pkg/spawner/job_spawner_test.go
</context>

<requirements>
### 1. Add checkActiveCurrentJob parse error test

In task_event_handler_test.go, add a test where job_started_at is malformed (non-parseable). Verify the code treats it as elapsed (grace period bypassed) and proceeds to spawn.

### 2. Add spawnIfNeeded spawn notification failure test

In task_event_handler_test.go, add a test where PublishSpawnNotification fails but SpawnJob succeeds. Verify the handler logs a warning but does not fail the overall operation.

### 3. Add IsJobActive K8s list error test

In job_spawner_test.go, add a test where the K8s List call fails. Use a reactor to inject the error. Verify IsJobActive returns (false, error).

### 4. Add applyTaskIDLabel nil map test

In job_spawner_test.go, add a test where job.Labels is nil when applyTaskIDLabel is called. Verify the code creates a new map and adds the label.

### 5. Verify coverage

```bash
cd task/executor && go test -coverprofile=/tmp/cover.out ./pkg/handler/... ./pkg/spawner/... && go tool cover -func=/tmp/cover.out
```

Target: ≥80% coverage for affected functions.
</requirements>

<constraints>
- Only change files in `task/executor/pkg/`
- Do NOT commit — dark-factory handles git
- Tests must use Ginkgo/Gomega with Counterfeiter mocks
- External test packages
</constraints>

<verification>
cd task/executor && make precommit
</verification>
