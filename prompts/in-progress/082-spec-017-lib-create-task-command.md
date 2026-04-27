---
status: committing
spec: ["017"]
summary: 'Added CreateTaskCommandOperation constant and CreateTaskCommand struct to lib/agent_task-commands.go, extended the validation regression table in agent_task-commands_test.go, added JSON round-trip tests for CreateTaskCommand, and updated CHANGELOG.md under ## Unreleased.'
container: agent-082-spec-017-lib-create-task-command
dark-factory-version: v0.135.19-1-gc08c946
created: "2026-04-27T20:30:00Z"
queued: "2026-04-27T20:25:25Z"
started: "2026-04-27T20:25:27Z"
branch: dark-factory/create-task-command
---

<summary>
- Adds `CreateTaskCommandOperation` constant (`"create-task"`) to the lib package alongside the existing `IncrementFrontmatterCommandOperation` and `UpdateFrontmatterCommandOperation`
- Adds `CreateTaskCommand` struct carrying a `TaskIdentifier`, initial `TaskFrontmatter`, and optional `Body` string
- Extends the cqrs validation regression test table in `lib/agent_task-commands_test.go` so CI catches any future operation-string violations for the new constant
- The new constant is the only wire-format contract between producers (pr-watcher, cron jobs) and the controller — no other file changes
- CHANGELOG entry documenting the new minor-bump addition
</summary>

<objective>
Add the `CreateTaskCommand` type and `CreateTaskCommandOperation` constant to the `lib/` package so that producers can publish a well-typed Kafka command to request task creation. This is the first of two prompts for spec-017; the controller executor is wired in prompt 2.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-cqrs.md` in `~/.claude/plugins/marketplaces/coding/docs/` — CommandOperation shape and validation, cqrs regex `^[a-z][a-z-]*$`
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega DescribeTable/Entry, external test packages

**Key files to read in full before editing:**

- `lib/agent_task-commands.go` — existing command constants and struct definitions. The file currently defines `IncrementFrontmatterCommandOperation`, `UpdateFrontmatterCommandOperation`, `IncrementFrontmatterCommand`, `UpdateFrontmatterCommand`, and `BodySection`. The new constant and struct must follow exactly the same pattern.

  The warning comment block at the top of the constants section reads:
  ```
  // IMPORTANT: operation strings must match base.CommandOperation.Validate regex
  // `^[a-z][a-z-]*$` (lowercase letters and hyphens only, starting with a letter).
  // Underscores, digits, and uppercase are REJECTED at runtime by cqrs.
  // Every constant below MUST also be added to the Validate-all test table in
  // agent_task-commands_test.go. CI catches misses there.
  ```

- `lib/agent_task-commands_test.go` — contains the `DescribeTable("all lib CommandOperation constants pass base.CommandOperation.Validate", ...)` regression table. Every new constant must get an `Entry(...)` here. The file uses the `lib_test` external package, dots-imports Ginkgo/Gomega, and imports `github.com/bborbe/cqrs/base` and the `lib` package.

- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` type (`map[string]interface{}`). The `CreateTaskCommand.Frontmatter` field uses this exact type (same as `UpdateFrontmatterCommand.Updates`).

- `lib/agent_task-identifier.go` — `TaskIdentifier` type (string alias with `Validate()` method). The `CreateTaskCommand.TaskIdentifier` field uses this exact type.

Run before editing to confirm current state:
```bash
grep -n "CommandOperation\|type.*Command\b" lib/agent_task-commands.go
grep -n "Entry(" lib/agent_task-commands_test.go
```
</context>

<requirements>

