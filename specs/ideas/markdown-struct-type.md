---
tags:
  - dark-factory
  - spec
status: idea
---

## Summary

- Replace the grab-bag of free functions in `lib/delivery/markdown.go` with a `Markdown` type that holds parsed frontmatter and a list of sections.
- Parse once, expose mutating methods (`SetFrontmatter`, `ReplaceOrAppendSection`, `AppendSection`), round-trip back to string via `String()`.
- Frontmatter preserves key order, quoting, and comments via `yaml.Node` — not a plain `map[string]string`.
- Section parsing is code-fence aware: `## heading` inside a fenced block is not a section boundary.
- Existing free functions become thin wrappers that delegate to `Markdown` methods, so callers migrate incrementally without a breaking change.

## Problem

`lib/delivery/markdown.go` exposes six free functions (`SetFrontmatterField`, `HasSection`, `AppendSection`, `ReplaceSection`, `ReplaceOrAppendSection`, `ParseMarkdownFrontmatter`) that each re-scan the full content string. Problems:

- **No shared parse.** A caller that reads frontmatter AND replaces a section scans the string twice. As multi-op writes grow (spec `atomic-content-edit-commands`), this waste compounds.
- **Weak section detection.** Line-start match on `"## "` treats code-fenced `## heading` as a real section. Agent task files containing shell snippets or markdown code examples can be silently corrupted.
- **Map-based frontmatter loses order.** `ParseMarkdownFrontmatter` returns `map[string]string` — key order, comments, quoting style are discarded. Any refactor that serializes back churns commit diffs.
- **Grown organically.** Every agent feature added one more function. Testing composition (set field + append section + replace section) requires wiring three string-in-string-out calls, all idempotent only by coincidence.

## Goal

A `Markdown` type that parses a task file once, exposes typed mutation methods, and round-trips back to an identical string when unchanged. Callers compose multi-step edits on one in-memory instance, then serialize once. Free-function API remains available as a compatibility wrapper.

## Non-goals

- Full markdown AST (goldmark, blackfriday). The section-boundary rule is still line-start `##`; we only add code-fence awareness.
- Preserving comments across frontmatter key deletions. Frontmatter comment preservation applies only to keys we don't touch.
- Generalizing beyond the current use cases (task files with YAML frontmatter + `##` sections). No support for TOML, `+++` delimiters, front-matter-less documents beyond pass-through.
- Replacing the atomic-command model in `atomic-content-edit-commands`. The command handlers still operate under the gitclient mutex; `Markdown` is their read-modify helper, not a replacement.

## Desired Behavior

1. `Parse(content string) (*Markdown, error)` returns a `Markdown` or an error if frontmatter delimiters are malformed. Missing frontmatter is not an error — returns a `Markdown` with an empty frontmatter node and the full body.
2. `m.GetFrontmatter(key string) (string, bool)` reads a key.
3. `m.SetFrontmatter(key, value string)` updates or inserts, preserving order of untouched keys.
4. `m.HasSection(heading string) bool` — exact line-start match, code-fence-aware.
5. `m.ReplaceOrAppendSection(heading, newSection string)` — replaces every existing matching section, or appends if none found.
6. `m.AppendSection(newSection string)` — unconditionally appends.
7. `m.String() string` round-trips: `Parse(Parse(x).String()).String() == Parse(x).String()`.
8. When no mutation is called between `Parse` and `String`, output equals input byte-for-byte.

## Constraints

**Must not change:**
- Free-function signatures in `lib/delivery/markdown.go` (wrappers stay).
- Behavior of existing free functions on inputs the current tests cover.
- `lib/delivery` package path and public surface.

**Must preserve:**
- Frontmatter key order when setting untouched keys.
- Frontmatter comments on untouched keys.
- Trailing newline conventions (current: exactly one `\n` at end after `AppendSection`).

## Assumptions

- `gopkg.in/yaml.v3` `yaml.Node` is sufficient for order-preserving frontmatter mutation. No new yaml dep needed.
- Code-fence detection can be limited to triple-backtick fences (``` ``` ```). Tilde fences (`~~~`) and indented code blocks are out of scope — no agent task file currently uses them.
- Callers are single-goroutine per `Markdown` instance. No concurrent-access safety required.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Malformed frontmatter YAML | `Parse` returns wrapped error | Caller inspects, typically escalates to `human_review` |
| Duplicate `## Result` sections | `ReplaceOrAppendSection` removes all matching, appends once (current behavior) | None |
| `## heading` inside fenced block | Not detected as section — ignored | None |
| Round-trip changes byte layout on unmutated input | Test failure | Fix serializer — do not ship |

## Acceptance Criteria

- [ ] `Markdown` type with the 8 behaviors above
- [ ] All existing free functions become one-line wrappers over `Markdown` methods
- [ ] Table test: round-trip fidelity on a 20-row fixture covering real task files from `OpenClaw/tasks/`
- [ ] Test: code-fenced `## heading` is NOT detected as a section boundary
- [ ] Test: setting a frontmatter key preserves order and comments of adjacent keys
- [ ] Test: `Parse` on malformed frontmatter returns wrapped error with source-position context
- [ ] All existing tests in `lib/delivery/` still pass unchanged
- [ ] `cd lib && make precommit` passes

## Verification

```
cd lib && make precommit
```

## Do-Nothing Option

Keep the free functions. Risks accumulate as agents publish more structured output: (a) multi-op writes scan the file N times, (b) code-fenced examples in task bodies can be misparsed, (c) frontmatter refactors churn commit history. Tolerable today; worse every quarter.

## Alternatives Considered

- **Full markdown parser (goldmark)** — rejected: heavy dep, real AST is overkill for `##`-sectioned files with YAML frontmatter; most node types (blockquote, emphasis, links) are noise for our use case.
- **Keep free functions, add a `multiOp` helper** — rejected: doesn't address section/fence correctness or frontmatter order loss; just hides the re-scan cost.
- **yaml.Node frontmatter, string body** (chosen slice) — partial: preserves frontmatter fidelity but leaves section handling as-is. Fine half-step; full struct is only incrementally more work and gives code-fence awareness.
