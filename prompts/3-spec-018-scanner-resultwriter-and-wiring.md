---
status: draft
spec: [018-use-git-rest-for-vault-writes]
created: "2026-05-02T19:50:00Z"
branch: dark-factory/use-git-rest-for-vault-writes
---

<summary>
- `vault_scanner.go` gains a `NewGitRestVaultScanner` constructor whose implementation uses `gitClient.ListFiles`, `gitClient.ReadFile`, `gitClient.WriteFile` instead of `os.DirFS` / `os.WriteFile` — enabling the scanner to enumerate vault files via git-rest HTTP when `USE_GIT_REST=true`
- `result.FindTaskFilePath` is updated to accept a `gitclient.GitClient` parameter (replacing the `taskDirPath string` local-FS walk) so command executors use the gitClient to enumerate and read vault files
- All callers of `FindTaskFilePath` (`result_writer.go`, `task_increment_frontmatter_executor.go`, `task_update_frontmatter_executor.go`) updated to pass the injected `gitClient`
- `main.go` gains `USE_GIT_REST bool` (default `false`) and `GIT_REST_URL string` flags; when `USE_GIT_REST=true` the application constructs a `gitrestclient`-backed `GitClient` instead of the SSH-based one, skips the local `git clone`, and the scanner and all command handlers automatically use git-rest via the same interface
- Controller `/readiness` handler reflects git-rest health when `USE_GIT_REST=true`
- All tests updated to compile with the new `FindTaskFilePath` signature; existing behaviour is preserved on the `USE_GIT_REST=false` (default) code path
</summary>

<objective>
Adapt the vault scanner and result writer to use the `gitclient.GitClient` interface methods (`ListFiles`, `ReadFile`, `WriteFile`) instead of local filesystem calls. Then wire the `USE_GIT_REST` feature flag in `main.go` so the application can run either with the SSH-based gitclient or the HTTP-based gitrestclient adapter. After this prompt, `USE_GIT_REST=true` fully replaces the local git clone with git-rest HTTP API calls for all vault reads and writes.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-context-cancellation-in-loops.md` in `~/.claude/plugins/marketplaces/coding/docs/`

**Prerequisites:** Prompts 1 and 2 of spec-018 have shipped. Verify before editing:
```bash
grep -n "ListFiles\|ReadFile\|WriteFile" task/controller/pkg/gitclient/git_client.go
grep -n "func NewGitClient" task/controller/pkg/gitrestclient/git_client_adapter.go
```
If either returns empty, STOP and report `status: failed` with message "interface extension and adapter not yet deployed (prompts 1-2 of spec-018)".

**Key files to read IN FULL before editing (chunked reads if > 2000 lines):**

- `task/controller/pkg/scanner/vault_scanner.go` — `vaultScanner` struct, `runCycle`, `scanFiles`, `processFile`, `injectAndStore`. The new `NewGitRestVaultScanner` uses the same business logic but replaces `os.DirFS`, `fs.WalkDir`, `fs.ReadFile`, and `os.WriteFile` with `gitClient.ListFiles`, `gitClient.ReadFile`, `gitClient.WriteFile`.

- `task/controller/pkg/result/result_writer.go` — `FindTaskFilePath` function (lines 56-101). It takes `taskDirPath string` and uses `os.DirFS`. Change its signature to accept `gitClient gitclient.GitClient` and `taskDir string` (relative, e.g. `"tasks"`) instead of `taskDirPath`, and use `gitClient.ListFiles` + `gitClient.ReadFile`. All callers must be updated.

- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — calls `result.FindTaskFilePath(ctx, taskDirPath, cmd.TaskIdentifier)`. Must be updated to pass `gitClient`.

- `task/controller/pkg/command/task_update_frontmatter_executor.go` — same.

- `task/controller/pkg/result/result_writer.go` `WriteResult` method — calls `FindTaskFilePath`. Must be updated.

- `task/controller/main.go` — `application` struct, `Run` method, `createHTTPServer`. Add `USE_GIT_REST`, `GIT_REST_URL` fields; branch in `Run` on `a.UseGitRest`.

- `task/controller/pkg/factory/factory.go` — `CreateCommandConsumer` and `CreateSyncLoop`. No structural changes; the right `GitClient` implementation is passed in from `main.go`.

- `docs/controller-design.md` — `## Atomic Frontmatter Commands` (the Kafka command handlers prompt 3 wires) and `## Push Retry with Rebase` (which becomes obsolete on the gitrest path). Reference for the contracts being preserved.

