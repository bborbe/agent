# Interactive Agent Service

The `interactive` package in this module serves the HTTP surface an interactive agent
image exposes: readiness, metrics and prompt intake, over a per-session cache of
long-lived conversations. It was extracted from `agent-pi/main.go`, where it existed
only as caller-side code; a second interactive image (`agent-claude`) previously had
no way to reuse it.

This document is the frozen contract. Both consumers honour the interface described
here, and it does not change without a spec.

## Constructing the service

```go
import (
	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
)

svc := interactive.NewService(sessions, listen, providerBaseURL, registry, auth, publicURL)
svc := interactive.NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, publicURL, permissions)
```

| Parameter | Meaning |
|---|---|
| `sessions` | An `agentlib.SessionFactory` — one conversation per session id, built on first use |
| `listen` | The address the service binds; the deployed default is `:9090` |
| `providerBaseURL` | The endpoint `GET /readiness` dials; empty means the check is skipped |
| `registry` | The Prometheus registry `GET /metrics` gathers from — a parameter rather than a library singleton, so each binary keeps its own metrics identity |
| `auth` | The authentication decision every gated route requires. Build it with `interactive.NewAuthToken(token)` or `interactive.AuthFromEnv(ctx)`, or state the opt-out with `interactive.AuthDisabled`. The zero value is not a usable default — it refuses every gated request. |
| `publicURL` | The externally reachable address the Agent Card advertises, verbatim. Build it with `interactive.A2APublicURLFromEnv(ctx)`, which reads `A2A_PUBLIC_URL` and returns an error when it is unset or empty. It is never derived from `listen` or from a request header, so the container-local address can never be advertised. |
| `permissions` | The permission registry the endpoint serves and the sessions consult; pass the same instance to the session factory, or the endpoint serves nothing. `NewService` takes none and does not serve the route |

The `publicURL` parameter is a breaking addition for the consumer repositories that call
`interactive.NewService` / `interactive.NewServiceWithPermissions`. It is the address
accessor's counterpart to the token accessor: `interactive.A2APublicURLFromEnv(ctx)
(string, error)` fails closed when the address is unset, so a service that cannot
advertise a real endpoint does not start rather than advertising a container-local one.

`Service.Handler()` returns the router; `Service.Run(ctx)` serves it until the context
is cancelled.

## The session seam

`agentlib.Session` is the only thing the HTTP surface knows about a backend:

```go
type Session interface {
	Prompt(ctx context.Context, prompt string) (string, error)
	Close(ctx context.Context) error
}

