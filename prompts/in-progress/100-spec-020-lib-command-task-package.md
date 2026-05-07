---
status: committing
spec: [020-agent-lib-command-package-restructure]
summary: Created lib/command/task package with CreateCommand, UpdateFrontmatterCommand, IncrementFrontmatterCommand types, Validate methods, typed senders, counterfeiter mocks, and comprehensive tests; added dupl exclusion for sender files in .golangci.yml.
container: agent-100-spec-020-lib-command-task-package
dark-factory-version: v0.151.2-4-g3dc5753
created: "2026-05-07T18:00:00Z"
queued: "2026-05-07T18:17:58Z"
started: "2026-05-07T18:18:00Z"
branch: dark-factory/agent-lib-command-package-restructure
---

<summary>
- All three agent task commands now validate themselves before publishing — the create-task command keeps its existing rules from the previous spec, and the two frontmatter commands gain new rules that reject malformed payloads at the producer
- Each command has a typed sender that refuses to publish an invalid payload to Kafka — invalid commands return an error without touching the wire
- The wire format is byte-identical: existing producers and consumers see the exact same JSON and the exact same operation strings; this is a pure code-organization change
- Existing files in the lib package are left untouched in this step — callers still compile against the old types; the next prompt switches them over and deletes the old files
- The new layout mirrors the trading lib's per-command package pattern, so future changes touch one file per command instead of a flat shared file
</summary>

<objective>
Create the `lib/command/task/` package — the canonical home for all three agent task commands — with per-command files, `Validate` methods, typed senders, counterfeiter mocks, and comprehensive tests. This is prompt 1 of 2 for spec-020; prompt 2 migrates callers and deletes the now-superseded flat `lib/agent_task-commands.go` and related files. No existing files are modified here.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-validation-framework-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `validation.All`, `validation.Name`, `validation.HasValidationFunc`, `validation.Error`
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega `DescribeTable`/`Entry`, external test packages, ≥80% coverage
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `New*`/`Create*` constructors, zero business logic in factories
- `go-cqrs.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `cdb.CommandObjectSender`, `CommandObject` construction

**Prerequisite: spec-019 has shipped.** Verify before editing:
```bash
grep -n "func (cmd CreateTaskCommand) Validate" lib/agent_create-task-command.go
grep -n "Title" lib/agent_task-commands.go
```
If either grep returns empty, STOP and report `status: failed` with message "spec-019 lib changes not yet deployed (prompt 1 of spec-019)".

**Key files to read in full before editing:**

- `lib/agent_task-commands.go` — current `CreateTaskCommand`, `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand` structs and their operation constants. Note `BodySection` is also defined here (used by `UpdateFrontmatterCommand.Body`). All JSON tags and operation strings must be preserved exactly.
- `lib/agent_create-task-command.go` — existing `Validate` logic for `CreateTaskCommand` (title + body rules). Port this verbatim into the new package's `create-command.go`.
- `lib/agent_create-task-command-sender.go` — existing `CreateTaskCommandSender` sender pattern: counterfeiter annotation, interface, factory, private struct, `SendCommand` implementation. Mirror this for all three senders.
- `lib/agent_create-task-command-sender_test.go` — sender test pattern (three cases: validation fails → publisher not called; validation passes → publisher called with correct operation + schemaID; publisher returns error → propagated). Mirror for all three sender tests.
- `lib/agent_task.go` — `validation.All` / `validation.Name` composition pattern. Mirror exactly.
- `lib/lib_suite_test.go` — counterfeiter `//go:generate` directive pattern and Ginkgo suite bootstrap.

Run before editing to confirm state:
```bash
grep -n "BodySection" lib/agent_task-commands.go lib/agent_markdown.go
grep -n "TaskV1SchemaID" lib/agent_cdb-schema.go
ls lib/mocks/
```
</context>

<requirements>

1. **Create directory `lib/command/task/`**

   ```bash
   mkdir -p lib/command/task/mocks
   ```