- `docs/task-flow-and-failure-semantics.md` — `## Status Taxonomy`, frontmatter schema. Reference only; no edits.

- `docs/kafka-schema-design.md` — confirms partitioning by `task_id` so the read-modify-write under the gitrest adapter is correct.

Run before editing:
```bash
grep -n "FindTaskFilePath" task/controller/pkg/result/result_writer.go task/controller/pkg/command/*.go
grep -n "taskDirPath\|gitClient.Path" task/controller/pkg/command/task_increment_frontmatter_executor.go task/controller/pkg/command/task_update_frontmatter_executor.go
grep -n "GIT_URL\|GIT_REST\|USE_GIT" task/controller/main.go
ls task/controller/pkg/scanner/
```
</context>

<requirements>

1. **Adapt `task/controller/pkg/scanner/vault_scanner.go`**

   Add a second constructor `NewGitRestVaultScanner` that creates a `vaultScanner` configured to use `gitClient.ListFiles` / `gitClient.ReadFile` / `gitClient.WriteFile` instead of `os.DirFS` / `os.ReadFile` / `os.WriteFile`.

   The cleanest approach: introduce a `fileOps` interface or function set inside the scanner package that abstracts file listing/reading/writing, allowing one `vaultScanner` struct to work for both modes. Alternatively, add a boolean field `useGitClientOps` and branch in `scanFiles` and `processFile`.

   Recommended approach: add a `fileOps` struct field holding functions:
   ```go
   type fileOps struct {
       listFiles func(ctx context.Context, glob string) ([]string, error)
       readFile  func(ctx context.Context, relPath string) ([]byte, error)
       writeFile func(ctx context.Context, relPath string, content []byte) error
   }
   ```

   `NewVaultScanner` (existing, unchanged signature) creates `fileOps` that use `os.ReadFile` / local `filepath.Glob` / `os.WriteFile`.
   `NewGitRestVaultScanner` creates `fileOps` that delegate to `gitClient.ListFiles` / `gitClient.ReadFile` / `gitClient.WriteFile`.

   Update `scanFiles` and `processFile` to use `v.ops.listFiles`, `v.ops.readFile`, `v.ops.writeFile` instead of `os.DirFS`, `fs.WalkDir`, `fs.ReadFile`, `os.WriteFile`.

   IMPORTANT: The gitrest scanner's `runCycle` must NOT call `gitClient.CommitAndPush` after writing task_identifiers — each `WriteFile` call auto-commits via git-rest's POST. Change `runCycle` to skip `CommitAndPush` when in gitrest mode (the `fileOps.writeFile` already committed). The simplest way: the gitrest `fileOps.writeFile` commits automatically, so after writing, the `CommitAndPush` call would be a no-op anyway (the `gitRestGitClientAdapter.CommitAndPush` is already a no-op). No special casing needed.

   The existing `NewVaultScanner` constructor signature MUST NOT change — it is called from `factory.go` which is not modified.

   Add `NewGitRestVaultScanner(gitClient gitclient.GitClient, taskDir string, pollInterval time.Duration, trigger <-chan struct{}) VaultScanner`.

   Key difference in `scanFiles` for gitrest mode:
   - Use `v.ops.listFiles(ctx, taskDir+"/*.md")` instead of `fs.WalkDir(os.DirFS(taskDirPath), ".")`
   - For each path returned, use `v.ops.readFile(ctx, path)` to get content
   - relPath is the path as returned by ListFiles (e.g. `"tasks/foo.md"`)
   - absPath equivalent: for gitrest mode, the "absPath" passed to `v.ops.writeFile` is the relPath
   - The `injectAndStore` function writes to `absPath` on disk for the local mode; for gitrest mode it writes to `relPath` via `v.ops.writeFile(ctx, relPath, newContent)`

   Adapt `injectAndStore` to use the `ops` functions. The function currently calls `os.WriteFile(absPath, ...)` — change to call `v.ops.writeFile(ctx, relPath, ...)`.

   After the adaptation, add `NewGitRestVaultScanner` tests:
   - Test that `runCycle` calls `ops.listFiles` and `ops.readFile` for each file
   - Test that a new file (no task_identifier) triggers `ops.writeFile` with injected ID
   - Test that `ops.writeFile` error is logged but does not crash the scan cycle

