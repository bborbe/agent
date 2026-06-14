---
status: approved
spec: [044-multi-vault-routing]
created: "2026-06-14T21:22:30Z"
queued: "2026-06-14T21:26:34Z"
branch: dark-factory/multi-vault-routing
---

<summary>
- The CreateCommandSender constructor now takes a second argument that lets the producer stamp a default vault onto every command that doesn't carry one explicitly
- An input command with empty TargetVault is published with the default vault substituted in; an input command with TargetVault already set is published as-is, never overridden
- When the constructor receives an invalid default vault (one that fails the slug regex), construction itself succeeds — the error surfaces on the first SendCommand call, not at startup, so a misconfigured producer fails loudly at the first publish
- The empty-string default preserves the pre-spec behavior exactly: every existing test passes with the new second argument set to ""
- The single in-repo test call site of NewCreateCommandSender is updated to the two-argument form, and five new tests cover the four required matrix cases (no override, default applied, no override on already-set, invalid default)
- The counterfeiter annotation on CreateCommandSender is updated to the new mock path / fake name; running `make generate` from the lib directory regenerates the mock
- No consumer of the new behavior is added in this prompt — the controller-side routing lives in prompt 3
- The CHANGELOG is not touched in this prompt; the entry is owned by prompt 3 (the controller-side one) to keep the changelog free of duplicate bullet points

</summary>

<objective>
Extend `NewCreateCommandSender` to accept a `defaultVault string` second argument. When `SendCommand` is called with a command whose `TargetVault` is empty and `defaultVault` is non-empty, the sender substitutes `defaultVault` into the command before publishing. The sender does NOT override a non-empty `TargetVault`. An invalid `defaultVault` is detected at first publish time (not at construction) and surfaces as a wrapped validation error. Update the single in-repo call site of the constructor (the existing test file) to pass `""` as the second argument, and add the unit tests required by spec 044 AC 5, 6, 7, 8.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md` (Interface → Constructor → Struct + error wrapping rules).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` (use `github.com/bborbe/errors`, never `fmt.Errorf`; `Wrapf` for formatted messages).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` (Ginkgo v2 / Gomega / no stdlib table tests / no direct `*testing.T` outside the suite entry-point).

Key files to read in full before editing:
- `/workspace/lib/command/task/create-command.go` — the `CreateCommand` struct (now with the new `TargetVault string \`json:"targetVault,omitempty"\`` field added by prompt 1) and its `Validate` method (which now validates the slug regex on `TargetVault`)
- `/workspace/lib/command/task/create-command-sender.go` — the file to modify; read fully (it is short, 62 lines)
- `/workspace/lib/command/task/create-command-sender_test.go` — the existing Ginkgo test file; the `BeforeEach` at line 26 builds the sender with the old one-argument form and must change
- `/workspace/mocks/task-create-command-sender.go` — the counterfeiter-generated fake; running `make generate` from the lib service directory regenerates it after the interface/annotation changes
- `/workspace/lib/command/task/create-command_test.go` — read-only reference; confirms the new `TargetVault` field exists and that the slug regex is exercised on the struct

Inlined load-bearing snippets (copy verbatim into the new code, do not paraphrase from memory):

Current `NewCreateCommandSender` (lines 27-31 of `create-command-sender.go`):
```go
func NewCreateCommandSender(commandObjectSender cdb.CommandObjectSender) CreateCommandSender {
    return &createCommandSender{
        commandObjectSender: commandObjectSender,
    }
}
```

Current `createCommandSender` struct (lines 33-35):
```go
type createCommandSender struct {
    commandObjectSender cdb.CommandObjectSender
}
```

