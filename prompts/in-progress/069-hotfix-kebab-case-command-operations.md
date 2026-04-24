---
status: committing
summary: Renamed CommandOperation strings from underscore to kebab-case in lib, metrics, executors, tests, and docs; added DescribeTable regression test in lib and controller; all three make precommit runs exited 0.
container: agent-069-hotfix-kebab-case-command-operations
dark-factory-version: v0.132.0
created: "2026-04-24T09:00:00Z"
queued: "2026-04-24T08:58:13Z"
started: "2026-04-24T08:59:22Z"
---

<summary>
- Renames two CommandOperation string literals in `lib/agent_task-commands.go` from snake_case to kebab-case so they pass cqrs validation (`^[a-z][a-z-]*$`)
- Unblocks executor → controller publishing: currently `IncrementFrontmatterCommand` is rejected by cqrs with "illegal commandOperation", leaving tasks stuck in a publish-retry loop with `trigger_count` never incremented
- Updates every call site that hard-codes these two strings (metrics labels, tests, docs)
- Adds a regression test that enumerates every `base.CommandOperation` constant in `lib/` and validates each against the cqrs regex — prevents this class of bug from recurring
- Adds a warning comment above the constants documenting the regex contract
- No schema version bump, no wire-format change — only the operation-string literal changes
- `make precommit` must pass in `lib/`, `task/controller/`, `task/executor/`
</summary>

<objective>
Fix the invalid `CommandOperation` string literals `"increment_frontmatter"` and `"update_frontmatter"` (rejected by cqrs regex `^[a-z][a-z-]*$`) by renaming them to kebab-case, update every call site that hard-codes the underscore form, and add a regression test that enumerates all `lib.*CommandOperation` constants and validates each one — so a future constant with an illegal character fails CI instead of production.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega `DescribeTable` / `Entry`, external test packages
- `go-cqrs.md` in `~/.claude/plugins/marketplaces/coding/docs/` — CommandOperation shape and validation
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — label pre-init, label stability

**Why this hotfix exists.** Spec 015 introduced two new `base.CommandOperation` constants in `lib/agent_task-commands.go`:

```go
const IncrementFrontmatterCommandOperation base.CommandOperation = "increment_frontmatter"
const UpdateFrontmatterCommandOperation    base.CommandOperation = "update_frontmatter"
```

The cqrs library validates command operations against a strict regex:

```
// github.com/bborbe/cqrs/base/base_command-operation.go
`^[a-z][a-z-]*$`  // lowercase letters and hyphens only, starting with a letter
```

Underscores are rejected. In dev, executor task `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9` entered a publish-retry loop:

```
W0424 08:47:36 run_log.go:18] run failed: publish increment trigger_count for task ba1bad61-...:
  send command for operation increment_frontmatter:
  create message failed: validate command failed:
  validate command of commandOperation 'increment_frontmatter' failed:
  validate command operation failed: illegal commandOperation
```

No Jobs spawn (trigger-cap ordering invariant is structurally sound — publish must succeed before spawn), but `trigger_count` never increments, so the task is stuck.

**Key files to read in full before editing:**

- `lib/agent_task-commands.go` — constant declarations (~lines 14 and 18); the two strings must change
- `lib/agent_task-commands_test.go` — existing tests; extend this file with the regression table
- `lib/lib_suite_test.go` — Ginkgo suite root
- `task/controller/pkg/metrics/metrics.go` — `FrontmatterCommandsTotal` counter vec; find the pre-init loop and every `WithLabelValues(...)` call site
- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — verify it uses `lib.IncrementFrontmatterCommandOperation` (constant, not literal); note every `metrics.FrontmatterCommandsTotal.WithLabelValues(...)` call
- `task/controller/pkg/command/task_update_frontmatter_executor.go` — same verification
- `task/executor/pkg/result_publisher.go` — `PublishIncrementTriggerCount` implementation; must use the constant, not a literal
- `task/executor/pkg/handler/task_event_handler.go` — log messages may mention the operation string
- `docs/controller-design.md` — any mention of `increment_frontmatter` / `update_frontmatter` becomes kebab
- `docs/task-flow-and-failure-semantics.md` — same

Run these before editing (project root = `~/Documents/workspaces/agent`):

```bash
grep -rn "increment_frontmatter\|update_frontmatter" . --include='*.go' --include='*.md' | grep -v vendor
```

Inventory every match. Each one must be updated (or, for `*_test.go` files that assert metric label values, updated to match the new label values).

