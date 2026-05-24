---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Removes ctx parameter from NewHealthcheckTriggerHandler
- Passes runner.Run directly to NewBackgroundRunHandler
- Fixes context lifecycle bug where handler context was cancelled before server finished
</summary>

<objective>
NewHealthcheckTriggerHandler at healthcheck_trigger_handler.go:23 accepts a ctx parameter that is passed to libhttp.NewBackgroundRunHandler. This context gets cancelled when the caller (the Run goroutine in main.go) exits, which happens before the HTTP server finishes listening. After this fix, the handler derives its own context from each incoming HTTP request.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/handler/healthcheck_trigger_handler.go
- task/executor/main.go (~line 148, where handler is created)
</context>

<requirements>
### 1. Confirm libhttp.NewBackgroundRunHandler signature

`libhttp.NewBackgroundRunHandler(ctx context.Context, runFunc run.Func) http.Handler` REQUIRES a context. The signature cannot be changed without modifying `bborbe/http`. The fix must pass a non-cancelled context, NOT remove the ctx parameter.

### 2. Update NewHealthcheckTriggerHandler

Keep the ctx parameter on `NewHealthcheckTriggerHandler` but document that the caller MUST pass a context whose lifetime matches the HTTP server, not a transient goroutine context. Or, more robust: drop the caller's ctx and use `context.Background()` inside the factory:

```go
func NewHealthcheckTriggerHandler(
    runner probe.HealthcheckRunner,
) http.Handler {
    return libhttp.NewBackgroundRunHandler(context.Background(), runner.Run)
}
```

The trigger should outlive any individual request and only stop when the process exits. `context.Background()` is appropriate here because graceful shutdown is handled by `libhttp.NewServer`'s built-in `Shutdown(ctx)` path.

### 3. Update main.go call site

Remove the ctx argument from the `NewHealthcheckTriggerHandler` call in `main.go` around line 148:

```go
handler.NewHealthcheckTriggerHandler(runner)  // no ctx parameter
```

### 4. Verify the change compiles

```bash
cd task/executor && make build
```

### 5. Run tests

```bash
cd task/executor && make test
```
</requirements>

<constraints>
- Only change files in `task/executor/`
- Do NOT commit — dark-factory handles git
- DO NOT attempt to call `libhttp.NewBackgroundRunHandler` without ctx — that signature does not exist
- The fix is to drop the caller's ctx and use `context.Background()` inside the factory
</constraints>

<verification>
cd task/executor && make precommit
</verification>
