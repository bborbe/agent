---
status: verifying
approved: "2026-10-01T05:03:51Z"
generating: "2026-10-01T05:19:05Z"
prompted: "2026-10-01T05:52:23Z"
verifying: "2026-10-01T07:58:14Z"
branch: dark-factory/shared-interactive-agent-service
---

## Summary

- `github.com/bborbe/agent` gains a runner-agnostic interactive session service: an HTTP surface (`/readiness`, `/metrics`, `POST /prompt`) backed by a per-session cache of long-lived runner sessions.
- Today that service exists only as caller-side code inside `agent-pi/main.go`. `agent-claude` has none of it, so a second interactive image would be a hand-synced copy of the same routing, cache and locking code.
- The session-holding mechanism is backend-specific, so the library defines one session interface with an implementation per backend. This spec delivers the interface, the HTTP service, and the pi implementation; the Claude implementation is a sibling spec (`claude-streaming-session.md`).
- `agent-pi` becomes a thin binary that constructs the shared service with its own runner. The port itself is a hand-off (see Suggested Decomposition), not a prompt of this spec.
- agent-pi's existing `:9090` contract is preserved exactly. The only added observable is a turn-boundary log pair.

## Problem

Both `bborbe/agent-pi` and `bborbe/agent-claude` are thin binaries over the shared module `github.com/bborbe/agent`, and both need the same interactive capability: a long-running pod that accepts a prompt over HTTP, holds a conversation across requests, and reports readiness and metrics. The capability is not shared. It lives in agent-pi's `main.go` as caller-side code — the router, the per-session runner cache, the per-session lock, the session-id validation, the body cap, the readiness dial — while the library it depends on imports `net/http` nowhere at all. agent-claude therefore has to reimplement all of it, and any divergence between the two copies would be invisible until runtime.

## Goal

A single implementation of the interactive session service lives in `github.com/bborbe/agent`, parameterised by a runner-agnostic session interface. An agent image supplies its own session implementation and constructs the shared service in a handful of lines. Adding a third interactive backend means writing one session implementation, not another copy of the HTTP layer.

## Non-goals

- Building the `claude-interactive` Config CR or its K8s manifests — that is the parent goal's SC1, a separate task.
- Renaming the agent configs by mode. The deployed Config is `pi-interactive` and stays `pi-interactive`.
- Changing `pi.Runner` or `claude.ClaudeRunner` — both are consumed elsewhere and keep their current signatures.
- The Claude session implementation — sibling spec `claude-streaming-session.md`.
- Porting `agent-pi` or `agent-claude` onto the shared service — both are hand-offs named in Suggested Decomposition, each delivered through its own repo's pipeline.
- Adding authentication to the HTTP surface. The existing posture is namespace-scoped reachability, unchanged (see `docs/agent-network-security.md`).
- A general-purpose HTTP framework. Only the three routes the service already serves.

## Assumptions

- **agent-pi's service behaviour is the contract.** Everything in the frozen-contract block below is read from `agent-pi/main.go` at `origin/master` and is treated as intentional. Where the code's behaviour and its comments disagree, the code wins.
- **The deployed Config is `pi-interactive`.** Verified live in dev on 2026-09-30; `pi-service` does not exist. If the parent goal renames it, the verification rung below is updated, not the contract.
- **A long-lived runner process is acceptable to both backends.** pi already holds one via `--session-id`; Claude can (sibling spec). No backend needs a stateless request/response shape.
- **`net/http` in the library is a deliberate dependency.** The library currently imports it nowhere; this spec is what changes that. `bborbe/http` moves from indirect to direct in `go.mod` as a result.
- **The session map is bounded by idle eviction and a size limit.** An entry is closed and dropped once it has gone the configured `sessionIdleTimeout` without serving a turn, and the least recently used entry is closed and dropped once the cache holds the configured `maxSessions` (defaulting to `interactive.DefaultMaxSessions`, 8, when non-positive); either way the id is rebuilt on its next turn, and an entry mid-turn is skipped and left in the cache. This was originally assumed unnecessary — the surface is namespace-scoped and the id space operator-controlled — but each held entry is a live child process, so the assumption did not survive measurement: the deployed service's memory grew linearly at roughly 88 MiB per session and the container was OOMKilled at about eleven. Idle eviction alone was not enough, because a session that keeps serving turns is never idle; the size limit bounds concurrency where idle eviction bounds only accumulation. See Security / Abuse Cases.

