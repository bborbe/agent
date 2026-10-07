---
status: completed
spec: [054-shared-interactive-agent-service]
summary: Added a maxSessions size bound to the interactive session cache, evicting the least recently used session on both the allocation path and the sweep, with the new DefaultMaxSessions constant, a maxSessions constructor parameter, metrics and docs updated
execution_id: agent-session-cache-cap-exec-233-session-cache-cap
dark-factory-version: v0.196.0
created: "2026-10-07T12:54:19Z"
queued: "2026-10-07T12:54:19Z"
started: "2026-10-07T12:54:35Z"
completed: "2026-10-07T13:06:14Z"
---

# Cap the interactive session cache and evict the least recently used

<summary>
- The interactive service now drops a session that has gone a configured period without serving a turn, which bounds how many sessions accumulate over time.
- That alone does not bound how many are held at once: a session that keeps serving turns is never idle, so many simultaneous callers still grow the container until the kernel kills it.
- The cache now also has a maximum size, and once it is reached the least recently used session is closed and dropped to make room.
- The limit binds when a new session is created, not only on a periodic sweep, so a burst of callers arriving faster than the sweep cannot overshoot it.
- A session that is serving a turn is never dropped to make room, so the limit yields rather than killing a turn in flight — and when it has to yield, that is logged loudly rather than silently.
- The service reports how many sessions it is holding and how many it has dropped, so the limit is observable from outside the process.
- Nothing else changes: the same session id still reaches the same conversation while it stays in use, and a session that was dropped is rebuilt on its next turn.
</summary>

<objective>
Bound the number of sessions the interactive service holds at once, by closing and dropping the least recently used session whenever the cache is at its maximum size, so many simultaneous callers cannot grow the container's memory until the kernel kills it.
</objective>

<context>
Read CLAUDE.md for project conventions. This repo is a single Go module with one root `Makefile`; run `make` at the repo root.

This change follows directly from the idle-eviction change already merged. Read these before writing anything:
- `interactive/session-cache.go` — `sessionCache`, `sessionEntry`, `newSessionCache`, `Get`, `closeIdle`, `reap`, `DefaultSessionIdleTimeout`, and the two collectors. This is the file under change; `closeIdle` is the function whose locking discipline you are copying.
- `interactive/service.go` — `service`, `NewService`, `NewServiceWithPermissions`, `Run`, and the `reapInterval` constant. Both constructors gain a parameter.
- `interactive/prompt.go` and `interactive/a2a-handler.go` — the only two **production** callers of `cache.Get`; both hold a request `ctx` (`r.Context()` in the prompt handler, the `Execute` ctx in the A2A executor), which `Get` will now need. ⚠️ **The external `interactive_test` package calls `cache.Get` at 16 sites in `interactive/session-cache_test.go`** (lines 87, 98, 106, 110, 117, 124, 131, 144, 157, 164, 169, 204, 230, 231, 245, 264), and every one of those must gain the `ctx` argument too. They are not constructor sites, so requirement 7's file list does not cover them.
- `interactive/export_test.go` — the test-only shim the external `interactive_test` package uses to reach unexported symbols. It already exposes `CloseIdle`, `Reap`, `IdleTimeout` and `NewSessionCache`; follow that pattern.
- `interactive/session-cache_test.go` — the existing Ginkgo suite for this file; the new tests belong beside these and reuse its fakes and clock.
- `docs/interactive-service.md` — the repo's declared frozen contract. It documents the cache's behaviour, so it changes with the code.
- `specs/in-progress/054-shared-interactive-agent-service.md` — this change extends the eviction the previous one added, and **three** places in the spec describe only the *idle* bound, so all three now under-describe the real one: the Security / Abuse Cases paragraph, the Failure Modes row, and the **Assumptions** bullet *"The session map is bounded by idle eviction"*. Update all three in this change.
- `interactive/session-cache.go`'s own comments — several become false and are not `docs/` files, so requirement 8 lists them explicitly. One is load-bearing.
- ⚠️ The `NewSessionCache` shim is called at `interactive/session-cache_test.go:82`, `:184` and `:198`; each needs the new `maxSessions` argument, in addition to the 16 `cache.Get` sites above.

