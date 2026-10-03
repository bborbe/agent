---
status: completed
spec: [057-interactive-service-authentication]
summary: 'Added a bearer-token gate to the interactive service: /prompt and /permission now require Authorization: Bearer <token> (constant-time compared), /readiness and /metrics stay exempt, and both constructors take an explicit interactive.Auth decision that fails closed at its zero value.'
execution_id: agent-interactive-auth-exec-225-spec-057-interactive-service-authentication
dark-factory-version: v0.196.0
created: "2026-10-02T23:20:00Z"
queued: "2026-10-02T23:34:59Z"
started: "2026-10-03T00:25:53Z"
completed: "2026-10-03T00:30:25Z"
---

<summary>
- The interactive HTTP surface stops being open: a request to the prompt route or the permission route must now carry a bearer token, and one that carries none or a wrong one is refused with `401` — before any work at all, so a refused request cannot occupy a session and leaves no turn-boundary log lines behind.
- The two infrastructure routes, readiness and metrics, stay open, and that exemption is a recorded decision rather than an oversight: a kubelet probe and a Prometheus scrape cannot present a token without the token being written into the pod spec and the scrape configuration.
- Every other route is gated by construction, including the permission route that is only registered when a permission registry is supplied, and including any route added later.
- The token comparison is constant-time, so a caller cannot recover the token's length or prefix by measuring how long a refusal takes.
- A consumer cannot build the service without stating its authentication choice: the constructor takes an explicit decision, and a decision left unset fails closed (it refuses every gated request) rather than silently serving everything.
- A consumer that is deliberately unexposed states that opt-out in one visible, greppable line, and that line is covered by a test rather than assumed to work.
- The token is read from the process environment at startup; an unset token is an error, so a service that cannot authenticate fails to start instead of serving unauthenticated.
- The token is never logged, never written into source, and never written next to the scheme prefix anywhere in the package.
- Nothing changes for an authenticated caller: session-id validation, the body cap, the readiness body strings, the locking discipline and the turn-boundary log pair all behave exactly as before.
- The repository README and changelog are updated in the same change.

</summary>

<objective>
Add a bearer-token gate to the `interactive` service so that `/prompt` and `/permission` require a valid `Authorization: Bearer <token>` header while `/readiness` and `/metrics` stay open, and make the authentication decision a required constructor argument that fails closed when left unset. Implements spec 057 Desired Behaviors 1-5 and Acceptance Criteria 1-8. The frozen-contract document is updated by the sibling prompt `2-spec-057-frozen-contract-authentication.md`, not here.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1; `interactive/` is a package in that root module, not a separate module, and it has no Makefile of its own). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private and named after the interface with a lowercased first letter; `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-library-guide.md` — public-API compatibility for a library consumed from other repositories.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), suite timeout, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; `no-fmt-errorf`, `no-bare-return-err`, `no-context-background-in-business-logic`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-http-handler-refactoring-guide.md` — handler and middleware placement and shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every exported type, field and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80 lines / 50 statements, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md` — logging levels; nothing on this path may log the token.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape, the frozen preamble, and the conventional-prefix rule.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing (repo-relative):
- `interactive/service.go` — the `Service` interface, `NewService` (four parameters), `NewServiceWithPermissions` (five), the private `service` struct and `Handler()`. This is the file the new constructor parameter and the middleware wrap are added to.
- `interactive/prompt.go` — `promptHandler`: the route the gate must protect, and the precedent for how a route answers a fixed status.
- `interactive/permission-handler.go` — the conditionally registered route, so the gate is shown to cover it too.
- `interactive/readiness.go` — the exempt route and its frozen body strings; the gate must not touch them.
- `interactive/session-cache.go` — `sessionEntry.Prompt` emits the `turn start id=<id>` / `turn end id=<id>` pair while holding the session lock. Acceptance Criterion 5 measures that a refused request emits neither.
- `interactive/session-id.go` — the package's precedent for a package-level `var` (a compiled regexp) and for a `const` header name (`sessionHeader`).
- `interactive/service_test.go` — `captureStderr`, `newTestServer`, `postPrompt`, and the single-sourced frozen-contract table (`contractEntries` / `routeEntries` / `promptEntries` / `runContractTable`). Every one of these call sites must keep compiling and every existing row must keep its status.
- `interactive/permission_test.go` — `newPermissionTestServer` and its helpers; its call site must keep compiling too. Do not redefine any helper it already declares.
- `interactive/interactive_suite_test.go` — the Ginkgo suite, the 60 s timeout, the `//go:generate ... counterfeiter ... -generate` directive.
- `docs/interactive-service.md` — the frozen contract this change amends. READ it to know which existing rows must keep their statuses; do NOT edit it in this prompt.
- `docs/agent-network-security.md` — the network posture reference.
- `specs/in-progress/057-interactive-service-authentication.md` — the spec this prompt implements.

Load-bearing facts, verified against this working tree. Do not re-derive these, do not contradict them:

