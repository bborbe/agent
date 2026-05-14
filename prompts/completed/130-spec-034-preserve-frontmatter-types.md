---
status: completed
spec: [034-preserve-frontmatter-types-through-delivery]
summary: Changed ParseMarkdownFrontmatter return type from map[string]string to map[string]any, removing fmt.Sprintf conversion to preserve native YAML types (int, float64, bool, slice, nested map), updated tests with type-native assertions and added round-trip tests for trigger_count and spawn_notification, and added CHANGELOG entry.
container: agent-130-spec-034-preserve-frontmatter-types
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T15:00:00Z"
queued: "2026-05-14T15:06:03Z"
started: "2026-05-14T15:06:05Z"
completed: "2026-05-14T15:12:21Z"
branch: dark-factory/preserve-frontmatter-types-through-delivery
---

<summary>
- `ParseMarkdownFrontmatter` now returns `map[string]any` instead of `map[string]string` — integers, floats, booleans, lists, and nested maps are preserved in their native Go types
- A `trigger_count: 0` line parsed and re-marshaled through `yaml.Marshal` produces the same unquoted-integer form, eliminating the int-vs-string conflict that caused git merge conflicts on probe files
- The delivery path in `result-deliverer.go` receives native-typed values from the parser and puts them directly into `TaskFrontmatter` — no more silent stringification of numeric/boolean frontmatter
- Nil values continue to be omitted from the parsed map (unchanged behavior)
- Invalid YAML continues to return an empty map and the original content (unchanged failure mode)
- Existing string fields, status, and phase assignments are unchanged
- Tests in `markdown_test.go` are updated to assert native types and new cases (bool, list, nested map) are added
- All consumer modules (`task/controller`, `task/executor`, `agent/claude`, `agent/code`, `agent/gemini`) continue to compile and pass `make precommit` without code changes — their typed accessors already handle both `int` and `float64`
</summary>

