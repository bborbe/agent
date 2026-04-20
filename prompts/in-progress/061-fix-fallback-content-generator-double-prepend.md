---
status: committing
summary: 'Fixed fallbackContentGenerator.Generate to pass AgentResultInfo.Output verbatim when non-empty, eliminating double ## Result heading and duplicated **Message:** line; added regression specs; updated CHANGELOG with v0.45.2.'
container: agent-061-fix-fallback-content-generator-double-prepend
dark-factory-version: v0.131.0-1-gb3e2949
created: "2026-04-20T19:34:32Z"
queued: "2026-04-20T19:34:32Z"
started: "2026-04-20T19:36:34Z"
---

<summary>
- `lib/delivery/content-generator.go:fallbackContentGenerator.Generate` double-prepends the `## Result` heading and duplicates `**Message:**` because it treats `AgentResultInfo.Output` as a body-fragment while every caller already passes a full section (heading + Status + Message, rendered by `BuildResultSection`)
- Fix: trust `result.Output` verbatim when non-empty; build a minimal section only when empty; move status→frontmatter mapping into a private helper for readability
- Internal split only — public `ContentGenerator` interface, `AgentResultInfo` struct, `NewFallbackContentGenerator` factory, and call sites all unchanged
- Adds Ginkgo specs for the 2026-04-20b regression (duplicate heading + duplicate Message) and the empty-Output fallback path
- `cd lib && make precommit` passes
</summary>

<objective>
Fix the duplicate `## Result` + duplicate `**Message:**` written back on every completed task (observed 2026-04-20 smoke runs, reproduced 2026-04-20b after lib/v0.45.1 dedup fix shipped). Root cause is NOT in `ReplaceOrAppendSection` — that fix was defense-in-depth and is correct. The real bug is a **contract mismatch** between `lib/claude/result-deliverer.go` and `lib/delivery/content-generator.go`:

- `resultDelivererAdapter.DeliverResult` (`lib/claude/result-deliverer.go:35`) sets `Output: result.RenderResultSection()`
- `RenderResultSection` → `BuildResultSection` (`lib/claude/result-deliverer.go:46`) produces the **full** section: `"## Result\n\n**Status:** done\n**Message:** foo\n"`
- `fallbackContentGenerator.Generate` (`lib/delivery/content-generator.go:48-58`) then prepends `"## Result\n\n"` AGAIN and appends `"**Message:** " + result.Message + "\n"` AGAIN

Observed smoke writeback (`smoke-claude-dev-2026-04-20b.md`):
```
## Result

## Result

**Status:** done
**Message:** hello from dev

**Message:** hello from dev
```

The lib/v0.45.1 `ReplaceOrAppendSection` fix correctly dedups `## Result` inside the existing `originalContent`, but cannot dedup duplication introduced within the **newly built** section passed to it.

Fix by aligning the contract: `AgentResultInfo.Output`, when non-empty, is the complete Result section. `fallbackContentGenerator` passes it through verbatim. If `Output` is empty, the generator synthesises a minimal section from `Status`/`Message`.

Scope is strictly this bug plus the internal helper split needed to make the fix readable. No public API changes, no call-site edits, no refactor of other code.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega

**Files to read before editing:**
- `lib/delivery/content-generator.go` — current buggy `Generate`
- `lib/delivery/content-generator_test.go` — existing Ginkgo specs; update + extend
- `lib/delivery/status.go` — `AgentResultInfo` struct (unchanged)
- `lib/claude/result-deliverer.go:32-38` — the single real-world caller; sets `Output` to full section string
- `lib/claude/result-deliverer.go:46-66` — `BuildResultSection` format (what `Output` looks like in practice)

**Current buggy code (`lib/delivery/content-generator.go:27-62`):**