1. `interactive/service.go` currently declares, in this order: the `Service` interface (`Handler() http.Handler`, `Run(ctx context.Context) error`), `NewService(sessions agentlib.SessionFactory, listen string, providerBaseURL string, registry *prometheus.Registry) Service`, `NewServiceWithPermissions(sessions agentlib.SessionFactory, listen string, providerBaseURL string, registry *prometheus.Registry, permissions PermissionRegistry) Service`, the private `service` struct with fields `cache *sessionCache`, `listen string`, `providerBaseURL string`, `registry *prometheus.Registry`, `permissions PermissionRegistry`, then `Handler()` and `Run(ctx)`. `Handler()` builds `http.NewServeMux()` and registers `/readiness`, `/metrics`, `/prompt`, and `/permission` when `s.permissions != nil`. `Run(ctx)` uses `libhttp.NewServer(s.listen, s.Handler())`.
2. The ONLY call sites of these two constructors in this repository are in tests: `interactive/service_test.go` (`newTestServer`, and the `Describe("Run")` spec) and `interactive/permission_test.go` (`newPermissionTestServer`). There is NO production call site in this repo — the two consumer binaries live in other repositories and are explicitly out of this spec's scope. `grep -rn 'interactive.NewService' --include='*.go' .` confirms this; re-run it before you start.
3. `interactive/service_test.go` already declares, in `package interactive_test`: `captureStderr(fn func()) string`, `newTestServer(factory agentlib.SessionFactory, providerBaseURL string) *httptest.Server`, `postPrompt(serverURL, sessionID, body string) (int, error)`, `strptr`, `contractObserver`, `contractBackend`, `newMockBackend`, `runContractTable`, `contractEntries`, `routeEntries`, `promptEntries`. REUSE `captureStderr` and `newMockBackend`; do not redefine any of them.
4. `interactive/permission_test.go` already declares, in `package interactive_test`: `permissionTool`, `permissionPrompt`, `panicPrompt`, `permissionFactory`, `permissionSession`, `pendingDTO`, `promptResult`, `verdictBody`, `newPermissionTestServer`, `getPermissions`, `postVerdict`, `postPromptFull`, `startPrompt`, `waitPending`, `postRaw`. Do not redefine any of them; do not change `newPermissionTestServer`'s signature.
5. `interactive/service_test.go` sets `flag.Set("logtostderr", "true")` and `flag.Set("v", "2")` in `BeforeSuite`, which is what makes the `turn start` line visible to `captureStderr`. Acceptance Criterion 5 depends on that being already true — do not change `BeforeSuite`.
6. `github.com/bborbe/errors` is at `v1.6.1` and exports `New(ctx, message string) error`, `Errorf(ctx, format string, ...) error`, `Wrap(ctx, err error, message string) error`, `Wrapf(ctx, err error, format string, ...) error`, `Join`, `Is`, `As`, `Cause`.
7. `context.Background()` appears ZERO times in this repo's non-test code (`grep -rn 'context.Background()' --include='*.go' . | grep -v _test.go` is empty) — it is a repo rule. Every error-returning function that needs a context takes one as its first parameter; `interactive/readiness.go`'s `dialAddress(ctx context.Context, raw string) (string, error)` and `claude/claude-runner.go`'s `resolveConfigDir(ctx context.Context, config ClaudeRunnerConfig) (string, error)` are the local precedents.
8. `github.com/bborbe/http` is already imported by `interactive/service.go` as `libhttp`; `crypto/subtle`, `os` and `strings` are standard library and add no dependency. Do NOT add any new module requirement — the spec forbids a new dependency for the comparison.
9. `.golangci.yml` enables govet, errcheck, staticcheck, unused, revive, gosec, gocyclo, depguard, dupl, funlen (80 lines / 50 statements), gocognit (20), nestif (4), maintidx (20), errname, unparam, bodyclose, forcetypeassert, asasalint, prealloc. It does NOT enable `gochecknoglobals`, so a package-level `var AuthDisabled` is acceptable — and `interactive/session-id.go`'s `var sessionIDPattern = regexp.MustCompile(...)` is the in-package precedent.
10. The `interactive` test package is the external `package interactive_test`; the suite timeout is 60 s; tests are exempt from the production `no-raw-go-func` rule and already use raw `go func(){...}()`.
11. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`. No `git` command is used in this prompt's verification regardless, because the daemon does not check verification exit codes.
12. `make precommit` at the repository root runs `ensure format generate test check addlicense` (the root Makefile; `interactive/` has none). `make test` runs the whole module with `-race`. Both are run from `/workspace`.
13. Lint limits that bind the new files: funlen 80 lines / 50 statements, gocognit 20, nestif 4, golines line length 100.
</context>

<requirements>

## 1. The authentication seam — new file `interactive/auth.go`

Create `interactive/auth.go` (`package interactive`, BSD license header matching the sibling files, no `init()`, no package-level mutable state).

### 1a. Constants

```go
// authTokenEnv is the environment variable the service reads its bearer token from.
// The token is delivered as a runtime-only pod secret, so it is read from the process
// environment and never from a file, a manifest or a source literal.
const authTokenEnv = "INTERACTIVE_AUTH_TOKEN"

