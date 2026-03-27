---
status: failed
container: agent-005-add-trigger-endpoint
dark-factory-version: v0.67.3-dirty
created: "2026-03-27T10:00:00Z"
queued: "2026-03-27T12:58:22Z"
started: "2026-03-27T13:24:08Z"
completed: "2026-03-27T13:29:27Z"
---

<summary>
- The task controller exposes a /trigger HTTP endpoint that forces an immediate scan cycle
- Hitting /trigger wakes up the existing scanner loop without creating a second scanner instance
- The regular poll-interval ticker continues to work unchanged alongside manual triggers
- A nil trigger channel is safe (scanner falls back to ticker-only mode)
- The scanner, factory, and main wiring are updated; existing endpoints are unaffected
</summary>

<objective>
Add a `/trigger` HTTP endpoint to agent-task-controller that forces an immediate vault scan cycle. This lets operators and other services trigger a sync on demand instead of waiting for the next poll tick.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `task/controller/main.go` — application struct, `Run()`, `createHTTPServer()`, `service.Run()` call
- `task/controller/pkg/scanner/vault_scanner.go` — `VaultScanner` interface, `vaultScanner` struct, `Run` method with ticker loop
- `task/controller/pkg/scanner/vault_scanner_test.go` — existing tests including `testGitClient`, `runCycle` tests
- `task/controller/pkg/sync/sync_loop.go` — `SyncLoop` interface, `NewSyncLoop` constructor
- `task/controller/pkg/sync/sync_loop_test.go` — existing sync loop tests
- `task/controller/pkg/factory/factory.go` — `CreateSyncLoop` factory function
- `task/controller/pkg/factory/factory_test.go` — factory test

Library APIs:
- `run.NewTrigger() run.Trigger` — creates a trigger (implements `run.Fire` and `run.Done`)
- `run.Fire` interface — `Fire()` method
- `run.Done` interface — `Done() <-chan struct{}` method
</context>

<requirements>
1. Modify `task/controller/pkg/scanner/vault_scanner.go`:

   a. Change the `NewVaultScanner` constructor to accept an additional `trigger <-chan struct{}` parameter:
   ```go
   func NewVaultScanner(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       trigger <-chan struct{},
   ) VaultScanner
   ```

   b. Add a `trigger <-chan struct{}` field to the `vaultScanner` struct.

   c. Modify the `Run` method to also listen on the trigger channel. Change the existing select from:
   ```go
   select {
   case <-ctx.Done():
       return nil
   case <-ticker.C:
       v.runCycle(ctx, results)
   }
   ```
   to:
   ```go
   select {
   case <-ctx.Done():
       return nil
   case <-ticker.C:
       v.runCycle(ctx, results)
   case <-v.trigger:
       v.runCycle(ctx, results)
   }
   ```
   A nil trigger channel never selects — this is correct Go behavior and means the scanner falls back to ticker-only mode.

2. Modify `task/controller/pkg/scanner/vault_scanner_test.go`:

   a. Update the `vaultScanner` struct literal in `BeforeEach` to include the new `trigger` field. Use a buffered channel:
   ```go
   triggerCh = make(chan struct{}, 1)
   s = &vaultScanner{
       gitClient:    fakeGit,
       taskDir:      taskDir,
       pollInterval: time.Second,
       hashes:       make(map[string][32]byte),
       trigger:      triggerCh,
   }
   ```

   b. Update all `NewVaultScanner` calls in tests to pass a nil trigger channel:
   ```go
   vs := NewVaultScanner(fakeGit, taskDir, time.Hour, nil)
   ```

   c. Add a new test case under the `Run` Describe block:
   ```go
   It("runs a cycle immediately when trigger fires", func() {
       content := "---\nstatus: todo\nassignee: claude\n---\n# Triggered task"
       absPath := filepath.Join(tmpDir, taskDir, "triggered.md")
       Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

       triggerCh := make(chan struct{}, 1)
       vs := NewVaultScanner(fakeGit, taskDir, time.Hour, triggerCh)
       runResults := make(chan ScanResult, 1)
       runCtx, cancel := context.WithCancel(ctx)
       defer cancel()

       done := make(chan error, 1)
       go func() {
           done <- vs.Run(runCtx, runResults)
       }()

       triggerCh <- struct{}{}
       var result ScanResult
       Eventually(runResults, time.Second).Should(Receive(&result))
       Expect(result.Changed).To(HaveLen(1))
       Expect(string(result.Changed[0].Name)).To(Equal("triggered"))

       cancel()
       Eventually(done, time.Second).Should(Receive(BeNil()))
   })
   ```

3. Modify `task/controller/pkg/factory/factory.go`:

   Change `CreateSyncLoop` to accept and pass through a trigger channel:
   ```go
   func CreateSyncLoop(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       eventObjectSender cdb.EventObjectSender,
       trigger <-chan struct{},
   ) pkgsync.SyncLoop {
       return pkgsync.NewSyncLoop(
           scanner.NewVaultScanner(
               gitClient,
               taskDir,
               pollInterval,
               trigger,
           ),
           publisher.NewTaskPublisher(
               eventObjectSender,
               lib.TaskV1SchemaID,
           ),
       )
   }
   ```
   No other factory functions needed — the trigger wakes the existing scanner, not a new one.

4. Modify `task/controller/pkg/factory/factory_test.go`:

   No changes needed. The existing test checks `Expect(factory.CreateSyncLoop).NotTo(BeNil())` which validates the function value exists — this compiles regardless of parameter count.

5. Modify `task/controller/main.go`:

   a. In `Run()`, create a trigger before `CreateSyncLoop`:
   ```go
   trigger := run.NewTrigger()
   ```

   b. Pass `trigger.Done()` to `CreateSyncLoop`:
   ```go
   syncLoop := factory.CreateSyncLoop(
       gitClient,
       a.TaskDir,
       a.PollInterval,
       eventObjectSender,
       trigger.Done(),
   )
   ```

   c. Change `createHTTPServer` signature to accept `run.Fire`:
   ```go
   func (a *application) createHTTPServer(trigger run.Fire) run.Func {
   ```

   d. Add the `/trigger` route using an inline handler that fires the trigger:
   ```go
   router.Path("/trigger").HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
       trigger.Fire()
       _, _ = resp.Write([]byte("trigger fired"))
   })
   ```
   This wakes the existing scanner loop via the trigger channel — no background goroutine needed.

   e. Update the `Run()` method to pass the trigger:
   ```go
   return service.Run(
       ctx,
       a.createHTTPServer(trigger),
       syncLoop.Run,
   )
   ```

   f. Add `"net/http"` and `"github.com/bborbe/run"` to imports if not already present.

6. Run `make generate` in `task/controller/` to regenerate mocks.

7. Run `make test` in `task/controller/` to verify all tests pass.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` types — these are frozen
- Do NOT change the VaultScanner interface signature — only the constructor and private struct change
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Existing HTTP endpoints (healthz, readiness, metrics, setloglevel) must continue to work unchanged
- Existing tests must still pass
- The trigger must wake up the SAME scanner instance, not create a second one
- A nil trigger channel must be safe (scanner falls back to ticker-only mode)
- `make test` must pass before declaring done
</constraints>

<verification>
Run `make test` in `task/controller/` — must pass.
Run `make precommit` in `task/controller/` — must pass with exit code 0.
Verify manually: `grep -n "/trigger" task/controller/main.go` shows the new route.
Verify manually: `grep -n "trigger" task/controller/pkg/scanner/vault_scanner.go` shows the trigger channel in select.
</verification>
