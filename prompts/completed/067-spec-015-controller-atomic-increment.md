---
status: completed
spec: [015-atomic-frontmatter-increment-and-trigger-cap]
summary: Implemented AtomicReadModifyWriteAndCommitPush on GitClient, IncrementFrontmatterExecutor and UpdateFrontmatterExecutor command handlers with phase escalation, exported FindTaskFilePath/ExtractFrontmatter/ExtractBody helpers, added FrontmatterCommandsTotal metric, wired both executors in factory, and added comprehensive unit tests.
container: agent-067-spec-015-controller-atomic-increment
dark-factory-version: v0.132.0
created: "2026-04-24T07:42:14Z"
queued: "2026-04-24T08:05:26Z"
started: "2026-04-24T08:08:55Z"
completed: "2026-04-24T08:19:47Z"
branch: dark-factory/atomic-frontmatter-increment-and-trigger-cap
---

<summary>
- `GitClient` interface gains `AtomicReadModifyWriteAndCommitPush` — a method that reads a file, calls a user-supplied `modify` function on its bytes, writes the result, and commits+pushes, all under the existing mutex so no read-modify-write race is possible
- New `IncrementFrontmatterExecutor` command handler reads the task file from disk, increments the named field under the gitclient mutex, and writes atomically; if the new value reaches `max_triggers`, it also sets `phase: human_review` in the same write (mirroring `applyRetryCounter` for `trigger_count`)
- New `UpdateFrontmatterExecutor` command handler reads the task file, merges only the specified key-value pairs, and writes atomically — no other frontmatter keys are touched
- Both handlers are registered alongside the existing `TaskResultExecutor` in `factory.go`
- Unit tests cover: monotonic increment across sequential commands, only-named-keys mutation for partial update, phase escalation when trigger_count reaches max_triggers
- `docs/controller-design.md` updated to document the new atomic commands and their write contract
- `cd task/controller && make precommit` passes
</summary>

<objective>
Implement the controller-side handlers for the two new atomic frontmatter commands introduced in prompt 1. `IncrementFrontmatterExecutor` makes the `trigger_count` bump structurally non-idempotent (reads current value from disk → increments → writes), closing the "nothing to commit" failure mode from the original spawn-loop bug. `UpdateFrontmatterExecutor` enables safe partial-key edits without clobbering concurrent writes. This prompt depends on prompt 1 (`lib.IncrementFrontmatterCommand`, `lib.UpdateFrontmatterCommand`, and their operation constants must exist in `lib/`).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, counterfeiter mocks, external test packages
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf

**This prompt depends on prompt 1 being complete.** Verify before starting:
```bash
grep -n "IncrementFrontmatterCommand\|TriggerCount\|MaxTriggers" lib/agent_task-commands.go lib/agent_task-frontmatter.go 2>/dev/null
```
If those symbols are absent, stop — prompt 1 has not been applied.

**Key files to read in full before editing:**

- `task/controller/pkg/gitclient/git_client.go` — full file; understand the `GitClient` interface, the private `mu sync.Mutex`, how `AtomicWriteAndCommitPush` (line ~331) acquires the lock, writes the file, then calls private `commitAndPush`; understand what `commitAndPush` does (git add, git commit, push with retry)
- `task/controller/pkg/command/task_result_executor.go` — the existing "update" handler; this is the exact structural pattern to follow for the two new handlers (same `cdb.CommandObjectExecutorTxFunc` shape)
- `task/controller/pkg/result/result_writer.go` — `WriteResult`, `applyRetryCounter`, `mergeFrontmatter`, `extractFrontmatter`, and how the file is located by walking the task dir (`WalkDir` logic lines 60–96); the new handlers share this file-finding logic
- `task/controller/pkg/factory/factory.go` — how `NewTaskResultExecutor` is instantiated and wrapped in `cdb.CommandObjectExecutorTxs`; the two new executors are added to the same slice
- `task/controller/pkg/metrics/metrics.go` — existing metric vars; add new labels, do not rename existing ones

Run these before editing to map the current codebase:
```bash
grep -n "func.*GitClient\|interface" task/controller/pkg/gitclient/git_client.go | head -20
grep -n "AtomicWriteAndCommitPush\|commitAndPush\|mu\.Lock\|mu\.Unlock" task/controller/pkg/gitclient/git_client.go | head -30
grep -n "CommandObjectExecutorTxFunc\|cdb\." task/controller/pkg/command/task_result_executor.go | head -20
grep -n "CommandObjectExecutorTxs\|NewTaskResultExecutor\|RunCommandConsumer" task/controller/pkg/factory/factory.go | head -20
grep -n "SchemaID\|TaskV1SchemaID" lib/agent_cdb-schema.go task/controller/pkg/command/task_result_executor.go 2>/dev/null | head -20
```
</context>

