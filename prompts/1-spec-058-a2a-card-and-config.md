---
status: draft
spec: [058-interactive-service-a2a-endpoint]
created: "2026-10-05T20:20:00Z"
branch: dark-factory/interactive-service-a2a-endpoint
---

# A2A agent card, public-address configuration and the card route

<summary>
- The interactive service now serves a standards-compliant A2A Agent Card at the well-known path `/.well-known/agent-card.json`, as JSON.
- The card is readable **without** a credential, deliberately — the same posture as a public `robots.txt` — because discovery is public by design; it is one more literal path in the service's existing auth-exemption switch.
- The address the card advertises is **configuration, not a constant**: it is never derived from the listen address or the request's Host header, so the container-local `0.0.0.0` can never leak into the card.
- A new fail-closed accessor reads that address from the `A2A_PUBLIC_URL` environment variable and returns an error naming the variable when it is unset or empty, so a service that cannot advertise a real endpoint fails to start instead of advertising a wrong one.
- Both service constructors take the public address as a new parameter, so a consumer cannot build the service without stating it.
- The card advertises exactly one A2A interface (the JSON-RPC binding) whose URL is the configured public address verbatim.
- The card carries no credential, no secret and no derived address.
- A new module dependency provides the Agent Card type; the wire format is the SDK's, not hand-rolled.
- The credential path stays confined to the existing auth file — this change adds no second gate and no new credential.
- Existing routes, statuses and behaviour are unchanged; the existing frozen-contract rows keep passing.

</summary>

<objective>
Give the `interactive` service an A2A discovery surface: a public Agent Card at `/.well-known/agent-card.json` whose advertised endpoint is the externally reachable address supplied as configuration, so a standards-compliant A2A client can discover the service without a credential. Implements spec 058 Desired Behaviors 1 and 2 and Acceptance Criteria 1, 2 and 6. The JSON-RPC execution endpoint itself is a sibling prompt (`2-spec-058-a2a-jsonrpc-handler.md`), not this one.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1; `interactive/` is a package in that root module, not a separate module, and it has no Makefile of its own). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private and named after the interface with a lowercased first letter; `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-library-guide.md` — public-API compatibility for a library consumed from other repositories; the constructor signature change is a breaking change for the consumers.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; `no-fmt-errorf`, `no-bare-return-err`, `no-context-background-in-business-logic`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-http-handler-refactoring-guide.md` — handler and middleware placement and shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every exported type, field and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md` — logging levels; nothing on this path may log a credential.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), suite timeout, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80 lines / 50 statements, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing (repo-relative):
- `interactive/service.go` — the `Service` interface, `NewService`, `NewServiceWithPermissions`, the private `service` struct, `Handler()` and `Run(ctx)`. This is the file the new constructor parameter and the new route registration are added to.
- `interactive/auth.go` — `authTokenEnv`, `AuthFromEnv`, `authExempt` and `requireAuth`. This file is the exemplar the new public-address accessor mirrors, and the only file the new exemption `case` is added to.
- `interactive/session-id.go` — the package's precedent for a package-level `var` (a compiled regexp) and a `const` (`sessionHeader`, `defaultSessionID`).
- `interactive/readiness.go` — the package's precedent for a small handler served by a `service` method.
- `interactive/service_test.go` — `newTestServer`, the `Describe("Run")` spec, `contractEntries` / `runContractTable`, and `captureStderr`. Two of its call sites pass the constructor's parameters and must be updated.
- `interactive/auth_test.go` — `newAuthTestServer`, `newAuthPermissionTestServer`, `newAuthMockFactory`, `authTestToken`, `requestWithAuth`. Two of its call sites pass the constructor's parameters and must be updated; its fixtures are reused by this prompt's tests.
- `interactive/permission_test.go` — `newPermissionTestServer`; its one call site passes the constructor's parameters and must be updated.
- `interactive/interactive_suite_test.go` — the Ginkgo suite, the 60 s timeout, the `//go:generate ... counterfeiter ... -generate` directive.
- `docs/interactive-service.md` — the frozen contract. READ it to know which existing rows must keep their statuses; do NOT edit it in this prompt (the sibling prompt `3-spec-058-frozen-contract-a2a.md` owns the doc update).
- `specs/in-progress/058-interactive-service-a2a-endpoint.md` — the spec this prompt implements.

