---
status: approved
spec: [020-agent-lib-command-package-restructure]
created: "2026-05-07T18:00:00Z"
queued: "2026-05-07T18:17:58Z"
branch: dark-factory/agent-lib-command-package-restructure
---

<summary>
- All internal callers of the old flat `lib.*Command` types are migrated to the new `task.*Command` types from `github.com/bborbe/agent/lib/command/task`
- Three controller executors (`task_create_task_executor.go`, `task_update_frontmatter_executor.go`, `task_increment_frontmatter_executor.go`) and their test files import the new package and use renamed types
- The executor's `result_publisher.go` and its test file import the new package and use `task.UpdateFrontmatterCommand`, `task.IncrementFrontmatterCommand`, `task.BodySection`
- The flat `lib/agent_task-commands.go` (command structs + operation constants) is deleted
- The spec-019 files `lib/agent_create-task-command.go`, `lib/agent_create-task-command-sender.go`, `lib/agent_create-task-command-sender_test.go`, `lib/agent_task-commands_test.go`, and `lib/mocks/lib-create-task-command-sender.go` are deleted
- Operation constant strings are wire-identical after the migration — the controller decodes existing Kafka events identically
- `make precommit` exits 0 in `lib/`, `task/controller/`, and `task/executor/`
</summary>

<objective>
Complete the spec-020 restructure by migrating every caller of the old flat `lib.*Command` types to the new `lib/command/task` sub-package, then retiring the superseded files in `lib/`. After this prompt the flat `agent_task-commands.go` and all 019-introduced sibling files no longer exist; the codebase compiles and all tests pass across three module boundaries.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, Counterfeiter, external test packages

**Prerequisite: prompt 1 of spec-020 has shipped.** Verify before editing:
```bash
ls lib/command/task/create-command.go \
   lib/command/task/update-frontmatter-command.go \
   lib/command/task/increment-frontmatter-command.go \
   lib/command/task/mocks/task-create-command-sender.go \
   lib/command/task/mocks/task-update-frontmatter-command-sender.go \
   lib/command/task/mocks/task-increment-frontmatter-command-sender.go
```
If any file is missing, STOP and report `status: failed` with message "lib/command/task package not yet deployed (prompt 1 of spec-020)".

**Key files to read in full before editing:**

- `lib/agent_task-commands.go` — the file to be deleted. Read it to understand what `lib.CreateTaskCommand`, `lib.UpdateFrontmatterCommand`, `lib.IncrementFrontmatterCommand`, `lib.BodySection`, and operation constants look like so you can find and replace all references.
- `lib/agent_task-commands_test.go` — the test file to be deleted. Read it to verify no test case belongs in any surviving file.
- `lib/agent_create-task-command.go` — file to be deleted.
- `lib/agent_create-task-command-sender.go` — file to be deleted.
- `lib/agent_create-task-command-sender_test.go` — file to be deleted.
- `lib/mocks/lib-create-task-command-sender.go` — file to be deleted.
- `task/controller/pkg/command/task_create_task_executor.go` — uses `lib.CreateTaskCommand`, `lib.CreateTaskCommandOperation`. Full read required.
- `task/controller/pkg/command/task_create_task_executor_test.go` — uses `lib.CreateTaskCommand`, `lib.TaskFrontmatter`. Full read required.
- `task/controller/pkg/command/task_update_frontmatter_executor.go` — uses `lib.UpdateFrontmatterCommand`, `lib.UpdateFrontmatterCommandOperation`, `lib.BodySection`. Full read required.
- `task/controller/pkg/command/task_update_frontmatter_executor_test.go` — uses `lib.UpdateFrontmatterCommand`, `lib.BodySection`. Full read required.
- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — uses `lib.IncrementFrontmatterCommand`, `lib.IncrementFrontmatterCommandOperation`. Full read required.
- `task/controller/pkg/command/task_increment_frontmatter_executor_test.go` — uses `lib.IncrementFrontmatterCommand`. Full read required.
- `task/controller/pkg/command/task_frontmatter_sequence_test.go` — uses BOTH `lib.IncrementFrontmatterCommand` AND `lib.UpdateFrontmatterCommand`. Full read required.
- `task/controller/pkg/command/command_operations_test.go` — passive consumer; references local re-decl constants (`command.IncrementFrontmatterCommandOperation`, `command.UpdateFrontmatterCommandOperation`). Survives unchanged because the local re-decls remain — the RHS values switch from `lib.*` to `task.*`. Read to confirm.
- `task/executor/pkg/result_publisher.go` — uses `lib.UpdateFrontmatterCommand`, `lib.UpdateFrontmatterCommandOperation`, `lib.IncrementFrontmatterCommand`, `lib.IncrementFrontmatterCommandOperation`, `lib.BodySection`. Full read required.
- `task/executor/pkg/result_publisher_test.go` — uses `lib.UpdateFrontmatterCommand` and `lib.UpdateFrontmatterCommandOperation` (does NOT reference Increment or BodySection). Full read required.