## Acceptance Criteria

- [ ] **AC1 — the library serves the routes.** A test starts the shared service through its constructor and reaches all three routes — `GET /readiness`, `GET /metrics`, `POST /prompt` — with no dependency on the consumer binaries' `main`.
  - Evidence: `go test ./<service-pkg>/... -v` exits 0 and lists the subtest `serves all three routes`, whose handler is built by the shared constructor and exercised through `httptest.NewServer`. A symbol grep (`git grep -nE 'ListenAndServe|ServeMux|http\.Handler'`) is **not** evidence on its own — `var _ http.Handler = nil` satisfies it — so it is kept only as a smoke check in the Verification rung.
- [ ] **AC2 — the `:9090` contract is reproduced exactly.** A table test against the shared service's handler asserts every row below. All rows pass.
  - Evidence: `go test ./<service-pkg>/... -v` prints one named subtest per row, all passing.

  | Request | Expected |
  |---|---|
  | `GET /readiness`, `PROVIDER_BASE_URL` unset | 200, body exactly `OK (PROVIDER_BASE_URL unset — provider reachability not checked)` |
  | `GET /readiness`, `PROVIDER_BASE_URL` set but unroutable | 503 |
  | `GET /metrics` | 200 |
  | `GET /prompt` | 405 |
  | `POST /prompt`, no `X-Session-Id` | 200; runner called with session id `identity` |
  | `POST /prompt`, `X-Session-Id:` present but empty | 400 |
  | `POST /prompt`, `X-Session-Id: bad id!` | 400 |
  | `POST /prompt`, body > 1 MiB | 200; body truncated to 1 MiB |
  | `POST /prompt`, empty body | 400 |
  | `POST /prompt`, valid | 200, `Content-Type: text/plain; charset=utf-8` |

- [ ] **AC3 — per-session locking is two-level, and no request is rejected by the lock.** Two concurrent requests on **distinct** session ids both reach the runner while the first is still blocked; two concurrent requests on the **same** id do not — the second's run starts only after the first returns.
  - Evidence: `go test ./<service-pkg>/... -v` exits 0 and lists the subtests `distinct ids overlap` and `same id serialises`, driven by a runner fake that blocks on a channel until released. The test fails if the map lock is held while a session lock is taken.
- [ ] **AC4 — each run is bracketed by a turn-boundary log pair.** A run on session id `X` emits `turn start id=X` after the session lock is acquired and `turn end id=X` after the runner returns, in that order. A request rejected before the lock (malformed id, empty body) emits neither.
  - Evidence: `go test ./<service-pkg>/... -v` exits 0 and lists the subtests `turn pair emitted` and `no turn line for rejected request`. The first captures `glog` output and asserts both lines, in order, with the id; the second asserts zero `turn start` lines across AC2's rejection cases.
- [ ] **AC5 — the pi session implementation delegates with the continuity config intact.** The pi session implementation satisfies the session interface, and opening a session by id produces a `pi.Runner` whose config carries `PersistSession: true` and `SessionID: id`; the task path (no id) keeps `PersistSession: false`.
  - Evidence: `go test ./pi/... -v` exits 0 and lists the subtests `session path persists` and `task path does not`, using a counterfeiter fake of `pi.Runner` and asserting the config values each path produces.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — format, generate, test, lint, license all clean
- `make test` — Ginkgo suite passes
- `git grep -nE 'ListenAndServe|ServeMux' -- '*.go'` — matches only under the new service package
- `go vet ./...` — no findings

### Operator-executable (runs on the host after PR merge, spec verification ladder)

- `cd ~/Documents/workspaces/agent && make test` — library suite green on the host

The three items below run only once the `agent-pi` hand-off has landed. They are the parent goal's end-to-end criterion, not a gate on this spec — this spec's own prompts can complete while they are still pending.

