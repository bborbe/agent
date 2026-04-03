---
status: completed
spec: [005-agent-result-capture]
summary: Added TASK_ID env var to K8s Job container spec in job_spawner.go, added matching test assertion, and updated CHANGELOG.md with feat entry.
container: agent-018-spec-005-task-executor-task-id
dark-factory-version: v0.69.0
created: "2026-03-29T20:15:00Z"
queued: "2026-03-30T11:59:37Z"
started: "2026-03-30T11:59:38Z"
completed: "2026-03-30T12:09:18Z"
branch: dark-factory/agent-result-capture
---

<summary>
- Pass TASK_ID env var to spawned K8s Jobs so agents know which task they're working on
- Agents use TASK_ID when publishing their result back to Kafka
- One-line change in spawner + matching test assertion
</summary>

<objective>
Add `TASK_ID` env var to every K8s Job spawned by `task/executor/pkg/spawner/job_spawner.go`. The value is `string(task.TaskIdentifier)`. This closes the loop from spec-005: agents need their task identifier to publish results back via Kafka so task/controller can write the result to the vault.
</objective>

<context>
Read CLAUDE.md for project conventions and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `task/executor/pkg/spawner/job_spawner.go` — the file to modify; SpawnJob builds the corev1.EnvVar slice
- `task/executor/pkg/spawner/job_spawner_test.go` — tests to update; currently verifies TASK_CONTENT, KAFKA_BROKERS, BRANCH in envMap
- `docs/agent-job-interface.md` — confirms TASK_ID is already documented as a required env var for Pattern B Jobs
</context>

<requirements>
### 1. Update `task/executor/pkg/spawner/job_spawner.go`

In the `SpawnJob` method, add `TASK_ID` to the `Env` slice of the container spec, immediately after `TASK_CONTENT`:

```go
Env: []corev1.EnvVar{
    {Name: "TASK_CONTENT", Value: string(task.Content)},
    {Name: "TASK_ID", Value: string(task.TaskIdentifier)},
    {Name: "KAFKA_BROKERS", Value: s.kafkaBrokers},
    {Name: "BRANCH", Value: s.branch},
},
```

No other changes to the spawner logic.

### 2. Update `task/executor/pkg/spawner/job_spawner_test.go`

In the existing `It("creates a job with correct name and env vars", ...)` test case, add an assertion that `TASK_ID` is present and correct:

```go
Expect(envMap["TASK_ID"]).To(Equal("abc12345-rest-ignored"))
```

Place this assertion alongside the existing `TASK_CONTENT`, `KAFKA_BROKERS`, and `BRANCH` assertions.

### 3. Update `CHANGELOG.md`

Add or append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Pass TASK_ID env var to K8s Jobs spawned by task/executor so agents can reference their task on result publish
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT change the spawner constructor signature, interface, or any other method
- Do NOT modify any other package — only `pkg/spawner/job_spawner.go` and its test
- The env var name must be exactly `TASK_ID` — matches `docs/agent-job-interface.md`
- The value must be `string(task.TaskIdentifier)` — the full untruncated task identifier (not the job name prefix)
- Existing env vars (TASK_CONTENT, KAFKA_BROKERS, BRANCH) must remain in their current order; TASK_ID is inserted after TASK_CONTENT
- The `agent-task-v1-event` topic and existing task/controller git-to-Kafka sync behavior must not be changed
- task/controller remains the single git writer
</constraints>

<verification>
```bash
cd task/executor && make test
```
Must exit 0 — all spawner tests pass.

```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
