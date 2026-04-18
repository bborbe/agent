---
status: completed
spec: [008-task-retry-protection]
summary: 'Added RetryCount() and MaxRetries() accessors to TaskFrontmatter and changed FallbackContentGenerator to set phase: ai_review on failure.'
container: agent-047-spec-008-lib-retry-fields
dark-factory-version: v0.122.0-6-g6b02e84
created: "2026-04-18T15:30:00Z"
queued: "2026-04-18T15:12:26Z"
started: "2026-04-18T15:12:27Z"
completed: "2026-04-18T15:17:22Z"
branch: dark-factory/task-retry-protection
---

<summary>
- `TaskFrontmatter` gains two typed accessors: `RetryCount() int` and `MaxRetries() int`
- `RetryCount()` returns 0 when the field is absent — safe default for new tasks
- `MaxRetries()` returns 3 when the field is absent — matches the spec default
- Both handle YAML-unmarshalled `int` and JSON-unmarshalled `float64` so they work across Kafka and file reads
- `FallbackContentGenerator` now writes `phase: ai_review` on failure instead of `phase: human_review`, aligning it with the Kafka deliverer's override
- The test for the fallback generator is updated to reflect the new phase value
- All other accessor behaviour (`Status()`, `Phase()`, `Assignee()`, `Stage()`) is unchanged
- `cd lib && make precommit` passes with exit 0
</summary>

<objective>
Add `RetryCount()` and `MaxRetries()` typed accessors to `lib.TaskFrontmatter` and change `FallbackContentGenerator` to set `phase: ai_review` on failure. These are the two lib-layer foundations required by spec 008: the accessors are consumed by the controller's retry counter (prompt 2), and fixing the fallback generator prevents it from bypassing the counter by writing `human_review` directly.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — accessor conventions, error wrapping
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega, external test packages

**Files to read before editing:**
- `lib/agent_task-frontmatter.go` — existing `Status()`, `Phase()`, `Assignee()`, `Stage()` accessors; add new ones here
- `lib/agent_task_test.go` — existing lib tests; check if it covers frontmatter (may need a new test file)
- `lib/delivery/content-generator.go` — `FallbackContentGenerator.Generate`; change `human_review` → `ai_review` on failure path
- `lib/delivery/content-generator_test.go` — update the failing-result test to expect `ai_review`

**Frontmatter type context:**
`TaskFrontmatter` is `map[string]interface{}`. Values come from two sources:
- YAML (`gopkg.in/yaml.v3`): integers unmarshal as `int`
- JSON (Kafka): integers unmarshal as `float64`

Both cases must be handled in the accessors.

**Existing accessor pattern (from `lib/agent_task-frontmatter.go`):**
```go
func (f TaskFrontmatter) Status() domain.TaskStatus {
    v, _ := f["status"].(string)
    return domain.TaskStatus(v)
}
```
New accessors follow the same shape but return `int` with type-switching for `int`/`float64`.

**Why `MaxRetries()` default is 3:**
The spec states: "Tasks without `max_retries` field use the controller's default (3)". The default is encoded in the accessor itself so the controller does not need a magic constant.

**Why change `FallbackContentGenerator`:**
Currently `FallbackContentGenerator.Generate` sets `phase: human_review` on failure. This matters when `FileResultDeliverer` uses the generator directly (file-based delivery): the file gets `phase: human_review`, the executor skips it permanently, and the controller's retry counter never runs. Changing to `phase: ai_review` aligns the file-based delivery path with the Kafka path and lets the controller manage escalation.

The `KafkaResultDeliverer` overrides phase to `ai_review` regardless of what the generator produces, so this change does not affect Kafka delivery.
</context>

<requirements>

1. **Add `RetryCount() int` accessor to `lib/agent_task-frontmatter.go`**

   Insert after the `Stage()` method. Return 0 if the key is absent or the value is not a recognised numeric type.

   ```go
   // RetryCount returns the number of failed attempts recorded in frontmatter.
   // Returns 0 when the field is absent.
   func (f TaskFrontmatter) RetryCount() int {
       switch v := f["retry_count"].(type) {
       case int:
           return v
       case float64:
           return int(v)
       default:
           return 0
       }
   }
   ```

