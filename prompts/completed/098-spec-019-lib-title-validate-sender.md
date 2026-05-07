---
status: completed
spec: [019-human-readable-vault-task-paths]
summary: Added Title field to CreateTaskCommand, Validate method with cross-platform-safe title rules, CreateTaskCommandSender interface with factory and counterfeiter mock, and comprehensive tests covering all validation cases.
container: agent-098-spec-019-lib-title-validate-sender
dark-factory-version: v0.151.2-4-g3dc5753
created: "2026-05-07T16:04:09Z"
queued: "2026-05-07T16:23:49Z"
started: "2026-05-07T16:23:50Z"
completed: "2026-05-07T16:29:56Z"
branch: dark-factory/human-readable-vault-task-paths
---

<summary>
- `CreateTaskCommand` gains a required `Title string` field with `json:"title"` tag (no `omitempty`) — this is the human-readable vault filename hint
- `CreateTaskCommand.Validate(ctx)` enforces cross-platform-safe rules on `Title` (length 1..200, forbidden characters, path-traversal sequences, Windows reserved names, edge whitespace and dots) and max size on `Body` (≤500 KiB)
- Validation is composed via `github.com/bborbe/validation` (`validation.All` / `validation.Name`) — no ad-hoc string checks
- A new `CreateTaskCommandSender` interface (with counterfeiter annotation → `mocks/lib-create-task-command-sender.go`) provides a typed Kafka sender for the command
- `NewCreateTaskCommandSender(cdb.CommandObjectSender) CreateTaskCommandSender` is the factory constructor — follows the same in-repo pattern as `resultPublisher` in `task/executor/pkg/result_publisher.go`
- The sender's `SendCommand(ctx, cmd)` calls `cmd.Validate(ctx)` before publishing; a validation error is returned without calling Kafka
- Operation constant strings are unchanged: `"create-task"` is still `CreateTaskCommandOperation`
- Existing `CreateTaskCommand` tests (JSON round-trip, `omitempty` body) continue to pass
- `make precommit` is clean in `agent/lib`
</summary>

