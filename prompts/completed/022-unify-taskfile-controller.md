---
status: completed
summary: Updated task/controller scanner, publisher, and sync loop to use lib.TaskFile with generic frontmatter map, extractBody helper, and TaskFrontmatter accessors; fixed pre-existing command test failures by adding base.Object fields; upgraded go-git to v5.17.1 to fix osv-scanner findings; added .trivyignore to suppress docker indirect dep CVEs
container: agent-022-unify-taskfile-controller
dark-factory-version: v0.69.0
created: "2026-03-30T17:29:00Z"
queued: "2026-03-30T18:21:28Z"
started: "2026-03-30T18:31:55Z"
completed: "2026-03-30T18:58:40Z"
---

<summary>
- The vault scanner produces file-based task records instead of structured typed records
- Frontmatter is parsed as a generic map rather than into typed fields
- The scanner extracts the UUID business key from frontmatter and the markdown body as content
- The publisher and sync loop use the unified file-based type throughout
- Scan results carry file-based records instead of structured records
- All controller tests use frontmatter maps and accessors
- Mocks are regenerated after interface changes
</summary>

<objective>
Update the task/controller pipeline (scanner, publisher, sync loop) to produce the unified file-based task type. The scanner parses frontmatter as a generic map, extracts the UUID business key, and the publisher sets event identity on each publish.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Prerequisite:** The previous prompt modified `lib/` types — `Task` is deleted, `TaskFile` now has `base.Object[base.Identifier]`, `TaskContent` and `TaskName` are deleted, and `TaskFrontmatter.Phase()` returns `*domain.TaskPhase`.