2. **Create `lib/command/task/create-command.go`**

   Package `task`. Contains `CreateCommand` struct (renamed from `lib.CreateTaskCommand` — idiomatic Go drops the redundant "Task" prefix since the package is already named `task`), `CreateCommandOperation` constant, and the `Validate` method ported verbatim from `lib/agent_create-task-command.go`.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
       "context"
       "strings"

       "github.com/bborbe/cqrs/base"
       "github.com/bborbe/errors"
       "github.com/bborbe/validation"

       lib "github.com/bborbe/agent/lib"
   )

   // CreateCommandOperation is the Kafka command operation for creating a new vault task.
   // Wire string unchanged: "create-task".
   const CreateCommandOperation base.CommandOperation = "create-task"

   // CreateCommand is the payload for CreateCommandOperation.
   // JSON tags are byte-identical to the former lib.CreateTaskCommand for wire compatibility.
   type CreateCommand struct {
       TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
       Title          string              `json:"title"`
       Frontmatter    lib.TaskFrontmatter `json:"frontmatter"`
       Body           string              `json:"body,omitempty"`
   }

   // Validate enforces CreateCommand schema rules before publishing or processing.
   func (cmd CreateCommand) Validate(ctx context.Context) error {
       return validation.All{
           validation.Name("Title", validateCreateTitle(cmd.Title)),
           validation.Name("Body", validateCreateBody(cmd.Body)),
       }.Validate(ctx)
   }
   ```

   Then port the three private helper functions from `lib/agent_create-task-command.go` into this file, renaming them to drop the "CreateTask" prefix:
   - `validateCreateTaskTitle(title string)` → `validateCreateTitle(title string)`
   - `validateCreateTaskBody(body string)` → `validateCreateBody(body string)`
   - `validateTitleEdges`, `validateTitleForbiddenChars`, `validateTitleWindowsReserved` — keep their names (they are already package-scoped).

   Read `lib/agent_create-task-command.go` fully to copy the exact logic. Do not simplify or rewrite it.

3. **Create `lib/command/task/update-frontmatter-command.go`**

   Package `task`. Contains `UpdateFrontmatterCommand` struct (same name — no rename needed since "Frontmatter" disambiguates within the `task` package), `UpdateFrontmatterCommandOperation` constant, `BodySection` struct (moved from `lib/agent_task-commands.go`), and `Validate` method.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       "github.com/bborbe/errors"
       "github.com/bborbe/validation"

       lib "github.com/bborbe/agent/lib"
   )

   // UpdateFrontmatterCommandOperation is the Kafka command operation for partial frontmatter update.
   // Wire string unchanged: "update-frontmatter".
   const UpdateFrontmatterCommandOperation base.CommandOperation = "update-frontmatter"

   // UpdateFrontmatterCommand is the payload for UpdateFrontmatterCommandOperation.
   // JSON tags are byte-identical to the former lib.UpdateFrontmatterCommand.
   type UpdateFrontmatterCommand struct {
       TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
       Updates        lib.TaskFrontmatter `json:"updates"`
       Body           *BodySection        `json:"body,omitempty"`
   }

   // BodySection describes an idempotent body-section write for UpdateFrontmatterCommand.
   // Heading MUST include the markdown prefix (e.g. "## Failure").
   // Section MUST include the heading as its first line and a trailing newline.
   type BodySection struct {
       Heading string `json:"heading"`
       Section string `json:"section"`
   }

   // Validate enforces UpdateFrontmatterCommand schema rules before publishing.
   // TaskIdentifier must be non-empty. At least one of Updates (non-empty map) or
   // Body (non-nil) must be set — a no-op command with both absent is a producer bug.
   func (cmd UpdateFrontmatterCommand) Validate(ctx context.Context) error {
       return validation.All{
           validation.Name("TaskIdentifier", cmd.TaskIdentifier),
           validation.Name("UpdatesOrBody", validation.HasValidationFunc(func(ctx context.Context) error {
               if len(cmd.Updates) == 0 && cmd.Body == nil {
                   return errors.Wrap(ctx, validation.Error, "at least one of Updates or Body must be set")
               }
               return nil
           })),
       }.Validate(ctx)
   }
   ```