```bash
grep -rn "increment-frontmatter\|update-frontmatter" . --include='*.go' --include='*.md' | grep -v vendor
```

Expect: currently zero matches. After this prompt: matches in lib, metrics, executors, docs, and any test that asserts metric labels.
</context>

<requirements>

1. **Rename the two string literals in `lib/agent_task-commands.go`**

   Change:

   ```go
   const IncrementFrontmatterCommandOperation base.CommandOperation = "increment_frontmatter"
   ```

   to:

   ```go
   const IncrementFrontmatterCommandOperation base.CommandOperation = "increment-frontmatter"
   ```

   And:

   ```go
   const UpdateFrontmatterCommandOperation base.CommandOperation = "update_frontmatter"
   ```

   to:

   ```go
   const UpdateFrontmatterCommandOperation base.CommandOperation = "update-frontmatter"
   ```

   Directly above the constants (as a block comment that applies to both), add:

   ```go
   // IMPORTANT: operation strings must match base.CommandOperation.Validate regex
   // `^[a-z][a-z-]*$` (lowercase letters and hyphens only, starting with a letter).
   // Underscores, digits, and uppercase are REJECTED at runtime by cqrs.
   // Every constant below MUST also be added to the Validate-all test table in
   // agent_task-commands_test.go. CI catches misses there.
   ```

2. **Enumerate and update every non-constant reference to the two underscore strings**

   Run:

   ```bash
   grep -rn "increment_frontmatter\|update_frontmatter" . --include='*.go' --include='*.md' | grep -v vendor
   ```

   For every match outside `lib/agent_task-commands.go` (which you already fixed in step 1):

   - If the match is a Go constant or literal that feeds into `base.CommandOperation`, rename to the kebab-case form.
   - If the match is a **Prometheus metric label value** (e.g. in `task/controller/pkg/metrics/metrics.go` or executor call sites), rename to the kebab-case form — keeping operation-string and metric-label identical simplifies dashboards and alerts.
   - If the match is in a **test file** that asserts metric label values (e.g. `...WithLabelValues("increment_frontmatter")...`), rename to the kebab-case form so the test still matches the new label.
   - If the match is in **documentation** (`docs/*.md`, `CHANGELOG.md`), rename to the kebab-case form inline where it appears as an operation name, preserving surrounding prose.

   Known likely locations (verify each with `grep`; do NOT rely on this list being exhaustive — the grep output is authoritative):

   - `task/controller/pkg/metrics/metrics.go` — `FrontmatterCommandsTotal` pre-init loop label values, and any inline label list in comments
   - `task/controller/pkg/command/task_increment_frontmatter_executor.go` — `metrics.FrontmatterCommandsTotal.WithLabelValues(...)` calls; confirm the executor uses `lib.IncrementFrontmatterCommandOperation` (the constant) for the operation itself, not a literal
   - `task/controller/pkg/command/task_update_frontmatter_executor.go` — same
   - `task/controller/pkg/command/task_increment_frontmatter_executor_test.go` — **confirmed to hard-code `base.CommandOperation("increment_frontmatter")` at ~line 108**; update the literal to kebab-case
   - `task/controller/pkg/command/task_update_frontmatter_executor_test.go` — **confirmed to hard-code `base.CommandOperation("update_frontmatter")` at ~line 107**; update the literal to kebab-case
   - `lib/agent_task-commands_test.go` — **confirmed to hard-code the underscore operation literals at ~lines 21 and 27** (constant-value assertions); update both to kebab-case
   - `task/controller/pkg/command/*_test.go` — other metric label assertions
   - `task/executor/pkg/result_publisher.go` — confirm uses `lib.IncrementFrontmatterCommandOperation` constant (no literal)
   - `task/executor/pkg/result_publisher_test.go` — if any assertions on the operation string
   - `task/executor/pkg/handler/task_event_handler.go` — log format strings may mention the operation
   - `docs/controller-design.md`
   - `docs/task-flow-and-failure-semantics.md`
   - `CHANGELOG.md`

   **If a call site uses the `lib.IncrementFrontmatterCommandOperation` / `lib.UpdateFrontmatterCommandOperation` constant**, no change is needed at that site — the constant now resolves to the kebab-case value automatically. This is the preferred pattern; flag in your summary any call site that was using a literal string instead.

