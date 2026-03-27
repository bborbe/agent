---
status: approved
created: "2026-03-27T14:32:02Z"
queued: "2026-03-27T14:32:02Z"
---

<summary>
- All error returns in task/controller include stack traces and operation context
- Bare return err statements are replaced with errors.Wrapf calls
- Debugging production errors becomes easier with descriptive wrap messages
- Uses the existing github.com/bborbe/errors package already used throughout the project
- Three locations fixed: two in main.go Run method, one in sync_loop.go Run method
</summary>

<objective>
Replace all bare `return err` statements in task/controller with `errors.Wrapf(ctx, err, ...)` calls that add meaningful operation context for debugging.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `task/controller/main.go` — `application.Run()` method has two bare `return err` after `gitClient.EnsureCloned` and `libkafka.NewSyncProducer`
- `task/controller/pkg/sync/sync_loop.go` — `syncLoop.Run()` has one bare `return err` after `s.processResult`

The project already uses `github.com/bborbe/errors` for error wrapping consistently in `sync_loop.go` (processResult), `git_client.go`, and `task_publisher.go`. These three bare returns are the only exceptions.
</context>

<requirements>
1. **Modify `task/controller/main.go`:**

   a. Add `"github.com/bborbe/errors"` to the import block (not currently imported).

   b. Replace bare `return err` after `gitClient.EnsureCloned(ctx)`:

   **Before:**
   ```go
   if err := gitClient.EnsureCloned(ctx); err != nil {
       return err
   }
   ```

   **After:**
   ```go
   if err := gitClient.EnsureCloned(ctx); err != nil {
       return errors.Wrapf(ctx, err, "ensure git clone")
   }
   ```

   c. Replace bare `return err` after `libkafka.NewSyncProducer(...)`:

   **Before:**
   ```go
   if err != nil {
       return err
   }
   ```

   **After:**
   ```go
   if err != nil {
       return errors.Wrapf(ctx, err, "create kafka sync producer")
   }
   ```

2. **Modify `task/controller/pkg/sync/sync_loop.go`:**

   Replace bare `return err` after `s.processResult(ctx, result)`:

   **Before:**
   ```go
   if err := s.processResult(ctx, result); err != nil {
       return err
   }
   ```

   **After:**
   ```go
   if err := s.processResult(ctx, result); err != nil {
       return errors.Wrapf(ctx, err, "process scan result")
   }
   ```

   Note: `github.com/bborbe/errors` is already imported in this file.

3. Do NOT change any existing `errors.Wrapf` or `errors.Errorf` calls.

4. Do NOT modify test files.

5. Run `make test` in `task/controller/` to verify all tests pass.

6. Run `make precommit` in `task/controller/` to verify full precommit passes.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify test files
- Do NOT change existing error wrapping calls
- Use `github.com/bborbe/errors` — never `fmt.Errorf`
- Wrap messages should describe the failed operation concisely (e.g., "ensure git clone", not "failed to ensure git clone was successful")
</constraints>

<verification>
Run `make precommit` in `task/controller/` — must pass with exit code 0.

Verify no bare return err remains:
```bash
grep -rn 'return err$' task/controller/main.go task/controller/pkg/sync/sync_loop.go
```
Expected: 0 matches

Verify errors import added to main.go:
```bash
grep 'bborbe/errors' task/controller/main.go
```
Expected: 1 match
</verification>