<objective>
Fix `ParseMarkdownFrontmatter` in `lib/delivery/markdown.go` to preserve native YAML scalar types instead of converting everything to strings. This eliminates the root cause of git merge conflicts on numeric frontmatter fields (`trigger_count`, `retry_count`, etc.): one writer (the controller's increment executor) writes `trigger_count: 0` (int); the other (the result deliverer) currently writes `trigger_count: "0"` (quoted string). After this fix both writers produce the same unquoted-integer form.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`, always `errors.Wrapf`
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface/struct patterns, nil handling
- `changelog-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — entry format and `## Unreleased` rules
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Files to read in full before editing:**
- `lib/delivery/markdown.go` — contains `ParseMarkdownFrontmatter` (the function being changed) and `SetFrontmatterField`
- `lib/delivery/markdown_test.go` — test file; the `ParseMarkdownFrontmatter` block needs updating
- `lib/delivery/result-deliverer.go` — the single production caller (lines 124–129 copy `fmMap` into `TaskFrontmatter`)
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` type (`map[string]interface{}`) and its typed accessors (`TriggerCount`, `MaxTriggers`, `SpawnNotification`, `RetryCount`, `MaxRetries`); confirm they already handle both `int` and `float64`

**Current `ParseMarkdownFrontmatter` implementation (inline reference):**
```go
func ParseMarkdownFrontmatter(content string) (map[string]string, string) {
    // ... delimiter parsing ...
    var parsed map[string]interface{}
    if err := yaml.Unmarshal([]byte(fmRaw), &parsed); err != nil {
        return map[string]string{}, content
    }

    fm := make(map[string]string, len(parsed))
    for k, v := range parsed {
        switch val := v.(type) {
        case string:
            fm[k] = val
        case nil:
            // skip nil values
        default:
            fm[k] = fmt.Sprintf("%v", val)  // <-- this is the bug: 42 → "42"
        }
    }
    return fm, body
}
```

**New `ParseMarkdownFrontmatter` implementation (inline reference):**
```go
// ParseMarkdownFrontmatter splits a markdown document with YAML frontmatter into
// a typed map and the body. Returns empty map and full content if no frontmatter.
// Nil values are omitted. All other YAML scalar types (int, float64, bool, string),
// lists, and nested maps are preserved as their native Go types.
func ParseMarkdownFrontmatter(content string) (map[string]any, string) {
    if !strings.HasPrefix(content, "---") {
        return map[string]any{}, content
    }
    rest := content[3:]
    end := strings.Index(rest, "\n---")
    if end == -1 {
        return map[string]any{}, content
    }
    fmRaw := rest[:end]
    body := strings.TrimLeft(rest[end+4:], "\n")

    var parsed map[string]any
    if err := yaml.Unmarshal([]byte(fmRaw), &parsed); err != nil {
        return map[string]any{}, content
    }

    fm := make(map[string]any, len(parsed))
    for k, v := range parsed {
        if v != nil {
            fm[k] = v
        }
    }
    return fm, body
}
```

Key differences from the current implementation:
- Return type: `map[string]any` (was `map[string]string`)
- `fmt.Sprintf("%v", val)` conversion is GONE — native types preserved
- `fmt` import becomes unused and must be removed
- Nil check replaces the type switch

**Call site in `result-deliverer.go` (read; no code change needed):**
```go
fmMap, body := ParseMarkdownFrontmatter(generated)

frontmatter := agentlib.TaskFrontmatter{}
for k, v := range fmMap {
    frontmatter[k] = v   // v changes from string to any — still compiles
}
```
The copy loop already works with `map[string]any` since `TaskFrontmatter` is `map[string]interface{}`. Verify it compiles with `go build ./...` after the signature change — do NOT modify the loop.

**Consumer modules do NOT call `ParseMarkdownFrontmatter`:**
```bash
grep -rn "ParseMarkdownFrontmatter" /workspace/ --include="*.go"
```
Expected: matches only in `lib/delivery/markdown.go`, `lib/delivery/markdown_test.go`, `lib/delivery/result-deliverer.go`.

Consumer modules (`task/controller`, `task/executor`, `agent/claude`, `agent/code`, `agent/gemini`) import `lib/delivery` for other functions (`ReplaceOrAppendSection`, `NewNoopResultDeliverer`, `StripMarkdownCodeFences`, etc.) but do NOT call `ParseMarkdownFrontmatter`. They need no code changes — only verification that `make precommit` still passes.

**How typed accessors tolerate both int and float64 (inline reference from `lib/agent_task-frontmatter.go`):**
```go
func (f TaskFrontmatter) TriggerCount() int {
    switch v := f["trigger_count"].(type) {
    case int:
        return v
    case float64:
        return int(v)
    default:
        return 0
    }
}
```
All numeric accessors already handle both YAML-unmarshaled `int` and JSON-unmarshaled `float64`. The fix is additive: after the change, YAML-sourced values arrive as `int` (matching the first case), JSON-sourced values continue to arrive as `float64` (matching the second case).

**Wire format is already correct — no change needed.** The Kafka result payload is `agentlib.AgentResultInfo` at `lib/agent_task.go:22`, which declares `Frontmatter TaskFrontmatter` where `TaskFrontmatter = map[string]interface{}`. So the wire type already preserves YAML-native types. This prompt does NOT change `agent_task.go` or any other wire-format definition. Verify with:
```bash
grep -n "Frontmatter\s*TaskFrontmatter\|Frontmatter\s*map\[" /workspace/lib/agent_task.go
```
Expected: one match showing `Frontmatter TaskFrontmatter` (or equivalent). If this grep returns a different type, STOP and report — spec assumption broken.

**Note: `task/controller/pkg/scanner/vault_scanner.go` parses frontmatter independently** via direct `yaml.Unmarshal` into a `map[string]interface{}` then casts to `lib.TaskFrontmatter` (verified at lines 229 + 233). It already produces native types. **Do NOT refactor it** to call `ParseMarkdownFrontmatter` — that's a separate concern outside this spec's blast radius.
</context>

<requirements>

## 1. Verify callers before editing

```bash
grep -rn "ParseMarkdownFrontmatter" /workspace/ --include="*.go"
```
Expected: exactly 3 files — `markdown.go` (definition), `markdown_test.go` (tests), `result-deliverer.go` (single caller). STOP if any other file matches.

```bash
grep -n "^import\|\"fmt\"" lib/delivery/markdown.go
```
Confirm `fmt` is imported; it will be removed.

## 2. Change `ParseMarkdownFrontmatter` in `lib/delivery/markdown.go`

Read the full file before editing.

Replace the current function signature and implementation with the new version from `<context>`. Specifically:
- Change the return type from `map[string]string` to `map[string]any`
- Remove the `fmt.Sprintf("%v", val)` string conversion
- Replace the type switch with a nil guard: `if v != nil { fm[k] = v }`
- Remove the `"fmt"` import (it is now unused)
- Update the function comment to describe type preservation (use the new comment from `<context>`)

Verify the `fmt` import is gone:
```bash
grep -n '"fmt"' lib/delivery/markdown.go
```
Expected: zero matches.

Verify the new signature:
```bash
grep -n "func ParseMarkdownFrontmatter" lib/delivery/markdown.go
```
Expected: `map[string]any` in the signature.

Build check:
```bash
cd lib && go build ./delivery/...
```
Expected: exit 0.

## 3. Verify `result-deliverer.go` requires no changes

Read `lib/delivery/result-deliverer.go`. Locate the copy loop (lines ~124–129):
```go
fmMap, body := ParseMarkdownFrontmatter(generated)

frontmatter := agentlib.TaskFrontmatter{}
for k, v := range fmMap {
    frontmatter[k] = v
}
```
Confirm this loop compiles without modification. The variable `v` is now inferred as `any`; assigning it to `frontmatter[k]` (which is `map[string]interface{}`) is valid in Go since `any` is an alias for `interface{}`.

Do NOT modify `result-deliverer.go`.

Build to confirm:
```bash
cd lib && go build ./...
```
Expected: exit 0.

## 4. Update `lib/delivery/markdown_test.go`

Read the full file before editing. Tests are in external package `delivery_test`.

### 4a. Update the "handles arrays" test

The test currently asserts string representation. Change it to assert native slice:

Replace:
```go
It("handles arrays by converting to string representation", func() {
    content := "---\ntags:\n  - tag1\n  - tag2\n---\n\nBody.\n"
    fm, body := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKey("tags"))
    Expect(fm["tags"]).To(ContainSubstring("tag1"))
    Expect(fm["tags"]).To(ContainSubstring("tag2"))
    Expect(body).To(Equal("Body.\n"))
})
```

With:
```go
It("preserves array values as a native slice", func() {
    content := "---\ntags:\n  - tag1\n  - tag2\n---\n\nBody.\n"
    fm, body := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKey("tags"))
    Expect(fm["tags"]).To(ConsistOf("tag1", "tag2"))
    Expect(body).To(Equal("Body.\n"))
})
```

### 4b. Update the "handles numeric values" test

The test currently asserts string-form values. Change it to assert native types:

Replace:
```go
It("handles numeric values", func() {
    content := "---\ncount: 42\nprice: 3.14\n---\n\nBody.\n"
    fm, body := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKeyWithValue("count", "42"))
    Expect(fm).To(HaveKeyWithValue("price", "3.14"))
    Expect(body).To(Equal("Body.\n"))
})
```

With:
```go
It("preserves integer and float values as native numeric types", func() {
    content := "---\ncount: 42\nprice: 3.14\n---\n\nBody.\n"
    fm, body := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKeyWithValue("count", 42))
    Expect(fm).To(HaveKeyWithValue("price", 3.14))
    Expect(body).To(Equal("Body.\n"))
})
```

### 4c. Add new test cases inside the existing `Describe("ParseMarkdownFrontmatter", ...)` block

Add the following `It` blocks after the updated tests above:

```go
It("preserves boolean value as native bool type", func() {
    content := "---\nspawn_notification: true\nenabled: false\n---\n\nBody.\n"
    fm, body := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKeyWithValue("spawn_notification", true))
    Expect(fm).To(HaveKeyWithValue("enabled", false))
    Expect(body).To(Equal("Body.\n"))
})