Load-bearing facts, verified against this working tree and against the A2A SDK source in the module cache. Do not re-derive these, do not contradict them.

1. **The A2A SDK is NOT in this repo's module cache or `go.mod` yet.** The new dependency is `github.com/a2aproject/a2a-go/v2` (note the `/v2` suffix — the un-suffixed `github.com/a2aproject/a2a-go` module is the superseded v1 line and MUST NOT be added). Its `go.mod` declares `go 1.26.0`, which is satisfied by this repo's `go 1.27.1`. The API below was verified by downloading `v2.6.0` into `$(go env GOPATH)/pkg/mod/github.com/a2aproject/a2a-go/v2@v2.6.0` and reading the source; pin `v2.6.0` in the `go get`. There is no `vendor/` directory in this repo and `make precommit`'s `ensure` target removes one if present — never use `-mod=vendor`.

2. **The v2 Agent Card has NO top-level `url` field.** The spec's prose calls it "the card's `url`"; in this SDK the advertised endpoint lives in `a2a.AgentCard.SupportedInterfaces []*a2a.AgentInterface`, and each `a2a.AgentInterface` has a `URL string` field (`json:"url"`). Build it with the SDK helper so the protocol binding and version are set consistently:

   ```go
   func NewAgentInterface(url string, protocolBinding TransportProtocol) *AgentInterface {
       return &AgentInterface{URL: url, ProtocolBinding: protocolBinding, ProtocolVersion: Version}
   }
   ```

   `a2a.TransportProtocolJSONRPC` is the binding constant (`"JSONRPC"`). `a2a.Version` is `"1.0"`. The JSON that results is `"supportedInterfaces":[{"url":"<publicURL>","protocolBinding":"JSONRPC","protocolVersion":"1.0"}]` — so the advertised address is at `.supportedInterfaces[0].url`, not `.url`. See REVIEWER NOTES at the bottom of this prompt.

3. **The card is served by the SDK.** `a2asrv.NewStaticAgentCardHandler(card *a2a.AgentCard) http.Handler` marshals the card once and serves it as `Content-Type: application/json` for `GET` (and answers `OPTIONS`; a non-`GET`/`OPTIONS` method is `405`). The well-known path constant is `a2asrv.WellKnownAgentCardPath` = `"/.well-known/agent-card.json"`.

4. **`a2a.AgentCard` fields** (from `a2a/agent.go`): `SupportedInterfaces []*AgentInterface`, `Capabilities AgentCapabilities`, `DefaultInputModes []string`, `DefaultOutputModes []string`, `Description string`, `DocumentationURL string`, `IconURL string`, `Name string`, `Provider *AgentProvider`, `SecurityRequirements`, `SecuritySchemes`, `Signatures`, `Skills []AgentSkill`, `Version string`. There is no `URL` field. `a2a.AgentSkill` has `ID string`, `Name string`, `Description string`, `Tags []string`, `Examples []string`, `InputModes []string`, `OutputModes []string`. `a2a.AgentCapabilities` has `Extensions`, `PushNotifications bool`, `Streaming bool`, `ExtendedAgentCard bool`.

5. **`interactive/service.go` currently declares, in this order:** the `Service` interface (`Handler() http.Handler`, `Run(ctx context.Context) error`), `NewService(sessions agentlib.SessionFactory, listen string, providerBaseURL string, registry *prometheus.Registry, auth Auth) Service`, `NewServiceWithPermissions(sessions agentlib.SessionFactory, listen string, providerBaseURL string, registry *prometheus.Registry, auth Auth, permissions PermissionRegistry) Service`, the private `service` struct with fields `cache *sessionCache`, `listen string`, `providerBaseURL string`, `registry *prometheus.Registry`, `permissions PermissionRegistry`, `auth Auth`, then `Handler()` and `Run(ctx)`. `Handler()` builds `http.NewServeMux()` and registers `/readiness`, `/metrics`, `/prompt`, and `/permission` when `s.permissions != nil`, then returns `s.requireAuth(router)`.

