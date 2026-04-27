---
status: committing
spec: ["017"]
summary: Implemented CreateTaskCommand executor in task/controller/pkg/command/, wired it into the factory, added full Ginkgo test coverage (7 cases), and updated CHANGELOG.md
container: agent-083-spec-017-controller-create-task-executor
dark-factory-version: v0.135.19-1-gc08c946
created: "2026-04-27T20:30:00Z"
queued: "2026-04-27T20:25:25Z"
started: "2026-04-27T20:28:23Z"
branch: dark-factory/create-task-command
---

<summary>
- Adds a new CQRS executor `task_create_task_executor.go` in `task/controller/pkg/command/` that handles `CreateTaskCommandOperation`
- Executor validates the command (non-empty TaskIdentifier, required `assignee` and `status` frontmatter fields) and returns a wrapped error if validation fails
- Idempotency: if a file already exists at the derived path for the TaskIdentifier, the executor logs at info level and returns nil (no-op); the vault is not modified
- On success: marshals frontmatter + body into standard vault markdown format and calls `gitClient.AtomicWriteAndCommitPush` to create the file and push in one atomic operation
- File path is derived deterministically from `task_identifier` only (`taskDir/<task_identifier>.md`) — no producer-controlled string flows into the path beyond the validated identifier
- Executor is wired into the controller's command consumer in `task/controller/pkg/factory/factory.go`
- Full Ginkgo/Gomega test coverage: success path, idempotency (file already exists), empty identifier, missing `assignee`, missing `status`, git write error
- CHANGELOG entry documenting the controller change
</summary>

