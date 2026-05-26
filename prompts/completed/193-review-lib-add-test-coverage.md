---
status: completed
summary: Added 11 new Ginkgo test files covering agent_markdown.go, agent_parser.go, agent_phase.go, agent_runner.go, agent_schema.go, agent_task-assignee.go, agent_task-identifier.go, agent_status.go, agent_print-result.go, agent_task-content.go, and agent_task-frontmatter.go, raising lib/ coverage from 59.2% to 91.8%.
container: agent-exec-193-review-lib-add-test-coverage
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-26T15:21:34Z"
started: "2026-05-26T15:21:35Z"
completed: "2026-05-26T15:27:01Z"
---

<summary>
- lib/ root package is currently at 59.2% coverage; target ≥80%
- 4 test files already exist: agent_agent_test.go, agent_agent-provider_test.go, agent_task_test.go, agent_task-type_test.go — do NOT recreate or overwrite these
- Missing tests for: agent_markdown.go, agent_parser.go, agent_phase.go, agent_runner.go, agent_schema.go, agent_task-assignee.go, agent_task-identifier.go, agent_status.go, agent_step.go, agent_print-result.go, agent_task-content.go, agent_task-frontmatter.go, agent_result-deliverer.go, agent_cdb-schema.go
- Add Ginkgo/Gomega tests in package `lib_test` using Counterfeiter mocks for ResultDeliverer and Step
</summary>

<objective>
Raise root-package test coverage in lib/ from 59.2% to ≥80% by adding test files for the currently-untested source files. Do not overwrite existing tests. Coverage measured by `go test -cover .` from the lib/ directory.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Existing test pattern reference (read these to mirror the style):
- `lib/agent_agent_test.go` — package `lib_test`, Counterfeiter mocks from `lib/mocks`, Ginkgo v2 / Gomega
- `lib/agent_task_test.go` — validation/string-method coverage style
- `lib/lib_suite_test.go` — RunSpecs entry point (do NOT touch)

Source files needing tests (read each before writing its test):
- `lib/agent_markdown.go` — Marshal, ParseMarkdown, FindSection, AddSection, ReplaceSection, InsertSection, splitMarkdownFrontmatter
- `lib/agent_parser.go` — ParseStep[T] generic type, Run, ShouldRun
- `lib/agent_phase.go` — NewPhase constructor
- `lib/agent_runner.go` — StepRunner.Run, shouldExitStepRunner (lines around 38 and 94)
- `lib/agent_schema.go` — ExtractSection, ExtractSectionMap, MarshalSectionTyped
- `lib/agent_task-assignee.go` — String, Validate
- `lib/agent_task-identifier.go` — String, Bytes, Contains, Ptr, Equal, Validate
- `lib/agent_status.go` — enum/string methods
- `lib/agent_step.go` — Step interface helpers
- `lib/agent_print-result.go` — PrintResult function
- `lib/agent_task-content.go` — content helpers
- `lib/agent_task-frontmatter.go` — frontmatter map helpers
- `lib/agent_result-deliverer.go` — interface definition only (skip — interface alone has nothing to test)
- `lib/agent_cdb-schema.go` — schema helpers

Available mocks (already generated in lib/mocks/):
- `mocks.AgentStep` (for `Step` interface)
- `mocks.AgentResultDeliverer` (for `ResultDeliverer`)
- `mocks.AgentAIParser` (for `AIParser` — used by `ParseStep[T]` tests)
- Check `lib/mocks/` for the full list before adding tests that need a mock; if a needed mock is missing, ensure the source interface has the `//counterfeiter:generate` directive and run `make generate`.

Current per-file coverage (key gaps):
- agent_markdown.go: many funcs at 0% — ParseMarkdown (line 62), FindSection (line 75), AddSection (line 81), ReplaceSection (line 93)
- agent_parser.go: all 4 functions at 0%
- agent_schema.go: all 3 functions at 0%
- agent_task-assignee.go: 0% on line 18
- agent_print-result.go: 0%
</context>

<requirements>