6. **`interactive/auth.go`'s `authExempt` is an exact-path `switch`:**

   ```go
   func authExempt(path string) bool {
       switch path {
       case "/readiness", "/metrics":
           return true
       default:
           return false
       }
   }
   ```

   The card path joins the literal `case` list. `auth.go`'s credential logic (`Auth`, `AuthDisabled`, `AuthFromEnv`, `NewAuthToken`, `allows`, `requireAuth`, `bearerPrefix`, `authorizationHeader`, `authTokenEnv`) is unchanged; the ONLY edit to `auth.go` in this change is the one `case` line.

7. **The ONLY call sites of the two constructors in this repository are in tests** — `grep -rn 'interactive.NewService' --include='*.go' .` returns five: `interactive/service_test.go` (`newTestServer`, and the `Describe("Run")` spec), `interactive/auth_test.go` (`newAuthTestServer`, `newAuthPermissionTestServer`), `interactive/permission_test.go` (`newPermissionTestServer`). There is NO production call site in this repo — the two consumer binaries live in other repositories and are out of this spec's scope. Re-run that grep before you start.

8. `interactive/service_test.go` already declares, in `package interactive_test`: `captureStderr(fn func()) string`, `newTestServer(factory agentlib.SessionFactory, providerBaseURL string) *httptest.Server`, `postPrompt(serverURL, sessionID, body string) (int, error)`, `strptr`, `contractObserver`, `contractBackend`, `newMockBackend`, `runContractTable`, `contractEntries`, `routeEntries`, `promptEntries`. `interactive/auth_test.go` declares `authTestToken`, `authWrongToken`, `authEnv`, `newAuthMockFactory`, `newAuthTestServer`, `newAuthPermissionTestServer`, `requestWithAuth`, `postPromptAuth`. REUSE these; do not redefine any of them.

9. `interactive/service_test.go` sets `flag.Set("logtostderr", "true")` and `flag.Set("v", "2")` in `BeforeSuite` — that is what makes the `turn start` line visible to `captureStderr`. Do not change `BeforeSuite`.

10. `github.com/bborbe/errors` is at `v1.6.1` and exports `New(ctx, message string) error`, `Errorf(ctx, format string, ...) error`, `Wrap(ctx, err error, message string) error`, `Wrapf(ctx, err error, format string, ...) error`. `context.Background()` appears ZERO times in this repo's non-test code — every error-returning function that needs a context takes one as its first parameter. `interactive/readiness.go`'s `dialAddress(ctx context.Context, raw string) (string, error)` is the local precedent.

11. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`. No `git` command is used in this prompt's verification regardless, because the daemon does not check verification exit codes.

12. `make precommit` at the repository root runs `ensure format generate test check addlicense` (the root Makefile; `interactive/` has none). `make test` runs the whole module with `-race`. Both are run from `/workspace`. Lint limits that bind the new file: funlen 80 lines / 50 statements, gocognit 20, nestif 4, golines line length 100.

13. **The public address is NOT a secret.** Unlike the bearer token, it is advertised to the world by design. It is still configuration, not a constant, and the handler must never derive it from the listen address or the request `Host` header.

</context>

<requirements>

## 1. Add the A2A dependency

Add `github.com/a2aproject/a2a-go/v2` at `v2.6.0`. Write the code that imports it FIRST (requirements 2-4), then run `go get github.com/a2aproject/a2a-go/v2@v2.6.0 && go mod tidy` so the module is pinned to exactly `v2.6.0` and promoted to a direct requirement — plain `go mod tidy` resolves the LATEST `v2.x`, not `v2.6.0`, and drops a requirement nothing imports. If the resolved version is not `v2.6.0`, STOP and re-verify every symbol cited in requirements 2-4 against the downloaded source before proceeding. Do NOT run `go get`/`go mod tidy` before the importing code exists. Do NOT add the un-suffixed `github.com/a2aproject/a2a-go` module. Do NOT create or use a `vendor/` directory.

## 2. New file `interactive/a2a.go`

Create `interactive/a2a.go` (`package interactive`, BSD license header matching the sibling files, no `init()`, no package-level mutable state). It imports `context`, `net/http`, `os`, `github.com/bborbe/errors`, `github.com/a2aproject/a2a-go/v2/a2a`, and `github.com/a2aproject/a2a-go/v2/a2asrv`.

### 2a. Constants

```go
// a2aPublicURLEnv is the environment variable the service reads its externally reachable
// A2A address from. The deployed address is never the container-local listen address, so it
// is supplied as configuration and never derived from the listener or the request Host header.
const a2aPublicURLEnv = "A2A_PUBLIC_URL"

