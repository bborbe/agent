---
status: executing
spec: [006-result-writer-conflict-resolution]
container: agent-032-spec-006-push-retry-rebase
dark-factory-version: v0.94.1-dirty
created: "2026-04-04T00:00:00Z"
queued: "2026-04-04T12:18:51Z"
started: "2026-04-04T12:42:17Z"
---

<summary>
- When a push fails due to remote changes, the controller automatically fetches and rebases instead of losing the commit
- After a clean rebase (no conflict markers), the push is retried exactly once
- If the rebase produces conflict markers in working files, the rebase is aborted and an error is returned — the working directory is left clean
- Conflict detection scans conflicted files reported by git (not a full directory walk) using `git diff --name-only --diff-filter=U`
- The fast path (push succeeds on first attempt) has zero extra overhead — no fetch, no rebase, no scan
- Push retry is transparent to callers — CommitAndPush and AtomicWriteAndCommitPush behave the same externally
</summary>

<objective>
Extend the `gitClient` push logic so that a failed push triggers a fetch + rebase cycle. If the rebase is clean, the push is retried. If the rebase produces conflict markers, the rebase is aborted and an error is returned. This builds on the mutex added in the previous prompt (1-spec-006-git-serialization) — the rebase and retry all happen within the same lock.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read the coding plugin guides from `~/Documents/workspaces/coding/docs/`: `go-architecture-patterns.md`, `go-testing-guide.md`, `go-error-wrapping-guide.md`.

**Prerequisite:** prompt `spec-006-git-serialization` has already been applied. Before making changes, verify: `grep -n "sync.Mutex\|func.*commitAndPush" task/controller/pkg/gitclient/git_client.go` — must show the mutex field and private `commitAndPush` method. If not found, stop and report that the prerequisite prompt has not been applied.

Key files to read before making changes:
- `task/controller/pkg/gitclient/git_client.go` — current implementation after serialization prompt; modify `commitAndPush` here
- `task/controller/pkg/gitclient/git_client_test.go` — existing tests; extend with push-retry scenarios
- `task/controller/pkg/gitclient/gitclient_suite_test.go` — test suite bootstrap

**Git rebase flow (implement exactly this):**

When `git push` fails:
1. `git fetch origin` — get remote changes
2. `git rebase origin/<branch>` — replay local commit on top of remote
3. Check for conflicted files: `git -C <localPath> diff --name-only --diff-filter=U`
   - If output is non-empty → conflict markers present → `git rebase --abort` → return error
   - If output is empty → rebase was clean → retry `git push`
4. If the retry push also fails → return the push error (no second rebase attempt)

**Why `git diff --name-only --diff-filter=U`:**
`git rebase` exits with code 1 both for "stopped due to conflicts" and for other errors. Using `diff --diff-filter=U` (Unmerged) is the reliable way to detect whether conflict markers were left in working files after a rebase stops.
</context>

<requirements>
### 1. Extend `commitAndPush` with push-retry logic

In `task/controller/pkg/gitclient/git_client.go`, replace the current `commitAndPush` private method with:

```go
func (g *gitClient) commitAndPush(ctx context.Context, message string) error {
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    addCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "add", "-A")
    if out, err := addCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git add failed: %s", string(out))
    }
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    commitCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "commit", "-m", message)
    if out, err := commitCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git commit failed: %s", string(out))
    }
    if err := g.pushWithRetry(ctx); err != nil {
        return errors.Wrapf(ctx, err, "push failed")
    }
    return nil
}
```

### 2. Add `pushWithRetry` private method

```go
// pushWithRetry attempts git push. On failure, fetches and rebases.
// If rebase is clean, retries push once. If conflicts are detected, aborts and returns an error.
func (g *gitClient) pushWithRetry(ctx context.Context) error {
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    pushCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
    pushOut, pushErr := pushCmd.CombinedOutput()
    if pushErr == nil {
        return nil // fast path: push succeeded
    }
    glog.V(2).Infof("push failed (%v: %s), attempting fetch+rebase", pushErr, string(pushOut))

    // Fetch remote changes
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    fetchCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "fetch", "origin")
    if out, err := fetchCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "git fetch failed: %s", string(out))
    }

    // Rebase onto remote
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    rebaseCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rebase", "origin/"+g.branch)
    rebaseOut, rebaseErr := rebaseCmd.CombinedOutput()

    // Check for conflict markers regardless of rebase exit code
    conflicted, conflictErr := g.conflictedFiles(ctx)
    if conflictErr != nil {
        // Can't determine state — abort to be safe
        g.abortRebase(ctx)
        return errors.Wrapf(ctx, conflictErr, "check for conflicts after rebase")
    }
    if len(conflicted) > 0 {
        g.abortRebase(ctx)
        return errors.Errorf(ctx, "rebase produced merge conflicts in %d file(s): %v", len(conflicted), conflicted)
    }
    if rebaseErr != nil {
        // Rebase failed but no conflict markers — some other error
        return errors.Wrapf(ctx, rebaseErr, "git rebase failed: %s", string(rebaseOut))
    }

    glog.V(2).Infof("rebase clean, retrying push")
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    retryCmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "push")
    if out, err := retryCmd.CombinedOutput(); err != nil {
        return errors.Wrapf(ctx, err, "push retry failed: %s", string(out))
    }
    return nil
}
```