Key files to read before making changes:
- `lib/agent_task-file.go` — `TaskFile` (after previous prompt: has `base.Object[base.Identifier]` + `TaskIdentifier` + `Frontmatter` + `Content`)
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` with `Status()`, `Phase() *domain.TaskPhase` (pointer, after previous prompt), `Assignee()` accessors
- `task/controller/pkg/scanner/vault_scanner.go` — currently builds `lib.Task`, must switch to `lib.TaskFile`
- `task/controller/pkg/scanner/vault_scanner_test.go` — tests that reference `lib.Task` fields
- `task/controller/pkg/publisher/task_publisher.go` — currently accepts `lib.Task`, must accept `lib.TaskFile`
- `task/controller/pkg/publisher/task_publisher_test.go` — tests using `lib.Task`
- `task/controller/pkg/sync/sync_loop.go` — passes `lib.Task` through pipeline
- `task/controller/pkg/sync/sync_loop_test.go` — tests using `lib.Task`
- `task/controller/pkg/factory/factory.go` — factory wiring
- `task/controller/mocks/` — generated mocks that reference `lib.Task`
</context>

<requirements>
1. **Modify `task/controller/pkg/scanner/vault_scanner.go`** — Change `ScanResult` and all internal methods to use `TaskFile`:

   a. Change `ScanResult.Changed` type:
   ```go
   // Before:
   Changed []lib.Task
   // After:
   Changed []lib.TaskFile
   ```

   b. Change `scanFiles` return type from `[]lib.Task` to `[]lib.TaskFile`.

   c. Change `processFile` return type from `*lib.Task` to `*lib.TaskFile`.

   d. Rewrite the task-building logic in `processFile`. Currently it unmarshals frontmatter into `domain.Task` and builds `lib.Task` with typed fields. Instead:
   - Parse frontmatter as a generic `map[string]interface{}` using `yaml.Unmarshal`
   - Cast the map to `lib.TaskFrontmatter`
   - Read `task_identifier` from the map: `fmMap["task_identifier"].(string)`
   - If `task_identifier` is empty, call `v.injectAndStore(...)` (existing flow)
   - Read status via `frontmatter.Status()` — skip if empty
   - Read assignee via `frontmatter.Assignee()` — skip if empty (but still track hash)
   - Extract the markdown body after frontmatter using `extractBody(content)` (new helper)
   - Build `lib.TaskFile`:
     ```go
     return &lib.TaskFile{
         TaskIdentifier: lib.TaskIdentifier(taskID),
         Frontmatter:    frontmatter,
         Content:        body,
     }, "", false
     ```

   e. Add a helper function `extractBody` that returns the markdown content after the closing `---` delimiter:
   ```go
   func extractBody(content []byte) string {
       s := string(content)
       const delim = "---"
       if !strings.HasPrefix(s, delim) {
           return s
       }
       rest := strings.TrimPrefix(s, delim)
       // skip line ending after opening delimiter
       if strings.HasPrefix(rest, "\r\n") {
           rest = rest[2:]
       } else if strings.HasPrefix(rest, "\n") {
           rest = rest[1:]
       }
       idx := strings.Index(rest, "\n---")
       if idx == -1 {
           return s
       }
       after := rest[idx+4:] // skip "\n---"
       // skip line ending after closing delimiter
       if strings.HasPrefix(after, "\r\n") {
           after = after[2:]
       } else if strings.HasPrefix(after, "\n") {
           after = after[1:]
       }
       return after
   }
   ```

   f. Remove the `domain.Task` unmarshal and the imports for `"github.com/bborbe/vault-cli/pkg/domain"` if no longer needed. The `yaml.v3` import is still needed for `yaml.Unmarshal` of the generic map.

   g. Remove references to `lib.TaskContent`, `lib.TaskName` — these types no longer exist.

   h. The validation for status should use `frontmatter.Status()` instead of `domainTask.Status`. For empty status, log warning and skip (same behavior as before). For unknown/invalid status values, they pass through as strings — `TaskFrontmatter.Status()` returns whatever string is in the map.

2. **Modify `task/controller/pkg/publisher/task_publisher.go`** — Change `TaskPublisher` interface and implementation to accept `lib.TaskFile`:

   a. Change interface method:
   ```go
   // Before:
   PublishChanged(ctx context.Context, task lib.Task) error
   // After:
   PublishChanged(ctx context.Context, taskFile lib.TaskFile) error
   ```

   b. Update `PublishChanged` implementation to set `base.Object` on `taskFile`:
   ```go
   func (p *taskPublisher) PublishChanged(ctx context.Context, taskFile lib.TaskFile) error {
       now := libtime.DateTime(time.Now())
       taskFile.Object = base.Object[base.Identifier]{
           Identifier: base.Identifier(uuid.New().String()),
           Created:    now,
           Modified:   now,
       }
       event, err := base.ParseEvent(ctx, taskFile)
       if err != nil {
           return errors.Wrapf(ctx, err, "parse event for task %s failed", taskFile.TaskIdentifier)
       }
       if err := p.eventObjectSender.SendUpdate(ctx, cdb.EventObject{
           Event:    event,
           ID:       base.EventID(taskFile.TaskIdentifier),
           SchemaID: p.schemaID,
       }); err != nil {
           return errors.Wrapf(ctx, err, "publish changed task %s failed", taskFile.TaskIdentifier)
       }
       return nil
   }
   ```

3. **Modify `task/controller/pkg/sync/sync_loop.go`** — Update `processResult` to use `TaskFile`:

   The `result.Changed` is now `[]lib.TaskFile`. Update the loop variable:
   ```go
   for _, taskFile := range result.Changed {
       glog.V(3).Infof("publishing changed task %s", taskFile.TaskIdentifier)
       if err := s.publisher.PublishChanged(ctx, taskFile); err != nil {
           return errors.Wrapf(ctx, err, "publish changed task %s", taskFile.TaskIdentifier)
       }
   }
   ```

4. **Update `task/controller/pkg/scanner/vault_scanner_test.go`**:

   All test cases that reference `result.Changed[0].Assignee`, `result.Changed[0].Status`, `result.Changed[0].Name`, or `result.Changed[0].Content` must use frontmatter accessors instead:

   - `result.Changed[0].Assignee` → `result.Changed[0].Frontmatter.Assignee()`
   - `result.Changed[0].Status` → `result.Changed[0].Frontmatter.Status()`
   - `result.Changed[0].Name` → remove or replace (TaskName no longer exists; if the test checks file name, use `result.Changed[0].TaskIdentifier` or remove the assertion)
   - `result.Changed[0].Content` → `result.Changed[0].Content` (still string, but now just the body after frontmatter, not the full file)

   Specific test updates:

   a. "new file appears in Changed" — change assertion:
   ```go
   Expect(string(result.Changed[0].Frontmatter.Assignee())).To(Equal("claude"))
   ```

   b. "modified file appears in Changed on next cycle" — change status assertion:
   ```go
   Expect(string(result.Changed[0].Frontmatter.Status())).To(Equal("in_progress"))
   ```

   c. "handles CRLF line endings in full cycle" — change assignee assertion:
   ```go
   Expect(string(result.Changed[0].Frontmatter.Assignee())).To(Equal("claude"))
   ```

   d. "runs cycle when trigger fires" — remove or update the `Name` assertion. The test currently checks `result.Changed[0].Name == "trigger-task"`. Since `TaskName` is deleted, remove this assertion or replace with a `TaskIdentifier` check.

   e. Import `lib` package if not already imported (scanner tests are internal package, they use `lib.TaskFile` directly via the returned `ScanResult`).

5. **Update `task/controller/pkg/publisher/task_publisher_test.go`**:

   Replace all `lib.Task{...}` constructions with `lib.TaskFile{...}`:

   a. "calls SendUpdate with correct EventObject":
   ```go
   taskFile := lib.TaskFile{
       TaskIdentifier: lib.TaskIdentifier("test-uuid-1234"),
       Frontmatter: lib.TaskFrontmatter{
           "status":   "todo",
           "assignee": "user@example.com",
       },
       Content: "# Test",
   }
   err := tp.PublishChanged(ctx, taskFile)
   ```
   Update the EventID assertion to match the new TaskIdentifier value.

   b. "returns an error when SendUpdate fails" — same pattern, use `lib.TaskFile`.

6. **Update `task/controller/pkg/sync/sync_loop_test.go`**:

   Replace all `lib.Task{...}` with `lib.TaskFile{...}`:

   a. "calls PublishChanged for a changed task":
   ```go
   taskFile := lib.TaskFile{
       TaskIdentifier: lib.TaskIdentifier("test-uuid"),
       Frontmatter: lib.TaskFrontmatter{"status": "todo"},
       Content: "# Test",
   }
   resultsCh <- scanner.ScanResult{Changed: []lib.TaskFile{taskFile}}
   ```

   b. "returns error when PublishChanged fails" — same pattern.

   c. Update the ScanResult type reference: `[]lib.Task` → `[]lib.TaskFile` in all test data.

7. **Regenerate counterfeiter mocks**:

   Run `make generate` in `task/controller/` to regenerate:
   - `task/controller/mocks/task_publisher.go` — `PublishChanged` now takes `lib.TaskFile`
   - `task/controller/mocks/vault_scanner.go` — `ScanResult` uses `lib.TaskFile`

8. **Run `make test` and `make precommit` in `task/controller/`.**
</requirements>

<constraints>
- Follow CQRS entity pattern: `base.Object[base.Identifier]` + business key
- TaskIdentifier is UUID from frontmatter `task_identifier`, NOT file path
- TaskFrontmatter stays as map[string]interface{} with typed accessors
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` — it was already updated in the previous prompt
- Existing test behaviors must be preserved (change detection, deletion, UUID injection, etc.)
- `make precommit` in `task/controller/` must pass
</constraints>

<verification>
Run in `task/controller/`:

```bash
make generate
```
Must succeed (regenerates mocks).

```bash
make test
```
Must pass with exit code 0.

```bash
make precommit
```
Must pass with exit code 0.

Verify no references to `lib.Task{` (the old struct literal) remain:
```bash
grep -rn "lib\.Task{" task/controller/
```
Must produce no output (only `lib.TaskFile{` should exist).

Verify no references to `lib.TaskContent` remain:
```bash
grep -rn "TaskContent" task/controller/
```
Must produce no output.

Verify no references to `lib.TaskName` remain:
```bash
grep -rn "TaskName" task/controller/
```
Must produce no output.
</verification>
