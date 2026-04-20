---
status: approved
created: "2026-04-20T16:45:06Z"
queued: "2026-04-20T16:45:06Z"
---

<summary>
- `ReplaceOrAppendSection` becomes correct for inputs that contain **multiple** existing `## <heading>` sections: all matches are coalesced into a single replaced section
- Current implementation only replaces the FIRST occurrence; a second section in the input survives verbatim. This produced the observed duplicated `## Result` in 2026-04-20 smoke writebacks
- Internals split into three composable helpers (`HasSection`, `ReplaceSection`, `AppendSection`); `ReplaceOrAppendSection` becomes a thin dispatcher, public API unchanged
- Heading match tightened to line-start (prevents spurious matches on substrings like `## Results`)
- Ginkgo specs added for each new helper and for end-to-end coalesce scenarios (0/1/2/3 existing sections, substring non-match, unrelated-section preservation)
- `cd lib && make precommit` passes
</summary>

<objective>
Fix the duplicate-`## Result` bug observed on 2026-04-20 smoke validation runs. Every successful task ended with two `## Result` headers in the writeback file instead of one. Root cause: `lib/delivery/markdown.go:ReplaceOrAppendSection` finds the first occurrence via `strings.Index`, replaces up to the next `## ` heading, and leaves any additional same-named sections intact. Fix the function so that for any input, the output contains exactly one section matching `heading`, regardless of input shape.

Split the internals into three exported helpers (`HasSection`, `ReplaceSection`, `AppendSection`) so each piece is testable in isolation. `ReplaceOrAppendSection` keeps its signature and becomes `if HasSection { ReplaceSection } else { AppendSection }`.

Scope is strictly this bug + the helper split needed to implement it cleanly. No renames of unrelated code, no call-site changes, no cleanup elsewhere.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md`
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`

**Observed bug (2026-04-20 smoke run):**

Prod controller log (`agent-task-controller-0`) shows ONE agent Kafka event per task, with content length grown by ~2× a Result section. Final file `smoke-trade-analysis-prod-2026-04-20.md`:

```
## Result

## Result

**Status:** done
**Message:** no trades found in date range

**Message:** no trades found in date range
```

Controller writeback (`task/controller/pkg/result/result_writer.go:112`) replaces the full file atomically — so the duplication originates upstream, inside the agent's content generator. `lib/delivery/content-generator.go:60` calls `ReplaceOrAppendSection(updated, "## Result", section)` exactly once per delivery. The `updated` input must therefore already contain a `## Result`, and the current function only replaces the first one.

**Files to read before editing:**
- `lib/delivery/markdown.go:47-68` — current buggy `ReplaceOrAppendSection`
- `lib/delivery/markdown_test.go:42-72` — existing Ginkgo specs; extend
- `lib/delivery/content-generator.go:60` — single call site, stays unchanged

**Current behaviour (buggy):**

```go
idx := strings.Index(content, heading)       // finds FIRST occurrence only
if idx == -1 { return content + newSection } // append path
after := content[idx+len(heading):]
nextHeading := strings.Index(after, "\n## ") // second `## Result` counts as "next heading"
// → replaces from first to second, but preserves everything from second onward
```

With input `"## Result\n\n## Result\n\nstuff"`, the function replaces from the first `## Result` up to (but not including) the second `## Result`, then keeps the second section verbatim. Output: new-section + old-second-section.

**Required new behaviour:**

For any input, the output contains exactly **one** section matching `heading`. Headings are matched at line start only — `## Result` must not match `## Results (summary)`.
</context>

<requirements>