// authorizationHeader is the request header carrying the caller's bearer credential.
const authorizationHeader = "Authorization"

// bearerPrefix is the authorization scheme this service accepts.
const bearerPrefix = "Bearer "
```

### 1b. The decision value

```go
// Auth is a service's authentication decision: either the bearer token every gated
// request must carry, or an explicit opt-out.
//
// The zero value is NOT a usable default — it refuses every gated request. A service
// built with the zero value authenticates nothing and serves only the exempt routes,
// which is the fail-closed outcome: a decision left unmade cannot silently open the
// surface. Build a real decision with NewAuthToken or AuthFromEnv, or state the
// opt-out with AuthDisabled.
type Auth struct {
	token    string
	disabled bool
}
```

Both fields are unexported, so the only values that exist are the ones this file builds and the zero value. An external test package can still write `interactive.Auth{}` (an empty composite literal names no field), which is exactly what the zero-value test needs.

### 1c. The two ways to build a decision, and the opt-out

```go
// NewAuthToken returns the Auth that requires every gated request to present token as
// its bearer credential. An empty token is not a usable credential: it yields the same
// fail-closed value as the zero Auth.
func NewAuthToken(token string) Auth {
	return Auth{token: token}
}

// AuthDisabled is the explicit opt-out: a service built with it serves every route
// without authentication.
//
// It exists so that a consumer which is deliberately unexposed states that choice in
// one visible, greppable line rather than inheriting a default. Passing it on a pod that
// is later exposed serves that surface unauthenticated — the residual risk this design
// accepts — so grep for it before putting anything in front of the port.
var AuthDisabled = Auth{disabled: true}

// AuthFromEnv reads the bearer token from the process environment and returns the Auth
// that requires it.
//
// It returns an error when the variable is unset or empty, so a service that cannot
// authenticate fails to start rather than serving unauthenticated. The token value is
// never logged, never returned inside the error and never written anywhere.
func AuthFromEnv(ctx context.Context) (Auth, error) {
	token := os.Getenv(authTokenEnv)
	if token == "" {
		return Auth{}, errors.Errorf(ctx, "environment variable %s is unset", authTokenEnv)
	}
	return NewAuthToken(token), nil
}
```

The error message names the environment variable and nothing else. It must NOT include the token, a prefix of the token, or its length.

`AuthFromEnv` takes `ctx` because the repo forbids `context.Background()` in non-test code and every error-returning function here takes a context — see context fact 7. It is a plain function rather than a `New*` constructor because the repo's `New*` functions are pure composition with no I/O, and reading the environment is I/O.

### 1d. The comparison

```go
// allows reports whether header carries this decision's bearer credential.
//
// The comparison is constant-time: subtle.ConstantTimeCompare returns 0 when the
// lengths differ and compares every byte otherwise, so a caller cannot recover the
// token's length or prefix from response timing. The scheme prefix is not secret and is
// compared with an ordinary prefix test.
func (a Auth) allows(header string) bool {
	if a.disabled {
		return true
	}
	if a.token == "" {
		return false
	}
	if !strings.HasPrefix(header, bearerPrefix) {
		return false
	}
	presented := strings.TrimPrefix(header, bearerPrefix)
	return subtle.ConstantTimeCompare([]byte(presented), []byte(a.token)) == 1
}
```

Import `crypto/subtle` from the standard library. Do NOT write a hand-rolled byte loop, `==` on the strings, or `hmac.Equal` — the spec fixes the constant-time property and forbids a new dependency.

### 1e. The exemption and the middleware

```go
// authExempt reports whether path is served without authentication.
//
// /readiness and /metrics are exempt deliberately: a kubelet readiness probe and a
// Prometheus scrape cannot present a bearer token without the token being written into
// the pod spec's probe stanza and the scrape configuration, which multiplies the
// secret's exposure and can wedge the pod's Ready state. Neither route grants execution
// or reveals a credential. Every other path — including one registered after this
// change — is gated.
func authExempt(path string) bool {
	switch path {
	case "/readiness", "/metrics":
		return true
	default:
		return false
	}
}

// requireAuth wraps next in the service's authentication gate.
//
// Because the gate wraps the whole router, an unauthenticated request is refused before
// the wrapped handler runs: before the body is read, before a session is constructed and
// before the session lock is taken, so a refused request can neither occupy a session
// nor appear in the turn-boundary log pair.
func (s *service) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authExempt(r.URL.Path) || s.auth.allows(r.Header.Get(authorizationHeader)) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
```

The exemption is an exact path match inside one function, not a set the caller can extend and not a prefix test: a route registered after this change is gated by default, which is the property Desired Behavior 1 asks for. `WWW-Authenticate: Bearer` accompanies the `401` because RFC 9110 requires it on a 401 from a bearer-token-protected resource; the response body is not part of the contract, only the status is.

This file must log nothing. There is no `glog` import in it.

## 2. The constructor and the gate — edit `interactive/service.go`

Make exactly these changes; do not restructure the file.

1. Add an `auth Auth` field to the private `service` struct, last, documented as:

```go
	// auth is the authentication every gated route requires; the zero value refuses
	// every gated request.
	auth Auth
