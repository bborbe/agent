---
status: completed
container: agent-021-unify-taskfile-lib-types
dark-factory-version: v0.69.0
created: "2026-03-30T17:29:00Z"
queued: "2026-03-30T18:21:24Z"
started: "2026-03-30T18:21:44Z"
completed: "2026-03-30T18:31:53Z"
---

<summary>
- Two separate task representations are merged into one unified type
- Each published event gets its own unique event identity
- Tasks carry a stable UUID business key from the frontmatter, not the file path
- Frontmatter is stored as a generic map with typed accessors for well-known fields
- Content is the markdown body after frontmatter (plain string, no type alias)
- Unused type aliases (TaskContent, TaskName) are removed
- The old structured Task type is deleted — everything uses the unified file-based type
- Phase accessor returns a pointer so callers can distinguish "no phase set" from "empty phase"
</summary>

<objective>
Merge the two task representations into a single file-based type that follows the standard CQRS entity pattern (event identity + stable business key). This eliminates the dual-type confusion and makes the same type usable in both directions (publish and receive).
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `lib/agent_task.go` — current `Task` struct (to be deleted)
- `lib/agent_task-file.go` — current `TaskFile` struct (to be modified)
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` type with typed accessors
- `lib/agent_task-content.go` — `TaskContent` type (to be deleted)
- `lib/agent_task-name.go` — `TaskName` type (to be deleted)
- `lib/agent_task-identifier.go` — `TaskIdentifier` type (keep as-is)
- `lib/agent_task-assignee.go` — `TaskAssignee` type (keep as-is)

Reference pattern (do NOT modify, shown here for context):
```go
// From trading project — standard CQRS entity pattern:
type Account struct {
    base.Object[base.Identifier]
    AccountIdentifier AccountIdentifier `json:"accountIdentifier"` // stable business key
    AccountName       AccountName       `json:"accountName,omitempty"`
    // ... other typed fields
}
```
</context>

<requirements>
1. **Modify `lib/agent_task-file.go`** — Add `base.Object[base.Identifier]` embed to `TaskFile`:

   **Before:**
   ```go
   type TaskFile struct {
       TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
       Frontmatter    TaskFrontmatter `json:"frontmatter"`
       Content        string          `json:"content"`
   }
   ```

   **After:**
   ```go
   type TaskFile struct {
       base.Object[base.Identifier]
       TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
       Frontmatter    TaskFrontmatter `json:"frontmatter"`
       Content        string          `json:"content"`
   }
   ```

   Add the import for `"github.com/bborbe/cqrs/base"`.

   Update the `Validate` method to also validate the embedded Object:
   ```go
   func (t TaskFile) Validate(ctx context.Context) error {
       return validation.All{
           validation.Name("Object", t.Object),
           validation.Name("TaskIdentifier", t.TaskIdentifier),
       }.Validate(ctx)
   }
   ```

2. **Modify `lib/agent_task-frontmatter.go`** — Change the `Phase()` accessor to return `*domain.TaskPhase` instead of `domain.TaskPhase`, so callers can distinguish "no phase" from "empty phase":

   **Before:**
   ```go
   func (f TaskFrontmatter) Phase() domain.TaskPhase {
       v, _ := f["phase"].(string)
       return domain.TaskPhase(v)
   }
   ```

   **After:**
   ```go
   func (f TaskFrontmatter) Phase() *domain.TaskPhase {
       v, ok := f["phase"].(string)
       if !ok || v == "" {
           return nil
       }
       p := domain.TaskPhase(v)
       return &p
   }
   ```

3. **Delete `lib/agent_task.go`** — Remove the entire file. The `Task` struct is replaced by `TaskFile`.

4. **Delete `lib/agent_task-content.go`** — Remove the entire file. The `TaskContent` type is no longer needed; `TaskFile.Content` is a plain `string`.

5. **Delete `lib/agent_task-name.go`** — Remove the entire file. The `TaskName` type is no longer needed; task name can be derived from frontmatter or file path by consumers if needed.

6. **Verify no compilation errors in `lib/`** by running:
   ```bash
   cd lib && go build ./...
   ```
   Fix any import issues that arise from the deletions.

7. **Add tests for `TaskFile.Validate`** in `lib/agent_task-file_test.go`:
   - Valid TaskFile with Object + TaskIdentifier passes validation
   - Empty TaskIdentifier fails validation
   - Test `TaskFrontmatter.Phase()` returns nil when key absent, returns pointer when present, returns nil for empty string

8. **Do NOT modify any files outside `lib/`** — downstream consumers (task/controller, task/executor) will be updated in subsequent prompts.
</requirements>

<constraints>
- Follow CQRS entity pattern from trading: `base.Object[base.Identifier]` + business key
- TaskIdentifier is UUID from frontmatter, NOT file path
- TaskFrontmatter stays as map[string]interface{} with typed accessors
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Use `github.com/bborbe/cqrs/base` for Object and Identifier types
- Do NOT commit — dark-factory handles git
- Do NOT modify files outside `lib/` — other services are updated in later prompts
</constraints>

<verification>
Run in `lib/`:

```bash
cd lib && go vet ./...
```
Must pass with exit code 0.

Verify Task struct is gone:
```bash
grep -r "type Task struct" lib/
```
Must produce no output.

Verify TaskContent is gone:
```bash
grep -r "TaskContent" lib/
```
Must produce no output.

Verify TaskName is gone:
```bash
grep -r "TaskName" lib/
```
Must produce no output.

Verify TaskFile has base.Object:
```bash
grep "base.Object" lib/agent_task-file.go
```
Must show one occurrence.

Note: `make test` and `make precommit` in `lib/` may fail because lib has no independent Makefile. Compilation check via `go vet` is sufficient. Full verification happens after downstream prompts.
</verification>
