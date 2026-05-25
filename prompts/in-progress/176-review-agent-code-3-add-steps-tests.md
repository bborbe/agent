---
status: approved
created: "2026-05-24T12:00:00Z"
queued: "2026-05-25T22:23:09Z"
---

<summary>
- Adds Ginkgo v2 test suite for `pkg/steps` covering all 13 exported functions
- Tests all three step types: PlanStep, ExecuteStep, VerifyStep
- Tests the `compute` helper function with all four operations (add, sub, mul, unknown)
- Tests error paths including needsInput results and marshal failures
- No mocks needed — all dependencies are concrete types from agentlib
</summary>

<objective>
Achieve ≥80% test coverage on `pkg/steps`. The package contains pure business logic (13 exported functions) with 0% current coverage. Tests use real `agentlib.Markdown` constructed from test markdown strings — no mock infrastructure needed.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- agent/code/pkg/steps/steps.go — all step implementations (read fully)
- agent/code/pkg/factory/factory_test.go — Ginkgo v2 test pattern reference
- agent/code/pkg/factory/factory_suite_test.go — Ginkgo suite setup reference
</context>

<requirements>

## 1. Create test suite files

Create `agent/code/pkg/steps/steps_suite_test.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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

## 2. Create main test file

Create `agent/code/pkg/steps/steps_test.go`. Use `agentlib.Markdown` to construct test documents — no mocks needed since all dependencies are concrete internal types.

Test file must cover:

### compute function (table-driven)
| Input | Expected |
|-------|----------|
| `("add", 2, 3)` | `5, nil` |
| `("sub", 10, 4)` | `6, nil` |
| `("mul", 3, 7)` | `21, nil` |
| `("div", 6, 2)` | `0, error containing "unknown operation"` |

### PlanStep
- `Name()` returns `"plan"`
- `ShouldRun()` returns `(true, nil)` when no ## Plan section exists
- `ShouldRun()` returns `(false, nil)` when ## Plan exists
- `Run()` success: frontmatter with operation, a, b → writes ## Plan section, returns AgentStatusDone + NextPhase "in_progress"
- `Run()` error: missing 'operation' → needsInput result
- `Run()` error: missing 'a' or 'b' → needsInput result
- `Run()` error: marshal failure (use a type that can't marshal — or test the error path via struct tag)

### ExecuteStep
- `Name()` returns `"execute"`
- `ShouldRun()` returns `(true, nil)` when no ## Result section exists
- `ShouldRun()` returns `(false, nil)` when ## Result exists
- `Run()` success: reads ## Plan (add, 2, 3), writes ## Result with value 5, returns AgentStatusDone + NextPhase "ai_review"
- `Run()` error: missing ## Plan section → needsInput result
- `Run()` error: unknown operation in ## Plan → needsInput result
- `Run()` error: marshal failure

### VerifyStep
- `Name()` returns `"verify"`
- `ShouldRun()` always returns `(true, nil)` — no skip for final phase
- `Run()` pass: ## Plan (add, 2, 3), ## Result (value: 5) → Verdict "pass", NextPhase "done"
- `Run()` fail: ## Plan (add, 2, 3), ## Result (value: 99) → Verdict "fail", NextPhase "human_review", Message with expected/got
- `Run()` error: missing ## Plan → needsInput result
- `Run()` error: missing ## Result → needsInput result
- `Run()` error: compute fails on ## Plan → needsInput result

Use `agentlib.ParseMarkdown` to construct test documents with frontmatter and sections.

## 3. Run tests to verify coverage

```bash
cd agent/code && go test ./pkg/steps/... -coverprofile=/tmp/cover.out -v 2>&1 | tail -30
go tool cover -func=/tmp/cover.out
```

Target: ≥80% statement coverage.

## 4. Run make test then make precommit

```bash
cd agent/code && make test
```
Expected: exit 0.

```bash
cd agent/code && make precommit
```
Expected: exit 0.

</requirements>

<constraints>
- Only change files in `agent/code/`
- Do NOT commit — dark-factory handles git
- Tests must use Ginkgo v2 / Gomega (same as existing factory tests)
- No external mocks needed — use real `agentlib.Markdown` constructed from test strings
- Follow project conventions: counterfeiter annotations not needed for this package since there are no injected interfaces
</constraints>

<verification>
cd agent/code && make precommit
</verification>
