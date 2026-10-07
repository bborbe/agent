// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"sort"
	"sync"
	"time"

	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
)

// DefaultSessionIdleTimeout is the idle period a non-positive configured value is
// replaced with. A session that has gone this long without serving a turn is closed
// and dropped from the cache. It is exported so a caller can name the effective
// bound it gets when it passes a non-positive value, and so a test can assert the
// normalisation rather than the argument.
const DefaultSessionIdleTimeout = 15 * time.Minute

// DefaultMaxSessions is the number of sessions the cache holds at once when the caller
// supplies a non-positive maximum. A session that keeps serving turns is never idle, so
// idle eviction alone bounds accumulation but not concurrency; this is the bound on how
// many conversations are held simultaneously.
//
// The value comes from three measured inputs, not from dividing the limit by the
// per-session cost: one held session costs roughly 88 MiB on the deployed
// Claude-backed service, the container's memory limit is 1 GiB, and the limit must
// leave honest headroom for the Go runtime and service baseline plus the overshoot the
// soft limit permits while every candidate is mid-turn. Eight sessions hold about
// 704 MiB and leave roughly 31% of the limit free for those allowances. It is exported
// so a caller can name the effective bound it gets when it passes a non-positive value,
// and so a test can assert the normalisation rather than the argument.
const DefaultMaxSessions = 8

// sessionEntry is one conversation plus the lock that serializes its turns.
type sessionEntry struct {
	id      string
	session agentlib.Session
	// currentDateTime is the clock lastUsed is stamped from. It is the cache's
	// injected clock, so the entry never reads the wall clock itself.
	currentDateTime libtime.CurrentDateTimeGetter
	mu              sync.Mutex
	// lastUsed is the instant the entry's last turn finished. It is guarded by
	// this entry's own mu — never by the cache's map lock — because Prompt writes
	// it while holding only this lock and closeIdle and enforceLimit read it after
	// TryLock.
	lastUsed time.Time
}

// Prompt runs one turn on this conversation, serialized against every other turn on
// the same session and bracketed by the turn-boundary log pair.
//
// The window between the two log lines is exactly the lock-held window, which is what
// makes per-session serialisation observable from outside the process. turn end is
// emitted whenever the runner returns, including when it returns an error; a panic
// inside the runner is the one case that skips it, and the deferred unlock still
// releases the lock so the next request on this id does not deadlock.
//
// The last-used stamp is deferred, so it is written however the runner returns — an
// erroring session that keeps being called is active, not idle, and must not be
// evicted out from under its caller.
func (e *sessionEntry) Prompt(ctx context.Context, prompt string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	defer func() { e.lastUsed = e.currentDateTime.Now().Time() }()
	glog.V(2).Infof("turn start id=%s", e.id)
	result, err := e.session.Prompt(ctx, prompt)
	glog.V(2).Infof("turn end id=%s", e.id)
	return result, err
}

// sessionCache holds one entry per session id, built on first use and dropped once it
// has been idle for the configured period or when the cache is at its maximum size.
type sessionCache struct {
	factory         agentlib.SessionFactory
	currentDateTime libtime.CurrentDateTimeGetter
	// idleTimeout is the effective idle period. It is always positive: a
	// non-positive value supplied to newSessionCache is replaced with
	// DefaultSessionIdleTimeout, so the bound can never be switched off.
	idleTimeout time.Duration
	// maxSessions is the effective maximum number of entries held at once. It is
	// always positive: a non-positive value supplied to newSessionCache is replaced
	// with DefaultMaxSessions, so the bound can never be switched off.
	maxSessions int
	mu          sync.Mutex
	byID        map[string]*sessionEntry
	// sessionsHeld reports the number of entries currently in byID. It is set on
	// every Get and at the end of every sweep.
	sessionsHeld prometheus.Gauge
	// sessionsEvicted counts the entries closeIdle and enforceLimit have closed and
	// dropped, whatever the reason.
	sessionsEvicted prometheus.Counter
}

