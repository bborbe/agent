---
status: approved
spec: [038-rename-task-status-phase-taxonomy]
created: "2026-05-20T17:00:00Z"
queued: "2026-05-20T17:19:53Z"
branch: dark-factory/rename-task-status-phase-taxonomy
---

<summary>
- Test assertions using "todo" as expected task status in task/controller are updated to "next" (new canonical)
- Test frontmatter inputs with "status": "todo" in task/controller command tests are updated to "status": "next"
- Test frontmatter inputs with "phase": "in_progress" in task/controller result_writer_test.go are updated to "phase": "execution"
- "status": "in_progress" fields are NOT changed — TaskStatusInProgress is still the canonical value for that status
- At least one test in task/controller/pkg/scanner/ explicitly calls domain.NormalizeTaskPhase("in_progress") and asserts it equals domain.TaskPhaseExecution
- At least one test in task/controller/pkg/command/ explicitly calls domain.NormalizeTaskPhase("in_progress") and asserts it equals domain.TaskPhaseExecution
- At least one test calls domain.NormalizeTaskStatus("todo") and asserts it equals domain.TaskStatusNext
- make precommit exits 0 in task/controller
- All spec 038 acceptance criteria are satisfied and verified
</summary>

<objective>
Update string literal test assertions and inputs in task/controller that reference old canonical values ("todo" for status, "in_progress" for phase) to the new canonical values ("next", "execution"), and add alias roundtrip tests proving the normalize functions accept the legacy values. This is the final step of spec 038 — after this prompt all 6 modules pass make precommit and all acceptance criteria are met.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `go-enum-type-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/` — enum pattern, Available* lists, normalize functions
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — entry format, `## Unreleased` rules
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write

Read these project docs before editing:
- `docs/task-flow-and-failure-semantics.md` — phase and status lifecycle

**Pre-condition:** Prompt `1-spec-038-dep-bump` has already run and passed. vault-cli > v0.64.1 is in go.mod for all 6 modules.

**Critical distinction — what changes vs what does NOT:**
| Field | Old value | New value | Change? |
|-------|-----------|-----------|---------|
| `"status": "todo"` | TaskStatusTodo | TaskStatusNext | YES → `"next"` |
| `"status": "in_progress"` | TaskStatusInProgress | TaskStatusInProgress | NO — unchanged |
| `"phase": "in_progress"` | TaskPhaseInProgress | TaskPhaseExecution | YES → `"execution"` |

`"status": "in_progress"` appears extensively in result_writer_test.go and task_frontmatter_sequence_test.go — do NOT change these lines.

**Grep to verify pre-condition before starting:**
```bash
grep "vault-cli" task/controller/go.mod
```
Expected: version > v0.64.1. If this shows v0.64.1, prompt 1 did not complete — stop and investigate.

**Files to read in full before editing (all in task/controller/pkg/):**
- `scanner/vault_scanner_test.go` — find `Equal("todo")` assertion for status (~line 370)
- `command/task_create_task_executor_test.go` — many `"status": "todo"` input fields
- `command/task_frontmatter_sequence_test.go` — has `"status": "in_progress"` (do NOT change)
- `command/task_update_frontmatter_executor_test.go` — has `"status": "in_progress"` (do NOT change)
- `result/result_writer_test.go` — `"phase": "in_progress"` at ~lines 670, 1091; `"status": "in_progress"` at many lines (do NOT change the status ones); >1000 lines, use chunked reads
</context>

<requirements>

## 1. Update vault_scanner_test.go — status "todo" assertions

Read `task/controller/pkg/scanner/vault_scanner_test.go` before editing.

Locate all `"todo"` occurrences that are task STATUS assertions:
```bash
grep -n '"todo"' task/controller/pkg/scanner/vault_scanner_test.go
```

For each `Equal("todo")` assertion that tests a task STATUS field, change to `Equal("next")`:

Before:
```go
Expect(result["status"]).To(Equal("todo"))
```

After:
```go
Expect(result["status"]).To(Equal("next"))
```