1. **Rewrite `lib/delivery/markdown.go` as four exported functions**

   Replace the existing `ReplaceOrAppendSection` body. Public signature of `ReplaceOrAppendSection` stays identical. Three new exported helpers are added:

   Reference implementation:
   ```go
   // HasSection reports whether content contains at least one line that
   // matches heading at line-start. A match requires the line to equal
   // heading exactly, or to start with heading followed by a space or tab.
   // Substrings like "## Results" do NOT match "## Result".
   func HasSection(content, heading string) bool {
       for _, line := range strings.Split(content, "\n") {
           if isSectionStart(line, heading) {
               return true
           }
       }
       return false
   }

   // AppendSection appends newSection to content, ensuring a single blank
   // line separator and exactly one trailing newline in the result.
   func AppendSection(content, newSection string) string {
       trimmed := strings.TrimRight(content, "\n")
       return trimmed + "\n\n" + strings.TrimRight(newSection, "\n") + "\n"
   }

   // ReplaceSection removes every section whose heading line matches
   // heading (line-start match) and appends newSection once at the end.
   // If no section matches, behaves identically to AppendSection.
   func ReplaceSection(content, heading, newSection string) string {
       lines := strings.Split(content, "\n")
       kept := make([]string, 0, len(lines))
       skipping := false
       for _, line := range lines {
           if isSectionStart(line, heading) {
               skipping = true
               continue
           }
           if skipping && strings.HasPrefix(line, "## ") {
               skipping = false
           }
           if !skipping {
               kept = append(kept, line)
           }
       }
       return AppendSection(strings.Join(kept, "\n"), newSection)
   }

   // ReplaceOrAppendSection replaces every section matching heading with
   // newSection, or appends newSection if no matching section exists.
   // The result always contains exactly one section with this heading.
   func ReplaceOrAppendSection(content, heading, newSection string) string {
       if HasSection(content, heading) {
           return ReplaceSection(content, heading, newSection)
       }
       return AppendSection(content, newSection)
   }

   func isSectionStart(line, heading string) bool {
       if !strings.HasPrefix(line, heading) {
           return false
       }
       if len(line) == len(heading) {
           return true
       }
       c := line[len(heading)]
       return c == ' ' || c == '\t'
   }
   ```

   Notes:
   - `ReplaceOrAppendSection` public signature unchanged → `lib/delivery/content-generator.go:60` keeps working without edits
   - `isSectionStart` stays unexported (package-private helper)
   - Delete any now-unused helpers from the old implementation
   - Add GoDoc comments on each exported function as shown above

2. **Extend `lib/delivery/markdown_test.go` Ginkgo suite**

   Add three new top-level `Describe` blocks for the helpers, plus new specs inside the existing `Describe("ReplaceOrAppendSection", ...)` block. Place all additions after the current last `It` at line 71.

   ```go
   Describe("HasSection", func() {
       It("returns true for an exact heading line", func() {
           Expect(delivery.HasSection("## Result\n", "## Result")).To(BeTrue())
       })
       It("returns true when heading is followed by space", func() {
           Expect(delivery.HasSection("## Result stuff\n", "## Result")).To(BeTrue())
       })
       It("returns true when heading is followed by tab", func() {
           Expect(delivery.HasSection("## Result\ttrailing\n", "## Result")).To(BeTrue())
       })
       It("returns false for a heading substring (no word boundary)", func() {
           Expect(delivery.HasSection("## Results (summary)\n", "## Result")).To(BeFalse())
       })
       It("returns false for empty content", func() {
           Expect(delivery.HasSection("", "## Result")).To(BeFalse())
       })
       It("returns true when heading appears after other content", func() {
           Expect(delivery.HasSection("prefix\n\n## Result\n", "## Result")).To(BeTrue())
       })
   })

   Describe("AppendSection", func() {
       It("appends to empty content", func() {
           result := delivery.AppendSection("", "## Result\n\nnew\n")
           Expect(result).To(Equal("\n\n## Result\n\nnew\n"))
       })
       It("appends with single blank-line separator regardless of trailing newlines", func() {
           result := delivery.AppendSection("body\n\n\n", "## Result\n\nnew\n")
           Expect(result).To(Equal("body\n\n## Result\n\nnew\n"))
       })
       It("normalises trailing newlines to exactly one", func() {
           result := delivery.AppendSection("body", "## Result\n\nnew\n\n\n")
           Expect(strings.HasSuffix(result, "\n\n")).To(BeFalse())
           Expect(strings.HasSuffix(result, "\n")).To(BeTrue())
       })
   })

   Describe("ReplaceSection", func() {
       It("coalesces two existing sections with the same heading into one", func() {
           content := "prefix\n\n## Result\n\n## Result\n\n**Message:** stale\n"
           result := delivery.ReplaceSection(content, "## Result", "## Result\n\n**Message:** fresh\n")
           Expect(strings.Count(result, "## Result")).To(Equal(1))
           Expect(result).To(ContainSubstring("**Message:** fresh"))
           Expect(result).NotTo(ContainSubstring("**Message:** stale"))
       })
       It("coalesces three existing sections into one", func() {
           content := "body\n\n## Result\n\nA\n\n## Result\n\nB\n\n## Result\n\nC\n"
           result := delivery.ReplaceSection(content, "## Result", "## Result\n\nX\n")
           Expect(strings.Count(result, "## Result")).To(Equal(1))
           Expect(result).To(ContainSubstring("X"))
           Expect(result).NotTo(ContainSubstring("A"))
           Expect(result).NotTo(ContainSubstring("B"))
           Expect(result).NotTo(ContainSubstring("C"))
       })
       It("preserves unrelated sections when coalescing duplicates", func() {
           content := "## Details\n\nd1\n\n## Result\n\nA\n\n## Notes\n\nn1\n\n## Result\n\nB\n"
           result := delivery.ReplaceSection(content, "## Result", "## Result\n\nX\n")
           Expect(strings.Count(result, "## Result")).To(Equal(1))
           Expect(result).To(ContainSubstring("## Details"))
           Expect(result).To(ContainSubstring("d1"))
           Expect(result).To(ContainSubstring("## Notes"))
           Expect(result).To(ContainSubstring("n1"))
           Expect(result).To(ContainSubstring("X"))
       })
       It("does not treat a heading substring as a match", func() {
           content := "## Results (summary)\n\nr1\n"
           result := delivery.ReplaceSection(content, "## Result", "## Result\n\nnew\n")
           Expect(result).To(ContainSubstring("## Results (summary)"))
           Expect(result).To(ContainSubstring("r1"))
           Expect(result).To(ContainSubstring("## Result\n\nnew"))
       })
   })
   ```

   Also add two end-to-end specs inside the existing `Describe("ReplaceOrAppendSection", ...)` block:

   ```go
   It("coalesces multiple existing sections into exactly one", func() {
       content := "prefix\n\n## Result\n\n## Result\n\nstale\n"
       result := delivery.ReplaceOrAppendSection(content, "## Result", "## Result\n\nfresh\n")
       Expect(strings.Count(result, "## Result")).To(Equal(1))
       Expect(result).To(ContainSubstring("fresh"))
       Expect(result).NotTo(ContainSubstring("stale"))
   })

   It("does not match a heading substring", func() {
       content := "## Results (summary)\n\nr1\n"
       result := delivery.ReplaceOrAppendSection(content, "## Result", "## Result\n\nnew\n")
       Expect(result).To(ContainSubstring("## Results (summary)"))
       Expect(result).To(ContainSubstring("## Result\n\nnew"))
   })
   ```

   Add `"strings"` to the test imports if not already present.

