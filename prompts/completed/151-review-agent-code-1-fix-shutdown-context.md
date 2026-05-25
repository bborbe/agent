---
status: completed
summary: Replaced context.Background() with signal.NotifyContext in agent/code main.go and cmd/run-task/main.go to enable graceful shutdown on SIGTERM/SIGINT
container: agent-exec-151-review-agent-code-1-fix-shutdown-context
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-24T12:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T21:00:30Z"
completed: "2026-05-25T21:02:13Z"
---

<summary>
- Replaces `context.Background()` with `signal.NotifyContext` in both entry points
- Enables graceful shutdown on SIGTERM/SIGINT signals
- Both `main.go` and `cmd/run-task/main.go` now propagate OS signals through the service lifecycle
</summary>

<objective>
Fix graceful shutdown propagation in both agent-code entry points. Both `main.go` and `cmd/run-task/main.go` pass `context.Background()` to `service.Main`, which prevents the service from responding to OS signals (SIGTERM/SIGINT). After this change, cancellation signals flow through the application context and enable clean shutdown.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- agent/code/main.go — entry point at line 45-48
- agent/code/cmd/run-task/main.go — entry point at line 25-28
</context>

<requirements>

## 1. Fix main.go entry point

Read `agent/code/main.go` before editing.

Replace the `main` function (lines 45-48):

Before:
```go
func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}
```

After:
```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.SIGTERM, os.SIGINT)
	defer stop()
	app := &application{}
	os.Exit(service.Main(ctx, app, &app.SentryDSN, &app.SentryProxy))
}
```

Add `"os/signal"` and `"os"` to the imports if not already present (they should already be there).

## 2. Fix cmd/run-task/main.go entry point

Read `agent/code/cmd/run-task/main.go` before editing.

Replace the `main` function (lines 25-28):

Before:
```go
func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}
```

After:
```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.SIGTERM, os.SIGINT)
	defer stop()
	app := &application{}
	os.Exit(service.Main(ctx, app, &app.SentryDSN, &app.SentryProxy))
}
```

Add `"os/signal"` to the imports if not already present.

## 3. Run make test

```bash
cd agent/code && make test
```
Expected: exit 0.

</requirements>

<constraints>
- Only change files in `agent/code/`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors`, never `fmt.Errorf`
</constraints>

<verification>
cd agent/code && make precommit
</verification>
