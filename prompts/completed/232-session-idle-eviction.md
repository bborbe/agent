---
status: completed
spec: [054-shared-interactive-agent-service]
summary: 'Added idle-session eviction to the interactive session cache: last-used stamping on sessionEntry, a closeIdle sweep with try-lock mid-turn skipping and non-positive-period normalisation to the exported DefaultSessionIdleTimeout, a reaper started by Run on a 30s reapInterval, interactive_sessions_held/interactive_sessions_evicted_total collectors, a new mandatory sessionIdleTimeout parameter on both constructors, and updated CHANGELOG/docs/spec 054.'
execution_id: agent-session-idle-eviction-exec-232-session-idle-eviction
dark-factory-version: v0.196.0
created: "2026-10-07T10:59:48Z"
queued: "2026-10-07T10:59:48Z"
started: "2026-10-07T11:00:48Z"
completed: "2026-10-07T11:10:52Z"
---


# Evict idle sessions from the interactive session cache

<summary>
- The interactive service holds one conversation per session id, and today it holds every one of them forever.
- Each held conversation is a live child process, so the container's memory grows with the number of distinct sessions it has ever served.
- Measured on the deployed service: memory is linear in session count at roughly 88 MiB per session, and about eleven sessions reach the container's 1 GiB limit, where the kernel kills it.
- An idle session now stops being held: once a session has gone a configured period without serving a turn, its conversation is closed and its cache entry is dropped.
- The conversation is re-created on the session's next turn, so a caller that returns after a long pause still gets a working session — it simply starts a fresh one.
- The service exposes how many sessions it is holding and how many it has evicted, so the bound is observable from outside the process rather than inferred from memory.
- A session that is mid-turn is never evicted: eviction skips it and leaves it in the cache.
- A misconfigured idle period cannot switch the bound off — a non-positive value is replaced with a documented default and logged.
- Behaviour is otherwise unchanged: the same session id still reaches the same conversation, and the existing tests still pass.
</summary>

<objective>
Bound the interactive service's memory by closing and dropping a session's conversation once it has been idle for a configured period, so the container stops growing without limit as distinct session ids accumulate. The bound must come from releasing processes, not from raising the container's memory limit — the measured growth is linear and unbounded, so a larger limit only moves the failure point.
</objective>

