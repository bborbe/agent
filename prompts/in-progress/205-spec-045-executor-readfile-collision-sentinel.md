---
status: approved
spec: [045-bug-task-controller-filename-collision-idempotency]
created: "2026-06-20T15:11:00Z"
queued: "2026-06-20T15:10:44Z"
branch: dark-factory/bug-task-controller-filename-collision-idempotency
---

<summary>
- The create-task executor stops checking the vault filesystem directly and instead asks git-rest (over HTTP) whether the target filename already exists — the previous local-disk check always reported "not found" against the gitrest adapter, so every replayed command overwrote the file.
- When the target filename is already taken, the controller now writes nothing and returns the shared `ErrTaskAlreadyExists` sentinel, which the CQRS framework turns into a benign Failure on the result topic — operator-added fields and in-progress edits on recurring tasks are no longer stripped.
- A brand-new filename still creates the task exactly as before.
- A transient git-rest read error (timeout, 5xx) now propagates as an error and blocks the write — the controller never writes blindly when it could not confirm the filename is free.
- The old "if the filename is occupied by a different task, fall back to a UUID-named file" behavior is removed for the collision case; filename presence alone now decides ownership. The UUID fallback survives only for the title-fails-validation and title-contains-a-path-separator cases.
- Open question surfaced for the reviewer: git-rest does not expose a typed 404, so "not found" is detected by inspecting the read error. See the inline reviewer note in requirement 4.
</summary>

<objective>
Replace the create-task executor's local-disk existence checks (`os.Stat` / `os.ReadFile`) with a git-rest round-trip via `gitClient.ReadFile`, and on a filename collision return `errors.Wrapf(ctx, task.ErrTaskAlreadyExists, ...)` instead of overwriting or UUID-falling-back. Remove the frontmatter re-parse + `task_identifier` comparison. Keep the title-validation defense and its UUID fallback intact. Depends on prompt 1 (the `ErrTaskAlreadyExists` sentinel must already exist in `lib/command/task`).
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors.Wrapf(ctx, err, "msg", ...)` for wrapping; never `fmt.Errorf`; never `context.Background()` in `pkg/`; sentinel matching via `errors.Is`.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-cqrs.md` — `RunCommandConsumerTx` auto-converts a returned error into a Failure on the result topic; returning `(nil, nil, nil)` is "success, no event"; returning `(nil, nil, err)` is "Failure result + offset advances (no kafka replay)".
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, counterfeiter mocks (`mocks/git_client.go` already generated), external test package, coverage ≥80% for modified code, test every error path.

Key files to read IN FULL before editing:
- `/workspace/task/controller/pkg/command/task_create_task_executor.go` — the file to change. The executor body uses `os.Stat(absPath)` (line 71); `resolveCreateTaskPath` uses `os.ReadFile(titlePath)` (line 139) and a frontmatter re-parse + identifier comparison (lines 154-188).
- `/workspace/task/controller/pkg/gitrestclient/git_rest_client.go` — the `GitClient` interface (lines 301-342). `ReadFile(ctx, relPath string) ([]byte, error)` takes a path **relative to the repo root** (e.g. `"24 Tasks/Foo.md"`), NOT an absolute path. `AtomicWriteAndCommitPush(ctx, absPath, content, message)` takes an **absolute** path (it computes `filepath.Rel(basePath, absPath)` internally).
- `/workspace/task/controller/pkg/gitrestclient/git_client_adapter.go` — `Path()` returns a LOGICAL base path that does NOT exist on disk (lines 63-68); `ReadFile` delegates to `GitRestClient.Get` (lines 115-119).
- `/workspace/task/controller/pkg/gitrestclient/git_rest_client.go` lines 115-144 — `Get`: on a non-200 response (including 404) it returns `errors.Errorf(ctx, "GET %s returned %d: %s", relPath, resp.StatusCode, preview)`. THERE IS NO TYPED "not found" SENTINEL — a 404 is distinguishable only by the `404` substring in the error message. This is load-bearing for requirement 4.
- `/workspace/task/controller/pkg/command/task_create_task_executor_test.go` — the existing Ginkgo suite. The current tests use a real temp dir (`os.WriteFile`, `fakeGit.PathReturns(tmpDir)`, an `AtomicWriteAndCommitPushStub` that writes to disk). The collision/idempotency tests (the `Context("file already exists (idempotency)")`, `Context("title path occupied by a different task")`, and `Context("title path already occupied by the same task (idempotent)")` blocks) MUST be rewritten to drive `fakeGit.ReadFileReturns` / `ReadFileReturnsOnCall` instead of touching disk.
- `/workspace/task/controller/mocks/git_client.go` — counterfeiter fake for `GitClient`. Already exposes `ReadFileReturns(content []byte, err error)`, `ReadFileReturnsOnCall(i int, content []byte, err error)`, `ReadFileCalls(stub)`, `ReadFileCallCount()`, `AtomicWriteAndCommitPushCallCount()`. The `GitClient` interface is NOT changed by this prompt, so do NOT run `make generate` for it.