Do NOT change:
- Comments
- Occurrences inside the alias roundtrip test added in step 4 (which explicitly passes "todo" to NormalizeTaskStatus)
- Any occurrence that is not a status field assertion

Verify no remaining status "todo" assertions:
```bash
grep -n 'Equal("todo")' task/controller/pkg/scanner/vault_scanner_test.go
```
Expected: 0 lines (alias test added in step 4 uses NormalizeTaskStatus, not Equal("todo")).

Build check:
```bash
cd task/controller && go build ./pkg/scanner/...
```
Expected: exit 0.

Run iterative tests:
```bash
cd task/controller && go test ./pkg/scanner/... -v 2>&1 | grep -E "PASS|FAIL" | tail -20
```
Expected: exit 0.

## 2. Update task_create_task_executor_test.go — status "todo" inputs

Read `task/controller/pkg/command/task_create_task_executor_test.go` before editing.

Count how many "todo" status inputs there are:
```bash
grep -c '"todo"' task/controller/pkg/command/task_create_task_executor_test.go
```

Replace ALL occurrences of `"status":` fields set to `"todo"` with `"next"`:

Before (any spacing variant):
```go
"status":   "todo",
```
```go
"status": "todo",
```

After:
```go
"status":   "next",
```
```go
"status": "next",
```

Verify replacement was complete:
```bash
grep -n '"todo"' task/controller/pkg/command/task_create_task_executor_test.go
```
Expected: 0 remaining lines (alias test in step 4 may reference "todo" in NormalizeTaskStatus call only).

Build and test:
```bash
cd task/controller && go test ./pkg/command/... -v 2>&1 | grep -E "PASS|FAIL" | tail -20
```
Expected: exit 0.

## 3. Update result_writer_test.go — phase "in_progress" inputs

Read `task/controller/pkg/result/result_writer_test.go` before editing. File is >1000 lines — use chunked reads (offset/limit) to read it fully before editing.

Locate phase fields set to "in_progress":
```bash
grep -n '"phase"' task/controller/pkg/result/result_writer_test.go
```

Change ONLY `"phase":` field values from "in_progress" to "execution":

Before:
```go
"phase":           "in_progress",
```

After:
```go
"phase":           "execution",
```

Do NOT change `"status":` fields that happen to have value "in_progress" — TaskStatusInProgress is not being renamed. Confirm you are touching only phase fields:
```bash
grep -n '"in_progress"' task/controller/pkg/result/result_writer_test.go | grep -v '"status"'
```
Expected: 0 remaining lines (only phase occurrences should have been changed).

Also check line ~196 (`NotTo(ContainSubstring("in_progress"))`). Read the surrounding 10 lines and apply this concrete rule:

- If the assertion is inside a test case whose `"phase"` input was changed from `"in_progress"` to `"execution"` in this prompt → change the assertion to `NotTo(ContainSubstring("execution"))`.
- If the assertion is inside a test case whose `"status"` field carries `"in_progress"` and no phase field was changed → LEAVE UNCHANGED (status `"in_progress"` is not being renamed).
- If the assertion checks serialized output for the absence of legacy phase strings as a regression guard → LEAVE UNCHANGED (legacy alias may still appear during the transition window).

No `## Improvements` escape hatch — apply one of the three branches above deterministically.

Verify status fields are unchanged:
```bash
grep -c '"status".*"in_progress"\|"in_progress".*"status"' task/controller/pkg/result/result_writer_test.go
```
Expected: ≥1 (status "in_progress" lines intact; if 0, something went wrong — check immediately).

Build and test:
```bash
cd task/controller && go test ./pkg/result/... -v 2>&1 | grep -E "PASS|FAIL" | tail -20
```
Expected: exit 0.

## 4. Add alias roundtrip tests

### 4a. Add NormalizeTaskPhase alias test to scanner package

Read `task/controller/pkg/scanner/vault_scanner_test.go` before editing. Find the top-level `Describe(...)` block and append a new nested `Describe` inside it (or add a standalone top-level `Describe` at the bottom of the file if the structure does not permit nesting).