// newSessionCache creates an empty cache over factory.
//
// idleTimeout is the period after which an idle session is closed and dropped; a
// non-positive value is replaced with DefaultSessionIdleTimeout and logged, because a
// non-positive period would silently restore the unbounded growth eviction exists to
// remove. maxSessions is the number of entries held at once; a non-positive value is
// replaced with DefaultMaxSessions and logged, for the same reason — a misconfiguration
// must not be able to switch the limit off. currentDateTime is the clock the idle
// comparison is made against, injected rather than read from the wall clock so a test
// can advance it. registry receives the two collectors this cache registers and must
// not be nil.
func newSessionCache(
	factory agentlib.SessionFactory,
	idleTimeout time.Duration,
	maxSessions int,
	currentDateTime libtime.CurrentDateTimeGetter,
	registry *prometheus.Registry,
) *sessionCache {
	if idleTimeout <= 0 {
		glog.Warningf(
			"session idle timeout %s is not positive; using default %s",
			idleTimeout,
			DefaultSessionIdleTimeout,
		)
		idleTimeout = DefaultSessionIdleTimeout
	}
	if maxSessions <= 0 {
		glog.Warningf(
			"session max sessions %d is not positive; using default %d",
			maxSessions,
			DefaultMaxSessions,
		)
		maxSessions = DefaultMaxSessions
	}
	sessionsHeld := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "interactive_sessions_held",
		Help: "Number of sessions currently held in the interactive session cache.",
	})
	sessionsEvicted := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "interactive_sessions_evicted_total",
		Help: "Total number of sessions evicted from the interactive session cache, by idle timeout or to stay within its maximum size.",
	})
	registry.MustRegister(sessionsHeld, sessionsEvicted)
	// Pre-initialise the counter so rate() evaluates to zero, not no-data, before
	// the first eviction.
	sessionsEvicted.Add(0)
	return &sessionCache{
		factory:         factory,
		currentDateTime: currentDateTime,
		idleTimeout:     idleTimeout,
		maxSessions:     maxSessions,
		byID:            map[string]*sessionEntry{},
		sessionsHeld:    sessionsHeld,
		sessionsEvicted: sessionsEvicted,
	}
}

// Get returns the entry for id, building and caching it on first use, and updates the
// held-sessions gauge.
//
// The map lock guards the map and nothing else: it is released before the caller takes
// the entry's own lock, so two different sessions never contend here and one session's
// slow turn cannot delay another's lookup. Entries are dropped once they have been idle
// for the configured period (see closeIdle) or when the cache is at its maximum size
// (see enforceLimit); the id space is operator-controlled and the surface is
// namespace-scoped, but a held entry is a live child process, so the count of held
// sessions is bounded by eviction and by the size limit rather than by the id space.
//
// On a miss only, Get reserves the slot it is about to occupy by calling enforceLimit
// before inserting, so a burst arriving faster than the sweep cannot overshoot the
// limit. A lookup hit reserves nothing: calling enforceLimit unconditionally would evict
// a sibling on every Get at the limit. enforceLimit closes sessions, so it runs with the
// map lock released; the id is therefore re-resolved under the map lock afterwards and
// inserted only if it is still absent, because a concurrent first-use of the same id may
// have inserted it while the lock was down. Without that re-check both callers would
// insert, the map would keep the second, and the first session would be orphaned and
// never closed.
//
// The residual this does not fix: two concurrent first-uses of the same id may each
// evict a different entry, dropping two live sessions to make room for one. That is
// accepted rather than solved — the error is on the safe side (the cache ends smaller,
// never over the limit) and a per-id in-flight marker is out of scope.
func (c *sessionCache) Get(ctx context.Context, id string) *sessionEntry {
	c.mu.Lock()
	if entry, ok := c.byID[id]; ok {
		c.sessionsHeld.Set(float64(len(c.byID)))
		c.mu.Unlock()
		return entry
	}
	c.mu.Unlock()

	// Miss: reserve the slot this call is about to consume, with the map lock released
	// because enforceLimit closes sessions.
	c.enforceLimit(ctx, 1)

	c.mu.Lock()
	defer c.mu.Unlock()
	// The map lock was released across enforceLimit, so the id may have been inserted
	// by a concurrent first-use in the meantime.
	if entry, ok := c.byID[id]; ok {
		c.sessionsHeld.Set(float64(len(c.byID)))
		return entry
	}
	entry := &sessionEntry{
		id:              id,
		session:         c.factory.Create(id),
		currentDateTime: c.currentDateTime,
	}
	c.byID[id] = entry
	c.sessionsHeld.Set(float64(len(c.byID)))
	return entry
}