```

2. Both constructors take the decision as their fifth parameter — immediately after `registry` and before `permissions` — so the parameter sits in the same position in both and a reader cannot mistake it for optional:

```go
// NewService creates the interactive session service.
//
// sessions supplies one conversation per session id; listen is the address the service
// binds; providerBaseURL is the endpoint the readiness probe dials (empty means the
// check is skipped and reported as such); registry is the Prometheus registry the
// metrics route gathers from — a parameter rather than a library singleton, so each
// binary keeps its own metrics identity; auth is the authentication every gated route
// requires, and the zero Auth refuses every gated request. The permission endpoint is
// not served by this constructor; use NewServiceWithPermissions to serve it.
func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
) Service {
	return NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, nil)
}
```

```go
// NewServiceWithPermissions creates the interactive session service with the
// permission endpoint enabled.
//
// permissions is the registry the endpoint serves and the decider the sessions built by
// the caller's factory consult; the caller constructs it first and passes the one
// instance to both, which is what makes the endpoint and the sessions resolve through
// the same registry. A nil permissions is invalid here — use NewService for that. auth
// is the authentication every gated route requires, and the zero Auth refuses every
// gated request.
func NewServiceWithPermissions(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
	permissions PermissionRegistry,
) Service {
	return &service{
		cache:           newSessionCache(sessions),
		listen:          listen,
		providerBaseURL: providerBaseURL,
		registry:        registry,
		permissions:     permissions,
		auth:            auth,
	}
}
```

3. `Handler()` returns the router wrapped in the gate, so the gate covers every registered route including the conditionally registered one:

```go
func (s *service) Handler() http.Handler {
	router := http.NewServeMux()
	router.Handle("/readiness", s.readinessHandler())
	router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	router.Handle("/prompt", s.promptHandler())
	if s.permissions != nil {
		router.Handle("/permission", s.permissionHandler())
	}
	return s.requireAuth(router)
}
```

4. Update the doc comments on `Service.Handler` and on `service.Handler` to say that every route except `/readiness` and `/metrics` requires the configured bearer token, and why those two are exempt. `Run(ctx)` is unchanged.

5. Change nothing else in this file. Do NOT add a variadic option, a config struct, an interface parameter or an opt-out flag: the parameter is a plain `Auth` value and the spec's whole point is that a consumer must name its decision.

## 3. Update the three existing constructor call sites

The constructor now takes a fifth parameter, so three existing call sites no longer compile. Every one of them is a test helper that drives the frozen contract, and every one of them must keep asserting exactly the statuses it asserts today — so each passes `interactive.AuthDisabled`, which is the explicit statement "this test server serves unauthenticated".

1. `interactive/service_test.go`, `newTestServer` — change the construction call to `interactive.NewService(factory, ":0", providerBaseURL, prometheus.NewRegistry(), interactive.AuthDisabled)`. Keep the helper's signature `newTestServer(factory agentlib.SessionFactory, providerBaseURL string) *httptest.Server` unchanged, and keep its doc comment, adding one clause that it builds an explicitly unauthenticated service.
2. `interactive/service_test.go`, the `Describe("Run")` spec — add `interactive.AuthDisabled` as the fifth argument to its `interactive.NewService(...)` call.
3. `interactive/permission_test.go`, `newPermissionTestServer` — add `interactive.AuthDisabled` as the fifth argument (before `permissions`) to its `interactive.NewServiceWithPermissions(...)` call. Keep the helper's signature unchanged.

Do not change any other line of `interactive/service_test.go` or `interactive/permission_test.go`. In particular every row of `contractEntries` / `routeEntries` / `promptEntries` and every existing `It` keeps its current status assertions, because an `AuthDisabled` service behaves exactly as the service behaves today.

## 4. Tests — new file `interactive/auth_test.go`

Create `interactive/auth_test.go` in the external `package interactive_test`, with the BSD license header. It must reuse `captureStderr` and `newMockBackend` from `interactive/service_test.go` and must not redefine any helper that already exists in the package.

### 4a. Fixtures and helpers

```go
// authTestToken is the bearer token the auth tests configure. It is a placeholder,
// never a real credential, and it is never written next to the scheme prefix in this
// file, so the source-hygiene grep finds no `Bearer <token>` literal.
const authTestToken = "test-token-value"

// authWrongToken is a token of the same shape that is not the configured one.
const authWrongToken = "wrong-token-value"

