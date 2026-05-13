---
status: completed
spec: [027-agent-task-previous-assignee-frontmatter]
summary: Added clearAssignee helper as single chokepoint for all assignee-clear paths; all three escalation paths (trigger cap, retry cap, needs_input) now write previous_assignee frontmatter field; 17 existing tests extended, 3 new tests added, docs and CHANGELOG updated
container: agent-113-spec-027-previous-assignee-frontmatter
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T20:10:00Z"
queued: "2026-05-13T20:11:13Z"
started: "2026-05-13T20:16:57Z"
completed: "2026-05-13T20:24:33Z"
branch: dark-factory/agent-task-previous-assignee-frontmatter
---

<summary>
- The controller's result writer now sets `previous_assignee: <name>` in task frontmatter on every assignee-clear operation (trigger cap, retry cap, needs_input)
- The value is the agent name that held the task immediately before the clear — identical to what the existing body-side `**Assignee:**` bullet already records
- A new package-level `clearAssignee` helper is the single chokepoint that captures the pre-clear agent name, writes `previous_assignee`, and clears `assignee` — all three escalation paths are updated to use it
- When the pre-clear assignee is already empty (defensive case), `previous_assignee` is not written
- Operator re-delegation (setting assignee to a non-empty value) does NOT touch `previous_assignee` — the field persists across re-delegation
- Idempotent re-delivery: the value is recomputed from the stale agent payload's assignee on each delivery, so repeated writes are byte-identical
- Existing tests for all three escalation paths are extended in-place to assert `previous_assignee: <name>`
- Two new tests cover operator re-delegation persistence and the empty pre-clear assignee edge case
- `docs/task-flow-and-failure-semantics.md` Escalation terminology entry is extended with the new field contract
- CHANGELOG updated under `## Unreleased`
</summary>