Current `SendCommand` (lines 37-61):
```go
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

Current counterfeiter annotation (line 18, above the interface):
```go
//counterfeiter:generate -o ../../mocks/task-create-command-sender.go --fake-name TaskCreateCommandSender . CreateCommandSender
```

The interface body (lines 22-24) does NOT change:
```go
type CreateCommandSender interface {
    SendCommand(ctx context.Context, cmd CreateCommand) error
}
```

Spec being implemented: `specs/in-progress/044-multi-vault-routing.md`. The exact substitution semantics (Desired Behavior #3), the deferred-validation rule (Desired Behavior #4), and the four Acceptance Criteria this prompt covers (AC 5, 6, 7, 8) are spelled out in the spec.

Predecessor prompt: `1-multi-vault-routing-targetvault-field.md` (must run first and complete successfully — the `TargetVault` field and the slug regex must exist on `CreateCommand` before this prompt starts; this prompt calls `cmd.Validate(ctx)` to revalidate the substituted command, which depends on prompt 1's `validateCreateTargetVault`).
</context>

<requirements>

1. **Grep for all in-repo callers of `NewCreateCommandSender`**

   Before making any change, run:
   ```bash
   grep -rn "NewCreateCommandSender" /workspace/ --include="*.go"
   ```
   The expected output is exactly two non-spec lines:
   - the function definition in `create-command-sender.go`
   - the test call site in `create-command-sender_test.go` line 29 (`sender = task.NewCreateCommandSender(fakeSender)`)

   No other in-repo Go file constructs the sender today — the only producer that publishes `task.CreateCommand` is external to this repo (the spec lists it as a follow-up). This prompt updates only the test call site. If grep finds additional production callers in `/workspace/` (excluding `specs/`), STOP and surface this in the `## Improvements` section of the completion report as a PROMPT-category issue — do not guess at the right default value for those callers.

2. **Extend the `createCommandSender` struct with a `defaultVault` field**

   In `/workspace/lib/command/task/create-command-sender.go`, modify the `createCommandSender` struct (currently lines 33-35) to add the new field as the SECOND field. Field order is `commandObjectSender, defaultVault` — keeping the existing field first minimizes the diff and the struct literal in `NewCreateCommandSender` is the only call site that needs updating.

   ```go
   type createCommandSender struct {
       commandObjectSender cdb.CommandObjectSender
       defaultVault        string
   }
   ```