Why the idle bound is not enough, and this is the whole reason for the change: `closeIdle` skips any entry whose last turn is inside the idle window —
```go
if c.currentDateTime.Now().Time().Sub(entry.lastUsed) <= c.idleTimeout { entry.mu.Unlock(); continue }
```
— so a session that keeps serving turns is never evicted, however many such sessions there are. Measured on the deployed service: one held session costs roughly 88 MiB, and about eleven of them reach the container's 1 GiB limit, where it is OOMKilled. Idle eviction bounds *accumulation*; only a size limit bounds *concurrency*.

⚠️ **And a sweep-only limit is not enough either — this is the trap this change must not fall into.** `reapInterval` is 30 seconds, so a limit evaluated only on the sweep lets twelve session ids arriving inside one 30-second window reach 1 GiB and be killed before the first sweep runs. The limit therefore has to bind on the **allocation path** as well.

Pattern references — read these, do not invent a house style:
- `closeIdle` in `interactive/session-cache.go` — the locking discipline you must copy exactly: snapshot ids under the map lock, release it, `TryLock` each entry, skip the ones you cannot lock, delete only if the id still maps to the same pointer, and never call `Close` while holding the map lock.
- `metrics/metrics.go` and `metrics/metrics_test.go` — collector construction and how a metric is asserted through `registry.Gather()`.
- `github.com/bborbe/time/mocks` — `timemocks.CurrentDateTimeGetter`, the controllable clock the existing suite already uses.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md` — the cancellation contract for the existing sweep, which this change extends.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — this change adds a second `TryLock`-based locking site and releases the map lock across a call, so the mutex-discipline rules apply twice.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-prometheus-metrics-guide.md` — gauge versus counter, and label conventions.

Note on `make generate`: `make precommit` runs `generate`, which wipes and regenerates `mocks/`. This change alters no interface, so `mocks/` must come out byte-identical — if it does not, stop and say so rather than widening the change.
</context>

<requirements>
1. **Add an exported `DefaultMaxSessions` constant** in `interactive/session-cache.go`, beside `DefaultSessionIdleTimeout`. It is the number of sessions held at once when the caller supplies a non-positive value. Choose it from the measured cost and justify the exact value in the doc comment, stating all three terms so a later reader can re-derive it if any number moves:
   - the measured cost of one held session (~88 MiB),
   - the container's limit (1 GiB),
   - the allowances that make the headroom honest — the Go runtime and service baseline, and the **overshoot** the soft limit permits while candidates are mid-turn.
   `8` satisfies that (8 × 88 MiB = 704 MiB, ~31% nominal headroom). State the three inputs and the chosen value side by side; do not write it as `limit ÷ perSession`, which would imply a derivation that is not what produced it.

2. **Give `sessionCache` a `maxSessions int` field** and extend `newSessionCache` with a `maxSessions int` parameter, placed after `idleTimeout` and before `currentDateTime`. Normalise a non-positive value to `DefaultMaxSessions` and log a warning naming the value supplied — the same fail-closed shape `idleTimeout` already has, and for the same reason: a misconfiguration must not be able to switch the limit off.

