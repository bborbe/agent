---
status: completed
spec: [019-human-readable-vault-task-paths]
summary: Updated create-task executor to derive vault task file path from Title field with WARN+UUID fallback on invalid title or path collision; added 5 new test cases; updated docs and CHANGELOG.
container: agent-099-spec-019-controller-title-path
dark-factory-version: v0.151.2-4-g3dc5753
created: "2026-05-07T16:04:09Z"
queued: "2026-05-07T16:23:49Z"
started: "2026-05-07T16:29:58Z"
completed: "2026-05-07T16:35:06Z"
branch: dark-factory/human-readable-vault-task-paths
---

<summary>
- Controller's `create-task` executor resolves the vault path from `cmd.Title` instead of `cmd.TaskIdentifier` when `Title` passes re-validation
- Re-validation is defense-in-depth: the executor calls `cmd.Validate(ctx)` which enforces the same cross-platform-safe rules as the sender; on failure it logs WARN and materializes the task under `tasks/{task_identifier}.md` instead — the task is never dropped
- Path-collision detection: if the title-derived path (`tasks/{title}.md`) already exists but its `task_identifier` frontmatter differs from `cmd.TaskIdentifier`, the executor logs WARN and falls back to the UUID path; the original file is unchanged
- Idempotency preserved: if the resolved path (title or UUID) exists and its `task_identifier` matches, the executor returns nil without writing
- Existing UUID-named files are not renamed or touched by this change
- Update-frontmatter and increment-frontmatter executors still use `FindTaskFilePath` (UUID-based lookup) — the readable filename is set once at create time only
- `docs/task-flow-and-failure-semantics.md` updated with the WARN + UUID-fallback contract for invalid `Title` and for path collisions
- `make precommit` clean in `agent/task/controller`
</summary>

<objective>
Update the `create-task` executor in `task/controller/pkg/command/` to derive the vault task file path from the new `Title` field of `CreateTaskCommand`, with WARN + UUID fallback on invalid title or path collision. After this prompt, newly created vault task files land at human-readable paths (`tasks/{title}.md`) while the system guarantees no task is ever dropped. This is prompt 2 of 2 for spec-019; prompt 1 has shipped the `Title` field, `Validate` method, and sender helper in `agent/lib`.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, Counterfeiter fakes, ≥80% coverage

**Prerequisite:** Prompt 1 (spec-019 lib) has shipped. `lib.CreateTaskCommand` now has a `Title string` field and a `Validate(ctx)` method. Verify before editing:
```bash
grep -n "Title\b" lib/agent_task-commands.go
grep -n "func (cmd CreateTaskCommand) Validate" lib/agent_create-task-command.go
```
If either grep returns empty, STOP and report `status: failed` with message "lib Title field and Validate method not yet deployed (prompt 1 of spec-019)".

**Key files to read in full before editing:**

- `task/controller/pkg/command/task_create_task_executor.go` — current implementation. Derives path as `filepath.Join(taskDirPath, string(cmd.TaskIdentifier)+".md")`. The idempotency check uses `os.Stat(absPath)`. Both must be replaced with the new title-aware logic.

- `task/controller/pkg/command/task_create_task_executor_test.go` — current tests. Must be extended with title-path, WARN+fallback, and collision cases without breaking existing cases.

- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — reference for file content parsing (`parseTaskFrontmatter`, `marshalFileContent`). The collision check reads the existing file's frontmatter to find `task_identifier`.

- `docs/task-flow-and-failure-semantics.md` — must be updated with a new section documenting the WARN + UUID-fallback contract.

Run before editing:
```bash
grep -n "absPath\|os.Stat\|TaskIdentifier\|Title" task/controller/pkg/command/task_create_task_executor.go
grep -n "parseTaskFrontmatter\|marshalFileContent" task/controller/pkg/command/
```
</context>

<requirements>

