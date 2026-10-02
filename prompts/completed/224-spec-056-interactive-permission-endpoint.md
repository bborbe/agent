---
status: completed
spec: [056-interactive-permission-endpoint]
summary: 'Added a permission endpoint to the interactive service: a per-service PermissionRegistry that pauses a session turn on a permission request, GET/POST /permission to observe and resolve it, a NewServiceWithPermissions constructor delegating from the unchanged four-parameter NewService, plus tests, frozen-contract docs, README and changelog updates.'
execution_id: agent-permission-endpoint-exec-224-spec-056-interactive-permission-endpoint
dark-factory-version: v0.196.0
created: "2026-10-02T09:00:00Z"
queued: "2026-10-02T09:15:26Z"
started: "2026-10-02T09:15:28Z"
completed: "2026-10-02T09:25:30Z"
branch: dark-factory/interactive-permission-endpoint
---

<summary>
- An interactive agent pod can now ask before it acts, instead of failing the moment it wants a tool that is not already pre-approved.
- The pod's HTTP surface gains a permission endpoint: a caller outside the pod can see the tool-permission request a running turn is waiting on, and can post the verdict that releases it.
- An allow lets the paused tool run and the turn finishes normally; a deny hands the caller's own message back to the process that asked.
- A verdict that names a request nobody is waiting on is refused, and it neither creates nor revives an entry.
- A turn that is cancelled while it waits stops waiting, reports an error, and leaves nothing behind.
- A paused conversation never stalls a different conversation, and two services running in the same process share no pending state at all.
- The capability is opt-in at construction: the existing constructor keeps its exact signature and simply does not serve the new route, so an existing consumer compiles and behaves unchanged.
- The new route sits beside readiness, metrics and prompt intake; those three are unchanged, response for response.
- The service's frozen contract document, the README row that names the routes, and the changelog are updated in the same change.
</summary>

<objective>
Add a permission endpoint to the `interactive` service: a mid-turn permission request raised by a session becomes visible outside the pod on `GET /permission`, and a `POST /permission` verdict releases the paused turn. Add a permission-enabled constructor that takes the permission registry as a fifth parameter, and have the existing four-parameter constructor delegate to it with none, so the endpoint is simply not served. Implements spec 056 Desired Behaviors 1-6 and Acceptance Criteria 1-8.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private and named after the interface with a lowercased first letter; `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-library-guide.md` — public-API compatibility for a library consumed from other repositories; why a frozen constructor signature stays frozen.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), the suite timeout, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md` — counterfeiter directives and the `mocks/` package.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; `no-fmt-errorf`, `no-bare-return-err`, `no-context-background-in-business-logic`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-http-handler-refactoring-guide.md` — handler placement and shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — the `no-raw-go-func` rule (production code only; the existing `interactive` tests use raw goroutines).
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every exported type, field and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80 lines / 50 statements, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing (repo-relative):
- `interactive/service.go` — the `Service` interface, `NewService` (four parameters), the private `service` struct and `Handler()`. This is the file the new constructor and the new route are added to.
- `interactive/session-cache.go` — `sessionEntry` (its `mu` is held for the duration of one turn) and `sessionCache` (its map lock is released before the entry lock is taken). This is the two-level locking the permission wait must not break.
- `interactive/prompt.go` — `promptHandler` is the model for a route that validates, caps its body and answers with a fixed status; `maxPromptBytes` is the precedent for the verdict's body cap.
- `interactive/service_test.go` — the single-sourced frozen-contract table (`contractEntries` / `routeEntries` / `promptEntries` / `runContractTable`) and the helpers (`newTestServer`, `postPrompt`). The new 404 row goes into `routeEntries()`; the new tests live in a sibling file and reuse this style.
- `interactive/claude-session_test.go` — the `recordingFactory` / `recordingSession` pattern for a test-only `agentlib.SessionFactory`.
- `interactive/interactive_suite_test.go` — the Ginkgo suite, the 60 s timeout, and the `//go:generate ... counterfeiter ... -generate` directive.
- `claude/claude-session.go` — `PermissionRequest`, `PermissionDecision` and the `PermissionDecider` interface (around lines 42-69). Implement against these; do not reshape them.
- `mocks/claude-permission-decider.go` — the generated mock of `PermissionDecider`, so the `mocks` package's shape is known.
- `docs/interactive-service.md` — the frozen contract this change extends (Constructing, Routes, the contract table, Locking).
- `docs/agent-network-security.md` — the namespace-scoped posture the endpoint inherits; no authentication is added.
- `specs/in-progress/056-interactive-permission-endpoint.md` — the spec this prompt implements.