// authEnv is the environment variable the service reads its token from. It is spelled
// out here rather than imported, because the name is part of the contract.
const authEnv = "INTERACTIVE_AUTH_TOKEN"
```

Helpers (all in this file):

```go
// newAuthMockFactory returns a counterfeiter factory whose sessions answer every prompt
// with "session-result". A factory written as a bare &mocks.SessionFactory{} has no
// CreateReturns, so Create returns a nil Session and the first turn that reaches the
// prompt route panics — every fixture that lets a request through the gate uses this.
func newAuthMockFactory() *mocks.SessionFactory {
	session := &mocks.Session{}
	session.PromptReturns("session-result", nil)
	factory := &mocks.SessionFactory{}
	factory.CreateReturns(session)
	return factory
}
```

- `newAuthTestServer(auth interactive.Auth, factory agentlib.SessionFactory) *httptest.Server` — builds `interactive.NewService(factory, ":0", "", prometheus.NewRegistry(), auth)` and wraps `svc.Handler()` in `httptest.NewServer`.
- `newAuthPermissionTestServer(auth interactive.Auth, permissions interactive.PermissionRegistry, factory agentlib.SessionFactory) *httptest.Server` — builds `interactive.NewServiceWithPermissions(factory, ":0", "", prometheus.NewRegistry(), auth, permissions)` and wraps `svc.Handler()` in `httptest.NewServer`. This is the fixture the route-policy table needs, because `/permission` only exists on a permission-enabled service.
- `requestWithAuth(serverURL, method, path, body, authorization string) (int, error)` — builds the request (a `nil` reader when `body` is empty), sets `Authorization` to `authorization` **only when `authorization` is not empty** (an empty value means no header at all, which is the "absent header" case), sends it with `http.DefaultClient`, drains and closes the body, and returns the status. Set the `X-Session-Id` header to `"abc"` for every request so the prompt route's own validation cannot confuse a row.
- `postPromptAuth(serverURL, sessionID, body, authorization string) (int, error)` — the POST-to-`/prompt` convenience wrapper over `requestWithAuth`.

Never write a source literal of the form `"Bearer <8-or-more-token-chars>"`. Build every header value as `"Bearer " + <token-const>` (or `"Basic " + authTestToken` for the malformed case) so the source-hygiene grep in `<verification>` stays clean.

### 4b. The authentication specs

Add `var _ = Describe("Authentication", ...)` with one `It` per row. Name each `It` EXACTLY as given — the verification greps these names.

Every fixture that can let a request through the gate is built with `newAuthMockFactory()`; a bare `&mocks.SessionFactory{}` is safe ONLY on a row whose request is refused before it reaches a session (rows 1, 2, 4, 5, 6), because that factory's `Create` returns a nil `Session`.

1. **"rejects a request with no Authorization header"** (AC1) — build a server with `interactive.NewAuthToken(authTestToken)` over `newAuthMockFactory()`; `postPromptAuth(server.URL, "abc", "hello", "")` returns `401`.
2. **"rejects a request with a wrong token"** (AC2) — same server; `postPromptAuth(server.URL, "abc", "hello", "Bearer "+authWrongToken)` returns `401`.
3. **"serves a request with the correct token"** (AC3) — build the server over `newAuthMockFactory()`; `postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)` returns `200`. This is the control: without it a build that refuses everything passes rows 1 and 2.
4. **"rejects a malformed Authorization header"** (spec Failure Modes: wrong scheme, no value) — assert `401` for each of `"Basic "+authTestToken` (wrong scheme) and `"Bearer"` (scheme with no value) on the same server.
5. **"rejects a token that is a prefix of the correct one"** (spec Failure Modes: correct in value, wrong in length) — assert `401` for `"Bearer "+authTestToken[:len(authTestToken)-1]`.
6. **"rejects a zero-value auth decision"** (AC6) — build a server with `interactive.Auth{}` (the zero value, written as an empty composite literal) and assert `postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)` returns `401`. Add one line to the spec's body comment making the point explicit: the zero value is not a "serve everything" default, it is the fail-closed state.
7. **"serves unauthenticated when auth is explicitly disabled"** (AC7) — build a server with `interactive.AuthDisabled` over `newAuthMockFactory()` and assert `postPromptAuth(server.URL, "abc", "hello", "")` returns `200`.
8. **"refuses a request before any session work"** (AC5) — build an authenticated server over a `factory := newAuthMockFactory()` that the spec also holds a reference to. Capture stderr around a refused request and around an authenticated one:

```go
		var refusedStatus, servedStatus int
		refusedOut := captureStderr(func() {
			refusedStatus, _ = postPromptAuth(server.URL, "abc", "hello", "")
		})
		servedOut := captureStderr(func() {
			servedStatus, _ = postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)
		})

		Expect(refusedStatus).To(Equal(http.StatusUnauthorized))
		Expect(strings.Count(refusedOut, "turn start")).To(Equal(0))
		Expect(servedStatus).To(Equal(http.StatusOK))
		Expect(strings.Count(servedOut, "turn start")).To(Equal(1))
		Expect(factory.CreateCallCount()).To(Equal(1))
```

   The `CreateCallCount() == 1` assertion is the third half of the evidence: the refused request not only logged no turn, it never built a session.

9. **"gates every route as recorded"** (AC4) — a `DescribeTable` driven by `requestWithAuth`. Each row builds its own fixture inside the table body and closes it with `defer server.Close()`:

```go
		server := newAuthPermissionTestServer(
			interactive.NewAuthToken(authTestToken),
			interactive.NewPermissionRegistry(),
			newAuthMockFactory(),
		)
		defer server.Close()

		status, err := requestWithAuth(server.URL, method, path, body, authorization)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(expected))
