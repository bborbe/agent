---
status: completed
summary: 'Fixed passthroughContentGenerator to write ## Failure section on AgentStatusFailed and AgentStatusNeedsInput, mirroring fallback and section generators; added DescribeTable regression harness (6 entries) and focused PassthroughContentGenerator Describe block; updated CHANGELOG.md under ## Unreleased.'
container: agent-106-passthrough-generator-writes-failure-body
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-12T22:00:00Z"
queued: "2026-05-12T21:44:33Z"
started: "2026-05-12T21:44:34Z"
completed: "2026-05-12T21:48:04Z"
---

<summary>
- `passthroughContentGenerator` currently drops the failure reason on the floor: when an agent returns `AgentStatusFailed` (or `AgentStatusNeedsInput`) with an empty `result.Output`, the task body remains unchanged — operators see only frontmatter changes and no diagnostic information.
- The two sibling generators (`fallbackContentGenerator`, `sectionContentGenerator`) correctly write a `## Failure` section on `AgentStatusFailed`. The passthrough generator does not.
- This was found in prod 2026-05-12: pr-reviewer agent's `pr-plan` step failed with Claude API 401, returned `Status: failed` with the message; the task body remained "title + URL only" — operators had to dig in pod logs to discover the cause.
- Fix shape: extend `passthroughContentGenerator.Generate` to write `## Failure` on `AgentStatusFailed` and `AgentStatusNeedsInput`, mirroring the existing logic in the other two generators. Add table-driven tests covering every `AgentStatus × generator` combination so this can't regress.
- No new public API. No frontmatter contract change. Internal generator-only fix.
</summary>

<objective>
Every non-success agent result MUST leave an operator-readable explanation in the task body, regardless of which `ContentGenerator` the agent uses. Specifically: `passthroughContentGenerator` (used by pr-reviewer and other `lib.NewAgent` / `lib.StepRunner` agents) must write a `## Failure` section containing `result.Message` whenever `result.Status` is `AgentStatusFailed` or `AgentStatusNeedsInput`, identical in shape to what `fallbackContentGenerator` and `sectionContentGenerator` already do. A new table-driven test in `content-generator_test.go` exercises every generator × every non-success status combination and asserts the body contains the operator message.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions (Ginkgo/Gomega tests, multi-module mono-repo).

Code locations (verify before editing — line numbers pinned to current HEAD):

- `lib/delivery/content-generator.go:33-48` — `fallbackContentGenerator.Generate`: writes `## Failure` on `AgentStatusFailed` ✓ correct
- `lib/delivery/content-generator.go:80-91` — `buildFailureSection(result)`: shared helper, renders `## Failure\n\n- **Reason:** <message>`
- `lib/delivery/content-generator.go:128-134` — `passthroughContentGenerator.Generate`: **BUG** — does not handle `AgentStatusFailed`, returns `result.Output` verbatim (empty on early-step failures)
- `lib/delivery/content-generator.go:151-166` — `sectionContentGenerator.Generate`: writes `## Failure` on `AgentStatusFailed` ✓ correct (mirrors fallback)
- `lib/delivery/content-generator.go:50-75` — `applyStatusFrontmatter`: maps `AgentStatusFailed` and `AgentStatusNeedsInput` both to `phase: human_review` / `status: in_progress` — confirms failure and needs_input share the operator-review routing

Test file: `lib/delivery/content-generator_test.go`. Existing `Describe` blocks cover `FallbackContentGenerator` (line 18) and `NewSectionContentGenerator` (line 220). **There is NO `Describe` block for `PassthroughContentGenerator`** — that's why the bug slipped through. The existing tests are Ginkgo/Gomega style; match that style.

The `agentlib.AgentResultInfo` struct fields used: `Status`, `Output`, `Message`. The `agentlib.AgentStatus` enum values: `AgentStatusDone`, `AgentStatusFailed`, `AgentStatusNeedsInput`, `AgentStatusInProgress`.

Real-world failure that motivated this fix (from prod logs 2026-05-12 21:30 UTC):

```
I0512 21:30:41.075869 claude-runner.go:137] type(text): Failed to authenticate. API Error: 401 Invalid authentication credentials
I0512 21:30:41.539180 result-deliverer.go:196] publishing task update for taskID=712b7974... status=failed
{"Status":"failed","NextPhase":"","Message":"pr-plan claude run failed: claude CLI failed: : exit status 1","ContinueToNext":false}
```

