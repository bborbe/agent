---
status: approved
created: "2026-04-26T18:00:47Z"
queued: "2026-04-26T18:00:47Z"
---

<summary>
- Adds `NewSectionContentGenerator(heading string) ContentGenerator` to `lib/delivery/content-generator.go`. Mirrors the existing `FallbackContentGenerator` exactly, but writes its output section under a parameterized markdown heading instead of the hardcoded `## Result`.
- Use case: phase-aware agents need to write `## Plan` (planning phase), `## Review` (review phase), or other agent-specific section headings — currently each agent has hand-rolled section assembly to work around `FallbackContentGenerator`'s hardcoded `## Result`.
- Failure path unchanged: on `AgentStatusFailed` the generator still writes `## Failure` (failure-section convention is repo-wide, not section-specific).
- `AgentStatusInProgress` (added in sibling prompt) is honored: phase frontmatter is preserved, the parameterized section is replaced/appended.
- Tests cover: every status × heading-parameterization, in-place save preservation, replace-vs-append section logic.
- Fully additive — `FallbackContentGenerator` and existing call sites are not modified.
</summary>

<objective>
Generalize the existing `FallbackContentGenerator` so phase-aware agents can write to arbitrary section headings (`## Plan`, `## Review`, etc.) without inventing their own content-generator implementations. After this change, an agent's planning phase wires up `delivery.NewSectionContentGenerator("## Plan")` to produce updated task content with the plan in the right section, with the same status-frontmatter and failure-section semantics as the existing fallback.

Concrete motivation: `agent/backtest` PlanningRunner needs to write `## Plan`, ExecutionRunner writes `## Result`, ReviewRunner writes `## Review`. Today the `FallbackContentGenerator` only writes `## Result`, forcing each agent to either pre-format its `Output` field with the section heading + fences (fragile, easy to drift) or implement its own ContentGenerator from scratch (duplicates `applyStatusFrontmatter` + `buildFailureSection` logic).
</objective>

<context>
Read `CLAUDE.md` at repo root for project conventions.

Read these guides before writing code (in coding plugin docs):
- `go-error-wrapping-guide.md` — `github.com/bborbe/errors`, never `fmt.Errorf`
- `go-testing-guide.md` — Ginkgo/Gomega, external `_test` package
- `go-factory-pattern.md` — `Create*` prefix, public interface + private struct + `New*` constructor

**Cross-prompt dependency**: this prompt depends on the sibling prompt `add-agent-status-in-progress.md` having landed first. The sibling adds `AgentStatusInProgress` enum value AND extends `applyStatusFrontmatter` to handle it (preserving phase). Without the sibling, this prompt's `AgentStatusInProgress` test will fail because `applyStatusFrontmatter` falls to the default branch and writes `phase: human_review`. **dark-factory must execute these prompts in order: sibling first, then this one.**

**Files to read in full before editing:**

- `lib/delivery/content-generator.go` — defines `ContentGenerator` interface, `FallbackContentGenerator` impl, `applyStatusFrontmatter`, `buildFailureSection`, `buildMinimalResultSection` helpers. The new `SectionContentGenerator` mirrors this file's structure — re-use the helpers, do not duplicate them.
- `lib/delivery/markdown.go` — `SetFrontmatterField`, `ReplaceOrAppendSection` helpers (already exported). The new generator uses both.
- `lib/delivery/content-generator_test.go` — existing tests for `FallbackContentGenerator`. Extend, don't create parallel files.
- `lib/delivery/status.go` — `AgentResultInfo` struct + `AgentStatus` enum. **`AgentStatusInProgress` MUST exist** (from sibling prompt `add-agent-status-in-progress.md` which lands first). Verify with `grep -n "AgentStatusInProgress" lib/delivery/status.go` before proceeding — must show one definition. If missing, halt with status:failed and message "sibling prompt add-agent-status-in-progress has not landed".

**Design contract:**

`NewSectionContentGenerator(heading string)` returns a `ContentGenerator` that:

| `result.Status` | Behavior |
|---|---|
| `AgentStatusDone` | Apply status frontmatter (`status: completed, phase: done`); replace/append `<heading>` with `result.Output` (or `buildMinimalResultSection` if Output empty) |
| `AgentStatusInProgress` | Apply status frontmatter (`status: in_progress`, phase preserved); replace/append `<heading>` with `result.Output` |
| `AgentStatusNeedsInput` | Apply status frontmatter (`status: in_progress, phase: human_review`); replace/append `<heading>` with `result.Output` (or minimal) |
| `AgentStatusFailed` | Apply status frontmatter (`status: in_progress, phase: human_review`); replace/append `## Failure` (NOT `<heading>`) with `buildFailureSection` |

The failure path always writes `## Failure` regardless of the configured `heading` — failure is a repo-wide convention, not phase-specific.