<requirements>

1. **Verify prompt 1 is applied**

   ```bash
   grep -n "IncrementFrontmatterCommand\|UpdateFrontmatterCommand" lib/agent_task-commands.go
   grep -n "TriggerCount\|MaxTriggers" lib/agent_task-frontmatter.go
   ```
   Both must show the symbols. If either is absent, stop.

2. **Add `AtomicReadModifyWriteAndCommitPush` to `GitClient`**

   Read `task/controller/pkg/gitclient/git_client.go` in full first.

   In the `GitClient` interface, add after `AtomicWriteAndCommitPush`:
   ```go
   // AtomicReadModifyWriteAndCommitPush reads absPath, calls modify on its contents
   // to produce new contents, writes the result, and commits+pushes — all under
   // the gitclient mutex. modify must return the new file bytes or an error.
   // If modify returns an error, the file is not written and no commit is made.
   AtomicReadModifyWriteAndCommitPush(
       ctx context.Context,
       absPath string,
       modify func(current []byte) ([]byte, error),
       message string,
   ) error
   ```

   In the concrete implementation (the struct that implements `GitClient`), add the method:
   ```go
   func (g *gitClient) AtomicReadModifyWriteAndCommitPush(
       ctx context.Context,
       absPath string,
       modify func(current []byte) ([]byte, error),
       message string,
   ) error {
       g.mu.Lock()
       defer g.mu.Unlock()

       current, err := os.ReadFile(absPath)
       if err != nil {
           return errors.Wrapf(ctx, err, "read file for atomic modify")
       }
       updated, err := modify(current)
       if err != nil {
           return errors.Wrapf(ctx, err, "modify func failed")
       }
       if err := os.WriteFile(absPath, updated, 0600); err != nil {
           return errors.Wrapf(ctx, err, "write file for atomic modify")
       }
       return g.commitAndPush(ctx, message)
   }
   ```

   Adjust variable names and file-write mode to match the existing code style (read the file to confirm — use `0600` unless the existing code uses a different mode for task files). The key invariant: `os.ReadFile`, the `modify` call, `os.WriteFile`, and `commitAndPush` all run under a single mutex acquisition.

3. **Extract helpers in `task/controller/pkg/result/result_writer.go` and export for cross-package use**

   Decision (fixed — do not deviate): extract and **export** three helpers from `result_writer.go` so the new command handlers in `task/controller/pkg/command/` can reuse them without duplicating logic:

   ```go
   // FindTaskFilePath walks taskDirPath and returns the absolute path of the .md file
   // whose frontmatter has task_identifier == id, plus the parsed existing frontmatter.
   // Returns ("", nil, nil) when no match is found (not an error).
   func FindTaskFilePath(ctx context.Context, taskDirPath string, id lib.TaskIdentifier) (string, lib.TaskFrontmatter, error)

   // ExtractFrontmatter returns the YAML frontmatter string between the opening and
   // closing "---" delimiters. Returns an error if delimiters are missing.
   func ExtractFrontmatter(ctx context.Context, content []byte) (string, error)

   // ExtractBody returns the file body — the bytes after the closing "---\n" delimiter.
   // Returns an empty string if the body is empty; error if delimiters are missing.
   func ExtractBody(ctx context.Context, content []byte) (string, error)
   ```

   Rename the existing unexported `extractFrontmatter` to `ExtractFrontmatter` and update its internal callers. Add a sibling `ExtractBody` that parses the same content and returns everything after the closing `---\n` delimiter. Refactor `WriteResult` to use `FindTaskFilePath` (remove the inline WalkDir). All existing `result` package tests must pass after the refactor.

   **Placement decision (fixed)**: the new executors live in `task/controller/pkg/command/` (same package as `task_result_executor.go`), importing these exported helpers from `task/controller/pkg/result`. Do NOT move the new executors into the `result` package — keep the command/executor separation consistent with `task_result_executor.go`.