<context>
Read CLAUDE.md for project conventions. This repo is a single Go module with one root `Makefile` and no per-package Makefiles — run `make` at the repo root, not inside a package directory. (CLAUDE.md's "run in service dir, never at root" line is inherited from a sibling monorepo and does not describe this repo: `git ls-tree -r --name-only origin/master | grep -i makefile` returns exactly one path.)

Read these files before writing anything — they define the contract you are extending:
- `interactive/session-cache.go` — `sessionCache`, `sessionEntry`, `newSessionCache`, `Get`, `byID`, and `sessionEntry.Prompt`. This is the file under change.
- `interactive/service.go` — `service` struct, `NewService`, `NewServiceWithPermissions`, `Run`, and the `registry *prometheus.Registry` field. The cache is built in the two constructors and the reaper starts in `Run`.
- `interactive/prompt.go` — `promptHandler` calls `cache.Get(sessionID).Prompt(r.Context(), prompt)`.
- `interactive/a2a-handler.go` — `a2aExecutor.Execute` is the **second** caller of `cache.Get` (`result, err := e.cache.Get(sessionID).Prompt(ctx, prompt)`, ~line 79). These two are the **only** callers. Anything requirement 6 says happens "on every `Get`" must therefore live inside the cache itself, not at either call site — a change made in `prompt.go` alone leaves the A2A path's gauge stale.
- `interactive/export_test.go` — the test-only shim (package `interactive`) that exposes unexported symbols to the external `interactive_test` package; today it exposes only `ServerOptionFns`. Extend it to expose whatever requirement 9 must call directly — the external package cannot reach `closeIdle` or the cache constructor otherwise.
- `agent_session.go` — the `Session` interface. `Close(ctx) error` already exists on it and is already implemented by the Claude backend in `claude/claude-session.go` (~line 174, bounded by `sessionCloseGrace`); nothing in this repo currently calls it.
- `specs/in-progress/054-shared-interactive-agent-service.md` — this change **reverses an assumption that spec states explicitly**: under Assumptions it records *"The session map is not evicted. Session entries accumulate for the process lifetime"*, and its Failure Modes table carries the row *"Many distinct session ids arrive → One cache entry each, never evicted, for the process lifetime"*. The doc comment you will rewrite on `Get` cites it as `(spec 054, Security)`. Read the spec and update both places in this same change.
- `docs/interactive-service.md` — **the repo's declared frozen contract**: it opens *"This document is the frozen contract … it does not change without a spec"*, prints both constructor signatures (~lines 20–21) with their parameter table, and states at ~line 282 *"A session is built lazily on first use and cached by id, and the cache never evicts."* All three become wrong under this change.

Why the cache never evicts today (read the doc comment on `Get`): the id space was assumed operator-controlled and namespace-scoped. Measured 2026-10-07 against the deployed service, that assumption does not hold under the workload the platform intends — each distinct session id costs one held child process, and the container is OOMKilled at about eleven of them.

Pattern references — read these, do not invent a house style:
- `delivery/result-deliverer.go` — how `libtime.CurrentDateTimeGetter` (`github.com/bborbe/time`) is injected and used. Inject the clock; never call `time.Now()` in the cache.
- `github.com/bborbe/time/mocks` — `timemocks.CurrentDateTimeGetter` already ships a controllable clock (`NowReturns` / `NowReturnsOnCall`); use it in the tests rather than writing a new fake. (`SetNow` is **not** on this fake — it is on the concrete `libtime.CurrentDateTime` from `libtime.NewCurrentDateTime()`, which `metrics/metrics_test.go` uses, and on the separate `timemocks.CurrentDateTimeSetter`.) See `delivery/result-deliverer_test.go` for the fake's use.
- `metrics/metrics.go` — how a `*prometheus.Registry` is populated (`prometheus.NewGaugeVec`, `NewCounterVec`, `registry.MustRegister`). Follow the conventions there: pre-initialise counters with `.Add(0)`, and name a counter `<subsystem>_<name>_total`. `docs/job-metrics.md` documents the same conventions.
- `metrics/metrics_test.go` — how a metric is asserted (`registry.Gather()` + `dto.MetricFamily`), rather than by reaching into the collector.
- `interactive/service_test.go` — the Ginkgo/Gomega suite style, the external `interactive_test` package, and how a `prometheus.NewRegistry()` is passed to `NewService` in tests.
- `mocks/session-factory.go` and `mocks/session.go` — the existing `agentlib.SessionFactory` / `Session` counterfeiter fakes; `Session` already exposes `CloseCallCount`.
- For the reaper goroutine, read the coding guides `go-context-cancellation-in-loops.md` and `go-concurrency-patterns.md` (both under the coding plugin's `docs/` in the container). There is **no** ticker-loop exemplar in this repo — `claude/claude-plugin-installer.go` is a `for … select` over a slice, so do not treat it as one.
</context>

<requirements>
1. **Give `sessionEntry` a last-used timestamp.** Add a `lastUsed time.Time` field to `sessionEntry` in `interactive/session-cache.go`. It is guarded by the entry's **own** `e.mu` — not the cache's map lock. Stamp it at the end of `sessionEntry.Prompt` via a `defer`, so a turn that returns an error still counts as use: an erroring session that keeps being called is active, not idle.

2. **Inject the clock, the idle period and the registry into the cache.** `newSessionCache` gains the idle period (`time.Duration`), a `libtime.CurrentDateTimeGetter`, and the `*prometheus.Registry`. The cache declares and registers both collectors from requirement 6 and sets the gauge itself — inside `Get` and inside `closeIdle` — so the count is updated on every `Get` in the package, not only on the `POST /prompt` path.

3. **Add `closeIdle(ctx context.Context) int` on `*sessionCache`, and get its locking right.** `lastUsed` is written under `e.mu` (requirement 1), so reading it under the map lock is a data race — and `make test` runs `go test -race`, so requirement 9's concurrency test will fail on a racy implementation. The shape:
   - Snapshot the ids from `byID` under the map lock, then **release the map lock** before touching any entry.
   - For each id, resolve the entry under the map lock (it may already be gone) and call `entry.mu.TryLock()`.
     - **`TryLock` fails → the entry is mid-turn. Skip it entirely and leave it in `byID`.** A session serving a turn is active, not idle. Do **not** defer a close until the turn ends: that would evict a session the instant it finished work.
     - **`TryLock` succeeds →** compare `currentDateTime.Now().Time().Sub(entry.lastUsed)` against the idle period. If it is not older, unlock and skip.
   - For an idle entry: re-take the map lock and delete `byID[id]` **only if it still maps to this same entry pointer** (a concurrent `Get` may have replaced it), release the map lock, then call `entry.session.Close(ctx)` while still holding `entry.mu`, then unlock.
   - Never hold the map lock while calling `Close`. Holding `entry.mu` while briefly taking the map lock is safe — `Get` takes the map lock and never an entry lock, and `Prompt` takes an entry lock and never the map lock, so no cycle exists.
   - Return the number evicted. Log each eviction at `glog.V(2)` naming the session id, and log a failing `Close` as a warning rather than returning it — the entry is dropped from the cache either way, so a failure cannot wedge the loop.
   - Acknowledge in the doc comment that a handler which obtains an entry immediately before it is evicted will see its turn fail on a closed session. That window is inherent to the design and is bounded by the idle period.

4. **A non-positive idle period must not switch the bound off.** Because the parameter is mandatory at every call site (requirement 7), a non-positive value buys no compatibility and would silently restore the unbounded growth this change exists to remove. Normalise it at construction to an exported `DefaultSessionIdleTimeout` constant (15 minutes) and log a warning naming the value that was supplied. State the normalisation in both constructors' doc comments. Do not disable eviction.

5. **Start the reaper in `Run`.** `service.Run` currently returns `runServer(ctx)` directly. Before that, start a goroutine that ticks on a named `reapInterval` constant (30 seconds) and calls `closeIdle` each tick, stopping when `ctx.Done()` fires; then return `runServer(ctx)` unchanged. Follow `go-context-cancellation-in-loops.md` — the `select` must carry the `ctx.Done()` arm at the top of each iteration. Do not derive the tick from the idle period.

6. **Make the bound observable.** Declare and register two collectors on the `*prometheus.Registry` the cache now receives, following `metrics/metrics.go` and `docs/job-metrics.md`:
   - a **Gauge** `interactive_sessions_held` — the number of entries currently in `byID`, set inside `Get` and at the end of every `closeIdle` pass. A gauge is the right type for current state.
   - a **Counter** `interactive_sessions_evicted_total` — incremented by `closeIdle` **itself**, by the number it evicts, so a direct `closeIdle` call in a test observes the counter without starting `Run`.
   Pre-initialise the counter with `.Add(0)`.

7. **Thread the idle period through both constructors.** Add `sessionIdleTimeout time.Duration` as the final parameter of `NewService` and `NewServiceWithPermissions` in `interactive/service.go`, and pass it to `newSessionCache` together with the clock and the registry. `NewService` forwards it to `NewServiceWithPermissions` as it already forwards the other arguments. Document the parameter in both doc comments, including the non-positive normalisation from requirement 4.
   ⚠️ This is a breaking change to both exported constructors. Update every in-repo call site and every test that constructs the service — there are four files: `interactive/service_test.go`, `interactive/a2a_test.go`, `interactive/auth_test.go` and `interactive/permission_test.go` — so `make precommit` passes in this repo. A consumer outside this repo must be updated in the same release train; say so in the CHANGELOG entry rather than leaving it implicit.

8. **Update the documentation in the same change.** All three of these become wrong otherwise:
   - Add a `## Unreleased` entry at the top of `CHANGELOG.md` describing the new bound, the new parameter, and that it is a breaking change to the two exported constructors — use the repo's `feat!:` prefix for a breaking change (see the `v0.97.0` entry for the house shape, including its closing sentence about consumers adopting on bump).
   - Update `docs/interactive-service.md`: it declares itself the frozen contract, prints both constructor signatures (~lines 20–21) and their parameter table, and states "the cache never evicts" (~line 282). Correct all three, and document the two new metric names there.
   - Update `specs/in-progress/054-shared-interactive-agent-service.md`: **three** places now describe behaviour the code no longer has — the "session map is not evicted" Assumptions item, the matching Failure Modes row ("One cache entry each, never evicted, for the process lifetime"), and the "Unbounded session accumulation" bullet under Security / Abuse Cases, which closes *"A bounded map or TTL eviction is deliberately out of scope."* That last one is the most important: left alone, the spec's Security section would still assert that the very mechanism this change ships is out of scope. Rewrite all three.

9. **Write tests** in the existing `interactive/` Ginkgo suite, using the external `interactive_test` package and `timemocks.CurrentDateTimeGetter` for the clock. Extend `interactive/export_test.go` to expose what the external package must call directly — at minimum `closeIdle`, the cache constructor, `Get`, and an accessor for the effective idle period (the normalisation in requirement 4 is otherwise unassertable from outside). Cover, at minimum:
   - an entry idle longer than the period is evicted and its `Session.Close` was called exactly once;
   - an entry used within the period is **not** evicted;
   - `Get` after an eviction returns a **new** entry — the old one was dropped, so a returning caller gets a fresh conversation;
   - an entry whose turn is in flight is **skipped and left in the cache**: start a turn that blocks on a channel, advance the clock past the period, run `closeIdle`, and assert `Close` was **not** called; then release the turn and assert `Close` is still not called. Assert "still in the cache" by comparing the entry pointer `Get` returns before and after the `closeIdle` call (`==` on the two `:=`-inferred results) — that is what distinguishes *skipped* from *evicted-and-recreated*, and the two are otherwise indistinguishable from the outside;
   - a non-positive configured period is normalised to `DefaultSessionIdleTimeout` (assert the effective value, not the argument);
   - the gauge reads the live entry count and the counter matches the number of evictions, both asserted through `registry.Gather()` in the style of `metrics/metrics_test.go`;
   - **the reaper actually ticks, and this one is not optional.** Requirement 6 deliberately makes `closeIdle` callable on its own, so every test above would still pass if `Run` never started the goroutine — the eviction logic would be proven and the production path that triggers it not. Keep the loop body small and directly callable (e.g. a `reap(ctx context.Context, interval time.Duration) error` method that `Run` starts in a goroutine) and test it: assert `closeIdle` runs once per tick and that the method returns when `ctx` is cancelled. A `Run` that never starts the goroutine must not pass;
   - the suite passes under `-race`, which `make test` already enables.

   The new `closeIdle`, the reaper, and the two collectors must each be **at least 80% statement-covered** by the added tests.

10. **Before finishing, re-run `<verification>` and confirm it passes; then walk each of the nine `<summary>` bullets and each numbered requirement above against the change, and state in your final message which test covers each behavioural one.**
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Run `make` at the repo root. This change is confined to `interactive/`, `docs/interactive-service.md`, `specs/in-progress/054-shared-interactive-agent-service.md` and `CHANGELOG.md`.
- Do NOT change `agent_session.go`'s `Session` interface — `Close` already exists on it and is already implemented; you are adding the missing call site, not the method.
- Do NOT change the meaning of a session id, the `/prompt` or `/a2a` contract, the response shape, or the permission route.
- Do NOT evict an entry that is mid-turn, and do NOT hold the cache's map lock while closing a session.
- Do NOT read `lastUsed` under the map lock — it is guarded by the entry's own mutex, and `-race` will catch it.
- Do NOT introduce a hardcoded idle period: it is a constructor parameter. Do not disable eviction for any input.
- Do NOT raise or otherwise touch the container's memory limit; that lives in another repo and is out of scope for this change.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` at the repo root — it must pass. It includes `go test -race`.

Then confirm the new surface is present. Grep the package rather than one file, because the collectors may be declared wherever is cleanest inside `interactive/`:

```
grep -rq 'interactive_sessions_held' interactive/
grep -rq 'interactive_sessions_evicted_total' interactive/
grep -rq 'sessionIdleTimeout' interactive/
grep -rq 'closeIdle' interactive/
grep -rq 'DefaultSessionIdleTimeout' interactive/
grep -q 'sessionIdleTimeout' docs/interactive-service.md
grep -q 'reapInterval' interactive/
! grep -q 'the cache never evicts' docs/interactive-service.md
! grep -q 'session map is not evicted' specs/in-progress/054-shared-interactive-agent-service.md
! grep -q 'never evicted' specs/in-progress/054-shared-interactive-agent-service.md
```

Each command must exit 0 — including the two `!`-prefixed absence assertions, which are written that way on purpose: `grep -c` exits non-zero when the count is zero, so an absence expressed as "`grep -c` prints 0" fails the step it was meant to pass.
</verification>
