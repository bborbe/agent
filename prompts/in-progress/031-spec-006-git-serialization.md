---
status: approved
spec: [006-result-writer-conflict-resolution]
created: "2026-04-04T00:00:00Z"
queued: "2026-04-04T12:18:47Z"
---

<summary>
- All git operations (Pull, CommitAndPush) in the controller are protected by a shared mutex — scanner and result writer can never run git commands concurrently
- A new atomic method combines file write + git add + commit + push under a single lock, eliminating the window where a scanner git-add can sweep up a dirty result file
- ResultWriter delegates file writing to the new atomic method instead of calling os.WriteFile separately
- GitClient interface gains one new method; existing callers of Pull and CommitAndPush are unaffected
- Counterfeiter mock is regenerated to include the new method
- Fast path (no contention): zero added latency — the mutex is uncontested in the common case
</summary>

<objective>
Eliminate concurrent git working-directory access between the vault scanner and the result writer by adding a `sync.Mutex` to `gitClient` and exposing an `AtomicWriteAndCommitPush` method that holds the lock across the entire write → add → commit → push sequence. This is the correctness fix; push-retry and LLM conflict resolution come in subsequent prompts.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read the coding plugin guides from `~/Documents/workspaces/coding/docs/`: `go-architecture-patterns.md`, `go-testing-guide.md`, `go-composition.md`.

The problem: in `task/controller`, two goroutines share the same git working directory:
1. **SyncLoop goroutine** — drives `vaultScanner.runCycle`, which calls `gitClient.Pull` and then (when new task_identifiers are injected) `gitClient.CommitAndPush`.
2. **CommandConsumer goroutine** — drives `resultWriter.WriteResult`, which calls `os.WriteFile` to write a task file and then `gitClient.CommitAndPush`.

If both goroutines run simultaneously, a `git add -A` from one can pick up a dirty file written by the other — producing a corrupted commit. The fix: all git operations must be serialized via a shared mutex, and the file-write step must be fused into the same lock as the commit+push.

Key files to read before making changes:
- `task/controller/pkg/gitclient/git_client.go` — current GitClient interface + gitClient struct (no mutex yet)
- `task/controller/pkg/gitclient/git_client_test.go` — existing tests
- `task/controller/pkg/gitclient/gitclient_suite_test.go` — test suite bootstrap
- `task/controller/pkg/result/result_writer.go` — calls `os.WriteFile` then `gitClient.CommitAndPush` (the gap to close)
- `task/controller/pkg/result/result_writer_test.go` — existing tests (must still pass)
- `task/controller/mocks/git_client.go` — counterfeiter mock (must be regenerated)
</context>

<requirements>
### 1. Add mutex and split `gitClient` methods into locked + unlocked variants

In `task/controller/pkg/gitclient/git_client.go`:

**Add `mu sync.Mutex` field to `gitClient` struct:**
```go
type gitClient struct {
    gitURL    string
    localPath string
    branch    string
    mu        sync.Mutex
}
```

**Refactor `Pull`** — acquire mutex, delegate to private `pull`:
```go
func (g *gitClient) Pull(ctx context.Context) error {
    g.mu.Lock()
    defer g.mu.Unlock()
    return g.pull(ctx)
}

func (g *gitClient) pull(ctx context.Context) error {
    // #nosec G204 -- binary is hardcoded "git", localPath is from trusted internal config
    cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "pull", "--rebase")
    if out, err := cmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git pull failed: %s", string(out))
    }
    return nil
}
```

**Refactor `CommitAndPush`** — acquire mutex, delegate to private `commitAndPush`:
```go
func (g *gitClient) CommitAndPush(ctx context.Context, message string) error {
    g.mu.Lock()
    defer g.mu.Unlock()
    return g.commitAndPush(ctx, message)
}

func (g *gitClient) commitAndPush(ctx context.Context, message string) error {
    // #nosec G204 -- binary is hardcoded "git", localPath and message are from trusted internal config
    addCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "add", "-A")
    if out, err := addCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git add failed: %s", string(out))
    }
    // #nosec G204 -- binary is hardcoded "git", localPath and message are from trusted internal config
    commitCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "commit", "-m", message)
    if out, err := commitCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git commit failed: %s", string(out))
    }
    // #nosec G204 -- binary is hardcoded "git", localPath is from trusted internal config
    pushCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
    if out, err := pushCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git push failed: %s", string(out))
    }
    return nil
}
```

### 2. Add `AtomicWriteAndCommitPush` to the `GitClient` interface

In `task/controller/pkg/gitclient/git_client.go`, add to the `GitClient` interface:
```go
// AtomicWriteAndCommitPush writes content to absPath and commits+pushes under a single lock.
// No other git operation can interleave between the file write and the commit.
AtomicWriteAndCommitPush(ctx context.Context, absPath string, content []byte, message string) error
```

