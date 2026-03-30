---
status: created
spec: ["005"]
created: "2026-03-29T20:00:00Z"
branch: dark-factory/agent-result-capture
---

<summary>
- Spawned K8s Jobs gain a `TASK_ID` env var set to the task's unique identifier
- Agents can now read `TASK_ID` from the environment to reference the task when publishing results back to Kafka
- The env var name `TASK_ID` matches the contract documented in `docs/agent-job-interface.md`
- Existing env vars (`TASK_CONTENT`, `KAFKA_BROKERS`, `BRANCH`) are unchanged
- The spawner unit test is extended to assert `TASK_ID` is present with the correct value
</summary>

<objective>
Add the `TASK_ID` env var to every K8s Job spawned by `task/executor`. Agents need this value to reference the originating task when publishing their result to `agent-task-v1-request`. This is a one-line change in the spawner with a matching test assertion.
</objective>

<context>
Read CLAUDE.md for project conventions, and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `task/executor/pkg/spawner/job_spawner.go` — current env var list in `SpawnJob`
- `task/executor/pkg/spawner/job_spawner_test.go` — existing envMap assertions to understand test pattern
- `docs/agent-job-interface.md` — confirms env var name is `TASK_ID`
</context>

<requirements>
### 1. Add `TASK_ID` to spawned Job env vars

In `task/executor/pkg/spawner/job_spawner.go`, inside `SpawnJob`, add `TASK_ID` to the `Env` slice of the container spec.

Current env vars list:
```go
Env: []corev1.EnvVar{
    {Name: "TASK_CONTENT", Value: string(task.Content)},
    {Name: "KAFKA_BROKERS", Value: s.kafkaBrokers},
    {Name: "BRANCH", Value: s.branch},
},
```

Updated list (add `TASK_ID` as second entry, after `TASK_CONTENT`):
```go
Env: []corev1.EnvVar{
    {Name: "TASK_CONTENT", Value: string(task.Content)},
    {Name: "TASK_ID", Value: string(task.TaskIdentifier)},
    {Name: "KAFKA_BROKERS", Value: s.kafkaBrokers},
    {Name: "BRANCH", Value: s.branch},
},
```

### 2. Update the spawner test

In `task/executor/pkg/spawner/job_spawner_test.go`, in the "creates a job with correct name and env vars" test, add an assertion for `TASK_ID` alongside the existing `TASK_CONTENT`, `KAFKA_BROKERS`, `BRANCH` assertions:

```go
Expect(envMap["TASK_ID"]).To(Equal("abc12345-rest-ignored"))
```

Place it after the `TASK_CONTENT` assertion.

### 3. Update CHANGELOG.md

Append to `## Unreleased` in the root `CHANGELOG.md` (create the section before the first `## v` entry if it does not exist):

```
- feat: Pass TASK_ID env var to spawned K8s Jobs so agents can reference their originating task when publishing results
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT change any other env vars or job spec fields
- Do NOT modify `docs/agent-job-interface.md` — that is done in a later prompt
- The env var name must be exactly `TASK_ID` (uppercase, underscore) — this is the documented contract
- The value must be `string(task.TaskIdentifier)` — exact type conversion, no transformation
- Do NOT change the `task/controller` service — this prompt is task/executor only
- Existing tests must still pass
</constraints>

<verification>
```bash
cd task/executor && make test
```
Must exit 0 (all tests pass including the updated TASK_ID assertion).

```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
