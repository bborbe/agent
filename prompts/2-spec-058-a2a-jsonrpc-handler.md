---
status: draft
spec: [058-interactive-service-a2a-endpoint]
created: "2026-10-05T20:20:00Z"
branch: dark-factory/interactive-service-a2a-endpoint
---

# A2A JSON-RPC endpoint bridging to the session seam

<summary>
- The interactive service now answers a standards-compliant A2A `SendMessage` request at `POST /a2a`, so an A2A client can drive the agent over the network instead of a hand-written route.
- An authenticated request runs **one turn** on the conversation named by the request's `contextId`, through the same session cache and per-session lock the existing prompt route uses — not a fresh, uncached session per call.
- An absent `contextId` resolves to the same default conversation an absent session header resolves to today, so a caller that names no conversation lands in the one the service has always served.
- The request's text reaches the session as its prompt, and the agent's own computed reply comes back as the task's artifact — proven against the native prompt route, not against the A2A endpoint itself.
- A backend failure is reported honestly: the task ends `failed`, the error is logged, and the error text is never returned to the caller.
- The request's `contextId` is attacker-controlled and is validated against the service's existing anchored session-id pattern **before** any session is built, so a leading `-` or a `/` can never reach a backend CLI as a flag or a path.
- A request naming an A2A method this service does not implement returns a JSON-RPC error object, never a panic and never a silent success.
- The endpoint is registered on the router the bearer gate already wraps, so an unauthenticated request is refused `401` before the handler runs — no second credential and no new gate is added.
- No existing route's contract changes.

</summary>

<objective>
Serve the A2A JSON-RPC binding at `POST /a2a` and bridge a `SendMessage` request to the existing session seam, so an authenticated A2A client can run one turn on an addressed conversation and receive the agent's computed result, with the same caching, locking, id validation and failure reporting the native prompt route uses. Implements spec 058 Desired Behaviors 3-6 and Acceptance Criteria 3, 4, 7, 8 and 9. The Agent Card and its public-address configuration are the sibling prompt `1-spec-058-a2a-card-and-config.md`; the frozen-contract doc update is `3-spec-058-frozen-contract-a2a.md`.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1; `interactive/` is a package in that root module, not a separate module, and it has no Makefile of its own). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private and named after the interface with a lowercased first letter.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; `no-fmt-errorf`, `no-bare-return-err`, `no-context-background-in-business-logic`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-http-handler-refactoring-guide.md` — handler and middleware placement and shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every exported type, field and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md` — logging levels; the failure path logs the error but nothing on this path may log a credential.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), suite timeout, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80 lines / 50 statements, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing (repo-relative):
- `interactive/service.go` — the `Service` interface, both constructors, the private `service` struct, and `Handler()`. The new route is registered in `Handler()`. This prompt's sibling (`1-spec-058-a2a-card-and-config.md`) has already added the `card` field, the `publicURL` parameter and the card route — if `WellKnownAgentCardPath` is NOT registered in `Handler()` and the `NewService` / `NewServiceWithPermissions` constructors do NOT take a `publicURL string` parameter, STOP and report a precondition failure.
- `interactive/a2a.go` — created by the sibling prompt; contains `a2aPublicURLEnv`, `A2APublicURLFromEnv`, `newAgentCard` and `agentCardHandler`. This prompt adds a sibling file, it does not edit that one.
- `interactive/session-cache.go` — `sessionEntry.Prompt` (holds the per-session lock and emits the `turn start id=<id>` / `turn end id=<id>` pair) and `sessionCache.Get(id)`. This is the seam the A2A executor bridges to.
- `interactive/session-id.go` — `sessionIDPattern` (the anchored regexp `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`) and `defaultSessionID` (`"identity"`). Both are reused unchanged.
- `interactive/prompt.go` — `promptHandler`: the native route whose caching, locking, id validation and failure behaviour the A2A path must mirror.
- `interactive/auth.go` — `requireAuth` and `authExempt`. The gate already wraps the whole router; this prompt registers the new route on that router and edits NOTHING in this file.
- `interactive/service_test.go` — `captureStderr`, `newTestServer`, `postPrompt`, `newMockBackend` and the frozen-contract table. Reuse `captureStderr` and `newMockBackend`; do not redefine anything.
- `interactive/auth_test.go` — `authTestToken`, `authWrongToken`, `newAuthMockFactory`, `newAuthTestServer`, `requestWithAuth`. Reuse these; do not redefine anything.
- `interactive/permission_test.go` — `postPromptFull(serverURL, sessionID, body string) (int, string, error)`, the prompt route helper that returns the response body. Reuse it for the native-route comparison.
- `interactive/a2a_test.go` — created by the sibling prompt; contains `testPublicURL`, `newA2ATestServer(publicURL string, auth interactive.Auth, factory agentlib.SessionFactory) *httptest.Server` and the Agent Card specs. Reuse these; do not redefine anything.
- `docs/interactive-service.md` — the frozen contract. READ it to know which existing rows must keep their statuses; do NOT edit it in this prompt.
- `specs/in-progress/058-interactive-service-a2a-endpoint.md` — the spec this prompt implements.