<objective>
Add a `Title` field to `CreateTaskCommand` in `agent/lib`, expose a `Validate(ctx)` method that enforces cross-platform-safe title rules and body size bounds, and ship a typed sender helper with counterfeiter mock so producers can publish create-task commands with validation guarantees before hitting Kafka. This is prompt 1 of 2 for spec-019; prompt 2 wires the `Title` field into the controller executor.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-validation-framework-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `validation.All`, `validation.Name`, `HasValidationFunc`, sentinel `validation.Error`
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega `DescribeTable`/`Entry`, external test packages, `≥80%` coverage rule
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `New*`/`Create*` constructors, zero business logic in factories
- `go-cqrs.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `cdb.CommandObjectSender`, `CommandObject` construction, `base.ParseEvent`

**Key files to read in full before editing:**

- `lib/agent_task-commands.go` — current `CreateTaskCommand` struct (has `TaskIdentifier`, `Frontmatter`, `Body`; no `Title` yet); warning comment above constants.
- `lib/agent_task-commands_test.go` — existing JSON round-trip tests for `CreateTaskCommand`. The new `Validate` tests must go in this same file or a new `lib/agent_create-task-command_test.go` in the same `lib_test` external package.
- `lib/agent_task.go` — canonical `validation.All` / `validation.Name` composition pattern (`Task.Validate`). Mirror this exactly.
- `lib/agent_task-identifier.go` — `TaskIdentifier.Validate` — shows `errors.Wrap(ctx, validation.Error, "...")` pattern.
- `lib/delivery/result-deliverer.go` — in-repo sender pattern: `cdb.NewCommandObjectSender`, `base.ParseEvent`, `commandCreator.NewCommand(operation, initiator, "", event)`. The new sender uses this same flow.
- `task/executor/pkg/result_publisher.go` — second in-repo sender example; shows `publishRaw` helper pattern.
- `lib/delivery/result-deliverer_test.go` — shows how to test a sender using `cqrsmocks.CDBCommandObjectSender` from `github.com/bborbe/cqrs/mocks`.

Run before editing to confirm current state:
```bash
grep -n "Title\|Validate" lib/agent_task-commands.go
grep -n "CreateTaskCommand" lib/agent_task-commands_test.go
ls lib/mocks/
```
</context>

<requirements>

1. **Add `Title` field to `CreateTaskCommand` in `lib/agent_task-commands.go`**

   Insert the `Title` field as the second field of the struct (after `TaskIdentifier`, before `Frontmatter`):

   ```go
   type CreateTaskCommand struct {
       TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
       Title          string          `json:"title"`
       Frontmatter    TaskFrontmatter `json:"frontmatter"`
       Body           string          `json:"body,omitempty"`
   }
   ```

   `Title` has no `omitempty` — it is required. The tag is `json:"title"`.

2. **Add `Validate(ctx context.Context) error` method on `CreateTaskCommand`**

   Create a new file `lib/agent_create-task-command.go`:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package lib

   import (
       "context"
       "strings"
       "unicode"

       "github.com/bborbe/errors"
       "github.com/bborbe/validation"
   )

   // Validate enforces CreateTaskCommand schema rules before publishing or processing.
   // Title must be cross-platform safe (see rules below). Body must be ≤500 KiB when non-empty.
   func (cmd CreateTaskCommand) Validate(ctx context.Context) error {
       return validation.All{
           validation.Name("Title", validateCreateTaskTitle(ctx, cmd.Title)),
           validation.Name("Body", validateCreateTaskBody(ctx, cmd.Body)),
       }.Validate(ctx)
   }

   func validateCreateTaskTitle(ctx context.Context, title string) validation.HasValidation {
       return validation.HasValidationFunc(func(ctx context.Context) error {
           // Length: 1..200 characters (rune count)
           runes := []rune(title)
           if len(runes) == 0 {
               return errors.Wrap(ctx, validation.Error, "title must not be empty")
           }
           if len(runes) > 200 {
               return errors.Wrapf(ctx, validation.Error, "title length %d exceeds maximum 200 characters", len(runes))
           }
           // Forbidden edges: leading/trailing space or dot
           if title[0] == ' ' || title[0] == '.' {
               return errors.Wrap(ctx, validation.Error, "title must not start with a space or dot")
           }
           if title[len(title)-1] == ' ' || title[len(title)-1] == '.' {
               return errors.Wrap(ctx, validation.Error, "title must not end with a space or dot")
           }
           // Forbidden sequence: path traversal
           if strings.Contains(title, "..") {
               return errors.Wrap(ctx, validation.Error, "title must not contain '..' (path traversal)")
           }
           // Forbidden characters: < > : " / \ | ? * and control chars 0x00-0x1F, 0x7F
           for _, r := range title {
               if r < 0x20 || r == 0x7F {
                   return errors.Wrapf(ctx, validation.Error, "title contains forbidden control character U+%04X", r)
               }
               switch r {
               case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
                   return errors.Wrapf(ctx, validation.Error, "title contains forbidden character %q", r)
               }
           }
           // Forbidden Windows reserved names (case-insensitive, with or without extension)
           base := title
           if idx := strings.LastIndex(title, "."); idx > 0 {
               base = title[:idx]
           }
           upper := strings.ToUpper(base)
           switch upper {
           case "CON", "PRN", "AUX", "NUL",
               "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
               "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
               return errors.Wrapf(ctx, validation.Error, "title %q is a forbidden Windows reserved name", title)
           }
           return nil
       })
   }

   func validateCreateTaskBody(ctx context.Context, body string) validation.HasValidation {
       return validation.HasValidationFunc(func(ctx context.Context) error {
           if len(body) > 500*1024 {
               return errors.Wrapf(ctx, validation.Error, "body length %d bytes exceeds maximum 500 KiB", len(body))
           }
           return nil
       })
   }
   ```

   Note: `unicode` import is needed only if you use `unicode.IsControl` — if you use the explicit range check `r < 0x20 || r == 0x7F`, you don't need the `unicode` import. Remove unused imports.

   Verify the signature: `func (cmd CreateTaskCommand) Validate(ctx context.Context) error` — value receiver, not pointer.