3. **Extend `NewCreateCommandSender` with the second argument**

   Change the signature to accept `defaultVault string` and store it in the struct. Construction does NOT validate the value — the spec says the validation surfaces on first publish, not at construction (Desired Behavior #4). The new signature:

   ```go
   func NewCreateCommandSender(
       commandObjectSender cdb.CommandObjectSender,
       defaultVault string,
   ) CreateCommandSender {
       return &createCommandSender{
           commandObjectSender: commandObjectSender,
           defaultVault:        defaultVault,
       }
   }
   ```

   The godoc comment above the constructor (line 26) gains a sentence: `// The defaultVault is substituted into cmd.TargetVault at SendCommand time when cmd.TargetVault is empty; an invalid defaultVault surfaces as a validation error on the first SendCommand call.`

4. **Implement the substitution in `SendCommand`**

   In the same file, modify `SendCommand` so the substitution happens BEFORE the `cmd.Validate(ctx)` call. The sequence is:
   1. If `cmd.TargetVault == ""` AND `s.defaultVault != ""`, copy `defaultVault` into `cmd.TargetVault`.
   2. Call `cmd.Validate(ctx)` as today.
   3. Continue with `base.ParseEvent(ctx, cmd)` and the existing publish flow unchanged.

   The new function body (replace the existing `SendCommand` entirely, keeping every other line identical):

   ```go
   func (s *createCommandSender) SendCommand(ctx context.Context, cmd CreateCommand) error {
       if cmd.TargetVault == "" && s.defaultVault != "" {
           cmd.TargetVault = s.defaultVault
       }
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

   The substitution is a value-copy on a `cmd CreateCommand` parameter (passed by value, so mutation is local and safe). The `Validate` call re-validates the substituted `cmd.TargetVault` against the slug regex — this is what surfaces an invalid `defaultVault` at first publish time per AC 8. Do NOT add a separate `regexp.MatchString` check in the sender — reusing `cmd.Validate` is the single source of truth and matches the in-repo precedent where the sender delegates all schema rules to the struct.

5. **Update the existing test call site to the two-argument form**

   In `/workspace/lib/command/task/create-command-sender_test.go`, the `BeforeEach` (around line 26) constructs the sender with one argument. Change line 29 to pass `""` as the second argument (preserving pre-spec behavior — every existing test in the file leaves `TargetVault` empty on its input commands, so the empty default produces the same wire form as today):

   ```go
   sender = task.NewCreateCommandSender(fakeSender, "")
   ```

   This is the only test that needs the constructor updated. The three existing `It` blocks (validation-fails, validation-passes, publisher-returns-error) all keep working without further changes because their inputs have empty `TargetVault` and the empty default substitutes nothing.

6. **Add the four new matrix tests (AC 5, 6, 7, 8)**

   In the same `create-command-sender_test.go` file, add a new `Context("defaultVault substitution", ...)` block inside the existing top-level `Describe("CreateCommandSender", ...)` block (the one starting at line 19). The new block contains four `It` entries that exercise the four AC cases. Use the existing `fakeSender.SendCommandObjectArgsForCall(0)` pattern to capture the published `cdb.CommandObject` and decode the event payload back into a `task.CreateCommand` to assert on `TargetVault`.

   ```go
   Context("defaultVault substitution", func() {
       // Helper to read the embedded CreateCommand from the published CommandObject.
       publishedCmd := func() task.CreateCommand {
           _, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
           var got task.CreateCommand
           Expect(cmdObj.Command.Data.MarshalInto(ctx, &got)).To(Succeed())
           return got
       }

       It("defaultVault '' preserves input TargetVault (AC 5)", func() {
           fakeSender.SendCommandObjectReturns(nil)
           sender := task.NewCreateCommandSender(fakeSender, "")
           cmd := task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "My Task",
               Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
               TargetVault:    "openclaw",
           }
           Expect(sender.SendCommand(ctx, cmd)).To(Succeed())
           Expect(publishedCmd().TargetVault).To(Equal("openclaw"))
       })

       It("defaultVault 'personal' fills empty input (AC 6)", func() {
           fakeSender.SendCommandObjectReturns(nil)
           sender := task.NewCreateCommandSender(fakeSender, "personal")
           cmd := task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "My Task",
               Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
           }
           Expect(sender.SendCommand(ctx, cmd)).To(Succeed())
           Expect(publishedCmd().TargetVault).To(Equal("personal"))
       })

       It("defaultVault does not override explicit input (AC 7)", func() {
           fakeSender.SendCommandObjectReturns(nil)
           sender := task.NewCreateCommandSender(fakeSender, "personal")
           cmd := task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "My Task",
               Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
               TargetVault:    "openclaw",
           }
           Expect(sender.SendCommand(ctx, cmd)).To(Succeed())
           Expect(publishedCmd().TargetVault).To(Equal("openclaw"))
       })

       It("invalid defaultVault surfaces at first SendCommand (AC 8)", func() {
           fakeSender.SendCommandObjectReturns(nil)
           sender := task.NewCreateCommandSender(fakeSender, "Bad Vault")
           cmd := task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "My Task",
               Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
           }
           err := sender.SendCommand(ctx, cmd)
           Expect(err).To(HaveOccurred())
           Expect(err.Error()).To(ContainSubstring("validate CreateCommand"))
           Expect(err.Error()).To(ContainSubstring("TargetVault"))
           // Publisher must not be called.
           Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(0))
       })
   })
   ```

   The four tests use local `sender` variables (not the `BeforeEach`-assigned one) because each test needs a different `defaultVault`. The `publishedCmd` helper is a closure local to the `Context` block — it captures the `ctx` and `fakeSender` from the outer `BeforeEach` and reads the most recent call. The `Bad Vault` test (AC 8) demonstrates that construction itself does NOT fail — only the first publish fails.

   The test file already imports `cqrsmocks` for `cqrsmocks.CDBCommandObjectSender` (line 22). The `base.Event.MarshalInto` method is the standard cqrs way to decode the published payload back into a typed struct — if the linter flags the import as unused, add `"github.com/bborbe/cqrs/base"` to the import block. Verify with the file's existing import block before adding.

7. **Verify the counterfeiter mock is regenerated**

   After implementing requirements 2-6, the `CreateCommandSender` interface body is UNCHANGED — only the constructor's signature and the struct's fields changed. The `//counterfeiter:generate` annotation on the interface (line 18) is unchanged. Run `cd /workspace/lib && make generate` and confirm:
   - the generated file `/workspace/mocks/task-create-command-sender.go` is byte-identical to the pre-change version (its inputs are interface-only and the interface is unchanged)
   - if `make generate` reports any diff, that is a prompt bug — surface it in `## Improvements` and stop

   Do NOT regenerate manually with `counterfeiter` directly. Use `make generate` from the lib directory.