2. **Add `MaxRetries() int` accessor to `lib/agent_task-frontmatter.go`**

   Insert after `RetryCount()`. Return 3 (the spec default) when the key is absent.

   ```go
   // MaxRetries returns the maximum number of failures allowed before escalation.
   // Returns 3 when the field is absent (spec default).
   func (f TaskFrontmatter) MaxRetries() int {
       switch v := f["max_retries"].(type) {
       case int:
           return v
       case float64:
           return int(v)
       default:
           return 3
       }
   }
   ```

3. **Add tests for `RetryCount()` and `MaxRetries()`**

   Add two new `Describe("RetryCount", …)` and `Describe("MaxRetries", …)` blocks inside the existing `Describe("TaskFrontmatter", …)` block in `lib/agent_task_test.go` (starts at line 76). Do NOT create a new test file. Test package stays `lib_test` (already set there).

   Required test cases for `RetryCount()`:
   - Key absent → 0
   - Key present as `int` (YAML path) → correct value
   - Key present as `float64` (JSON/Kafka path) → correct value
   - Key present as `0` → 0 (not confused with absent)

   Required test cases for `MaxRetries()`:
   - Key absent → 3 (default)
   - Key present as `int` 5 → 5
   - Key present as `float64` 10.0 → 10
   - Key present as `0` → 0 (explicit zero is honoured; spec: `max_retries: 0` escalates on first failure)

4. **Fix `FallbackContentGenerator.Generate` in `lib/delivery/content-generator.go`**

   In the `default` branch of the `switch result.Status` block, change `"human_review"` to `"ai_review"`:

   ```go
   // Before:
   updated = SetFrontmatterField(updated, "phase", "human_review")

   // After:
   updated = SetFrontmatterField(updated, "phase", "ai_review")
   ```

   Do NOT change the `status: in_progress` line — only the phase changes.

5. **Update `lib/delivery/content-generator_test.go`**

   The test case `"sets status=in_progress and phase=human_review for failed result"` must be updated:
   - Rename the `It` description to `"sets status=in_progress and phase=ai_review for failed result"`
   - Change the assertion from `Expect(generated).To(ContainSubstring("phase: human_review"))` to `Expect(generated).To(ContainSubstring("phase: ai_review"))`

   All other test cases remain unchanged.

</requirements>

<constraints>
- Do NOT change `Status()`, `Phase()`, `Assignee()`, or `Stage()` — only add new accessors
- Do NOT change `TaskFrontmatter` type definition or any other lib types
- Do NOT change the Kafka schema, `Task` struct, or `TaskContent` type
- Do NOT commit — dark-factory handles git
- Use `github.com/bborbe/errors` for error wrapping in any new error-returning code (the new accessors do not return errors)
- All existing tests must pass
- `make precommit` passes in `lib/`
- Test package must be `lib_test` (external test package per project convention)
</constraints>

<verification>
```bash
cd lib && make precommit
```
Must pass with exit code 0.

Verify new accessors exist:
```bash
grep -n "RetryCount\|MaxRetries" lib/agent_task-frontmatter.go
```
Must show both methods.

Verify fallback generator no longer writes human_review:
```bash
grep -n "human_review" lib/delivery/content-generator.go
```
Must return zero matches.

Verify fallback generator writes ai_review on failure:
```bash
grep -n "ai_review" lib/delivery/content-generator.go
```
Must show the failure-path assignment.

Verify tests cover max_retries: 0 (explicit zero respected):
```bash
grep -n "max_retries.*0\|0.*max_retries\|MaxRetries.*0\|0.*MaxRetries" lib/agent_task-frontmatter_test.go 2>/dev/null || grep -rn "max_retries.*0\|MaxRetries.*0" lib/
```
Must show a test covering the explicit-zero case.
</verification>