```go
Describe("domain.NormalizeTaskPhase alias (spec 038)", func() {
	It("normalizes legacy phase 'in_progress' to TaskPhaseExecution", func() {
		canonical, ok := domain.NormalizeTaskPhase("in_progress")
		Expect(ok).To(BeTrue())
		Expect(canonical).To(Equal(domain.TaskPhaseExecution))
	})
})
```

Add `"github.com/bborbe/vault-cli/pkg/domain"` to the import block if not already present.

Verify:
```bash
grep -n 'NormalizeTaskPhase\|TaskPhaseExecution' task/controller/pkg/scanner/vault_scanner_test.go
```
Expected: ≥2 matches (the normalize call and the assertion).

### 4b. Add NormalizeTaskPhase alias test to command package

Read `task/controller/pkg/command/task_frontmatter_sequence_test.go` before editing. Append a normalize alias test for phase inside the top-level `Describe` block (or at the bottom).

```go
Describe("domain.NormalizeTaskPhase alias (spec 038)", func() {
	It("normalizes legacy phase 'in_progress' to TaskPhaseExecution", func() {
		canonical, ok := domain.NormalizeTaskPhase("in_progress")
		Expect(ok).To(BeTrue())
		Expect(canonical).To(Equal(domain.TaskPhaseExecution))
	})
})
```

Add `"github.com/bborbe/vault-cli/pkg/domain"` to the import block if not already present.

Verify:
```bash
grep -n 'NormalizeTaskPhase\|TaskPhaseExecution' task/controller/pkg/command/task_frontmatter_sequence_test.go
```
Expected: ≥2 matches.

### 4c. Add NormalizeTaskStatus alias test

Read `task/controller/pkg/scanner/vault_scanner_test.go` (already open from step 4a) — append the status normalize test to the same `Describe("domain.NormalizeTaskPhase alias (spec 038)", ...)` block or create a sibling `Describe` for status:

```go
Describe("domain.NormalizeTaskStatus alias (spec 038)", func() {
	It("normalizes legacy status 'todo' to TaskStatusNext", func() {
		canonical, ok := domain.NormalizeTaskStatus("todo")
		Expect(ok).To(BeTrue())
		Expect(canonical).To(Equal(domain.TaskStatusNext))
	})
})
```

Verify:
```bash
grep -rn 'NormalizeTaskStatus("todo")' task/controller/pkg/ --include='*_test.go'
```
Expected: ≥1 match.

Run iterative tests for scanner package after all additions:
```bash
cd task/controller && go test ./pkg/scanner/... -v 2>&1 | grep -E "PASS|FAIL|alias" | tail -20
```
Expected: exit 0; alias test rows appear as PASS.

## 5. Run make test then make precommit in task/controller

```bash
cd task/controller && make test
```
Expected: exit 0. Fix any test failures before continuing.

```bash
cd task/controller && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make errcheck`, `make gosec`) and fix before retrying full precommit.

## 6. Final acceptance criteria verification

Run the full set of spec 038 acceptance criteria:

```bash
# AC: consistent vault-cli version across all modules
grep -rn 'github.com/bborbe/vault-cli' agent/ task/ lib/ --include='go.mod'
```
Expected: all entries on the same version > v0.64.1.

```bash
# AC: no default:"in_progress" in agent/claude
grep -n 'default:"in_progress"' agent/claude/main.go agent/claude/cmd/run-task/main.go
```
Expected: 0 lines.

```bash
grep -n 'default:"execution"' agent/claude/main.go agent/claude/cmd/run-task/main.go
```
Expected: ≥1 match in each file.

```bash
# AC: no default:"in_progress" in any main.go
grep -rn 'default:"in_progress"' agent/ task/ --include='main.go' --exclude-dir=vendor
```
Expected: 0 lines.

```bash
# AC: no legacy usage strings
grep -rn 'planning | in_progress | ai_review' agent/ --include='*.go' --exclude-dir=vendor
```
Expected: 0 lines.