4. **Create `lib/command/task/increment-frontmatter-command.go`**

   Package `task`. Contains `IncrementFrontmatterCommand` struct (same name), `IncrementFrontmatterCommandOperation` constant, and `Validate` method.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       "github.com/bborbe/errors"
       "github.com/bborbe/validation"

       lib "github.com/bborbe/agent/lib"
   )

   // IncrementFrontmatterCommandOperation is the Kafka command operation for atomic field increment.
   // Wire string unchanged: "increment-frontmatter".
   const IncrementFrontmatterCommandOperation base.CommandOperation = "increment-frontmatter"

   // IncrementFrontmatterCommand is the payload for IncrementFrontmatterCommandOperation.
   // JSON tags are byte-identical to the former lib.IncrementFrontmatterCommand.
   type IncrementFrontmatterCommand struct {
       TaskIdentifier lib.TaskIdentifier `json:"taskIdentifier"`
       Field          string             `json:"field"`
       Delta          int                `json:"delta"`
   }

   // Validate enforces IncrementFrontmatterCommand schema rules before publishing.
   // TaskIdentifier and Field must be non-empty. Delta is unconstrained (zero and negative are valid).
   func (cmd IncrementFrontmatterCommand) Validate(ctx context.Context) error {
       return validation.All{
           validation.Name("TaskIdentifier", cmd.TaskIdentifier),
           validation.Name("Field", validation.HasValidationFunc(func(ctx context.Context) error {
               if cmd.Field == "" {
                   return errors.Wrap(ctx, validation.Error, "field must not be empty")
               }
               return nil
           })),
       }.Validate(ctx)
   }
   ```

5. **Create `lib/command/task/create-command-sender.go`**

   Port from `lib/agent_create-task-command-sender.go`, adapting names for the new package. The sender calls `cmd.Validate(ctx)` before publishing via `cdb.CommandObjectSender`.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       cdb "github.com/bborbe/cqrs/cdb"
       cqrsiam "github.com/bborbe/cqrs/iam"
       "github.com/bborbe/errors"

       lib "github.com/bborbe/agent/lib"
   )

   //counterfeiter:generate -o mocks/task-create-command-sender.go --fake-name TaskCreateCommandSender . CreateCommandSender

   // CreateCommandSender sends CreateCommand payloads to Kafka.
   // Calls Validate before publishing — a validation error is returned without touching Kafka.
   type CreateCommandSender interface {
       SendCommand(ctx context.Context, cmd CreateCommand) error
   }

   // NewCreateCommandSender creates a CreateCommandSender using the given cdb.CommandObjectSender.
   func NewCreateCommandSender(commandObjectSender cdb.CommandObjectSender) CreateCommandSender {
       return &createCommandSender{
           commandObjectSender: commandObjectSender,
       }
   }

   type createCommandSender struct {
       commandObjectSender cdb.CommandObjectSender
   }

   func (s *createCommandSender) SendCommand(ctx context.Context, cmd CreateCommand) error {
       if err := cmd.Validate(ctx); err != nil {
           return errors.Wrapf(ctx, err, "validate CreateCommand")
       }
       event, err := base.ParseEvent(ctx, cmd)
       if err != nil {
           return errors.Wrapf(ctx, err, "parse CreateCommand event")
       }
       requestIDCh := make(chan base.RequestID, 1)
       requestIDCh <- base.NewRequestID()
       commandCreator := base.NewCommandCreator(requestIDCh)
       commandObject := cdb.CommandObject{
           Command: commandCreator.NewCommand(
               CreateCommandOperation,
               cqrsiam.Initiator("lib"),
               "",
               event,
           ),
           SchemaID: lib.TaskV1SchemaID,
       }
       if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
           return errors.Wrapf(ctx, err, "send CreateCommand to Kafka")
       }
       return nil
   }
   ```

   **Before writing:** grep to confirm `lib.TaskV1SchemaID` exists:
   ```bash
   grep -n "TaskV1SchemaID" lib/agent_cdb-schema.go
   ```
   And confirm the import aliases match the existing sender in `lib/agent_create-task-command-sender.go`:
   ```bash
   grep -n "cqrs/iam\|cqrs/cdb\|cqrs/base" lib/agent_create-task-command-sender.go
   ```

