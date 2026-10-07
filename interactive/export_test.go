// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"time"
)

// ServerOptionFns exposes the unexported serverOptionFns to the external
// interactive_test package, so a spec can assert the server-wide write deadline
// through github.com/bborbe/http. Test-only: this file is a _test.go file, is
// compiled only into test builds, and adds nothing to the package's public API.
var ServerOptionFns = serverOptionFns

// SessionCache and SessionEntry are aliases onto the unexported cache types, so the
// external interactive_test package can drive the cache directly — construct it,
// resolve an entry, run a turn on it, and sweep it — without the cache becoming part
// of the package's public API. Because they are aliases rather than wrappers, two
// entries obtained from Get compare equal exactly when they are the same pointer,
// which is what lets a spec tell "skipped" apart from "evicted and re-created".
type (
	// SessionCache is the unexported sessionCache.
	SessionCache = sessionCache
	// SessionEntry is the unexported sessionEntry.
	SessionEntry = sessionEntry
)

// NewSessionCache exposes the unexported cache constructor to the external test
// package, so a spec can build a cache over a controllable clock and a caller-owned
// registry without going through a Service.
var NewSessionCache = newSessionCache

// CloseIdle exposes (*sessionCache).closeIdle to the external test package. A type
// alias does not carry the unexported method across the package boundary, so the sweep
// is reached through this function rather than as a method on SessionCache.
func CloseIdle(ctx context.Context, cache *sessionCache) int { return cache.closeIdle(ctx) }

// Reap exposes (*sessionCache).reap to the external test package, so a spec can drive
// the reaper's loop body directly with a short interval.
func Reap(ctx context.Context, cache *sessionCache, interval time.Duration) error {
	return cache.reap(ctx, interval)
}

// IdleTimeout exposes the effective idle period of a cache: the value it normalised
// the constructor argument to, which is what a non-positive-argument spec must assert
// rather than the argument itself.
func IdleTimeout(cache *sessionCache) time.Duration { return cache.idleTimeout }

// SetReapInterval shortens the idle-session reaper's period on a service built by the
// constructors, so an external spec can observe that Run actually starts the reaper
// without waiting out the production 30-second period.
func SetReapInterval(svc Service, interval time.Duration) {
	if s, ok := svc.(*service); ok {
		s.reapInterval = interval
	}
}