Inlined load-bearing snippets (verified against source — copy verbatim, do not paraphrase):

Current executor existence-check block (`task_create_task_executor.go` lines 69-77) — to be REPLACED:
```go
taskDirPath := filepath.Join(gitClient.Path(), taskDir)
absPath := resolveCreateTaskPath(ctx, taskDirPath, cmd)
if _, err := os.Stat(absPath); err == nil {
	glog.Infof(
		"create-task: task file already exists at %s for %s, skipping (idempotent)",
		absPath, cmd.TaskIdentifier,
	)
	return nil, nil, nil
}
```

Current `resolveCreateTaskPath` (`task_create_task_executor.go` lines 111-189) — the collision/re-parse branch (the `os.ReadFile` + `result.ExtractFrontmatter` + `parseTaskFrontmatter` + identifier-compare from line 138 through the `uuidPath` return at line 188) is to be REMOVED; the title-validation defense (lines 119-136) STAYS.

Current `GitClient.ReadFile` contract (`git_rest_client.go` lines 336-338):
```go
// ReadFile reads the file at relPath (relative to repo root, e.g. "tasks/foo.md")
// and returns its content.
ReadFile(ctx context.Context, relPath string) ([]byte, error)
```

The `ErrTaskAlreadyExists` sentinel from prompt 1 lives at `github.com/bborbe/agent/lib/command/task` (already imported in this file as `task "github.com/bborbe/agent/lib/command/task"`). If `grep -nE 'var ErrTaskAlreadyExists' /workspace/lib/command/task/*.go` returns no match, prompt 1 has not shipped — STOP and report `status: failed` with message "ErrTaskAlreadyExists sentinel not yet deployed (prompt 1)".

Spec being implemented: `specs/in-progress/045-bug-task-controller-filename-collision-idempotency.md`. Desired Behavior items 1-7 and Acceptance Criteria AC2-AC6 are this prompt's scope.
</context>

<requirements>
1. **Compute the title path as BOTH a relative path (for ReadFile) and an absolute path (for the write).** In the executor closure, after the routing/frontmatter validation succeeds, derive the resolved relative path and absolute path. Replace the current block at lines 69-77 with logic that:
   - Calls `resolveCreateTaskRelPath(ctx, taskDir, cmd)` (the refactored resolver from requirement 2) to get a path RELATIVE to the repo root (e.g. `"24 Tasks/My Title.md"`).
   - Reads the existing file via `gitClient.ReadFile(ctx, relPath)`.
   - Branches on the read result per requirement 4.
   - Builds the absolute path for the write as `absPath := filepath.Join(gitClient.Path(), relPath)` and passes `absPath` (NOT `relPath`) to `AtomicWriteAndCommitPush` — that method requires an absolute path under the base path.

   The literal target of the existing code joined `gitClient.Path()` with `taskDir`; the new resolver returns `taskDir`-rooted relative paths so the executor must join `gitClient.Path()` with the resolver's result to recover the absolute write path.