1. **Update `task/controller/pkg/command/task_create_task_executor.go`**

   Replace the path-resolution and idempotency section with title-aware logic. After the existing frontmatter validation (assignee/status checks), insert:

   ```go
   taskDirPath := filepath.Join(gitClient.Path(), taskDir)
   absPath := resolveCreateTaskPath(ctx, taskDirPath, cmd)
   ```

   Then add the helper function `resolveCreateTaskPath` in the same file:

   ```go
   // resolveCreateTaskPath returns the absolute path where the task file should be written.
   // If cmd.Title passes validation and the title-derived path is unoccupied (or occupied by
   // the same task), the title path is returned. Otherwise a WARN is logged and the UUID path
   // is returned as fallback so the task is always materialized.
   func resolveCreateTaskPath(ctx context.Context, taskDirPath string, cmd lib.CreateTaskCommand) string {
       uuidPath := filepath.Join(taskDirPath, string(cmd.TaskIdentifier)+".md")

       // Re-validate the command (defense-in-depth: sender may have been bypassed).
       if err := cmd.Validate(ctx); err != nil {
           glog.Warningf(
               "create-task: Title validation failed for task %s (%v); falling back to UUID path",
               cmd.TaskIdentifier, err,
           )
           return uuidPath
       }

       titlePath := filepath.Join(taskDirPath, cmd.Title+".md")

       // Check if a file already exists at the title-derived path.
       existing, err := os.ReadFile(titlePath) // #nosec G304 -- path built from validated Title within taskDirPath
       if err != nil {
           if os.IsNotExist(err) {
               // Title path is free — use it.
               return titlePath
           }
           // Unexpected read error: fall back to UUID path and log.
           glog.Warningf(
               "create-task: could not read %s (%v); falling back to UUID path for task %s",
               titlePath, err, cmd.TaskIdentifier,
           )
           return uuidPath
       }

       // File exists at title path — extract + parse frontmatter to check task_identifier.
       frontmatterStr, extractErr := result.ExtractFrontmatter(ctx, existing)
       if extractErr != nil {
           glog.Warningf(
               "create-task: could not extract frontmatter at %s (%v); treating as collision, falling back to UUID path for task %s",
               titlePath, extractErr, cmd.TaskIdentifier,
           )
           return uuidPath
       }
       fm, parseErr := parseTaskFrontmatter(frontmatterStr)
       if parseErr != nil {
           glog.Warningf(
               "create-task: could not parse frontmatter at %s (%v); treating as collision, falling back to UUID path for task %s",
               titlePath, parseErr, cmd.TaskIdentifier,
           )
           return uuidPath
       }
       existingID, _ := fm.String("task_identifier")
       if lib.TaskIdentifier(existingID) == cmd.TaskIdentifier {
           // Same task owns this file — idempotent.
           return titlePath
       }
       // Different task_identifier at the title path — collision.
       glog.Warningf(
           "create-task: title path %s is already occupied by task %q (current task: %s); falling back to UUID path",
           titlePath, existingID, cmd.TaskIdentifier,
       )
       return uuidPath
   }
   ```

   Update the main handler body to use `absPath` from `resolveCreateTaskPath` for both the idempotency check and the write call:

   ```go
   // Idempotency check.
   if _, err := os.Stat(absPath); err == nil {
       glog.Infof(
           "create-task: task file already exists at %s for %s, skipping (idempotent)",
           absPath, cmd.TaskIdentifier,
       )
       return nil, nil, nil
   }

   content, err := buildCreateTaskContent(ctx, cmd)
   if err != nil {
       return nil, nil, errors.Wrapf(ctx, err, "build task file content for %s", cmd.TaskIdentifier)
   }
   if err := gitClient.AtomicWriteAndCommitPush(
       ctx,
       absPath,
       content,
       "[agent-task-controller] create task "+string(cmd.TaskIdentifier),
   ); err != nil {
       return nil, nil, errors.Wrapf(ctx, err, "atomic write and push for task %s", cmd.TaskIdentifier)
   }
   glog.V(2).Infof("create-task: created task file at %s for %s", absPath, cmd.TaskIdentifier)
   return nil, nil, nil
   ```

   Remove the old `taskDirPath`/`absPath` lines that used `string(cmd.TaskIdentifier)+".md"` directly.

   **`parseTaskFrontmatter` and `result.ExtractFrontmatter` are existing package-level functions** — confirm signatures before use:
   ```bash
   grep -n "func parseTaskFrontmatter" task/controller/pkg/command/task_increment_frontmatter_executor.go
   grep -n "func ExtractFrontmatter" task/controller/pkg/result/result_writer.go
   ```
   Expected signatures:
   - `parseTaskFrontmatter(frontmatterStr string) (lib.TaskFrontmatter, error)` — at `task_increment_frontmatter_executor.go:118`. Takes a frontmatter STRING (already extracted between `---` delimiters), NO `ctx`, NOT raw bytes.
   - `ExtractFrontmatter(ctx context.Context, content []byte) (string, error)` — at `pkg/result/result_writer.go:239`. Takes raw file bytes, returns the YAML frontmatter string between the first two `---` delimiters.
   The two-step pattern is already used by `task_increment_frontmatter_executor.go:96-104` and `task_update_frontmatter_executor.go:100`. Do NOT duplicate — call the existing functions.

   **New import required**:
   ```go
   import "github.com/bborbe/agent/task/controller/pkg/result"
   ```
   Verify by grep:
   ```bash
   grep -n "task/controller/pkg/result" task/controller/pkg/command/task_create_task_executor.go
   ```
   The import may already be present (used elsewhere in the file); add only if absent.

   **`fm.String("task_identifier")` return type:** returns `(string, bool)`. Use:
   ```go
   existingID, _ := fm.String("task_identifier")
   ```