3. **Update `FrontmatterCommandsTotal` label pre-init in `task/controller/pkg/metrics/metrics.go`**

   Read the current pre-init loop. It likely looks like:

   ```go
   for _, op := range []string{"increment_frontmatter", "update_frontmatter"} {
       FrontmatterCommandsTotal.WithLabelValues(op, "success").Add(0)
       FrontmatterCommandsTotal.WithLabelValues(op, "error").Add(0)
   }
   ```

   (Exact shape may differ — match what is there.) Rename both underscore strings to kebab-case. Keep the pattern identical; only the two string values change.

   If the file also has a comment listing valid label values (e.g. `// operation: increment_frontmatter, update_frontmatter`), update that comment too.

4. **Add regression test in `lib/agent_task-commands_test.go`**

   Read the existing test file to understand the Ginkgo/Gomega setup and package conventions (external test package `lib_test`, imports of `lib`, `base` from `github.com/bborbe/cqrs/base`, `context`). Append a new `Describe` block (do NOT delete or rename existing tests):

   ```go
   var _ = Describe("CommandOperation validation", func() {
       var ctx context.Context
       BeforeEach(func() {
           ctx = context.Background()
       })

       // IMPORTANT: this table is the single source of truth for "every CommandOperation
       // constant declared in lib/". When you add a new lib.*CommandOperation constant,
       // you MUST add a matching Entry here. If you forget, this suite will not catch it —
       // so reviewers must enforce the rule. The comment above the constants in
       // agent_task-commands.go reminds contributors of this.
       DescribeTable("all lib CommandOperation constants pass base.CommandOperation.Validate",
           func(op base.CommandOperation) {
               Expect(op.Validate(ctx)).To(Succeed())
           },
           Entry("IncrementFrontmatterCommandOperation", lib.IncrementFrontmatterCommandOperation),
           Entry("UpdateFrontmatterCommandOperation", lib.UpdateFrontmatterCommandOperation),
       )
   })
   ```

   Imports to add if not already present:

   ```go
   import (
       "context"

       "github.com/bborbe/cqrs/base"
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"

       "github.com/<repo-path>/lib"
   )
   ```

   Resolve the actual `lib` import path from the existing `agent_task-commands_test.go` file (it is in the same `lib_test` package — so `lib.IncrementFrontmatterCommandOperation` is reachable via the existing import alias).

   Confirm the `DescribeTable` imports: `DescribeTable` and `Entry` come from `github.com/onsi/ginkgo/v2` (same dot-import that provides `Describe`, `It`, etc.). No extra import needed.

5. **Add a sanity-check test in `task/controller/pkg/command/` for `TaskResultCommandOperation`**

   Find `TaskResultCommandOperation` (or whatever the controller-side command-operation constant is named — grep `task/controller/pkg/command/` for `base.CommandOperation`). It is already kebab-case (e.g. `"update"`) per the existing controller design, so this test should pass on first run — it exists as a future-regression guard.

   Add a new test file `task/controller/pkg/command/command_operations_test.go` (or append to an existing `*_test.go` file in the same package) with the same `DescribeTable` pattern:

   ```go
   var _ = Describe("controller CommandOperation validation", func() {
       var ctx context.Context
       BeforeEach(func() { ctx = context.Background() })

       DescribeTable("all controller-local CommandOperation constants pass base.CommandOperation.Validate",
           func(op base.CommandOperation) {
               Expect(op.Validate(ctx)).To(Succeed())
           },
           // Add one Entry per base.CommandOperation constant defined in this package.
           Entry("TaskResultCommandOperation", command.TaskResultCommandOperation),
       )
   })
   ```

   If there are no controller-local `base.CommandOperation` constants (all operations live in `lib/`), SKIP this step and note in your summary "no controller-local CommandOperation constants found; controller-side regression test not added."

   If the test file needs a Ginkgo suite bootstrap, follow the existing pattern in sibling `*_suite_test.go` files in `task/controller/pkg/command/`.

6. **Do NOT modify `github.com/bborbe/cqrs/base/base_command-operation.go` or its regex.** This is an upstream dependency; the fix is in the consumer.

7. **Do NOT introduce a schema version bump.** The Kafka message wire format is unchanged — only the operation-string literal changes. Downstream consumers that pattern-match on the operation string must be updated at the same release (covered by step 2 above, since the controller handler is the only consumer).

8. **Verify kebab-case coverage after edits**

   ```bash
   # Must be ZERO matches anywhere outside vendor:
   grep -rn "increment_frontmatter\|update_frontmatter" . --include='*.go' --include='*.md' | grep -v vendor

   # Must show matches in lib, metrics, executors, docs, tests:
   grep -rn "increment-frontmatter\|update-frontmatter" . --include='*.go' --include='*.md' | grep -v vendor
   ```

