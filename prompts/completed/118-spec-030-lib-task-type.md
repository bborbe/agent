---
status: completed
spec: [030-executor-inject-task-type-env]
summary: Created lib.TaskType named type with validation, well-known constants, and TaskFrontmatter.TaskType() accessor; all tests pass and make precommit exits 0.
container: agent-118-spec-030-lib-task-type
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T12:10:00Z"
queued: "2026-05-14T12:14:15Z"
started: "2026-05-14T12:14:17Z"
completed: "2026-05-14T12:16:41Z"
branch: dark-factory/executor-inject-task-type-env
---

<summary>
- A new named type `lib.TaskType` (`type TaskType string`) is added to `lib/`, giving task types a first-class Go type parallel to `TaskIdentifier` and `TaskAssignee`
- The type has `String()`, `Bytes()`, and `Ptr()` methods identical in shape to other `lib` named types
- `TaskType.Validate(ctx)` enforces three rules: non-empty, matches `^[a-z0-9-]+$`, and max 63 characters — matching the existing CRD-side constraint
- Six well-known task-type constants are defined (`TaskTypeClaude`, `TaskTypePRReview`, `TaskTypeBacktest`, `TaskTypeHypothesis`, `TaskTypeTradeAnalysis`, `TaskTypeOAuthProbe`); `TaskTypeOAuthProbe` is marked deprecated
- A typed `TaskType() lib.TaskType` accessor is added to `TaskFrontmatter`, returning the `task_type` frontmatter field as the named type (or `TaskType("")` when absent or non-string)
- Unit tests cover all validation edge cases and all three frontmatter accessor variants (present, absent, non-string)
- `make precommit` passes in `lib/`
</summary>

<objective>
Create the `lib.TaskType` named type with validation and well-known constants, and add a typed `TaskType()` accessor to `lib.TaskFrontmatter`. This is the foundation consumed by prompt 2 which injects `TASK_TYPE` into spawned Jobs — once this ships, the executor can forward the verbatim frontmatter value without re-parsing.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct order, named types, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages (`package lib_test`), coverage ≥80%
- `go-doc-best-practices.md` in `~/.claude/plugins/marketplaces/coding/docs/` — GoDoc comment style for exported symbols
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**
- `lib/agent_task-identifier.go` — shape to mirror: `type TaskIdentifier string` with `String()`, `Bytes()`, `Ptr()`, `Validate(ctx)` methods
- `lib/agent_task-assignee.go` — another named type for comparison; simpler `Validate` (empty-only check)
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` map type; existing typed accessors (`Assignee()`, `Phase()`, `Stage()`) define the exact pattern to follow for `TaskType()`
- `lib/lib_suite_test.go` — existing Ginkgo suite file for `package lib_test`; do NOT add a second `TestLib` function

**Inline reference — `TaskIdentifier` Validate shape to mirror:**
```go
func (t TaskIdentifier) Validate(ctx context.Context) error {
	if t == "" {
		return errors.Wrap(ctx, validation.Error, "identifier missing")
	}
	return nil
}
```
`TaskType.Validate` adds two more checks after the empty check.

**Inline reference — exact accessor pattern to follow in `agent_task-frontmatter.go`:**
```go
// existing Assignee() accessor — TaskType() follows this exact pattern
func (f TaskFrontmatter) Assignee() TaskAssignee {
	v, _ := f["assignee"].(string)
	return TaskAssignee(v)
}
```
`TaskType()` is identical: `v, _ := f["task_type"].(string)` → `return TaskType(v)`.

**Symbol verification — bborbe/errors and bborbe/validation:**
These are already used throughout `lib/`. Find an existing usage to confirm import paths:
```bash
grep -rn "bborbe/errors\|bborbe/validation" lib/go.mod | head -3
grep -rn "errors.Wrap\|validation.Error" lib/agent_task-assignee.go | head -3
```
</context>

<requirements>

## 1. Create `lib/agent_task-type.go`

New file. License header required (copy from `lib/agent_task-identifier.go`). Package: `lib`.

**Imports needed:**
```go
import (
	"context"
	"regexp"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)
