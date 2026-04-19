---
status: created
spec: [012-generic-claude-task-runner]
created: "2026-04-19T18:00:00Z"
branch: dark-factory/generic-claude-task-runner
---

<summary>
- `trade-analysis/pkg.AgentResult` gains four methods implementing `claude.AgentResultLike`: `GetStatus`, `GetMessage`, `GetFiles`, `RenderResultSection`
- `RenderResultSection()` produces byte-identical markdown to today's `pkg/result-deliverer.go BuildResultSection` output (including `Analyzed/Skipped/Total` table); a golden-file unit test guards this
- Local `pkg.ResultDeliverer` interface deleted; concrete structs (`FileResultDeliverer`, `tradeAnalysisContentGenerator`) now satisfy `claude.ResultDeliverer[pkg.AgentResult]` directly
- Local `pkg.BuildResultSection` function deleted (logic moved into `AgentResult.RenderResultSection`)
- `pkg/task-runner.go`, `pkg/task-runner_test.go`, `mocks/pkg-task-runner.go` deleted — the duplicated parser is gone
- `pkg/factory/factory.go` switches from `pkg.NewTaskRunner(...)` to `claude.NewTaskRunner[pkg.AgentResult](...)`
- `go.mod` bumped to the new `lib/vX.Y.Z` tag; `go mod tidy` run
- The smoke-test parse error (`parse claude result failed: <prose prefix>\n\n{valid JSON}`) is eliminated — trade-analysis now uses `extractLastJSONObject` from `lib/claude`
- `CHANGELOG.md` `## Unreleased` entry added
- `cd ~/Documents/workspaces/trading/agent/trade-analysis && make precommit` passes
</summary>

<objective>
Migrate `trade-analysis` to use `lib/claude.NewTaskRunner[pkg.AgentResult]` and delete the private `pkg/task-runner.go` copy that was missing spec 010's prose-stripping parser fix. After this change, every task-runner fix in `lib/claude` automatically benefits trade-analysis. The smoke-test task `94884aa4-…` will stop failing with the old `parse claude result failed` error.
</objective>