6. **Create `lib/command/task/update-frontmatter-command-sender.go`**

   Same pattern as the create sender. The initiator string is `"lib"` (matching existing senders).

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       cdb "github.com/bborbe/cqrs/cdb"
       cqrsiam "github.com/bborbe/cqrs/iam"
       "github.com/bborbe/errors"

       lib "github.com/bborbe/agent/lib"
   )

   //counterfeiter:generate -o mocks/task-update-frontmatter-command-sender.go --fake-name TaskUpdateFrontmatterCommandSender . UpdateFrontmatterCommandSender

   // UpdateFrontmatterCommandSender sends UpdateFrontmatterCommand payloads to Kafka.
   // Calls Validate before publishing — a validation error is returned without touching Kafka.
   type UpdateFrontmatterCommandSender interface {
       SendCommand(ctx context.Context, cmd UpdateFrontmatterCommand) error
   }

   // NewUpdateFrontmatterCommandSender creates an UpdateFrontmatterCommandSender.
   func NewUpdateFrontmatterCommandSender(commandObjectSender cdb.CommandObjectSender) UpdateFrontmatterCommandSender {
       return &updateFrontmatterCommandSender{
           commandObjectSender: commandObjectSender,
       }
   }

   type updateFrontmatterCommandSender struct {
       commandObjectSender cdb.CommandObjectSender
   }

   func (s *updateFrontmatterCommandSender) SendCommand(ctx context.Context, cmd UpdateFrontmatterCommand) error {
       if err := cmd.Validate(ctx); err != nil {
           return errors.Wrapf(ctx, err, "validate UpdateFrontmatterCommand")
       }
       event, err := base.ParseEvent(ctx, cmd)
       if err != nil {
           return errors.Wrapf(ctx, err, "parse UpdateFrontmatterCommand event")
       }
       requestIDCh := make(chan base.RequestID, 1)
       requestIDCh <- base.NewRequestID()
       commandCreator := base.NewCommandCreator(requestIDCh)
       commandObject := cdb.CommandObject{
           Command: commandCreator.NewCommand(
               UpdateFrontmatterCommandOperation,
               cqrsiam.Initiator("lib"),
               "",
               event,
           ),
           SchemaID: lib.TaskV1SchemaID,
       }
       if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
           return errors.Wrapf(ctx, err, "send UpdateFrontmatterCommand to Kafka")
       }
       return nil
   }
   ```

7. **Create `lib/command/task/increment-frontmatter-command-sender.go`**

   Same pattern.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       cdb "github.com/bborbe/cqrs/cdb"
       cqrsiam "github.com/bborbe/cqrs/iam"
       "github.com/bborbe/errors"

       lib "github.com/bborbe/agent/lib"
   )

   //counterfeiter:generate -o mocks/task-increment-frontmatter-command-sender.go --fake-name TaskIncrementFrontmatterCommandSender . IncrementFrontmatterCommandSender

   // IncrementFrontmatterCommandSender sends IncrementFrontmatterCommand payloads to Kafka.
   // Calls Validate before publishing — a validation error is returned without touching Kafka.
   type IncrementFrontmatterCommandSender interface {
       SendCommand(ctx context.Context, cmd IncrementFrontmatterCommand) error
   }

   // NewIncrementFrontmatterCommandSender creates an IncrementFrontmatterCommandSender.
   func NewIncrementFrontmatterCommandSender(commandObjectSender cdb.CommandObjectSender) IncrementFrontmatterCommandSender {
       return &incrementFrontmatterCommandSender{
           commandObjectSender: commandObjectSender,
       }
   }

   type incrementFrontmatterCommandSender struct {
       commandObjectSender cdb.CommandObjectSender
   }

   func (s *incrementFrontmatterCommandSender) SendCommand(ctx context.Context, cmd IncrementFrontmatterCommand) error {
       if err := cmd.Validate(ctx); err != nil {
           return errors.Wrapf(ctx, err, "validate IncrementFrontmatterCommand")
       }
       event, err := base.ParseEvent(ctx, cmd)
       if err != nil {
           return errors.Wrapf(ctx, err, "parse IncrementFrontmatterCommand event")
       }
       requestIDCh := make(chan base.RequestID, 1)
       requestIDCh <- base.NewRequestID()
       commandCreator := base.NewCommandCreator(requestIDCh)
       commandObject := cdb.CommandObject{
           Command: commandCreator.NewCommand(
               IncrementFrontmatterCommandOperation,
               cqrsiam.Initiator("lib"),
               "",
               event,
           ),
           SchemaID: lib.TaskV1SchemaID,
       }
       if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
           return errors.Wrapf(ctx, err, "send IncrementFrontmatterCommand to Kafka")
       }
       return nil
   }
   ```