2. **Refactor `resolveCreateTaskPath` into `resolveCreateTaskRelPath` returning a repo-relative path; remove the collision branch and the frontmatter re-parse.** Rename the function to `resolveCreateTaskRelPath` and change it to take `taskDir string` (instead of the pre-joined `taskDirPath`) and return a path relative to the repo root. Keep ONLY the title-validation defense and the path-separator defense; their UUID fallback returns the UUID-relative path. Remove the `os.ReadFile` call, the `result.ExtractFrontmatter` call, the `parseTaskFrontmatter` call, and the `task_identifier` comparison entirely. The new function:

   ```go
   // resolveCreateTaskRelPath returns the repo-root-relative path where the task
   // file should be written. If cmd.Title passes validation and contains no path
   // separators, the title-derived path is returned; otherwise a WARN is logged and
   // the UUID-derived path is returned as fallback so the task is always materialized.
   // Filename-collision detection is the caller's job (via gitClient.ReadFile) — this
   // function no longer reads the vault or compares task_identifier.
   func resolveCreateTaskRelPath(
   	ctx context.Context,
   	taskDir string,
   	cmd task.CreateCommand,
   ) string {
   	uuidRelPath := filepath.Join(taskDir, string(cmd.TaskIdentifier)+".md")

   	// Re-validate the command (defense-in-depth: sender may have been bypassed).
   	if err := cmd.Validate(ctx); err != nil {
   		glog.Warningf(
   			"create-task: Title validation failed for task %s (%v); falling back to UUID path",
   			cmd.TaskIdentifier, err,
   		)
   		return uuidRelPath
   	}

   	// Reject titles containing path separators to prevent path traversal.
   	if strings.ContainsAny(cmd.Title, "/\\") {
   		glog.Warningf(
   			"create-task: Title %q contains path separator; falling back to UUID path",
   			cmd.Title,
   		)
   		return uuidRelPath
   	}

   	return filepath.Join(taskDir, cmd.Title+".md")
   }
   ```

   Keep `cmd.Validate(ctx)` and `strings.ContainsAny(cmd.Title, "/\\")` verbatim — the spec requires both defense lines intact.

3. **Wire the resolver + read + write in the executor body.** Replace the old lines 69-77 block (and the subsequent write block keeps using `absPath`). The new shape:

   ```go
   relPath := resolveCreateTaskRelPath(ctx, taskDir, cmd)
   if existing, err := gitClient.ReadFile(ctx, relPath); err == nil {
   	// File present at the title path → collision. Write nothing; return the
   	// sentinel so the CQRS framework emits a benign Failure on the result topic.
   	glog.Infof(
   		"create-task: title path %s already occupied (%d bytes), returning ErrTaskAlreadyExists for %s",
   		relPath, len(existing), cmd.TaskIdentifier,
   	)
   	return nil, nil, errors.Wrapf(
   		ctx, task.ErrTaskAlreadyExists, "title path %s occupied", relPath,
   	)
   } else if !isNotFoundReadError(err) {
   	// Transient / unexpected git-rest read error → propagate, do NOT write.
   	return nil, nil, errors.Wrapf(
   		ctx, err, "check existing task file at %s for %s", relPath, cmd.TaskIdentifier,
   	)
   }
   // err is a "not found" read error → title path is free, proceed to write.

   content, err := buildCreateTaskContent(ctx, cmd)
   if err != nil {
   	return nil, nil, errors.Wrapf(ctx, err, "build task file content for %s", cmd.TaskIdentifier)
   }
   absPath := filepath.Join(gitClient.Path(), relPath)
   if err := gitClient.AtomicWriteAndCommitPush(
   	ctx,
   	absPath,
   	content,
   	"[agent-task-controller] create task "+string(cmd.TaskIdentifier),
   ); err != nil {
   	return nil, nil, errors.Wrapf(ctx, err, "atomic write and push for task %s", cmd.TaskIdentifier)
   }
   glog.V(2).Infof("create-task: created task file at %s for %s", relPath, cmd.TaskIdentifier)
   return nil, nil, nil
   ```

   The log level for the collision case is `glog.Infof` (INFO): per the spec's Open Questions, collision is an expected outcome of the manual-trigger and stale-replay paths, so it must NOT be a WARN (which would be alert noise).

4. **Add the `isNotFoundReadError` helper that classifies a git-rest read error as "file not found".** git-rest's `Get` returns a generic wrapped error for a 404 (`"GET <relPath> returned 404: ..."`) — there is no typed sentinel. Detect "not found" by the `404` substring:

   ```go
   // isNotFoundReadError reports whether a gitClient.ReadFile error means the file
   // does not exist (git-rest returns HTTP 404). git-rest's Get does not expose a
   // typed not-found sentinel, so this matches the "404" status embedded in the
   // wrapped error message produced by gitRestClient.Get
   // ("GET <path> returned 404: ..."). A nil error is NOT a not-found error and must
   // be handled by the caller before calling this helper.
   func isNotFoundReadError(err error) bool {
   	if err == nil {
   		return false
   	}
   	return strings.Contains(err.Error(), "returned 404")
   }
   ```

   <!-- REVIEWER NOTE (spec Open Question / gap): git-rest's GitClient.ReadFile/Get does NOT
        expose a typed 404 — see git_rest_client.go:134-140. The spec's Desired Behavior #3/#4
        assume a distinguishable "not found" but the interface (which the spec forbids extending)
        only gives a generic error string. Substring-matching "returned 404" is the minimal change
        that satisfies AC3 (not-found → write) and AC5 (other error → propagate) without touching
        the GitClient interface. If git-rest ever changes its 404 error wording, this classifier
        breaks open in the SAFE direction: an unrecognized error is treated as "not not-found" →
        propagated → no write (AC5 behavior), never a blind overwrite. A follow-up could add a
        typed ErrFileNotFound to the gitrest layer, but that is out of scope for spec 045
        (Constraint: do NOT add new methods to the GitClient interface). -->

   Match the substring `"returned 404"` (not bare `"404"`) to avoid false positives on a path or body that merely contains the digits 404.