Implement on `gitClient`:
```go
func (g *gitClient) AtomicWriteAndCommitPush(ctx context.Context, absPath string, content []byte, message string) error {
    g.mu.Lock()
    defer g.mu.Unlock()
    // #nosec G306 -- 0600 is intentional for task files (gosec requirement)
    if err := os.WriteFile(absPath, content, 0600); err != nil {
        return errors.Wrapf(ctx, err, "write file %s", absPath)
    }
    return g.commitAndPush(ctx, message)
}
```

### 3. Update `ResultWriter` to use `AtomicWriteAndCommitPush`

In `task/controller/pkg/result/result_writer.go`:

Replace the two-step `os.WriteFile` + `gitClient.CommitAndPush` call with a single `gitClient.AtomicWriteAndCommitPush` call.

**Before (in `WriteResult`):**
```go
if writeErr := os.WriteFile(matchedAbsPath, newContent, 0600); writeErr != nil {
    return errors.Wrapf(ctx, writeErr, "write file failed")
}

glog.V(2).Infof("WriteResult: committing and pushing for task %s", req.TaskIdentifier)
if commitErr := r.gitClient.CommitAndPush(ctx, fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier)); commitErr != nil {
    return errors.Wrapf(ctx, commitErr, "commit and push failed")
}
```

**After:**
```go
glog.V(2).Infof("WriteResult: writing and pushing for task %s", req.TaskIdentifier)
if err := r.gitClient.AtomicWriteAndCommitPush(
    ctx,
    matchedAbsPath,
    newContent,
    fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier),
); err != nil {
    return errors.Wrapf(ctx, err, "atomic write and push failed")
}
```

Remove the now-unused `"os"` import if it is no longer needed anywhere else in the file. (Check: `os.DirFS` and `os.WriteFile` are both used in result_writer.go — remove the `os.WriteFile` call but keep `os.DirFS`.)

### 4. Regenerate the counterfeiter mock

The `GitClient` interface gained a new method. Regenerate the mock:
```bash
cd task/controller && go generate ./pkg/gitclient/...
```
Or run:
```bash
cd task/controller && make generate
```

The mock at `task/controller/mocks/git_client.go` must include `AtomicWriteAndCommitPush`.

### 5. Update `result_writer_test.go` to use `AtomicWriteAndCommitPush`

In `task/controller/pkg/result/result_writer_test.go`:

The existing tests use `FakeGitClient`. Update them:
- The `FakeGitClient.CommitAndPushCallCount()` assertions that verified `CommitAndPush` was called should now verify `AtomicWriteAndCommitPush` was called (`AtomicWriteAndCommitPushCallCount()`).
- The fake's `AtomicWriteAndCommitPush` stub must actually write the file to disk so the test's file-content assertions still work. Configure the stub:
  ```go
  fakeGitClient.AtomicWriteAndCommitPushStub = func(ctx context.Context, absPath string, content []byte, message string) error {
      return os.WriteFile(absPath, content, 0600)
  }
  ```
- Verify the commit message passed to `AtomicWriteAndCommitPush` contains the task identifier (use `AtomicWriteAndCommitPushArgsForCall(0)` to get the message arg).
- Existing "CommitAndPush is never called for unknown tasks" tests: verify `AtomicWriteAndCommitPushCallCount() == 0` instead.

### 6. Run tests and precommit

```bash
cd task/controller && make test
cd task/controller && make precommit
```
</requirements>

<constraints>
- CQRS command format and task schema must not change (see `docs/kafka-schema-design.md`)
- Controller actor model must not change (see `docs/controller-design.md`)
- Do NOT add push-retry or LLM conflict resolution in this prompt — that is handled in subsequent prompts
- Do NOT change the `EnsureCloned` method — no mutex needed there (called once at startup before any goroutines start)
- `sync.Mutex` is NOT reentrant in Go — `commitAndPush` (private) must NOT acquire the mutex; it is called by `CommitAndPush` (public) and `AtomicWriteAndCommitPush` which already hold the lock
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`, never `context.Background()` in pkg/ code
- File permissions must remain `0600` for task files (gosec requirement)
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `make precommit` passes in `task/controller`
</constraints>

<verification>
Verify mutex field exists:
```bash
grep -n "sync.Mutex\|mu sync" task/controller/pkg/gitclient/git_client.go
```
Must show the mutex field.

Verify `AtomicWriteAndCommitPush` is in the interface and implementation:
```bash
grep -n "AtomicWriteAndCommitPush" task/controller/pkg/gitclient/git_client.go task/controller/mocks/git_client.go task/controller/pkg/result/result_writer.go
```
Must appear in all three files.

Verify `os.WriteFile` removed from result_writer (replaced by atomic method):
```bash
grep -n "os.WriteFile" task/controller/pkg/result/result_writer.go
```
Must produce no output.

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
