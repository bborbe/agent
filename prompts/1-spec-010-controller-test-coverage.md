---
status: draft
spec: [010-failure-vs-needs-input-semantics]
created: "2026-04-19T11:00:00Z"
branch: dark-factory/failure-vs-needs-input-semantics
---

<summary>
- Adds a missing test case to the controller result writer: a result arriving with `phase: human_review` (e.g. from a `needs_input` agent response) must not increment `retry_count`
- Adds an explicit `needs_input` scenario test: agent emits `needs_input` → content-generator sets `phase: human_review` → controller writes it without retrying
- Confirms the "phase already `human_review` on second delivery" guard: if `retry_count` is already non-zero and a new result arrives with `phase: human_review`, the count stays unchanged
- Adds `## Unreleased` CHANGELOG entries documenting the spec 010 changes already shipped (parser, content-generator routing, controller guard)
- `cd task/controller && make precommit` passes
</summary>

<objective>
Complete the test coverage gap left by spec 010. The implementation in `task/controller/pkg/result/result_writer.go` already correctly skips the retry counter when `phase == "human_review"` (the `needs_input` path). The acceptance criteria requires an explicit unit test covering that guard. Add it now, along with a CHANGELOG entry for the shipped changes.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega patterns, external test packages

**Key files to read before editing:**

- `task/controller/pkg/result/result_writer.go` — `applyRetryCounter` (lines 131–151): the `phase == "human_review"` guard that skips retry increment; this is already implemented correctly
- `task/controller/pkg/result/result_writer_test.go` — existing test structure; `Context("retry counter", ...)` block (around line 472) and `Context("spawn notification", ...)` block (around line 589); add the new `needs_input` context block immediately after the `spawn notification` context
- `lib/delivery/content-generator.go` — confirms `AgentStatusNeedsInput` already sets `phase: human_review` in the frontmatter before publishing to Kafka
- `CHANGELOG.md` — top-level file; no `## Unreleased` section exists yet; check with `grep -n "Unreleased" CHANGELOG.md` before creating

**Implementation already complete (DO NOT re-implement):**

These files are already correct — only tests and CHANGELOG are missing:
- `lib/claude/task-runner.go` — `extractLastJSONObject` prose-tolerant JSON scanner
- `lib/claude/task-runner_test.go` — prose-prefix, prose-suffix, nested-braces tests (already present)
- `lib/delivery/content-generator.go` — `needs_input` → `human_review` routing (already present)
- `lib/delivery/content-generator_test.go` — all three status tests (already present)
- `task/controller/pkg/result/result_writer.go` — `human_review` guard in `applyRetryCounter` (already present)
</context>

<requirements>

1. **Add `Context("needs_input result", ...)` block to `task/controller/pkg/result/result_writer_test.go`**

   Insert immediately after the closing `})` of the `Context("spawn notification", ...)` block (around line 638).
   Test package stays `result_test` (external package, as already established).

   Add these test cases:

   a. **`needs_input` on first attempt: phase stays `human_review`, retry_count not incremented**

   The scenario: content-generator has already set `phase: human_review` in the incoming result frontmatter (which is how `needs_input` reaches the controller — via the content-generator's routing). The controller must write the file with `phase: human_review` and NOT increment `retry_count`.

   ```go
   Context("needs_input result", func() {
       It("does not increment retry_count when phase is human_review (needs_input path)", func() {
           writeTaskFile(
               "my-task.md",
               "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: ai_review\nassignee: claude\nretry_count: 0\n---\nOriginal body\n",
           )
           taskFile = lib.Task{
               TaskIdentifier: identifier,
               Frontmatter: lib.TaskFrontmatter{
                   "task_identifier": "test-task-uuid-1234",
                   "status":          "in_progress",
                   "phase":           "human_review",
               },
               Content: lib.TaskContent("No trades found in the requested window.\n"),
           }
           Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
           written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
           s := string(written)
           // retry_count must NOT be incremented
           Expect(s).To(ContainSubstring("retry_count: 0"))
           // phase must stay human_review — not overwritten by escalation logic
           Expect(s).To(ContainSubstring("phase: human_review"))
           // no escalation section — needs_input is not an infra failure
           Expect(s).NotTo(ContainSubstring("## Retry Escalation"))
           Expect(s).To(ContainSubstring("No trades found"))
       })

       It("does not increment retry_count when phase is already human_review and retry_count > 0 (terminal guard)", func() {
           writeTaskFile(
               "my-task.md",
               "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: human_review\nassignee: claude\nretry_count: 2\n---\nPrevious body\n",
           )
           taskFile = lib.Task{
               TaskIdentifier: identifier,
               Frontmatter: lib.TaskFrontmatter{
                   "task_identifier": "test-task-uuid-1234",
                   "status":          "in_progress",
                   "phase":           "human_review",
               },
               Content: lib.TaskContent("Another result arrives but task is terminal.\n"),
           }
           Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())
           written, _ := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
           s := string(written)
           // retry_count must remain at 2 — not incremented again
           Expect(s).To(ContainSubstring("retry_count: 2"))
           Expect(s).To(ContainSubstring("phase: human_review"))
           Expect(s).NotTo(ContainSubstring("## Retry Escalation"))
       })
   })
   ```

   Both test cases use the existing `writeTaskFile` helper, `identifier`, `taskFile`, `writer`, and `tmpDir` variables already declared in the `BeforeEach` block.

2. **Verify the existing tests still pass before running precommit**

   ```bash
   cd task/controller && make test
   ```
   Must exit 0.

3. **Add `## Unreleased` to `CHANGELOG.md`**

   First check:
   ```bash
   grep -n "Unreleased" CHANGELOG.md | head -3
   ```
   If `## Unreleased` already exists, APPEND the bullets below to it.
   If not, INSERT the section immediately ABOVE the first `## v` heading (which is currently `## v0.42.1`).

   Add:
   ```markdown
   ## Unreleased

   - feat: distinguish `needs_input` (task-level, human_review immediately, no retry) from `failed` (infra-level, retry up to max_retries)
   - fix: prose-wrapped Claude output no longer synthesises an infra failure; result parser extracts the last balanced JSON object from any surrounding text
   - fix: controller result writer skips retry counter when incoming result already has `phase: human_review`
   ```

</requirements>

<constraints>
- Do NOT change `task/controller/pkg/result/result_writer.go` — the implementation is already correct; add tests only
- Do NOT change `lib/` files — those are already complete (content-generator, task-runner, etc.)
- Do NOT change `agent/claude/` files — workflow.md guidance update is handled by prompt 2
- Do NOT commit — dark-factory handles git
- Test package must be `result_test` (external package, matches existing convention)
- Use `github.com/bborbe/errors` for any error wrapping in new code (the test helpers do not return errors)
- All existing tests must pass
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>
Verify the new test cases compile and pass:
```bash
cd task/controller && make test
```
Must exit 0.

Verify new test cases exist:
```bash
grep -n "needs_input result\|human_review.*needs_input\|terminal guard" task/controller/pkg/result/result_writer_test.go
```
Must show both new test descriptions.

Verify CHANGELOG entry:
```bash
grep -n -A5 "^## Unreleased" CHANGELOG.md | head -10
```
Must show the three new bullets.

Run full precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