1. **Run baseline coverage** to confirm starting point:
   ```bash
   cd lib && go test -coverprofile=/tmp/before.out . && go tool cover -func=/tmp/before.out | tail -1
   ```
   Record the number.

2. **Add `lib/agent_markdown_test.go`** (package `lib_test`):
   - `Marshal`: success path with frontmatter + multiple sections; success with no frontmatter (skip the yaml-error path — `TaskFrontmatter` is `map[string]any` and forcing yaml.Marshal to error is impractical)
   - `ParseMarkdown`: valid frontmatter+body, missing frontmatter (no `---`), malformed yaml
   - `FindSection`: section present, section absent
   - `AddSection`: appends to end
   - `ReplaceSection`: existing section replaced, no-op when section absent
   - `InsertSection`: inserts before given heading
   - `splitMarkdownFrontmatter`: empty input, no frontmatter, missing closing `---`

3. **Add `lib/agent_runner_test.go`** (package `lib_test`):
   - `StepRunner.Run`: step succeeds → deliverer called
   - `StepRunner.Run`: step returns error → wrapped and returned
   - `StepRunner.Run`: `ctx.Done()` triggers early exit
   - `shouldExitStepRunner`: various step-result cases
   - Use `mocks.AgentStep` and `mocks.AgentResultDeliverer`

4. **Add `lib/agent_parser_test.go`** (package `lib_test`):
   - `ParseStep[T].Run` happy path with a concrete T (use a simple test struct)
   - `ParseStep[T].Run` when `AIParser.Parse` errors
   - `ParseStep[T].ShouldRun` true/false branches

5. **Add `lib/agent_phase_test.go`** (package `lib_test`):
   - `NewPhase` with name + step → returned phase has correct fields

6. **Add `lib/agent_schema_test.go`** (package `lib_test`):
   - `ExtractSection`: section found, section missing
   - `ExtractSectionMap`: multiple sections
   - `MarshalSectionTyped`: success + JSON marshal error path (use unmarshalable type like `func()` to force error)

7. **Add `lib/agent_task-assignee_test.go`** (package `lib_test`):
   - `String`, `Validate` for valid/empty/invalid inputs

8. **Add `lib/agent_task-identifier_test.go`** (package `lib_test`):
   - `String`, `Bytes`, `Contains`, `Ptr`, `Equal`, `Validate`
   - Cover both valid UUIDs and empty/invalid strings

9. **Add `lib/agent_status_test.go`**, `lib/agent_step_test.go`, `lib/agent_print-result_test.go`, `lib/agent_task-content_test.go`, `lib/agent_task-frontmatter_test.go`, `lib/agent_cdb-schema_test.go`:
   - One test file per source file, covering exported functions
   - Skip if the source file contains only interface definitions or generated code

10. **Verify coverage** after adding tests:
    ```bash
    cd lib && go test -coverprofile=/tmp/after.out . && go tool cover -func=/tmp/after.out | tail -1
    ```
    Must be ≥80% on the root package (`github.com/bborbe/agent/lib`). If still below 80%, identify remaining 0% lines via `go tool cover -func=/tmp/after.out | grep "0.0%"` and add tests until the threshold is met.

11. **Run precommit**:
    ```bash
    cd lib && make precommit
    ```
    Must exit 0.

</requirements>

<constraints>
- Only ADD new `*_test.go` files in `lib/` — never modify existing tests or non-test source files
- Existing tests must still pass — do not change `agent_agent_test.go`, `agent_agent-provider_test.go`, `agent_task_test.go`, `agent_task-type_test.go`, `lib_suite_test.go`
- All new tests: package `lib_test` (external), Ginkgo v2 + Gomega
- Use Counterfeiter mocks from `lib/mocks` — never hand-rolled mocks
- Use `errors.Is`/`errors.As` from std lib for assertions; production code uses `github.com/bborbe/errors` for wrapping
- Do NOT commit — dark-factory handles git
- Coverage target: ≥80% on `github.com/bborbe/agent/lib`
</constraints>

<verification>
cd lib && make precommit
cd lib && go test -coverprofile=/tmp/cov.out . && go tool cover -func=/tmp/cov.out | tail -1
</verification>