It("preserves nested map as map[string]interface{}", func() {
    content := "---\nmeta:\n  key: val\n  num: 7\n---\n\nBody.\n"
    fm, body := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKey("meta"))
    nested, ok := fm["meta"].(map[string]interface{})
    Expect(ok).To(BeTrue(), "expected nested map to be map[string]interface{}")
    Expect(nested).To(HaveKeyWithValue("key", "val"))
    Expect(nested).To(HaveKeyWithValue("num", 7))
    Expect(body).To(Equal("Body.\n"))
})

It("round-trips trigger_count integer as unquoted int (spec 034 AC)", func() {
    // Verifies the fix for the git-conflict root cause:
    // trigger_count: 0 must remain an integer after parse, not become "0".
    content := "---\ntrigger_count: 0\n---\n\nBody.\n"
    fm, _ := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKeyWithValue("trigger_count", 0))

    // Confirm yaml.Marshal serializes the int without quotes.
    out, err := yaml.Marshal(fm)
    Expect(err).NotTo(HaveOccurred())
    Expect(string(out)).To(ContainSubstring("trigger_count: 0"))
    Expect(string(out)).NotTo(ContainSubstring(`trigger_count: "0"`))
})

It("round-trips spawn_notification bool as unquoted bool (spec 034 AC)", func() {
    content := "---\nspawn_notification: true\n---\n\nBody.\n"
    fm, _ := delivery.ParseMarkdownFrontmatter(content)
    Expect(fm).To(HaveKeyWithValue("spawn_notification", true))

    out, err := yaml.Marshal(fm)
    Expect(err).NotTo(HaveOccurred())
    Expect(string(out)).To(ContainSubstring("spawn_notification: true"))
    Expect(string(out)).NotTo(ContainSubstring(`spawn_notification: "true"`))
})
```

The round-trip tests require the `gopkg.in/yaml.v3` import in the test file. Add it to the import block:
```go
import (
    "strings"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "gopkg.in/yaml.v3"

    "github.com/bborbe/agent/lib/delivery"
)
```

Run iterative tests:
```bash
cd lib && go test ./delivery/... -v 2>&1 | tail -30
```
Fix any compile errors before continuing. Expected: all existing tests pass plus the new cases.

Coverage check:
```bash
cd lib && go test -coverprofile=/tmp/delivery-cover.out ./delivery/... && \
  go tool cover -func=/tmp/delivery-cover.out | grep -E "markdown\.go|total"