3. **Existing three specs must pass unchanged**

   - "appends section when heading not found"
   - "replaces existing section"
   - "replaces section when not the last section"

   These currently use `ContainSubstring` assertions and must continue to pass without modification. Do NOT edit the existing specs.

4. **Update CHANGELOG.md at repo root**

   Append under `## Unreleased` (create the section immediately above the first `## v...` heading if missing):
   ```markdown
   - fix: ReplaceOrAppendSection now coalesces multiple existing `## Result` sections into exactly one, fixing duplicate sections observed in 2026-04-20 smoke writebacks
   - refactor: split markdown section helpers into HasSection, AppendSection, ReplaceSection (ReplaceOrAppendSection now composes them); public API unchanged
   ```

</requirements>

<constraints>
- Scope: only `lib/delivery/markdown.go`, `lib/delivery/markdown_test.go`, `CHANGELOG.md`
- `ReplaceOrAppendSection` public signature MUST NOT change
- Do NOT rename any other field, function, or type
- Do NOT touch `lib/delivery/content-generator.go` (single call site, keeps working via unchanged `ReplaceOrAppendSection`)
- Do NOT touch `task/executor/` or `task/controller/`
- Do NOT touch `mocks/` — no mocks should need regeneration for this change
- Do NOT commit — dark-factory handles git
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` for any error wrapping — never `fmt.Errorf`
- Heading match must be line-start only (whole-line prefix followed by end-of-line, space, or tab)
- `isSectionStart` stays unexported
- All existing tests must pass; new specs must pass
</constraints>

<verification>

Verify the old single-pass algorithm is gone:
```bash
grep -n 'strings.Index(content, heading)' lib/delivery/markdown.go
```
Must return no matches.

Verify the four exported functions exist:
```bash
grep -nE '^func (HasSection|AppendSection|ReplaceSection|ReplaceOrAppendSection)\b' lib/delivery/markdown.go
```
Must show all four.

Verify new test descriptions exist:
```bash
grep -nE 'HasSection|AppendSection|ReplaceSection|coalesces|heading substring' lib/delivery/markdown_test.go
```
Must show the new Describe blocks and specs.

Run the unit tests:
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
