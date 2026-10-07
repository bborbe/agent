// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
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
	// it while holding only this lock and closeIdle reads it after TryLock.
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
// has been idle for the configured period.
type sessionCache struct {
	factory         agentlib.SessionFactory
	currentDateTime libtime.CurrentDateTimeGetter
	// idleTimeout is the effective idle period. It is always positive: a
	// non-positive value supplied to newSessionCache is replaced with
	// DefaultSessionIdleTimeout, so the bound can never be switched off.
	idleTimeout time.Duration
	mu          sync.Mutex
	byID        map[string]*sessionEntry
	// sessionsHeld reports the number of entries currently in byID. It is set on
	// every Get and at the end of every closeIdle pass.
	sessionsHeld prometheus.Gauge
	// sessionsEvicted counts the entries closeIdle has closed and dropped.
	sessionsEvicted prometheus.Counter
}

// newSessionCache creates an empty cache over factory.
//
// idleTimeout is the period after which an idle session is closed and dropped; a
// non-positive value is replaced with DefaultSessionIdleTimeout and logged, because a
// non-positive period would silently restore the unbounded growth eviction exists to
// remove. currentDateTime is the clock the idle comparison is made against, injected
// rather than read from the wall clock so a test can advance it. registry receives the
// two collectors this cache registers and must not be nil.
func newSessionCache(
	factory agentlib.SessionFactory,
	idleTimeout time.Duration,
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
	sessionsHeld := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "interactive_sessions_held",
		Help: "Number of sessions currently held in the interactive session cache.",
	})
	sessionsEvicted := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "interactive_sessions_evicted_total",
		Help: "Total number of idle sessions evicted from the interactive session cache.",
	})
	registry.MustRegister(sessionsHeld, sessionsEvicted)
	// Pre-initialise the counter so rate() evaluates to zero, not no-data, before
	// the first eviction.
	sessionsEvicted.Add(0)
	return &sessionCache{
		factory:         factory,
		currentDateTime: currentDateTime,
		idleTimeout:     idleTimeout,
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
// for the configured period (see closeIdle); the id space is operator-controlled and the
// surface is namespace-scoped, but a held entry is a live child process, so the count of
// held sessions is bounded by eviction rather than by the id space.
func (c *sessionCache) Get(id string) *sessionEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byID[id]
	if !ok {
		entry = &sessionEntry{
			id:              id,
			session:         c.factory.Create(id),
			currentDateTime: c.currentDateTime,
		}
		c.byID[id] = entry
	}
	c.sessionsHeld.Set(float64(len(c.byID)))
	return entry
}

// closeIdle closes and drops every entry that has been idle for longer than the
// configured period and returns how many it evicted. It also advances the eviction
// counter by that number and refreshes the held-sessions gauge.
//
// An entry whose turn is in flight is skipped and left in the cache: TryLock fails
// while Prompt holds the entry lock, and a session serving a turn is active, not idle.
// The close is never deferred to the end of that turn — doing so would evict a session
// the instant it finished work.
//
// A handler that obtains an entry immediately before this method evicts it will see its
// turn fail on a closed session. That window is inherent to evicting at all and is
// bounded by the idle period: it can only open for an entry that has already gone the
// whole period without a turn.
//
// Locking: ids are snapshotted under the map lock, which is then released before any
// entry is touched, because lastUsed is guarded by the entry's own mutex and reading it
// under the map lock would race. For each id the entry is re-resolved under the map
// lock (it may already be gone) and its own lock is taken with TryLock. The map lock is
// taken again briefly to delete the id only if it still maps to this same entry
// pointer, and is always released before Close runs. Holding the entry lock while
// briefly taking the map lock is safe: Get takes the map lock and never an entry lock,
// and Prompt takes an entry lock and never the map lock, so no cycle exists.
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
		glog.V(2).Infof("evicting idle session id=%s", id)
		if err := entry.session.Close(ctx); err != nil {
			// The entry is dropped from the cache either way, so a failing Close is
			// reported and not returned — it cannot wedge the sweep.
			glog.Warningf("close idle session id=%s failed: %v", id, err)
		}
		entry.mu.Unlock()
		evicted++
	}

	c.mu.Lock()
	c.sessionsHeld.Set(float64(len(c.byID)))
	c.mu.Unlock()
	c.sessionsEvicted.Add(float64(evicted))
	return evicted
}

// reap calls closeIdle once per interval until ctx is cancelled, then returns nil. It
// is the reaper's loop body, kept separate from Run so it can be driven directly with a
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
		c.closeIdle(ctx)
	}
}