8. **Create `lib/command/task/task_suite_test.go`**

   Ginkgo suite bootstrap for the new package. External test package `task_test`. Include the `//go:generate` directive so `go generate ./...` from `lib/` picks up the counterfeiter annotations.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package task_test

   import (
       "testing"
       "time"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
       "github.com/onsi/gomega/format"
   )

   //go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate

   func TestTask(t *testing.T) {
       time.Local = time.UTC
       format.TruncatedDiff = false
       RegisterFailHandler(Fail)
       RunSpecs(t, "Task Suite")
   }
   ```

9. **Create `lib/command/task/create-command_test.go`**

   External package `task_test`. Tests for JSON wire format, operation constant value, and `Validate`.

   Include:
   - JSON round-trip test for `CreateCommand` (verify all fields serialize/deserialize correctly including `title`)
   - Test that `CreateCommandOperation` equals `base.CommandOperation("create-task")`
   - `Validate` tests: mirror the full `CreateTaskCommand.Validate` test block from `lib/agent_task-commands_test.go` (empty title, 200/201 runes, forbidden chars table, path traversal, leading/trailing space/dot, Windows reserved names table, unicode, dot mid-name, body size limits)

   Read `lib/agent_task-commands_test.go` fully — copy and adapt the `CreateTaskCommand.Validate` tests, replacing `lib.CreateTaskCommand` with `task.CreateCommand`.

   Wire-format test: assert that `json.Marshal(CreateCommand{TaskIdentifier: "t1", Title: "T", Frontmatter: lib.TaskFrontmatter{"status": "todo"}})` produces JSON containing `"taskIdentifier"`, `"title"`, `"frontmatter"` as keys and that the `body` key is absent (omitempty).

10. **Create `lib/command/task/update-frontmatter-command_test.go`**

    External package `task_test`. Tests for JSON wire format, operation constant value, and `Validate`.

    Include:
    - JSON round-trip: `UpdateFrontmatterCommand{TaskIdentifier: "t", Updates: lib.TaskFrontmatter{"status": "done"}}` → marshal → unmarshal → assert fields
    - JSON omits `body` key when `Body` is nil
    - `UpdateFrontmatterCommandOperation` equals `"update-frontmatter"`
    - `Validate` cases:
      a. Valid: `TaskIdentifier` non-empty + `Updates` non-empty → `Succeed()`
      b. Valid: `TaskIdentifier` non-empty + `Body` non-nil (Updates empty) → `Succeed()`
      c. Valid: `TaskIdentifier` non-empty + both `Updates` non-empty and `Body` non-nil → `Succeed()`
      d. Error: empty `TaskIdentifier` + non-empty `Updates` → error containing "TaskIdentifier" (or "empty")
      e. Error: non-empty `TaskIdentifier` + empty `Updates` + nil `Body` → error containing "UpdatesOrBody" (or "at least one")
    - `BodySection` JSON round-trip: `{Heading: "## H", Section: "## H\n\ntext\n"}` → marshal → unmarshal

11. **Create `lib/command/task/increment-frontmatter-command_test.go`**

    External package `task_test`. Tests for JSON wire format, operation constant value, and `Validate`.

    Include:
    - JSON round-trip: `IncrementFrontmatterCommand{TaskIdentifier: "t", Field: "trigger_count", Delta: 1}`
    - JSON handles zero delta (Delta: 0)
    - JSON handles negative delta (Delta: -1)
    - `IncrementFrontmatterCommandOperation` equals `"increment-frontmatter"`
    - `Validate` cases:
      a. Valid: `TaskIdentifier` non-empty + `Field` non-empty + `Delta` 0 → `Succeed()`
      b. Valid: `Delta` negative → `Succeed()` (unconstrained)
      c. Valid: `Delta` positive → `Succeed()`
      d. Error: empty `TaskIdentifier` → error
      e. Error: empty `Field` → error containing "Field" (or "empty")
      f. Error: empty `TaskIdentifier` AND empty `Field` → error (both fail)

12. **Create sender test files**

    For each sender, create one test file following the exact pattern in `lib/agent_create-task-command-sender_test.go`. Three cases per sender:
    - Validation fails → `SendCommandObjectCallCount()` is 0, error returned
    - Validation passes → called exactly once, `cmdObj.Command.Operation` equals the operation constant, `cmdObj.SchemaID` equals `lib.TaskV1SchemaID`
    - Publisher returns error → error propagated

    **`lib/command/task/create-command-sender_test.go`** — use `TaskCreateCommandSender` fake from `mocks/`; invalid cmd has empty `Title`; valid cmd has non-empty `Title` and `TaskIdentifier`.

    **`lib/command/task/update-frontmatter-command-sender_test.go`** — use `TaskUpdateFrontmatterCommandSender` fake; invalid cmd has empty `TaskIdentifier` + nil `Body` + empty `Updates`; valid cmd has `TaskIdentifier` + non-empty `Updates`.

    **`lib/command/task/increment-frontmatter-command-sender_test.go`** — use `TaskIncrementFrontmatterCommandSender` fake; invalid cmd has empty `Field`; valid cmd has non-empty `TaskIdentifier` and `Field`.

    Use `cqrsmocks.CDBCommandObjectSender` from `github.com/bborbe/cqrs/mocks` as the backing fake (same pattern as `lib/agent_create-task-command-sender_test.go`).

    Mocks will be at `lib/command/task/mocks/task-create-command-sender.go` etc. — they are generated in step 13. Write the test file BEFORE running generate; the test file's import of the mock will compile after generation.

13. **Generate mocks**

    ```bash
    cd lib && make generate
    ```

    This runs `go generate ./...`, which recurses into `lib/command/task/` and processes the three `//counterfeiter:generate` annotations (steps 5–7), producing:
    - `lib/command/task/mocks/task-create-command-sender.go` — fake name `TaskCreateCommandSender`
    - `lib/command/task/mocks/task-update-frontmatter-command-sender.go` — fake name `TaskUpdateFrontmatterCommandSender`
    - `lib/command/task/mocks/task-increment-frontmatter-command-sender.go` — fake name `TaskIncrementFrontmatterCommandSender`

    If `make generate` fails, run directly:
    ```bash
    cd lib/command/task && go generate ./...
    ```

