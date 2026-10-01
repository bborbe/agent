// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "context"

//counterfeiter:generate -o mocks/session.go --fake-name Session . Session

// Session is one conversation with a backend that outlives a single request.
//
// It is the seam the interactive HTTP service depends on: the service holds one
// Session per session id and never learns which backend backs it. A backend
// implements it over whatever mechanism gives it continuity — pi's --session-id,
// a Claude process held open — and a backend that holds nothing between turns
// still satisfies it, because Close may be a no-op.
type Session interface {
	// Prompt sends one turn to the conversation and returns that turn's result.
	Prompt(ctx context.Context, prompt string) (string, error)

	// Close releases the resources the conversation holds. It is called when the
	// session is no longer needed and must be safe to call once; an implementation
	// that holds nothing returns nil.
	Close(ctx context.Context) error
}

//counterfeiter:generate -o mocks/session-factory.go --fake-name SessionFactory . SessionFactory

// SessionFactory builds the Session for one session id.
//
// Create is construction only: it performs no I/O and cannot fail, so a session
// exists exactly when a caller addresses it and an unseen id starts a fresh
// conversation with no registration step. Any failure a backend can have
// surfaces from Session.Prompt, where the HTTP service already has a defined
// failure path.
type SessionFactory interface {
	// Create returns the session for id, building it on first use.
	Create(id string) Session
}