8. **Run `make precommit` in the lib service directory**

   From `/workspace/lib`:

   ```bash
   cd /workspace/lib && make precommit
   ```

   Must exit 0. The new four matrix tests pass; the three pre-existing tests (validation-fails, validation-passes, publisher-returns-error) all still pass because the empty-default case preserves the prior wire form.

</requirements>

<constraints>
- Do NOT change the `CreateCommandSender` interface body. Only the constructor signature and the struct's internal fields change. The interface is the public contract; its `SendCommand(ctx, cmd) error` method signature stays unchanged.
- Do NOT change the `//counterfeiter:generate` annotation on the interface. The mock file path and fake name are unchanged; running `make generate` produces an identical mock file.
- Do NOT add a separate regex check inside `SendCommand` to validate `defaultVault`. The slug check happens via `cmd.Validate(ctx)` on the substituted command — reusing the struct's `Validate` method is the single source of truth and matches the in-repo precedent in `createCommandSender.SendCommand` (line 38) which already calls `cmd.Validate(ctx)`.
- Do NOT validate `defaultVault` at construction time. The spec (Desired Behavior #4) requires the error to surface at first `SendCommand` call so a misconfigured deployment fails loudly on first publish, not at process start (which would be silent if the process never publishes).
- Do NOT add a CHANGELOG entry in this prompt. Changelog updates are the controller-side prompt 3's responsibility.
- Do NOT add new callers of `NewCreateCommandSender` in this prompt. The spec explicitly defers per-producer default-vault rollout to follow-up specs for individual producers (recurring-task-creator, maintainer). The only call site updated here is the existing test.
- Do NOT add a per-vault topic, per-vault URL, or per-vault config struct. The spec's Non-goal #1 forbids it.
- Do NOT add a config flag to disable the substitution. The substitution is the spec's design; an opt-out would defeat the controller-side routing.
- Do NOT commit — dark-factory handles git.
- All existing tests in `lib/command/task/...` must continue to pass after the change.
- Follow the project's `bborbe/errors` and `github.com/bborbe/validation` patterns (no `fmt.Errorf`, no `context.Background()` in business logic).
- Coverage for the new substitution branch must be ≥80% per the project's DoD — the four new `It` entries cover the four AC matrix cases plus the no-op empty-default case (which is exercised by the three pre-existing tests).
- Use the literal string `""` (empty string) for the `defaultVault` argument in the test `BeforeEach`. Do NOT introduce a new constant or helper for this.
- Use the closure-local `publishedCmd` helper for reading the published command — do NOT add an exported helper to the production package.
</constraints>

<verification>
```bash
cd /workspace/lib && go test ./command/task/... -v -run 'defaultVault'
```
Must show all four new `It` entries in the `defaultVault substitution` Context turning green.

```bash
cd /workspace/lib && go test ./command/task/... -v
```
Must pass with all existing tests in the package (the three pre-existing CreateCommandSender tests + the prompt 1 TargetVault tests) still green.

```bash
grep -rn "NewCreateCommandSender" /workspace/ --include="*.go"
```
Must return exactly three non-spec lines: the function definition in `create-command-sender.go`, the test call in `create-command-sender_test.go` line 29 (now with two arguments), and the new Context tests' local constructors. No other production code in `/workspace/` constructs this sender.

```bash
cd /workspace/lib && make precommit
```
Must exit 0.
</verification>

## DARK-FACTORY-REPORT
```yaml
status: success # or: failed, partial
summary: <one-paragraph description of what changed>
verification:
  command: "cd /workspace/lib && make precommit"
  exitCode: 0
improvements:
  - <category: PROMPT|GUIDE|GLOBAL>: <one-line suggestion>  # or omit if none
```
</content>
</invoke>