3. **Create sender helper `lib/agent_create-task-command-sender.go`**

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package lib

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       cdb "github.com/bborbe/cqrs/cdb"
       cqrsiam "github.com/bborbe/cqrs/iam"
       "github.com/bborbe/errors"
   )

   //counterfeiter:generate -o mocks/lib-create-task-command-sender.go --fake-name LibCreateTaskCommandSender . CreateTaskCommandSender

   // CreateTaskCommandSender sends CreateTaskCommand payloads to Kafka.
   // It calls Validate before publishing — a validation error is returned without touching Kafka.
   type CreateTaskCommandSender interface {
       SendCommand(ctx context.Context, cmd CreateTaskCommand) error
   }

   // NewCreateTaskCommandSender creates a CreateTaskCommandSender using the given cdb.CommandObjectSender.
   func NewCreateTaskCommandSender(commandObjectSender cdb.CommandObjectSender) CreateTaskCommandSender {
       return &createTaskCommandSender{
           commandObjectSender: commandObjectSender,
       }
   }

   type createTaskCommandSender struct {
       commandObjectSender cdb.CommandObjectSender
   }

   func (s *createTaskCommandSender) SendCommand(ctx context.Context, cmd CreateTaskCommand) error {
       if err := cmd.Validate(ctx); err != nil {
           return errors.Wrapf(ctx, err, "validate CreateTaskCommand")
       }
       event, err := base.ParseEvent(ctx, cmd)
       if err != nil {
           return errors.Wrapf(ctx, err, "parse CreateTaskCommand event")
       }
       requestIDCh := make(chan base.RequestID, 1)
       requestIDCh <- base.NewRequestID()
       commandCreator := base.NewCommandCreator(requestIDCh)
       commandObject := cdb.CommandObject{
           Command: commandCreator.NewCommand(
               CreateTaskCommandOperation,
               cqrsiam.Initiator("lib"),
               "",
               event,
           ),
           SchemaID: TaskV1SchemaID,
       }
       if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
           return errors.Wrapf(ctx, err, "send CreateTaskCommand to Kafka")
       }
       return nil
   }
   ```

   **Before writing imports:** grep to confirm the import paths exist:
   ```bash
   grep -rn "cqrsiam\|cqrs/iam" lib/delivery/result-deliverer.go
   grep -rn "base.ParseEvent\|base.NewCommandCreator" lib/delivery/result-deliverer.go
   grep -rn "cdb.NewCommandObject\|cqrsiam.Initiator" task/executor/pkg/result_publisher.go
   ```
   Use the exact import aliases found in those files.

4. **Regenerate mocks**

   ```bash
   cd lib && make generate
   ```

   This produces `lib/mocks/lib-create-task-command-sender.go`. Counterfeiter wiring lives in `lib/lib_suite_test.go` (a `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate` directive) and `lib/tools.env` (`COUNTERFEITER_VERSION`). Existing mocks (e.g. `lib-step.go`, `lib-result-deliverer.go`) prove `make generate` already works — no `tools.go` file exists in this repo. If `make generate` fails, confirm `COUNTERFEITER_VERSION` is set in `lib/tools.env` and the `//go:generate` directive exists in `lib/lib_suite_test.go`.