// agentCardName names this service in the Agent Card a client discovers.
const agentCardName = "interactive"

// agentCardVersion is the Agent Card's own version string. The format is the provider's to
// choose; this service has no separate release version to borrow.
const agentCardVersion = "1.0.0"
```

### 2b. The fail-closed accessor

Mirror `AuthFromEnv` in `interactive/auth.go` exactly in shape — a plain function (not a `New*` constructor, because reading the environment is I/O), taking `ctx` as its first parameter because the repo forbids `context.Background()` in non-test code:

```go
// A2APublicURLFromEnv reads the externally reachable A2A address from the process
// environment.
//
// It returns an error when the variable is unset or empty, so a service that cannot
// advertise a real endpoint fails to start rather than advertising the container-local
// listen address. The address is public by design and is not a credential.
func A2APublicURLFromEnv(ctx context.Context) (string, error) {
    publicURL := os.Getenv(a2aPublicURLEnv)
    if publicURL == "" {
        return "", errors.Errorf(ctx, "environment variable %s is unset", a2aPublicURLEnv)
    }
    return publicURL, nil
}
```

The error message names the environment variable and nothing else.

### 2c. The card builder

A pure function (no I/O, no conditionals) that turns the configured address into the Agent Card:

```go
// newAgentCard builds the A2A Agent Card this service advertises. The single supported
// interface is the JSON-RPC binding at publicURL, which is the externally reachable address
// supplied as configuration — never the listen address and never a value derived from a
// request. The card carries no credential.
func newAgentCard(publicURL string) *a2a.AgentCard {
    return &a2a.AgentCard{
        Name:        agentCardName,
        Version:     agentCardVersion,
        Description: "Interactive agent HTTP surface, exposed over A2A.",
        SupportedInterfaces: []*a2a.AgentInterface{
            a2a.NewAgentInterface(publicURL, a2a.TransportProtocolJSONRPC),
        },
        Capabilities:       a2a.AgentCapabilities{},
        DefaultInputModes:  []string{"text/plain"},
        DefaultOutputModes: []string{"text/plain"},
        Skills: []a2a.AgentSkill{
            {
                ID:          "prompt",
                Name:        "Prompt",
                Description: "Runs one turn on an interactive agent conversation and returns the agent's reply.",
                Tags:        []string{"agent"},
            },
        },
    }
}
```

`a2a.AgentCapabilities{}` leaves `Streaming` and `PushNotifications` false, which is this spec's posture (streaming and push are out of scope).

### 2d. The card handler

A `service` method returning the SDK's card handler:

```go
// agentCardHandler serves the Agent Card. The card is public by design: it advertises the
// endpoint and skills and carries no credential, so the route is exempt from the gate.
func (s *service) agentCardHandler() http.Handler {
    return a2asrv.NewStaticAgentCardHandler(s.card)
}
```

## 3. Edit `interactive/service.go`

Make exactly these changes; do not restructure the file.

1. Add a `card *a2a.AgentCard` field to the private `service` struct, last, documented as:

```go
    // card is the Agent Card served at the well-known path. Its interface URL is the
    // configured public address, so the card can never advertise the container-local
    // listen address.
    card *a2a.AgentCard
