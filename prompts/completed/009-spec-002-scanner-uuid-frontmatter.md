---
status: completed
spec: ["002"]
summary: Extended VaultScanner to inject stable UUIDv4 task_identifier into task frontmatter, commit write-backs atomically, and use UUID as TaskIdentifier on all published events
container: agent-009-spec-002-scanner-uuid-frontmatter
dark-factory-version: v0.68.1
created: "2026-03-27T00:00:00Z"
queued: "2026-03-27T16:09:23Z"
started: "2026-03-27T16:20:03Z"
completed: "2026-03-27T16:30:27Z"
---

<summary>
- Task files without a `task_identifier` field in their frontmatter receive a UUIDv4 written back to disk within the same scan cycle
- Frontmatter write-back inserts `task_identifier: <uuid>` at the top of the frontmatter block without reordering or removing existing fields
- Files whose UUID was just generated are excluded from the Kafka publish for that cycle; they are picked up on the next cycle once the commit is confirmed
- All UUID write-backs are committed and pushed in a single git operation at the end of the scan cycle
- If the git push fails, the entire scan result is suppressed for that cycle (no Kafka events emitted)
- If any file write fails, the batch commit is skipped for that cycle; only that file is affected
- Kafka events for existing tasks carry the UUID from their frontmatter, not the file path
- Delete events use the UUID stored from when the file was last seen, ensuring consumers can correlate deletions
</summary>

<objective>
Extend `VaultScanner` to inject a stable `task_identifier` UUID into task files that lack one, commit the write-backs atomically, and use the UUID (not the file path) as `lib.TaskIdentifier` on all published events. This is the second and final prompt for spec-002.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `task/controller/pkg/scanner/vault_scanner.go` — the file to modify; read it fully before touching it
- `task/controller/pkg/scanner/vault_scanner_test.go` — read all existing tests; several must be updated
- `task/controller/pkg/gitclient/git_client.go` — `GitClient` interface; must have `CommitAndPush` method (added by prompt spec-002-gitclient-commit-push, which must run first)
- `lib/agent_task.go` — `lib.Task` struct (frozen — do not modify)
- `lib/agent_task-identifier.go` — `lib.TaskIdentifier` type (frozen — do not modify)
</context>

<requirements>
### 1. Define a local frontmatter-ID struct

Add a private struct at the top of `vault_scanner.go` for extracting the `task_identifier` field from YAML frontmatter:

```go
type frontmatterID struct {
    TaskIdentifier string `yaml:"task_identifier"`
}
```

### 2. Define a `fileEntry` struct to replace the hash-only map value

Replace:
```go
hashes map[string][32]byte
```
with:
```go
hashes map[string]fileEntry
```

where:
```go
type fileEntry struct {
    hash           [32]byte
    taskIdentifier lib.TaskIdentifier
}
```

Update the `vaultScanner` struct and `NewVaultScanner` constructor accordingly:
```go
hashes: make(map[string]fileEntry),
```

### 3. Add `injectTaskIdentifier(content []byte, id string) ([]byte, error)`

This pure function inserts `task_identifier: <id>` immediately after the opening frontmatter delimiter, preserving all existing content and line endings:

```go
func injectTaskIdentifier(content []byte, id string) ([]byte, error) {
    s := string(content)
    if strings.HasPrefix(s, "---\r\n") {
        return []byte("---\r\ntask_identifier: " + id + "\r\n" + s[5:]), nil
    }
    if strings.HasPrefix(s, "---\n") {
        return []byte("---\ntask_identifier: " + id + "\n" + s[4:]), nil
    }
    return nil, errors.Errorf(context.Background(), "content does not start with frontmatter delimiter")
}
```

Note: `errors.Errorf(context.Background(), ...)` is acceptable here because `injectTaskIdentifier` is a pure function with no context parameter. Do NOT use `fmt.Errorf`.

### 4. Modify `parseTask` to accept a pre-determined `taskIdentifier`

Change the signature from:
```go
func (v *vaultScanner) parseTask(ctx context.Context, absPath, relPath string) *lib.Task
```
to:
```go
func (v *vaultScanner) parseTask(ctx context.Context, absPath, relPath string, taskIdentifier lib.TaskIdentifier) *lib.Task
```

And replace the line `TaskIdentifier: lib.TaskIdentifier(relPath)` with `TaskIdentifier: taskIdentifier`.

The caller (in `scanFiles`) has already determined the UUID before calling `parseTask`, so `parseTask` itself no longer needs to derive an identifier.

### 5. Rewrite `scanFiles` and `collectDeleted`