```bash
# AC: CRD comment updated
grep -n 'execution' task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Expected: ≥1 match in Trigger/Phases doc.

```bash
# AC: NormalizeTaskPhase alias test in scanner AND command
grep -B2 -A4 'NormalizeTaskPhase("in_progress")' task/controller/pkg/scanner/*_test.go task/controller/pkg/command/*_test.go
```
Expected: ≥1 assertion block in each directory.

```bash
# AC: NormalizeTaskStatus alias test
grep -rn 'NormalizeTaskStatus("todo")' --include='*_test.go' --exclude-dir=vendor
```
Expected: ≥1 line with a TaskStatusNext assertion within 4 lines.

```bash
# AC: all 6 modules pass precommit (lib and executor verified in prompt 1)
(cd lib                && make precommit)
(cd task/controller   && make precommit)
(cd task/executor     && make precommit)
(cd agent/claude      && make precommit)
(cd agent/gemini      && make precommit)
(cd agent/code        && make precommit)
```
Expected: all 6 exit 0.

</requirements>

<constraints>
- **Only change "status": "todo" → "status": "next"** for task STATUS fields. Do NOT touch "status": "in_progress" — TaskStatusInProgress is not being renamed and those assertions/inputs must remain unchanged.
- **Only change "phase": "in_progress" → "phase": "execution"** for task PHASE fields. Be precise: look at the field key ("phase" vs "status") before changing any value.
- **Alias roundtrip tests must NOT be deleted** — the spec requires at least one test per dimension that proves NormalizeTaskPhase("in_progress") → execution and NormalizeTaskStatus("todo") → next. These tests are the only remaining legitimate references to the legacy string values in test code.
- **External test packages** — keep all `package ..._test` declarations unchanged.
- **errors.Wrapf from github.com/bborbe/errors** for any new error wrapping (no new errors expected in this prompt).
- **Do NOT modify production code** — this prompt is test-only. If a production code change is needed to make tests pass, document it in `## Improvements` as a PROMPT issue rather than silently making the change.
- **Do NOT commit.** dark-factory handles git.
- `cd task/controller && make precommit` must exit 0.
</constraints>

<verification>

Pre-condition: vault-cli version in task/controller > v0.64.1:
```bash
grep "vault-cli" task/controller/go.mod
```
Expected: version > v0.64.1.

No remaining status "todo" assertions (excluding normalize calls):
```bash
grep -rn '"todo"' task/controller/pkg/ --include='*_test.go' | grep -v 'Normalize\|alias'
```
Expected: 0 lines.

No remaining phase "in_progress" literals (excluding normalize calls):
```bash
grep -rn '"phase".*"in_progress"\|"in_progress".*"phase"' task/controller/pkg/ --include='*_test.go' | grep -v 'Normalize\|alias'
```
Expected: 0 lines.

Status "in_progress" fields intact:
```bash
grep -c '"status".*"in_progress"' task/controller/pkg/result/result_writer_test.go
```
Expected: ≥5 (many test cases use status in_progress, none should be changed).

NormalizeTaskPhase alias test in both packages:
```bash
grep -rn 'NormalizeTaskPhase("in_progress")' task/controller/pkg/scanner/*_test.go task/controller/pkg/command/*_test.go
```
Expected: ≥1 match in each directory.

NormalizeTaskStatus alias test present:
```bash
grep -rn 'NormalizeTaskStatus("todo")' --include='*_test.go' --exclude-dir=vendor
```
Expected: ≥1 line.

Full precommit in task/controller:
```bash
cd task/controller && make precommit
```
Expected: exit 0.

All 6 modules passing:
```bash
(cd lib                && make precommit)
(cd task/controller   && make precommit)
(cd task/executor     && make precommit)
(cd agent/claude      && make precommit)
(cd agent/gemini      && make precommit)
(cd agent/code        && make precommit)
```
Expected: all 6 exit 0.

</verification>