5. **Remove now-dead imports and confirm survivors.** After the changes:
   - The `os` import is no longer used in this file (the only uses were `os.Stat` and `os.ReadFile`, both removed) — REMOVE it.
   - The `"github.com/bborbe/agent/task/controller/pkg/result"` import is no longer used in this file (its only use was `result.ExtractFrontmatter` in the removed block; `result` is NOT used elsewhere in `task_create_task_executor.go`) — REMOVE it.
   - `lib "github.com/bborbe/agent/lib"` STAYS (still used by `validateCreateTaskFrontmatter` and `buildCreateTaskContent` via `lib.TaskFrontmatter`).
   - `strings`, `filepath`, `glog`, `errors`, `task`, `gitclient`, `routing`, `cdb`, `base`, `libkv`, `validation` all STAY.
   Run `cd /workspace/task/controller && go build ./pkg/command/...` after editing to catch any leftover unused import; fix per the compiler.

6. **Rewrite the existing collision/idempotency tests to drive `ReadFile`** in `/workspace/task/controller/pkg/command/task_create_task_executor_test.go`. The current tests that pre-create files on disk no longer exercise the real path (the executor reads via `gitClient.ReadFile`, not `os`). DELETE the three existing blocks at the cited lines (the on-disk-pre-write pattern is obsolete) and INSERT the AC2/AC3/AC4/AC5 contexts below in their place:
   - DELETE `Context("file already exists (idempotency)")` — `task_create_task_executor_test.go:148`
   - DELETE `Context("title path occupied by a different task")` — `task_create_task_executor_test.go:281`
   - DELETE `Context("title path already occupied by the same task (idempotent)")` — `task_create_task_executor_test.go:312`
   - INSERT the four new contexts shown in the code block below.

   Keep all OTHER existing contexts (malformed payload, empty TaskIdentifier, missing assignee/status, success new file, git write error, valid/invalid/empty title, vault routing) UNCHANGED — but note requirement 7 about their `ReadFile` default.

   ```go
   Context("title path already occupied (collision)", func() {
   	It("returns ErrTaskAlreadyExists and does not write (AC2)", func() {
   		// Second ReadFile call returns existing content → collision on replay.
   		fakeGit.ReadFileReturnsOnCall(0,
   			nil, errors.New("GET 24 Tasks/Replay Task.md returned 404: not found"))
   		fakeGit.ReadFileReturnsOnCall(1,
   			[]byte("---\ntask_identifier: replay-task\nassignee: claude\nstatus: next\n---\n"), nil)

   		cmdObj := buildCmdObj(task.CreateCommand{
   			TaskIdentifier: lib.TaskIdentifier("replay-task"),
   			Title:          "Replay Task",
   			Frontmatter:    lib.TaskFrontmatter{"assignee": "claude", "status": "next"},
   		})

   		// First create: file not found → writes.
   		_, _, err := executor.HandleCommand(ctx, nil, cmdObj)
   		Expect(err).NotTo(HaveOccurred())
   		Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))

   		// Replay: file now exists → sentinel, no second write.
   		_, _, err = executor.HandleCommand(ctx, nil, cmdObj)
   		Expect(err).To(HaveOccurred())
   		Expect(errors.Is(err, task.ErrTaskAlreadyExists)).To(BeTrue())
   		Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1)) // still 1
   	})
   })

   Context("new filename", func() {
   	It("writes exactly once and returns nil when ReadFile reports not-found (AC3)", func() {
   		fakeGit.ReadFileReturns(nil, errors.New("GET 24 Tasks/Brand New.md returned 404: not found"))
   		cmdObj := buildCmdObj(task.CreateCommand{
   			TaskIdentifier: lib.TaskIdentifier("brand-new"),
   			Title:          "Brand New",
   			Frontmatter:    lib.TaskFrontmatter{"assignee": "claude", "status": "next"},
   		})
   		_, _, err := executor.HandleCommand(ctx, nil, cmdObj)
   		Expect(err).NotTo(HaveOccurred())
   		Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
   	})
   })

   Context("collision with a different task_identifier", func() {
   	It("returns ErrTaskAlreadyExists and does not write (AC4)", func() {
   		// Existing file at the title path belongs to a DIFFERENT task — filename owns the
   		// slot; the executor must not consult frontmatter, must not write.
   		fakeGit.ReadFileReturns(
   			[]byte("---\ntask_identifier: someone-else\nassignee: alice\nstatus: todo\n---\n"), nil)
   		cmdObj := buildCmdObj(task.CreateCommand{
   			TaskIdentifier: lib.TaskIdentifier("new-task-id"),
   			Title:          "My Colliding Task",
   			Frontmatter:    lib.TaskFrontmatter{"assignee": "claude", "status": "next"},
   		})
   		_, _, err := executor.HandleCommand(ctx, nil, cmdObj)
   		Expect(err).To(HaveOccurred())
   		Expect(errors.Is(err, task.ErrTaskAlreadyExists)).To(BeTrue())
   		Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(0))
   	})
   })

   Context("transient git-rest read error", func() {
   	It("propagates the wrapped error and does not write (AC5)", func() {
   		fakeGit.ReadFileReturns(nil, errors.New("GET 24 Tasks/Flaky.md returned 503: service unavailable"))
   		cmdObj := buildCmdObj(task.CreateCommand{
   			TaskIdentifier: lib.TaskIdentifier("flaky-task"),
   			Title:          "Flaky",
   			Frontmatter:    lib.TaskFrontmatter{"assignee": "claude", "status": "next"},
   		})
   		_, _, err := executor.HandleCommand(ctx, nil, cmdObj)
   		Expect(err).To(HaveOccurred())
   		Expect(errors.Is(err, task.ErrTaskAlreadyExists)).To(BeFalse())
   		Expect(err.Error()).To(ContainSubstring("503"))
   		Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(0))
   	})
   })
   ```

   The `errors` import in the test file is the stdlib `"errors"` already imported at line 9 (used by the existing `errors.New(...)` and `errors.Is(...)` calls) — no import change needed for these tests.