- `kubectlnukedev -n dev logs <pi-interactive-pod> --since=15m | grep -E 'turn (start|end) id='` — the turn-boundary pair appears in the pod log
- Two sequential `POST /prompt` calls on one `X-Session-Id` against `pi-interactive` — turn 1 asks the session to remember a word the operator picks at run time, turn 2 asks for it back — the second response carries that word
- `curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'X-Session-Id: bad id!' --data x http://<pi-interactive>:9090/prompt` — returns `400`

## Desired Behavior

1. **A runner-agnostic session interface exists in the library.** It models a conversation that outlives one request: opening a session by id, sending a prompt and receiving that turn's result, and closing it. It is the seam both backends implement, and the only thing the HTTP layer knows about a backend. `Close` exists for the sibling spec's consumer — the Claude implementation holds a process that must be terminated — and the pi implementation satisfies it trivially, since pi spawns per turn and holds nothing. No AC here tests `Close`; its exercise belongs to `claude-streaming-session.md`.

2. **The library owns the HTTP surface.** A constructor takes a session factory, a listen address, a provider base URL and a Prometheus registry, and returns something that serves `/readiness`, `/metrics` and `POST /prompt`. The registry is a parameter rather than a library-internal singleton, so each binary keeps its own metrics identity. The library imports `net/http` for the first time; no other package in the library gains that dependency.

3. **The existing `:9090` contract is reproduced exactly** — the rows of AC2, unchanged: method gates, status codes, the 1 MiB body cap, the `text/plain; charset=utf-8` response, the `X-Session-Id` regex `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`, the absent-header default of `identity`, and the present-but-empty-is-an-error rule. `/readiness` keeps both of its modes and its exact body strings.

4. **No request is rejected because of the lock, and no two sessions contend.** A session is built lazily on first use and cached by id. The cache's map lock guards only the map and is released before the caller takes the session's own lock, so a request for one session never blocks on another session's turn. Observable property: two different sessions run concurrently; two requests on one session serialise.

5. **Each run is bracketed by a turn-boundary log pair.** `turn start id=<id>` is emitted once the session lock is held and `turn end id=<id>` once the runner returns. The window between them is the lock-held window, which is what makes per-session serialisation observable from outside the process. Neither line is emitted for a request rejected before the lock.

6. **The pi backend supplies its session implementation over the existing runner.** Session continuity keeps using pi's `--session-id` flag and the `PersistSession`/`SessionID` pair on `PiRunnerConfig`; no new pi mechanism is introduced, and the task path's behaviour is unchanged.

## Constraints

**Interfaces that must not change.** `pi.Runner` (`Run(ctx context.Context, prompt string) (*Result, error)`) and `claude.ClaudeRunner` (`Run(ctx, prompt) (*ClaudeResult, error)`) keep their signatures — both are consumed outside this spec's scope. The session abstraction wraps them; it does not replace them. `PiRunnerConfig` keeps `PersistSession` and `SessionID` unchanged.

**Frozen HTTP contract.** Route set `/readiness`, `/metrics`, `POST /prompt`; default listen `:9090`; body cap 1 MiB; session header `X-Session-Id`; regex `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`; absent → `identity`; present-but-empty → 400; non-POST on `/prompt` → 405; readiness unset body exactly `OK (PROVIDER_BASE_URL unset — provider reachability not checked)`. These are agent-pi's current values and are frozen by this spec.

**Module and language.** `github.com/bborbe/agent`, Go 1.27.1. `github.com/prometheus/client_golang` is already a direct require of this module; `github.com/bborbe/http` is present as indirect and becomes direct when the service package imports it. No other new dependency is required.

**Code conventions.** Per `docs/dod.md` and the coding guidelines: exported types, functions and interfaces carry doc comments; errors are wrapped with `github.com/bborbe/errors` (`errors.Wrap(ctx, err, "…")`), never `fmt.Errorf` and never a bare `return err`; Interface → Constructor → Struct → Method, with the interface exported, the implementation struct private and named as the interface with a lowercased first letter, and `New*` returning the interface; factory functions are pure composition with no conditionals, no I/O and no `context.Background()`; no package-level mutable state and no `init()`; Ginkgo v2 / Gomega tests in `*_test` packages with counterfeiter mocks for every new interface.

