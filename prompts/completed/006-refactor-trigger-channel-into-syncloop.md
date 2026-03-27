---
status: completed
summary: Refactored trigger channel ownership into SyncLoop — SyncLoop owns the buffered channel internally, exposes Trigger() method, factory creates channel internally, main.go uses syncLoop.Trigger() instead of raw channel send.
container: agent-006-refactor-trigger-channel-into-syncloop
dark-factory-version: v0.67.9
created: "2026-03-27T14:01:31Z"
queued: "2026-03-27T14:32:01Z"
started: "2026-03-27T15:34:40Z"
completed: "2026-03-27T15:43:36Z"
---

<summary>
- SyncLoop owns the trigger channel internally instead of receiving it from the outside
- SyncLoop interface gains a Trigger() method that external callers use to request immediate scans
- Factory and scanner constructors no longer accept a trigger channel parameter
- The HTTP handler in main.go calls syncLoop.Trigger() instead of sending on a raw channel
- createHTTPServer accepts SyncLoop (or a trigger interface) instead of a raw channel
- All tests and mocks are updated to match the new signatures
</summary>

<objective>
Refactor the trigger channel out of the SyncLoop/VaultScanner constructor chain. SyncLoop should own the buffered trigger channel internally, expose a `Trigger()` method, and pass the channel to VaultScanner itself. This simplifies the API surface — callers trigger scans via a method call instead of managing raw channels.
</objective>

<context>
Read CLAUDE.md for project conventions.

Key files to read before making changes:
- `task/controller/pkg/sync/sync_loop.go` — `SyncLoop` interface, `NewSyncLoop` constructor, `syncLoop` struct
- `task/controller/pkg/sync/sync_loop_test.go` — sync loop tests using `mocks.FakeVaultScanner` and `mocks.FakeTaskPublisher`
- `task/controller/pkg/scanner/vault_scanner.go` — `VaultScanner` interface, `NewVaultScanner` constructor with `trigger <-chan struct{}` param, `vaultScanner` struct
- `task/controller/pkg/scanner/vault_scanner_test.go` — scanner tests, direct struct construction, trigger channel tests
- `task/controller/pkg/factory/factory.go` — `CreateSyncLoop` factory with `trigger <-chan struct{}` param
- `task/controller/pkg/factory/factory_test.go` — factory test (checks `CreateSyncLoop` is defined)
- `task/controller/main.go` — `application.Run()` creates trigger channel, passes to factory and `createHTTPServer`
- `task/controller/mocks/sync_loop.go` — generated counterfeiter mock for SyncLoop
</context>

<requirements>
1. **Modify `task/controller/pkg/sync/sync_loop.go`:**

   a. Add `Trigger()` method to the `SyncLoop` interface:
   ```go
   type SyncLoop interface {
       Run(ctx context.Context) error
       Trigger()
   }
   ```

   b. Add a `trigger chan struct{}` field to the `syncLoop` struct (bidirectional, since syncLoop both sends and passes it as receive-only to scanner).

   c. Change `NewSyncLoop` to accept an additional `trigger chan struct{}` parameter:
   ```go
   func NewSyncLoop(
       scanner scanner.VaultScanner,
       publisher publisher.TaskPublisher,
       trigger chan struct{},
   ) SyncLoop {
       return &syncLoop{
           scanner:   scanner,
           publisher: publisher,
           trigger:   trigger,
       }
   }
   ```

   d. Implement `Trigger()` on the `syncLoop` struct using the same non-blocking send pattern currently in `main.go`:
   ```go
   func (s *syncLoop) Trigger() {
       select {
       case s.trigger <- struct{}{}:
       default:
       }
   }
   ```

2. **Modify `task/controller/pkg/factory/factory.go`:**

   Remove the `trigger <-chan struct{}` parameter from `CreateSyncLoop`. The factory creates the buffered trigger channel internally and passes it to both `NewVaultScanner` and `NewSyncLoop`:

   **Before:**
   ```go
   func CreateSyncLoop(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       eventObjectSender cdb.EventObjectSender,
       trigger <-chan struct{},
   ) pkgsync.SyncLoop {
   ```

   **After:**
   ```go
   func CreateSyncLoop(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       eventObjectSender cdb.EventObjectSender,
   ) pkgsync.SyncLoop {
       trigger := make(chan struct{}, 1)
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
           trigger,
       )
   }
   ```

   Note: `scanner.NewVaultScanner` still accepts `trigger <-chan struct{}` — a `chan struct{}` is assignable to `<-chan struct{}`. The scanner's constructor and struct field are UNCHANGED.