After this published, the task file `tasks/PR Review github - bborbe-trading - 122 - ...md` had updated frontmatter (`phase: human_review`, `status: in_progress`) but the body remained `# PR Review: ...\n\n\nhttps://github.com/bborbe/trading/pull/122` — no `## Failure` section, no operator-readable trace of the Claude 401.

Test pattern in use here: Ginkgo's `DescribeTable` + `Entry` for table-driven tests. Look at existing `Describe` blocks in `content-generator_test.go` (line 18 `FallbackContentGenerator`, line 220 `NewSectionContentGenerator`) for the project's Ginkgo/Gomega style — match that.
</context>

<requirements>

## 1. Add table-driven test in `lib/delivery/content-generator_test.go`

Use Ginkgo's `DescribeTable` + `Entry` to exercise every `(generator, status)` combination.

The test asserts that for every non-success status across all three generators, the produced body contains `result.Message` somewhere — protecting against the exact regression we're fixing.

```go
var _ = DescribeTable("every ContentGenerator surfaces result.Message on non-success status",
    func(generatorName string, generator delivery.ContentGenerator, status agentlib.AgentStatus) {
        ctx := context.Background()
        originalContent := "---\nstatus: in_progress\n---\nTags: [[Task]]\n"
        result := agentlib.AgentResultInfo{
            Status:  status,
            Output:  "", // simulate early-step failure: no output produced
            Message: "diagnostic message for " + string(status),
        }
        out, err := generator.Generate(ctx, originalContent, result)
        Expect(err).NotTo(HaveOccurred())
        Expect(out).To(ContainSubstring("diagnostic message for "+string(status)),
            "%s on status=%s MUST surface result.Message in the body, got:\n%s",
            generatorName, status, out)
    },
    // Three generators × two non-success statuses = 6 entries.
    Entry("fallback / failed",      "fallback",      delivery.NewFallbackContentGenerator(),        agentlib.AgentStatusFailed),
    Entry("fallback / needs_input", "fallback",      delivery.NewFallbackContentGenerator(),        agentlib.AgentStatusNeedsInput),
    Entry("section / failed",       "section",       delivery.NewSectionContentGenerator("## Plan"), agentlib.AgentStatusFailed),
    Entry("section / needs_input",  "section",       delivery.NewSectionContentGenerator("## Plan"), agentlib.AgentStatusNeedsInput),
    Entry("passthrough / failed",      "passthrough", delivery.NewPassthroughContentGenerator(),    agentlib.AgentStatusFailed),
    Entry("passthrough / needs_input", "passthrough", delivery.NewPassthroughContentGenerator(),    agentlib.AgentStatusNeedsInput),
)
```

Before the fix in §3 lands, **the two `passthrough` entries MUST fail** — this proves the test catches the regression. Run `cd lib && make test` once after adding the test (and before any code change) to confirm the expected failure shape, then proceed with §3.

The four non-passthrough entries (`fallback / failed`, `fallback / needs_input`, `section / failed`, `section / needs_input`) MUST pass at this stage — that's the regression-guard for the already-working generators. `fallback` and `section` route NeedsInput through `buildMinimalResultSection` which embeds `result.Message` under `## Result`, so the `ContainSubstring(message)` assertion holds regardless of the heading.

Confirm the import block of the test file already has `"github.com/onsi/ginkgo/v2"` (for `DescribeTable`, `Entry`) and `"github.com/onsi/gomega"` — both are standard for this file. Add `"context"` and `agentlib`/`delivery` imports if not already present.

## 2. Add focused `Describe("PassthroughContentGenerator")` block in `content-generator_test.go`

Mirrors the structure of the existing `Describe("FallbackContentGenerator")` block (line 18) and `Describe("NewSectionContentGenerator")` block (line 220). Cover the same status surface as the table-driven test plus:

- `It("writes ## Failure with result.Message on AgentStatusFailed", func() { ... })`
- `It("writes ## Failure with result.Message on AgentStatusNeedsInput", func() { ... })`
- `It("returns result.Output verbatim with status=completed frontmatter on AgentStatusDone", func() { ... })`
- `It("preserves phase from input and keeps status=in_progress on AgentStatusInProgress", func() { ... })`

Body assertions for the failure cases use `ContainSubstring("## Failure")` AND `ContainSubstring(result.Message)`.

## 3. Fix `passthroughContentGenerator.Generate` in `lib/delivery/content-generator.go`