// closeIdle closes and drops every entry that has been idle for longer than the
// configured period and returns how many it evicted. It also advances the eviction
// counter by that number; the held-sessions gauge is set by the sweep that calls it.
//
// An entry whose turn is in flight is skipped and left in the cache: TryLock fails
// while Prompt holds the entry lock, and a session serving a turn is active, not idle.
// The close is never deferred to the end of that turn — doing so would evict a session
// the instant it finished work.
//
// A handler that obtains an entry immediately before this method evicts it will see its
// turn fail on a closed session. That window is inherent to evicting at all; for an
// idle eviction it can only open for an entry that has already gone the whole period
// without a turn, though enforceLimit can also drop an entry that was used moments ago.
//
// Locking: ids are snapshotted under the map lock, which is then released before any
// entry is touched, because lastUsed is guarded by the entry's own mutex and reading it
// under the map lock would race. For each id the entry is re-resolved under the map
// lock (it may already be gone) and its own lock is taken with TryLock. The map lock is
// taken again briefly to delete the id only if it still maps to this same entry
// pointer, and is always released before Close runs. Holding the entry lock while
// briefly taking the map lock is safe: Get never takes an entry lock while holding the
// map lock, and Prompt takes an entry lock and never the map lock, so no cycle exists.
func (c *sessionCache) closeIdle(ctx context.Context) int {
	c.mu.Lock()
	ids := make([]string, 0, len(c.byID))
	for id := range c.byID {
		ids = append(ids, id)
	}
	c.mu.Unlock()

	evicted := 0
	for _, id := range ids {
		c.mu.Lock()
		entry, ok := c.byID[id]
		c.mu.Unlock()
		if !ok {
			continue
		}
		if !entry.mu.TryLock() {
			// Mid-turn: active, not idle. Leave it in the cache.
			continue
		}
		if c.currentDateTime.Now().Time().Sub(entry.lastUsed) <= c.idleTimeout {
			entry.mu.Unlock()
			continue
		}
		c.mu.Lock()
		if c.byID[id] == entry {
			delete(c.byID, id)
		}
		c.mu.Unlock()
		glog.V(2).Infof("evicting session reason=idle id=%s", id)
		if err := entry.session.Close(ctx); err != nil {
			// The entry is dropped from the cache either way, so a failing Close is
			// reported and not returned — it cannot wedge the sweep.
			glog.Warningf("close idle session id=%s failed: %v", id, err)
		}
		entry.mu.Unlock()
		evicted++
	}

	c.sessionsEvicted.Add(float64(evicted))
	return evicted
}