Run before editing to find ALL references to the types being migrated:
```bash
grep -rn "lib\.CreateTask\|lib\.UpdateFrontmatter\|lib\.IncrementFrontmatter\|lib\.BodySection\|CreateTaskCommandOperation\|UpdateFrontmatterCommandOperation\|IncrementFrontmatterCommandOperation" \
    task/controller/ task/executor/ lib/ \
    --include="*.go"
```
Every match must be addressed (migrated or deleted) before declaring complete.
</context>

<requirements>

1. **Migrate `task/controller/pkg/command/task_create_task_executor.go`**

   Change:
   - Import `lib "github.com/bborbe/agent/lib"` stays (used for `lib.TaskIdentifier`, `lib.TaskFrontmatter`, etc.)
   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace `var cmd lib.CreateTaskCommand` → `var cmd task.CreateCommand`
   - Replace `lib.CreateTaskCommandOperation` → `task.CreateCommandOperation` (in `CommandObjectExecutorTxFunc` call AND in the local constant if any)
   - Replace `lib.CreateTaskCommand{...}` constructors in test helpers → `task.CreateCommand{...}` (in the production file — check if there are helper functions in this file that construct command objects)

   The `resolveCreateTaskPath` helper uses `cmd lib.CreateTaskCommand` — update to `cmd task.CreateCommand`. Read the full file to find every occurrence.

2. **Migrate `task/controller/pkg/command/task_create_task_executor_test.go`**

   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace every `lib.CreateTaskCommand{...}` → `task.CreateCommand{...}`
   - Keep `lib "github.com/bborbe/agent/lib"` if `lib.TaskFrontmatter`, `lib.TaskIdentifier`, etc. are still used
   - Read the full test file to find every occurrence

3. **Migrate `task/controller/pkg/command/task_update_frontmatter_executor.go`**

   Change:
   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace `lib.UpdateFrontmatterCommandOperation` → `task.UpdateFrontmatterCommandOperation` (the local constant `UpdateFrontmatterCommandOperation` redeclares the value from lib — update its RHS)
   - Replace `var cmd lib.UpdateFrontmatterCommand` → `var cmd task.UpdateFrontmatterCommand`
   - Replace `lib.BodySection` → `task.BodySection` in `buildUpdateModifyFn` function signature and body
   - Keep `lib "github.com/bborbe/agent/lib"` for `lib.TaskFrontmatter`, `lib.TaskIdentifier` still used

4. **Migrate `task/controller/pkg/command/task_update_frontmatter_executor_test.go`**

   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace `lib.UpdateFrontmatterCommand{...}` → `task.UpdateFrontmatterCommand{...}`
   - Replace `lib.BodySection{...}` → `task.BodySection{...}`
   - Keep `lib "github.com/bborbe/agent/lib"` if other lib types are still used

5. **Migrate `task/controller/pkg/command/task_increment_frontmatter_executor.go`**

   Change:
   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace `lib.IncrementFrontmatterCommandOperation` → `task.IncrementFrontmatterCommandOperation` (the local constant `IncrementFrontmatterCommandOperation` redeclares the value from lib — update its RHS)
   - Replace `var cmd lib.IncrementFrontmatterCommand` → `var cmd task.IncrementFrontmatterCommand`
   - Replace `lib.IncrementFrontmatterCommand` in `buildIncrementModifyFn` signature → `task.IncrementFrontmatterCommand`
   - Keep `lib "github.com/bborbe/agent/lib"` for `lib.TaskFrontmatter`, etc.

6. **Migrate `task/controller/pkg/command/task_increment_frontmatter_executor_test.go`**

   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace `lib.IncrementFrontmatterCommand{...}` → `task.IncrementFrontmatterCommand{...}`