type SessionFactory interface {
	Create(id string) Session
}
```

`Create` is construction only — no I/O, no failure. Any failure a backend can have
surfaces from `Session.Prompt`, where the service already has a defined failure path.
A backend that holds nothing between turns still satisfies the interface, because
`Close` may be a no-op.

## Routes

| Route | Method | Behaviour | Policy |
|---|---|---|---|
| `/readiness` | `GET` | Dials the provider; see below | Exempt — kubelet probe |
| `/metrics` | `GET` | Prometheus scrape endpoint over the injected registry | Exempt — Prometheus scrape |
| `/prompt` | `POST` | Runs one turn on the addressed conversation | Bearer token required |
| `/permission` | `GET`, `POST` | Lists the pending permission requests; delivers a verdict to the one it names | Bearer token required |
| `/.well-known/agent-card.json` | `GET` | The A2A Agent Card as JSON | Exempt — public discovery |
| `/a2a` | `POST` | The A2A JSON-RPC binding; runs one turn on the conversation named by the request's `contextId` | Bearer token required |

The card route is exempt by decision: discovery is public by design, the same posture as
a public `robots.txt`. It carries no credential and never the bearer token — the card
advertises the endpoint and skills and nothing secret. `/a2a` is gated exactly like
`/prompt`.

Any other method on `/prompt` is `405` **for a request that has passed the gate**.
`/permission` is served only by a service built with a permission registry; the plain
`NewService` does not register it and answers `404` **for a request that has passed the
gate**. Every route except `/readiness`, `/metrics` and `/.well-known/agent-card.json`
requires the bearer token described in [Authentication](#authentication); those three are
exempt by decision, not by omission, and the reason is recorded there. The network posture
is namespace-scoped reachability, unchanged — see
[agent-network-security.md](agent-network-security.md).

## The contract, row by row

| Request | Expected |
|---|---|
| `GET /readiness`, `PROVIDER_BASE_URL` unset | `200`, body exactly `OK (PROVIDER_BASE_URL unset — provider reachability not checked)` |
| `GET /readiness`, `PROVIDER_BASE_URL` set but unroutable | `503` |
| `GET /metrics` | `200` |
| `GET /prompt` | `405` |
| `POST /prompt`, no `X-Session-Id` | `200`; served from session id `identity` |
| `POST /prompt`, `X-Session-Id:` present but empty | `400` |
| `POST /prompt`, `X-Session-Id: bad id!` | `400` |
| `POST /prompt`, body > 1 MiB | `200`; body truncated to 1 MiB |
| `POST /prompt`, empty body | `400` |
| `POST /prompt`, valid | `200`, `Content-Type: text/plain; charset=utf-8` |
| `POST /prompt`, no `Authorization` header | `401` |
| `POST /prompt`, `Authorization: Bearer <wrong token>` | `401` |
| `POST /prompt`, `Authorization: Bearer <correct token>` | `200` — the behaviour the valid-body row above describes, unchanged |
| `GET /permission`, permission-enabled, no `Authorization` header | `401` |
| `GET /permission`, permission-enabled, nothing pending | `200`, `Content-Type: application/json`, body exactly `[]` |
| `GET /permission`, plain `NewService` | `404` |
| `POST /permission`, pending id, `{"id":"…","allow":true}` | `200`, body exactly `{}` |
| `POST /permission`, unknown id | `404` |
| `POST /permission`, missing `id` | `400` |
| `POST /permission`, malformed JSON | `400` |
| `PUT /permission`, permission-enabled | `405` |
| `GET /.well-known/agent-card.json`, no `Authorization` header | `200`, body is the Agent Card JSON whose `name` names the agent |
| `GET /.well-known/agent-card.json`, `A2A_PUBLIC_URL` set | the card's `supportedInterfaces[0].url` equals that value verbatim — never `0.0.0.0`, never `localhost` |
| `POST /a2a`, no `Authorization` header | `401` |
| `POST /a2a`, `Authorization: Bearer <wrong token>` | `401` |
| `POST /a2a`, `Authorization: Bearer <correct token>`, `SendMessage` | `200`, a `completed` task whose artifact carries the agent's reply for that conversation |
| `POST /a2a`, `contextId` failing the session-id regex | JSON-RPC error response; no session built |
| `POST /a2a`, unimplemented method | JSON-RPC "method not found" error object |
| `POST /a2a`, backend turn fails | `failed` task; the error is logged and never returned in the body |
| `POST /a2a`, malformed JSON-RPC body | JSON-RPC error response; no session built |
| `POST /a2a`, body larger than 1 MiB | refused by the 1 MiB cap before the SDK handler reads it — **not** truncated as `/prompt` does, because a truncated JSON body cannot parse |

Every pre-existing row in this table describes a request that has **already passed the
authentication gate**: its status assumes a valid `Authorization` header on the gated
routes (`/prompt`, `/permission` and `/a2a`) and no header on the exempt ones
(`/readiness`, `/metrics` and `/.well-known/agent-card.json`). The `401` rows are the
gate's own refusal and are therefore **pre-gate** — they describe the request that never
reaches the route at all.

### Session id

The header is `X-Session-Id`. An absent header resolves to the default conversation
`identity`, which is the conversation the service served before the header existed, so
an existing caller is never orphaned.

A present header is validated against the anchored regex:

```
^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$
```

The regex is the security boundary, not a style preference: the id reaches a backend
CLI as a command-line argument, so a leading `-` would be parsed as a flag, and `.` or
`/` would escape a session directory if the id were used to build a storage path.

Absent and present-but-empty are deliberately different outcomes. An empty value is a
present header that does not match the regex, so it is a `400`. Validation happens
before the body is read and before any session is built.

### Body cap

`POST /prompt` reads at most 1 MiB, applied with `io.LimitReader` before the body is
buffered so an oversized request cannot exhaust the pod's memory. Note precisely what
this does: it **truncates** an oversized body to 1 MiB and processes the truncated
prompt, returning a normal `200`. It does not reject, and the caller cannot detect the
truncation from the response. Rejecting instead would be a separate, reviewable change.

### Readiness

The probe **dials** the provider rather than calling its API. The question a readiness
probe asks is "can this agent reach its provider at all", and a dial answers it without
spending a request or needing a valid key. The dial timeout is 5 seconds.

The port is derived from the scheme when the URL omits it — `http` defaults to `80`,
anything else to `443` — and a bare `host:port` with no scheme is dialled as given.
Pointing `PROVIDER_BASE_URL` at an unroutable address must turn the probe red, or the
check is only testing that the process is alive.

Both body strings are frozen. With `PROVIDER_BASE_URL` unset the probe answers `200`:

```
OK (PROVIDER_BASE_URL unset — provider reachability not checked)
```

When the dial succeeds it answers `200` with the resolved `host:port`:

```
OK (provider reachable at <host>:<port>)
```

and when the dial fails, `503` with `provider unreachable at <host>:<port>: <dial error>`.

## Authentication

Every gated route requires the request header `Authorization: Bearer <token>`. The
scheme is matched exactly as `Bearer `, and the value that follows it is the service's
token. A request with no header, a wrong token, or a malformed header — a different
scheme, or a scheme with no value — is refused with `401` before the request reaches the
route's own handler: before the body is read, before a session is built and before the
session lock is taken. A refused request therefore cannot occupy a session and emits
neither line of the turn-boundary pair. A `401` carries `WWW-Authenticate: Bearer`; its
body is not part of the contract.

The token is compared in constant time, so a caller cannot recover the token's length or
prefix by measuring how long a refusal takes.

The token is read at startup from the `INTERACTIVE_AUTH_TOKEN` environment variable and
exists only as a runtime-injected value: never in source, never in an image layer, never
in a committed manifest. An unset or empty variable is an error, so a service that
cannot authenticate fails to start rather than serving unauthenticated — the pod does
not reach Ready. The token is never logged, at any verbosity, and never returned in an
error.

The service cannot be built without stating its authentication choice. Both constructors
take the decision as their fifth parameter, and the zero value fails closed: it refuses
every gated request rather than serving everything. A consumer that is deliberately
unexposed states the opt-out explicitly with `interactive.AuthDisabled`, which is a
visible, greppable line rather than an inherited default.

`/readiness` and `/metrics` stay open, deliberately. A kubelet readiness probe and a
Prometheus scrape cannot present a bearer token without the token being written into the
pod spec's probe stanza and the scrape configuration, which multiplies the secret's
exposure beyond the runtime injection this design depends on, and a misconfigured
readiness probe wedges the pod's Ready state. Neither route grants execution or reveals
a credential. The consequence is stated plainly: these two routes remain readable by
anything that can reach the port.

The A2A surface follows the same policy. The Agent Card route
`/.well-known/agent-card.json` is exempt by decision, alongside `/readiness` and
`/metrics`, for the same reason: discovery is public and the card carries no credential,
so it must never embed the bearer token or any other secret. The `/a2a` route is gated by
the same bearer token as every other non-exempt route. The A2A surface adds no second
credential — no API key, no per-route token, no additional scheme.

The token is a shared secret, so any holder has the full authority of the endpoint — it
authenticates "something that knows the token", not a named principal. Per-caller
identity, rotation without restart and revocation are out of scope and are not claimed.
Rotating the secret leaves the running pod serving the old token until it is restarted.

The gate is worthless over plaintext to an untrusted network. The recorded posture pairs
the token with cluster-network binding; exposing the port beyond the cluster without TLS
is not covered by this contract.

## Permission endpoint

`/permission` is the answer path for a mid-turn tool-permission request. A session
built with a decider blocks inside `DecidePermission` while the CLI waits; the request
it is waiting on is readable from outside the process on this route, and a caller posts
the verdict that releases it.

Two directions:

- **`GET /permission`** lists the requests currently pending, as a JSON array:

  ```json
  [{"id":"…","tool_name":"Bash","description":"…","input_preview":"…"}]
  ```

  Nothing pending renders exactly `[]` — an empty array, never `null`.

- **`POST /permission`** delivers a verdict to the request it names:

  ```json
  {"id":"…","allow":true,"message":"…"}
  ```

  `allow` true lets the tool run; false returns the denial, carrying `message`, to the
  process that asked. A resolved verdict answers `200` with body exactly `{}`.

The `id` is generated per request and is the only handle a verdict may name. An id that
no request holds is refused with `404`: the POST neither creates nor revives an entry.
An entry is removed exactly once — by a verdict when one is delivered, or by the waiting
turn when its context is cancelled — so a second verdict for the same id, or a verdict
racing a cancellation, finds nothing and is refused.

The body cap is 64 KiB, applied with `io.LimitReader`; an oversized body is truncated and
fails to parse, which is a `400`. A missing `id` is a `400` and a malformed body is a
`400`. A method other than `GET` or `POST` is a `405`.

The endpoint is gated like `/prompt`: it requires the bearer token, and
[Authentication](#authentication) records the policy. It adds no logging of its own. The
tool-input preview is carried to the caller inside the namespace and is never logged.
The endpoint is the route `agent-pi` does not serve: it builds through `NewService`,
which takes no permission registry.

## Locking

A session is built lazily on first use and cached by id, and the cache never evicts.
Locking is two-level:

- The cache's **map lock** guards the map and nothing else. It is released before the
  caller takes the entry's own lock, so two different sessions never contend there.
- Each entry has its own **session lock**, held for the duration of one turn.

The observable property: two different session ids run at the same time; two requests
on one id serialise — the second's turn starts only after the first returns. **No
request is ever rejected because of the lock.**

The lock is released by a deferred unlock, so a runner that panics mid-turn does not
deadlock the next request on that session.

The permission registry holds its own lock, and only around its map reads and writes —
never across the wait for a verdict. A turn paused on a permission request therefore
does not hold the map lock and does not stall another session, which keeps the "no
request is ever rejected because of the lock" invariant above true rather than widening
it into one lock held across the wait.

## Turn-boundary logging

Each turn writes two log lines at verbosity `V(2)`:

```
turn start id=<id>
turn end id=<id>
```

`turn start` is emitted once the session lock is held and `turn end` once the runner
returns, so the window between them is exactly the lock-held window — that is what makes
per-session serialisation observable from the pod log. A request rejected before the
lock (malformed id, empty body, wrong method) emits neither line.

The prompt itself is never logged by content: only its byte length and a truncated
SHA-256 digest. The session id appears in a log line only in the turn-boundary pair, and
the regex has already constrained it to `[A-Za-z0-9_-]`. A runner error is logged and
never returned in the response body — the caller receives the fixed string
`prompt failed`.

## A2A surface

`POST /a2a` serves the A2A JSON-RPC binding, and
`GET /.well-known/agent-card.json` serves the Agent Card that advertises it. The wire
method this service implements is `SendMessage` — the A2A protocol 1.0 JSON-RPC method
name; an unimplemented method returns a JSON-RPC error object, not a panic and not a
silent `200`.

One `SendMessage` runs one turn on the conversation named by the request's `contextId`,
through the same session cache and per-session lock `POST /prompt` uses, so an A2A turn
reuses the session the native route built rather than constructing a fresh uncached one.
An absent `contextId` resolves to the default conversation `identity`, exactly as an
absent `X-Session-Id` does.

`contextId` is validated against the anchored session-id regex
(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`) before any session is built — the same security
boundary the `X-Session-Id` header has, and for the same reason: the id reaches a backend
CLI as an argument, so a leading `-` would be read as a flag and `.` or `/` would escape a
session directory. An id that fails the pattern is refused having built no session and
emitted no `turn start` line.

A successful turn returns a `completed` task whose artifact carries the agent's reply for
that conversation. A backend error returns a `failed` task, with the error logged and
never returned in the body.

The Agent Card advertises exactly one A2A interface — the JSON-RPC binding — whose URL is
the configured public address, used verbatim. That address is configuration, supplied as
the constructor's `publicURL` parameter and read from `A2A_PUBLIC_URL` by
`interactive.A2APublicURLFromEnv`; it is never derived from the listen address or from a
request header, so the container-local address can never be advertised.

This repository authors only the one-shot `SendMessage` bridge. The SDK's JSON-RPC handler
also routes `SendStreamingMessage` (SSE) through the same executor; that inherited method
is not part of this contract, and push notifications and task polling are not built here.
The SDK's handler additionally dispatches the rest of the A2A 1.0 method set — task
lookup, cancellation, push-configuration management and the extended card — against its
own default in-memory stores; none of those is part of this contract either. Only
`SendMessage` is authored and documented here.