<context>
Read `CLAUDE.md` for project conventions (the trading workspace's `CLAUDE.md`).

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Interface→Constructor→Struct, counterfeiter annotations
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, golden-file patterns

**Worktree requirement:** CLAUDE.md forbids editing directly in `~/Documents/workspaces/trading/` or any deployment worktree. Before editing, create a feature worktree:

```bash
cd ~/Documents/workspaces/trading
git fetch
git worktree add ../trading-generic-claude -b feature/generic-claude-task-runner origin/master
cd ../trading-generic-claude/agent/trade-analysis
```

All file edits below happen inside `trading-generic-claude/agent/trade-analysis`, NOT `trading/agent/trade-analysis`. `make precommit` also runs in the feature worktree.

**Preconditions:**
1. Prompt 1 (lib generic task-runner) must be merged to `master` in the `agent` repo
2. A new `lib/vX.Y.Z` tag must exist on GitHub — discover the tag at the start of this prompt:

```bash
git -C ~/Documents/workspaces/agent fetch --tags
LIB_TAG=$(git -C ~/Documents/workspaces/agent tag -l 'lib/v*' --sort=-v:refname | head -1)
echo "Using lib tag: $LIB_TAG"
```

If `LIB_TAG` is empty or matches the tag that was already in `trade-analysis/go.mod` before this prompt started, STOP — prompt 1 has not released yet. Re-queue this prompt after `lib/vX.Y.Z` is tagged.

Use `$LIB_TAG` in step 7 (`go get github.com/bborbe/agent/lib@$LIB_TAG`).

**Key files to read before editing:**

- `pkg/types.go` — `AgentResult` struct with `Analyzed`, `Skipped`, `Total`, `Status`, `Message`, `Files` fields; four new methods go here
- `pkg/result-deliverer.go` — contains `pkg.ResultDeliverer` interface (to delete), `FileResultDeliverer` struct, `tradeAnalysisContentGenerator` struct, and `BuildResultSection` function (to delete after moving logic to `RenderResultSection`)
- `pkg/task-runner.go` — the duplicated task-runner (to delete entirely)
- `pkg/task-runner_test.go` — tests for the duplicated task-runner (to delete entirely)
- `mocks/pkg-task-runner.go` — counterfeiter mock for `pkg.TaskRunner` (to delete)
- `pkg/factory/factory.go` — `CreateKafkaResultDeliverer` (return type change) and the `pkg.NewTaskRunner(...)` call (to replace with `claude.NewTaskRunner[pkg.AgentResult](...)`)
- `go.mod` — needs `github.com/bborbe/agent/lib` bumped to the new tag

**Understanding today's `BuildResultSection` in `pkg/result-deliverer.go`:**

Read the current implementation carefully. `AgentResult.RenderResultSection()` must produce byte-identical output. The current function likely renders something like:

```
## Result

| Field | Value |
|-------|-------|
| Status | done |
| Message | ... |
| Analyzed | 3 |
| Skipped | 1 |
| Total | 4 |

**Files:**
- [[path/to/file.md]]
```

Read the actual implementation — do NOT guess the format. The golden-file test must capture the exact current output.
</context>

<requirements>

1. **Add `AgentResultLike` methods to `pkg/types.go`**

   Add four value-receiver methods to `AgentResult` implementing `claude.AgentResultLike`:

   ```go
   func (r AgentResult) GetStatus() claude.AgentStatus { return r.Status }
   func (r AgentResult) GetMessage() string              { return r.Message }
   func (r AgentResult) GetFiles() []string              { return r.Files }
   func (r AgentResult) RenderResultSection() string     { /* see step 2 */ }
   ```

   Import `claudelib "github.com/bborbe/agent/lib/claude"` (use the import alias matching the rest of the package).

2. **Implement `AgentResult.RenderResultSection()` with exact current output**

   Read `pkg/result-deliverer.go`'s `BuildResultSection` function. Copy its rendering logic into `AgentResult.RenderResultSection()` verbatim — do NOT simplify or restructure. The golden-file test in step 3 will catch any byte-level difference.

   The new method belongs in `pkg/types.go` (or a new `pkg/result-section.go` if the file becomes too long).

3. **Add golden-file test for `RenderResultSection` in `pkg/`**

   Create `pkg/types_test.go` (or add to an existing test file in `pkg/`) using the Ginkgo/Gomega pattern:

   ```go
   var _ = Describe("AgentResult.RenderResultSection", func() {
       It("produces byte-identical output to the previous BuildResultSection", func() {
           result := AgentResult{
               Status:   claude.AgentStatusDone,
               Message:  "3 trades analyzed",
               Analyzed: 3,
               Skipped:  1,
               Total:    4,
               Files:    []string{"tasks/task-abc.md"},
           }
           got := result.RenderResultSection()
           // The golden string below must match the output of the OLD pkg.BuildResultSection
           // exactly. If this test fails, restore the render logic before merging.
           Expect(got).To(Equal(expectedRenderSection))
       })
   })
   ```

   **Golden string (inline — do not re-derive):**

   For the input above (`Status=done, Message="3 trades analyzed", Analyzed=3, Skipped=1, Total=4, Files=["tasks/task-abc.md"]`), the literal output of the old `pkg.BuildResultSection` is:

   ```
   ## Result

   **Status:** done
   **Message:** 3 trades analyzed

   | Metric | Value |
   |--------|-------|
   | Total | 4 |
   | Analyzed | 3 |
   | Skipped | 1 |

   **Files:**
   - [[tasks/task-abc.md]]
   ```

   Use a Go raw-string literal with a leading newline after the backtick and NO trailing newline beyond the final `\n` after `]]`. Match byte-for-byte.

   Also add a test with zero `Analyzed/Skipped/Total` (error-path result from `newErrorResult`):

   ```go
   It("handles error result with zero trade fields", func() {
       result := AgentResult{
           Status:  claude.AgentStatusFailed,
           Message: "claude CLI failed: timeout",
       }
       got := result.RenderResultSection()
       Expect(got).To(ContainSubstring("**Status:** failed"))
       Expect(got).To(ContainSubstring("claude CLI failed"))
   })
   ```

4. **Delete `pkg/task-runner.go`, `pkg/task-runner_test.go`, `mocks/pkg-task-runner.go`**

   ```bash
   rm pkg/task-runner.go pkg/task-runner_test.go mocks/pkg-task-runner.go
   ```

   After deletion, verify no remaining file imports `pkg.TaskRunner` or `pkg.NewTaskRunner`:
   ```bash
   grep -rn "pkg\.TaskRunner\|pkg\.NewTaskRunner\|NewTaskRunner(" --include="*.go" .
   ```
   Any remaining reference to `NewTaskRunner` must be `claude.NewTaskRunner` (the call site in factory, added in step 6).

5. **Rewrite `pkg/result-deliverer.go`**

   The current file contains (read it before editing):
   - `pkg.ResultDeliverer` interface (lines 18-20) — **delete**
   - `pkg.NewResultDelivererAdapter(inner libagent.ResultDeliverer)` constructor (lines 22-25) + internal `resultDelivererAdapter` struct (lines 27-37) — **delete entirely** (replaced by `claudelib.NewResultDelivererAdapter[AgentResult]` from lib)
   - `pkg.NewNoopResultDeliverer()` constructor (lines 39-42) — **rewrite** to return `claudelib.ResultDeliverer[AgentResult]`:
     ```go
     func NewNoopResultDeliverer() claudelib.ResultDeliverer[AgentResult] {
         return claudelib.NewResultDelivererAdapter[AgentResult](libagent.NewNoopResultDeliverer())
     }
     ```
   - `pkg.NewFileResultDeliverer(filePath string)` constructor (lines 44-49) — **rewrite** to return `claudelib.ResultDeliverer[AgentResult]`:
     ```go
     func NewFileResultDeliverer(filePath string) claudelib.ResultDeliverer[AgentResult] {
         generator := &tradeAnalysisContentGenerator{}
         return claudelib.NewResultDelivererAdapter[AgentResult](libagent.NewFileResultDeliverer(generator, filePath))
     }
     ```
   - `tradeAnalysisContentGenerator` struct + `Generate` method (lines 51-64) — **keep as-is**, it satisfies `libagent.ContentGenerator` which is unchanged
   - `ReplaceOrAppendResultSection` function (lines 66-81) — **keep as-is**
   - `BuildResultSection` function (lines 83-114) — **delete** (logic now lives in `AgentResult.RenderResultSection()`)
   - Counterfeiter annotation for local `ResultDeliverer` (line 15) — **delete** (no longer needed, mock comes from `lib/mocks`)

   Add import: `claudelib "github.com/bborbe/agent/lib/claude"` (use this exact alias to match style; `libagent` stays for `lib/delivery`).

   Delete the counterfeiter-generated mock too:
   ```bash
   rm mocks/pkg-result-deliverer.go 2>/dev/null || true
   ```

   After edit, verify no caller references the deleted local `NewResultDelivererAdapter`:
   ```bash
   grep -rn "pkg\.NewResultDelivererAdapter\|pkg\.ResultDeliverer\b" --include="*.go" .
   ```
   Must return nothing.

6. **Update `pkg/factory/factory.go`**

   a. **Replace `pkg.NewTaskRunner(...)` with `claude.NewTaskRunner[pkg.AgentResult](...)`**

   The old call site:
   ```go
   pkg.NewTaskRunner(runner, instructions, branch, stage, gitRestURL, deliverer)
   ```

   The new call site:
   ```go
   claude.NewTaskRunner[pkg.AgentResult](
       runner,
       instructions,
       map[string]string{
           "Branch":      branch.String(),
           "Stage":       stage.String(),
           "GIT_REST_URL": gitRestURL.String(),
       },
       deliverer,
   )
   ```

   The `envContext` map keys (`Branch`, `Stage`, `GIT_REST_URL`) are the exact keys the old `pkg/task-runner.go` passed to `claudelib.BuildPrompt` — verified against the code before this prompt was written. Do not rename or add keys.

   b. **Update `CreateKafkaResultDeliverer` return type** from `pkg.ResultDeliverer` to `claudelib.ResultDeliverer[pkg.AgentResult]`. Read the current function body first — it does NOT take a `currentDateTime` param (it inlines `libtime.NewCurrentDateTime()`). Do NOT add parameters; only the return type + the adapter call change. The body currently calls `pkg.NewResultDelivererAdapter(delivery.NewKafkaResultDeliverer(...))` — switch to `claudelib.NewResultDelivererAdapter[pkg.AgentResult](delivery.NewKafkaResultDeliverer(...))`.

   c. Add import `claudelib "github.com/bborbe/agent/lib/claude"` if not already present (use alias matching the file's existing style).

7. **Bump `go.mod` to the new lib tag**

   Use the `$LIB_TAG` value captured in the `<context>` precondition step. The tag looks like `lib/v0.38.0` — `go get` requires this exact form:

   ```bash
   go get github.com/bborbe/agent/lib@$LIB_TAG
   go mod tidy
   ```

   Verify `go.mod` shows the new version:
   ```bash
   grep "github.com/bborbe/agent/lib" go.mod
   ```

8. **Update `CHANGELOG.md`** in the trade-analysis repo root

   Check for `## Unreleased` first:
   ```bash
   grep -n "Unreleased" CHANGELOG.md | head -3
   ```
   If `## Unreleased` exists, append. If not, insert above the first `## v` heading.

   Add:
   ```markdown
   - feat: trade-analysis: migrate to lib/claude generic TaskRunner, delete duplicated parser (fixes parse claude result failed on prose-wrapped output)
   ```

</requirements>

<constraints>
- `AgentResult.RenderResultSection()` output must be byte-identical to the old `pkg.BuildResultSection` output for the same inputs — the golden-file test enforces this
- Do NOT change `main.go` beyond what is required for compilation (factory return-type changes propagate via inference)
- Do NOT change `delivery.AgentResultInfo` or any `lib/delivery/` files
- Do NOT commit — dark-factory handles git
- The `envContext` map keys in the `claude.NewTaskRunner[pkg.AgentResult](...)` call must match the keys the old `pkg.BuildPrompt` used — read the old code before deleting it
- All existing tests must pass (the deleted tests are for the deleted code; no regressions in other tests)
- `cd ~/Documents/workspaces/trading/agent/trade-analysis && make precommit` must exit 0
- Use value receivers on `AgentResult`'s `AgentResultLike` methods (same pattern as `lib/claude.AgentResult`)
- Use `github.com/bborbe/errors` for any error wrapping — never `fmt.Errorf`
</constraints>

<verification>
Verify `AgentResultLike` methods exist on `pkg.AgentResult`:
```bash
grep -n "GetStatus\|GetMessage\|GetFiles\|RenderResultSection" pkg/types.go
```
Must show all four methods.

Verify deleted files are gone:
```bash
ls pkg/task-runner.go pkg/task-runner_test.go mocks/pkg-task-runner.go 2>&1
```
Must show "No such file or directory" for all three.

Verify `pkg.ResultDeliverer` interface is gone:
```bash
grep -n "type ResultDeliverer interface" pkg/result-deliverer.go 2>&1
```
Must return nothing.

Verify factory uses generic task-runner:
```bash
grep -n "claude.NewTaskRunner\[pkg.AgentResult\]" pkg/factory/factory.go
```
Must show one match.

Verify golden-file test exists:
```bash
grep -rn "RenderResultSection\|golden" pkg/ --include="*_test.go" | head -5
```
Must show the golden-file test.

Run tests:
```bash
make test
```
Must exit 0.

Run precommit:
```bash
make precommit
```
Must exit 0.
</verification>