6a. **Migrate `task/controller/pkg/command/task_frontmatter_sequence_test.go`** (cross-command sequence test)

   This file was missed in earlier listings. It exercises sequenced increment-then-update flows and references BOTH command types.

   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace every `lib.IncrementFrontmatterCommand{...}` → `task.IncrementFrontmatterCommand{...}`
   - Replace every `lib.UpdateFrontmatterCommand{...}` → `task.UpdateFrontmatterCommand{...}`
   - Replace `lib.BodySection{...}` → `task.BodySection{...}` if present
   - The `command.IncrementFrontmatterCommandOperation` / `command.UpdateFrontmatterCommandOperation` references inside `command_operations_test.go` (sibling file) survive unchanged because the local re-decls in the executors remain, but their RHS now points at `task.*` (handled in steps 3 and 5).
   - Keep `lib "github.com/bborbe/agent/lib"` for `lib.TaskFrontmatter`, `lib.TaskIdentifier`, etc.

7. **Migrate `task/executor/pkg/result_publisher.go`**

   Change:
   - Add import `task "github.com/bborbe/agent/lib/command/task"`
   - Replace `lib.UpdateFrontmatterCommand{...}` → `task.UpdateFrontmatterCommand{...}`
   - Replace `lib.IncrementFrontmatterCommand{...}` → `task.IncrementFrontmatterCommand{...}`
   - Replace `lib.BodySection{...}` → `task.BodySection{...}`
   - Replace `lib.UpdateFrontmatterCommandOperation` → `task.UpdateFrontmatterCommandOperation`
   - Replace `lib.IncrementFrontmatterCommandOperation` → `task.IncrementFrontmatterCommandOperation`
   - Keep `lib "github.com/bborbe/agent/lib"` for `lib.Task`, `lib.TaskV1SchemaID`, etc.

8. **Migrate `task/executor/pkg/result_publisher_test.go`**

   Read the full file. If it references `lib.UpdateFrontmatterCommand`, `lib.IncrementFrontmatterCommand`, or `lib.BodySection`, replace them with `task.*`. Add the `task` import if needed. Keep `lib` import if other lib types are still used.

9. **Verify no surviving references to migrated types in callers**

   After steps 1–8, run:
   ```bash
   grep -rn "lib\.CreateTask\|lib\.UpdateFrontmatter\|lib\.IncrementFrontmatter\|lib\.BodySection" \
       task/controller/ task/executor/ \
       --include="*.go"
   ```
   Must return no matches. Fix any remaining references before proceeding.

10. **Delete old `lib/` files**

    ```bash
    cd lib && git rm agent_task-commands.go
    cd lib && git rm agent_create-task-command.go
    cd lib && git rm agent_create-task-command-sender.go
    cd lib && git rm agent_create-task-command-sender_test.go
    cd lib && git rm agent_task-commands_test.go
    cd lib && git rm mocks/lib-create-task-command-sender.go
    ```

    **Why `agent_task-commands_test.go` is deleted in full:** every test in the file references `lib.CreateTaskCommand`, `lib.UpdateFrontmatterCommand`, `lib.IncrementFrontmatterCommand`, or operation constants that no longer exist in `lib/`. All tests are replicated in `lib/command/task/` by prompt 1. No test content survives in `lib/`.

    **Note on `lib/agent_markdown.go`:** it contains a comment `// The CQRS BodySection (in...`. Update this comment to reference `task.BodySection` from `lib/command/task` instead, but do NOT change any production code — this is a comment-only change. Read the file to see the exact line before editing.

    ```bash
    grep -n "BodySection" lib/agent_markdown.go
    ```
    Update the comment to avoid stale documentation.

11. **Run iterative tests after each module change**

    After steps 1–8 (before deleting):
    ```bash
    cd task/controller && make test
    cd task/executor && make test
    ```

    After step 10 (after deleting):
    ```bash
    cd lib && make test
    ```

    Fix any failures before proceeding.

12. **Verify coverage in new package is still ≥80%**

    ```bash
    cd lib && go test -coverprofile=/tmp/task-cover.out -mod=vendor ./command/task/...
    go tool cover -func=/tmp/task-cover.out | grep "total:"
    ```

13. **Update `CHANGELOG.md` at repo root**

    If a `## Unreleased` section already exists, append to it. Add:
    ```markdown
    - refactor(lib): move `CreateTaskCommand` (→ `task.CreateCommand`), `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand`, and `BodySection` to `lib/command/task` sub-package; remove flat `agent_task-commands.go`
    - refactor(task/controller): migrate command executors to `lib/command/task` types
    - refactor(task/executor): migrate `ResultPublisher` to `lib/command/task` types
    ```