1. **Add `CreateTaskCommandOperation` constant in `lib/agent_task-commands.go`**

   Append after the existing `UpdateFrontmatterCommandOperation` constant (keeping the warning comment block above all constants):

   ```go
   // CreateTaskCommandOperation is the Kafka command operation for creating a new vault task.
   // The controller materializes a task file at the standard vault location for the given
   // task_identifier. If a file already exists for that identifier, the command is a no-op.
   const CreateTaskCommandOperation base.CommandOperation = "create-task"
   ```

   The value `"create-task"` MUST pass the cqrs regex `^[a-z][a-z-]*$` — verify by inspection.

2. **Add `CreateTaskCommand` struct in `lib/agent_task-commands.go`**

   Append after the `UpdateFrontmatterCommand` and `BodySection` definitions:

   ```go
   // CreateTaskCommand is the payload for CreateTaskCommandOperation.
   // The controller creates a new vault task file at the standard path for TaskIdentifier,
   // writing the supplied Frontmatter and optional Body. If a file for TaskIdentifier already
   // exists the command is a strict no-op (idempotent). Frontmatter MUST include at minimum
   // "assignee" and "status" keys; the executor rejects the command with a validation error
   // if either is absent.
   type CreateTaskCommand struct {
   	TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
   	Frontmatter    TaskFrontmatter `json:"frontmatter"`
   	Body           string          `json:"body,omitempty"`
   }
   ```

   The `Frontmatter` field name mirrors `UpdateFrontmatterCommand.Updates` semantics but is
   called `Frontmatter` here because it describes the initial full frontmatter, not a partial
   update.

3. **Update the validation regression table in `lib/agent_task-commands_test.go`**

   Add one `Entry` to the existing `DescribeTable("all lib CommandOperation constants pass base.CommandOperation.Validate", ...)`:

   ```go
   Entry("CreateTaskCommandOperation", lib.CreateTaskCommandOperation),
   ```

   Place it after the existing `UpdateFrontmatterCommandOperation` entry. Do NOT delete or
   reorder the existing entries.

4. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased` (create the section if absent):

   ```markdown
   - feat(lib): add `CreateTaskCommand` and `CreateTaskCommandOperation = "create-task"` so producers can request vault task creation via Kafka without embedding vault git logic
   ```

5. **Run tests**

   ```bash
   cd lib && make test
   ```

   Must exit 0. Then run:

   ```bash
   cd lib && go test -run CommandOperation -v ./...
   ```

   Output must include `CreateTaskCommandOperation` PASS.

</requirements>

<constraints>
- Only edit these files: `lib/agent_task-commands.go`, `lib/agent_task-commands_test.go`, `CHANGELOG.md`
- Operation string MUST be `"create-task"` — matches cqrs regex `^[a-z][a-z-]*$`
- Do NOT modify `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand`, or `BodySection`
- Do NOT modify `lib/agent_task-identifier.go` or `lib/agent_task-frontmatter.go`
- Do NOT add any business logic to the lib package — constants and types only
- Use `github.com/bborbe/errors` for any new error paths (none expected in this prompt)
- Ginkgo v2 only. External test package (`lib_test`) — matches existing file
- All existing tests must pass
- Do NOT commit — dark-factory handles git
- `cd lib && make precommit` must exit 0
</constraints>

<verification>

Verify constant value and GoDoc comment:
```bash
grep -n "CreateTaskCommandOperation\|CreateTaskCommand" lib/agent_task-commands.go
```
Must show the constant with value `"create-task"` and the struct definition.

Verify operation passes cqrs regex (manually: `"create-task"` → only lowercase letters and hyphens → passes `^[a-z][a-z-]*$`).

Verify test entry exists:
```bash
grep -n "CreateTaskCommandOperation" lib/agent_task-commands_test.go
```
Must show one `Entry(...)` line.

Run the regression test:
```bash
cd lib && go test -run CommandOperation -v ./...
```
Must exit 0 and show PASS for all three entries: IncrementFrontmatterCommandOperation, UpdateFrontmatterCommandOperation, CreateTaskCommandOperation.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "CreateTaskCommand\|create-task" CHANGELOG.md
```
Must show the Unreleased entry.

</verification>