<objective>
Implement the `CreateTaskCommand` executor in `task/controller/pkg/command/` and wire it into the controller's CQRS command consumer factory. After this prompt, any service in the cluster can publish a `CreateTaskCommand` to Kafka and the controller will create the corresponding vault task file — without each producer needing vault git access or duplicating path-layout knowledge.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-cqrs.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `cdb.CommandObjectExecutorTxFunc`, `cdb.ErrCommandObjectSkipped`, RunCommandConsumerTxDefault pattern
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, Counterfeiter mocks, external test packages
- `go-validation-framework-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — validation.Error sentinel

**Prerequisite:** Prompt 1 (spec-017 lib) has shipped. `lib.CreateTaskCommandOperation` and `lib.CreateTaskCommand` exist in the lib package. Verify before editing:
```bash
grep -n "CreateTaskCommandOperation\|CreateTaskCommand" lib/agent_task-commands.go
```
If the grep returns empty, STOP and report `status: failed` with message "lib types not yet deployed (prompt 1 of spec-017)".

**Key files to read in full before editing:**

- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — canonical executor reference. Note the constructor signature `NewIncrementFrontmatterExecutor(gitClient, taskDir)`, the `cdb.CommandObjectExecutorTxFunc(operation, idempotent, handler)` pattern, the unmarshal-command → find-path → atomic-write-push flow, and the shared helper functions (`parseTaskFrontmatter`, `marshalFileContent`, `intFromFrontmatter`).

- `task/controller/pkg/command/task_update_frontmatter_executor.go` — second reference; shows `buildUpdateModifyFn` pattern. Note that for UPDATE the code calls `AtomicReadModifyWriteAndCommitPush` (read-modify-write). For CREATE we call `AtomicWriteAndCommitPush` (write-only — no existing content to read).

- `task/controller/pkg/gitclient/git_client.go` — GitClient interface. The method for creating a new file is:
  ```go
  AtomicWriteAndCommitPush(
      ctx context.Context,
      absPath string,
      content []byte,
      message string,
  ) error
  ```
  Use this for CREATE (not `AtomicReadModifyWriteAndCommitPush`).

- `task/controller/pkg/result/result_writer.go` — contains `ExtractFrontmatter`, `ExtractBody`, and `FindTaskFilePath`. The new executor does NOT use `FindTaskFilePath` for the idempotency check because the path is deterministic from `task_identifier` — use `os.Stat` instead (cheaper, no directory walk).

- `task/controller/pkg/factory/factory.go` — `CreateCommandConsumer` function. Shows how executors are collected into `cdb.CommandObjectExecutorTxs{}` and passed to `cdb.RunCommandConsumerTxDefault`. Add `command.NewCreateTaskExecutor(gitClient, taskDir)` to this slice.

- `task/controller/pkg/command/` — list the directory to understand which test suite file exists (`*_suite_test.go`). The new `task_create_task_executor_test.go` must register in the same Ginkgo suite.

Run before editing:
```bash
ls task/controller/pkg/command/
grep -n "CommandObjectExecutorTxs\|NewIncrementFrontmatter\|NewUpdateFrontmatter" task/controller/pkg/factory/factory.go
```
</context>

<requirements>

1. **Create `task/controller/pkg/command/task_create_task_executor.go`**

   Full implementation following the increment-frontmatter executor pattern:

   ```go
   package command

   import (
       "context"
       "os"
       "path/filepath"

       "github.com/bborbe/cqrs/base"
       "github.com/bborbe/cqrs/cdb"
       "github.com/bborbe/errors"
       libkv "github.com/bborbe/kv"
       "github.com/golang/glog"
       "gopkg.in/yaml.v3"

       lib "github.com/bborbe/agent/lib"
       "github.com/bborbe/agent/task/controller/pkg/gitclient"
   )

   // NewCreateTaskExecutor creates a cdb.CommandObjectExecutorTx that materializes
   // a new vault task file for the given task_identifier. If a file for that identifier
   // already exists the command is a strict no-op (idempotent). Frontmatter must include
   // "assignee" and "status"; missing fields return a wrapped validation error.
   func NewCreateTaskExecutor(
       gitClient gitclient.GitClient,
       taskDir string,
   ) cdb.CommandObjectExecutorTx {
       return cdb.CommandObjectExecutorTxFunc(
           lib.CreateTaskCommandOperation,
           true,
           func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
               var cmd lib.CreateTaskCommand
               if err := commandObject.Command.Data.MarshalInto(ctx, &cmd); err != nil {
                   return nil, nil, errors.Wrapf(
                       ctx,
                       cdb.ErrCommandObjectSkipped,
                       "malformed CreateTaskCommand: %v",
                       err,
                   )
               }
               if err := cmd.TaskIdentifier.Validate(ctx); err != nil {
                   return nil, nil, errors.Wrapf(ctx, err, "validate task_identifier")
               }
               if err := validateCreateTaskFrontmatter(ctx, cmd.Frontmatter); err != nil {
                   return nil, nil, errors.Wrapf(ctx, err, "validate frontmatter")
               }
               taskDirPath := filepath.Join(gitClient.Path(), taskDir)
               absPath := filepath.Join(taskDirPath, string(cmd.TaskIdentifier)+".md")
               if _, err := os.Stat(absPath); err == nil {
                   glog.Infof("create-task: task file already exists for %s, skipping (idempotent)", cmd.TaskIdentifier)
                   return nil, nil, nil
               }
               content, err := buildCreateTaskContent(ctx, cmd)
               if err != nil {
                   return nil, nil, errors.Wrapf(ctx, err, "build task file content for %s", cmd.TaskIdentifier)
               }
               if err := gitClient.AtomicWriteAndCommitPush(
                   ctx,
                   absPath,
                   content,
                   "[agent-task-controller] create task "+string(cmd.TaskIdentifier),
               ); err != nil {
                   return nil, nil, errors.Wrapf(ctx, err, "atomic write and push for task %s", cmd.TaskIdentifier)
               }
               glog.V(2).Infof("create-task: created task file for %s", cmd.TaskIdentifier)
               return nil, nil, nil
           },
       )
   }
   ```

   Add these helpers in the same file:

   ```go
   func validateCreateTaskFrontmatter(ctx context.Context, fm lib.TaskFrontmatter) error {
       if fm.Assignee() == "" {
           return errors.Wrap(ctx, validation.Error, "frontmatter missing required field: assignee")
       }
       if status, _ := fm.String("status"); status == "" {
           return errors.Wrap(ctx, validation.Error, "frontmatter missing required field: status")
       }
       return nil
   }

   func buildCreateTaskContent(ctx context.Context, cmd lib.CreateTaskCommand) ([]byte, error) {
       // Inject task_identifier into the frontmatter so the scanner can index this file.
       fm := make(lib.TaskFrontmatter)
       for k, v := range cmd.Frontmatter {
           fm[k] = v
       }
       fm["task_identifier"] = string(cmd.TaskIdentifier)
       marshaled, err := yaml.Marshal(map[string]any(fm))
       if err != nil {
           return nil, errors.Wrapf(ctx, err, "marshal frontmatter")
       }
       return []byte("---\n" + string(marshaled) + "---\n" + cmd.Body), nil
   }
   ```

   **Import paths:** resolve the exact module path from existing executor files (grep for the import block in `task_increment_frontmatter_executor.go`). Do NOT guess — copy the `github.com/bborbe/agent/...` import paths from the existing file.

   **Validation pattern**: `lib/agent_task-assignee.go` line 23 shows the canonical pattern: `errors.Wrap(ctx, validation.Error, "<message>")` (import `github.com/bborbe/validation`). Use this — match the existing convention so failures route correctly to the spec's "Wrapped validation error" failure mode.

   **`fm.String("status")`** returns `(string, bool)`. Use the comma-ok form: `if s, _ := fm.String("status"); s == ""`. Do NOT compare the call result directly — that won't compile.

2. **Create `task/controller/pkg/command/task_create_task_executor_test.go`**

   External test package (`package command_test`), same suite as sibling files. Coverage must include all specified paths.

   Test table structure (mirror the increment executor test pattern):

   ```go
   package command_test

   import (
       "context"
       "os"
       "path/filepath"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"

       lib "github.com/bborbe/agent/lib"
       "github.com/bborbe/agent/task/controller/pkg/command"
       // import the fake gitclient from mocks/ — resolve path from sibling tests
   )

   var _ = Describe("NewCreateTaskExecutor", func() {
       // ... (see test cases below)
   })
   ```

   **Required test cases:**

   a. **Malformed command payload** — `commandObject.Command.Data.MarshalInto` fails → executor returns wrapped `cdb.ErrCommandObjectSkipped`; vault not written.

   b. **Empty TaskIdentifier** — `cmd.TaskIdentifier = ""` → executor returns validation error; vault not written.

   c. **Missing `assignee` in frontmatter** — Frontmatter has `status` but no `assignee` → executor returns validation error; vault not written.

   d. **Missing `status` in frontmatter** — Frontmatter has `assignee` but no `status` → executor returns validation error; vault not written.

   e. **File already exists (idempotency)** — Pre-create `absPath` before calling executor; executor returns nil; git write NOT called (fake gitclient's `AtomicWriteAndCommitPush` call count stays 0).

   f. **Success: new file created** — Valid command with `task_identifier`, `assignee`, `status`, and a body string; fake gitclient's `AtomicWriteAndCommitPush` called exactly once; content passed to write contains `---\n` + `task_identifier` + `assignee` + `status` in YAML frontmatter + `---\n` + body; commit message contains the `task_identifier`.

   g. **Git write error** — Fake gitclient's `AtomicWriteAndCommitPush` returns an error → executor returns wrapped error.

   **Test setup pattern (mirror `task_increment_frontmatter_executor_test.go`):**
   - Use the Counterfeiter-generated fake GitClient from `task/controller/mocks/` (check the mocks directory)
   - Create a temp directory with `GinkgoT().TempDir()` as the git root
   - Wire `NewCreateTaskExecutor(fakeGitClient, taskDir)` and invoke the inner handler function via the CQRS test helper pattern used in sibling tests

   Before writing any mock import paths, grep:
   ```bash
   ls task/controller/mocks/
   grep -rn "fakeGitClient\|FakeGitClient\|gitclient.GitClient" task/controller/pkg/command/*_test.go | head -5
   ```

3. **Wire `NewCreateTaskExecutor` into `task/controller/pkg/factory/factory.go`**

   In `CreateCommandConsumer`, add the new executor to the `cdb.CommandObjectExecutorTxs{}` slice:

   ```go
   executors := cdb.CommandObjectExecutorTxs{
       command.NewTaskResultExecutor(resultWriter),
       command.NewIncrementFrontmatterExecutor(gitClient, taskDir),
       command.NewUpdateFrontmatterExecutor(gitClient, taskDir),
       command.NewCreateTaskExecutor(gitClient, taskDir),   // ← add this line
   }
   ```

   No other changes to `factory.go`.

4. **Regenerate mocks if needed**

   Run `make generate` in `task/controller/` if the GitClient interface or any mock source annotation changed. If the mocks are already up to date, skip.

5. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased` (create the section above the latest version heading if absent — sibling prompt 1 may have already created it):

   ```markdown
   - feat(task/controller): add `CreateTaskCommand` executor — controller now materializes vault task files on Kafka command; idempotent (no-op if file already exists), validates required frontmatter fields (assignee, status)
   ```

6. **Run tests iteratively**

   After implementing the executor and tests:
   ```bash
   cd task/controller && make test
   ```
   Fix any failures before proceeding.

   Then run full precommit:
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- File path MUST be derived as `filepath.Join(taskDirPath, string(cmd.TaskIdentifier)+".md")` — no producer-controlled substring may flow into the path beyond the validated `TaskIdentifier`
- TaskIdentifier validation is delegated to `cmd.TaskIdentifier.Validate(ctx)` (the existing method on `TaskIdentifier`) — do NOT re-implement validation inline
- Idempotency check uses `os.Stat` (cheap path existence check) — do NOT use `result.FindTaskFilePath` (expensive directory walk, unnecessary for deterministic paths)
- File MUST contain `task_identifier` in its frontmatter so the scanner can index it on next pull
- Use `gitClient.AtomicWriteAndCommitPush` for the new-file write (not `AtomicReadModifyWriteAndCommitPush` — there is no existing content to read)
- Error wrapping via `github.com/bborbe/errors` — never `fmt.Errorf`
- Logging via `github.com/golang/glog` — `glog.Infof` for the idempotency no-op, `glog.V(2).Infof` for success
- Ginkgo v2 + Gomega; Counterfeiter fakes (never manual mocks)
- External test package (`command_test`) — matches sibling test files
- All existing tests must pass after the change
- Do NOT modify `lib/` — the types were shipped in prompt 1
- Do NOT modify `task/executor/` — this change is controller-only
- Do NOT modify `result_writer.go` or any existing executor — additive changes only
- Do NOT commit — dark-factory handles git
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>

Verify the executor file was created:
```bash
ls task/controller/pkg/command/task_create_task_executor.go
```

Verify the constructor signature:
```bash
grep -n "func NewCreateTaskExecutor" task/controller/pkg/command/task_create_task_executor.go
```
Must show `gitClient gitclient.GitClient, taskDir string`.

Verify factory wiring:
```bash
grep -n "NewCreateTaskExecutor" task/controller/pkg/factory/factory.go
```
Must show one line adding the executor to the slice.

Verify idempotency path uses os.Stat:
```bash
grep -n "os.Stat" task/controller/pkg/command/task_create_task_executor.go
```
Must show the existence check.

Verify task_identifier is injected into the written frontmatter:
```bash
grep -n "task_identifier" task/controller/pkg/command/task_create_task_executor.go
```
Must show the `fm["task_identifier"] = string(cmd.TaskIdentifier)` line in `buildCreateTaskContent`.

Verify tests cover idempotency and validation:
```bash
grep -n "already exists\|missing.*assignee\|missing.*status\|Empty.*Identifier" task/controller/pkg/command/task_create_task_executor_test.go
```

Run tests:
```bash
cd task/controller && go test -v ./pkg/command/... -run CreateTask
```
Must exit 0 and list all specified cases as PASS.

Run full precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

Verify no underscore in the new operation constant (carried over from lib prompt 1, but sanity-check here):
```bash
grep -rn "create_task" task/controller/ lib/ --include='*.go'
```
Must return zero lines.

Verify CHANGELOG updated:
```bash
grep -n "CreateTaskCommand\|create-task" CHANGELOG.md
```
Must show at least two entries (one from lib prompt 1, one from this prompt).

</verification>
