---
status: completed
summary: Verified libhttp.NewServer defaults (ReadTimeout=30s, WriteTimeout=30s, IdleTimeout=60s, ReadHeaderTimeout=10s, MaxHeaderBytes=1MB) are appropriate for task-executor HTTP API; added comment documenting intentional acceptance of defaults
container: agent-exec-164-review-task-executor-add-http-server-timeouts
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T22:31:53Z"
completed: "2026-05-25T22:35:36Z"
---

<summary>
- Verify `libhttp.NewServer` defaults match desired security posture (30s read/write, 60s idle, 1MB header)
- If shorter timeouts wanted, pass `func(o *libhttp.ServerOptions)` to override
- If defaults acceptable, document and close as no-op
- Mitigates slowloris attacks and connection exhaustion
</summary>

<objective>
Confirm HTTP server has sensible timeout configuration. The server is created via `libhttp.NewServer(addr, router)`, which already applies defaults (ReadTimeout 30s, WriteTimeout 30s, IdleTimeout 60s, MaxHeaderBytes 1MB). Verify these are sufficient; if not, override via an option function.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/main.go (~line 134, `createHTTPServer` function)
- `github.com/bborbe/http` package source (in `$GOPATH/pkg/mod/github.com/bborbe/http@v*`) — `http_server.go`: `CreateServerOptions` defaults
</context>

<requirements>
### 1. Read libhttp.NewServer source

Confirm the default values applied by `CreateServerOptions` (ReadTimeout 30s, WriteTimeout 30s, IdleTimeout 60s, MaxHeaderBytes 1MB). Note: there are NO `libhttp.WithReadTimeout` / `WithWriteTimeout` / `WithIdleTimeout` helper functions — only the variadic `optionFns ...func(*ServerOptions)` signature.

### 2. Decide: are defaults sufficient?

If YES (likely for task/executor): add a comment near the `libhttp.NewServer(a.Listen, router)` call documenting that defaults are intentionally accepted, then exit. This prompt is a no-op.

If NO: override with an option function, e.g.:
```go
return libhttp.NewServer(a.Listen, router,
    func(o *libhttp.ServerOptions) {
        o.ReadTimeout = 10 * time.Second
        o.WriteTimeout = 30 * time.Second
    },
).Run(ctx)
```

### 3. Do NOT use non-existent helpers

Do NOT write `libhttp.WithReadTimeout(...)` — that function does not exist in `bborbe/http`. The only option pattern is the closure form above.

### 4. Run make precommit

```bash
cd task/executor && make precommit
```
</requirements>

<constraints>
- Only change files in `task/executor/`
- Do NOT commit — dark-factory handles git
- Verify timeout values are appropriate (10s read, 30s write, 60s idle are reasonable defaults)
</constraints>

<verification>
cd task/executor && make precommit
</verification>