**Why `heading` is a parameter and not a per-call argument:**
- The generator is wired up once in factory code and used many times. The heading reflects the phase semantic (planning writes ## Plan, review writes ## Review). Per-call would invert the dependency direction.
- Matches the existing `FallbackContentGenerator` pattern (no args; behavior is fixed at construction).

Grep before editing:
```bash
grep -n "FallbackContentGenerator\|NewSectionContentGenerator\|ReplaceOrAppendSection" lib/delivery/*.go | grep -v _test.go
```
</context>

<requirements>

## 1. Add `NewSectionContentGenerator` to `lib/delivery/content-generator.go`

Append AFTER the existing `FallbackContentGenerator` and helpers (do not interleave). Mirror the structure exactly:

```go
// NewSectionContentGenerator creates a ContentGenerator that writes its output under a
// parameterized markdown heading (e.g. "## Plan", "## Review"). On AgentStatusFailed
// it writes a "## Failure" section instead, regardless of the configured heading —
// the failure-section convention is repo-wide, not phase-specific.
//
// Use this for phase-aware agents whose phases write distinct sections (planning → ## Plan,
// execution → ## Result, review → ## Review).
func NewSectionContentGenerator(heading string) ContentGenerator {
    return &sectionContentGenerator{heading: heading}
}

type sectionContentGenerator struct {
    heading string
}

func (g *sectionContentGenerator) Generate(
    _ context.Context,
    originalContent string,
    result AgentResultInfo,
) (string, error) {
    updated := applyStatusFrontmatter(originalContent, result.Status)
    if result.Status == AgentStatusFailed {
        section := buildFailureSection(result)
        return ReplaceOrAppendSection(updated, "## Failure", section), nil
    }
    section := result.Output
    if section == "" {
        section = buildMinimalResultSection(result)
    }
    return ReplaceOrAppendSection(updated, g.heading, section), nil
}
```

Note the `heading` parameter is the FULL section heading including the leading `## ` (e.g. `"## Plan"`, not `"Plan"`). This matches `ReplaceOrAppendSection`'s API.

## 2. Tests in `lib/delivery/content-generator_test.go`

Add a `Describe("NewSectionContentGenerator", ...)` block with cases:

- **Heading parameterization**: with `heading = "## Plan"` and `Status: AgentStatusDone, Output: "<plan content>"`, the generated content has a `## Plan` section containing `<plan content>`.
- **Failure ignores heading**: with `heading = "## Plan"` and `Status: AgentStatusFailed, Message: "boom"`, the generated content has a `## Failure` section (NOT `## Plan`).
- **Empty Output → minimal section**: with `Status: AgentStatusDone` and empty `Output`, `buildMinimalResultSection` is used as the section body.
- **In-place save preservation**: with `Status: AgentStatusInProgress` and input containing `phase: planning`, the generated content has `status: in_progress` and `phase: planning` preserved (NOT overwritten to `human_review` or `done`).
- **Section replacement**: input content already has a `## Plan` section with old content; generator replaces it (does not duplicate).
- **Section append**: input content has no `## Plan` section; generator appends one.
- **Custom heading**: with `heading = "## Review"`, output goes under `## Review` (sanity check that the heading is wired through).

Re-use existing test fixtures and helpers from the `FallbackContentGenerator` block. Do not duplicate.

## 3. Counterfeiter mock regen (if interface changes)

The `ContentGenerator` interface itself is unchanged — we're adding a new IMPLEMENTATION. No counterfeiter regen needed. Verify:

```bash
grep -n "counterfeiter:generate" lib/delivery/content-generator.go
```

The existing `//counterfeiter:generate` annotation on `ContentGenerator` covers all implementations. No new annotations needed.

## 4. CHANGELOG

Add an entry under `## Unreleased`:

```
- feat(lib): add NewSectionContentGenerator(heading) to lib/delivery for phase-aware agents writing custom section headings (## Plan, ## Review, etc.) — same status-frontmatter + failure-section semantics as FallbackContentGenerator
```

## 5. Verify

```bash
make test
```

Then specifically:

```bash
go test -run "SectionContentGenerator\|FallbackContentGenerator" -v ./lib/delivery/...
```

All cases pass; existing FallbackContentGenerator tests unaffected.

```bash
make precommit
```

Exit code 0.

</requirements>

<constraints>
- `NewSectionContentGenerator(heading string)` accepts the FULL heading including `## ` prefix (matches `ReplaceOrAppendSection` API)
- Failure path ALWAYS writes `## Failure` (not the configured heading) — failure-section convention is global
- Re-use `applyStatusFrontmatter`, `buildFailureSection`, `buildMinimalResultSection` from the existing file — do NOT duplicate
- Re-use `ReplaceOrAppendSection` from `lib/delivery/markdown.go` — do NOT reimplement
- `FallbackContentGenerator` and its `Generate` method are NOT modified — fully additive change
- `ContentGenerator` interface signature is NOT modified — adding new impl, not changing contract
- Errors via `github.com/bborbe/errors` — never `fmt.Errorf`
- No `time.Now()` or `context.Background()` in business logic
- Do NOT commit — dark-factory handles git
- All existing tests must still pass
</constraints>

<verification>
```bash
make test
```
Must exit 0.

Check the constructor exists:
```bash
grep -n "NewSectionContentGenerator\|sectionContentGenerator" lib/delivery/content-generator.go
```
Must show: exported constructor, private struct, Generate method.

Check the implementation reuses helpers (no duplication):
```bash
grep -c "applyStatusFrontmatter\|buildFailureSection\|buildMinimalResultSection" lib/delivery/content-generator.go
```
Each helper is referenced from BOTH `FallbackContentGenerator` and `sectionContentGenerator` — count should be > number of definitions.

Check ContentGenerator interface unchanged:
```bash
grep -A 3 "type ContentGenerator interface" lib/delivery/content-generator.go
```
Must show only `Generate(ctx, originalContent, result) (string, error)` — no new methods.

Check tests:
```bash
go test -v ./lib/delivery/... 2>&1 | tail -30
```
All cases pass; new SectionContentGenerator describe block visible.

```bash
make precommit
```
Must exit 0.
</verification>