14. **Final precommit in all three module directories**

    ```bash
    cd lib && make precommit
    cd task/controller && make precommit
    cd task/executor && make precommit
    ```

    All three must exit 0.

</requirements>

<constraints>
- The new import alias is `task "github.com/bborbe/agent/lib/command/task"` in every migrated file
- Type name mapping:
  - `lib.CreateTaskCommand` → `task.CreateCommand`
  - `lib.UpdateFrontmatterCommand` → `task.UpdateFrontmatterCommand`
  - `lib.IncrementFrontmatterCommand` → `task.IncrementFrontmatterCommand`
  - `lib.BodySection` → `task.BodySection`
- Operation constant mapping (used in local re-declarations inside controller executors):
  - `lib.CreateTaskCommandOperation` → `task.CreateCommandOperation`
  - `lib.UpdateFrontmatterCommandOperation` → `task.UpdateFrontmatterCommandOperation`
  - `lib.IncrementFrontmatterCommandOperation` → `task.IncrementFrontmatterCommandOperation`
- Wire format is unchanged — JSON tags are identical in the new package; Kafka consumers decode existing events without modification
- `lib.TaskIdentifier`, `lib.TaskFrontmatter`, `lib.Task`, `lib.TaskV1SchemaID` remain in `lib/` — do NOT move or delete them
- After deleting `agent_task-commands.go`, the `lib` package must still compile; its remaining files must not reference `CreateTaskCommand`, `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand`, or `BodySection`
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`
- All existing tests must pass in all three modules after migration
- Coverage for `lib/command/task/` remains ≥80% (no net decrease from prompt 1)
- Do NOT commit — dark-factory handles git
- All three `make precommit` runs must exit 0
</constraints>

<verification>

Verify old lib files are gone:
```bash
ls lib/agent_task-commands.go 2>&1
ls lib/agent_create-task-command.go 2>&1
ls lib/agent_create-task-command-sender.go 2>&1
ls lib/mocks/lib-create-task-command-sender.go 2>&1
```
All four must report "No such file or directory".

Verify no surviving references to old types in callers:
```bash
grep -rn "lib\.CreateTask\|lib\.UpdateFrontmatter\|lib\.IncrementFrontmatter\|lib\.BodySection" \
    task/controller/ task/executor/ lib/ \
    --include="*.go"
```
Must return no matches.

Verify new import in each migrated file:
```bash
grep -n "lib/command/task" \
    task/controller/pkg/command/task_create_task_executor.go \
    task/controller/pkg/command/task_update_frontmatter_executor.go \
    task/controller/pkg/command/task_increment_frontmatter_executor.go \
    task/executor/pkg/result_publisher.go
```
Must show four matches.

Verify type renames in controller:
```bash
grep -n "task\.CreateCommand\b\|task\.UpdateFrontmatterCommand\|task\.IncrementFrontmatterCommand\|task\.BodySection" \
    task/controller/pkg/command/task_create_task_executor.go \
    task/controller/pkg/command/task_update_frontmatter_executor.go \
    task/controller/pkg/command/task_increment_frontmatter_executor.go
```
Must show matches in all three files.

Verify type renames in executor:
```bash
grep -n "task\.UpdateFrontmatterCommand\|task\.IncrementFrontmatterCommand\|task\.BodySection" \
    task/executor/pkg/result_publisher.go
```
Must show matches.

Verify operation constants updated:
```bash
grep -n "task\.CreateCommandOperation\|task\.UpdateFrontmatterCommandOperation\|task\.IncrementFrontmatterCommandOperation" \
    task/controller/pkg/command/
```
Must show the constants sourcing from the new package.

Verify lib still compiles and tests pass:
```bash
cd lib && go build ./...
cd lib && make test
```
Must exit 0.

Verify coverage:
```bash
cd lib && go test -coverprofile=/tmp/task-cover.out -mod=vendor ./command/task/... && \
    go tool cover -func=/tmp/task-cover.out | grep "total:"
```
Coverage must be ≥80%.

Run all three precommits:
```bash
cd lib && make precommit
cd task/controller && make precommit
cd task/executor && make precommit
```
All three must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "lib/command/task\|agent_task-commands" CHANGELOG.md
```
Must show the Unreleased entries.

</verification>