<objective>
Add `previous_assignee` frontmatter field to task files. On every path in `result_writer.go` where `assignee` is cleared to `""`, also write `previous_assignee: <pre-clear-name>`. This lets operators query parked tasks by which agent parked them without parsing body content. The change is a 5-line addition to one file plus test extensions.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, ≥80% coverage
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`, never `fmt.Errorf`
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**

- `task/controller/pkg/result/result_writer.go` — the sole implementation file changing; focus on `applyRetryCounter`, `applyTriggerCap`, `applyRetryCap`, and the `needs_input` block
- `task/controller/pkg/result/result_writer_test.go` — full read; all existing Context blocks must be understood before adding assertions

**Inline reference — current `applyTriggerCap` (verify against actual file before editing):**

```go
func (r *resultWriter) applyTriggerCap(
    merged, existing lib.TaskFrontmatter,
    triggerCount int,
    body string,
) string {
    if triggerCount == 0 || triggerCount < merged.MaxTriggers() {
        return body
    }
    agentName := string(merged.Assignee()) // capture before clear
    merged["assignee"] = ""
    if containsEscalationSection(body, "## Trigger Cap Escalation") {
        restoreExistingPhase(existing, merged)
        return body
    }
    return body + r.triggerEscalationSection(triggerCount, agentName, merged)
}
```

**Inline reference — current `applyRetryCap` (verify before editing):**

```go
func (r *resultWriter) applyRetryCap(
    merged, existing lib.TaskFrontmatter,
    retryCount int,
    body string,
) string {
    if retryCount < merged.MaxRetries() {
        return body
    }
    agentName := string(merged.Assignee()) // capture before clear
    merged["assignee"] = ""
    if containsEscalationSection(body, "## Retry Escalation") {
        restoreExistingPhase(existing, merged)
        return body
    }
    return body + r.escalationSection(retryCount, agentName)
}
```

**Inline reference — current `needs_input` block in `applyRetryCounter` (verify before editing):**

```go
// needs_input: agent explicitly requested human review — clear assignee so task surfaces in operator inbox
if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
    merged["assignee"] = ""
}
```

**Inline reference — `lib.TaskFrontmatter.Assignee()` method (from `lib/agent_task-frontmatter.go`):**

```go
func (f TaskFrontmatter) Assignee() TaskAssignee {
    v, _ := f["assignee"].(string)
    return TaskAssignee(v)
}
```

`TaskAssignee` is a string-based type; `string(merged.Assignee())` converts to a plain string.
</context>

<requirements>

## 1. Add `clearAssignee` helper to `task/controller/pkg/result/result_writer.go`

Place this function immediately above `restoreExistingPhase` (around line 229 today):

```go
// clearAssignee sets previous_assignee to the current assignee value (if non-empty),
// then clears assignee to "". Returns the captured name for use in escalation body text.
// This is the single chokepoint for all assignee-clear operations in the result writer.
func clearAssignee(merged lib.TaskFrontmatter) string {
    agentName := string(merged.Assignee())
    if agentName != "" {
        merged["previous_assignee"] = agentName
    }
    merged["assignee"] = ""
    return agentName
}
```

No new imports are needed — `lib.TaskFrontmatter` is already imported.

## 2. Update `applyTriggerCap` to use `clearAssignee`

Replace the two lines:
```go
agentName := string(merged.Assignee()) // capture before clear
merged["assignee"] = ""
```
with:
```go
agentName := clearAssignee(merged)
```

The rest of `applyTriggerCap` is unchanged. `agentName` is still available for the call to `r.triggerEscalationSection`.

## 3. Update `applyRetryCap` to use `clearAssignee`

Same replacement as step 2:
```go
agentName := clearAssignee(merged)
```

The rest of `applyRetryCap` is unchanged. `agentName` is still available for `r.escalationSection`.

## 4. Update the `needs_input` block in `applyRetryCounter` to use `clearAssignee`

Replace:
```go
merged["assignee"] = ""
```
with:
```go
clearAssignee(merged) // sets previous_assignee and clears assignee
```

The full block becomes:
```go
if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
    clearAssignee(merged) // sets previous_assignee and clears assignee
}
```

## 5. Compile check (do NOT run tests yet — they will fail in step 6 due to substring collision)

```bash
cd task/controller && go build ./...
```

Expected: exit 0. Do NOT run `make test` here — many existing tests in `result_writer_test.go` assert `NotTo(ContainSubstring("assignee: claude"))` after a clear, and after this change the YAML will contain `previous_assignee: claude` which **contains** that substring. Step 6 fixes those assertions BEFORE running tests.

## 6. Fix substring-collision in existing assertions (REQUIRED — do before adding new assertions)

The new `previous_assignee: <name>` line will contain the substring `assignee: <name>`. Existing tests that assert the cleared assignee with `NotTo(ContainSubstring("assignee: claude"))` (or other agent names) will flip to failing. Fix every such assertion in `task/controller/pkg/result/result_writer_test.go` BEFORE adding the new positive assertions.

**Search for affected assertions:**
```bash
grep -n 'NotTo(ContainSubstring("assignee: ' task/controller/pkg/result/result_writer_test.go
```

**Substitution (line-anchored matcher avoids the `previous_assignee:` prefix):**

Replace every occurrence of the form:
```go
Expect(s).NotTo(ContainSubstring("assignee: <name>"))
```
with:
```go
Expect(s).NotTo(ContainSubstring("\nassignee: <name>"))
```

The leading `\n` anchors the match to the start of a line, so `previous_assignee:` (which is preceded by `\n` followed by `previous_`) cannot match. Apply this substitution to **every match** the grep returns — the auditor identified ~17 sites; fix them all consistently regardless of the exact count.

Run compile check after the substitution:
```bash
cd task/controller && go build ./...
```
Expected: exit 0.

## 7. Extend existing tests in `task/controller/pkg/result/result_writer_test.go`

Read the full test file before editing. For each of the following `It` blocks, add one assertion immediately after the last existing assertion in that block:

```go
Expect(s).To(ContainSubstring("previous_assignee: claude"))
```

**Tests to extend — Context "retry counter":**
- `"escalates when retry_count (set by executor) meets default max_retries, preserving lifecycle phase"`
- `"escalates immediately when retry_count (set by executor) meets max_retries 0, preserving lifecycle phase"`
- `"writes assignee: empty and preserves phase: ai_review at retry cap"`
- `"writes assignee: empty and preserves phase: in_progress at retry cap"`
- `"writes assignee: empty and preserves phase: planning at retry cap"`

**Tests to extend — Context "needs_input result":**
- `"does not increment retry_count when phase is human_review (needs_input path)"` — `previous_assignee: claude` (disk assignee was `claude`)
- `"does not increment retry_count when phase is already human_review and retry_count > 0 (terminal guard)"` — `previous_assignee: claude` (disk assignee was `claude`)
- `"clears assignee when agent emits needs_input (phase: human_review)"` — `previous_assignee: claude`

**Tests to extend — Context "trigger_count cap escalation":**
- `"keeps phase: human_review sticky when incoming payload carries stale phase: ai_review at cap"` — `previous_assignee: claude`
- `"does not append duplicate Trigger Cap Escalation section on repeated writes at cap"` — `previous_assignee: claude`
- `"keeps phase: human_review sticky despite inherited spawn_notification=true at already-parked task"` — `previous_assignee: claude`
- `"does not append duplicate Retry Escalation section on repeated writes at retry cap"` — `previous_assignee: claude`
- `"writes assignee: empty and preserves phase: ai_review at trigger cap"` — `previous_assignee: claude`
- `"writes assignee: empty and preserves phase: in_progress at trigger cap"` — `previous_assignee: claude`
- `"writes assignee: empty and preserves phase: planning at trigger cap"` — `previous_assignee: claude`
- `"keeps assignee empty and phase unchanged when stale result arrives at already-parked task"` — `previous_assignee: claude` (stale agent payload carries `assignee: claude`, which `clearAssignee` captures on re-delivery)
- `"escalation section body records the agent name active at escalation time, not the cleared value"` — `previous_assignee: claude`

**Important:** Do NOT add new `It` blocks for these — only extend the existing ones. The spec requires extension in-place.

## 7. Add new test: operator re-delegation persistence

Inside Context `"trigger_count cap escalation"`, add a new `It` block:

```go
It("previous_assignee persists when operator re-delegates by setting a non-empty assignee", func() {
    // First write: trigger cap fires — assignee cleared, previous_assignee set
    writeTaskFile(
        "my-task.md",
        "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\ntrigger_count: 3\nmax_triggers: 3\nassignee: claude\n---\n## Result\nStatus: failed\n",
    )
    taskFile = lib.Task{
        TaskIdentifier: identifier,
        Frontmatter: lib.TaskFrontmatter{
            "task_identifier": "test-task-uuid-1234",
            "status":          "in_progress",
            "phase":           "ai_review",
            "trigger_count":   3,
            "max_triggers":    3,
            "assignee":        "claude",
        },
        Content: lib.TaskContent("## Result\nStatus: failed\n"),
    }
    Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
    written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
    s := string(written)
    Expect(s).To(ContainSubstring("previous_assignee: claude"))
    Expect(s).NotTo(ContainSubstring("\nassignee: claude")) // line-anchored to skip previous_assignee:

    // Second write: operator re-delegates by setting a non-empty assignee
    taskFile = lib.Task{
        TaskIdentifier: identifier,
        Frontmatter: lib.TaskFrontmatter{
            "task_identifier": "test-task-uuid-1234",
            "status":          "in_progress",
            "phase":           "planning",
            "trigger_count":   0, // operator reset
            "max_triggers":    3,
            "assignee":        "backtest-agent", // re-delegation
        },
        Content: lib.TaskContent("## Task\nRetrying with backtest-agent.\n"),
    }
    Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
    written2, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
    s2 := string(written2)
    // previous_assignee must NOT be cleared or overwritten — it persists
    Expect(s2).To(ContainSubstring("previous_assignee: claude"))
    // new assignee is set
    Expect(s2).To(ContainSubstring("assignee: backtest-agent"))
})
```

**Why this works:** The second write has `trigger_count: 0`, so `applyTriggerCap` returns early without touching frontmatter. `clearAssignee` is never called. `previous_assignee: claude` from disk is preserved by `mergeFrontmatter` (agent didn't send `previous_assignee`, so the disk value is kept).

**Verify the merge contract first.** Before writing this test, read `mergeFrontmatter` in `task/controller/pkg/result/result_writer.go` (or wherever it lives) and confirm: when an incoming task's frontmatter does NOT contain a key that exists on disk, the merge preserves the disk value (does NOT remove it). If that contract does NOT hold, this test will fail and the helper needs the same treatment as `previous_assignee` itself — preserve-on-merge. Cite the line you verified in a comment next to the test.

## 8. Add new test: empty pre-clear assignee (defensive case)

Inside Context `"trigger_count cap escalation"`, add a new `It` block:

```go
It("does not set previous_assignee when pre-clear assignee is already empty (defensive case)", func() {
    // disk: assignee already "", no previous_assignee
    writeTaskFile(
        "my-task.md",
        "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\ntrigger_count: 3\nmax_triggers: 3\nassignee: \"\"\n---\n## Result\nStatus: failed\n",
    )
    taskFile = lib.Task{
        TaskIdentifier: identifier,
        Frontmatter: lib.TaskFrontmatter{
            "task_identifier": "test-task-uuid-1234",
            "status":          "in_progress",
            "phase":           "ai_review",
            "trigger_count":   3,
            "max_triggers":    3,
            "assignee":        "", // empty — malformed upstream state
        },
        Content: lib.TaskContent("## Result\nStatus: failed\n"),
    }
    Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
    written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
    s := string(written)
    // agentName captured from merged.Assignee() is "", so clearAssignee skips writing previous_assignee
    Expect(s).NotTo(ContainSubstring("previous_assignee:"))
    // escalation section is still appended
    Expect(s).To(ContainSubstring("## Trigger Cap Escalation"))
})
```

## 9.5. Add YAML round-trip test for `previous_assignee` (boundary test)

The substring-based assertions added in step 7 verify the field is present in the serialized bytes but do NOT verify it round-trips through the YAML parser back into a frontmatter map. Add one test (inside Context `"trigger_count cap escalation"` or as a sibling) that exercises the full marshal+unmarshal boundary:

```go
It("previous_assignee round-trips through YAML on a parked task", func() {
    writeTaskFile(
        "my-task.md",
        "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\ntrigger_count: 3\nmax_triggers: 3\nassignee: claude\n---\n## Result\nStatus: failed\n",
    )
    taskFile = lib.Task{
        TaskIdentifier: identifier,
        Frontmatter: lib.TaskFrontmatter{
            "task_identifier": "test-task-uuid-1234",
            "status":          "in_progress",
            "phase":           "ai_review",
            "trigger_count":   3,
            "max_triggers":    3,
            "assignee":        "claude",
        },
        Content: lib.TaskContent("## Result\nStatus: failed\n"),
    }
    Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())

    written, err := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
    Expect(err).NotTo(HaveOccurred())

    // Parse the written file's frontmatter back into a map and assert the key
    // exists with the expected value. This exercises the YAML marshal+unmarshal
    // boundary, not just substring presence in the bytes.
    fm, _, err := extractFrontmatterFromFile(written) // helper already used elsewhere in this test file
    Expect(err).NotTo(HaveOccurred())
    Expect(fm["previous_assignee"]).To(Equal("claude"))
})
```

If `extractFrontmatterFromFile` (or whatever the test file already uses to parse frontmatter — grep for `Unmarshal` or `yaml.NewDecoder` near the top of the test file) does not exist, use the same approach the surrounding tests use to parse YAML — DO NOT introduce a new YAML library.

## 9. Run tests after extending them

```bash
cd task/controller && make test
```

Expected: all tests pass including the two new `It` blocks. Fix compile errors before continuing.

## 10. Check coverage for `pkg/result/` package

```bash
cd task/controller && go test -coverprofile=/tmp/result-cover.out ./pkg/result/... && go tool cover -func=/tmp/result-cover.out | grep "total:"
```

Expected: ≥80% total coverage for the result package.

**Do NOT pass `-mod=vendor`** — `make ensure` (called by `make precommit`) removes the `vendor/` tree during iterative testing; the default `-mod=mod` is correct. `make precommit` re-creates vendor in its own pipeline as needed.

## 11. Update `docs/task-flow-and-failure-semantics.md`

Locate the **Terminology table** (around line 19). Find the `| **Escalation** | ... |` row. Extend the cell to append:

```
The controller also writes `previous_assignee: <name>` with the pre-clear agent name on every assignee-clear event, enabling operator queries by parked-by-agent without body parsing. The field persists across operator re-delegation. Reference: spec 027.
```

Add it as a continuation of the existing row text, separated by a space. The row is a Markdown table cell — keep it on one logical line.

## 12. Update `CHANGELOG.md` at repo root

There is already an `## Unreleased` section. Append to it:

```markdown
- feat(task/controller): write `previous_assignee` frontmatter field on every assignee-clear path (trigger cap, retry cap, needs_input) — captures the pre-clear agent name so operator-inbox queries can group parked tasks by parked-by-agent without parsing body content; persists across operator re-delegation
```

## 13. Run final precommit

```bash
cd task/controller && make precommit
```

Must exit 0. If any linter fails, run only the failing target (`make lint`, `make gosec`, etc.) and fix before retrying `make precommit`.

</requirements>

<constraints>
- Change is confined to `task/controller/pkg/result/result_writer.go`, `task/controller/pkg/result/result_writer_test.go`, `docs/task-flow-and-failure-semantics.md`, and root `CHANGELOG.md`. No file in `lib/*`, `task/executor/*`, `agent/*`, or `prompt/*` is modified. No `task/controller/mocks/` regeneration needed (no interface signature changes).
- The frontmatter field name is `previous_assignee` (snake_case, matches `current_job`, `task_identifier`, `task_type` conventions).
- `previous_assignee` is written ONLY when the pre-clear assignee is non-empty. When the captured `agentName` is `""`, the field is omitted — this is the defensive behavior and must be documented in the test from step 8.
- `previous_assignee` is NEVER written on non-clear paths (e.g., successful completions, spawn notifications, re-delegation). `clearAssignee` is only called from the three escalation code paths.
- The writer never reads `previous_assignee` to decide what to write; the value always comes from the live `merged.Assignee()` at the moment of the clear.
- Existing tests are extended in-place — do NOT add parallel `It` blocks for the assertion; only append `Expect(s).To(ContainSubstring("previous_assignee: claude"))` to each listed test.
- `make precommit` runs in `task/controller` — NEVER at repo root.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required.
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- No new mocks generated (no interface changes).
</constraints>

