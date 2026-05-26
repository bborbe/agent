---
status: completed
summary: Added comprehensive Ginkgo v2 test suite for agent/gemini/pkg/steps covering ExecuteStep, VerifyStep, compute, and needsInput — 95.7% statement coverage
container: agent-exec-178-review-agent-gemini-add-steps-tests
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-26T00:00:58Z"
completed: "2026-05-26T00:05:22Z"
---

<summary>
- Steps package has 0% test coverage despite containing business-critical logic
- `ExecuteStep.Run` and `VerifyStep.Run` determine task outcomes (done vs human_review)
- The `compute` helper has 4 operation branches and an error path — all untested
- After this change, steps package achieves ≥80% statement coverage
</summary>

<objective>
Add comprehensive tests for the steps package covering all run branches and compute operations.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- agent/gemini/pkg/steps/steps.go (full file, all functions)
- agent/gemini/pkg/steps/steps_test.go (existing test file if any)
</context>

<requirements>
1. **Create `agent/gemini/pkg/steps/steps_test.go`** if it doesn't exist

   Use Ginkgo/Gomega with external test package `steps_test`.

   Suite setup:
   ```go
   package steps_test

   import (
       "testing"
       "time"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
       "github.com/onsi/gomega/format"
   )

   func TestSteps(t *testing.T) {
       time.Local = time.UTC
       format.TruncatedDiff = false
       RegisterFailHandler(Fail)
       RunSpecs(t, "Steps Suite")
   }
   ```

2. **Test `ExecuteStep`** - add these cases:

   - `Name()` → returns "execute"
   - `ShouldRun()` with "## Result" absent → returns true
   - `ShouldRun()` with "## Result" present → returns false
   - `Run()` with "add" operation → `result.Value == a+b`
   - `Run()` with "sub" operation → `result.Value == a-b`
   - `Run()` with "mul" operation → `result.Value == a*b`
   - `Run()` with unknown operation → returns `needsInput` error
   - `Run()` with marshal failure → returns wrapped error

   Build test markdown using `agentlib.ParseMarkdown`:
   ```go
   markdown := agentlib.ParseMarkdown("## Plan\noperation: add\na: 2\nb: 3")
   step := steps.NewExecuteStep(markdown)
   result, err := step.Run(ctx)
   ```

3. **Test `VerifyStep`** - add these cases:

   - `Name()` → returns "verify"
   - `ShouldRun()` → always returns true
   - `Run()` with pass verdict → `NextPhase == "done"`
   - `Run()` with fail verdict → `NextPhase == "human_review"`, `Message` contains expected/got
   - `Run()` with extract plan failure → returns `needsInput` error
   - `Run()` with extract result failure → returns `needsInput` error
   - `Run()` with compute failure → returns `needsInput` error

4. **Test `compute` helper** directly - add these cases:

   - `compute("add", 2, 3)` → returns 5, nil
   - `compute("sub", 5, 3)` → returns 2, nil
   - `compute("mul", 4, 7)` → returns 28, nil
   - `compute("div", 1, 2)` → returns error "unknown operation"
   - Edge cases: negative numbers, zero

5. **Test `needsInput` helper** - add these cases:

   - Returns `AgentStatusNeedsInput` in result
   - Returns nil error (signals non-error status via Result, not error)

6. **Create test markdown helpers**:

   ```go
   func makePlanMarkdown(op string, a, b int) *agentlib.Markdown {
       content := fmt.Sprintf("## Plan\noperation: %s\na: %d\nb: %d", op, a, b)
       return agentlib.ParseMarkdown(content)
   }

   func makeVerifyMarkdown(op string, a, b, resultValue int) *agentlib.Markdown {
       content := fmt.Sprintf("## Plan\noperation: %s\na: %d\nb: %d\n\n## Result\nvalue: %d", op, a, b, resultValue)
       return agentlib.ParseMarkdown(content)
   }
   ```

7. **Run tests with coverage**
   ```bash
   cd agent/gemini && go test -v -coverprofile=/tmp/steps-cover.out -mod=vendor ./pkg/steps/...
   go tool cover -func=/tmp/steps-cover.out | grep "total:"
   ```

8. **Coverage must be ≥80%**

   If coverage is below 80%, add more test cases.

9. **Run precommit**
   ```bash
   cd agent/gemini && make precommit
   ```
</requirements>

<constraints>
- Only change files in `agent/gemini/`
- Do NOT commit — dark-factory handles git
- Follow project conventions in `CLAUDE.md` and `docs/` — Ginkgo/Gomega, external test package
- No mocks needed — steps.go uses only concrete `agentlib.Markdown` type
- Coverage target: ≥80% statement coverage
- Test all error paths
</constraints>

<verification>
cd agent/gemini && make precommit
</verification>