Load-bearing facts, verified by reading the A2A SDK source at `$(go env GOPATH)/pkg/mod/github.com/a2aproject/a2a-go/v2@v2.6.0`. Do not re-derive these, do not contradict them.

1. **The wire method name is `SendMessage`, not `message/send`.** The v2 SDK implements A2A protocol `1.0` (`a2a.Version = "1.0"`), whose JSON-RPC method names are PascalCase: `a2asrv`'s internal constant `MethodMessageSend = "SendMessage"` (likewise `GetTask`, `CancelTask`). The spec's `message/send` spelling is the superseded v0.3 name. A request with `"method":"SendMessage"` dispatches to `SendMessage`; any other method name returns a JSON-RPC `-32601` method-not-found error. See REVIEWER NOTES.

2. **Constructing the handler.** `a2asrv.NewHandler(executor a2asrv.AgentExecutor, options ...RequestHandlerOption) a2asrv.RequestHandler` builds the request handler; with no options it defaults to an in-memory task store and an in-memory queue manager, which is all a one-shot `SendMessage` needs. `a2asrv.NewJSONRPCHandler(handler a2asrv.RequestHandler, options ...a2asrv.TransportOption) http.Handler` returns the `http.Handler` to register.

3. **The executor interface** (`a2asrv/agentexec.go`):

   ```go
   type AgentExecutor interface {
       Execute(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error]
       Cancel(ctx context.Context, execCtx *ExecutorContext) iter.Seq2[a2a.Event, error]
   }
   ```

   The `iter.Seq2[a2a.Event, error]` sequence is a `func(yield func(a2a.Event, error) bool)` closure: yield each event with a `nil` error, and stop early when `yield` returns `false`. Yielding a non-nil error aborts the execution and that error becomes the JSON-RPC error response. `iter` is the standard-library package `iter` (Go 1.23+).

4. **The executor context** (`a2asrv/exectx.go`): `ExecutorContext` has a field `Message *a2a.Message` (the message that triggered execution) and a field `ContextID string` (the server-resolved task context — **generated** as a fresh UUID when the caller supplied none). `ExecutorContext` implements `a2a.TaskInfoProvider` via `TaskInfo() a2a.TaskInfo`. **Read the caller's context id from `execCtx.Message.ContextID`, NOT from `execCtx.ContextID`** — only the former is empty when the caller named no conversation, which is what the default-conversation behaviour requires.

5. **Message and parts** (`a2a/core.go`): `a2a.Message` has `ID string` (`json:"messageId"`), `ContextID string` (`json:"contextId,omitempty"`), `Role MessageRole`, `Parts ContentParts` (a `[]*Part`), and others. A text part is built with `a2a.NewTextPart(text string) *Part` and read back with `(*Part).Text() string` (returns `""` for a non-text part). `a2a.MessageRoleUser` is `"ROLE_USER"`. The handler validates the incoming request and returns a JSON-RPC `-32602` invalid-params error when `params.message` is missing, its `messageId` is empty, its `parts` are empty, or its `role` is empty — so the executor only ever sees a message with at least one part.