Replace the current `scanFiles` signature and body with a version that:
- Returns `([]lib.Task, []lib.TaskIdentifier, []string, bool)` where the third value is the list of `relPath` strings that were written back, and the fourth is a write-error flag.
- For each changed `.md` file:
  a. Extract frontmatter with `extractFrontmatter`.
  b. If extraction fails → log warning, skip file (do not attempt UUID injection).
  c. Parse the extracted frontmatter into `frontmatterID` using `yaml.Unmarshal`.
  d. If `fmID.TaskIdentifier == ""` (no task_identifier):
     - Generate a UUID: `id := uuid.New().String()`
     - Inject: `newContent, err := injectTaskIdentifier(content, id)`; if err → log warning, skip file.
     - Write back: `err = os.WriteFile(absPath, newContent, 0600)`; if err → log warning, set `writeError = true`, do NOT update `v.hashes` for this file, continue to next file.
     - On successful write: store `v.hashes[relPath] = fileEntry{hash: [32]byte{}, taskIdentifier: lib.TaskIdentifier(id)}` (zero-value hash as sentinel so the file is detected as changed on the next cycle and published).
     - Add `relPath` to the `written` slice.
     - Do NOT add this task to `changed` (it will be published next cycle).
  e. If `fmID.TaskIdentifier != ""`:
     - Store `v.hashes[relPath] = fileEntry{hash: hash, taskIdentifier: lib.TaskIdentifier(fmID.TaskIdentifier)}`.
     - Call `v.parseTask(ctx, absPath, relPath, lib.TaskIdentifier(fmID.TaskIdentifier))`.
     - If the result is non-nil, append to `changed`.

Update `collectDeleted` to use `v.hashes[relPath].taskIdentifier` as the deleted identifier:

```go
func (v *vaultScanner) collectDeleted(seen map[string]struct{}) []lib.TaskIdentifier {
    var deleted []lib.TaskIdentifier
    for relPath, entry := range v.hashes {
        if _, ok := seen[relPath]; !ok {
            deleted = append(deleted, entry.taskIdentifier)
            delete(v.hashes, relPath)
        }
    }
    return deleted
}
```

Also update the hash-unchanged check:
```go
// Before (old):
if existing, ok := v.hashes[relPath]; ok && existing == hash {
    return nil
}

// After (new) — compare only the hash field:
if existing, ok := v.hashes[relPath]; ok && existing.hash == hash {
    return nil
}
```

### 6. Rewrite `runCycle` to commit write-backs and suppress on push failure

```go
func (v *vaultScanner) runCycle(ctx context.Context, results chan<- ScanResult) {
    if err := v.gitClient.Pull(ctx); err != nil {
        glog.Warningf("git pull failed: %v", err)
        return
    }
    glog.V(3).Infof("git pull succeeded, scanning %s", v.taskDir)

    changed, deleted, written, writeError := v.scanFiles(ctx)

    if len(written) > 0 && !writeError {
        if err := v.gitClient.CommitAndPush(ctx, "[agent-task-controller] add task_identifier to tasks"); err != nil {
            glog.Warningf("git commit+push failed, skipping publish: %v", err)
            return
        }
    }

    result := ScanResult{Changed: changed, Deleted: deleted}
    select {
    case results <- result:
    default:
    }
}
```

If `writeError` is true (any write-back failed), skip CommitAndPush but still send the scan result for tasks that already had a `task_identifier`. The written-but-failing files will be retried next cycle.

### 7. Update tests in `task/controller/pkg/scanner/vault_scanner_test.go`

**7a. Update `testGitClient` to expose `CommitAndPush` control** (this was done in prompt 1, but verify it matches):
```go
type testGitClient struct {
    path          string
    pullErr       error
    commitPushErr error
}
func (t *testGitClient) CommitAndPush(_ context.Context, _ string) error { return t.commitPushErr }
```

**7b. Update the `vaultScanner` struct literal** in `BeforeEach` (field `hashes` type changes):
```go
s = &vaultScanner{
    gitClient:    fakeGit,
    taskDir:      taskDir,
    pollInterval: time.Second,
    hashes:       make(map[string]fileEntry),
    trigger:      triggerCh,
}
```

**7c. Update `parseTask` tests** — `parseTask` now takes a fourth argument `taskIdentifier lib.TaskIdentifier`. Update every direct call to `s.parseTask(ctx, absPath, relPath)` to pass a test UUID as the fourth argument (e.g., `lib.TaskIdentifier("test-uuid-1234")`). Update the assertion:
```go
// OLD:
Expect(string(task.TaskIdentifier)).To(Equal(relPath))
// NEW:
Expect(string(task.TaskIdentifier)).To(Equal("test-uuid-1234"))
```