```

**Package-level regex (compiled once):**
```go
var taskTypeRegexp = regexp.MustCompile(`^[a-z0-9-]+$`)
```

**Named type with methods:**
```go
// TaskType identifies the category of work a task represents.
// Matched against the agent's declared task-type set before spawning a Job.
type TaskType string

func (t TaskType) String() string {
	return string(t)
}

func (t TaskType) Bytes() []byte {
	return []byte(t)
}

func (t TaskType) Ptr() *TaskType {
	return &t
}

// Validate returns an error when the task type is empty, contains characters
// outside [a-z0-9-], or exceeds 63 characters — matching the CRD-side constraint.
func (t TaskType) Validate(ctx context.Context) error {
	if t == "" {
		return errors.Wrap(ctx, validation.Error, "task type missing")
	}
	if len(t) > 63 {
		return errors.Wrap(ctx, validation.Error, "task type exceeds 63 characters")
	}
	if !taskTypeRegexp.MatchString(string(t)) {
		return errors.Wrap(ctx, validation.Error, "task type must match ^[a-z0-9-]+$")
	}
	return nil
}
```

**Well-known constants (add after the methods):**
```go
const (
	// TaskTypeClaude is the task type for Claude agent jobs.
	TaskTypeClaude TaskType = "claude"
	// TaskTypePRReview is the task type for PR review jobs.
	TaskTypePRReview TaskType = "pr-review"
	// TaskTypeBacktest is the task type for backtesting jobs.
	TaskTypeBacktest TaskType = "backtest"
	// TaskTypeHypothesis is the task type for hypothesis evaluation jobs.
	TaskTypeHypothesis TaskType = "hypothesis"
	// TaskTypeTradeAnalysis is the task type for trade analysis jobs.
	TaskTypeTradeAnalysis TaskType = "trade-analysis"
	// TaskTypeOAuthProbe is the task type for OAuth probe health-check jobs.
	//
	// Deprecated: use TaskTypeHealthcheck once introduced by the oauth-probe rename spec.
	TaskTypeOAuthProbe TaskType = "oauth-probe"
)
```

## 2. Add `TaskType()` accessor to `lib/agent_task-frontmatter.go`

Read the full file before editing. Add the following method immediately after the `Assignee()` method (to group it with the other typed frontmatter accessors):

```go
// TaskType returns the task_type frontmatter field as a typed TaskType.
// Returns TaskType("") when the field is absent or holds a non-string value.
func (f TaskFrontmatter) TaskType() TaskType {
	v, _ := f["task_type"].(string)
	return TaskType(v)
}
```

Verify the method was added:
```bash
grep -n "func.*TaskFrontmatter.*TaskType" lib/agent_task-frontmatter.go
```
Expected: one match.

## 3. Create `lib/agent_task-type_test.go`

New test file. Package: `lib_test`. License header required.

Do NOT add a new `TestLib` function — `lib/lib_suite_test.go` already defines it. This file only adds test cases.

**Test structure:**
```go
var _ = Describe("TaskType", func() {
	Describe("Validate", func() {
		DescribeTable("valid values",
			func(value lib.TaskType) {
				Expect(value.Validate(ctx)).To(Succeed())
			},
			Entry("claude constant", lib.TaskTypeClaude),
			Entry("pr-review constant", lib.TaskTypePRReview),
			Entry("backtest constant", lib.TaskTypeBacktest),
			Entry("hypothesis constant", lib.TaskTypeHypothesis),
			Entry("trade-analysis constant", lib.TaskTypeTradeAnalysis),
			Entry("oauth-probe constant", lib.TaskTypeOAuthProbe),
			Entry("63-character value", lib.TaskType("a23456789012345678901234567890123456789012345678901234567890abc")),
		)

		DescribeTable("invalid values",
			func(value lib.TaskType) {
				Expect(value.Validate(ctx)).NotTo(Succeed())
			},
			Entry("empty string", lib.TaskType("")),
			Entry("uppercase letter", lib.TaskType("MyType")),
			Entry("underscore", lib.TaskType("my_type")),
			Entry("64-character value", lib.TaskType("a234567890123456789012345678901234567890123456789012345678901234")),
		)
	})

	Describe("String", func() {
		It("returns the underlying string", func() {
			Expect(lib.TaskTypeClaude.String()).To(Equal("claude"))
		})
	})

	Describe("Bytes", func() {
		It("returns the underlying bytes", func() {
			Expect(lib.TaskTypeClaude.Bytes()).To(Equal([]byte("claude")))
		})
	})

	Describe("Ptr", func() {
		It("returns a non-nil pointer to the value", func() {
			tt := lib.TaskTypeClaude
			Expect(lib.TaskTypeClaude.Ptr()).To(Equal(&tt))
		})
	})
})
```

Add a `var ctx context.Context` in a `BeforeEach` at the top of the `Describe` block:
```go
var (
	ctx context.Context
)
BeforeEach(func() {
	ctx = context.Background()
})
```

**Imports needed:**
```go
import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
)
```

Verify test count after writing:
```bash
cd lib && go test ./... -v 2>&1 | grep -E "PASS|FAIL|It " | head -20
```

## 4. Append `Describe("TaskType")` sub-block to existing `Describe("TaskFrontmatter")` in `lib/agent_task_test.go`

**DO NOT create a new test file.** `lib/agent_task_test.go:76` already has `var _ = Describe("TaskFrontmatter", func() { ... })`. Adding a second top-level `Describe("TaskFrontmatter")` in a separate file collides on the Ginkgo tree name and splits the spec report.

Verify the existing block before editing:
```bash
grep -n 'Describe("TaskFrontmatter"' lib/agent_task_test.go
```
Expected: one match around line 76.

Read the full `lib/agent_task_test.go` file. Find the closing `})` of the existing `Describe("TaskFrontmatter", func() { ... })` block. Immediately before that closing brace, insert a new sub-block:

```go
	Describe("TaskType", func() {
		It("returns the task_type value as TaskType when the key is present and is a string", func() {
			f := lib.TaskFrontmatter{"task_type": "claude"}
			Expect(f.TaskType()).To(Equal(lib.TaskType("claude")))
		})

		It("returns TaskType(\"\") when the task_type key is absent", func() {
			f := lib.TaskFrontmatter{}
			Expect(f.TaskType()).To(Equal(lib.TaskType("")))
		})

		It("returns TaskType(\"\") when the task_type key holds a non-string value", func() {
			f := lib.TaskFrontmatter{"task_type": 42}
			Expect(f.TaskType()).To(Equal(lib.TaskType("")))
		})
	})