7. **Make the existing happy-path tests not accidentally hit a collision.** The other existing tests (`success: new file created`, `valid title`, `invalid title`, `empty title`, and the `vault routing` write cases) currently rely on a clean temp dir so the executor's old `os.Stat` returned not-exist. After this change the executor calls `gitClient.ReadFile`; the fake's default return for an unstubbed `ReadFile` is `(nil, nil)` — which the new executor would treat as a COLLISION (content present, nil error). To keep those tests writing as before, set a package-wide default in the `BeforeEach` so an unstubbed read looks like "not found":

   ```go
   // Default: every title path is free unless a test overrides ReadFile.
   fakeGit.ReadFileReturns(nil, errors.New("GET file returned 404: not found"))
   ```

   Add this line to the existing `BeforeEach` (after `fakeGit.PathReturns(tmpDir)` and before the `executor = command.NewCreateTaskExecutor(...)` assignment). Tests that need a collision override it via `ReadFileReturns` / `ReadFileReturnsOnCall` (as in requirement 6). The `AtomicWriteAndCommitPushStub` that writes to a real temp file may stay for the tests that assert on written content via `os.ReadFile`, but it is no longer required for existence detection.

8. **Verify the resolver-level path shape did not regress.** The `valid title` test asserts `absPath` (from `AtomicWriteAndCommitPushArgsForCall`) `HaveSuffix("My Feature Task.md")` and NOT containing the task_identifier; the `invalid title` / `empty title` tests assert it ends with `<task_identifier>.md`. These still hold because `resolveCreateTaskRelPath` preserves the title-vs-UUID decision and the executor joins `gitClient.Path()` (the temp dir) with the relative path. Do NOT change those assertions.
</requirements>