```

2. Both constructors take the public address as a new `publicURL string` parameter — immediately after `auth` and before `permissions` (in `NewServiceWithPermissions`), so the parameter sits in the same position in both and a reader cannot mistake it for optional:

```go
// NewService creates the interactive session service.
//
// sessions supplies one conversation per session id; listen is the address the service
// binds; providerBaseURL is the endpoint the readiness probe dials (empty means the
// check is skipped and reported as such); registry is the Prometheus registry the
// metrics route gathers from — a parameter rather than a library singleton, so each
// binary keeps its own metrics identity; auth is the authentication every gated route
// requires, and the zero Auth refuses every gated request; publicURL is the externally
// reachable address the Agent Card advertises, and it is never derived from listen. The
// permission endpoint is not served by this constructor; use NewServiceWithPermissions
// to serve it.
func NewService(
    sessions agentlib.SessionFactory,
    listen string,
    providerBaseURL string,
    registry *prometheus.Registry,
    auth Auth,
    publicURL string,
) Service {
    return NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, publicURL, nil)
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
// gated request. publicURL is the externally reachable address the Agent Card
// advertises, and it is never derived from listen.
func NewServiceWithPermissions(
    sessions agentlib.SessionFactory,
    listen string,
    providerBaseURL string,
    registry *prometheus.Registry,
    auth Auth,
    publicURL string,
    permissions PermissionRegistry,
) Service {
    return &service{
        cache:           newSessionCache(sessions),
        listen:          listen,
        providerBaseURL: providerBaseURL,
        registry:        registry,
        permissions:     permissions,
        auth:            auth,
        card:            newAgentCard(publicURL),
    }
}
```

3. `Handler()` registers the card route on the router that is then wrapped in the gate (never beside it):

```go
func (s *service) Handler() http.Handler {
    router := http.NewServeMux()
    router.Handle("/readiness", s.readinessHandler())
    router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
    router.Handle("/prompt", s.promptHandler())
    router.Handle(a2asrv.WellKnownAgentCardPath, s.agentCardHandler())
    if s.permissions != nil {
        router.Handle("/permission", s.permissionHandler())
    }
    return s.requireAuth(router)
}
```

4. Add the `github.com/a2aproject/a2a-go/v2/a2a` and `.../a2asrv` imports. Update the doc comments on `Service.Handler` and `service.Handler` to say the card route is served without a credential because discovery is public by design.

5. Change nothing else in this file. Do NOT add a variadic option, a config struct or an opt-out flag.

## 4. Edit `interactive/auth.go` — one line

Add the card path to the existing literal case list in `authExempt`, so the switch reads:

```go
func authExempt(path string) bool {
    switch path {
    case "/readiness", "/metrics", "/.well-known/agent-card.json":
        return true
    default:
        return false
    }
}
```

Do NOT add prefix, glob or pattern matching. Do NOT relocate `authExempt`. Do NOT touch any other line of `auth.go` — the credential logic (`Auth`, `AuthDisabled`, `AuthFromEnv`, `NewAuthToken`, `allows`, `requireAuth`, `bearerPrefix`, `authorizationHeader`, `authTokenEnv`) is unchanged.

## 5. Update the five existing constructor call sites

The constructors now take a sixth parameter, so five call sites no longer compile. Every one of them is a test helper that drives the frozen contract, and every one must keep asserting exactly the statuses it asserts today. Add a shared test constant and pass it at each site.

1. In `interactive/a2a_test.go` (created in requirement 6), declare:

   ```go
   // testPublicURL is the externally reachable address the test fixtures advertise in their
   // Agent Card. It is deliberately NOT the httptest server's own URL: the card's address is
   // configuration, so it must never be derived from the listener.
   const testPublicURL = "https://agent.example.test/a2a"
   ```

2. `interactive/service_test.go`, `newTestServer` — change the construction call to `interactive.NewService(factory, ":0", providerBaseURL, prometheus.NewRegistry(), interactive.AuthDisabled, testPublicURL)`. Keep the helper's signature and doc comment unchanged.
3. `interactive/service_test.go`, the `Describe("Run")` spec — add `testPublicURL` as the sixth argument to its `interactive.NewService(...)` call.
4. `interactive/auth_test.go`, `newAuthTestServer` — change the construction call to `interactive.NewService(factory, ":0", "", prometheus.NewRegistry(), auth, testPublicURL)`. Keep the helper's signature unchanged.
5. `interactive/auth_test.go`, `newAuthPermissionTestServer` — add `testPublicURL` as the sixth argument (before `permissions`) to its `interactive.NewServiceWithPermissions(...)` call.
6. `interactive/permission_test.go`, `newPermissionTestServer` — add `testPublicURL` as the sixth argument (before `permissions`) to its `interactive.NewServiceWithPermissions(...)` call.

Do not change any other line of these three files. In particular every row of `contractEntries` / `routeEntries` / `promptEntries` and every existing `It` keeps its current status assertions — the card route changes none of them.

## 6. Tests — new file `interactive/a2a_test.go`

Create `interactive/a2a_test.go` in the external `package interactive_test`, with the BSD license header. Reuse `authTestToken`, `newAuthMockFactory` and `newAuthTestServer` from `interactive/auth_test.go`; do not redefine any helper that already exists in the package.

Add one helper:

```go
// newA2ATestServer builds a plain service advertising publicURL and wraps its handler in an
// httptest server.
func newA2ATestServer(
    publicURL string,
    auth interactive.Auth,
    factory agentlib.SessionFactory,
) *httptest.Server {
    svc := interactive.NewService(factory, ":0", "", prometheus.NewRegistry(), auth, publicURL)
    return httptest.NewServer(svc.Handler())
}
```

Add `var _ = Describe("Agent card", ...)` with these specs. Name each `It` EXACTLY as given.

1. **"serves the agent card without a credential"** (AC1) — build an auth-enabled server with `newA2ATestServer(testPublicURL, interactive.NewAuthToken(authTestToken), newAuthMockFactory())`. Issue `GET <server>/.well-known/agent-card.json` with NO `Authorization` header. Assert the status is `200`, and that the body parses as JSON whose `name` equals `interactive` (decode into a `map[string]any`, or into `a2a.AgentCard`). The auth-enabled fixture is load-bearing: the package's default `newTestServer` builds with `AuthDisabled`, where every route answers `200` without a credential, so a `200` there would prove nothing about the card's exemption.
2. **"advertises the configured public address verbatim"** (AC2) — build a server with a distinct public URL (e.g. `"https://card.example.test/a2a"`) and a valid token, `GET` the card path, decode into `a2a.AgentCard`, and assert `card.SupportedInterfaces[0].URL` equals that value verbatim. Also assert the raw body does NOT contain `0.0.0.0`.
3. **"reads the public address from the environment and fails closed"** (AC2) — in one `It`: set `os.Setenv("A2A_PUBLIC_URL", "https://env.example.test/a2a")` with `DeferCleanup(os.Unsetenv, "A2A_PUBLIC_URL")`, call `interactive.A2APublicURLFromEnv(context.Background())`, expect no error and the exact value back; then unset the variable (restoring any prior value with `DeferCleanup`) and call the accessor again, expect an error whose message contains `A2A_PUBLIC_URL`.

`context.Background()` is fine in a test file. Keep the empty-input, unset and present cases covered.

</requirements>

<constraints>
- **The card is public by design.** `GET /.well-known/agent-card.json` is served without a credential because discovery is public, the same posture as a public `robots.txt`. The card MUST NOT embed the bearer token, any other secret, or any derived address.
- **The advertised address is configuration, never a constant and never derived.** It is the value of `A2A_PUBLIC_URL`, carried to the service as a constructor parameter and used verbatim. It is NEVER derived from the listen address, from the request `Host` header, or from anything else the container can observe.
- **An unset or empty public address fails closed.** `A2APublicURLFromEnv` returns an error naming `A2A_PUBLIC_URL`, so a service that cannot advertise a real endpoint fails to start rather than advertising a wrong one. This mirrors the `AuthFromEnv` fail-closed shape.
- **`requireAuth` keeps wrapping the whole router.** The card route is registered on the wrapped router, never beside it; its exemption comes only from the exact-path `authExempt` switch.
- **`authExempt` stays an exact-path switch.** The card joins it as a literal `case`; no prefix, glob or pattern matching is added, and the function is not relocated. The only edit to `auth.go` is that one `case` line.
- **`auth.go`'s credential logic is unchanged** — `Auth`, `AuthDisabled`, `AuthFromEnv`, `NewAuthToken`, `allows`, `requireAuth` and the `Bearer ` scheme are untouched.
- **No second credential and no new credential of any kind.** `INTERACTIVE_AUTH_TOKEN` remains the only one.
- **The new dependency is `github.com/a2aproject/a2a-go/v2` only.** Do NOT add the un-suffixed `github.com/a2aproject/a2a-go` module (the superseded v1 line). Do NOT vendor.
- **Write the importing code before running `go mod tidy`.** `go mod tidy` removes a direct requirement nothing imports.
- **The card route changes no existing route's contract.** Every existing frozen-contract row keeps its current status assertion. `/readiness`, `/metrics`, `/prompt` and `/permission` are untouched.
- **`interactive/a2a.go` must not contain the identifiers `authorizationHeader`, `authTokenEnv`, `AuthFromEnv`, `NewAuthToken` or `authExempt`** — not in code and not in a comment (a comment that merely *describes* the gate would fail Acceptance Criterion 6's grep). If you want to say the accessor mirrors the token accessor, describe it in words ("mirrors the token accessor's fail-closed shape") without spelling the identifier. The same applies to `interactive/service.go`'s new doc comments.
- **Logging.** This path adds no logging of its own and no metrics family. The spec asks for neither. The public address is not a secret but is still not logged.
- **Error handling** uses `github.com/bborbe/errors` — `errors.Errorf` / `errors.New` / `errors.Wrap` / `errors.Wrapf`, never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in non-test code.
- **Code conventions** per `docs/dod.md`: exported items carry doc comments; Interface → Constructor → Struct → Method; no `init()`, no package-level mutable state; factory functions are pure composition (no conditionals, no I/O).
- **Tests.** Ginkgo v2 / Gomega in the external `interactive_test` package; counterfeiter mocks only, never hand-written; coverage for the new code >= 80%. Reuse the existing helpers; redefine nothing.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; gocognit 20; nestif 4.
- **Hand-off, not this prompt.** The JSON-RPC execution endpoint (`POST /a2a`), the executor bridging to the session seam, the `docs/interactive-service.md` update, the README row and the CHANGELOG entry all land in the sibling prompts (`2-spec-058-a2a-jsonrpc-handler.md`, `3-spec-058-frozen-contract-a2a.md`). This prompt adds the dependency, the accessor, the card, the card route and the exemption.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable; none needs host tooling, Docker, a cluster or a second repository.

```bash
# 1. The dependency is present and is the /v2 line only.
grep -n 'github.com/a2aproject/a2a-go/v2' go.mod
! grep -qE 'github\.com/a2aproject/a2a-go v' go.mod   # the un-suffixed v1 module must be absent