<verification>

Verify `clearAssignee` helper exists:
```bash
grep -n "func clearAssignee" task/controller/pkg/result/result_writer.go
```
Expected: one definition.

Verify `clearAssignee` is the only place that writes `previous_assignee`:
```bash
grep -n "previous_assignee" task/controller/pkg/result/result_writer.go
```
Expected: exactly two occurrences — one in `clearAssignee` (the write), zero elsewhere.

Verify all three escalation paths use `clearAssignee` (no bare `merged["assignee"] = ""`):
```bash
grep -n 'merged\["assignee"\] = ""' task/controller/pkg/result/result_writer.go
```
Expected: zero matches (all cleared via `clearAssignee`).

Verify test assertions for `previous_assignee` cover all three paths:
```bash
grep -n "previous_assignee" task/controller/pkg/result/result_writer_test.go
```
Expected: multiple matches spread across the retry, trigger cap, and needs_input contexts.

Verify re-delegation test exists:
```bash
grep -n "re-delegates\|re-delegation" task/controller/pkg/result/result_writer_test.go
```
Expected: at least one match.

Verify defensive edge case test exists:
```bash
grep -n "pre-clear assignee is already empty\|defensive case" task/controller/pkg/result/result_writer_test.go
```
Expected: at least one match.

Verify docs updated:
```bash
grep -n "previous_assignee" docs/task-flow-and-failure-semantics.md
```
Expected: at least one match.

Verify CHANGELOG updated:
```bash
grep -n "previous_assignee" CHANGELOG.md
```
Expected: at least one match under `## Unreleased`.

Run tests:
```bash
cd task/controller && make test
```
Expected: exit 0, all specs pass.

Run coverage:
```bash
cd task/controller && go test -coverprofile=/tmp/result-cover.out ./pkg/result/... && go tool cover -func=/tmp/result-cover.out | grep "total:"
```
Expected: ≥80%. Do NOT pass `-mod=vendor` — `make ensure` removed vendor; default mode is correct.

Run precommit:
```bash
cd task/controller && make precommit
```
Expected: exit 0.

</verification>
