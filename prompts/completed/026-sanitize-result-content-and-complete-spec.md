---
status: completed
spec: [005-agent-result-capture]
summary: Added sanitizeContent function to escape bare --- lines in agent result content before writing task files, added corresponding test case, and marked spec 005 as completed.
container: agent-026-sanitize-result-content-and-complete-spec
dark-factory-version: v0.89.1-dirty
created: "2026-04-03T12:15:00Z"
queued: "2026-04-03T10:39:22Z"
started: "2026-04-03T10:39:24Z"
completed: "2026-04-03T10:53:44Z"
---

<summary>
- Result content containing YAML frontmatter delimiters no longer corrupts task files
- Sanitization strips leading triple-dash lines from agent output before writing
- Test covers the corruption scenario with embedded delimiters
- Written files always have exactly two frontmatter delimiters
- Spec 005 acceptance criteria verified and marked complete
</summary>

<objective>
Prevent agent result content containing `---` from corrupting task file YAML frontmatter, and mark spec 005-agent-result-capture as complete since all acceptance criteria are met.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Problem:** ResultWriter writes `---\n{frontmatter}\n---\n{content}` to task files. If agent output in `content` contains a line that is exactly `---`, Obsidian and any YAML parser will interpret it as a frontmatter boundary, corrupting the file structure.

**Current code:** `result_writer.go` line 88 builds the file as:
```go
newContent := []byte("---\n" + string(marshaledFrontmatter) + "---\n" + string(req.Content))
```

No sanitization is applied to `req.Content`.

**Fix:** Before writing, replace any line that is exactly `---` (with optional trailing whitespace) in the content with `\-\-\-` (escaped). This preserves readability while preventing YAML parser confusion.

Files to read before making changes:
- `task/controller/pkg/result/result_writer.go` — WriteResult method, line 88 is the write point
- `task/controller/pkg/result/result_writer_test.go` — existing tests to follow patterns
- `specs/in-progress/005-agent-result-capture.md` — spec to mark complete
</context>

<requirements>
1. **Add content sanitization in `result_writer.go`:**
   - Before line 88 (the `newContent` assignment), sanitize `req.Content`
   - Replace any line that is exactly `---` (with optional trailing whitespace/newline) with `\-\-\-`
   - Use `strings.ReplaceAll` or a simple line-by-line scan
   - Only replace lines that are exactly `---` — do not touch `---` embedded within longer lines
   - Extract sanitization into a private function `sanitizeContent(content string) string`

2. **Add test case in `result_writer_test.go`:**
   - New `Context("content with YAML delimiters")` block
   - Write a task file with valid frontmatter
   - Call WriteResult with content containing `---` on its own line, e.g.:
     ```
     ## Result\n\nOutput:\n---\nsome yaml block\n---\nDone.\n
     ```
   - Assert the written file has valid frontmatter (parseable YAML between first two `---`)
   - Assert the content portion contains `\-\-\-` instead of bare `---`
   - Assert `CommitAndPush` was called

3. **Mark spec 005 as complete:**
   - In `specs/in-progress/005-agent-result-capture.md`, change `status: verifying` to `status: completed`
   - Add `completed: "2026-04-03"` to frontmatter
   - Check all acceptance criteria checkboxes (replace `- [ ]` with `- [x]`)
</requirements>

<constraints>
- Do NOT change the frontmatter writing logic — only sanitize the content portion
- Do NOT change any existing test cases
- Do NOT update CHANGELOG.md
- Do NOT commit — dark-factory handles git
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Sanitize only exact `---` lines, not `---` within other text (e.g. `foo---bar` stays unchanged)
</constraints>

<verification>
Run tests in task/controller:

```bash
cd task/controller && make test
```
Must pass with exit code 0.

Verify sanitization exists:

```bash
grep -n "sanitizeContent" task/controller/pkg/result/result_writer.go
```
Must show the function definition and usage.

Verify spec is marked complete:

```bash
grep "status:" specs/in-progress/005-agent-result-capture.md | head -1
```
Must show `status: completed`.

Run precommit:

```bash
cd task/controller && make precommit
```
Must pass with exit code 0.
</verification>