```

   Rows, exactly these six:

| Method | Path | Body | Authorization | Expected |
|---|---|---|---|---|
| `GET` | `/readiness` | `""` | `""` | `200` |
| `GET` | `/metrics` | `""` | `""` | `200` |
| `POST` | `/prompt` | `"hello"` | `""` | `401` |
| `GET` | `/permission` | `""` | `""` | `401` |
| `POST` | `/prompt` | `"hello"` | `"Bearer "+authTestToken` | `200` |
| `GET` | `/permission` | `""` | `"Bearer "+authTestToken` | `200` |

   Use `prometheus.NewRegistry()` inside the fixture (an empty registry still answers `/metrics` with `200`), and `PROVIDER_BASE_URL` `""` so `/readiness` answers `200` without dialling.

### 4c. The environment specs

Add `var _ = Describe("Auth from environment", ...)` with:

1. **"reads the token from the process environment"** — set the env var with `os.Setenv(authEnv, authTestToken)` and `DeferCleanup(os.Unsetenv, authEnv)`; call `interactive.AuthFromEnv(context.Background())`; expect no error; build a server with the returned `Auth` and assert `postPromptAuth(server.URL, "abc", "hello", "Bearer "+authTestToken)` returns `200` and `postPromptAuth(server.URL, "abc", "hello", "")` returns `401`.
2. **"fails when the token is not set"** — `os.Unsetenv(authEnv)` with `DeferCleanup` restoring whatever was there; call `interactive.AuthFromEnv(context.Background())`; expect an error AND expect the returned `Auth` to be the zero value (`Expect(auth).To(Equal(interactive.Auth{}))`), which is what makes the caller's "fail to start" path meaningful. Assert the error message does NOT contain the token: `Expect(err.Error()).NotTo(ContainSubstring(authTestToken))`.

`context.Background()` is fine in a test file (context fact 7 applies to non-test code).

## 5. README and CHANGELOG

**`README.md`** — the `interactive/` row currently reads "Shared interactive HTTP surface — readiness, metrics, prompt intake and the permission endpoint over a per-session cache of long-lived conversations". Extend it so it also says the prompt and permission routes require a bearer token while readiness and metrics stay open. Keep the edit to that one row.

**`CHANGELOG.md`** — the newest section is `## v0.93.1` and no `## Unreleased` section exists. Create `## Unreleased` immediately after the SemVer preamble block (after the last MAJOR/MINOR/PATCH bullet and its blank line) and directly above `## v0.93.1` — never above any line of the preamble. Add ONE bullet with the `feat:` prefix, per `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`: name that every route of the interactive service is now either authenticated with a bearer token or explicitly exempt, name the two exempt routes and why, name that the constructor now takes an explicit authentication decision which fails closed when left at its zero value, name the `AuthDisabled` opt-out, name the `NewAuthToken` exported constructor that builds a token from a known value, name the constant-time comparison, and state plainly that the constructor signature change is breaking for the two consumer repositories.

## 6. Scope containment

Create or edit ONLY these files:
- `interactive/auth.go` (new)
- `interactive/auth_test.go` (new)
- `interactive/service.go` (the `auth` field, the two constructor signatures, the `Handler()` wrap, the doc comments)
- `interactive/service_test.go` (the two `interactive.NewService` call sites ONLY)
- `interactive/permission_test.go` (the one `interactive.NewServiceWithPermissions` call site ONLY)
- `README.md` (the `interactive/` row ONLY)
- `CHANGELOG.md` (the new `## Unreleased` section ONLY)

Do NOT touch `interactive/prompt.go`, `interactive/permission-handler.go`, `interactive/permission.go`, `interactive/readiness.go`, `interactive/session-cache.go`, `interactive/session-id.go`, `interactive/claude-session_test.go`, `docs/`, `claude/`, `pi/`, `mocks/`, or any other package. In particular do NOT edit `docs/interactive-service.md` — that is the sibling prompt `2-spec-057-frozen-contract-authentication.md`. Do NOT add a metrics family, a rate limiter, a token rotation mechanism, an allowlist, or a client for the gated routes; the spec asks for none of them.
</requirements>