2. **Update `task/controller/pkg/command/task_create_task_executor_test.go`**

   Add the following test cases to the existing `Describe("HandleCommand", ...)` block (after the existing cases):

   a. **Valid title → file written at `tasks/{title}.md`**
      ```go
      Context("valid title", func() {
          It("writes the task file at tasks/{title}.md", func() {
              taskID := lib.TaskIdentifier("uuid-1234")
              cmdObj := buildCmdObj(lib.CreateTaskCommand{
                  TaskIdentifier: taskID,
                  Title:          "My Feature Task",
                  Frontmatter: lib.TaskFrontmatter{
                      "assignee": "claude",
                      "status":   "todo",
                  },
                  Body: "Task description.\n",
              })
              _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
              Expect(err).NotTo(HaveOccurred())
              Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
              _, absPath, _, _ := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)
              Expect(absPath).To(HaveSuffix("My Feature Task.md"))
              Expect(absPath).NotTo(ContainSubstring(string(taskID)))
          })
      })
      ```

   b. **Invalid title → WARN + UUID fallback**
      ```go
      Context("invalid title (contains forbidden char)", func() {
          It("logs WARN and writes the task file at tasks/{task_identifier}.md", func() {
              taskID := lib.TaskIdentifier("uuid-5678")
              cmdObj := buildCmdObj(lib.CreateTaskCommand{
                  TaskIdentifier: taskID,
                  Title:          "bad/title",
                  Frontmatter: lib.TaskFrontmatter{
                      "assignee": "claude",
                      "status":   "todo",
                  },
              })
              _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
              Expect(err).NotTo(HaveOccurred())
              Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
              _, absPath, _, _ := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)
              Expect(absPath).To(HaveSuffix(string(taskID) + ".md"))
          })
      })
      ```

   c. **Empty title → WARN + UUID fallback**
      ```go
      Context("empty title", func() {
          It("logs WARN and writes the task file at tasks/{task_identifier}.md", func() {
              taskID := lib.TaskIdentifier("uuid-empty-title")
              cmdObj := buildCmdObj(lib.CreateTaskCommand{
                  TaskIdentifier: taskID,
                  Title:          "",
                  Frontmatter: lib.TaskFrontmatter{
                      "assignee": "claude",
                      "status":   "todo",
                  },
              })
              _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
              Expect(err).NotTo(HaveOccurred())
              Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
              _, absPath, _, _ := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)
              Expect(absPath).To(HaveSuffix(string(taskID) + ".md"))
          })
      })
      ```

   d. **Title collision → WARN + UUID fallback; existing file unchanged**
      ```go
      Context("title path occupied by a different task", func() {
          It("falls back to UUID path; existing file is unchanged", func() {
              // Pre-create the title path with a different task_identifier.
              titlePath := filepath.Join(tmpDir, taskDir, "My Colliding Task.md")
              originalContent := []byte("---\ntask_identifier: other-task-id\nassignee: alice\nstatus: todo\n---\n")
              Expect(os.WriteFile(titlePath, originalContent, 0600)).To(Succeed())

              taskID := lib.TaskIdentifier("new-task-id")
              cmdObj := buildCmdObj(lib.CreateTaskCommand{
                  TaskIdentifier: taskID,
                  Title:          "My Colliding Task",
                  Frontmatter: lib.TaskFrontmatter{
                      "assignee": "claude",
                      "status":   "todo",
                  },
              })
              _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
              Expect(err).NotTo(HaveOccurred())

              // New task written at UUID path.
              Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
              _, absPath, _, _ := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)
              Expect(absPath).To(HaveSuffix(string(taskID) + ".md"))

              // Original file unchanged.
              Expect(os.ReadFile(titlePath)).To(Equal(originalContent))
          })
      })
      ```

   e. **Idempotency with title path: same task already at title path → no write**
      ```go
      Context("title path already occupied by the same task (idempotent)", func() {
          It("returns nil without calling AtomicWriteAndCommitPush", func() {
              taskID := lib.TaskIdentifier("same-task-id")
              titlePath := filepath.Join(tmpDir, taskDir, "Existing Title.md")
              Expect(os.WriteFile(titlePath, []byte("---\ntask_identifier: same-task-id\n---\n"), 0600)).To(Succeed())

              cmdObj := buildCmdObj(lib.CreateTaskCommand{
                  TaskIdentifier: taskID,
                  Title:          "Existing Title",
                  Frontmatter: lib.TaskFrontmatter{
                      "assignee": "claude",
                      "status":   "todo",
                  },
              })
              _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
              Expect(err).NotTo(HaveOccurred())
              Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(0))
          })
      })
      ```

   Update the existing **"success: new file created"** test to include `Title: "New Task ABC"` in the `CreateTaskCommand` literal and assert `absPath` ends with `"New Task ABC.md"` instead of `string(taskID)+".md"`.

   Update the existing **"file already exists (idempotency)"** test by setting `Title: "existing-task"` so the title-derived path matches the pre-existing fixture file (`existing-task.md`). The executor's title-path probe finds the file, sees the matching `task_identifier`, and returns the title path — preserving the original test's idempotency intent without branching ambiguity. Do NOT delete the test or rewrite it as a UUID-fallback test; the simple `Title: "existing-task"` change is sufficient.