3. **Add `enforceLimit(ctx context.Context, reserve int) int` on `*sessionCache`.** It closes and drops the **least recently used** entries until `len(byID)+reserve <= maxSessions`, and returns how many it evicted. **`reserve` is the number of slots the caller is about to consume**: the sweep passes `0`, and `Get` passes `1`, because `Get` inserts the entry it is about to create immediately after the call. Without `reserve` the contract is unsatisfiable in both directions — evicting to `len(byID) <= maxSessions` lets `Get` overshoot the limit by one, while evicting to `len(byID) < maxSessions` makes `Get` do the sweep's work and the two required tests contradict each other. It is a **two-pass** walk — reading `lastUsed` requires the entry's own lock, so the ordering cannot be known before the locks are taken:
   - **Pass 1 — rank.** Snapshot the ids under the map lock, then release it. For each id, re-resolve the entry under the map lock, then `TryLock` it. If `TryLock` fails the entry is mid-turn — skip it. If it succeeds, read `lastUsed` under that lock, record `(id, lastUsed, entry)`, and unlock. Sort the recorded candidates by `lastUsed`, oldest first. Read `lastUsed` **only under the entry's own mutex**; reading it under the map lock is a data race and `make test` runs with `-race`.
   - **Pass 2 — evict.** Walk the sorted candidates oldest first. `TryLock` each; if it fails it became mid-turn, so skip it and continue to the next. Otherwise delete the id from `byID` under the map lock **only if it still maps to this same entry pointer**, release the map lock, call `Close`, then unlock. Stop as soon as `len(byID)+reserve <= maxSessions`.
   - Never close a session that is serving a turn, and never defer the close to the end of that turn.
   - Log a failing `Close` as a warning rather than returning it — the entry is dropped either way, so a failure cannot wedge the walk.
   - **If every candidate is mid-turn the limit cannot be enforced.** Leave the count above it and log that at **`glog.Warningf`**, naming the held count and the limit — this is the one condition that defeats the bound, so it must be visible at the default verbosity rather than only under `-v=2`. Set the held gauge before returning so the over-limit state is visible in metrics even if the sweep is interrupted — the sweep wrapper's set is the authoritative one, and this early set only has to agree with it. Return 0. Do not block, spin, or wait for a turn to end. State in the doc comment that under sustained load where every session is perpetually mid-turn the cache is genuinely unbounded, and that this is the logged limit of the design rather than a temporary yield.

4. **Bind the limit on the allocation path, not only on the sweep.** Change `Get` to `Get(ctx context.Context, id string) *sessionEntry` and on the **miss path only** — when the id is absent — have it call `enforceLimit(ctx, 1)` **before inserting**, and with the map lock **not** held, since `enforceLimit` closes sessions. ⚠️ **A lookup hit must never reserve a slot**: calling it unconditionally would evict a sibling on every `Get` at the limit, which no mandated test would catch because they all hold at or below `DefaultMaxSessions`.
   ⚠️ **Because the map lock is released across that call, `Get` must re-resolve the id under the map lock before inserting and insert only if it is still absent.** Otherwise two concurrent first-uses of the same id both observe it absent, both run `enforceLimit`, and both insert — the map keeps the second, `factory.Create` ran twice, and the first session is orphaned and never closed. `-race` will not catch this: it is a logic race, not a data race.
   Note in the doc comment the residual this does **not** fix: two concurrent first-uses of the same id may each evict a different entry, dropping two live sessions to make room for one. That is accepted rather than solved — the error is on the safe side (the cache ends smaller, never over the limit), and a per-id in-flight marker is out of scope for this change. Update both callers (`interactive/prompt.go`, `interactive/a2a-handler.go`), which already hold a request `ctx`. Keep the sweep call as well: the sweep is what reclaims entries that went idle between allocations. A burst arriving faster than `reapInterval` must not be able to overshoot the limit.

5. **Evict idle entries before enforcing the limit.** A sweep must reclaim genuinely idle sessions first, so the limit only removes sessions that are actually competing for room. Run `closeIdle` then `enforceLimit(ctx, 0)` inside the existing sweep — the sweep reserves no slot, because it is not about to insert anything. Do not start a second goroutine or a second ticker. **Move the `interactive_sessions_held` set out of `closeIdle` and into the sweep that calls both**, so no sweep leaves the gauge stale after `closeIdle` — the sweep wrapper is the authoritative setter, and `enforceLimit`'s early set on the all-mid-turn return writes the same value, so the two always agree.
   Drive the rewritten spec and the combined-sweep assertions through the **existing `Reap` shim** with a short interval, asserting with `Eventually` — the sweep lives inline in `reap`'s ticker body and there is no separate sweep shim to call.
   ⚠️ **That invalidates an existing spec, and you are authorised to update it.** `interactive/session-cache_test.go`'s *"reports the live entry count and the eviction count"* asserts `interactive_sessions_held` **after a bare `CloseIdle` call**, so with the set moved out of `closeIdle` that assertion fails. Rewrite that spec to drive the sweep instead of calling `closeIdle` directly, so the suite still passes and still asserts the same property.