**Logging.** The prompt is never logged by content — only its byte length and a truncated digest. The session id is not logged on the request path; the turn-boundary pair is the one place an id appears in a log line, and it is an id the caller supplied and the regex has already constrained.

**Related docs.** `docs/agent-network-security.md` states the namespace-scoped, unauthenticated posture this spec preserves. `docs/interaction-count.md` defines the per-turn metric the Claude session (sibling spec) feeds.

**Repository hygiene.** `README.md` is updated for the new package; `CHANGELOG.md` gets an entry under `## Unreleased`. The frozen `:9090` contract — routes, status codes, header name, regex, readiness body strings — is written to a new `docs/interactive-service.md`, so the interface both consumers honour outlives this spec. No `replace` or `exclude` directives in `go.mod`.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| A session id fails the regex | 400 before the body is read; no runner built; no turn log line | Caller corrects the id; no intervention |
| Two requests on the same session id | The second blocks on the session lock until the first returns, then runs its own turn | None — designed serialisation |
| Two requests on distinct ids | Both run concurrently; the map lock is held during neither | None — designed parallelism |
| The runner returns an error mid-turn | 500 `prompt failed`; the error is logged, never returned to the caller; `turn end` still fires | Inspect the pod log for the wrapped error; the session stays usable |
| The runner panics while holding the session lock | The lock is released by a deferred unlock, so the next request on that session does not deadlock. Whether the panic ends the process is the server's concern, not the session's | Inspect the pod log for the panic stack; the session needs no reset |
| The provider is unreachable at `/readiness` | 503 with the dial failure; `/prompt` keeps serving | Provider recovers; readiness flips back |
| `PROVIDER_BASE_URL` is unset | `/readiness` answers 200 with the explicit "not checked" body rather than a false OK | Set the variable if reachability should be checked |
| Many distinct session ids arrive | One cache entry each while in use; the cache holds at most `maxSessions` entries, dropping the least recently used to make room, and each entry is also closed and dropped once it has been idle for `sessionIdleTimeout`, so the held count is bounded rather than growing for the process lifetime | None — eviction is automatic. A session that is perpetually mid-turn is not evictable, so under sustained all-mid-turn load the cache is genuinely unbounded and says so in a warning naming the held count and the limit. `interactive_sessions_held` and `interactive_sessions_evicted_total` show the bound working (see Security / Abuse Cases) |
| A session id returns after being evicted | Its next turn builds a fresh conversation and is served normally; the earlier conversation is gone | None — the caller gets a working session that has simply started over |
| A body exceeds 1 MiB | Truncated to 1 MiB and processed; the caller gets a normal 200 with no signal that truncation happened | Accepted as pre-existing behaviour, preserved by this extraction. Rejecting instead is a separate change |
| A vendored copy of the library's routing appears in a consumer worktree | A consumer-side grep would false-FAIL | Consumer probes exclude `vendor/**` explicitly |
| The library gains `net/http` and a consumer's build breaks | `make precommit` fails in that consumer repo | Caught at prompt time in that repo, before merge |

## Security / Abuse Cases

The HTTP surface is reachable only inside its namespace; no authentication is added, matching the existing posture documented in `docs/agent-network-security.md`.

