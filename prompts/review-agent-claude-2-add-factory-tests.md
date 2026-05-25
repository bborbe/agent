---
status: draft
created: "2026-05-24T11:10:00Z"
queued: "2026-05-25T22:23:08Z"
---

<summary>
- BuildInstructions in pkg/prompts/prompts.go has 0% test coverage — exported function returning observable data should be tested
- CreateKafkaResultDeliverer and CreateFileResultDeliverer have 0% coverage — smoke tests asserting non-nil return missing
- CreateAgent has 0% coverage — smoke test asserting non-nil *agentlib.Agent return missing
- factory_suite_test.go lacks GinkgoConfiguration() and 60s timeout, inconsistent with other suites in the project
</summary>

<objective>
Add missing tests for BuildInstructions, CreateKafkaResultDeliverer, CreateFileResultDeliverer, and CreateAgent in agent/claude. Add proper GinkgoConfiguration to factory_suite_test.go. Coverage target ≥80% for new code.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` for Ginkgo/Gomega patterns.

Files to read before making changes:
- agent/claude/pkg/prompts/prompts.go — BuildInstructions at line 21-26
- agent/claude/pkg/factory/factory.go — CreateKafkaResultDeliverer at line 63, CreateFileResultDeliverer at line 83, CreateAgent at line 94
- agent/claude/pkg/factory/factory_test.go — existing test patterns
- agent/claude/pkg/factory/factory_suite_test.go — current suite setup
- agent/claude/main_test.go — reference for correct GinkgoConfiguration pattern
</context>

<requirements>

## 1. Add test for BuildInstructions

Create `agent/claude/pkg/prompts/prompts_suite_test.go` following the project pattern (external test package, GinkgoConfiguration with 60s timeout).

Then create `agent/claude/pkg/prompts/prompts_test.go`:

```go
package prompts_test

import (
	"testing"

	"github.com/bborbe/agent/agent/claude/pkg/prompts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BuildInstructions", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is workflow", func() {
		instrs := prompts.BuildInstructions()
		Expect(instrs[0].Name).To(Equal("workflow"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("second instruction is output-format", func() {
		instrs := prompts.BuildInstructions()
		Expect(instrs[1].Name).To(Equal("output-format"))
		Expect(instrs[1].Content).NotTo(BeEmpty())
	})
})
```

Run tests:
```bash
cd agent/claude && go test ./pkg/prompts/... -v 2>&1 | grep -E "PASS|FAIL|BuildInstructions"
```

## 2. Add smoke tests for CreateKafkaResultDeliverer and CreateFileResultDeliverer

Read `agent/claude/pkg/factory/factory_test.go` before editing.

Add to the existing factory test suite:

```go
var _ = Describe("CreateKafkaResultDeliverer", func() {
	It("returns a non-nil ResultDeliverer", func() {
		// Use minimal valid inputs
		deliverer := factory.CreateKafkaResultDeliverer(
			nil, // syncProducer - nil is acceptable for smoke test
			"",  // branch
			"",  // taskID
			"",  // originalContent
			nil, // currentDateTime
		)
		Expect(deliverer).NotTo(BeNil())
	})
})
```

```go
var _ = Describe("CreateFileResultDeliverer", func() {
	It("returns a non-nil ResultDeliverer", func() {
		deliverer := factory.CreateFileResultDeliverer("/tmp/test-output.md")
		Expect(deliverer).NotTo(BeNil())
	})
})
```

## 3. Add smoke test for CreateAgent

Add to factory_test.go:

```go
var _ = Describe("CreateAgent", func() {
	It("returns a non-nil *agentlib.Agent", func() {
		agent := factory.CreateAgent(
			"", // claudeConfigDir
			"", // agentDir
			nil, // allowedTools
			"", // model
			nil, // claudeEnv
			nil, // envContext
		)
		Expect(agent).NotTo(BeNil())
	})
})
```

## 4. Fix factory_suite_test.go GinkgoConfiguration

Read `agent/claude/pkg/factory/factory_suite_test.go` and `agent/claude/main_test.go` (as reference).

Update factory_suite_test.go to match the standard pattern:

Before:
```go
func TestFactory(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Factory Suite")
}
```

After:
```go
//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate
func TestSuite(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    suiteConfig, reporterConfig := GinkgoConfiguration()
    suiteConfig.Timeout = 60 * time.Second
    RunSpecs(t, "Factory Suite", suiteConfig, reporterConfig)
}
```

Note: Also rename the function from `TestFactory` to `TestSuite` to match the project standard.

## 5. Add //go:generate to main_test.go files

Read `agent/claude/main_test.go` and `agent/claude/cmd/run-task/main_test.go`.

Add the generate directive to both (if not already present):

```go
//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate
func TestSuite(t *testing.T) {
```

Check first — the agent said these files already have full suite setup. Verify if the directive is present:
```bash
grep -n "//go:generate.*counterfeiter" agent/claude/main_test.go agent/claude/cmd/run-task/main_test.go
```

If missing, add it above the TestSuite function.

## 6. Run make test then make precommit

```bash
cd agent/claude && make test
```
Expected: exit 0.

```bash
cd agent/claude && make precommit
```
Expected: exit 0.

</requirements>

<constraints>
- Only change files in `agent/claude/`
- Do NOT commit — dark-factory handles git
- All new test files must use external test package (`package ..._test`)
- Follow Ginkgo/Gomega patterns from existing test files in the project
- Coverage target ≥80% for new code
</constraints>

<verification>
cd agent/claude && make precommit
</verification>