# 2. The accessor exists and reads the named variable.
grep -n 'func A2APublicURLFromEnv' interactive/a2a.go
grep -n 'A2A_PUBLIC_URL' interactive/a2a.go

# 3. The card carries the address in supportedInterfaces (the SDK has no top-level url).
grep -n 'NewAgentInterface' interactive/a2a.go
grep -n 'TransportProtocolJSONRPC' interactive/a2a.go

# 4. The card route is registered on the wrapped router, at the SDK's well-known path.
grep -n 'WellKnownAgentCardPath' interactive/service.go
grep -n 'return s.requireAuth(router)' interactive/service.go   # still exactly one match

# 5. The exemption switch lists the card path among its literal cases.
grep -n -A6 'func authExempt' interactive/auth.go

# 6. Both constructors take publicURL before permissions.
grep -n -A9 'func NewService(' interactive/service.go
grep -n -A10 'func NewServiceWithPermissions(' interactive/service.go

# 7. Acceptance Criterion 6: the credential path stays confined to interactive/auth.go.
grep -rln --include='*.go' --exclude='*_test.go' -E 'authorizationHeader|authTokenEnv|AuthFromEnv|NewAuthToken|authExempt' interactive/
# Must print EXACTLY: interactive/auth.go
test "$(grep -rln --include='*.go' --exclude='*_test.go' -E 'authorizationHeader|authTokenEnv|AuthFromEnv|NewAuthToken|authExempt' interactive/)" = "interactive/auth.go" && echo "AC6 OK"

