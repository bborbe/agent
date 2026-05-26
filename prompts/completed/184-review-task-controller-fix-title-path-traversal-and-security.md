---
status: completed
summary: 'Three security fixes applied: Title path separator validation in resolveCreateTaskPath, context cancellation check before first HTTP attempt in Post/Delete retry loops, and io.LimitReader bounds on HTTP response reads in Get/List'
container: agent-exec-184-review-task-controller-fix-title-path-traversal-and-security
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T22:23:09Z"
started: "2026-05-26T06:25:21Z"
completed: "2026-05-26T06:28:42Z"
---

<summary>
- Validates Title field contains no path separators or traversal sequences before constructing file paths
- Adds context cancellation check at top of retry loop before first HTTP attempt
- Limits HTTP response body reads with io.LimitReader to prevent unbounded memory allocation
</summary>

<objective>
Three security-related issues in `pkg/gitrestclient/` and `pkg/command/`: (1) `task_create_task_executor.go` constructs a file path from `cmd.Title` without rejecting `/` or `\` characters — the `#nosec G304` annotation may be incorrect; (2) `git_rest_client.go` retry loop checks context cancellation only after the first failed attempt, so a pre-existing cancelled context blocks on HTTP for up to 30s; (3) `io.ReadAll` on HTTP responses has no size limit. After this fix, Title is validated before path use, the retry loop checks ctx before all I/O, and HTTP responses are bounded.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
- `task/controller/pkg/command/task_create_task_executor.go` (~line 95-135, `resolveCreateTaskPath`)
- `task/controller/pkg/gitrestclient/git_rest_client.go` (~line 128-145, Post retry loop; ~line 107, Get; ~line 238, List)
</context>

<requirements>

### Part A: Title path validation in task_create_task_executor.go

1. **In `resolveCreateTaskPath`**, before constructing `titlePath := filepath.Join(taskDirPath, cmd.Title+".md")`, add a validation check:

   ```go
   // Reject titles containing path separators to prevent path traversal.
   // cmd.Title has already passed cmd.Validate() which checks cross-platform safety,
   // but we add a defense-in-depth check here for path chars specifically.
   if strings.ContainsAny(cmd.Title, "/\\") {
       glog.Warningf(
           "create-task: Title %q contains path separator; falling back to UUID path",
           cmd.Title,
       )
       return uuidPath
   }
   ```

   Note: `strings` is already imported in this file. Add the check before the `os.ReadFile` call at line 114.

2. **Remove the `#nosec G304` comment** from the `os.ReadFile` call since the path is now validated, or keep it if the reviewer agrees the validation is sufficient defense-in-depth.

### Part B: Context cancellation check before first HTTP attempt in git_rest_client.go

3. **In the `Post` method's retry loop** at line 128, add a context check at the top of the loop body (before the HTTP attempt on `attempt == 0`):

   ```go
   select {
   case <-ctx.Done():
       return errors.Wrapf(ctx, ctx.Err(), "POST %s cancelled before attempt", relPath)
   default:
   }
   ```

   This should be the first thing inside the `for attempt := 0; attempt < 5; attempt++` loop, before the `if attempt > 0` backoff block.

4. **Apply the same fix to the `Delete` method's retry loop** at line 186.

### Part C: Bounded HTTP response body reads in git_rest_client.go

5. **In the `Get` method**, change:
   ```go
   body, err := io.ReadAll(resp.Body)
   ```
   To:
   ```go
   body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10 MiB max
   ```

6. **Apply the same fix to the `List` method** at line 238.

7. **Run tests:**
   ```bash
   cd task/controller && make test
   ```

8. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change files under `task/controller/pkg/command/` and `task/controller/pkg/gitrestclient/`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors`
</constraints>

<verification>
cd task/controller && make precommit
</verification>
