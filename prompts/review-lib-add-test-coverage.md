---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Add agent_agent_test.go covering Agent.Run phase dispatch, validate, findPhase, unsupportedPhase
- Add agent_markdown_test.go covering Marshal, ParseMarkdown, FindSection, AddSection, ReplaceSection, InsertSection
- Add agent_runner_test.go covering StepRunner.Run and shouldExitStepRunner
- Add agent_parser_test.go covering ParseStep[T] Run and ShouldRun
- Add agent_schema_test.go covering ExtractSection, ExtractSectionMap, MarshalSectionTyped
- Add agent_phase_test.go covering NewPhase
- Add agent_task_test.go covering TaskIdentifier and TaskAssignee validation
</summary>

<objective>
Increase test coverage in lib/ root package from 24.7% to above 80%. The root package has no test files despite containing core agent types (Agent, StepRunner, Phase, markdown utilities, schema extraction). After this change, all exported functions in the root package have tests covering normal and error paths.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` for Ginkgo/Gomega patterns.

Files to read before making changes (read ALL first):
- lib/agent_agent.go
- lib/agent_markdown.go
- lib/agent_runner.go
- lib/agent_parser.go
- lib/agent_schema.go
- lib/agent_phase.go
- lib/agent_task-assignee.go
- lib/agent_task-identifier.go
- lib/agent_task-type.go
- lib/claude/task-runner_test.go (for reference on Ginkgo/Gomega patterns used in this repo)
</context>

<requirements>
1. Create `lib/agent_agent_test.go`:
   - Test `Agent.Run` with valid phase transitions (`planning`, `in_progress`, `ai_review`)
   - Test `Agent.Run` with unsupported phase → returns error result with `unsupportedPhase`
   - Test validate failure path
   - Test `findPhase` when phase exists and when it doesn't
   - Use Counterfeiter mocks for `ResultDeliverer`

2. Create `lib/agent_markdown_test.go`:
   - Test `Marshal`: `yaml.Marshal` error path
   - Test `ParseMarkdown` with valid/invalid frontmatter
   - Test `FindSection`, `AddSection`, `ReplaceSection`, `InsertSection`
   - Test `splitMarkdownFrontmatter` edge cases

3. Create `lib/agent_runner_test.go`:
   - Test `StepRunner.Run`: normal step execution
   - Test `StepRunner.Run`: `ctx.Done()` triggers early exit
   - Test `shouldExitStepRunner` with various step results
   - Use Counterfeiter mocks for `Step` and `ResultDeliverer`

4. Create `lib/agent_parser_test.go`:
   - Test `ParseStep[T].Run` with `AIParser.Parse` error path
   - Test `ParseStep[T].ShouldRun`

5. Create `lib/agent_schema_test.go`:
   - Test `ExtractSection` and `ExtractSectionMap`
   - Test `MarshalSectionTyped` with `json.MarshalIndent` error

6. Create `lib/agent_phase_test.go`:
   - Test `NewPhase` with valid name and step

7. Create `lib/agent_task_test.go` (or split into `agent_task-identifier_test.go`, `agent_task-assignee_test.go`):
   - Test `TaskIdentifier` `String`, `Bytes`, `Contains`, `Ptr`, `Equal`, `Validate`
   - Test `TaskAssignee` `String`, `Validate`
   - Test validation edge cases

All tests: use Ginkgo/Gomega, Counterfeiter mocks (not manual mocks), external test package (`_test` suffix). Coverage target: ≥80% on new code.
</requirements>

<constraints>
- Only change files in `lib/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/` — error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`), context propagation, factory pattern, time injection
- Tests must use Ginkgo v2 / Gomega with Counterfeiter mocks
- External test packages (package name ending in _test)
</constraints>

<verification>
cd lib && make precommit
</verification>