```
Expected: `markdown.go` function coverage ≥80%.

## 5. Add CHANGELOG entry

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add the following bullet:
```markdown
- fix(lib/delivery): `ParseMarkdownFrontmatter` now returns `map[string]any` preserving native YAML types (int, float64, bool, list, map) — eliminates git merge conflicts caused by one writer serializing `trigger_count: 0` (int) while another serialized `trigger_count: "0"` (quoted string)
```

Verify:
```bash
grep -n "ParseMarkdownFrontmatter" CHANGELOG.md
```
Expected: at least 1 match.

## 6. Run `make precommit` in `lib/`

```bash
cd lib && make test
```
Expected: exit 0. Fix failures before proceeding.

```bash
cd lib && make precommit
```
Expected: exit 0. If any target fails, run only the failing target (`make lint`, `make gosec`, etc.) and fix before retrying.

## 7. Verify consumer modules compile and pass precommit

Consumer modules use `replace github.com/bborbe/agent/lib => ../../lib` so they pick up the modified `lib/` automatically. None call `ParseMarkdownFrontmatter`, so no code changes are needed.

Run in sequence:
```bash
cd task/controller && make precommit
```
Expected: exit 0.

```bash
cd task/executor && make precommit
```
Expected: exit 0.

```bash
cd agent/claude && make precommit
```
Expected: exit 0.

```bash
cd agent/code && make precommit
```
Expected: exit 0.

```bash
cd agent/gemini && make precommit
```
Expected: exit 0.

If any consumer precommit fails for a reason unrelated to the signature change (e.g. a pre-existing linter issue), document it in `## Improvements` and report `status: partial` in the completion report.