<constraints>
- **The gate covers every registered route except `/readiness` and `/metrics`.** The two exemptions are a recorded decision (a kubelet probe and a Prometheus scrape cannot carry a token without the token being written into the pod spec and the scrape configuration), not an oversight. The exemption is an exact path match in one function so a route registered later is gated by default.
- **An unauthenticated request is refused before any work.** The gate wraps the whole router, so refusal precedes the body read, the session construction and the session lock. A refused request must emit neither `turn start` nor `turn end`, and must not call `SessionFactory.Create`.
- **The comparison is constant-time.** Use `crypto/subtle.ConstantTimeCompare`. No hand-rolled byte loop, no `==`, no `hmac.Equal`. **No new dependency** for the comparison — the standard library is sufficient (spec Constraint).
- **The constructor requires an explicit auth decision, and the zero value fails closed.** Passing nothing is a compile error; passing the zero `Auth` refuses every gated request rather than serving everything. Do NOT add a variadic option, a config struct, an interface parameter, an env fallback inside the constructor, or an opt-out flag to either constructor.
- **`AuthDisabled` is a deliberate, greppable opt-out and must behave as declared.** It is the operator's chosen design — fail-closed by explicitness — so it is tested, not assumed. Do not make it the default, do not remove it, and do not rename it.
- **This repository owns the token's environment-variable name.** The spec fixes the shape (a bearer token delivered as a runtime-only pod secret) but not the identifier, so `INTERACTIVE_AUTH_TOKEN` is this repo's decision. The sibling attention-store mechanism's name could not be checked from here. The deployment repository follows this name, not the reverse. If the name must change later it is one constant in `auth.go`, one in `auth_test.go`, and one sentence in the frozen contract — so do not spend effort trying to discover a canonical name.
- **The token exists only as a runtime-injected value.** It is read from `INTERACTIVE_AUTH_TOKEN` at startup and appears in no source literal, no manifest and no image layer. An unset token is an error, so a service that cannot authenticate fails to start rather than serving unauthenticated.
- **The token is never logged, at any verbosity, in any error path, or in any test failure output.** Evidence for anything token-shaped is a key name, a status or a count — never a value. Do not log the `Authorization` header, the presented credential or the configured token. `interactive/auth.go` imports no `glog`.
- **The frozen contract's existing behaviour must not change for an authenticated caller.** Session-id validation, the absent-vs-empty distinction, the 1 MiB body cap and its truncate-not-reject semantics, the readiness body strings, the two-level locking and the turn-boundary log pair all keep their current contract. Every existing row in `contractEntries` / `routeEntries` / `promptEntries` keeps its current status assertion, because the test helpers now build an `AuthDisabled` service.
- **The permission endpoint stays conditionally registered.** It is served only when a permissions registry is supplied; this change does not make it unconditional. The gate sits in front of it, it does not replace the condition.
- **The library must keep building for both consumers.** One consumer does not wire a permissions registry, so the change must be expressible at its call site without it adopting the permission endpoint — which is what the `auth` parameter sitting before `permissions` in both constructors achieves. The two consumer repositories are NOT updated here.
- **Error handling** uses `github.com/bborbe/errors` — `errors.Errorf` / `errors.New` / `errors.Wrap` / `errors.Wrapf`, never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in non-test code.
- **Code conventions** per `docs/dod.md`: exported items carry doc comments; Interface → Constructor → Struct → Method; no `init()`, no package-level mutable state; factory functions are pure composition (no conditionals, no I/O).
- **Logging.** The auth path adds no logging of its own and no metrics family. The spec asks for neither.
- **Tests.** Ginkgo v2 / Gomega in the external `interactive_test` package; counterfeiter mocks only, never hand-written; coverage for the new code >= 80%. Reuse `captureStderr` and `newMockBackend`; redefine nothing that already exists in the package.
- **Repository hygiene.** `README.md`'s `interactive/` row and `CHANGELOG.md`'s new `## Unreleased` section are updated in this change. The CHANGELOG preamble block is frozen: nothing is inserted above or inside it.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; gocognit 20; nestif 4.
- **Hand-off, not this prompt.** Wiring the token into a deployment (the pod secret, the probe stanza, the scrape configuration), bumping a consumer's dependency, and updating the two consumer call sites all land in their own repositories through their own flows. This repository only exposes the mechanism.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable; none needs host tooling, Docker, a cluster or a second repository.