9. **Run tests iteratively, module by module**

   ```bash
   cd ~/Documents/workspaces/agent/lib && make precommit
   cd ~/Documents/workspaces/agent/task/controller && make precommit
   cd ~/Documents/workspaces/agent/task/executor && make precommit
   ```

   All three must exit 0. Fix failures as they appear (most likely: a test that asserts `WithLabelValues("increment_frontmatter")` and now needs the kebab form).

10. **Run the regression test explicitly to confirm it exists and passes**

    ```bash
    cd ~/Documents/workspaces/agent/lib && go test -run CommandOperation -v ./...
    ```

    Output must include the `DescribeTable` entries for `IncrementFrontmatterCommandOperation` and `UpdateFrontmatterCommandOperation`, both passing.

11. **Update `CHANGELOG.md` at repo root**

    Append to `## Unreleased` (create if absent):

    ```markdown
    - fix: rename CommandOperation strings `increment_frontmatter` → `increment-frontmatter` and `update_frontmatter` → `update-frontmatter` so they pass cqrs regex `^[a-z][a-z-]*$`; unblocks trigger_count increment publish; adds regression test enumerating all lib CommandOperation constants against base.CommandOperation.Validate
    ```

</requirements>

<constraints>
- Kebab-case only for `base.CommandOperation` strings (regex `^[a-z][a-z-]*$` — lowercase letters and hyphens only, starting with a letter). No underscores, no digits, no uppercase.
- Do NOT modify `github.com/bborbe/cqrs/base/base_command-operation.go` or its regex.
- Do NOT introduce a schema version bump — the message wire format is unchanged, only the operation-string literal.
- Do NOT delete or rename any existing `base.CommandOperation` constant other than the two named in this prompt.
- Do NOT touch task files (`24 Tasks/*.md`) or any Obsidian content — this is a pure code fix.
- Metric label values should match the kebab-case operation strings exactly, to keep dashboards and alerts readable.
- Use `github.com/bborbe/errors` for any new error wrapping — never `fmt.Errorf`.
- Ginkgo v2 only (`DescribeTable`, `Entry`). No Ginkgo v1.
- All existing tests must pass after the rename.
- Do NOT commit — dark-factory handles git.
- `make precommit` must exit 0 in `lib/`, `task/controller/`, and `task/executor/`.
</constraints>

<verification>

Verify the constant values:
```bash
grep -n "IncrementFrontmatterCommandOperation\|UpdateFrontmatterCommandOperation" ~/Documents/workspaces/agent/lib/agent_task-commands.go
```
Must show the kebab-case values `"increment-frontmatter"` and `"update-frontmatter"`. Must NOT show underscore forms.

Verify the warning comment is present above the constants:
```bash
grep -n "base.CommandOperation.Validate regex" ~/Documents/workspaces/agent/lib/agent_task-commands.go
```
Must show one match.

Verify no underscore form remains anywhere outside vendor:
```bash
cd ~/Documents/workspaces/agent && grep -rn "increment_frontmatter\|update_frontmatter" . --include='*.go' --include='*.md' | grep -v vendor
```
Must output zero lines.

Verify kebab form is present in the expected files:
```bash
cd ~/Documents/workspaces/agent && grep -rn "increment-frontmatter\|update-frontmatter" lib/ task/controller/ task/executor/ docs/ CHANGELOG.md --include='*.go' --include='*.md'
```
Must show matches in: `lib/agent_task-commands.go`, `task/controller/pkg/metrics/metrics.go`, at least one doc file, and `CHANGELOG.md`.

Verify the regression test exists:
```bash
grep -n "DescribeTable\|all lib CommandOperation" ~/Documents/workspaces/agent/lib/agent_task-commands_test.go
```
Must show the `DescribeTable` and the description string.

Run the regression test in isolation:
```bash
cd ~/Documents/workspaces/agent/lib && go test -run CommandOperation -v ./...
```
Must exit 0. Output must list at least two entries (IncrementFrontmatterCommandOperation, UpdateFrontmatterCommandOperation) both PASS.

Run precommit in every affected module:
```bash
cd ~/Documents/workspaces/agent/lib && make precommit
cd ~/Documents/workspaces/agent/task/controller && make precommit
cd ~/Documents/workspaces/agent/task/executor && make precommit
```
All three must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "increment-frontmatter\|update-frontmatter" ~/Documents/workspaces/agent/CHANGELOG.md
```
Must show the Unreleased entry.

</verification>