```go
func (g *fallbackContentGenerator) Generate(
    _ context.Context,
    originalContent string,
    result AgentResultInfo,
) (string, error) {
    updated := originalContent

    switch result.Status {
    case AgentStatusDone:
        updated = SetFrontmatterField(updated, "status", "completed")
        updated = SetFrontmatterField(updated, "phase", "done")
    case AgentStatusNeedsInput:
        updated = SetFrontmatterField(updated, "status", "in_progress")
        updated = SetFrontmatterField(updated, "phase", "human_review")
    default:
        updated = SetFrontmatterField(updated, "status", "in_progress")
        updated = SetFrontmatterField(updated, "phase", "ai_review")
    }

    var section strings.Builder
    section.WriteString("## Result\n\n")          // BUG: heading already in Output
    if result.Output != "" {
        section.WriteString(result.Output)
        section.WriteString("\n")
    }
    if result.Message != "" {
        section.WriteString("**Message:** ")      // BUG: Message already in Output
        section.WriteString(result.Message)
        section.WriteString("\n")
    }

    updated = ReplaceOrAppendSection(updated, "## Result", section.String())
    return updated, nil
}
```

**What `Output` actually contains in prod** (from `BuildResultSection` for status=done, message="hello from dev"):
```
## Result

**Status:** done
**Message:** hello from dev
```

So the `section` built above ends up as:
```
## Result

## Result

**Status:** done
**Message:** hello from dev

**Message:** hello from dev
```
</context>

<requirements>

1. **Rewrite `lib/delivery/content-generator.go`**

   Replace the `fallbackContentGenerator.Generate` body. Extract status→frontmatter mapping and empty-Output fallback section building into two private helpers. Public API is unchanged.

   Reference implementation:

   ```go
   func (g *fallbackContentGenerator) Generate(
       _ context.Context,
       originalContent string,
       result AgentResultInfo,
   ) (string, error) {
       updated := applyStatusFrontmatter(originalContent, result.Status)
       section := result.Output
       if section == "" {
           section = buildMinimalResultSection(result)
       }
       return ReplaceOrAppendSection(updated, "## Result", section), nil
   }

   // applyStatusFrontmatter updates status+phase frontmatter fields based on agent result status.
   func applyStatusFrontmatter(content string, status AgentStatus) string {
       switch status {
       case AgentStatusDone:
           content = SetFrontmatterField(content, "status", "completed")
           content = SetFrontmatterField(content, "phase", "done")
       case AgentStatusNeedsInput:
           // task-level failure: agent ran cleanly but task is impossible/underspecified.
           // Route straight to human_review — retrying a semantically-wrong task wastes compute.
           content = SetFrontmatterField(content, "status", "in_progress")
           content = SetFrontmatterField(content, "phase", "human_review")
       default: // failed and any other status — infra failure, eligible for retry
           content = SetFrontmatterField(content, "status", "in_progress")
           content = SetFrontmatterField(content, "phase", "ai_review")
       }
       return content
   }

   // buildMinimalResultSection renders a fallback ## Result block when AgentResultInfo.Output is empty.
   // Callers that supply a non-empty Output are trusted to provide the full section verbatim.
   func buildMinimalResultSection(result AgentResultInfo) string {
       var b strings.Builder
       b.WriteString("## Result\n\n")
       b.WriteString("**Status:** ")
       b.WriteString(string(result.Status))
       b.WriteString("\n")
       if result.Message != "" {
           b.WriteString("**Message:** ")
           b.WriteString(result.Message)
           b.WriteString("\n")
       }
       return b.String()
   }
   ```

   Notes:
   - `ContentGenerator` interface, `NewFallbackContentGenerator` factory, `fallbackContentGenerator` type — unchanged
   - Both helpers stay unexported (package-private)
   - Keep the existing `// task-level failure ...` and `// failed and any other status ...` comments on the status switch — they explain non-obvious routing logic
   - `strings` import stays (used by `buildMinimalResultSection`)