**7d. Update the "new file appears in Changed" `runCycle` test** — the test file must have `task_identifier` in its frontmatter for the task to be published (files without `task_identifier` are written back and not published this cycle):
```go
content := "---\ntask_identifier: 11111111-1111-4111-8111-111111111111\nstatus: todo\nassignee: claude\n---\n# New task"
```

**7e. Update the "unchanged file is not in Changed on second cycle" test** similarly (add `task_identifier` to content).

**7f. Update the "modified file appears in Changed on next cycle" test** — both the original and updated content must have `task_identifier`:
```go
content := "---\ntask_identifier: 22222222-2222-4222-8222-222222222222\nstatus: todo\nassignee: claude\n---\n# Original"
updated := "---\ntask_identifier: 22222222-2222-4222-8222-222222222222\nstatus: in_progress\nassignee: claude\n---\n# Updated"
```

**7g. Update the "deleted file appears in Deleted" test** — the file must have `task_identifier` so the UUID is stored in `hashes`. Change the assertion from `ContainSubstring("deleted-task.md")` to verify the deleted identifier matches the UUID from the file's frontmatter:
```go
content := "---\ntask_identifier: 33333333-3333-4333-8333-333333333333\nstatus: todo\nassignee: claude\n---\n# Task"
// ...after deletion:
Expect(string(result.Deleted[0])).To(Equal("33333333-3333-4333-8333-333333333333"))
```

**7h. Add new test cases** (all in the `runCycle` `Describe` block):

- **UUID injected when task_identifier absent**: write a file without `task_identifier`, call `runCycle`. Expect no task in `result.Changed` (not published this cycle). Expect the file on disk now contains `task_identifier:` in its content. Expect `CommitAndPush` was called (check via `fakeGit.commitPushErr`; or read the file and verify injection).

- **Task published on second cycle after injection**: after the first `runCycle` writes back the UUID, call `runCycle` a second time. Expect `result.Changed` has one task whose `TaskIdentifier` is a valid UUID string (matches `MatchRegexp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")`).

- **CommitAndPush failure suppresses ScanResult**: set `fakeGit.commitPushErr = errors.New("push failed")`. Write a file without `task_identifier`. Call `runCycle`. Expect no result on `results` channel (`Consistently(results, 50*time.Millisecond).ShouldNot(Receive())`).

- **Trigger test** (`Run` test "runs a cycle immediately when trigger fires"): update the file content to include `task_identifier: 44444444-4444-4444-8444-444444444444` so the task is published and appears in `result.Changed`.

**7i. Add `injectTaskIdentifier` unit tests** (in the `parseTask` `Describe` block or a new `Describe`):
- LF line endings: input `"---\nstatus: todo\n---\n"` → output starts with `"---\ntask_identifier: test-id\nstatus: todo\n---\n"`
- CRLF line endings: input `"---\r\nstatus: todo\r\n---\r\n"` → output starts with `"---\r\ntask_identifier: test-id\r\nstatus: todo\r\n---\r\n"`
- No frontmatter delimiter: input `"no frontmatter"` → error returned

### 8. Update `CHANGELOG.md`

Add under `## Unreleased` (or create it immediately after `# Changelog`):

```
## Unreleased

- feat: Inject stable UUIDv4 task_identifier into vault task frontmatter and use UUID as TaskIdentifier on Kafka events
```

### 9. Run make generate and make test

Run `make generate` in `task/controller/` to regenerate any affected mocks.
Run `make test` in `task/controller/` to verify all tests pass.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- The `VaultScanner` interface signature (`Run(ctx context.Context, results chan<- ScanResult) error`) must not change
- The `lib.Task` struct and `lib.TaskIdentifier` type remain unchanged — only values change from paths to UUIDs
- The `TaskPublisher` interface is unchanged
- Frontmatter write-back must preserve all existing fields and formatting — use string injection, not YAML re-marshal
- The commit message must be `"[agent-task-controller] add task_identifier to tasks"` so it is machine-recognizable
- `github.com/google/uuid` is already in go.mod — do not add it again
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`
- File write uses `os.WriteFile(absPath, content, 0600)` — permission `0600` is required by gosec
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- Existing tests must still pass after changes — update them as described in requirement 7
- `make test` must pass before declaring done
</constraints>

<verification>
Run `make test` in `task/controller/` — must pass.
Run `make precommit` in `task/controller/` — must pass with exit code 0.
</verification>