```bash
# 1. Both constructors require an auth argument (and it sits before permissions).
grep -n -A8 'func NewService(' interactive/service.go
grep -n -A9 'func NewServiceWithPermissions(' interactive/service.go
# Each must show an `auth Auth` parameter.

# 2. The delegating call passes the decision through.
grep -n 'return NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, nil)' interactive/service.go
# Must match exactly once.

# 3. The explicit opt-out is declared and greppable.
grep -rn 'AuthDisabled' interactive/
# Must return >= 1 line.

# 4. The token is read from the process environment, not a literal.
grep -n 'os.Getenv' interactive/auth.go
grep -n 'authTokenEnv' interactive/auth.go
# Each must return >= 1 line.

# 5. The comparison is constant-time.
grep -n 'subtle.ConstantTimeCompare' interactive/auth.go
# Must return >= 1 line.

# 6. The gate wraps the whole router.
grep -n 'return s.requireAuth(router)' interactive/service.go
# Must match exactly once.

# 7. Source hygiene: no bearer literal anywhere in the package.
! grep -rqE 'Bearer [A-Za-z0-9_-]{8,}' interactive/
# Must print nothing and exit 0. Absence assertion — a bare `grep -c` prints 0 but EXITS 1
# on a zero count, which would fail the step it was meant to pass.

# 8. No context.Background() in the new production file.
! grep -n 'context.Background()' interactive/auth.go
# Must print nothing.

# 9. The auth path logs nothing.
! grep -n 'glog' interactive/auth.go
# Must print nothing.

# 10. The package builds and every named spec ran.
go test -mod=mod -race -v ./interactive/ > /tmp/interactive-auth-test.log 2>&1
# Must exit 0. Each of these names must appear in the log:
grep -E "rejects a request with no Authorization header|rejects a request with a wrong token|serves a request with the correct token|rejects a malformed Authorization header|rejects a token that is a prefix of the correct one|rejects a zero-value auth decision|serves unauthenticated when auth is explicitly disabled|refuses a request before any session work|gates every route as recorded|reads the token from the process environment|fails when the token is not set" /tmp/interactive-auth-test.log

# 11. Every route-policy row ran.
grep -c "gates every route as recorded" /tmp/interactive-auth-test.log
# Must be >= 6 (one per table row).

# 12. The token never appears in captured test output.
! grep -q 'test-token-value' /tmp/interactive-auth-test.log
# Must print nothing. Absence assertion — `grep -c` prints 0 but EXITS 1 on a zero count,
# which would fail the very step it was meant to pass.

# 13. Coverage for the new code.
go test -coverprofile=/tmp/cover-auth.out -mod=mod ./interactive/... && go tool cover -func=/tmp/cover-auth.out | grep -E 'auth.go|total'
# Every function in interactive/auth.go must be >= 80%.

# 14. The whole module's tests.
make test
# Must exit 0.

# 15. Final validation.
make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then re-run `make precommit` once more.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **Two prompts, per the spec's § Suggested Decomposition.** This one establishes the behaviour and covers Desired Behaviors 1-5 and Acceptance Criteria 1-8; the sibling `2-spec-057-frozen-contract-authentication.md` is doc-only and covers AC9. Acceptance Criterion 10 is not prompt-scoped — it is a host-side, post-deploy check on a tag that does not exist yet, and belongs on the spec's Verification ladder, not in a prompt. No scenario prompt is emitted: httptest reaches every behaviour in this change, so the `docs/rules/scenario-writing.md` four-condition test fails on its first condition.
- **Open question 1 — the environment variable name is not fixed by the spec.** The spec says "a runtime-injected value" and "read from the process environment" but never names the variable. This prompt fixes it at `INTERACTIVE_AUTH_TOKEN`. The sibling attention-store mechanism's name was not available to check against; if the operator's settled name differs, change the `authTokenEnv` constant, the `authEnv` test constant, and the doc sentence in the sibling prompt together.
- **Open question 2 — "rejects a zero-value auth decision" is read as "the zero value fails closed".** The spec's AC6 pairs a constructor-arity grep with this test row and says the decision "cannot be omitted or left at its zero value". This prompt therefore makes `Auth{}` refuse every gated request (401) rather than making the constructor panic or return an error. That keeps both constructors' signatures free of an error return and keeps `AuthDisabled` the only way to serve unauthenticated. If the reviewer reads AC6 as "the constructor must reject the zero value", the change is a constructor error return, which ripples into all three test call sites and into both consumer repositories.
- **Open question 3 — `WWW-Authenticate: Bearer` is an addition beyond the spec's letter.** RFC 9110 requires the header on a 401 from a bearer-protected resource. It is one line and the response body is explicitly not part of the contract. Delete the `w.Header().Set(...)` line if the reviewer judges it out of scope.
- **Open question 4 — `NewAuthToken` is an exported constructor the spec does not name.** It exists so the middleware tests can configure a known token without mutating the process environment, and it is the pure-composition half of the seam (`AuthFromEnv` is the I/O half). If the reviewer prefers a single env-only entry point, the tests move to `os.Setenv` and `NewAuthToken` becomes unexported.
- **The three existing call sites pass `AuthDisabled` on purpose.** `newTestServer`, the `Run` spec and `newPermissionTestServer` drive the frozen-contract table, whose rows must keep their current statuses. Passing `AuthDisabled` there is the honest statement of what those servers are, and it is also what makes the pre-existing `GET /permission` → `404` row survive: with a real token in front, that row's observable becomes 401 without a header and 404 with one. The sibling doc prompt records that distinction.
- **Status precedence is deliberate and worth a reviewer's eye.** Because the gate is outermost, an unauthenticated `GET /prompt` is `401` rather than `405`, and an unauthenticated `GET /permission` on a plain `NewService` is `401` rather than `404`. This is what Desired Behavior 2 ("refused before any work happens") requires, and it is why the frozen contract's gated rows need the "with a valid token" qualifier that the sibling prompt adds.
- **The changelog prefix can only express a minor bump.** `feat:` is the only prefix in `changelog-guide.md` that fits a new capability, while the constructor signature change is API-breaking (MAJOR by the repo's own SemVer preamble). The bullet says so in prose; the version arithmetic is the release tooling's call, not this prompt's.
- **Operator-executable rung (not run in the container).** The spec's Verification ladder additionally asks for the deployed-pod image tag, the no-token / wrong-token / correct-token curl triple, and a count-only token search across the built image and the committed manifests. All of those need a cluster, a built image and the consumer release, none of which exist inside the container, and none belongs in this prompt.