Replace the current 7-line body with a branch that mirrors `fallbackContentGenerator` for failure cases:

```go
func (g *passthroughContentGenerator) Generate(
    _ context.Context,
    _ string,
    result agentlib.AgentResultInfo,
) (string, error) {
    updated := applyStatusFrontmatter(result.Output, result.Status)
    if result.Status == agentlib.AgentStatusFailed || result.Status == agentlib.AgentStatusNeedsInput {
        // result.Output is unreliable on early-step failures — agents return
        // status=failed/needs_input WITHOUT having written anything to Output.
        // Surface result.Message via the shared ## Failure section so operators
        // can diagnose without log archaeology. Mirrors fallback + section generators.
        return ReplaceOrAppendSection(updated, "## Failure", buildFailureSection(result)), nil
    }
    return updated, nil
}
```

Note: the passthrough generator's first argument is `originalContent` (currently unused — see the underscore). It must STAY unused — the agent's `result.Output` is the source of truth for happy-path content per the doc comment at line 109-121. The failure-section branch uses `result.Output` as the base (which is `""` on early failure), then appends `## Failure` on top.

**Important:** keep the doc comment at line 109-121 accurate. Add a line like "On AgentStatusFailed or AgentStatusNeedsInput, the passthrough generator splices a `## Failure` section into `result.Output` so operators always see the failure reason — without this, early-step failures (where Output is empty) leave a body-less task."

## 4. Add CHANGELOG entry

In `CHANGELOG.md` at the repo root, add an entry under `## Unreleased` (create the section if missing — insert immediately before the first versioned section):

```markdown
- fix(lib/delivery): `passthroughContentGenerator` now writes a `## Failure` body section on `AgentStatusFailed` and `AgentStatusNeedsInput`, mirroring the existing behavior of `fallbackContentGenerator` and `sectionContentGenerator`. Previously, agents using the passthrough generator (e.g. pr-reviewer) lost the failure reason whenever `result.Output` was empty — operators had to dig through TTL-cleaned pod logs to diagnose. Live incident: pr-reviewer task `712b7974-cfbf-5999-a1fc-6946207e21c3` on 2026-05-12 — Claude API 401 → empty task body. Adds table-driven regression test covering every generator × non-success status.
```

</requirements>

<constraints>

- Follow `CLAUDE.md` at the repo root: errors lib, no `fmt.Errorf`. Test files may use `context.Background()` in `BeforeEach` — that's the established pattern, see existing tests.
- The shared `buildFailureSection(result)` helper at line 80 is the only acceptable way to render the body — do NOT inline the format string in the generator. Helper-reuse is the regression guard.
- Do NOT change the public `ContentGenerator` interface signature. Internal-only fix.
- Do NOT change `applyStatusFrontmatter` — phase/status routing for these statuses is already correct.
- Do NOT touch `result_publisher.go` or any K8s-Job failure path (`PublishFailure`); that's a separate code path with its own `## Failure` write at `task/executor/pkg/result_publisher.go:85-98` — leave it alone.
- Tests MUST run via `make test` in `lib/` (not the repo root). Verify: `cd lib && make test` passes.
- Run `make precommit` in `lib/` before considering done.
- Match existing Ginkgo style — `var _ = Describe(...)`, `BeforeEach`/`JustBeforeEach`, `Expect(...).To(...)`. Don't invent a new test framework.
- Don't add new dependencies. Stdlib `strings` is already imported.

</constraints>

<verification>

```bash
cd lib && make precommit
```

Expected: zero failures, all tests pass including the new table-driven harness and the new `Describe("PassthroughContentGenerator")` block. The `DescribeTable` should report 6 passing entries.

Manual verification of the fix shape:

```bash
grep -A 8 "func (g \*passthroughContentGenerator) Generate" lib/delivery/content-generator.go
```

Expected: function body contains `if result.Status == agentlib.AgentStatusFailed || result.Status == agentlib.AgentStatusNeedsInput {` branch and a call to `buildFailureSection(result)`.

Manual verification of the regression-guard test:

```bash
grep -A 3 "passthrough / failed\|passthrough / needs_input" lib/delivery/content-generator_test.go
```

Expected: both `Entry` lines present in the `DescribeTable` block.

Note: function names and file paths in this prompt are stable, but line numbers may drift between the time this prompt was written (2026-05-12 22:00 UTC, against agent HEAD `f4aed27`) and execution. Re-verify line numbers before editing — the function/symbol names are authoritative.

</verification>