```

No new imports needed — `lib` is already imported in this file.

Verify the sub-block landed inside the existing Describe:
```bash
grep -n 'Describe("TaskFrontmatter"\|Describe("TaskType"' lib/agent_task_test.go
```
Expected: `Describe("TaskFrontmatter"` line followed by `Describe("TaskType"` line (Describe("TaskType") line number is greater).

## 5. Run iterative tests

```bash
cd lib && go test ./...
```

Fix compile errors before continuing. Common issues:
- `lib_test.go` redeclares `ctx` — make sure each `Describe` block uses its own `BeforeEach`-scoped vars
- `taskTypeRegexp` is package-private — ensure it is lowercase and in package `lib` (not `lib_test`)
- `validation.Error` — imported from `github.com/bborbe/validation`

Coverage check after tests pass:
```bash
cd lib && go test -coverprofile=/tmp/tasktype-cover.out ./... && go tool cover -func=/tmp/tasktype-cover.out | grep -E "agent_task-type|agent_task-frontmatter"
```
Expected: `agent_task-type.go` and `agent_task-frontmatter.go` both at ≥80% statement coverage.

## 6. Update `CHANGELOG.md` at repo root

Check for an existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header. Add:

```markdown
- feat(lib): add TaskType named type with validation, well-known constants, and TaskFrontmatter.TaskType() accessor (spec 030)
```

Verify:
```bash
grep -A 5 "^## Unreleased" CHANGELOG.md
```
Expected: the new bullet present.

## 7. Run final precommit in `lib/`

```bash
cd lib && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying.

</requirements>

<constraints>
- `type TaskType string` lives in `lib/agent_task-type.go`. No other file in `lib/` is created.
- The only change to `lib/agent_task-frontmatter.go` is the addition of the `TaskType()` method. No other methods are changed or added.
- `Validate` checks three things in this order: (1) empty string → error, (2) length > 63 → error, (3) regex mismatch → error. Returns nil for valid values.
- `taskTypeRegexp` is a package-level `var` compiled once with `regexp.MustCompile`. It is NOT compiled inside `Validate` on every call.
- Well-known constants are defined as untyped `const` block with `TaskType = "..."` values; they carry GoDoc comments.
- `TaskTypeOAuthProbe` carries the exact GoDoc: `// Deprecated: use TaskTypeHealthcheck once introduced by the oauth-probe rename spec.`
- The `TaskType()` accessor follows the EXACT same pattern as `Assignee()`: a two-value type assertion `v, _ := f["task_type"].(string)`, returning `TaskType(v)`. Non-string values silently yield `TaskType("")` via the blank identifier.
- Test files are in `package lib_test`. No test file adds a `TestLib` function — `lib_suite_test.go` owns it.
- No mock generation is needed — `TaskType` is a named type, not an interface.
- No changes to `task/executor/`, `agent/`, `prompt/`, or any other directory outside `lib/` and root `CHANGELOG.md`.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify new file exists:
```bash
ls lib/agent_task-type.go
```
Expected: file present.

Verify type definition and methods:
```bash
grep -n "type TaskType string\|func.*TaskType.*String\|func.*TaskType.*Bytes\|func.*TaskType.*Ptr\|func.*TaskType.*Validate" lib/agent_task-type.go
```
Expected: 5 lines (type + 4 methods).

Verify all 6 constants defined:
```bash
grep -n "TaskTypeClaude\|TaskTypePRReview\|TaskTypeBacktest\|TaskTypeHypothesis\|TaskTypeTradeAnalysis\|TaskTypeOAuthProbe" lib/agent_task-type.go
```
Expected: 6 constant definitions.

Verify deprecated GoDoc on TaskTypeOAuthProbe:
```bash
grep -B 2 "TaskTypeOAuthProbe" lib/agent_task-type.go | grep -i "deprecated"
```
Expected: one match containing "Deprecated".

Verify regex is package-level (not inside Validate):
```bash
grep -n "taskTypeRegexp" lib/agent_task-type.go
```
Expected: two lines — `var taskTypeRegexp = regexp.MustCompile(...)` and one `MatchString` call inside `Validate`.

Verify accessor was added to frontmatter file:
```bash
grep -n "func.*TaskFrontmatter.*TaskType" lib/agent_task-frontmatter.go
```
Expected: one match.

Run all lib tests:
```bash
cd lib && go test ./...
```
Expected: exit 0, all specs pass.

Run coverage:
```bash
cd lib && go test -coverprofile=/tmp/tasktype-cover.out ./... && go tool cover -func=/tmp/tasktype-cover.out | grep -E "agent_task-type|agent_task-frontmatter"
```
Expected: both files at ≥80% coverage.

Run precommit:
```bash
cd lib && make precommit
```
Expected: exit 0.

</verification>
