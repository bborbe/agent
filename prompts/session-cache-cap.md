---
status: draft
---

# Cap the interactive session cache and evict the least recently used

<summary>
- The interactive service now drops a session that has gone a configured period without serving a turn, which bounds how many sessions accumulate over time.
- That alone does not bound how many are held at once: a session that keeps serving turns is never idle, so a burst of simultaneous callers still grows the container until the kernel kills it.
- The cache now also has a maximum size, and once it is reached the least recently used session is closed and dropped to make room.
- A session that is serving a turn is never dropped to make room, so the cap yields temporarily rather than killing a turn in flight.
- The service reports how many sessions it is holding and how many it has dropped, so the cap is observable from outside the process.
- An operator can see from the log whether a session was dropped for being idle or dropped to make room.
- Nothing else changes: the same session id still reaches the same conversation while it stays in use, and a session that was dropped is rebuilt on its next turn.
</summary>

<objective>
Bound the number of sessions the interactive service holds at once, by closing and dropping the least recently used session whenever the cache is at its maximum size, so a burst of simultaneous callers cannot grow the container's memory until the kernel kills it.
</objective>

<context>
Read CLAUDE.md for project conventions. This repo is a single Go module with one root `Makefile`; run `make` at the repo root.

This change follows directly from the idle-eviction change already merged. Read these before writing anything:
- `interactive/session-cache.go` — `sessionCache`, `sessionEntry`, `newSessionCache`, `Get`, `closeIdle`, `reap`, `DefaultSessionIdleTimeout`, and the two collectors. This is the file under change; `closeIdle` is the function whose locking discipline you are copying.
- `interactive/service.go` — `service`, `NewService`, `NewServiceWithPermissions`, `Run`, and the `reapInterval` constant. Both constructors gain a parameter.
- `interactive/export_test.go` — the test-only shim the external `interactive_test` package uses to reach unexported symbols. Extend it for whatever the new tests must call.
- `interactive/session-cache_test.go` — the existing Ginkgo suite for this file; the new tests belong beside these and should reuse its fakes and clock.
- `docs/interactive-service.md` — the repo's declared frozen contract. It documents the cache's behaviour, so it changes with the code.

Why the idle bound is not enough, and this is the whole reason for the change: `closeIdle` skips any entry whose last turn is inside the idle window —
```go
if c.currentDateTime.Now().Time().Sub(entry.lastUsed) <= c.idleTimeout { entry.mu.Unlock(); continue }
```
— so a session that keeps serving turns is never evicted, however many such sessions there are. Measured on the deployed service: one held session costs roughly 88 MiB, and about eleven of them reach the container's 1 GiB limit, where it is OOMKilled. Idle eviction bounds *accumulation*; only a size cap bounds *concurrency*.

Pattern references — read these, do not invent a house style:
- `closeIdle` in `interactive/session-cache.go` — the locking discipline you must copy exactly: snapshot ids under the map lock, release it, `TryLock` each entry, skip the ones you cannot lock, delete only if the id still maps to the same pointer, and never call `Close` while holding the map lock.
- `metrics/metrics.go` and `metrics/metrics_test.go` — collector construction and how a metric is asserted through `registry.Gather()`.
- `github.com/bborbe/time/mocks` — `timemocks.CurrentDateTimeGetter`, the controllable clock the existing suite already uses.
- For the sweep loop, `go-context-cancellation-in-loops.md` and `go-concurrency-patterns.md` under the coding plugin's `docs/` in the container.
</context>

<requirements>
1. **Add an exported `DefaultMaxSessions` constant** in `interactive/session-cache.go`, beside `DefaultSessionIdleTimeout`. It is the number of sessions held at once when the caller supplies a non-positive value. Choose it from the measured cost rather than round numbers: a held session is about 88 MiB and the container's limit is 1 GiB, so the constant must leave clear headroom below the limit at the cap. **`8`** satisfies that; state the arithmetic in the doc comment so a later reader can re-derive it if either number moves.

2. **Give `sessionCache` a `maxSessions int` field** and extend `newSessionCache` with a `maxSessions int` parameter, placed after `idleTimeout` and before `currentDateTime`. Normalise a non-positive value to `DefaultMaxSessions` and log a warning naming the value supplied — the same fail-closed shape `idleTimeout` already has, and for the same reason: a misconfiguration must not be able to switch the bound off.

3. **Add `enforceLimit(ctx context.Context) int` on `*sessionCache`.** It closes and drops the **least recently used** entries until `len(byID) <= maxSessions`, and returns how many it evicted. Copy `closeIdle`'s locking discipline exactly:
   - Snapshot the ids under the map lock, then release it before touching any entry.
   - Order the candidates by `lastUsed`, oldest first. Read `lastUsed` **only under the entry's own mutex** — reading it under the map lock is a data race, and `make test` runs with `-race`.
   - `TryLock` each candidate in that order. **If it fails, the entry is mid-turn: skip it and move to the next candidate.** Never close a session that is serving a turn, and never defer the close to the end of that turn.
   - Delete the id from `byID` under the map lock only if it still maps to the same entry pointer, and release the map lock before calling `Close`.
   - Log a failing `Close` as a warning rather than returning it — the entry is dropped either way, so a failure cannot wedge the sweep.
   - **If every candidate is mid-turn the cap cannot be enforced.** Leave the count above the cap, log that at `glog.V(2)` naming the held count and the cap, and return 0. Do not block, spin, or wait for a turn to end.

