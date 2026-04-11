---
status: completed
summary: Added UUID validation and uniqueness checking to vault scanner — non-UUID and duplicate task_identifiers are now replaced with generated UUIDs via isValidUUID, isIdentifierUnique, and removeTaskIdentifier helpers; existing valid unique UUIDs are preserved.
container: agent-041-controller-validate-task-identifier
dark-factory-version: v0.108.0-dirty
created: "2026-04-11T12:51:57Z"
queued: "2026-04-11T12:51:57Z"
started: "2026-04-11T12:51:59Z"
completed: "2026-04-11T12:59:06Z"
---

<summary>
- Task controller validates that task identifiers are valid UUIDs
- Non-UUID identifiers are replaced with a generated UUID automatically
- Duplicate identifiers across task files are detected and replaced
- Existing valid unique UUIDs are preserved without changes
- Controller guarantees every published task has a unique UUID identifier
</summary>

<objective>
The task controller guarantees that every task has a valid, unique UUID as its
task_identifier. Hand-written or duplicate identifiers are automatically replaced
with generated UUIDs. Existing valid unique UUIDs are preserved.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read coding plugin docs for Go patterns: `go-error-wrapping-guide.md`, `go-testing-guide.md`.

Key files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner.go` — `processFile` at line 174-177 reads `task_identifier` from frontmatter; if empty calls `injectAndStore` to generate UUID; if non-empty accepts it as-is without validation
- `task/controller/pkg/scanner/vault_scanner.go` — `vaultScanner.hashes` map tracks `fileEntry{hash, taskIdentifier}` per relative path; used to detect changes and collect deletions
- `task/controller/pkg/scanner/vault_scanner.go` — `injectAndStore` generates UUID via `uuid.New().String()` and writes it into the file frontmatter
- `task/controller/pkg/scanner/vault_scanner_test.go` — existing tests for scan cycle behavior

Important facts:
- `github.com/google/uuid` is already imported — use `uuid.Parse(taskID)` to validate UUID format
- The `hashes` map already tracks all known task identifiers per file — can be used for uniqueness checking
- When a task_identifier is replaced, the file must be rewritten (same as `injectAndStore` does for empty IDs)
- Rewritten files trigger a git commit+push in `runCycle` via the `written` slice
- The controller should log a warning when replacing an identifier so operators can see what happened
</context>

<requirements>

1. **Add UUID validation helper**

   In `vault_scanner.go`, add a function:
   ```go
   func isValidUUID(s string) bool
   ```
   Uses `uuid.Parse(s)` — returns true if no error.

2. **Add uniqueness check helper**

   Add a method on `vaultScanner`:
   ```go
   func (v *vaultScanner) isIdentifierUnique(id string, relPath string) bool
   ```
   Iterates `v.hashes` — returns false if any other file (different `relPath`) has the same `taskIdentifier`.

3. **Update `processFile` to validate and replace**

   After reading `taskID` from frontmatter (line 174), before accepting it:

   a. If `taskID` is empty → call `injectAndStore` (existing behavior, unchanged).

   b. If `taskID` is non-empty but not a valid UUID → log warning `"replacing non-UUID task_identifier %q in %s"`, `return v.injectAndStore(content, absPath, relPath)`.

   c. If `taskID` is a valid UUID but not unique (another file in `v.hashes` has same ID) → log warning `"replacing duplicate task_identifier %q in %s"`, `return v.injectAndStore(content, absPath, relPath)`. First-seen wins — only subsequent duplicates get replaced.

   d. If `taskID` is a valid unique UUID → keep it (existing behavior, unchanged).

4. **Update tests**

   Add test cases in `vault_scanner_test.go`:
   - File with non-UUID task_identifier (e.g., `"my-custom-id"`) → gets replaced with UUID
   - File with duplicate UUID (same as another file) → gets replaced with fresh UUID
   - File with valid unique UUID → preserved unchanged
   - File with empty task_identifier → gets UUID injected (existing test, verify still passes)

</requirements>

<constraints>
- Do NOT change the `injectAndStore` function — reuse it for all replacement cases
- Do NOT change the `ScanResult` struct or `VaultScanner` interface
- Do NOT change the git commit message format in `runCycle`
- Log replacements at Warning level so operators notice
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Run precommit:

```bash
cd task/controller && make precommit
```
Must pass with exit code 0.

Verify UUID validation:

```bash
grep -n "isValidUUID\|uuid.Parse" task/controller/pkg/scanner/vault_scanner.go
```
Must show validation function.

Verify uniqueness check:

```bash
grep -n "isIdentifierUnique\|duplicate" task/controller/pkg/scanner/vault_scanner.go
```
Must show uniqueness check.

Verify replacement logic:

```bash
grep -n "replacing.*task_identifier" task/controller/pkg/scanner/vault_scanner.go
```
Must show warning log messages for both non-UUID and duplicate cases.
</verification>
