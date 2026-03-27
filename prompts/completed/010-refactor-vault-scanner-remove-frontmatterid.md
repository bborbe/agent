---
status: completed
summary: Eliminated frontmatterID struct and parseTask method; processFile now parses domain.Task once and uses TaskIdentifier directly, with tests updated to cover processFile edge cases via runCycle integration tests
container: agent-010-refactor-vault-scanner-remove-frontmatterid
dark-factory-version: v0.69.0
created: "2026-03-27T16:00:00Z"
queued: "2026-03-27T21:32:07Z"
started: "2026-03-27T22:40:19Z"
completed: "2026-03-27T22:47:14Z"
---

<summary>
- The custom frontmatterID struct is removed — domain.Task.TaskIdentifier replaces it
- processFile no longer double-parses: it reads the file once, parses domain.Task once, and uses all fields directly
- The separate parseTask method is eliminated — its logic merges into processFile
- Files without task_identifier still get UUID injection via the existing injectTaskIdentifier flow
- Files without assignee or with invalid status are still silently skipped
- All existing scan behaviors (change detection, deletion detection, UUID injection, commit-and-push) are preserved
- Tests are updated to remove parseTask references and test processFile/runCycle paths instead
</summary>

<objective>
Eliminate the `frontmatterID` struct and the `parseTask` method from `vault_scanner.go`. Instead, parse `domain.Task` once in `processFile` and use its `TaskIdentifier` field directly. This removes redundant file reads and double-parsing of frontmatter.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner.go` — contains `frontmatterID` struct (lines ~39-41), `processFile` method, `parseTask` method, `injectAndStore`, `extractFrontmatter`, `injectTaskIdentifier`
- `task/controller/pkg/scanner/vault_scanner_test.go` — contains tests for `parseTask`, `injectTaskIdentifier`, `runCycle`, `Run`
- `task/controller/vendor/github.com/bborbe/vault-cli/pkg/domain/task.go` — `domain.Task` struct with `TaskIdentifier string` field (`yaml:"task_identifier,omitempty"`)
- `lib/agent_task.go` — `lib.Task` struct that processFile ultimately returns
- `lib/agent_task-identifier.go` — `lib.TaskIdentifier` type
</context>

<requirements>
1. **Remove the `frontmatterID` struct** from `task/controller/pkg/scanner/vault_scanner.go`. Delete:
   ```go
   type frontmatterID struct {
       TaskIdentifier string `yaml:"task_identifier"`
   }
   ```

2. **Rewrite `processFile`** to parse `domain.Task` instead of `frontmatterID`, and merge the `parseTask` logic into it. The new `processFile` should:

   a. Read the file content via `fs.ReadFile(fsys, path)` (unchanged)
   b. Compute SHA-256 hash and check against `v.hashes[relPath]` for early return (unchanged)
   c. Call `extractFrontmatter(ctx, content)` to get the raw frontmatter string (unchanged)
   d. Unmarshal frontmatter into `domain.Task` (via `yaml.Unmarshal`) instead of `frontmatterID`
   e. If `domainTask.TaskIdentifier == ""`, call `v.injectAndStore(content, absPath, relPath)` and return (same as before)
   f. If `domainTask.TaskIdentifier != ""`:
      - Validate: if `domainTask.Status == ""`, log warning and return `nil, "", false`
      - If `domainTask.Assignee == ""`, store the hash entry and return `nil, "", false` (file is tracked but not published)
      - Otherwise, store the hash entry and build + return the `lib.Task` directly:
        ```go
        name := strings.TrimSuffix(filepath.Base(absPath), ".md")
        task := &lib.Task{
            TaskIdentifier: lib.TaskIdentifier(domainTask.TaskIdentifier),
            Name:           lib.TaskName(name),
            Status:         domainTask.Status,
            Phase:          domainTask.Phase,
            Assignee:       lib.TaskAssignee(domainTask.Assignee),
            Content:        lib.TaskContent(content),
        }
        ```

   IMPORTANT: When `domainTask.Assignee == ""`, still update `v.hashes[relPath]` so the file is tracked for change/deletion detection. The current `parseTask` returns nil but the hash was already stored in `processFile` before calling `parseTask`. The new code must preserve this behavior.

   IMPORTANT: When `domainTask.Status` is invalid (e.g., "definitely_invalid_status"), `domain.TaskStatus.UnmarshalYAML` returns an error during `yaml.Unmarshal`. This means the yaml.Unmarshal call itself will fail. Handle this by logging a warning and returning `nil, "", false` — same as the current behavior where `parseTask` logs "invalid frontmatter" for YAML errors.

3. **Delete the `parseTask` method entirely** from `vault_scanner.go`. It is no longer called.

4. **Keep these functions unchanged:**
   - `injectTaskIdentifier` — still needed for UUID injection into raw content
   - `extractFrontmatter` — still needed to extract frontmatter substring for YAML parsing
   - `injectAndStore` — still needed for the UUID injection + write flow
   - `collectDeleted` — unchanged
   - `scanFiles` — unchanged
   - `runCycle` — unchanged
   - `Run` — unchanged
   - `NewVaultScanner` — unchanged

5. **Keep the `yaml.v3` import** — it is still needed for `yaml.Unmarshal` of `domain.Task` in `processFile`.

6. **Update tests in `task/controller/pkg/scanner/vault_scanner_test.go`:**

   The `Describe("parseTask", ...)` block tests `s.parseTask(ctx, absPath, relPath, taskIdentifier)` directly. Since `parseTask` is removed, these tests must be refactored:

   a. **Remove or rewrite the `Describe("parseTask", ...)` block.** The behaviors it tests are now covered by `processFile`/`runCycle` integration. Specifically:
      - "returns Task for valid frontmatter with assignee" — already covered by the "new file appears in Changed" runCycle test
      - "returns nil for valid frontmatter with empty assignee" — already covered by existing runCycle tests (files without assignee don't appear in Changed)
      - "returns nil for missing frontmatter delimiters" — already covered (files without frontmatter are skipped)
      - "returns nil for malformed YAML" — the "definitely_invalid_status" test. This IS important to keep. Convert it to a runCycle-level test: write a file with `task_identifier` + invalid status + assignee, run a cycle, verify it does NOT appear in Changed.
      - "returns nil when file cannot be read" — remove (processFile handles read errors before parsing)
      - "handles windows-style line endings in frontmatter" — convert to a runCycle-level test: write a file with CRLF + task_identifier + valid status + assignee, run a cycle, verify it appears in Changed with correct fields

   b. Add these new test cases in a `Describe("processFile edge cases", ...)` or within the existing `Describe("runCycle", ...)`:

      ```go
      It("skips file with invalid status", func() {
          content := "---\ntask_identifier: bad-status-uuid\nstatus: definitely_invalid_status\nassignee: claude\n---\n"
          absPath := filepath.Join(tmpDir, taskDir, "bad-status.md")
          Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

          s.runCycle(ctx, results)
          var result ScanResult
          Expect(results).To(Receive(&result))
          Expect(result.Changed).To(BeEmpty())
      })

      It("handles CRLF line endings in full cycle", func() {
          content := "---\r\ntask_identifier: crlf-uuid\r\nstatus: todo\r\nassignee: claude\r\n---\r\n# Task"
          absPath := filepath.Join(tmpDir, taskDir, "crlf-cycle.md")
          Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

          s.runCycle(ctx, results)
          var result ScanResult
          Expect(results).To(Receive(&result))
          Expect(result.Changed).To(HaveLen(1))
          Expect(string(result.Changed[0].Assignee)).To(Equal("claude"))
      })
      ```

7. **Verify no references to `frontmatterID` remain** in the scanner package:
   ```bash
   grep -r "frontmatterID" task/controller/pkg/scanner/
   ```
   Expected: no output

8. **Verify no references to `parseTask` remain** in the scanner package:
   ```bash
   grep -r "parseTask" task/controller/pkg/scanner/
   ```
   Expected: no output
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` types — these are frozen
- Do NOT change the `VaultScanner` interface
- Do NOT change `NewVaultScanner` constructor signature
- Do NOT change `injectTaskIdentifier` or `extractFrontmatter` functions
- Do NOT change `injectAndStore` method
- Do NOT change `go.mod` or `go.sum` — vault-cli v0.51.0 is already in place
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Factory functions must have zero business logic
- Existing tests must still pass (all non-parseTask tests)
- `make precommit` must pass before declaring done
</constraints>

<verification>
Run in `task/controller/`:

```bash
make test
```
Must pass with exit code 0.

```bash
make precommit
```
Must pass with exit code 0.

Verify no leftover references:
```bash
grep -r "frontmatterID" task/controller/pkg/scanner/
grep -r "parseTask" task/controller/pkg/scanner/
```
Both must produce no output.

Verify domain.Task is used in processFile:
```bash
grep "domain.Task" task/controller/pkg/scanner/vault_scanner.go
```
Must show at least one occurrence (the `var domainTask domain.Task` line in processFile).
</verification>