4. **Evict idle entries before enforcing the cap.** A sweep must reclaim genuinely idle sessions first, so the cap only removes sessions that are actually competing for room. Add `enforceLimit` to the same sweep `closeIdle` runs in — do not start a second goroutine or a second ticker.

5. **Reuse the existing counter and name the reason in the log.** Both kinds of eviction advance `interactive_sessions_evicted_total`; do **not** add a label to it or a third collector, because that changes an existing metric's shape for every consumer. Log the reason instead: `glog.V(2)` with `reason=idle` for a `closeIdle` eviction and `reason=capacity` for an `enforceLimit` one, each naming the session id. The `interactive_sessions_held` gauge must be set at the end of the whole sweep so it reflects the count after both passes.

6. **Thread `maxSessions` through both constructors.** Add it as the final parameter of `NewService` and `NewServiceWithPermissions` in `interactive/service.go`, after `sessionIdleTimeout`, and pass it to `newSessionCache`. Document it in both doc comments, including the non-positive normalisation.
   ⚠️ This is a breaking change to both exported constructors, on top of the one the previous release made. Update every in-repo call site and every test that constructs the service — `interactive/service_test.go`, `interactive/a2a_test.go`, `interactive/auth_test.go` and `interactive/permission_test.go` — so `make precommit` passes.

7. **Update the documentation in the same change.** All of these become wrong otherwise:
   - `CHANGELOG.md` — a `## Unreleased` entry with the repo's `feat!:` prefix, describing the cap, the new parameter, the new constant with its arithmetic, and that it is a breaking change to both constructors.
   - `docs/interactive-service.md` — the frozen contract. It documents the cache's eviction behaviour and both constructor signatures; correct all of it, and document the new parameter and constant.
   - `README.md` — the `interactive/` row describes the cache; it now also has a maximum size.

8. **Write tests** in `interactive/session-cache_test.go`, in the existing style, with `timemocks.CurrentDateTimeGetter` and the existing fakes. Extend `interactive/export_test.go` for whatever the external package must call. Cover, at minimum:
   - the cap is enforced: with `maxSessions` sessions held, adding one more and running a sweep drops exactly one and `Close` was called on it exactly once;
   - **recency decides which one**: the dropped entry is the least recently used, and the most recently used survives — advance the clock and serve a turn on one entry to make the ordering unambiguous;
   - an entry whose turn is in flight is **skipped and left in the cache** even when it is the least recently used, and a later entry is dropped instead;
   - when every candidate is mid-turn the cap is not enforced: nothing is closed, the held count stays above the cap, and the call returns 0 without blocking;
   - a non-positive `maxSessions` is normalised to `DefaultMaxSessions` (assert the effective value, not the argument);
   - idle eviction still happens and still takes precedence — an entry idle past the idle window is dropped by the idle pass, not counted against the cap;
   - `interactive_sessions_held` reflects the count after a sweep that both reclaims idle entries and enforces the cap, and the counter advances by the total;
   - the suite passes under `-race`, which `make test` already enables.

9. **Before finishing, re-run `<verification>` and confirm it passes; then walk each of the seven `<summary>` bullets and each numbered requirement above against the change, and state in your final message which test covers each behavioural one.**
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Run `make` at the repo root. This change is confined to `interactive/`, `docs/interactive-service.md`, `README.md` and `CHANGELOG.md`.
- Do NOT change `agent_session.go`'s `Session` interface, the meaning of a session id, the `/prompt` or `/a2a` contract, the response shape, or the permission route.
- Do NOT evict an entry that is mid-turn, and do NOT hold the cache's map lock while closing a session.
- Do NOT read `lastUsed` under the map lock.
- Do NOT add a second goroutine, ticker, or sweep — `enforceLimit` runs inside the existing sweep.
- Do NOT add a label to `interactive_sessions_evicted_total` or introduce a third collector.
- Do NOT remove or weaken idle eviction; the cap is an addition to it, not a replacement.
- Do NOT raise or otherwise touch the container's memory limit; that lives in another repo.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` at the repo root — it must pass. It includes `go test -race`.

Then confirm the new surface is present:

```
grep -rq 'DefaultMaxSessions' interactive/
grep -rq 'enforceLimit' interactive/
grep -rq 'maxSessions' interactive/
grep -q 'DefaultMaxSessions' docs/interactive-service.md
grep -q 'DefaultMaxSessions' README.md
grep -q 'DefaultMaxSessions' CHANGELOG.md
! grep -q 'interactive_sessions_evicted_total{' interactive/
awk '/^## /{sec=$0} /DefaultMaxSessions/{print "sits under: " sec}' CHANGELOG.md
```

Each command must exit 0, and the `awk` must print `sits under: ## Unreleased` — that is the changelog fold guard, and it is the one check whose *output* matters rather than its exit code.

All the checks except the `!`-prefixed one are presence assertions, so `grep -q` is the right form. The `!`-prefixed line asserts the counter gained **no label** — written as an absence on purpose, because `grep -c` exits 1 on a zero count and would fail the step it was meant to pass.
</verification>