3. **Update `docs/task-flow-and-failure-semantics.md`**

   Add a new section **`## Create-Task Path Resolution`** (or append to an existing relevant section). Insert before the `## References` section at the bottom:

   ```markdown
   ## Create-Task Path Resolution (spec-019)

   When the controller processes a `create-task` command it resolves the vault path as follows:

   1. **Title valid + title path unoccupied** → write `tasks/{title}.md`
   2. **Title valid + title path occupied by the same `task_identifier`** → no-op (idempotent)
   3. **Title valid + title path occupied by a different `task_identifier`** → WARN + fall back to `tasks/{task_identifier}.md`
   4. **Title fails validation (any rule)** → WARN + fall back to `tasks/{task_identifier}.md`

   In cases 3 and 4 the task is always materialized under its UUID path — the system never drops the task. The WARN log surfaces the anomaly to operators.

   **UUID fallback is permanent contract, not a migration affordance.** Producers that bypass the sender's `Validate`-before-publish (e.g. anyone with Kafka write access publishing a raw command) will trigger the fallback; the WARN log is the alerting mechanism. The existing file at `tasks/{task_identifier}.md` (if any) is the idempotency record.
   ```

4. **Update `CHANGELOG.md` at repo root**

   This repo's `CHANGELOG.md` uses release-versioned headers (`## v0.54.17`, `## v0.54.16`, …) — there is NO `## Unreleased` section by convention. Prepend a new `## Unreleased` section at the top of the file (above the most recent version header) and add the entry there. The release tooling will rename `## Unreleased` → `## vX.Y.Z` on the next release.

   Final shape (after edit):

   ```markdown
   # Changelog

   ## Unreleased

   - feat(task/controller): create-task executor now writes vault task files at `tasks/{title}.md`; re-validates `Title` on receive with WARN + UUID-path fallback on failure or path collision

   ## v0.54.17

   - fix(ci): point `actions/setup-go` …
   ```

   If a `## Unreleased` section already exists (created by prompt 1 of this spec), append the entry there instead of creating a new section.