14. **Update `CHANGELOG.md` at repo root**

    If a `## Unreleased` section already exists (created by spec-019 prompts), append to it. Otherwise create it above the most recent version header.

    Add:
    ```markdown
    - feat(lib): add `lib/command/task` package with `CreateCommand`, `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand` types, `Validate` methods, and typed command senders
    ```

15. **Run tests iteratively**

    ```bash
    cd lib && make test
    ```
    Fix any failures. Common issues:
    - Import cycle: `lib/command/task` must NOT import anything from `lib/command/task/mocks` in non-test files
    - Missing `lib.TaskV1SchemaID`: grep `lib/agent_cdb-schema.go` to confirm the symbol

    ```bash
    cd lib && make precommit
    ```
    Must exit 0.

</requirements>

<constraints>
- Package declaration in all new files: `package task`
- Test files: `package task_test` (external test package)
- Type renames: `CreateTaskCommand` → `task.CreateCommand`, `CreateTaskCommandOperation` → `task.CreateCommandOperation`. `UpdateFrontmatterCommand` and `IncrementFrontmatterCommand` keep their names (the package prefix already disambiguates). `BodySection` moves to `update-frontmatter-command.go`.
- JSON tags MUST be byte-identical to the old declarations — do NOT change any `json:"..."` tag
- Operation string values MUST be unchanged: `"create-task"`, `"update-frontmatter"`, `"increment-frontmatter"`
- `CreateCommand.Validate` logic must be a verbatim port from `lib/agent_create-task-command.go` — do NOT simplify or alter the title/body rules
- `UpdateFrontmatterCommand.Validate`: rejects empty `TaskIdentifier`; rejects both `Updates` being empty AND `Body` being nil simultaneously
- `IncrementFrontmatterCommand.Validate`: rejects empty `TaskIdentifier` or empty `Field`; `Delta` zero and negative are valid
- Senders call `cmd.Validate(ctx)` before `base.ParseEvent` — validation errors are returned without calling Kafka
- Counterfeiter annotations: fake names `TaskCreateCommandSender`, `TaskUpdateFrontmatterCommandSender`, `TaskIncrementFrontmatterCommandSender`; output paths relative to the source file (`mocks/task-*.go`)
- Mocks directory: `lib/command/task/mocks/`
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`
- No import cycle: `lib/command/task` imports `lib`; `lib` does NOT import `lib/command/task`
- DO NOT modify any existing file in `lib/` or elsewhere — callers are migrated in prompt 2
- Coverage for `lib/command/task/` ≥80%
- Do NOT commit — dark-factory handles git
- `cd lib && make precommit` must exit 0
</constraints>

<verification>

Verify package exists and has all expected files:
```bash
ls lib/command/task/*.go lib/command/task/mocks/*.go
```
Must show: `create-command.go`, `create-command-sender.go`, `update-frontmatter-command.go`, `update-frontmatter-command-sender.go`, `increment-frontmatter-command.go`, `increment-frontmatter-command-sender.go`, `task_suite_test.go`, test files, and three mock files.

Verify type names in new package:
```bash
grep -n "type CreateCommand struct\|type UpdateFrontmatterCommand struct\|type IncrementFrontmatterCommand struct\|type BodySection struct" lib/command/task/*.go
```
Must show all four types.

Verify operation constants:
```bash
grep -n "CreateCommandOperation\|UpdateFrontmatterCommandOperation\|IncrementFrontmatterCommandOperation" lib/command/task/*.go
```
Values must be `"create-task"`, `"update-frontmatter"`, `"increment-frontmatter"` respectively.

Verify Validate methods:
```bash
grep -n "func (cmd.*) Validate" lib/command/task/*.go
```
Must show three Validate methods (one per command, value receiver).

Verify counterfeiter annotations:
```bash
grep -n "counterfeiter:generate" lib/command/task/*.go
```
Must show three annotations pointing to `mocks/task-*.go`.

Verify mocks were generated:
```bash
ls lib/command/task/mocks/
```
Must show three generated files.

Verify no import cycle:
```bash
grep -rn "lib/command/task" lib/*.go lib/delivery/*.go 2>/dev/null
```
Must return no matches (lib/ does NOT import the sub-package).

Verify JSON tags preserved:
```bash
grep -n 'json:"' lib/command/task/create-command.go lib/command/task/update-frontmatter-command.go lib/command/task/increment-frontmatter-command.go
```
Must show `taskIdentifier`, `title`, `frontmatter`, `body,omitempty`, `updates`, `field`, `delta`, `heading`, `section`.

Run tests:
```bash
cd lib && go test -v -coverprofile=/tmp/task-cover.out -mod=vendor ./command/task/...
go tool cover -func=/tmp/task-cover.out | grep "total:"
```
Must exit 0. Coverage `total:` line must be ≥80%.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify old lib files untouched:
```bash
ls lib/agent_task-commands.go lib/agent_create-task-command.go lib/agent_create-task-command-sender.go
```
All three must still exist (migration happens in prompt 2).

</verification>
