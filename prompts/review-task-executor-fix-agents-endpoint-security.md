---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Adds authentication to /agents endpoint
- Requires X-Agent-Auth header with shared secret
- Returns 401 Unauthorized if header missing or invalid
</summary>

<objective>
The GET /agents endpoint at agents_handler.go exposes internal K8s resource names, container image references, and mounted Secret names without authentication. An unauthenticated network request can retrieve this sensitive configuration data. After this change, the endpoint requires an X-Agent-Auth header with a valid shared secret.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/handler/agents_handler.go (~line 27, ServeHTTP)
- task/executor/main.go (how the handler is wired)
</context>

<requirements>
### 1. Add authentication check to ServeHTTP

Add a shared secret check using the X-Agent-Auth header:

```go
func (h *agentsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    expectedSecret := os.Getenv("AGENTS_AUTH_SECRET")
    if expectedSecret != "" {
        if r.Header.Get("X-Agent-Auth") != expectedSecret {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
    }
    // ... rest of handler
}
```

### 2. Document the environment variable

Add a comment explaining AGENTS_AUTH_SECRET usage. If empty, authentication is disabled (for development).

### 3. Add test for authentication failure

In the test file, add test cases:
- Request without X-Agent-Auth header returns 401
- Request with wrong header value returns 401
- Request with correct header proceeds normally

### 4. Run make test

```bash
cd task/executor && make test
```
</requirements>

<constraints>
- Only change files in `task/executor/`
- Do NOT commit — dark-factory handles git
- If AGENTS_AUTH_SECRET is empty, authentication is disabled (backwards compatible for dev)
</constraints>

<verification>
cd task/executor && make precommit
</verification>