2. **Adapt `task/controller/pkg/result/result_writer.go`**

   Change `FindTaskFilePath` signature:

   ```go
   // BEFORE:
   func FindTaskFilePath(
       ctx context.Context,
       taskDirPath string,
       id lib.TaskIdentifier,
   ) (string, lib.TaskFrontmatter, error)

   // AFTER:
   func FindTaskFilePath(
       ctx context.Context,
       gitClient gitclient.GitClient,
       taskDir string,
       id lib.TaskIdentifier,
   ) (string, lib.TaskFrontmatter, error)
   ```

   Implementation using the new GitClient methods:
   ```go
   glob := taskDir + "/*.md"
   paths, err := gitClient.ListFiles(ctx, glob)
   if err != nil {
       return "", nil, errors.Wrapf(ctx, err, "list task files with glob %s", glob)
   }
   for _, relPath := range paths {
       content, err := gitClient.ReadFile(ctx, relPath)
       if err != nil {
           glog.V(3).Infof("FindTaskFilePath: skip %s (read error: %v)", relPath, err)
           continue
       }
       // ... same frontmatter parsing and task_identifier matching logic as before ...
       // Return (relPath, existingFrontmatter, nil) when matched
       // Return ("", nil, nil) when no match (unchanged semantics)
   }
   return "", nil, nil
   ```

   The returned path is now a relative path (e.g. `"tasks/foo.md"`) — rename the variable everywhere from `matchedAbsPath` to `matchedRelPath` for honesty. Callers that need the historical absolute path compose it as `filepath.Join(gitClient.Path(), matchedRelPath)`.

   Update `resultWriter` struct to hold `gitClient gitclient.GitClient`:
   ```go
   type resultWriter struct {
       gitClient       gitclient.GitClient
       taskDir         string
       currentDateTime libtime.CurrentDateTimeGetter
   }
   ```
   (It already has `gitClient` — confirm by reading the file. If it does, the struct is already correct and only the method body needs changing.)

   Update `resultWriter.WriteResult` to call:
   ```go
   matchedRelPath, existingFrontmatter, err := FindTaskFilePath(ctx, r.gitClient, r.taskDir, req.TaskIdentifier)
   ```

   And use `matchedRelPath` (relative) to compute `absPath` for the `AtomicWriteAndCommitPush` call:
   ```go
   absPath := filepath.Join(r.gitClient.Path(), matchedRelPath)
   ```

   **Path()-under-gitrest contract**: under `USE_GIT_REST=true`, `gitClient.Path()` returns the logical base path (`/data/vault`) which does NOT exist on disk. The adapter's `AtomicWriteAndCommitPush` is responsible for stripping `Path()` back off to recover the relative path; callers MUST pass `filepath.Join(gitClient.Path(), relPath)` so the adapter's `filepath.Rel(basePath, absPath)` yields the original `relPath`. Local-disk gitclient continues to use the real path, unchanged. Both modes share the same caller code; the contract is enforced by the adapter's compile-time `var _ gitclient.GitClient` assertion plus a unit test (added below).

   **Add `## Review` preservation test in `result_writer_test.go`** (mandatory — spec AC headline regression):
   ```go
   It("preserves prior ## Review content when writing a new result", func() {
       fakeGC := &mocks.FakeGitClient{}
       fakeGC.ListFilesReturns([]string{"tasks/foo.md"}, nil)
       fakeGC.ReadFileReturns([]byte(`---
   task_identifier: foo
   ---
   # Body
   ## Review
   Prior review content
   `), nil)
       fakeGC.PathReturns("/data/vault")
       w := result.NewResultWriter(fakeGC, "tasks", clock)
       err := w.WriteResult(ctx, &lib.UpdateTaskCommand{
           TaskIdentifier: "foo",
           Frontmatter:    lib.TaskFrontmatter{...},
           Content:        "# Body\n\n## Review\nNew review content\n",
       })
       Expect(err).NotTo(HaveOccurred())

       // Inspect the bytes passed to AtomicWriteAndCommitPush:
       Expect(fakeGC.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
       _, _, content, _ := fakeGC.AtomicWriteAndCommitPushArgsForCall(0)
       // Either the prior review survives in-place, OR it survives under an
       // "## Outdated by force-push <sha>" marker. NEVER stripped silently.
       Expect(string(content)).To(SatisfyAny(
           ContainSubstring("Prior review content"),
           ContainSubstring("## Outdated by"),
       ))
   })
   ```
   This test must FAIL on a write path that strips `## Review` and PASS on a write path that preserves it (today's bug). It locks down the behaviour the spec exists to fix. Add it whether or not the WriteResult code is changed in this prompt.

   **Add boundary test for `FindTaskFilePath`** asserting it actually drives the gitClient (not `os.DirFS`):
   ```go
   It("calls gitClient.ListFiles + ReadFile with the expected glob and matched paths", func() {
       fakeGC := &mocks.FakeGitClient{}
       fakeGC.ListFilesReturns([]string{"tasks/a.md", "tasks/b.md"}, nil)
       fakeGC.ReadFileReturnsOnCall(0, []byte("---\ntask_identifier: foo\n---\n"), nil)
       fakeGC.ReadFileReturnsOnCall(1, []byte("---\ntask_identifier: bar\n---\n"), nil)
       _, _, err := result.FindTaskFilePath(ctx, fakeGC, "tasks", "bar")
       Expect(err).NotTo(HaveOccurred())
       Expect(fakeGC.ListFilesCallCount()).To(Equal(1))
       _, glob := fakeGC.ListFilesArgsForCall(0)
       Expect(glob).To(Equal("tasks/*.md"))
       Expect(fakeGC.ReadFileCallCount()).To(BeNumerically(">=", 1))
   })
   ```

3. **Update all callers of `FindTaskFilePath`**

   In `task/controller/pkg/command/task_increment_frontmatter_executor.go`:
   ```go
   // BEFORE:
   taskDirPath := filepath.Join(gitClient.Path(), taskDir)
   absPath, _, err := result.FindTaskFilePath(ctx, taskDirPath, cmd.TaskIdentifier)

   // AFTER:
   absPath, _, err := result.FindTaskFilePath(ctx, gitClient, taskDir, cmd.TaskIdentifier)
   // absPath is now relative from gitClient.ListFiles; compute full absPath for gitClient methods:
   fullAbsPath := filepath.Join(gitClient.Path(), absPath)
   ```
   Then use `fullAbsPath` in `gitClient.AtomicReadModifyWriteAndCommitPush`.

   Apply the same pattern to `task_update_frontmatter_executor.go`.

   Check for any other callers:
   ```bash
   grep -rn "FindTaskFilePath" task/controller/
   ```
   Update all.

4. **Update tests that call `FindTaskFilePath`**

   ```bash
   grep -rn "FindTaskFilePath" task/controller/pkg/
   ```

   For each test file that creates a fake task dir and calls `FindTaskFilePath` directly: update the call signature. The test will pass a `FakeGitClient` with `ListFilesReturns(...)` and `ReadFileReturns(...)` stubs.

   For scanner tests: update to use the `ops` function approach if scanner tests were testing internal behavior via mocked gitClient.

5. **Add `USE_GIT_REST` and `GIT_REST_URL` to `task/controller/main.go`**

   Add two new fields to the `application` struct:
   ```go
   GitRestURL  string `required:"false" arg:"git-rest-url"   env:"GIT_REST_URL"   usage:"git-rest HTTP API base URL (required when USE_GIT_REST=true)" default:"http://vault-obsidian-openclaw:9090"`
   UseGitRest  bool   `required:"false" arg:"use-git-rest"   env:"USE_GIT_REST"   usage:"use git-rest HTTP API instead of local git clone"             default:"false"`
   ```

   In `application.Run`, replace the gitclient construction block with:
   ```go
   var gitClient gitclient.GitClient
   if a.UseGitRest {
       if a.GitRestURL == "" {
           return errors.Errorf(ctx, "GIT_REST_URL is required when USE_GIT_REST=true")
       }
       restClient := gitrestclient.NewGitRestClient(a.GitRestURL)
       gitClient = gitrestclient.NewGitClient(restClient, vaultLocalPath)
       if err := gitClient.EnsureCloned(ctx); err != nil {
           return errors.Wrapf(ctx, err, "probe git-rest readiness")
       }
       glog.V(1).Infof("using git-rest HTTP API at %s", a.GitRestURL)
   } else {
       conflictResolver := conflict.NewGeminiConflictResolver(a.GeminiAPIKey)
       gitClient = gitclient.NewGitClient(a.GitURL, vaultLocalPath, a.GitBranch, conflictResolver)
       if err := gitClient.EnsureCloned(ctx); err != nil {
           return errors.Wrapf(ctx, err, "ensure git clone")
       }
   }
   ```

   Add the import:
   ```go
   "github.com/bborbe/agent/task/controller/pkg/gitrestclient"
   ```

   In `createHTTPServer`, update the `/readiness` handler to reflect git-rest health when `USE_GIT_REST=true`:

   Change the method signature to accept the gitRestClient:
   ```go
   func (a *application) createHTTPServer(syncLoop pkgsync.SyncLoop, gitRestClient gitrestclient.GitRestClient) run.Func
   ```

   In the `/readiness` handler:
   ```go
   router.Path("/readiness").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
       w.Header().Set("Content-Type", "text/plain")
       if gitRestClient != nil {
           ready, err := gitRestClient.IsReady(req.Context())
           if err != nil || !ready {
               w.WriteHeader(http.StatusServiceUnavailable)
               _, _ = w.Write([]byte("git-rest not ready"))
               return
           }
       }
       _, _ = w.Write([]byte("OK"))
   })
   ```

   **Note on Kafka pause coupling**: spec Desired Behavior #4 says "If git-rest's `/readiness` returns 503, controller pauses Kafka consumption — Kafka offsets stay put — and resumes when readiness flips back to 200." For this prompt, the pause comes for free from prompt 1's `Post`/`Delete` retry logic: when git-rest returns 5xx, retries block the consumer goroutine inside the command handler; offsets are not advanced because the handler does not return success. The `controller_kafka_consume_paused_total` counter is incremented inside `Post`/`Delete` retry (prompt 1). No additional Kafka-side pause logic is needed in this prompt. If a future requirement needs to pause AT the consumer (e.g. before invoking the handler), that becomes a separate spec.

   Pass `gitRestClient` from `Run`:
   ```go
   var gitRestClientForReadiness gitrestclient.GitRestClient
   if a.UseGitRest {
       gitRestClientForReadiness = restClient // same instance created above
   }
   // ...
   return service.Run(
       ctx,
       syncLoop.Run,
       commandConsumer,
       a.createHTTPServer(syncLoop, gitRestClientForReadiness),
   )
   ```

   Also update `factory.CreateSyncLoop` call in `Run`: when `USE_GIT_REST=true`, use `NewGitRestVaultScanner` instead of (implicitly via factory) the default scanner:
   ```go
   var syncLoop pkgsync.SyncLoop
   if a.UseGitRest {
       trigger := make(chan struct{}, 1)
       restScanner := scanner.NewGitRestVaultScanner(gitClient, a.TaskDir, a.PollInterval, trigger)
       syncLoop = pkgsync.NewSyncLoop(
           restScanner,
           publisher.NewTaskPublisher(eventObjectSender, lib.TaskV1SchemaID, currentDateTime),
           trigger,
       )
   } else {
       syncLoop = factory.CreateSyncLoop(gitClient, a.TaskDir, a.PollInterval, eventObjectSender, currentDateTime)
   }
   ```

   Add required imports if not already present:
   ```go
   "github.com/bborbe/agent/task/controller/pkg/scanner"
   "github.com/bborbe/agent/task/controller/pkg/publisher"
   ```

6. **Update `CHANGELOG.md` at repo root**

   Append bullets to `## Unreleased` (insert the heading under `# Changelog` if absent):

   ```markdown
   - feat(task/controller): adapt vault scanner and `FindTaskFilePath` to use `gitclient.GitClient` interface methods instead of `os.DirFS` — enables git-rest HTTP mode
   - feat(task/controller): add `USE_GIT_REST` and `GIT_REST_URL` flags to `main.go`; feature flag switches all vault I/O to git-rest HTTP API when enabled
   - feat(task/controller): controller `/readiness` reflects git-rest health when `USE_GIT_REST=true`
   ```

7. **Run iterative tests:**

   ```bash
   cd task/controller && make test
   ```
   Must pass. Fix any compilation errors from the `FindTaskFilePath` signature change.

   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- `FindTaskFilePath` signature change is the most impactful: grep ALL callers before editing and update every one
- The existing `NewVaultScanner` constructor signature MUST NOT change — `factory.go` calls it unchanged
- `USE_GIT_REST` default is `false` — existing deployments are unaffected until the flag is set
- When `USE_GIT_REST=false`, the entire old code path runs unchanged (gitclient, local clone, os.DirFS in scanner)
- `GIT_REST_URL` default is `"http://vault-obsidian-openclaw:9090"` — correct for both dev and prod namespace (spec constraint)
- `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY` flags remain in the struct as-is — removal is prompt 5 (final cleanup)
- Error wrapping via `github.com/bborbe/errors` — never `fmt.Errorf`
- Ginkgo v2 + Gomega; `FakeGitClient` from `mocks/` for updated tests
- All existing tests must still pass with no behavioral changes on the default path
- Do NOT commit — dark-factory handles git
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>
```bash
# Verify FindTaskFilePath new signature
grep -n "func FindTaskFilePath" task/controller/pkg/result/result_writer.go
# Must show: (ctx context.Context, gitClient gitclient.GitClient, taskDir string, id lib.TaskIdentifier)

# Verify all callers updated
grep -rn "FindTaskFilePath" task/controller/pkg/
# Must show no callers passing a string taskDirPath; all must pass a gitClient

# Verify new scanner constructor
grep -n "func NewGitRestVaultScanner" task/controller/pkg/scanner/vault_scanner.go
# Must show the function

# Verify new flags in main.go
grep -n "USE_GIT_REST\|GIT_REST_URL\|UseGitRest\|GitRestURL" task/controller/main.go
# Must show struct fields and usage in Run()

# Verify readiness handler updated
grep -n "gitRestClient\|IsReady\|ServiceUnavailable" task/controller/main.go
# Must show the readiness check

# Verify import added
grep -n "gitrestclient" task/controller/main.go
# Must show the import

# Run all tests
cd task/controller && make test
# Must exit 0

cd task/controller && make precommit
# Must exit 0

grep -n "USE_GIT_REST\|FindTaskFilePath\|vault scanner" CHANGELOG.md
# Must show Unreleased entries
```
</verification>