Load-bearing facts, verified against this working tree. Do not re-derive these, do not contradict them:

1. `interactive.NewService(sessions agentlib.SessionFactory, listen string, providerBaseURL string, registry *prometheus.Registry) Service` is the current four-parameter constructor. Its signature is frozen: `bborbe/agent-pi` is a different repository whose call site must compile unchanged.
2. `service` is a private struct with fields `cache *sessionCache`, `listen string`, `providerBaseURL string`, `registry *prometheus.Registry`; `Handler()` builds an `http.NewServeMux()` registering `/readiness`, `/metrics` and `/prompt`. `Run(ctx)` is unchanged by this prompt.
3. `claude.PermissionRequest` is `struct { ToolName string; Description string; InputPreview string }`; `claude.PermissionDecision` is `struct { Allow bool; Message string }`; `claude.PermissionDecider` is `interface { DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error) }`. Verified in `claude/claude-session.go`.
4. `github.com/bborbe/errors` is at `v1.6.1` and exports `New(ctx, message string) error`, `Errorf(ctx, format string, ...) error`, `Wrap(ctx, err error, message string) error`, `Wrapf(ctx, err error, format string, ...) error`, `Join`, `Is`, `As`, `Cause`. **`Wrap(ctx, nil, message)` returns `nil`** (verified in `errors@v1.6.1/errors_wrap.go`), so a wrapped error is non-nil only when the wrapped error is non-nil. `ctx.Err()` is non-nil whenever `<-ctx.Done()` fires, which is what makes AC4's returned error non-nil.
5. `github.com/google/uuid` is already a DIRECT requirement in `go.mod` (`v1.6.0`) and is already used as `uuid.New().String()` in `delivery/result-deliverer.go`. No new dependency is added.
6. The `interactive` test package is the external `package interactive_test`; the suite timeout is 60 s. `interactive/service_test.go` uses raw `go func(){...}()` in tests, so tests are exempt from the production `no-raw-go-func` rule.
7. `httptest.Server.Close()` blocks until every outstanding request completes. A test that leaves a turn pending and then closes the server hangs until the suite timeout — every such test MUST post a verdict (or cancel the request) before closing.
8. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`. No `git` command is used in this prompt's verification regardless, because the daemon does not check verification exit codes.
9. The counterfeiter short form is a silent no-op in this repo; use the explicit `//counterfeiter:generate -o ../mocks/<file>.go --fake-name <Name> . <Interface>` long form on the interface line, and `make generate` (run by `make precommit`) produces the mock.
10. Lint limits that bind the new files: funlen 80 lines / 50 statements, nestif complexity 4, golines line length 100.
</context>

<requirements>

## 1. The registry — new file `interactive/permission.go`

Create `interactive/permission.go` (`package interactive`, BSD license header matching the sibling files, no `init()`, no package-level mutable state). It holds the per-service pending-request state and the seam the sessions consult.

**The pending-request view.** Define the value the endpoint serves:

```go
// PendingPermission is one permission request currently waiting for a verdict, as
// served on GET /permission.
type PendingPermission struct {
	// ID identifies the request. It is generated per request and is the only handle
	// a verdict may name.
	ID string `json:"id"`
	// ToolName is the name of the tool the CLI wants to invoke.
	ToolName string `json:"tool_name"`
	// Description is the CLI's human-readable description of the invocation.
	Description string `json:"description"`
	// InputPreview is a preview of the tool input. It is never logged.
	InputPreview string `json:"input_preview"`
}
```

**The registry interface.** Define it and add the counterfeiter directive on the interface line in the repo's explicit long form:

```go
//counterfeiter:generate -o ../mocks/interactive-permission-registry.go --fake-name InteractivePermissionRegistry . PermissionRegistry

// PermissionRegistry holds the permission requests one service is currently waiting
// on. It is the PermissionDecider the sessions built for that service consult, so a
// request raised by a turn appears on that service's endpoint and nowhere else.
type PermissionRegistry interface {
	// DecidePermission registers request as pending and blocks until a verdict is
	// posted for it or ctx is cancelled.
	DecidePermission(ctx context.Context, request claude.PermissionRequest) (claude.PermissionDecision, error)

	// List returns the requests currently pending. The result is never nil, so the
	// endpoint always renders a JSON array.
	List() []PendingPermission

	// Resolve delivers decision to the pending request named by id and reports
	// whether such a request was pending. It never blocks.
	Resolve(id string, decision claude.PermissionDecision) bool
}
```