4. **Create `task/controller/pkg/command/task_increment_frontmatter_executor.go`**

   Follow the exact structural pattern from `task_result_executor.go`:
   - Declare an operation constant: `const IncrementFrontmatterCommandOperation = lib.IncrementFrontmatterCommandOperation`
   - `NewIncrementFrontmatterExecutor(gitClient gitclient.GitClient, taskDir string) cdb.CommandObjectExecutorTx`
   - Returns a `cdb.CommandObjectExecutorTxFunc` (or equivalent — read the existing handler to confirm the exact type)
   - Operation: `IncrementFrontmatterCommandOperation`
   - Handler logic:
     1. Deserialize `commandObject.Command.Data` into `lib.IncrementFrontmatterCommand`
     2. Call `result.FindTaskFilePath(ctx, taskDirPath, cmd.TaskIdentifier)` to locate the file
     3. If file not found: log warning, increment `metrics.FrontmatterCommandsTotal.WithLabelValues("increment_frontmatter", "not_found")`, return nil (no error — same pattern as result_writer's not_found handling)
     4. Call `gitClient.AtomicReadModifyWriteAndCommitPush(ctx, absPath, modifyFn, message)` where `modifyFn`:
        a. Calls `result.ExtractFrontmatter(ctx, current)` to get existing frontmatter YAML string
        b. Calls `result.ExtractBody(ctx, current)` to get the body (bytes after the closing `---\n`)
        c. Unmarshals frontmatter YAML into `lib.TaskFrontmatter`
        d. Reads current value of `cmd.Field` using the same int/float64 switch as `TriggerCount()`, defaulting to 0
        e. Computes `newVal := currentVal + cmd.Delta`
        f. Sets `frontmatter[cmd.Field] = newVal`
        g. **Cap escalation**: if `cmd.Field == "trigger_count" && newVal >= frontmatter.MaxTriggers()` → set `frontmatter["phase"] = string(domain.TaskPhaseHumanReview)` in the same write. Import path: `github.com/bborbe/vault-cli/pkg/domain`. Verify the constant name with `grep -n "TaskPhaseHumanReview" vendor/github.com/bborbe/vault-cli/pkg/domain/*.go` if `go mod` vendor is present, otherwise from the already-vendored package in another file of this repo.
        h. Marshals updated frontmatter back to YAML using the **exact pattern from `result_writer.go:107`**: `yaml.Marshal(map[string]any(merged))` (the `map[string]any(...)` cast is load-bearing; `yaml.Marshal(merged)` directly produces different output because `TaskFrontmatter` has YAML tags).
        i. Reconstructs file content: `"---\n" + string(marshaledFrontmatter) + "---\n" + body`
        j. Returns new file bytes
     5. After the atomic write succeeds, follow the same return pattern as `task_result_executor.go` (return nil on success; dark-factory's CQRS layer handles the ACK/event emission).
   - Metrics: increment `metrics.FrontmatterCommandsTotal.WithLabelValues("increment_frontmatter", "success")` on success, `"error"` on error. Do NOT reuse `ResultsWrittenTotal` — that metric is semantically for agent result writes, and reusing it would muddy dashboards.

5. **Create `task/controller/pkg/command/task_update_frontmatter_executor.go`**

   Same structural pattern:
   - `const UpdateFrontmatterCommandOperation = lib.UpdateFrontmatterCommandOperation`
   - `NewUpdateFrontmatterExecutor(gitClient gitclient.GitClient, taskDir string) cdb.CommandObjectExecutorTx`
   - Handler logic:
     1. Deserialize `commandObject.Command.Data` into `lib.UpdateFrontmatterCommand`
     2. Call `result.FindTaskFilePath(...)` to locate the file
     3. If not found: log warning, increment `metrics.FrontmatterCommandsTotal.WithLabelValues("update_frontmatter", "not_found")`, return nil
     4. If `cmd.Updates == nil || len(cmd.Updates) == 0`: return nil without calling `AtomicReadModifyWriteAndCommitPush` (no-op write, no metric increment). Document this explicitly as "empty updates is a valid no-op".
     5. Call `gitClient.AtomicReadModifyWriteAndCommitPush(ctx, absPath, modifyFn, message)` where `modifyFn`:
        a. Extract frontmatter + body via `result.ExtractFrontmatter` / `result.ExtractBody`
        b. Unmarshal frontmatter YAML into `lib.TaskFrontmatter`
        c. For each key in `cmd.Updates`, set `frontmatter[key] = value` (merge semantics)
        d. **Leaves all other frontmatter keys unchanged**
        e. Marshal frontmatter back via `yaml.Marshal(map[string]any(merged))` (same pattern as increment handler — see step 4 point h)
        f. Reconstruct file content: `"---\n" + string(marshaledFrontmatter) + "---\n" + body`
        g. Returns new bytes
     6. Metrics: `metrics.FrontmatterCommandsTotal.WithLabelValues("update_frontmatter", "success")` on success, `"error"` on error

6. **Wire both new executors in `task/controller/pkg/factory/factory.go` — signature change required**

   Read `factory.go` and `main.go` / app wiring to understand the full call chain. The new executors take `gitClient` and `taskDir` as constructor parameters, but the current `CreateCommandConsumer(...)` factory function does NOT receive these — `gitClient` and `taskDir` are captured privately inside `resultWriter`. You MUST extend `CreateCommandConsumer` and update its caller(s).

   Steps:

   a. Grep for callers first:
      ```bash
      grep -rn "CreateCommandConsumer(" --include="*.go" .
      ```
      Expect one or two call sites in `cmd/` or `main.go`.

   b. Change `CreateCommandConsumer` signature — add `gitClient gitclient.GitClient` and `taskDir string` parameters. Document the old → new signature at the top of the function comment.

   c. Update every caller to pass `gitClient` and `taskDir`. These are already being constructed earlier in the wiring (they must exist, since `resultWriter` takes them) — just forward them.

   d. Inside `CreateCommandConsumer`, build the executors slice:
      ```go
      executors := cdb.CommandObjectExecutorTxs{
          command.NewTaskResultExecutor(resultWriter),
          command.NewIncrementFrontmatterExecutor(gitClient, taskDir),
          command.NewUpdateFrontmatterExecutor(gitClient, taskDir),
      }
      ```
      Adjust variable names to match the actual local names in `factory.go`.

   e. Do NOT capture `gitClient` / `taskDir` via the existing `resultWriter` — they are private fields of an unexported struct. Thread them explicitly.

7. **Add unit tests**

   Create `task/controller/pkg/command/task_increment_frontmatter_executor_test.go` and `task_update_frontmatter_executor_test.go`. Read the existing test file in that package (if any) to match style.

   **For IncrementFrontmatterExecutor:**

   a. **Monotonic increment**: Send two sequential `IncrementFrontmatterCommand{Field: "trigger_count", Delta: 1}` for the same task. Assert the final frontmatter value is 2, not 1 (i.e., no lost increment).

   b. **Phase escalation at cap**: Set `max_triggers: 2` in the initial frontmatter. Send one increment (trigger_count: 0 → 1, no escalation), then a second (1 → 2, escalation: phase becomes `human_review`). Assert `phase == "human_review"` after the second increment.

   c. **No escalation below cap**: trigger_count=1, max_triggers=3 → after increment → phase is unchanged.

   d. **Field not present**: Treat as 0; after increment, field == 1.

   **For UpdateFrontmatterExecutor:**

   e. **Only named keys change**: Start with frontmatter `{status: "in_progress", phase: "ai_review", assignee: "claude"}`. Send `UpdateFrontmatterCommand{Updates: {phase: "human_review"}}`. Assert `phase == "human_review"`, `status == "in_progress"` (unchanged), `assignee == "claude"` (unchanged).

   f. **Empty updates**: Send with `Updates: nil` or empty map. Assert file is unchanged (or write is a no-op).

   Use a temporary directory + real files for these tests (not a mock gitclient), or use the existing fake gitclient if one exists in `task/controller/mocks/`. Read the test infrastructure in `task/controller/pkg/` to decide the right approach. The tests must validate the actual byte contents of the modified file (read the file after the command runs and parse the frontmatter to assert values).

8. **Add `AtomicReadModifyWriteAndCommitPush` test to gitclient tests**

   Find `task/controller/pkg/gitclient/git_client_test.go`. Add a test that:
   - Creates a temp file with initial content
   - Calls `AtomicReadModifyWriteAndCommitPush` with a modify function that appends " modified"
   - Asserts the file now contains " modified"
   - (Skip the commit assertion if the test uses a fake git or stub — match the existing test infrastructure)

9. **Add new `FrontmatterCommandsTotal` metric**

   In `task/controller/pkg/metrics/metrics.go`, add a new Prometheus counter vector:

   ```go
   // FrontmatterCommandsTotal counts atomic frontmatter command executions
   // by operation ("increment_frontmatter" | "update_frontmatter") and
   // outcome ("success" | "error" | "not_found").
   var FrontmatterCommandsTotal = promauto.NewCounterVec(
       prometheus.CounterOpts{
           Name: "agent_task_controller_frontmatter_commands_total",
           Help: "Total number of atomic frontmatter commands processed, by operation and outcome.",
       },
       []string{"operation", "outcome"},
   )
   ```

   Pre-initialise every label combination in the `init()` block (same pattern as existing metrics — line ~65 of `metrics.go`):

   ```go
   for _, op := range []string{"increment_frontmatter", "update_frontmatter"} {
       for _, outcome := range []string{"success", "error", "not_found"} {
           FrontmatterCommandsTotal.WithLabelValues(op, outcome).Add(0)
       }
   }
   ```

   Use the `.Add(0)` pattern — that is the existing convention in this file, not bare `.WithLabelValues(...)`.

10. **Update `docs/controller-design.md`**

   Add a section or update the relevant section to document:
   - `AtomicReadModifyWriteAndCommitPush` and how it differs from `AtomicWriteAndCommitPush` (read provides a consistent baseline under lock)
   - `IncrementFrontmatterExecutor`: operation `"increment_frontmatter"`, payload shape, cap escalation behaviour
   - `UpdateFrontmatterExecutor`: operation `"update_frontmatter"`, payload shape, partial-key semantics

11. **Run tests iteratively**

    After steps 2–3:
    ```bash
    cd task/controller && make test
    ```
    After steps 4–6:
    ```bash
    cd task/controller && make test
    ```
    Fix failures before continuing.

</requirements>

<constraints>
- Controller remains the single writer to vault. Executor never writes git directly — it publishes commands on Kafka.
- `AtomicReadModifyWriteAndCommitPush` MUST run the read, modify, write, and commit under a single mutex acquisition — no other git operation can interleave.
- Do NOT introduce a new mutex. Use the existing `mu sync.Mutex` in the gitclient struct.
- The file-finding WalkDir logic from `result_writer.go` must be extracted into a shared helper (do not duplicate). Export it as `FindTaskFilePath` if cross-package access is needed.
- `applyRetryCounter` in `result_writer.go` is NOT modified in this prompt — the retry_count path stays untouched until prompt 3.
- Cap escalation logic: only trigger when `cmd.Field == "trigger_count"` AND `newVal >= frontmatter.MaxTriggers()`. Do NOT escalate for arbitrary field increments.
- `Delta` sign: `cmd.Delta` may be any int. Negative deltas are allowed (they decrement). Do NOT reject them. Cap escalation only fires on `newVal >= MaxTriggers()`, so decrements cannot trigger escalation. The spec only uses `+1` today; decrement support is for forward compatibility and has zero extra cost.
- `phase: human_review` is set IN THE SAME ATOMIC WRITE as the trigger_count increment (same `modifyFn` call, same file write) — not as a separate command.
- Do NOT touch `task/executor/`, `prompt/`, `lib/`, or `agent/claude/` in this prompt.
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`.
- All existing tests must pass (including the result_writer tests after the refactor in step 3).
- Do NOT commit — dark-factory handles git.
- `cd task/controller && make precommit` must exit 0.
</constraints>

<verification>
Verify `AtomicReadModifyWriteAndCommitPush` is in the interface:
```bash
grep -n "AtomicReadModifyWriteAndCommitPush" task/controller/pkg/gitclient/git_client.go
```
Must show both the interface method and the implementation.

Verify increment executor exists:
```bash
grep -n "NewIncrementFrontmatterExecutor\|IncrementFrontmatterCommandOperation" task/controller/pkg/command/task_increment_frontmatter_executor.go
```
Must show both.

Verify update executor exists:
```bash
grep -n "NewUpdateFrontmatterExecutor\|UpdateFrontmatterCommandOperation" task/controller/pkg/command/task_update_frontmatter_executor.go
```
Must show both.

Verify both are wired in factory:
```bash
grep -n "NewIncrementFrontmatterExecutor\|NewUpdateFrontmatterExecutor" task/controller/pkg/factory/factory.go
```
Must show both.

Verify shared file-finder helper:
```bash
grep -n "findTaskFilePath\|FindTaskFilePath" task/controller/pkg/result/result_writer.go
```
Must show the helper (and its call from WriteResult).

Verify phase escalation logic:
```bash
grep -n "human_review\|MaxTriggers\|trigger_count" task/controller/pkg/command/task_increment_frontmatter_executor.go
```
Must show the cap escalation.

Run all controller tests:
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