<constraints>
- `gitClient.ReadFile(ctx, relPath)` already exists on the interface — do NOT add new methods to `GitClient`. (Spec Constraint.) The collision check must use the EXISTING `ReadFile`.
- `ReadFile` takes a path RELATIVE to the repo root; `AtomicWriteAndCommitPush` takes an ABSOLUTE path. Do not swap them. (Verified from `git_rest_client.go` / `git_client_adapter.go`.)
- Keep `cmd.Validate(ctx)` and `strings.ContainsAny(cmd.Title, "/\\")` and their UUID fallback intact — only the collision branch changes. (Spec Constraint / Non-goal.)
- The frontmatter re-parse + `task_identifier` comparison is REMOVED — filename ownership is decided by filename alone, regardless of identifier. (Spec Desired Behavior #6/#7, AC4.)
- Return `errors.Wrapf(ctx, task.ErrTaskAlreadyExists, "title path %s occupied", relPath)` on collision — wrapped so `errors.Is` still matches the sentinel. (Spec Desired Behavior #2.)
- Use `github.com/bborbe/errors.Wrapf` for all wrapping; never `fmt.Errorf`, never bare `return err`, never `context.Background()` in `pkg/`. (Spec Constraint.)
- Log collision at INFO (`glog.Infof`), not WARN — collision is expected on manual-trigger and stale-replay. (Spec Open Question resolution.)
- Do NOT change `task.CreateCommand` wire shape. (Spec Constraint.)
- Do NOT regenerate the `GitClient` counterfeiter mock — the interface is unchanged. (Spec Constraint.)
- Do NOT change `UpdateCommand`, `IncrementFrontmatter`, or `DeleteCommand` executors. (Spec Non-goal.) In particular, `parseTaskFrontmatter` (defined in `task_increment_frontmatter_executor.go`) and `result.ExtractFrontmatter` are STILL used by the increment/update executors — do NOT delete those functions; only stop calling them from `task_create_task_executor.go`.
- Ginkgo v2 / Gomega; counterfeiter for mocks; external test package (`command_test`). (Spec Constraint.)
- Coverage for the modified `NewCreateTaskExecutor`, `resolveCreateTaskRelPath`, and `isNotFoundReadError` must be ≥80% — the AC2-AC5 tests cover collision, not-found, different-identifier collision, and transient-error branches; ensure both branches of `isNotFoundReadError` (404 vs non-404) are exercised (AC3 hits the 404 branch, AC5 hits the non-404 branch).
- Do NOT commit — dark-factory handles git.
- All existing tests in `task/controller/...` must continue to pass.
- Add a `## Unreleased` entry to `/workspace/CHANGELOG.md` (repo root) with a single `fix:` bullet, e.g.:
  `- fix(task/controller): create-task executor checks filename existence via git-rest ReadFile instead of local os.Stat/os.ReadFile, and returns the new lib/command/task.ErrTaskAlreadyExists sentinel on collision so replayed CreateCommands no longer overwrite already-materialized recurring task files (stripping claude_session_id/phase). Insert immediately after the SemVer preamble, before `## v0.68.1`.
</constraints>

<verification>
```bash
# Confirm prompt 1 shipped the sentinel this prompt depends on.
grep -nE 'var ErrTaskAlreadyExists' /workspace/lib/command/task/*.go
# Must return one match; if empty, STOP — report status:failed "sentinel not yet deployed (prompt 1)".
```

```bash
# Confirm os.Stat / os.ReadFile are gone from the create executor.
grep -nE 'os\.(Stat|ReadFile)' /workspace/task/controller/pkg/command/task_create_task_executor.go
# Must return zero matches.
```

```bash
# Confirm the collision path returns the wrapped sentinel.
grep -n 'task.ErrTaskAlreadyExists' /workspace/task/controller/pkg/command/task_create_task_executor.go
# Must return at least one match (the errors.Wrapf collision return).
```

```bash
# AC2-AC5 — run the create-task executor tests.
cd /workspace/task/controller && go test ./pkg/command/... -v
# Must pass, including the new AC2/AC3/AC4/AC5 rows.
```

```bash
# AC6 — full service precommit.
cd /workspace/task/controller && make precommit
# Must exit 0.
```
</verification>

## DARK-FACTORY-REPORT
```yaml
status: success # or: failed, partial
summary: <one-paragraph description of what changed>
verification:
  command: "cd /workspace/task/controller && make precommit"
  exitCode: 0
improvements:
  - <category: PROMPT|GUIDE|GLOBAL>: <one-line suggestion>  # or omit if none
```