// enforceLimit closes and drops least-recently-used entries until len(byID)+reserve is
// at or below the cache's maximum size, and returns how many it evicted.
//
// reserve is the number of slots the caller is about to consume: the sweep passes 0,
// and Get passes 1 because it inserts the entry it is about to create immediately after
// this call. Without reserve the contract is unsatisfiable in both directions —
// evicting to len(byID) <= maxSessions lets Get overshoot by one, while evicting to
// len(byID) < maxSessions makes Get do the sweep's work.
//
// The walk is two-pass because reading lastUsed requires the entry's own lock, so the
// recency order cannot be known before the locks are taken. Pass one snapshots the ids
// under the map lock, releases it, TryLocks each entry to read lastUsed under its own
// mutex, and sorts the readable candidates oldest first; an entry that is mid-turn
// fails TryLock and is skipped. Pass two walks the sorted candidates oldest first,
// TryLocks each (skipping any that became mid-turn), deletes the id from the map only
// if it still maps to that same entry pointer, releases the map lock, and closes the
// session. A failing Close is logged as a warning rather than returned: the entry is
// dropped either way, so it cannot wedge the walk.
//
// A session serving a turn is never closed to make room, and the close is never
// deferred to the end of that turn. If every candidate is mid-turn the limit cannot be
// enforced: the count is left above it and a warning naming the held count and the
// limit is logged at the default verbosity, because this is the one condition that
// defeats the bound. Under sustained load where every session is perpetually mid-turn
// the cache is genuinely unbounded — that is the logged limit of this design, not a
// temporary yield, and this method does not block, spin, or wait for a turn to end.
func (c *sessionCache) enforceLimit(ctx context.Context, reserve int) int {
	type candidate struct {
		id       string
		lastUsed time.Time
		entry    *sessionEntry
	}

	c.mu.Lock()
	ids := make([]string, 0, len(c.byID))
	for id := range c.byID {
		ids = append(ids, id)
	}
	c.mu.Unlock()

	// Pass 1 — rank. lastUsed is read only under the entry's own mutex; reading it
	// under the map lock would be a data race.
	candidates := make([]candidate, 0, len(ids))
	for _, id := range ids {
		c.mu.Lock()
		entry, ok := c.byID[id]
		c.mu.Unlock()
		if !ok {
			continue
		}
		if !entry.mu.TryLock() {
			// Mid-turn: it cannot be ranked and must not be evicted.
			continue
		}
		candidates = append(candidates, candidate{
			id:       id,
			lastUsed: entry.lastUsed,
			entry:    entry,
		})
		entry.mu.Unlock()
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].lastUsed.Before(candidates[j].lastUsed)
	})

	// Pass 2 — evict, oldest first.
	evicted := 0
	for _, cand := range candidates {
		c.mu.Lock()
		held := len(c.byID)
		c.mu.Unlock()
		if held+reserve <= c.maxSessions {
			break
		}
		if !cand.entry.mu.TryLock() {
			// It became mid-turn between the passes. Skip it and try the next.
			continue
		}
		c.mu.Lock()
		if c.byID[cand.id] == cand.entry {
			delete(c.byID, cand.id)
		}
		c.mu.Unlock()
		glog.V(2).Infof("evicting session reason=capacity id=%s", cand.id)
		if err := cand.entry.session.Close(ctx); err != nil {
			glog.Warningf("close session id=%s failed: %v", cand.id, err)
		}
		cand.entry.mu.Unlock()
		evicted++
	}

	c.sessionsEvicted.Add(float64(evicted))

	c.mu.Lock()
	held := len(c.byID)
	c.mu.Unlock()
	if held+reserve > c.maxSessions {
		glog.Warningf(
			"session cache over maximum size: held=%d max=%d; every evictable candidate is mid-turn",
			held,
			c.maxSessions,
		)
		// Set the held gauge here so the over-limit state is visible even if the
		// sweep is interrupted; the sweep's own set writes the same value.
		c.sessionsHeld.Set(float64(held))
		// Return the real count, not 0: this branch is reached after pass two may
		// already have evicted entries, and the counter above was advanced by that
		// same number. Returning 0 here would contradict this function's contract and
		// make "nothing needed evicting" indistinguishable from "could not evict
		// enough" — the warning above and the held gauge are what carry that
		// distinction.
		return evicted
	}
	return evicted
}

// sweep reclaims genuinely idle entries first, then enforces the size limit on what is
// left, and finally publishes the held-sessions gauge. Reclaiming idle entries first
// means the limit only removes sessions that are actually competing for room. It is the
// single authoritative setter of the held gauge: closeIdle and enforceLimit do not set
// it on their normal paths, so no sweep can leave it stale.
func (c *sessionCache) sweep(ctx context.Context) {
	c.closeIdle(ctx)
	// The sweep reserves no slot: it is not about to insert anything.
	c.enforceLimit(ctx, 0)
	c.mu.Lock()
	c.sessionsHeld.Set(float64(len(c.byID)))
	c.mu.Unlock()
}

// reap calls sweep once per interval until ctx is cancelled, then returns nil. It is
// the reaper's loop body, kept separate from Run so it can be driven directly with a
// short interval in a test.
func (c *sessionCache) reap(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		c.sweep(ctx)
	}
}