5. **Add `Validate` tests in `lib/agent_task-commands_test.go`**

   Add a new `Describe("CreateTaskCommand.Validate", ...)` block after the existing `CreateTaskCommand` JSON tests. Use external package `lib_test`. Tests must cover:

   a. **Valid title** — `"My Task"` → `Succeed()`

   b. **Empty title** → error containing "empty"

   c. **Title length 200** (exactly) → `Succeed()`

   d. **Title length 201** → error containing "exceed" or "200"

   e. **Forbidden characters** — use `DescribeTable` with one entry per character class:
      - `"bad<title"` (< forbidden)
      - `"bad>title"` (> forbidden)
      - `"bad:title"` (: forbidden)
      - `"bad\"title"` (" forbidden)
      - `"bad/title"` (/ forbidden)
      - `"bad\\title"` (\ forbidden)
      - `"bad|title"` (| forbidden)
      - `"bad?title"` (? forbidden)
      - `"bad*title"` (* forbidden)
      - title containing `\x01` (control char 0x01)
      - title containing `\x7F` (DEL)
      All must return error

   f. **Path traversal** — `"some..title"` → error

   g. **Leading space** — `" leading"` → error

   h. **Trailing space** — `"trailing "` → error

   i. **Leading dot** — `".hidden"` → error

   j. **Trailing dot** — `"trailing."` → error

   k. **Windows reserved names** — `DescribeTable` with entries:
      - `"CON"`, `"con"`, `"Con"` (case variations)
      - `"CON.md"` (with extension)
      - `"PRN"`, `"AUX"`, `"NUL"`
      - `"COM1"`, `"COM9"`
      - `"LPT1"`, `"LPT9"`
      All must return error

   l. **Unicode allowed** — `"Täsk Überblick"` → `Succeed()`

   m. **Dot mid-name allowed** — `"my.task-name"` → `Succeed()`

   n. **Body max exceeded** — `Body: strings.Repeat("x", 500*1024+1)` with a valid title → error

   o. **Body exactly 500 KiB** — `Body: strings.Repeat("x", 500*1024)` → `Succeed()`

   p. **Empty body** — `Body: ""` with valid title → `Succeed()`

6. **Add sender tests in `lib/agent_create-task-command-sender_test.go`**

   External package `lib_test`, same Ginkgo suite. Tests:

   a. **Validation fails → publisher not called**
      Use an invalid command (empty `Title`); `sender.SendCommand(ctx, cmd)` returns error;
      `fakeSender.SendCommandObjectCallCount()` is 0.

   b. **Validation passes → publisher called exactly once with correct operation**
      Use a valid command; `sender.SendCommand(ctx, cmd)` returns nil;
      `fakeSender.SendCommandObjectCallCount()` is 1;
      `_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)`;
      `cmdObj.Command.Operation` equals `lib.CreateTaskCommandOperation`;
      `cmdObj.SchemaID` equals `lib.TaskV1SchemaID`.

   c. **Publisher returns error → error propagated**
      Valid command; `fakeSender.SendCommandObjectReturns(errors.New("kafka down"))`;
      `sender.SendCommand` returns an error containing "kafka down".

   Use `cqrsmocks.CDBCommandObjectSender` from `github.com/bborbe/cqrs/mocks` as the fake sender (same pattern as `lib/delivery/result-deliverer_test.go`).

   ```go
   package lib_test

   import (
       "context"

       cqrsmocks "github.com/bborbe/cqrs/mocks"
       stderrors "errors"
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"

       "github.com/bborbe/agent/lib"
   )

   var _ = Describe("CreateTaskCommandSender", func() {
       var (
           ctx        context.Context
           fakeSender *cqrsmocks.CDBCommandObjectSender
           sender     lib.CreateTaskCommandSender
       )

       BeforeEach(func() {
           ctx = context.Background()
           fakeSender = &cqrsmocks.CDBCommandObjectSender{}
           sender = lib.NewCreateTaskCommandSender(fakeSender)
       })

       // ... test cases a, b, c as described above
   })
   ```

7. **Update existing `CreateTaskCommand` JSON tests**

   The existing JSON round-trip tests in `lib/agent_task-commands_test.go` for `CreateTaskCommand` must be updated to include the new `Title` field. Find the two existing `It(...)` specs and add:
   - `Title: "My Task Name"` to the `cmd` struct literal
   - `Expect(got.Title).To(Equal(cmd.Title))` after the unmarshal assertions

8. **Update `CHANGELOG.md` at repo root**

   This repo's `CHANGELOG.md` uses release-versioned headers (`## v0.54.17`, `## v0.54.16`, …) — there is NO `## Unreleased` section by convention. Prepend a new `## Unreleased` section at the top of the file (above the most recent version header) and add the entries there. The release tooling will rename `## Unreleased` → `## vX.Y.Z` on the next release.

   Final shape (after edit):

   ```markdown
   # Changelog

   ## Unreleased

   - feat(lib): add `Title` field to `CreateTaskCommand` with cross-platform-safe validation rules enforced by a new `Validate(ctx)` method
   - feat(lib): add `CreateTaskCommandSender` interface and `NewCreateTaskCommandSender` factory with validate-before-send invariant

   ## v0.54.17

   - fix(ci): point `actions/setup-go` …
   ```

9. **Run tests iteratively**

   ```bash
   cd lib && make test
   ```

   Fix any failures before proceeding (most likely: existing JSON round-trip tests break because `Title` is now required — update them per step 7 above).

   ```bash
   cd lib && make precommit
   ```

   Must exit 0.

</requirements>

<constraints>
- `Title` field: tag is `json:"title"` — no `omitempty`. It is required.
- `Validate` must compose via `validation.All` / `validation.Name` — no ad-hoc `if err != nil` chains at the top level
- Windows reserved name check: case-insensitive on the base name (before the last `.`); check both `"CON"` and `"CON.md"`
- Forbidden character check: `< > : " / \ | ? *` and control chars `0x00-0x1F`, `0x7F`. Do NOT forbid spaces mid-name, hyphens, underscores, or unicode letters/digits
- Body validation: empty body (len==0) is valid (a task may have only frontmatter); Body > 500*1024 bytes is invalid. (Spec line 12 was clarified to say `length ≤500 KiB; empty body is valid` — supersedes earlier `1..500 KiB` phrasing.)
- Sender file: counterfeiter annotation must use `--fake-name LibCreateTaskCommandSender` and output to `mocks/lib-create-task-command-sender.go`
- `SendCommand` signature: `SendCommand(ctx context.Context, cmd CreateTaskCommand) error` (not pointer)
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`
- Ginkgo v2 + Gomega; external test package `lib_test`
- Do NOT modify `lib/agent_task-commands.go` constants — operation strings are unchanged
- Do NOT modify `task/controller/` — controller is wired in prompt 2
- All existing tests must pass after struct field addition (update JSON tests per step 7)
- Do NOT commit — dark-factory handles git
- `cd lib && make precommit` must exit 0
</constraints>

<verification>

Verify `Title` field added:
```bash
grep -n "Title" lib/agent_task-commands.go
```
Must show `Title string \`json:"title"\`` with no `omitempty`.

Verify `Validate` method exists:
```bash
grep -n "func (cmd CreateTaskCommand) Validate" lib/agent_create-task-command.go
```
Must show the method.

Verify sender interface with counterfeiter annotation:
```bash
grep -n "counterfeiter:generate\|CreateTaskCommandSender" lib/agent_create-task-command-sender.go
```
Must show both the annotation and the interface.

Verify mock was generated:
```bash
ls lib/mocks/lib-create-task-command-sender.go
```
Must exist.

Verify validation covers forbidden chars and Windows names:
```bash
grep -n "CON\|LPT\|forbidden" lib/agent_create-task-command.go
```
Must show the Windows reserved name list.

Run Validate unit tests:
```bash
cd lib && go test -v -run "CreateTaskCommand.Validate" ./...
```
Must exit 0 with all cases PASS.

Run sender tests:
```bash
cd lib && go test -v -run "CreateTaskCommandSender" ./...
```
Must exit 0.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "CreateTaskCommandSender\|Title.*field" CHANGELOG.md
```
Must show Unreleased entries.

</verification>
