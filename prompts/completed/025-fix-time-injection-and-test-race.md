---
status: completed
summary: Injected CurrentDateTimeGetter into taskPublisher replacing time.Now(), removed time.Local and format.TruncatedDiff from main_test.go files to fix data race with gexec.Build, updated factory and main.go call sites, updated publisher tests with fixed time assertions
container: agent-025-fix-time-injection-and-test-race
dark-factory-version: v0.89.1-dirty
created: "2026-04-03T09:20:00Z"
queued: "2026-04-03T09:49:09Z"
started: "2026-04-03T09:49:42Z"
completed: "2026-04-03T10:10:17Z"
---

<summary>
- Production code no longer calls time.Now() directly — uses an injected time provider
- Main test suites no longer set time.Local, eliminating the data race with gexec.Build
- Publisher constructor accepts a time provider as parameter, following coding guidelines
- Factory wiring updated to pass the time provider through to publisher
- Publisher tests use a fixed time for deterministic assertions
</summary>

<objective>
Fix data race in test suites caused by `time.Local = time.UTC` combined with `gexec.Build()`, and replace the remaining `time.Now()` call in production code with injected `CurrentDateTimeGetter` per coding guidelines.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Root cause:** The `main_test.go` files set `time.Local = time.UTC` (a global variable write) in `TestSuite()`, which races with `gexec.Build()` running `go build` in a goroutine that internally calls `time.Now()` (a global variable read). Main packages with `gexec.Build` should NOT have `time.Local` setup because the build goroutine reads time globals concurrently.

**Coding guidelines reference (go-time-injection.md):**
- Never call `time.Now()` directly in production code
- Inject `CurrentDateTimeGetter` via constructor
- Create `libtime.NewCurrentDateTime()` once in `main.go`, pass down
- In tests: use `currentDateTime.SetNow()` with fixed time

Files to read before making changes:
- `task/controller/main_test.go` — has `time.Local = time.UTC` + `gexec.Build` (the race)
- `task/executor/main_test.go` — same issue
- `task/controller/pkg/publisher/task_publisher.go` — has `time.Now()` on line 47
- `task/controller/pkg/publisher/task_publisher_test.go` — tests for publisher
- `task/controller/pkg/factory/factory.go` — where `NewTaskPublisher` is called in `CreateSyncLoop` (line 40)
- `task/controller/main.go` — where `CreateSyncLoop` is called (line 78, need to create `currentDateTime` and pass it)

Also read for time injection patterns (external coding guide):
- `~/Documents/workspaces/coding/docs/go-time-injection.md`
</context>

<requirements>
1. **Fix `task/controller/main_test.go`:**
   - Remove `time.Local = time.UTC` line
   - Remove `"time"` import if no longer needed (keep it if `suiteConfig.Timeout` uses it)
   - Remove `format.TruncatedDiff = false` and `"github.com/onsi/gomega/format"` import — main packages should follow the minimal pattern from the coding guide
   - Keep `gexec.Build`, `suiteConfig.Timeout`, and `//go:generate` directive

2. **Fix `task/executor/main_test.go`:**
   - Same changes as requirement 1

3. **Inject `CurrentDateTimeGetter` into `taskPublisher`:**
   - Add `currentDateTimeGetter libtime.CurrentDateTimeGetter` parameter to `NewTaskPublisher`
   - Store it in `taskPublisher` struct
   - Replace `time.Now()` on line 47 with `s.currentDateTimeGetter.Now()`
   - Remove `"time"` import from `task_publisher.go` (keep `libtime` import)
   - Import: `libtime "github.com/bborbe/time"` (already present)

4. **Update publisher constructor call site in `task/controller/pkg/factory/factory.go`:**
   - `NewTaskPublisher` is called in `CreateSyncLoop` at line 40
   - Add `currentDateTimeGetter libtime.CurrentDateTimeGetter` parameter to `CreateSyncLoop`:
     ```go
     // Old signature:
     func CreateSyncLoop(gitClient gitclient.GitClient, taskDir string, pollInterval time.Duration, eventObjectSender cdb.EventObjectSender) pkgsync.SyncLoop
     // New signature:
     func CreateSyncLoop(gitClient gitclient.GitClient, taskDir string, pollInterval time.Duration, eventObjectSender cdb.EventObjectSender, currentDateTimeGetter libtime.CurrentDateTimeGetter) pkgsync.SyncLoop
     ```
   - Pass it through to `publisher.NewTaskPublisher(eventObjectSender, lib.TaskV1SchemaID, currentDateTimeGetter)`
   - Update call site in `task/controller/main.go` (line 78) where `CreateSyncLoop` is called: create `currentDateTime := libtime.NewCurrentDateTime()` and pass it as last argument

5. **Update publisher tests:**
   - In `task/controller/pkg/publisher/task_publisher_test.go`:
   - Create `currentDateTime := libtime.NewCurrentDateTime()` in `BeforeEach`
   - Set fixed time: `currentDateTime.SetNow(libtimetest.ParseDateTime("2026-01-15T10:00:00Z"))`
   - Pass `currentDateTime` to `NewTaskPublisher`
   - Assert that `task.Object.Created` and `task.Object.Modified` equal the fixed time after calling `PublishChanged`
   - Import `libtimetest "github.com/bborbe/time/test"` if using `ParseDateTime`

6. **Regenerate mocks** in task/controller:
   ```bash
   cd task/controller && make generate
   ```

7. **Run tests** in both modules:
   ```bash
   cd task/controller && make test
   cd task/executor && make test
   ```
</requirements>

<constraints>
- Do NOT remove `time.Local = time.UTC` from non-main test suites (only from `main_test.go` files) — the standard package pattern uses it and it doesn't race there
- Do NOT change `task/controller/pkg/publisher/task_publisher.go` PublishDeleted method (it doesn't use time)
- Use `github.com/bborbe/time` (imported as `libtime`), never `time.Now()` in production
- Use `github.com/bborbe/time/test` (imported as `libtimetest`) only in test files
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Do NOT update CHANGELOG.md
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
Verify no `time.Now()` in production code:

```bash
grep -rn "time.Now()" --include="*.go" task/controller/pkg/ task/executor/pkg/ lib/ | grep -v _test.go | grep -v vendor | grep -v mocks
```
Must produce no output.

Verify `main_test.go` files don't set `time.Local`:

```bash
grep -n "time.Local" task/controller/main_test.go task/executor/main_test.go
```
Must produce no output.

Run tests in task/controller:

```bash
cd task/controller && make test
```
Must pass with exit code 0.

Run tests in task/executor:

```bash
cd task/executor && make test
```
Must pass with exit code 0.

Run race detector specifically on controller:

```bash
cd task/controller && go test -race -count=1 ./... 2>&1 | grep -c "DATA RACE"
```
Must output 0.
</verification>
