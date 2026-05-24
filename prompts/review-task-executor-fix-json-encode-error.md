---
status: draft
created: "2026-05-24T00:00:00Z"
---

<summary>
- Adds error handling to JSON encoder in agents_handler.go
- Logs warning when client disconnects mid-write
- Returns HTTP 500 if encoding fails
</summary>

<objective>
The HTTP handler for GET /agents silently ignores errors from json.NewEncoder.Encode. If a client disconnects mid-write, the error is swallowed and HTTP 200 is returned with a partial/incomplete JSON response. After this fix, the handler properly handles encode failures.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- task/executor/pkg/handler/agents_handler.go (~line 56)
</context>

<requirements>
### 1. Fix error handling in agents_handler.go ServeHTTP method

Around line 56, change the `json.NewEncoder(w).Encode(entries)` call to handle the error:

```go
if err := json.NewEncoder(w).Encode(entries); err != nil {
    glog.Warningf("encode agent configs: %v", err)
    // Client disconnected or encoding failed - return 500
    http.Error(w, "failed to encode response", http.StatusInternalServerError)
    return
}
```

### 2. Add test for encode failure

In the test file for agents_handler, add a test case where the encoder fails (e.g., by using a writer that returns an error after partial write).

### 3. Run make test

```bash
cd task/executor && make test
```
</requirements>

<constraints>
- Only change files in `task/executor/pkg/handler/`
- Do NOT commit — dark-factory handles git
- Follow project conventions: error wrapping with `github.com/bborbe/errors`, context propagation
</constraints>

<verification>
cd task/executor && make precommit
</verification>