# 8. The new production file logs nothing and uses no context.Background().
! grep -n 'glog' interactive/a2a.go
! grep -n 'context.Background()' interactive/a2a.go

# 9. The package's tests pass (this is the package's only test entry point — do not use a
#    -run <name> filter, which would exit 0 without running anything).
go test ./interactive/... -count=1

# 10. The whole module's tests and the full gate.
make test
make precommit
```

Every command must produce the annotated result or the prompt is not done. If `make precommit` fails, fix the issue and re-run ONLY the failing target until it passes, then re-run `make precommit` once more.

**Self-check before finishing:** re-run the block above and confirm each annotated result; then walk spec 058 Acceptance Criteria 1, 2 and 6 against the change and confirm each is met. In particular confirm the card is served with no `Authorization` header on an auth-ENABLED server, that the advertised address equals the configured value verbatim and is not `0.0.0.0`, that an unset `A2A_PUBLIC_URL` produces an error naming the variable, and that requirement 7's grep prints exactly one path.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **Decomposition deviates from the spec's 5-prompt table (5 → 3).** The spec's prompt 1 ("card construction + config") cannot satisfy AC2 on its own: AC2's evidence is "the **served** card's `url`", and the card builder is unexported while every test file in this package is the external `interactive_test` package — so the card can only be observed through the route the spec's prompt 2 adds. Card construction and the card route are therefore one atomic, independently-verifiable change, and this prompt merges them. The spec's prompt 5 ("tests") is folded into the implementation prompts because a tests-only prompt would duplicate the tests the implementation prompts must write anyway (the sizing-guide anti-pattern), and the precedent prompt `225-spec-057-interactive-service-authentication.md` placed tests with their implementation. The result is three prompts: this one (card + config + route + exemption), `2-spec-058-a2a-jsonrpc-handler.md` (the JSON-RPC endpoint + executor), `3-spec-058-frozen-contract-a2a.md` (docs). No scenario prompt is emitted: every behaviour here is reached by the in-process `httptest` server, so the `docs/rules/scenario-writing.md` four-condition test fails on its first condition.
- **Open question 1 — the wire format diverges from the spec's literal AC2 text.** The spec says the served card's `url` (checked with `jq -r .url`). The mandated SDK (`github.com/a2aproject/a2a-go/v2@v2.6.0`, A2A protocol `1.0`) has NO top-level `url` field on `a2a.AgentCard`; the advertised endpoint lives at `supportedInterfaces[0].url`. This prompt implements the SDK's real shape and the test asserts `SupportedInterfaces[0].URL`. The spec's *intent* — advertise the configured address verbatim, never `0.0.0.0` — is preserved exactly. If the reviewer insists on a literal top-level `url`, the alternative is the SDK's v0.3 compatibility layer `a2acompat/a2av0` (`a2av0.NewStaticAgentCardProducer` + `a2asrv.NewAgentCardHandler`), which emits a card carrying BOTH shapes — but that package imports the un-suffixed `github.com/a2aproject/a2a-go` module the spec explicitly calls "the superseded v1 line", so it was rejected here.
- **Open question 2 — the card's `name`, `version`, `description` and skill are not fixed by the spec.** AC1 only requires the body's `name` to name the agent. This prompt fixes `agentCardName = "interactive"`, `agentCardVersion = "1.0.0"`, one generic `prompt` skill and a one-line description. The spec's Security section describes the card as advertising "the endpoint and skills", which is why a skill is included. If the operator wants a different name or a real version string, change the three constants and the skill in `interactive/a2a.go`.
- **Open question 3 — the `publicURL` parameter position.** The spec says the address "reaches the service as a constructor parameter" but not where. It is placed after `auth` and before `permissions`, mirroring how `auth` was added in spec 057 (before `permissions`) and keeping the existing parameters' positions stable for the two out-of-scope consumer repositories. Any position is breaking; this one is the least disruptive.
- **Open question 4 — the SDK version.** Pinned at `v2.6.0`, the version whose source was read to write this prompt. If the container resolves a later `v2.x` whose API differs, the executor must re-verify the cited symbols before writing them and report the discrepancy — do NOT assume the API is stable across minor versions without grepping the downloaded source.
- **SDK streaming note.** The sibling prompt uses `a2asrv.NewJSONRPCHandler`, which also serves the SDK's streaming method. The spec's Non-goal ("streaming is out") means this change does not BUILD streaming; the SDK handler's inherited behaviour is not a new surface this repo authored. Flagged for the sibling prompt, not this one.