### 3. Add `conflictedFiles` and `abortRebase` helpers

```go
// conflictedFiles returns the list of files with unresolved conflict markers.
// Uses `git diff --name-only --diff-filter=U` which lists unmerged (conflicted) paths.
func (g *gitClient) conflictedFiles(ctx context.Context) ([]string, error) {
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "diff", "--name-only", "--diff-filter=U")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, errors.Wrapf(ctx, err, "git diff --name-only --diff-filter=U failed: %s", string(out))
    }
    output := strings.TrimSpace(string(out))
    if output == "" {
        return nil, nil
    }
    return strings.Split(output, "\n"), nil
}

// abortRebase runs `git rebase --abort` to restore the working directory to a clean state.
// Errors are logged but not returned — this is a best-effort cleanup.
func (g *gitClient) abortRebase(ctx context.Context) {
    // #nosec G204 -- binary is hardcoded "git", args from trusted internal config
    cmd := exec.CommandContext(ctx, "git", "-C", g.localPath, "rebase", "--abort")
    if out, err := cmd.CombinedOutput(); err != nil {
        glog.Warningf("git rebase --abort failed: %v: %s", err, string(out))
    }
}
```

### 4. Add `"strings"` import if not already present

`strings.TrimSpace` and `strings.Split` are needed for `conflictedFiles`. Add to the import block if missing.

### 5. Update `git_client_test.go` with push-retry test cases

The existing tests use real git subprocess calls against a temp repo (or mocks — read the test file first to understand the current approach). Add tests for:

**If tests use a real temp git repo** (subprocess tests):
- Set up a local "remote" bare repo and a local clone using `git init --bare` + `git clone`
- **Test: push succeeds on first attempt** — make a commit locally, push succeeds, verify `commitAndPush` (via `CommitAndPush`) returns nil
- **Test: push fails, clean rebase** — simulate remote advancing by pushing a commit to the bare repo directly, then call `CommitAndPush` on the local clone; verify: no error returned, remote has both commits
- **Test: push fails, rebase conflict** — advance remote with content that conflicts with local; verify `CommitAndPush` returns an error containing "merge conflicts"; verify working directory is clean (no conflict markers, rebase aborted)

**If tests use fakes/mocks** (read existing tests to determine):
- Add test cases using `exec.Command` stubs or actual temp repos, following the existing test pattern

**In all cases**, the fast path (push succeeds) must have zero calls to `git fetch` or `git rebase`. Verify that when push succeeds, `CommitAndPush` returns nil and the remote has the commit — the absence of error is sufficient.

**Follow existing Ginkgo structure:** `Describe("CommitAndPush") / Context("when push fails") / It("...")` — do not use stdlib `testing.T` tests.

### 6. Verify no breaking changes to callers

`CommitAndPush` and `AtomicWriteAndCommitPush` public signatures are unchanged. The push-retry is entirely internal to `pushWithRetry`. No changes needed to:
- `task/controller/pkg/result/result_writer.go`
- `task/controller/pkg/scanner/vault_scanner.go`
- `task/controller/pkg/factory/factory.go`
- `task/controller/main.go`
</requirements>

<constraints>
- CQRS command format and task schema must not change (see `docs/kafka-schema-design.md`)
- Controller actor model must not change (see `docs/controller-design.md`)
- Fast path (push succeeds on first attempt): zero extra overhead — no fetch, no rebase, no file scan
- The rebase and retry must all happen inside the mutex (they are called from `commitAndPush` which is called from the mutex-holding public methods — do NOT acquire the mutex again in `pushWithRetry`)
- Do NOT add LLM conflict resolution in this prompt — conflicted files return an error; LLM resolution is prompt 3
- `abortRebase` must be called before returning any conflict error — leave the working directory clean
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`, never `context.Background()` in pkg/ code
- Use `github.com/golang/glog` for logging — `glog.V(2).Infof` for push retry events, `glog.Warningf` for abort failures
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `make precommit` passes in `task/controller`
</constraints>

<verification>
Verify `pushWithRetry` method exists:
```bash
grep -n "func.*pushWithRetry\|func.*conflictedFiles\|func.*abortRebase" task/controller/pkg/gitclient/git_client.go
```
Must show all three methods.

Verify fast path has no fetch/rebase on success:
```bash
grep -n "fetch\|rebase" task/controller/pkg/gitclient/git_client.go
```
Must show fetch and rebase only inside `pushWithRetry` (not in the main push call path before the fast-path return).

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
