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
```

| Parameter | Meaning |
|---|---|
| `sessions` | An `agentlib.SessionFactory` — one conversation per session id, built on first use |
| `listen` | The address the service binds; the deployed default is `:9090` |
| `providerBaseURL` | The endpoint `GET /readiness` dials; empty means the check is skipped |
| `registry` | The Prometheus registry `GET /metrics` gathers from — a parameter rather than a library singleton, so each binary keeps its own metrics identity |

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

Any other method on `/prompt` is `405`. The service adds no authentication; the posture
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
