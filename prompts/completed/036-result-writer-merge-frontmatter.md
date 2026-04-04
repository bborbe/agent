---
status: completed
spec: [006-result-writer-conflict-resolution]
summary: ResultWriter now merges existing task file frontmatter with agent-provided frontmatter, preserving keys like assignee, tags, and custom fields while agent keys override existing values
container: agent-036-result-writer-merge-frontmatter
dark-factory-version: v0.95.0
created: "2026-04-04T18:34:17Z"
queued: "2026-04-04T18:34:17Z"
started: "2026-04-04T18:36:12Z"
completed: "2026-04-04T18:46:32Z"
---

<summary>
- Result writeback preserves existing task file frontmatter keys that the agent didn't send
- Agent-provided frontmatter keys override existing ones (status, phase updated by agent)
- Original keys like assignee, tags, task_identifier survive the writeback
- Existing tests updated to verify merge behavior
- Empty agent frontmatter no longer wipes existing file metadata
</summary>

<objective>
Change ResultWriter to merge incoming agent frontmatter with the existing file's frontmatter instead of replacing it entirely. Existing keys not present in the agent's response are preserved. Agent-provided keys override existing values. This fixes the e2e bug where result writeback loses assignee, tags, and other human-authored frontmatter.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read the coding guides from `~/Documents/workspaces/coding/docs/`: `go-architecture-patterns.md`, `go-testing-guide.md`, `go-error-wrapping-guide.md`.

**The bug:** When agent-backtest publishes a result, the controller's ResultWriter replaces the entire task file (frontmatter + content) with whatever the agent sent. The agent only sends `status`, `phase`, and `task_identifier` in frontmatter — so keys like `assignee`, `tags`, `retrigger` are lost.

**Root cause in `task/controller/pkg/result/result_writer.go` lines 90-97:**
```go
marshaledFrontmatter, err := yaml.Marshal(map[string]any(req.Frontmatter))
newContent := []byte(
    "---\n" + string(marshaledFrontmatter) + "---\n" + sanitizeContent(string(req.Content)),
)
```
This serializes only the agent's frontmatter. The existing file's frontmatter (already read during the WalkDir search at lines 54-83) is discarded.

**The fix:** After finding the matched file, read its existing frontmatter, merge the agent's frontmatter on top (agent keys win), then serialize the merged result.

Key files to read before making changes:
- `task/controller/pkg/result/result_writer.go` — current implementation
- `task/controller/pkg/result/result_writer_test.go` — existing tests to update
- `task/controller/pkg/result/result_suite_test.go` — test suite bootstrap
- `lib/agent_task.go` — `Task` struct with `Frontmatter` and `Content` fields
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` type (likely `map[string]any`)
</context>

<requirements>
### 1. Read existing frontmatter before writing

In `result_writer.go`, the `WriteResult` method already reads the matched file's content during the WalkDir loop (line 58: `content, readErr := fs.ReadFile(fsys, path)`), but discards it after extracting `task_identifier`. Instead, preserve the existing frontmatter when a match is found.

**Approach:** When a match is found, also parse and store the full existing frontmatter (not just task_identifier). After the WalkDir loop, merge existing + incoming frontmatter before writing.

Concrete changes:
- Add a variable `var existingFrontmatter lib.TaskFrontmatter` alongside `matchedAbsPath`
- When match is found (line 76-79), also unmarshal the raw frontmatter string (from `extractFrontmatter`) into a `lib.TaskFrontmatter` map and store it in `existingFrontmatter`
- After the WalkDir loop, merge: start with `existingFrontmatter`, then overlay all keys from `req.Frontmatter`

### 2. Merge frontmatter (existing + agent)

Create a private helper:
```go
// mergeFrontmatter returns a new frontmatter map with all keys from existing,
// overridden by all keys from incoming. Neither input map is modified.
func mergeFrontmatter(existing, incoming lib.TaskFrontmatter) lib.TaskFrontmatter {
    merged := make(lib.TaskFrontmatter, len(existing)+len(incoming))
    for k, v := range existing {
        merged[k] = v
    }
    for k, v := range incoming {
        merged[k] = v
    }
    return merged
}
```

### 3. Use merged frontmatter when writing

Replace lines 90-91:
```go
// Before:
marshaledFrontmatter, err := yaml.Marshal(map[string]any(req.Frontmatter))

// After:
merged := mergeFrontmatter(existingFrontmatter, req.Frontmatter)
marshaledFrontmatter, err := yaml.Marshal(map[string]any(merged))
```

### 4. Update tests

**Update "writes frontmatter and content to the matched file" test:**
- Existing file has `task_identifier`, `status`, `assignee: backtest-agent`, `tags: [agent-task, test]`
- Agent sends `status: done`, `phase: done`, `task_identifier`
- Assert result file contains: `assignee: backtest-agent`, `tags`, `status: done`, `phase: done`

**Add new test "preserves existing frontmatter keys not sent by agent":**
- Existing file: `task_identifier: X`, `assignee: backtest-agent`, `tags: [a, b]`, `custom_field: hello`
- Agent sends: `task_identifier: X`, `status: completed`, `phase: done`
- Assert result file:
  - Has `assignee: backtest-agent` (preserved)
  - Has `tags: [a, b]` (preserved)
  - Has `custom_field: hello` (preserved)
  - Has `status: completed` (from agent)
  - Has `phase: done` (from agent)
  - Has `task_identifier: X` (both had it)

**Add new test "agent keys override existing keys":**
- Existing file: `task_identifier: X`, `status: in_progress`, `phase: in_progress`
- Agent sends: `task_identifier: X`, `status: completed`, `phase: done`
- Assert: `status: completed`, `phase: done` (agent wins)

**Update "empty frontmatter" test:**
- When agent sends empty frontmatter, existing keys should ALL be preserved
- Existing file: `task_identifier: X`, `assignee: backtest-agent`
- Agent sends: empty frontmatter
- Assert: `task_identifier: X`, `assignee: backtest-agent` both preserved

**Update "fully replaces content on second call" test:**
- Content should still be fully replaced (only frontmatter is merged)
- Verify content replacement still works as before
</requirements>

<constraints>
- Only frontmatter is merged — content is still fully replaced by the agent's content (agent owns content transformation)
- Agent keys always win when both existing and incoming have the same key
- Do NOT change the `Task` struct, `TaskFrontmatter` type, or any lib/ types
- Do NOT change the CQRS command format or Kafka schema
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Use `github.com/golang/glog` for logging
- Do NOT commit — dark-factory handles git
- All existing tests must pass (some will need updating per requirements)
- `make precommit` passes in `task/controller`
</constraints>

<verification>
Verify mergeFrontmatter function exists:
```bash
grep -n "mergeFrontmatter" task/controller/pkg/result/result_writer.go
```
Must show the function.

Verify existing frontmatter is read and stored:
```bash
grep -n "existingFrontmatter" task/controller/pkg/result/result_writer.go
```
Must show variable declaration and usage.

Verify tests cover preservation:
```bash
grep -n "preserves\|assignee\|merge" task/controller/pkg/result/result_writer_test.go
```
Must show new test cases.

Run tests:
```bash
cd task/controller && make test
```
Must exit 0.

Run precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