2. **Update `lib/delivery/content-generator_test.go`**

   The existing specs pass `Output` WITHOUT the `## Result` heading in two cases (`"backtest complete"` at line 71, `"new result content"` at line 85). Under the new contract these strings are passed verbatim as the full section, so the `## Result` heading comes from `ReplaceOrAppendSection`'s append path (no existing heading in the original content). The existing assertions (`ContainSubstring("## Result")`, `ContainSubstring("backtest complete")`) still pass.

   For the "replaces the existing section" spec (line 80-92): the original contains `"## Result\n\nOld result.\n"` — `ReplaceSection` strips it and appends `"new result content"` verbatim. The assertion `ContainSubstring("## Result")` still passes because the original `## Result` heading appears in `originalContent` before ReplaceSection matches on it (ReplaceSection checks `isSectionStart` — the new section has no `## Result` heading at line start, but `ContainSubstring` matches any substring; we need to adjust this spec to provide a valid full-section Output).

   **Update the three specs where `Output` lacks the heading** to match the real-world contract (Output = full section):

   ```go
   // line 67-78 — empty original content
   Context("with empty original content", func() {
       It("returns a ## Result section without frontmatter", func() {
           result := delivery.AgentResultInfo{
               Status: delivery.AgentStatusDone,
               Output: "## Result\n\nbacktest complete\n",
           }
           generated, err := generator.Generate(ctx, "", result)
           Expect(err).NotTo(HaveOccurred())
           Expect(generated).To(ContainSubstring("## Result"))
           Expect(generated).To(ContainSubstring("backtest complete"))
       })
   })

   // line 80-92 — existing ## Result section
   Context("with existing ## Result section", func() {
       It("replaces the existing section", func() {
           original := "---\ntitle: My Task\n---\n\n## Task\n\nRun a backtest.\n\n## Result\n\nOld result.\n"
           result := delivery.AgentResultInfo{
               Status: delivery.AgentStatusDone,
               Output: "## Result\n\nnew result content\n",
           }
           generated, err := generator.Generate(ctx, original, result)
           Expect(err).NotTo(HaveOccurred())
           Expect(generated).NotTo(ContainSubstring("Old result."))
           Expect(generated).To(ContainSubstring("new result content"))
       })
   })

   // line 94-104 — markdown shape smoke test
   It("output is valid markdown with frontmatter when original has frontmatter", func() {
       original := "---\ntitle: Test\n---\n\nBody.\n"
       generated, err := generator.Generate(
           ctx,
           original,
           delivery.AgentResultInfo{Status: delivery.AgentStatusDone, Output: "## Result\n\nresult\n"},
       )
       Expect(err).NotTo(HaveOccurred())
       Expect(generated).To(HavePrefix("---"))
       Expect(generated[3:]).To(ContainSubstring("---"))
   })
   ```

   Also update the **failed-result spec** (line 41-52) — Output is empty, so `buildMinimalResultSection` synthesises the block. No change to the spec body needed; the existing assertions still pass (`phase: ai_review`, `timeout expired`).

   Also update the **needs_input spec** (line 54-64) — same situation, empty Output, synthesised block. No change needed.

   **Add new specs** for the regression + empty-Output behaviour, immediately after the existing last spec:

   ```go
   Context("2026-04-20b regression", func() {
       It("does NOT double the ## Result heading when Output contains it", func() {
           original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Details\n\nd\n"
           result := delivery.AgentResultInfo{
               Status:  delivery.AgentStatusDone,
               Output:  "## Result\n\n**Status:** done\n**Message:** hello from dev\n",
               Message: "hello from dev",
           }
           generated, err := generator.Generate(ctx, original, result)
           Expect(err).NotTo(HaveOccurred())
           Expect(strings.Count(generated, "## Result")).To(Equal(1))
       })

       It("does NOT duplicate the **Message:** line when Output already contains it", func() {
           original := "---\ntitle: My Task\n---\n"
           result := delivery.AgentResultInfo{
               Status:  delivery.AgentStatusDone,
               Output:  "## Result\n\n**Status:** done\n**Message:** hello from dev\n",
               Message: "hello from dev",
           }
           generated, err := generator.Generate(ctx, original, result)
           Expect(err).NotTo(HaveOccurred())
           Expect(strings.Count(generated, "**Message:** hello from dev")).To(Equal(1))
       })

       It("replaces an existing ## Result section without duplication on re-run", func() {
           original := "---\ntitle: My Task\n---\n\n## Result\n\n**Status:** done\n**Message:** old\n"
           result := delivery.AgentResultInfo{
               Status:  delivery.AgentStatusDone,
               Output:  "## Result\n\n**Status:** done\n**Message:** new\n",
               Message: "new",
           }
           generated, err := generator.Generate(ctx, original, result)
           Expect(err).NotTo(HaveOccurred())
           Expect(strings.Count(generated, "## Result")).To(Equal(1))
           Expect(strings.Count(generated, "**Message:** new")).To(Equal(1))
           Expect(generated).NotTo(ContainSubstring("**Message:** old"))
       })
   })

   Context("with empty Output (fallback minimal section)", func() {
       It("synthesises a ## Result block from Status+Message when Output is empty", func() {
           original := "---\ntitle: My Task\n---\n"
           result := delivery.AgentResultInfo{
               Status:  delivery.AgentStatusFailed,
               Output:  "",
               Message: "container OOMKilled",
           }
           generated, err := generator.Generate(ctx, original, result)
           Expect(err).NotTo(HaveOccurred())
           Expect(generated).To(ContainSubstring("## Result"))
           Expect(generated).To(ContainSubstring("**Status:** failed"))
           Expect(generated).To(ContainSubstring("**Message:** container OOMKilled"))
           Expect(strings.Count(generated, "## Result")).To(Equal(1))
       })

       It("omits **Message:** when both Output and Message are empty", func() {
           original := "---\ntitle: My Task\n---\n"
           result := delivery.AgentResultInfo{
               Status: delivery.AgentStatusDone,
           }
           generated, err := generator.Generate(ctx, original, result)
           Expect(err).NotTo(HaveOccurred())
           Expect(generated).To(ContainSubstring("**Status:** done"))
           Expect(generated).NotTo(ContainSubstring("**Message:**"))
       })
   })
   ```

   Add `"strings"` to the test imports (currently imports only `"context"` and the Ginkgo/Gomega dot-imports plus the `delivery` package).