6. **Reuse the existing counter and name the reason in the log.** Both kinds of eviction advance `interactive_sessions_evicted_total`; do **not** add a label to it or introduce a third collector, because that changes an existing metric's shape for every consumer. Log the reason instead: `glog.V(2)` with `reason=idle` for a `closeIdle` eviction and `reason=capacity` for an `enforceLimit` one, each naming the session id.

7. **Thread `maxSessions` through both constructors.** Add it as the final parameter of `NewService` and `NewServiceWithPermissions` in `interactive/service.go`, after `sessionIdleTimeout`, and pass it to `newSessionCache`. Document it in both doc comments, including the non-positive normalisation.
   It is a parameter rather than a package constant on purpose, and the reason belongs in the doc comment: the right value is a function of the container's memory limit, which lives in the deployment rather than in this library — so a caller with a smaller pod must be able to state a smaller limit without waiting for a library release. This mirrors `sessionIdleTimeout`, and the same argument applies to both.
   ⚠️ This is a breaking change to both exported constructors, on top of the one the previous release made. Update every in-repo call site and every test that constructs the service — there are **five**: `interactive/service_test.go`, `interactive/a2a_test.go`, `interactive/auth_test.go`, `interactive/permission_test.go` and `interactive/session-cache_test.go` — so `make precommit` passes.

8. **Update the documentation in the same change.** All of these become wrong otherwise:
   - `CHANGELOG.md` — create a `## Unreleased` section (there is none today; the head is `## v0.98.0`) directly above the first `## ` heading, and put one bullet under it with the repo's `feat!:` prefix, describing the limit, the new parameter, the new constant with its arithmetic, that the limit binds on the allocation path as well as the sweep, and that it is a breaking change to both constructors.
   - `docs/interactive-service.md` — the frozen contract. It documents the cache's eviction behaviour and both constructor signatures; correct all of it, and document the new parameter and constant. ⚠️ **It also says the window in which a handler may have an entry evicted from under it is "bounded by the idle period" — that is no longer true**: `enforceLimit` can drop an entry that was used moments ago, so correct that sentence rather than leaving it.
   - `README.md` — the `interactive/` row describes the cache. It now also has a maximum size; **name the `DefaultMaxSessions` constant and its default in the row**.
   - `specs/in-progress/054-shared-interactive-agent-service.md` — **three** places describe only the idle bound and now under-describe the real one: the Security / Abuse Cases paragraph, the Failure Modes row for "Many distinct session ids arrive", and the **Assumptions** bullet *"The session map is bounded by idle eviction"* (which also ends "See Security / Abuse Cases"). Update all three, and **name the `DefaultMaxSessions` constant and its default in the Security / Abuse Cases paragraph**, as that paragraph already names `interactive.DefaultSessionIdleTimeout`. Add the mid-turn caveat to the Failure Modes row alongside the size bound: a session that is perpetually mid-turn is not evictable, so under sustained all-mid-turn load the cache is genuinely unbounded and says so in a warning.
   - `interactive/session-cache.go` — **the comments this change falsifies**, none of which is a `docs/` file: the `sessionsHeld` field comment (*"set on every Get and at the end of every closeIdle pass"* — the set moves to the sweep), the `sessionsEvicted` field comment, the `sessionsEvicted` collector's `Help` string (*"Total number of idle sessions evicted"* — it now counts capacity evictions too, and that string is user-visible in `/metrics`), `closeIdle`'s doc comment (drop the *"refreshes the held-sessions gauge"* clause, and the *"bounded by the idle period"* sentence — the same sentence you are correcting in `docs/interactive-service.md`), `Get`'s doc comment (entries are now also dropped when the cache is over its maximum size), and the struct comment.
     ⚠️ **`closeIdle`'s locking paragraph ends *"Get takes the map lock and never an entry lock"* — that becomes FALSE**, because `Get` now takes entry locks inside `enforceLimit`. That sentence is the stated justification for the whole locking design, so rewrite it as **"Get never takes an entry lock while holding the map lock"**, which is what actually preserves the no-cycle argument.