Import `claude "github.com/bborbe/agent/claude"` (the package's own import path is `github.com/bborbe/agent/claude`). Pin the method set with a package-level compile-time assertion, so a signature drift fails the build rather than the consumer:

```go
// The registry is the PermissionDecider a session consults; the assignment pins the
// method set at compile time.
var _ claude.PermissionDecider = (PermissionRegistry)(nil)
```

**The implementation.** Private struct named after the interface with a lowercased first letter, a `New*` constructor returning the interface, and the three methods:

```go
// pendingPermission is one waiting request plus the buffered channel its verdict is
// delivered on.
type pendingPermission struct {
	request claude.PermissionRequest
	verdict chan claude.PermissionDecision
}

// permissionRegistry is the per-service implementation of PermissionRegistry.
type permissionRegistry struct {
	mu      sync.Mutex
	pending map[string]*pendingPermission
}

// NewPermissionRegistry creates an empty registry. Construct one per service and pass
// the same instance to the session factory and to NewServiceWithPermissions, so a
// request raised by a session is served by that service's endpoint.
func NewPermissionRegistry() PermissionRegistry {
	return &permissionRegistry{pending: map[string]*pendingPermission{}}
}
```

`DecidePermission` must:

1. Generate a fresh id with `uuid.New().String()` (`github.com/google/uuid`; do not roll your own id scheme).
2. Create a verdict channel **buffered with capacity 1**, so `Resolve` can never block on it.
3. Take `mu`, store `&pendingPermission{request: request, verdict: verdict}` under the id, release `mu`. The lock is held ONLY around this map write — never across the wait.
4. Block on a `select` between `<-verdict` (return the received decision, nil error) and `<-ctx.Done()`. On the `ctx.Done()` branch: take `mu`, `delete(r.pending, id)`, release `mu`, and return `claude.PermissionDecision{}` with `errors.Wrap(ctx, ctx.Err(), "permission request cancelled")`. `ctx.Err()` is non-nil whenever `ctx.Done()` is closed, so the wrap is non-nil — do NOT write `errors.Wrap(ctx, nil, ...)`, which returns nil and would make AC4's error nil.

The select is the pause: it blocks, it does not poll and it does not sleep. No `for` loop.

`List` must: take `mu` (defer release), build `make([]PendingPermission, 0, len(r.pending))`, append one `PendingPermission` per entry — `ID` set to the map key, and `ToolName`, `Description` and `InputPreview` copied from the stored request — and return it. The zero-length result must be non-nil so it marshals to `[]` and never to `null`. Do not sort; order is unspecified.

`Resolve` must: take `mu`, look up `r.pending[id]`, `delete(r.pending, id)` when present, release `mu`; then, if the entry was absent, return `false`; if it was present, send `decision` on the entry's channel and return `true`. The lock is released before the send. Because the channel is buffered with capacity 1 and the entry was just removed, the send never blocks and a second `Resolve` for the same id finds nothing.

State these invariants in doc comments, because they are what AC5 and AC6 measure:
- The map lock is held only around map reads and writes; it is NEVER held while the caller waits on the verdict channel, so one paused turn cannot stall a lookup for another.
- The entry is removed exactly once — by `Resolve` when a verdict is delivered, or by `DecidePermission` when its context is cancelled. Whichever lands first deletes it; the loser finds no entry.
- The registry is per-instance state: no package-level variable, no `init()`, no singleton. Two registries in one process share nothing.
- The file logs nothing, and in particular never logs `InputPreview`.

## 2. The route — new file `interactive/permission-handler.go`

Create `interactive/permission-handler.go` (`package interactive`). It serves the endpoint on the `service` receiver.

A body cap constant, mirroring `maxPromptBytes` in `interactive/prompt.go` (the endpoint is unauthenticated and reachable by anything in the namespace, so an unbounded read would let one caller exhaust the pod's memory):

```go
// maxPermissionBytes bounds the body a single POST /permission request may carry.
const maxPermissionBytes = 64 << 10
```

The verdict body type:

```go
// permissionVerdict is the POST /permission request body: the id of the request to
// resolve and the verdict to deliver to it.
type permissionVerdict struct {
	ID      string `json:"id"`
	Allow   bool   `json:"allow"`
	Message string `json:"message"`
}
```

The handler dispatches on the method:

```go
// permissionHandler serves the permission endpoint. GET lists the requests currently
// waiting for a verdict; POST delivers a verdict to the request it names.
func (s *service) permissionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.listPermissions(w)
		case http.MethodPost:
			s.resolvePermission(w, r)
		default:
			http.Error(w, "GET or POST required", http.StatusMethodNotAllowed)
		}
	})
}
```

`listPermissions(w)` must: call `s.permissions.List()`, `json.Marshal` it, and on a marshal error answer `500`; otherwise set `Content-Type: application/json` and write the marshalled bytes. Use `json.Marshal` plus `w.Write` (NOT `json.NewEncoder`, which appends a newline) so the empty body is exactly `[]` and the frozen contract row can assert it character for character.

`resolvePermission(w, r)` must:
1. Read the body with `io.ReadAll(io.LimitReader(r.Body, maxPermissionBytes))`; on a read error answer `400`. A body over the cap is truncated by the limit reader and will fail to parse, which is a `400` — that is the intended outcome, not an error to recover from.
2. `json.Unmarshal` into a `permissionVerdict`; on a parse error answer `400`.
3. Answer `400` when `verdict.ID == ""`.
4. Call `s.permissions.Resolve(verdict.ID, claude.PermissionDecision{Allow: verdict.Allow, Message: verdict.Message})`. When it returns `false`, answer `404` (no entry is created, revived or accepted). When it returns `true`, set `Content-Type: application/json` and write exactly `{}`.

Neither function logs anything, and neither ever logs a preview.

## 3. The constructor and the route — edit `interactive/service.go`

Make exactly these changes; do not restructure the file.

1. Add a `permissions PermissionRegistry` field to the private `service` struct, documented as "the permission registry the endpoint and the sessions resolve through; nil when the endpoint is not served".
2. Add the permission-enabled constructor, a pure composition with no conditionals and no I/O:

```go
// NewServiceWithPermissions creates the interactive session service with the
// permission endpoint enabled.
//
// permissions is the registry the endpoint serves and the decider the sessions built
// by the caller's factory consult; the caller constructs it first and passes the one
// instance to both, which is what makes the endpoint and the sessions resolve through
// the same registry. A nil permissions is invalid here — use NewService for that.
func NewServiceWithPermissions(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	permissions PermissionRegistry,
) Service {
	return &service{
		cache:           newSessionCache(sessions),
		listen:          listen,
		providerBaseURL: providerBaseURL,
		registry:        registry,
		permissions:     permissions,
	}
}
```

3. `NewService` keeps its exact four-parameter signature and body shape, and delegates:

```go
func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
) Service {
	return NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, nil)
}
```

Keep `NewService`'s existing doc comment and add one sentence saying the permission endpoint is not served by this constructor.

4. `Handler()` registers the new route only when a registry was supplied, so a plain `NewService` answers `404` on `/permission` (an unregistered path on `http.NewServeMux` is a `404`):

```go
func (s *service) Handler() http.Handler {
	router := http.NewServeMux()
	router.Handle("/readiness", s.readinessHandler())
	router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	router.Handle("/prompt", s.promptHandler())
	if s.permissions != nil {
		router.Handle("/permission", s.permissionHandler())
	}
	return router
}
```

5. Update the doc comments on `Service.Handler` and `service.Handler` to mention the permission endpoint when the service was built with a registry. `Run(ctx)` is unchanged.

## 4. Tests — new file `interactive/permission_test.go` (external `package interactive_test`)

### 4a. Fixture

Add a test-only session backend that raises a permission request through the registry it was built with and records the decision it received, so AC3's deny half can assert on what the decider's caller observed:

```go
// permissionTool is the tool name the fixture raises.
const permissionTool = "Bash"

// permissionPrompt is the prompt that makes a fixture session raise a request. Any
// other prompt completes immediately, so a second session can be shown to run while
// a first is paused.
const permissionPrompt = "raise-permission"

// panicPrompt is the prompt that makes a fixture session raise a request and then
// panic, so the deferred session-lock unlock can be shown to survive a panic while
// paused.
const panicPrompt = "raise-then-panic"

// permissionFactory builds sessions that raise one permission request per turn
// through the registry, and records every decision the raises received.
type permissionFactory struct {
	permissions interactive.PermissionRegistry

	mu        sync.Mutex
	decisions []claude.PermissionDecision
}
```

`Create(_ string) agentlib.Session` returns a `*permissionSession` holding the factory. `Decisions() []claude.PermissionDecision` returns a copy of the recorded slice. `permissionSession.Prompt(ctx, prompt)`:

- if `prompt == panicPrompt`: call `s.factory.permissions.DecidePermission(ctx, claude.PermissionRequest{ToolName: permissionTool})`, ignore the result, then `panic("boom while paused")`.
- else if `prompt != permissionPrompt`: return `"ok", nil` immediately.
- else: call `s.factory.permissions.DecidePermission(ctx, claude.PermissionRequest{ToolName: permissionTool})`; on error return `"", err`; record the decision; when `!decision.Allow` return `"", errors.New(decision.Message)`; otherwise return `"allowed", nil`.

`permissionSession.Close(_ context.Context) error` returns nil. Use the stdlib `errors` package in this test file (precedent: `interactive/service_test.go` imports stdlib `"errors"`).

### 4b. Helpers

Add to `interactive/permission_test.go` (do NOT change the existing `newTestServer` or `postPrompt`):

- `newPermissionTestServer(permissions interactive.PermissionRegistry, factory agentlib.SessionFactory) *httptest.Server` — builds `interactive.NewServiceWithPermissions(factory, ":0", "", prometheus.NewRegistry(), permissions)` and wraps `svc.Handler()` in `httptest.NewServer`.
- `getPermissions(serverURL string) (int, []pendingDTO, string, error)` — `GET /permission`, returning the status, the decoded array, the raw body string and any transport error.
- `postVerdict(serverURL, id string, allow bool, message string) (int, string, error)` — marshals `{"id":id,"allow":allow,"message":message}`, POSTs it to `/permission`, returns status and raw body.
- `postPromptFull(serverURL, sessionID, body string) (int, string, error)` — like the existing `postPrompt`, but also returns the response body.
- a local `pendingDTO` struct with fields `ID string` and `ToolName string`, carrying the json tags `json:"id"` and `json:"tool_name"` for decoding, plus a `promptResult` struct with fields `status int`, `body string` and `err error`.

Every test that starts a prompt which will block MUST post a verdict (or cancel the request) before its `defer server.Close()` runs — see context fact 7.

### 4c. The 404 row in the shared contract table

In `interactive/service_test.go`, add exactly one entry to `routeEntries()` (the rows that never reach a session), so both the mock backend and the Claude backend exercise it:

```go
Entry(
	"GET /permission without a permission registry",
	http.MethodGet,
	"/permission",
	nil,
	"",
	"",
	func(resp *http.Response, _ string, _ contractObserver) {
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	},
),
```

Do not change any pre-existing entry.

### 4d. The permission tests

Add `var _ = Describe("Permission endpoint", ...)` with one `It` per row below. Name each `It` exactly as given — the verification greps these names.

1. **"lists no pending requests on a permission-enabled service"** (AC1) — build a server with a fresh registry; `getPermissions` returns status `200` and body exactly `[]`; the decoded array is empty.
2. **"shows a pending request while its turn is blocked"** (AC2) — start `postPromptFull(server.URL, "sess-a", permissionPrompt)` in a goroutine; `Eventually` `getPermissions` returns length 1; assert the single element's `ToolName` equals `permissionTool` and its `ID` is non-empty; then post an allow verdict to release the goroutine and drain it.
3. **"resolves a pending request with an allow verdict"** (AC3 allow) — start the blocked prompt; `Eventually` length 1; read the id; `postVerdict(id, true, "")` returns status `200` and body exactly `{}`; the blocked prompt's result is status `200` and body `"allowed"`.
4. **"resolves a pending request with a deny verdict carrying the posted message"** (AC3 deny) — same setup; `postVerdict(id, false, "not this time")` returns `200` and body `{}`; the blocked prompt ends (its status is `500` and body `"prompt failed\n"`); and `factory.Decisions()` has exactly one entry with `Allow == false` and `Message == "not this time"` — asserted character for character.
5. **"refuses a verdict for an unknown id and creates nothing"** (AC3 negative) — `postVerdict("no-such-id", true, "")` returns `404`; a follow-up `getPermissions` returns status `200`, body exactly `[]`, empty array. This is the half that proves the POST neither created nor revived an entry.
6. **"refuses a second verdict for the same id"** (spec Failure Modes: same id posted twice) — raise a request, post an allow verdict (`200`), then post a second verdict for the same id and assert `404`, and that `getPermissions` is empty.
7. **"releases the waiter and leaves nothing pending when the turn is cancelled"** (AC4) — build a permission-enabled server with a `&mocks.SessionFactory{}`; in a goroutine call `permissions.DecidePermission(ctx, claude.PermissionRequest{ToolName: permissionTool})` with a `context.WithCancel(context.Background())`, sending the returned error to a buffered channel; `Eventually` `getPermissions` returns length 1; `cancel()`; `Eventually` the goroutine's error is non-nil; then `getPermissions` returns status `200`, body exactly `[]`, empty array. Assert the error AND the empty follow-up — the leak is the second half.
8. **"does not block a prompt on a different session id"** (AC5) — start the blocked prompt on `sess-a`; `Eventually` length 1 (the first is provably pending, not merely started); `postPromptFull(server.URL, "sess-b", "hello")` returns `200` and body `"ok"`; assert `getPermissions` still shows length 1 at that moment; then release `sess-a` with an allow verdict and drain the goroutine.
9. **"does not share pending state between two services"** (AC6) — build two permission-enabled servers over two independent registries; start the blocked prompt on the first; `Eventually` its `getPermissions` is length 1; assert the first's body shows length 1 and the second's body is exactly `[]` with an empty array, in the same test; then release the first so `Close` can return.
10. **"resolves the endpoint and its sessions through one registry"** (AC7) — construct `permissions := interactive.NewPermissionRegistry()` once, build the factory with that same instance, and build the service with `interactive.NewServiceWithPermissions(factory, ":0", "", prometheus.NewRegistry(), permissions)`; prompt that service's own session with `permissionPrompt`; `Eventually` its own `getPermissions` shows length 1 with `ToolName == permissionTool`; release it with a verdict. The construction call passing the one instance to both is the evidence.
11. **"rejects a malformed verdict body"** — POST a non-JSON body to `/permission` and assert `400`; also POST `{"allow":true}` (no id) and assert `400`.
12. **"rejects a method other than GET or POST"** — send `PUT /permission` to a permission-enabled server and assert `405`.
13. **"releases the session lock when a paused turn panics"** (spec Failure Modes: panic while paused) — start `postPromptFull(server.URL, "sess-a", panicPrompt)` in a goroutine; `Eventually` length 1; post an allow verdict so the decider returns and the session panics (the client sees a transport error — accept it); then `postPromptFull(server.URL, "sess-a", "hello")` on the same session id returns `200` and body `"ok"`. This proves the deferred unlock ran.

## 5. Documentation — `docs/interactive-service.md`, `README.md`, `CHANGELOG.md`

**`docs/interactive-service.md`** is a frozen contract and is updated in the same change. Update all of:
- **Constructing the service** — show `interactive.NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, permissions)` beside the existing `NewService` line, and add a `permissions` row to the parameter table: "the permission registry the endpoint serves and the sessions consult; pass the same instance to the session factory, or the endpoint serves nothing. `NewService` takes none and does not serve the route."
- **Routes** — add a row: `| /permission | GET, POST | Lists the pending permission requests; delivers a verdict to the one it names |`.
- **The contract, row by row** — add these rows (this table is the authority; the tests assert the same strings): `GET /permission`, permission-enabled, nothing pending → `200`, `Content-Type: application/json`, body exactly `[]`; `GET /permission`, plain `NewService` → `404`; `POST /permission`, pending id, `{"id":"…","allow":true}` → `200`, body exactly `{}`; `POST /permission`, unknown id → `404`; `POST /permission`, missing `id` → `400`; `POST /permission`, malformed JSON → `400`; `PUT /permission`, permission-enabled → `405`.
- A new **## Permission endpoint** section documenting the two directions, the JSON shapes (`{id, tool_name, description, input_preview}` and `{id, allow, message}`), that the id is generated per request and is the only handle a verdict may name, that an id no request holds is refused rather than treated as a new request, that the entry is removed exactly once (on verdict or on cancellation), that the endpoint adds no authentication and no logging of its own (the preview is never logged), and that it is the route `agent-pi` does not serve.
- **Locking** — add one paragraph: the permission registry holds its own lock only around its map operations and never across the wait, so a turn paused on a permission request does not hold the map lock and does not stall another session. This keeps the section's existing "no request is ever rejected because of the lock" invariant true and does not widen it into one lock held across the wait.

**`README.md`** — the `interactive/` row currently names "readiness, metrics and prompt intake". Extend it to name the permission endpoint as well. Keep the edit to that one row.

**`CHANGELOG.md`** — `## Unreleased` does not exist yet (the newest section is `## v0.92.0`); create it at the top, below the preamble and above `## v0.92.0`. Add one `feat:` bullet following `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`: name the permission endpoint, the two directions, the permission-enabled constructor, the fact that `NewService` keeps its signature and does not serve the route, and the per-service registry with cancellation cleanup.

## 6. Scope containment

Create or edit ONLY these files:
- `interactive/permission.go` (new)
- `interactive/permission-handler.go` (new)
- `interactive/permission_test.go` (new)
- `interactive/service.go` (the field, the new constructor, the delegating `NewService`, the route registration, the doc comments)
- `interactive/service_test.go` (one added entry in `routeEntries()` only)
- `mocks/interactive-permission-registry.go` (generated by `make generate`)
- `docs/interactive-service.md`
- `README.md`
- `CHANGELOG.md`

Do NOT touch `interactive/session-cache.go`, `interactive/session-id.go`, `interactive/readiness.go`, `interactive/prompt.go`, `interactive/claude-session_test.go`, `claude/`, `pi/`, or any other package. Do NOT change `claude.PermissionDecider`, `claude.PermissionRequest` or `claude.PermissionDecision`. Do NOT add authentication to the endpoint (spec Non-goal). Do NOT add a client for the endpoint, a Config CR, or any `agent-claude` wiring (spec Non-goals — the `agent-claude` port is a separate hand-off).
</requirements>

<constraints>
- **`NewService` keeps its four-parameter signature (frozen public API).** `bborbe/agent-pi` is a different repository whose call site must compile unchanged; the endpoint is enabled only by the new `NewServiceWithPermissions`, which takes the registry as a fifth parameter. Do NOT add a variadic option, a config struct, an interface parameter or an opt-out flag to `NewService`.
- **`claude.PermissionDecider` and its value types are frozen.** Implement against `DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)`; do not add a method, a field or a second decider seam.
- **`docs/interactive-service.md` is a frozen contract and is updated in the same change.** The new route is a new row in its Routes table and new rows in its contract table. Its existing rows and its existing `## Locking` invariant ("no request is ever rejected because of the lock") must stay true and must not be widened into one lock held across the wait.
- **The pending wait must not hold the session cache's map lock.** The cache's two-level discipline is what keeps a paused session from stalling every other session. The registry holds its own lock only around its map reads and writes, never across the blocking receive.
- **The wait must observe context cancellation.** A waiter that ignores it leaks one entry per cancelled turn, which the pod cannot recover from without a restart. The `select` must have a `<-ctx.Done()` branch that deletes the entry and returns a non-nil error.
- **The registry and the decider are per-service state, not package state.** No package-level variable, no `init()`, no singleton — a test binary holds more than one service, and a global would couple them.
- **No authentication is added to the endpoint (spec Non-goal).** The posture is namespace-scoped reachability, unchanged — see `docs/agent-network-security.md`. Do not add a token, a header check or an auth middleware. Do not add a client for the endpoint.
- **Error handling** uses `github.com/bborbe/errors` — `errors.Wrap` / `errors.Wrapf` / `errors.Errorf` / `errors.New`, never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in business logic. Note `errors.Wrap(ctx, nil, …)` returns `nil`.
- **Code conventions** per `docs/dod.md`: exported items carry doc comments; Interface → Constructor → Struct → Method with the implementation struct private and named after the interface with a lowercased first letter; no `init()`, no package-level mutable state; factory functions are pure composition (no conditionals, no I/O).
- **No new dependency.** `github.com/google/uuid` is already a direct requirement; use it for the id and add nothing else. No `replace` and no new `exclude`.
- **Logging.** The permission path adds no logging of its own, and the tool-input preview is never logged (spec Security). Do not add a metrics family either — the spec asks for none.
- **Tests.** Ginkgo v2 / Gomega in the external `interactive_test` package; counterfeiter mocks only, never hand-written; coverage for the new code >= 80%. Every test that leaves a turn pending must release it before its server is closed (context fact 7).
- **Repository hygiene.** `README.md`'s `interactive/` row and `CHANGELOG.md`'s new `## Unreleased` section are updated in this change.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; nestif complexity 4.
- **Hand-off, not this prompt.** The `agent-claude` port — constructing the registry, passing it to both the session factory and the permission-enabled service, and updating that repository's stale `nil`-decider comment — belongs to `bborbe/agent-claude`'s own pipeline and is NOT done here. This repository only exposes the capability.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable.

```bash
# 1. The route is registered somewhere in the package.
grep -rn '"/permission"' interactive/
# Must return >= 1 line.

# 2. The frozen contract documents the route.
grep -n '/permission' docs/interactive-service.md
# Must return >= 1 line.

# 3. The permission-enabled constructor exists and the plain one keeps four parameters.
grep -n 'func NewServiceWithPermissions(' interactive/service.go
grep -n 'func NewService(' interactive/service.go
grep -n 'return NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, nil)' interactive/service.go
# Each must match.

# 4. The frozen decider signature is untouched.
grep -n 'DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)' claude/claude-session.go
# Must match exactly once.

# 5. No package-level state and no init() in the new files. The compile-time assertion
#    `var _ claude.PermissionDecider = ...` is a blank-identifier declaration, not state,
#    and is excluded by the `^var [a-zA-Z]` pattern.
! grep -n '^func init(' interactive/permission.go interactive/permission-handler.go
! grep -nE '^var [a-zA-Z]' interactive/permission.go interactive/permission-handler.go
# Each must print nothing.

# 6. The permission tests pass and every row ran.
go test -mod=mod -race -v ./interactive/ > /tmp/interactive-permission-test.log 2>&1
# Must exit 0. Each of these names must appear:
grep -E "lists no pending requests on a permission-enabled service|shows a pending request while its turn is blocked|resolves a pending request with an allow verdict|resolves a pending request with a deny verdict carrying the posted message|refuses a verdict for an unknown id and creates nothing|refuses a second verdict for the same id|releases the waiter and leaves nothing pending when the turn is cancelled|does not block a prompt on a different session id|does not share pending state between two services|resolves the endpoint and its sessions through one registry|rejects a malformed verdict body|rejects a method other than GET or POST|releases the session lock when a paused turn panics" /tmp/interactive-permission-test.log

# 7. The new 404 contract row is driven by both backends.
grep -c "GET /permission without a permission registry" /tmp/interactive-permission-test.log
# Must be >= 2.

# 8. Coverage for the new code.
go test -coverprofile=/tmp/cover.out -mod=mod ./interactive/... && go tool cover -func=/tmp/cover.out | grep -E 'permission|total'
# The permission functions must each be >= 80%.

# 9. The whole module's tests.
make test
# Must exit 0.

# 10. Final validation.
make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then re-run `make precommit` once more.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **One prompt, per the spec's § Suggested Decomposition.** The registry, the decider, the route and the constructor are one mechanism with one seam — the wait — and the route is the only way that wait is observed. Every acceptance criterion except the doc-grep half of AC8 is HTTP-level, so a mechanism-only prompt would carry criteria it could not satisfy until a surface prompt landed. The spec's constraint — any row's `Covers ACs` must be satisfiable by that row alone — is met by keeping this a single prompt: it covers AC1-AC8.
- **Open question 1 — the POST body shape is an implementation choice the spec does not fix.** The spec says "POST /permission naming a pending id" and "an allow body" / "a deny body", and fixes the route at exactly `/permission` (the spec's own evidence command greps the literal `"/permission"`). This prompt therefore puts the id in a JSON body, `{"id":…,"allow":…,"message":…}`, rather than in the path. If the reviewer prefers `POST /permission/{id}`, the route literal in the spec's evidence command would change too, so the body form was chosen to keep the route exactly `/permission`.
- **Open question 2 — the POST success response body is not fixed by the spec.** The spec requires status `200` and says the evidence quotes the body, but does not name one. This prompt fixes it at exactly `{}` with `Content-Type: application/json`, so the frozen contract row and the test assert one string. If a different body is wanted, change it in `interactive/permission-handler.go`, the contract row in `docs/interactive-service.md` and the corresponding test together.
- **Open question 3 — `maxPermissionBytes = 64 << 10` is a defensive bound, not a spec requirement.** The spec's Security section does not ask for a verdict-body cap. It is included because the endpoint is unauthenticated and its sibling `POST /prompt` caps its body for exactly that reason; a truncated oversized body fails to parse and is answered `400`. Delete the constant and use `r.Body` directly if the reviewer judges this YAGNI.
- **The plain-constructor `404` row lives in the shared contract table.** `runContractTable` builds its server through `NewService`, so the "no registry → 404" row belongs in `routeEntries()` and is exercised by both the mock and the Claude backends. The permission-enabled rows cannot live there (that table never builds a permission-enabled service) and live in `interactive/permission_test.go` instead; the doc's contract table is the single authority and both test locations assert against it.
- **Naming.** The filename is `1-spec-056-interactive-permission-endpoint.md`, matching the repo's existing inbox convention (`1-spec-047-…`, `1-spec-048-…`) and the completed `NNN-spec-MMM-…` shape; the `1-` is the ordering prefix and dark-factory replaces it with its own number on approve.
- **Operator-executable rung (not run in the container).** The spec's Verification ladder additionally asks for `git tag --contains <merge-sha>` after merge, and `go build ./...` in `bborbe/agent-pi` and `bborbe/agent-claude` after the dependency bump — the latter needs `agent-claude`'s own hand-off port to have landed, and `agent-pi` is the consumer whose compatibility the resolved constructor shape exists to protect. Neither belongs in this prompt: the container has no host tooling and no second repository checked out.