5. **Run tests iteratively**

   ```bash
   cd task/controller && make test
   ```

   Fix any failures before proceeding.

   ```bash
   cd task/controller && make precommit
   ```

   Must exit 0.

</requirements>

<constraints>
- The WARN + UUID fallback is permanent contract — not a migration affordance. Document it as such.
- Path collision check: read existing file at `tasks/{title}.md`, parse frontmatter, compare `task_identifier`. Only if `task_identifier` differs → WARN + UUID fallback.
- `parseTaskFrontmatter` is a package-level function in `task/controller/pkg/command/`. Do NOT duplicate it — call it. Grep first to confirm its signature.
- `fm.String("task_identifier")` returns `(string, bool)` — use the comma-ok form.
- Existing frontmatter validation (assignee/status checks) remains unchanged — abort on those failures (not WARN+fallback).
- `cmd.Validate(ctx)` validates both Title and Body; on any failure the executor falls back to UUID path (behavior is the same for both bad-title and oversized-body).
- Update-frontmatter and increment-frontmatter executors use `FindTaskFilePath` — do NOT modify them.
- Do NOT rename or modify existing UUID-named vault files.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`
- Logging: `glog.Warningf` for fallback events; `glog.V(2).Infof` for success
- Ginkgo v2 + Gomega; `FakeGitClient` from `task/controller/mocks/`; external test package `command_test`
- All existing tests must still pass after changes
- Do NOT commit — dark-factory handles git
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>

Verify `resolveCreateTaskPath` helper exists:
```bash
grep -n "func resolveCreateTaskPath" task/controller/pkg/command/task_create_task_executor.go
```
Must show the function.

Verify UUID path is no longer hardcoded in the main handler. The string `string(cmd.TaskIdentifier)+".md"` may still appear inside `resolveCreateTaskPath` (the UUID-fallback construction); it must NOT appear in the main `HandleCommand` body.

```bash
# Find where ".md" appears with TaskIdentifier; expect it ONLY inside resolveCreateTaskPath
awk '/^func /{fn=$2} /TaskIdentifier.*\.md|task_identifier.*\.md/{print fn":"$0}' task/controller/pkg/command/task_create_task_executor.go
```
Expected: matches only inside `resolveCreateTaskPath` (e.g. `uuidPath := filepath.Join(...)`); zero matches inside the `HandleCommand` (or whichever the main handler function is named).

Verify title path usage:
```bash
grep -n "cmd.Title" task/controller/pkg/command/task_create_task_executor.go
```
Must show `cmd.Title+".md"` inside `resolveCreateTaskPath`.

Verify WARN logging on fallback:
```bash
grep -n "Warningf" task/controller/pkg/command/task_create_task_executor.go
```
Must show at least two `glog.Warningf` calls (title validation failure + collision).

Verify collision test exists:
```bash
grep -n "collision\|occupied by a different\|Colliding" task/controller/pkg/command/task_create_task_executor_test.go
```
Must show the collision test case.

Verify title test exists:
```bash
grep -n "title.*\.md\|Title.*My Feature" task/controller/pkg/command/task_create_task_executor_test.go
```
Must show the valid-title test asserting `HaveSuffix("My Feature Task.md")`.

Verify docs updated:
```bash
grep -n "Create-Task Path Resolution\|UUID-path fallback\|WARN.*fallback" docs/task-flow-and-failure-semantics.md
```
Must show the new section.

Run tests:
```bash
cd task/controller && go test -v ./pkg/command/... -run CreateTask
```
Must exit 0 with all cases PASS.

Run full precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

Verify CHANGELOG updated:
```bash
grep -n "title.*path\|Title.*fallback\|human-readable" CHANGELOG.md
```
Must show the Unreleased entry.

</verification>