9. **Write tests** in `interactive/session-cache_test.go`, in the existing style, with `timemocks.CurrentDateTimeGetter` and the existing fakes. Extend `interactive/export_test.go` with the shims the external package needs, following the existing pattern — name them explicitly: `EnforceLimit(ctx context.Context, cache *sessionCache, reserve int) int` (mirroring `CloseIdle`) and `MaxSessions(cache) int` (mirroring `IdleTimeout`). Cover, at minimum:
   - **the limit is enforced on the allocation path**: with `maxSessions` sessions held, creating one more through `Get` drops exactly one immediately — **with no sweep at all** — and `Close` was called on the dropped entry exactly once;
   - the sweep enforces the limit too, and does not over-evict: with the count already at the limit, a sweep drops nothing; after one more `Get` the count is still at the limit, and a sweep still drops nothing;
   - **the allocation path cannot overshoot**: creating `maxSessions + 1` entries through `Get` alone, with no sweep at all, never lets the held count exceed the limit at any point;
   - **recency decides which one**: the dropped entry is the least recently used and the most recently used survives — advance the clock and serve a turn on one entry so the ordering is unambiguous;
   - an entry whose turn is in flight is **skipped and left in the cache** even when it is the least recently used, and a later entry is dropped instead;
   - when every candidate is mid-turn the limit is not enforced: nothing is closed, the held count stays above the limit, and the call returns 0 without blocking;
   - a non-positive `maxSessions` is normalised to `DefaultMaxSessions` (assert the effective value, not the argument);
   - idle eviction still happens and still takes precedence — an entry idle past the idle window is dropped by the idle pass, not counted against the limit;
   - `interactive_sessions_held` reflects the count after a sweep that both reclaims idle entries and enforces the limit, and the counter advances by the total;
   - the suite passes under `-race`, which `make test` already enables.

10. **Before finishing, re-run `<verification>` and confirm it passes; then walk each of the seven `<summary>` bullets and each numbered requirement above against the change, and state in your final message which test covers each behavioural one.**
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Run `make` at the repo root. This change is confined to `interactive/`, `docs/interactive-service.md`, `README.md`, `CHANGELOG.md` and `specs/in-progress/054-shared-interactive-agent-service.md`.
- Do NOT change `agent_session.go`'s `Session` interface, the meaning of a session id, the `/prompt` or `/a2a` contract, the response shape, or the permission route.
- Do NOT evict an entry that is mid-turn, and do NOT hold the cache's map lock while closing a session.
- Do NOT read `lastUsed` under the map lock.
- Do NOT add a second goroutine, ticker, or sweep.
- Do NOT add a label to `interactive_sessions_evicted_total` or introduce a third collector.
- Do NOT remove or weaken idle eviction; the limit is an addition to it, not a replacement.
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
grep -q 'DefaultMaxSessions' specs/in-progress/054-shared-interactive-agent-service.md
! grep -rq 'NewCounterVec' interactive/
awk '/^## /{sec=$0} /DefaultMaxSessions/{print "sits under: " sec}' CHANGELOG.md
```

Each command must exit 0, and the `awk` must print `sits under: ## Unreleased` — that is the changelog fold guard, and it is the one check whose *output* matters rather than its exit code.

All the checks except the `!`-prefixed one are presence assertions, so `grep -q` is the right form. The `!`-prefixed line asserts no labelled counter was introduced anywhere in the package — written as an absence on purpose, because `grep -c` exits 1 on a zero count and would fail the step it was meant to pass. It is stated as "no `NewCounterVec` in the package" rather than "no label on `interactive_sessions_evicted_total`" because a label is added by `NewCounterVec(CounterOpts{Name: …}, []string{…})`, which contains no `{` — a check for the metric name followed by a brace would pass whether or not a label had been added.
</verification>