</requirements>

<constraints>
- **`ParseMarkdownFrontmatter` signature change is the only public API change in this prompt.** All other functions in `lib/delivery/` are unchanged.
- **Do NOT modify `result-deliverer.go`.** The copy loop already works with `map[string]any` — changing it would introduce unnecessary diff.
- **Do NOT modify any consumer module Go code** (`task/controller/`, `task/executor/`, `agent/claude/`, `agent/code/`, `agent/gemini/`). They need no changes.
- **Nil values remain excluded** from the parsed map (unchanged behavior).
- **Invalid YAML returns empty map and original content** (unchanged failure mode).
- **No `fmt.Sprintf` for type conversion.** The entire conversion is removed — native types are passed through.
- **`fmt` import must be removed** from `markdown.go` once it is unused.
- **Tests must be in external package `delivery_test`** (already the case; do not change the package line).
- **Error wrapping uses `github.com/bborbe/errors`** — `ParseMarkdownFrontmatter` returns no errors so this is N/A; do not add `fmt.Errorf` anywhere.
- **Do NOT commit.** Dark-factory handles git. The paired `vX.Y.Z` + `lib/vX.Y.Z` tag release is handled by the management session after the YOLO commit lands.
- `cd lib && make precommit` must exit 0.
- All consumer module `make precommit` runs must exit 0 (or be documented as pre-existing failures in `## Improvements`).
</constraints>

<verification>

Signature changed:
```bash
grep -n "func ParseMarkdownFrontmatter" lib/delivery/markdown.go
```
Expected: `map[string]any` in return type.

`fmt` import removed:
```bash
grep -n '"fmt"' lib/delivery/markdown.go
```
Expected: zero matches.

No string conversion remaining:
```bash
grep -n "Sprintf\|fmt\." lib/delivery/markdown.go
```
Expected: zero matches.

Call site unchanged:
```bash
grep -n "ParseMarkdownFrontmatter\|for k, v := range fmMap" lib/delivery/result-deliverer.go
```
Expected: both lines present, no modification.

Callers outside lib/delivery:
```bash
grep -rn "ParseMarkdownFrontmatter" /workspace/ --include="*.go"
```
Expected: exactly `markdown.go`, `markdown_test.go`, `result-deliverer.go`.

Integer type preserved:
```bash
cd lib && go test ./delivery/... -run "TestDelivery/ParseMarkdownFrontmatter" -v 2>&1 | grep -E "PASS|FAIL|preserves integer"
```
Expected: `preserves integer and float values as native numeric types` PASS.

Round-trip test:
```bash
cd lib && go test ./delivery/... -run "TestDelivery/ParseMarkdownFrontmatter/round-trips_trigger_count" -v 2>&1
```
Expected: PASS.

CHANGELOG:
```bash
grep -n "ParseMarkdownFrontmatter" CHANGELOG.md
```
Expected: at least 1 match.

lib precommit:
```bash
cd lib && make precommit
```
Expected: exit 0.

Consumer builds (quick check before full precommit):
```bash
cd task/controller && go build ./... && \
cd ../../task/executor && go build ./... && \
cd ../../agent/claude && go build ./... && \
cd ../../agent/code && go build ./... && \
cd ../../agent/gemini && go build ./...
```
Expected: all exit 0.

Consumer precommit runs (full):
```bash
cd task/controller && make precommit
cd task/executor && make precommit
cd agent/claude && make precommit
cd agent/code && make precommit
cd agent/gemini && make precommit
```
Each must exit 0.

</verification>