- **Input size.** The 1 MiB body cap is applied with `io.LimitReader` before the body is buffered, so an oversized request cannot exhaust memory. Note precisely what this does and does not do: it **truncates** an oversized body to 1 MiB and processes the truncated prompt, returning a normal 200 — it does not reject, and the caller cannot detect the truncation from the response. This is agent-pi's current behaviour and is preserved deliberately; turning it into a 400 is a separate, reviewable change, not part of an extraction.
- **Input validation.** The session id is validated against the anchored regex before the body is read and before any runner is built, so a malformed id can neither allocate a session nor reach a backend. The regex is anchored at both ends and forbids a leading `-`, so an id cannot traverse a filesystem path — relevant because both backends use the id as a session-file key.
- **Session accumulation.** Every distinct id creates a cache entry, and on a Claude-backed service each held entry is a live child process. This is now **mitigated by idle eviction and a size limit**, not accepted. Idle eviction bounds accumulation: `Run` starts a reaper that sweeps every 30 seconds and closes and drops every entry that has gone the configured `sessionIdleTimeout` without serving a turn. The size limit bounds concurrency, which idle eviction cannot: a session that keeps serving turns is never idle, so the cache now holds at most the configured `maxSessions` entries (defaulting to `interactive.DefaultMaxSessions`, 8, when non-positive) and closes and drops the least recently used one to make room, on the periodic sweep, so a burst arriving faster than the 30-second sweep is brought back at the next tick. A session serving a turn is never dropped to make room — the limit yields rather than killing a turn in flight — and when every candidate is mid-turn the limit cannot be enforced, which is logged as a warning naming the held count and the limit, so under sustained all-mid-turn load the cache is genuinely unbounded. A non-positive configured bound is replaced with its documented default (`interactive.DefaultSessionIdleTimeout`, 15 minutes, and `interactive.DefaultMaxSessions`, 8) rather than disabled, so a misconfiguration cannot restore the unbounded growth. A caller that returns after a long pause still gets a working session; it simply starts a fresh conversation, because the old entry was dropped. The remaining exposure is the namespace-scoped, operator-controlled id space the surface always had, and the bounds are observable from outside the process through `interactive_sessions_held` and `interactive_sessions_evicted_total`. The reason eviction is required at all: the original assumption that the id space was narrow enough not to matter did not survive measurement — see Assumptions.
- **Log injection.** The prompt is logged as a byte count and a truncated SHA-256 digest, never as content. The session id is not logged on the request path. The turn-boundary pair logs only the id, which the regex has already constrained to `[A-Za-z0-9_-]`.
- **Error leakage.** Runner errors are logged and never returned in the response body; the caller receives a fixed `prompt failed` string.

## Suggested Decomposition

Prompts are generated in this order, all inside `bborbe/agent`.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Session interface + the HTTP service (routes, cache, two-level lock, id resolution, readiness, turn logging) + tests | 1, 2, 3, 4, 5 | 1, 2, 3, 4 | — |
| 2 | pi session implementation over `pi.Runner` + tests | 6 | 5 | prompt 1 (implements the interface prompt 1 defines) |

Rationale: prompt 1 establishes the interface and the service, and is the only prompt that can define the seam. Prompt 2 is a thin adapter that needs the interface, so it follows.

### Hand-offs (not prompts of this spec)

Two consumer ports are required for the parent task's end-to-end criterion, and neither is generated here — the daemon for this project holds credentials for `bborbe/agent` only, so a prompt targeting another repo would fail on push and lose its commit on container exit.

| Consumer | Repo | Delivered by |
|---|---|---|
| Port `agent-pi` onto the shared service; delete its routing | `bborbe/agent-pi` | A prompt in that repo's own pipeline. `bborbe/agent-pi` is already scaffolded (`.dark-factory.yaml`, `docs/dod.md`, `prompts/`, `specs/` all present). |
| Add service mode to `agent-claude`; pin `@anthropic-ai/claude-code` | `bborbe/agent-claude` | A prompt in that repo's own pipeline, after scaffolding it (`prompts/`, `specs/`, `docs/dod.md`, a root `CLAUDE.md` — none exist today). |

## Do-Nothing Option

agent-claude's interactive mode is built by copying agent-pi's service code into its own `main.go`. That is the second copy this spec exists to prevent: the router, the session cache, the two-level lock, the id validation, the body cap and the readiness dial would then live in two repositories and drift independently, with nothing detecting the divergence until a runtime behaviour differs between the two images. The parent goal needs a third interactive backend eventually, which would be a third copy.

## Scenarios

No new scenario. The interactive continuity this spec's service enables is exercised end-to-end by the `agent-pi` hand-off's own verification (two sequential prompts on one session id against a deployed pod), which reaches the real CLI and the mounted volume through the real deployment path. A scenario harness would add a second, weaker copy of that probe without reaching anything the deployment check does not. The library-internal behaviour this spec actually adds — the contract table, the two-level lock, the turn-boundary pair — is reachable by unit tests and is not deployment-dependent.
