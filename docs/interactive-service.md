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

svc := interactive.NewService(sessions, listen, providerBaseURL, registry)
svc := interactive.NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, permissions)
```

| Parameter | Meaning |
|---|---|
| `sessions` | An `agentlib.SessionFactory` — one conversation per session id, built on first use |
| `listen` | The address the service binds; the deployed default is `:9090` |
| `providerBaseURL` | The endpoint `GET /readiness` dials; empty means the check is skipped |
| `registry` | The Prometheus registry `GET /metrics` gathers from — a parameter rather than a library singleton, so each binary keeps its own metrics identity |
| `permissions` | The permission registry the endpoint serves and the sessions consult; pass the same instance to the session factory, or the endpoint serves nothing. `NewService` takes none and does not serve the route |

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

| Route | Method | Behaviour |
|---|---|---|
| `/readiness` | `GET` | Dials the provider; see below |
| `/metrics` | `GET` | Prometheus scrape endpoint over the injected registry |
| `/prompt` | `POST` | Runs one turn on the addressed conversation |
| `/permission` | `GET`, `POST` | Lists the pending permission requests; delivers a verdict to the one it names |

Any other method on `/prompt` is `405`. `/permission` is served only by a service built
with a permission registry; the plain `NewService` does not register it and answers
`404`. The service adds no authentication; the posture
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
| `GET /permission`, permission-enabled, nothing pending | `200`, `Content-Type: application/json`, body exactly `[]` |
| `GET /permission`, plain `NewService` | `404` |
| `POST /permission`, pending id, `{"id":"…","allow":true}` | `200`, body exactly `{}` |
| `POST /permission`, unknown id | `404` |
| `POST /permission`, missing `id` | `400` |
| `POST /permission`, malformed JSON | `400` |
| `PUT /permission`, permission-enabled | `405` |

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

The endpoint adds no authentication — the posture is namespace-scoped reachability,
unchanged, like its siblings — and no logging of its own. The tool-input preview is
carried to the caller inside the namespace and is never logged. The endpoint is the
route `agent-pi` does not serve: it builds through `NewService`, which takes no registry.

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
