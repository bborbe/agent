---
status: failed
spec: ["004"]
container: agent-017-spec-004-cleanup-prompt-layer
dark-factory-version: v0.69.0
created: "2026-03-29T13:00:00Z"
queued: "2026-03-29T14:35:59Z"
started: "2026-03-29T19:33:10Z"
completed: "2026-03-29T19:37:17Z"
branch: dark-factory/task-executor-service
---

<summary>
- Remove the entire prompt/ directory (prompt/controller and prompt/executor) since task/executor replaces both
- Remove all Prompt-related types from lib/ (agent_prompt*.go files)
- Remove PromptV1SchemaID from the CDB schema list in lib/
- Delete any tests in lib/ that import or reference Prompt types
- Update CHANGELOG with the removal
</summary>

<objective>
After this prompt executes, the prompt layer is fully removed from the codebase. No prompt/ directory, no Prompt types in lib/, no PromptV1 schema registration. The only Kafka schema remaining is TaskV1. `make precommit` passes in lib/.
</objective>

<context>
The prompt layer (prompt/controller + prompt/executor) has been replaced by task/executor. prompt/controller is deployed but will be undeployed separately from K8s (not part of this code change). prompt/executor was never deployed (skeleton only).

Prompt types in lib/ are no longer referenced by any service after task/executor replaces the pipeline.

Reference files:
- `lib/agent_cdb-schema.go` — contains CDBSchemaIDs list with PromptV1SchemaID to remove
- `prompt/` — entire directory to delete
- `lib/agent_prompt*.go` — all Prompt type files to delete
</context>

<requirements>
### 1. Delete `prompt/` directory

Remove the entire `prompt/` directory and all its contents:
- `prompt/controller/` (deployed service — code removal only, K8s undeployment is separate)
- `prompt/executor/` (skeleton, never deployed)

```bash
rm -rf prompt/
```

### 2. Delete Prompt types from lib/

Remove all files matching `lib/agent_prompt*.go`:
- `lib/agent_prompt.go`
- `lib/agent_prompt-identifier.go`
- `lib/agent_prompt-instruction.go`
- `lib/agent_prompt-message.go`
- `lib/agent_prompt-output.go`
- `lib/agent_prompt-parameters.go`
- `lib/agent_prompt-status.go`

If any additional `agent_prompt*.go` files exist, delete them too.

```bash
rm -f lib/agent_prompt*.go
```

### 3. Remove PromptV1SchemaID from `lib/agent_cdb-schema.go`

Edit `lib/agent_cdb-schema.go`: remove `PromptV1SchemaID` from the `CDBSchemaIDs` slice. Keep TaskV1SchemaID.

### 4. Remove any Prompt references and tests from lib/

Search lib/ for remaining references to Prompt types. If any exist in non-prompt files (e.g., imports, type aliases, test files), remove them.

```bash
grep -r "Prompt" lib/ --include="*.go"
```

This includes test files (`*_test.go`) that test Prompt types — delete those tests entirely. Fix any remaining references. If a file becomes empty after removal, delete it.

### 5. Run `go mod tidy` in lib/

```bash
cd lib && go mod tidy
```

### 6. Update `CHANGELOG.md`

Add to `## Unreleased` in the root `CHANGELOG.md`. If `## Unreleased` does not exist, create it before the first `## v` entry:

```
- Remove prompt layer (prompt/controller, prompt/executor, Prompt types from lib/) — replaced by task/executor
```

### 7. Verify

```bash
cd lib && make precommit
```

This must pass. If prompt/controller or prompt/executor had their own go.mod files referencing lib/, their removal should not affect lib/'s build.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify task/ directory — this prompt only removes prompt-related code
- Do NOT modify any K8s resources — undeploying prompt/controller from the cluster is a separate manual step
- Do NOT remove TaskV1 types or the agent-task-v1 schema — only Prompt types are removed
- If any non-prompt code in lib/ imports Prompt types, that's a bug — report it but do not modify non-lib code
</constraints>

<verification>
Verify prompt directory is gone:
```bash
test ! -d prompt/ && echo "PASS: prompt/ removed" || echo "FAIL: prompt/ still exists"
```

Verify no prompt type files remain:
```bash
ls lib/agent_prompt*.go 2>/dev/null && echo "FAIL: prompt files remain" || echo "PASS: prompt files removed"
```

Verify no PromptV1SchemaID in schema list:
```bash
grep -c "PromptV1" lib/agent_cdb-schema.go && echo "FAIL: PromptV1 still referenced" || echo "PASS: PromptV1 removed"
```

Verify lib builds:
```bash
cd lib && make precommit
```
</verification>
