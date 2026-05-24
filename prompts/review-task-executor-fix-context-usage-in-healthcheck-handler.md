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
### 1. Update NewHealthcheckTriggerHandler signature

Remove the `ctx context.Context` parameter:

```go
func NewHealthcheckTriggerHandler(
    runner probe.HealthcheckRunner,
) http.Handler {
    return libhttp.NewBackgroundRunHandler(runner.Run)
}
```

### 2. Update main.go call site

Remove the ctx argument from the NewHealthcheckTriggerHandler call in main.go around line 148:

```go
handler.NewHealthcheckTriggerHandler(runner)  // no ctx parameter
```

### 3. Verify the change compiles

```bash
cd task/executor && make build
```

### 4. Run tests

```bash
cd task/executor && make test
```
</requirements>

<constraints>
- Only change files in `task/executor/`
- Do NOT commit — dark-factory handles git
- Verify libhttp.NewBackgroundRunHandler accepts run.Func without ctx
</constraints>

<verification>
cd task/executor && make precommit
</verification>