3. **Update CHANGELOG.md at repo root**

   Prepend a new version section `## v0.45.2` immediately above the existing `## v0.45.1` heading, matching the file's pure-version-heading convention:

   ```markdown
   ## v0.45.2

   - fix: fallbackContentGenerator.Generate now trusts AgentResultInfo.Output verbatim when non-empty, eliminating the double `## Result` heading and duplicated `**Message:**` line observed in 2026-04-20b smoke writebacks
   - refactor: split fallbackContentGenerator internals into applyStatusFrontmatter + buildMinimalResultSection helpers; public ContentGenerator interface unchanged
   ```

   Do NOT introduce an `## Unreleased` section — the repo does not use that convention.

</requirements>

<constraints>
- Scope: only `lib/delivery/content-generator.go`, `lib/delivery/content-generator_test.go`, `CHANGELOG.md`
- `ContentGenerator` interface MUST NOT change
- `NewFallbackContentGenerator` return type MUST NOT change
- `AgentResultInfo` struct MUST NOT change
- Do NOT touch `lib/delivery/markdown.go` (already fixed in v0.45.1)
- Do NOT touch `lib/claude/result-deliverer.go` — that defines the contract this PR aligns to
- Do NOT touch `task/executor/` or `task/controller/`
- Do NOT touch `mocks/` — no mock regeneration needed
- Do NOT commit — dark-factory handles git
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` for any error wrapping — never `fmt.Errorf`
- `applyStatusFrontmatter` and `buildMinimalResultSection` stay unexported
- All existing specs in `content-generator_test.go` must pass (with the three small Output-contract updates listed above); new specs must pass
</constraints>

<verification>

Verify the old double-prepend code is gone:
```bash
grep -nE 'section.WriteString\("## Result' lib/delivery/content-generator.go
```
Must return no matches (the only remaining `"## Result\n\n"` literal is in `buildMinimalResultSection`).

Verify both helpers exist:
```bash
grep -nE '^func (applyStatusFrontmatter|buildMinimalResultSection)\b' lib/delivery/content-generator.go
```
Must show both.

Verify Generate is one-liner-ish (no more strings.Builder in Generate itself):
```bash
awk '/^func \(g \*fallbackContentGenerator\) Generate/,/^}/' lib/delivery/content-generator.go | grep -c 'strings.Builder'
```
Must print `0`.

Verify the regression specs exist:
```bash
grep -nE '2026-04-20b regression|fallback minimal section|double the ## Result heading' lib/delivery/content-generator_test.go
```
Must show the new `Context` blocks.

Run unit tests:
```bash
cd lib && make test
```
Must exit 0.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

</verification>