6. **Events** (`a2a/core.go`):
   - `a2a.NewSubmittedTask(infoProvider a2a.TaskInfoProvider, initialMessage *a2a.Message) *a2a.Task` — creates the task the following events attach to. Yield it FIRST.
   - `a2a.NewArtifactEvent(infoProvider a2a.TaskInfoProvider, parts ...*Part) *a2a.TaskArtifactUpdateEvent` — attaches an artifact carrying the given parts.
   - `a2a.NewStatusUpdateEvent(infoProvider a2a.TaskInfoProvider, state a2a.TaskState, msg *a2a.Message) *a2a.TaskStatusUpdateEvent` — moves the task's state. `msg` may be `nil`.
   - `a2a.TaskStateCompleted` is `"TASK_STATE_COMPLETED"`, `a2a.TaskStateFailed` is `"TASK_STATE_FAILED"`, `a2a.TaskStateCanceled` is `"TASK_STATE_CANCELED"`.
   - `a2a.Task` has `Status a2a.TaskStatus` (`State a2a.TaskState`, `Message *a2a.Message`, `Timestamp *time.Time`) and `Artifacts []*a2a.Artifact` (`Parts a2a.ContentParts`).

7. **The canonical success sequence** (from the SDK's own `a2asrv/example_test.go` `ExampleAgentExecutor`), which yields a task with state `TASK_STATE_COMPLETED` and one artifact:

   ```go
   yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil)
   yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result)), nil)
   yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
   ```

8. **The response envelope.** A successful `SendMessage` answers `{"jsonrpc":"2.0","id":<id>,"result":{"task":{...}}}` — `result` is an `a2a.StreamResponse` whose `UnmarshalJSON` selects the single event key (`task`/`message`/`statusUpdate`/`artifactUpdate`). Decode `result` into `a2a.StreamResponse` and type-assert its `Event` to `*a2a.Task`. A failed call answers `{"jsonrpc":"2.0","id":<id>,"error":{"code":<int>,"message":"<text>"}}` with no `result`. Error codes: `-32600` invalid request, `-32601` method not found, `-32602` invalid params.

9. **Sentinel errors** (`a2a/errors.go`): `a2a.ErrInvalidParams` maps to `-32602`; `a2a.ErrMethodNotFound` maps to `-32601`. Yield `a2a.ErrInvalidParams` (or a value wrapping it) from the executor to produce a JSON-RPC error response.

10. **The session seam** (`interactive/session-cache.go`): `s.cache.Get(sessionID)` returns the cached `*sessionEntry` for an id (building it on first use); `entry.Prompt(ctx, prompt) (string, error)` holds that conversation's lock for the turn and emits the `turn start id=<id>` / `turn end id=<id>` pair around the call. This is exactly what `promptHandler` does, and it is why the A2A path shares the cache and the lock rather than building a fresh session.

11. `interactive/service.go`'s `Handler()` currently builds the router, registers `/readiness`, `/metrics`, `/prompt`, the well-known card path (added by the sibling prompt), and `/permission` when `s.permissions != nil`, then returns `s.requireAuth(router)`. `requireAuth` wraps the WHOLE router, so any route registered here is gated by the bearer token before its handler runs.

12. The ONLY call sites of the two constructors are in tests (the sibling prompt updated all five to pass `testPublicURL`). `grep -rn 'interactive.NewService' --include='*.go' .` confirms this; re-run it before you start. There is no production call site in this repo.

13. `context.Background()` appears ZERO times in this repo's non-test code. `github.com/bborbe/errors` is at `v1.6.1`. `.golangci.yml` does NOT enable `gochecknoglobals`. `make precommit` at the root runs `ensure format generate test check addlicense`; `make test` runs the whole module with `-race`. Lint limits that bind the new files: funlen 80 lines / 50 statements, gocognit 20, nestif 4, golines 100.

14. The package's only test entry point is `TestInteractive` (the Ginkgo suite). A `go test -run <name>` filter that matches no function exits 0 without running anything — do NOT use a `-run` filter as evidence.

</context>

<requirements>

## 1. New file `interactive/a2a-handler.go`

Create `interactive/a2a-handler.go` (`package interactive`, BSD license header matching the sibling files, no `init()`, no package-level mutable state). It imports `context`, `iter`, `net/http`, `strings`, `github.com/a2aproject/a2a-go/v2/a2a`, `github.com/a2aproject/a2a-go/v2/a2asrv`, and `github.com/golang/glog`.

### 1a. The route constant and the handler

```go
// a2aPath is the route the A2A JSON-RPC binding is served at. It is registered on the
// router the authentication gate wraps, so it is gated like every other non-exempt route.
const a2aPath = "/a2a"

// a2aHandler serves POST /a2a: the A2A JSON-RPC binding over the session seam.
func (s *service) a2aHandler() http.Handler {
    return a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(&a2aExecutor{cache: s.cache}))
}
```

### 1b. The executor bridging to the session seam

```go
// a2aExecutor bridges an A2A request to the session seam. One SendMessage runs one turn on
// the conversation named by the request's contextId, through the same session cache and
// per-session lock POST /prompt uses.
type a2aExecutor struct {
    cache *sessionCache
}

var _ a2asrv.AgentExecutor = (*a2aExecutor)(nil)
```

Implement `Execute` as a method returning an `iter.Seq2[a2a.Event, error]` closure that, in order:

1. Resolves the conversation id from the CALLER's context id, defaulting when absent:
   ```go
   sessionID := execCtx.Message.ContextID
   if sessionID == "" {
       sessionID = defaultSessionID
   }
   ```
2. Validates the id against `sessionIDPattern` **before** any session is built; on mismatch, `yield(nil, a2a.ErrInvalidParams)` and return. This is a named security control (spec 058 § Security / Abuse): the id reaches a backend CLI as an argument, so a leading `-` would be read as a flag and `.` or `/` would escape a session directory.
3. Extracts the prompt text from `execCtx.Message` (requirement 1c); if it is empty, `yield(nil, a2a.ErrInvalidParams)` and return.
4. `yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil)` — and STOP (return) if `yield` returns `false`.
5. Calls `result, err := e.cache.Get(sessionID).Prompt(ctx, prompt)`.
6. On error: log it (`glog.Warningf("a2a message failed: %v", err)`), then `yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, nil), nil)` and return. Do NOT put the error text into the status message — the caller must not receive it.
7. On success: `yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result)), nil)` (stop if `false`), then `yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)`.

Implement `Cancel` with the SDK's default cancellation shape — yield a single `a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil)`. One-shot `SendMessage` never has a live turn to interrupt, so this is a total function that matches the interface.

### 1c. The prompt-text extraction

```go
// promptFromMessage concatenates the text of every part of an A2A message, in order. A
// non-text part contributes nothing. An empty result means the message carries no text and
// the request is refused before any session is built.
func promptFromMessage(message *a2a.Message) string {
    var builder strings.Builder
    for _, part := range message.Parts {
        builder.WriteString(part.Text())
    }
    return strings.TrimSpace(builder.String())
}
```

`strings.TrimSpace` mirrors the native route's `prompt := strings.TrimSpace(string(body))`, so an all-whitespace message is treated as empty and refused.

## 2. Edit `interactive/service.go` — register the route

Add one line to `Handler()`, after the card route registration and before the conditional permission route:

```go
router.Handle(a2aPath, s.a2aHandler())
```

The route MUST be registered on the router that `Handler()` then passes to `s.requireAuth(...)` — never beside it. Update the doc comments on `Service.Handler` and `service.Handler` to mention the A2A route and that it requires the bearer token. Change nothing else in this file.

## 3. Tests — new file `interactive/a2a_handler_test.go`

Create `interactive/a2a_handler_test.go` in the external `package interactive_test`, with the BSD license header. Reuse `authTestToken`, `authWrongToken`, `newAuthMockFactory`, `captureStderr`, `newMockBackend`, `postPromptFull`, `testPublicURL` and `newA2ATestServer`; do not redefine any helper that already exists in the package.

### 3a. Helpers (all in this file)

- `postA2A(serverURL, authorization, body string) (int, string, error)` — builds a `POST <serverURL>/a2a` request with `Content-Type: application/json`, sets the `Authorization` header **only when `authorization` is not empty** (an empty value means no header at all), sends it with `http.DefaultClient`, drains and closes the body, and returns the status and the response body.
- `a2aSendBody(id int, contextID, text string) string` — builds a JSON-RPC `SendMessage` request carrying one text part: `{"jsonrpc":"2.0","id":<id>,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"<text>"}]}}}`. Include `"contextId":"<contextID>"` in the message **only when `contextID` is not empty** (an absent field is the "no conversation named" case). Marshal with `encoding/json`.
- `a2aMethodBody(id int, method string) string` — builds a JSON-RPC request for an arbitrary method name (used for the unimplemented-method case).
- `a2aErrorCode(body string) (int, bool)` — decodes a JSON-RPC response envelope (`jsonrpc`, `id`, `result json.RawMessage`, `error *struct{Code int; Message string}`) and reports the error code and whether an `error` object is present.
- `taskFromResult(body string) (*a2a.Task, error)` — decodes the envelope's `result` into an `a2a.StreamResponse` and type-asserts its `Event` to `*a2a.Task`.

### 3b. The specs

Add `var _ = Describe("A2A endpoint", ...)`. Name each `It` EXACTLY as given.

1. **"returns the agent's own computed result as a completed task"** (AC3) — build a `mocks.SessionFactory` whose `CreateStub` returns a `mocks.Session` whose `PromptStub` ECHOES its input (`return "echo:" + p, nil`). NEVER a constant: against a constant stub a handler that drops the request text entirely still passes, so the equality would prove nothing. Build an auth-enabled server with `newA2ATestServer(testPublicURL, interactive.NewAuthToken(authTestToken), factory)`. ⚠️ `postPromptFull` sets only `X-Session-Id` and **no `Authorization` header**, so against this auth-enabled server it returns `401` with body `unauthorized` and cannot serve as the native-route reference. Add one new helper in this file — `postPromptFullAuth(serverURL, sessionID, body, authorization string) (int, string, error)`, identical to `postPromptFull` but setting `Authorization` when `authorization` is non-empty (adding a helper is not a redefinition) — and take the reference with `postPromptFullAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)`. Comparing against the A2A endpoint itself would be circular. Then `postA2A(server.URL, "Bearer "+authTestToken, a2aSendBody(1, "abc", "hello"))`. Assert the A2A status is `200`, `taskFromResult` yields a task whose `Status.State` equals `a2a.TaskStateCompleted`, and whose `Artifacts[0].Parts[0].Text()` equals the native route's body.
2. **"refuses an unauthenticated request with 401"** (AC4) — build an auth-enabled server over `newAuthMockFactory()`. Assert `postA2A(server.URL, "", a2aSendBody(1, "abc", "hello"))` returns `401`, and `postA2A(server.URL, "Bearer "+authWrongToken, a2aSendBody(2, "abc", "hello"))` returns `401`. The pair is what makes the check falsifiable: a service with no gate that answers `400`/`404` for another reason must not pass.
3. **"serves an authenticated request with 200"** (AC4 control) — the same server; `postA2A(server.URL, "Bearer "+authTestToken, a2aSendBody(3, "abc", "hello"))` returns `200`. Without this control a build that refuses everything passes the two `401` rows.
4. **"serialises two calls on one contextId"** (AC7) — build a `mocks.SessionFactory` whose `CreateStub` returns a session whose `PromptStub` signals a `started` channel, blocks on a `release` channel, then returns. Issue two `SendMessage` requests on ONE contextId concurrently (two goroutines). Assert via `Eventually(started).Should(Receive())` / `Consistently(started, 200*time.Millisecond).ShouldNot(Receive())` that the second turn does not start while the first is blocked, then `close(release)` and assert both requests return `200`. This mirrors the existing `/prompt` "same id serialises" spec in `interactive/service_test.go`. Assert `factory.CreateCallCount()` is `1` — one cached session, not two.
5. **"resolves an absent contextId to the identity conversation"** (AC7) — build a server over `newAuthMockFactory()`; capture stderr around one `postA2A` whose body has NO `contextId`; assert the captured output contains `turn start id=identity`. This is the same default an absent `X-Session-Id` takes today.
6. **"shares the session cache with the native route"** (AC7) — build an **auth-disabled** server, `newA2ATestServer(testPublicURL, interactive.AuthDisabled, factory)`, with an echoing stub. On an auth-enabled server `postPromptFull` carries no token, is refused `401`, and builds no session — so the count would be `1` from the A2A call alone and the assertion would prove nothing. Call `postPromptFull(server.URL, "abc", "hello")` and assert `factory.CreateCallCount()` is `1` (the native route built it). Then `postA2A` with `contextId` `"abc"` and assert `factory.CreateCallCount()` is **still** `1` — the A2A turn reused the session the native route built, rather than constructing a fresh uncached one.
7. **"rejects a contextId that fails the pattern before building a session"** (AC8) — for each of `"-x"` and `"a/b"`: build a factory, capture stderr around a `postA2A` with that `contextId` and a valid token, assert the response carries a JSON-RPC `error` object (via `a2aErrorCode`) and NO `result`, and assert the captured output contains no `turn start` line and `factory.CreateCallCount()` is `0`.
8. **"reports an unimplemented method as a JSON-RPC error"** (AC9) — build a server over `newAuthMockFactory()`; `postA2A` with `a2aMethodBody(1, "not/a-real-method")` and a valid token. Assert the status is `200` (the JSON-RPC layer answers errors with HTTP `200` and an `error` object in the body), that `a2aErrorCode` reports an `error` present, and that the body does NOT contain a `result`. Also assert the handler did not panic — the request returning at all is the evidence.
9. **"reports a backend failure as a failed task without leaking the error"** (AC9) — build a factory whose session's `PromptStub` returns `("", errors.New("backend exploded"))`. Capture stderr around a `postA2A` with a valid token; assert the response is `200` with a task whose `Status.State` equals `a2a.TaskStateFailed`, that the captured output contains `backend exploded` at least once, and that the response body does NOT contain `backend exploded`.

</requirements>

<constraints>
- **One `SendMessage` runs one turn through the existing session seam.** Resolve the session with `s.cache.Get(sessionID)` and run the turn with `entry.Prompt(ctx, prompt)` — the same cache and the same per-session lock `POST /prompt` uses. Do NOT build a fresh `agentlib.Session` per request, and do NOT take a lock of your own.
- **The caller's context id is read from `execCtx.Message.ContextID`**, never from `execCtx.ContextID` (which the SDK generates when the caller supplies none). An absent `contextId` resolves to `defaultSessionID` (`"identity"`), exactly as an absent `X-Session-Id` does today.
- **`contextId` is validated before any session is built.** It is attacker-controlled and reaches a backend CLI as an argument, so it is matched against the existing anchored `sessionIDPattern` before `cache.Get`. A request that fails the pattern must build no session and emit no `turn start` line. This is a named security control, not a nicety.
- **A backend error is logged and never returned.** The `failed` task carries no error text; the error text must appear in the log and zero times in the response body.
- **No second credential and no new gate.** The route is registered on the router `requireAuth` wraps, so an unauthenticated request is refused `401` before the handler runs. Do NOT add any authentication, token check or header inspection to the A2A handler or the executor.
- **`auth.go` is untouched by this prompt.** The card exemption (sibling prompt) is the only change to that file.
- **`interactive/a2a-handler.go` must not contain the identifiers `authorizationHeader`, `authTokenEnv`, `AuthFromEnv`, `NewAuthToken` or `authExempt`** — not in code and not in a comment. Acceptance Criterion 6's grep must keep printing exactly `interactive/auth.go`.
- **No existing route's contract changes.** `/readiness`, `/metrics`, `/prompt` and `/permission` keep their current behaviour and statuses. Every existing frozen-contract row keeps its status assertion. Registering the route must not alter the router's handling of any other path.
- **No streaming and no push notifications are built.** Do not implement `SendStreamingMessage`, `SubscribeToTask`, `CancelTask` semantics beyond the SDK's default, or push-notification configuration. The SDK handler serves its own method set; this repo authors only the `SendMessage` bridge.
- **Body cap.** The native route's 1 MiB cap does NOT cover `/a2a`: the SDK's JSON-RPC handler decodes the body with `json.NewDecoder` and applies **no** limit (`grep -rn 'MaxBytesReader\|LimitReader' <sdk>/a2asrv/ <sdk>/internal/jsonrpc/` returns nothing), so without this the endpoint accepts an unbounded body — an authenticated memory-exhaustion vector. Cap it **before** the SDK handler sees it: in `a2aHandler()`, wrap so `r.Body = http.MaxBytesReader(w, r.Body, maxPromptBytes)` (reuse the existing `maxPromptBytes` constant). ⚠️ A truncated JSON body cannot parse, so `/a2a` cannot reproduce the native route's *truncate-and-process* semantics — it refuses instead; the sibling doc prompt records that deviation. The A2A path extracts its prompt from the parsed message and applies the same `strings.TrimSpace` + empty-refusal the native route uses. The anchored regex is `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`.
- **Error handling** uses `github.com/bborbe/errors` in this repo's own code — `errors.Errorf` / `errors.New` / `errors.Wrap` / `errors.Wrapf`, never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in non-test code. The sentinel errors yielded to the SDK are the SDK's own (`a2a.ErrInvalidParams`).
- **Code conventions** per `docs/dod.md`: exported items carry doc comments; no `init()`, no package-level mutable state; factory functions are pure composition (no conditionals, no I/O).
- **Tests.** Ginkgo v2 / Gomega in the external `interactive_test` package; counterfeiter mocks only, never hand-written; coverage for the new code >= 80%. Reuse `captureStderr`, `newMockBackend`, `newAuthMockFactory`, `postPromptFull`, `testPublicURL` and `newA2ATestServer`; redefine nothing.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; gocognit 20; nestif 4.
- **`taskFromResult` uses the checked type assertion.** `forcetypeassert` is enabled in `.golangci.yml`, so the single-value form `sr.Event.(*a2a.Task)` fails lint. Use `task, ok := sr.Event.(*a2a.Task); if !ok { return nil, errors.Errorf(...) }` — the helper already returns an `error`, so the check costs nothing.
- **Hand-off, not this prompt.** The Agent Card, the public-address configuration and the card route are the sibling `1-spec-058-a2a-card-and-config.md`. The `docs/interactive-service.md` update, the README row and the CHANGELOG entry are the sibling `3-spec-058-frozen-contract-a2a.md`.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable; none needs host tooling, Docker, a cluster or a second repository.

```bash
# 1. The route is registered on the wrapped router, beside the others.
grep -n 'a2aPath' interactive/service.go
grep -n 'return s.requireAuth(router)' interactive/service.go   # still exactly one match

# 2. The handler bridges to the session seam and reads the caller's context id.
grep -n 'cache.Get(sessionID)' interactive/a2a-handler.go
grep -n 'execCtx.Message.ContextID' interactive/a2a-handler.go
grep -n 'sessionIDPattern' interactive/a2a-handler.go
grep -n 'a2a.ErrInvalidParams' interactive/a2a-handler.go
grep -n 'a2a.TaskStateFailed' interactive/a2a-handler.go
grep -n 'a2a.TaskStateCompleted' interactive/a2a-handler.go

# 3. The wire method name used by the tests is the SDK's, not the v0.3 spelling.
grep -n '"SendMessage"' interactive/a2a_handler_test.go
! grep -q 'message/send' interactive/a2a_handler_test.go

# 4. The new production file adds no credential reference and no context.Background().
! grep -nE 'authorizationHeader|authTokenEnv|AuthFromEnv|NewAuthToken|authExempt' interactive/a2a-handler.go
! grep -n 'context.Background()' interactive/a2a-handler.go

# 5. Acceptance Criterion 6 still holds: the credential path stays in interactive/auth.go.
test "$(grep -rln --include='*.go' --exclude='*_test.go' -E 'authorizationHeader|authTokenEnv|AuthFromEnv|NewAuthToken|authExempt' interactive/)" = "interactive/auth.go" && echo "AC6 OK"

# 6. The package's tests pass (the package's only entry point; no -run filter).
go test ./interactive/... -count=1

# 7. The whole module's tests and the full gate.
make test
make precommit
```

Every command must produce the annotated result or the prompt is not done. If `make precommit` fails, fix the issue and re-run ONLY the failing target until it passes, then re-run `make precommit` once more.

**Self-check before finishing:** re-run the block above and confirm each annotated result; then walk spec 058 Acceptance Criteria 3, 4, 5, 7, 8 and 9 against the change and confirm each is met. In particular confirm: an authenticated `SendMessage` returns a `completed` task whose artifact equals the native route's body for the same echoing stub and conversation; no-header and wrong-token each return `401` while a valid token returns `200`; two calls on one `contextId` serialise and reuse one cached session; an absent `contextId` logs `turn start id=identity`; a bad `contextId` yields a JSON-RPC error with no session built; an unimplemented method yields a JSON-RPC error; and a backend failure yields a `failed` task whose error text is logged but absent from the body. Also confirm Acceptance Criterion 5 by running the existing frozen-contract rows and seeing them pass unchanged.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **Decomposition.** This is the spec's prompt 3 ("A2A JSON-RPC handler and the executor bridging to the session seam"), covering Desired Behaviors 3-6 and Acceptance Criteria 3, 4, 7, 8, 9. It folds the spec's separate test prompt into itself: the A2A tests exist only to prove this code, and a tests-only prompt would duplicate them. It depends on prompt 1 (the `service` struct's `card` field, the `publicURL` constructor parameter, the updated test helpers and `newA2ATestServer`), so it MUST run after it.
- **Open question 1 — the wire method name diverges from the spec's literal text.** The spec, its Acceptance Criteria and its Failure Modes all say `message/send`. The mandated SDK (`github.com/a2aproject/a2a-go/v2@v2.6.0`, A2A protocol `1.0`) dispatches on the method name `SendMessage`; `message/send` is the superseded v0.3 spelling and would return `-32601` method-not-found. This prompt implements and tests the SDK's real wire name. If the reviewer requires the v0.3 spelling, the alternative is the SDK's compatibility layer `a2acompat/a2av0` (`a2av0.NewJSONRPCHandler`), which serves `message/send` — but that package imports the un-suffixed `github.com/a2aproject/a2a-go` module the spec calls "the superseded v1 line", so it was rejected here.
- **Open question 2 — the prompt-text extraction is not specified by the spec.** The spec assumes "a deterministic input" and a message carrying it. This prompt concatenates the `Text()` of every part and trims, refusing an empty result with `a2a.ErrInvalidParams`. A single-part text message is the common case; multi-part messages are joined rather than silently truncated. If the reviewer wants only the first text part, or a different empty-message behaviour, change `promptFromMessage`.
- **Open question 3 — the executor is an unexported struct, not a `New*`-constructed exported interface.** The repo's Interface → Constructor → Struct convention applies to exported seams; this executor is a private adapter built inline in `a2aHandler()` and is exercised only through the route. If the reviewer prefers a `newA2AExecutor(cache *sessionCache) a2asrv.AgentExecutor` constructor, it is a one-line extraction.
- **Open question 4 — the SDK handler also serves streaming.** `a2asrv.NewJSONRPCHandler` routes `SendStreamingMessage` (SSE) as well as `SendMessage`. The spec's Non-goal "streaming is out" means this repo does not BUILD a streaming surface; the SDK's inherited method set is not authored here. The executor's `Execute` is the same for both. If the reviewer wants the streaming method actively disabled, that needs a transport option or a wrapper — not requested by the spec.
- **AC5 coverage.** Acceptance Criterion 5 (the enumerated existing-route rows are unchanged) needs no new test: those rows already exist in `contractEntries` / `routeEntries` / `promptEntries` and in `auth_test.go`'s route-policy table, and this change alters none of them. The self-check instructs the executor to run them and see them pass.
