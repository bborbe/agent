---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Adds explicit HTTP server timeouts
- Configures read, write, and idle timeouts
- Mitigates slowloris attacks and connection exhaustion
</summary>

<objective>
The HTTP server in task/executor is created without explicit timeouts, making it vulnerable to slowloris attacks and connection exhaustion. After this change, the server has explicit read, write, and idle timeouts configured.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/main.go (~line 134, createHTTPServer function)
</context>

<requirements>
### 1. Check libhttp.NewServer API

Verify what timeout options are available in the bborbe/http library. Look for options like WithReadTimeout, WithWriteTimeout, WithIdleTimeout.

### 2. Update createHTTPServer to use timeouts

If the API supports options:
```go
return libhttp.NewServer(
    a.Listen,
    router,
    libhttp.WithReadTimeout(10*time.Second),
    libhttp.WithWriteTimeout(30*time.Second),
    libhttp.WithIdleTimeout(60*time.Second),
).Run(ctx)
```

If the API does not support options, investigate alternative approaches to set timeouts on the underlying http.Server.

### 3. Run make build

```bash
cd task/executor && make build
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
