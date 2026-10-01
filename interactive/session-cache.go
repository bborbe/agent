// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"sync"

	"github.com/golang/glog"

	agentlib "github.com/bborbe/agent"
)

// sessionEntry is one conversation plus the lock that serializes its turns.
type sessionEntry struct {
	id      string
	session agentlib.Session
	mu      sync.Mutex
}

// Prompt runs one turn on this conversation, serialized against every other turn on
// the same session and bracketed by the turn-boundary log pair.
//
// The window between the two log lines is exactly the lock-held window, which is what
// makes per-session serialisation observable from outside the process. turn end is
// emitted whenever the runner returns, including when it returns an error; a panic
// inside the runner is the one case that skips it, and the deferred unlock still
// releases the lock so the next request on this id does not deadlock.
func (e *sessionEntry) Prompt(ctx context.Context, prompt string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	glog.V(2).Infof("turn start id=%s", e.id)
	result, err := e.session.Prompt(ctx, prompt)
	glog.V(2).Infof("turn end id=%s", e.id)
	return result, err
}

// sessionCache holds one entry per session id, built on first use and never evicted.
type sessionCache struct {
	factory agentlib.SessionFactory
	mu      sync.Mutex
	byID    map[string]*sessionEntry
}

// newSessionCache creates an empty cache over factory.
func newSessionCache(factory agentlib.SessionFactory) *sessionCache {
	return &sessionCache{factory: factory, byID: map[string]*sessionEntry{}}
}

// Get returns the entry for id, building and caching it on first use.
//
// The map lock guards the map and nothing else: it is released before the caller takes
// the entry's own lock, so two different sessions never contend here and one session's
// slow turn cannot delay another's lookup. Entries are never evicted; the id space is
// operator-controlled and the surface is namespace-scoped (spec 054, Security).
func (c *sessionCache) Get(id string) *sessionEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byID[id]
	if !ok {
		entry = &sessionEntry{id: id, session: c.factory.Create(id)}
		c.byID[id] = entry
	}
	return entry
}