3. **Modify `task/controller/main.go`:**

   a. Remove the `trigger := make(chan struct{}, 1)` line from `Run()`.

   b. Remove the `trigger` argument from the `factory.CreateSyncLoop` call:
   ```go
   syncLoop := factory.CreateSyncLoop(
       gitClient,
       a.TaskDir,
       a.PollInterval,
       eventObjectSender,
   )
   ```

   c. Change `createHTTPServer` signature — replace `trigger chan<- struct{}` with the SyncLoop:

   **Before:**
   ```go
   func (a *application) createHTTPServer(trigger chan<- struct{}) run.Func {
   ```

   **After:**
   ```go
   func (a *application) createHTTPServer(syncLoop pkgsync.SyncLoop) run.Func {
   ```

   Add the import for `pkgsync`:
   ```go
   pkgsync "github.com/bborbe/agent/task/controller/pkg/sync"
   ```

   d. Update the `/trigger` HTTP handler to call `syncLoop.Trigger()` instead of the raw channel send:

   **Before:**
   ```go
   router.Path("/trigger").HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
       select {
       case trigger <- struct{}{}:
       default:
       }
       glog.V(2).Infof("trigger fired via HTTP")
       _, _ = resp.Write([]byte("trigger fired"))
   })
   ```

   **After:**
   ```go
   router.Path("/trigger").HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
       syncLoop.Trigger()
       glog.V(2).Infof("trigger fired via HTTP")
       _, _ = resp.Write([]byte("trigger fired"))
   })
   ```

   e. Update the `createHTTPServer` call in `Run()`:

   **Before:**
   ```go
   return service.Run(
       ctx,
       syncLoop.Run,
       a.createHTTPServer(trigger),
   )
   ```

   **After:**
   ```go
   return service.Run(
       ctx,
       syncLoop.Run,
       a.createHTTPServer(syncLoop),
   )
   ```

4. **Modify `task/controller/pkg/sync/sync_loop_test.go`:**

   Update `NewSyncLoop` call to pass a trigger channel:

   **Before:**
   ```go
   syncLoop = pkgsync.NewSyncLoop(fakeScanner, fakePublisher)
   ```

   **After:**
   ```go
   syncLoop = pkgsync.NewSyncLoop(fakeScanner, fakePublisher, make(chan struct{}, 1))
   ```

   Add a test for the `Trigger()` method:
   ```go
   Describe("Trigger", func() {
       It("does not block when called", func() {
           // Trigger should be non-blocking even if nothing is consuming
           syncLoop.Trigger()
           syncLoop.Trigger() // second call should also not block (buffered channel, non-blocking send)
       })
   })
   ```

5. **Modify `task/controller/pkg/scanner/vault_scanner_test.go`:**

   No changes needed — the scanner tests already create and pass trigger channels directly to `NewVaultScanner`, and that constructor signature is unchanged.

   Verify by reading the test file: the `NewVaultScanner` calls in the test still pass `nil` or a `chan struct{}` as the trigger param, which remains correct.

6. **Regenerate mocks:**

   Run `make generate` in `task/controller/` to regenerate `task/controller/mocks/sync_loop.go`. The counterfeiter mock must now include the `Trigger()` method since it was added to the `SyncLoop` interface.

7. **Run `make test` in `task/controller/` to verify all tests pass.**

8. **Run `make precommit` in `task/controller/` to verify full precommit passes.**
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` types — these are frozen
- Do NOT change the `VaultScanner` interface — only `SyncLoop` interface changes
- Do NOT change `NewVaultScanner` constructor signature — it still accepts `trigger <-chan struct{}`
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Existing HTTP endpoints (healthz, readiness, metrics, setloglevel, trigger) must continue to work unchanged
- Existing tests must still pass
- `make precommit` must pass before declaring done
</constraints>

<verification>
Run `make precommit` in `task/controller/` — must pass with exit code 0.
Run `make test` in `task/controller/` — must pass.

Verify the trigger channel is no longer in main.go:
```bash
grep -c "make(chan struct{}" task/controller/main.go
```
Expected: 0

Verify SyncLoop interface has Trigger():
```bash
grep "Trigger()" task/controller/pkg/sync/sync_loop.go
```
Expected: appears in interface definition

Verify factory no longer accepts trigger param:
```bash
grep "trigger" task/controller/pkg/factory/factory.go
```
Expected: trigger appears only as internally created variable, not as function parameter

Verify createHTTPServer accepts SyncLoop:
```bash
grep "createHTTPServer" task/controller/main.go
```
Expected: signature includes `SyncLoop`, not `chan`
</verification>
