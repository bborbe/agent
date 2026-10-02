// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"sync"

	"github.com/bborbe/errors"
	"github.com/google/uuid"

	"github.com/bborbe/agent/claude"
)

// PendingPermission is one permission request currently waiting for a verdict, as
// served on GET /permission.
type PendingPermission struct {
	// ID identifies the request. It is generated per request and is the only handle
	// a verdict may name.
	ID string `json:"id"`
	// ToolName is the name of the tool the CLI wants to invoke.
	ToolName string `json:"tool_name"`
	// Description is the CLI's human-readable description of the invocation.
	Description string `json:"description"`
	// InputPreview is a preview of the tool input. It is never logged.
	InputPreview string `json:"input_preview"`
}

//counterfeiter:generate -o ../mocks/interactive-permission-registry.go --fake-name InteractivePermissionRegistry . PermissionRegistry

// PermissionRegistry holds the permission requests one service is currently waiting
// on. It is the PermissionDecider the sessions built for that service consult, so a
// request raised by a turn appears on that service's endpoint and nowhere else.
type PermissionRegistry interface {
	// DecidePermission registers request as pending and blocks until a verdict is
	// posted for it or ctx is cancelled.
	DecidePermission(
		ctx context.Context,
		request claude.PermissionRequest,
	) (claude.PermissionDecision, error)

	// List returns the requests currently pending. The result is never nil, so the
	// endpoint always renders a JSON array.
	List() []PendingPermission

	// Resolve delivers decision to the pending request named by id and reports
	// whether such a request was pending. It never blocks.
	Resolve(id string, decision claude.PermissionDecision) bool
}

// The registry is the PermissionDecider a session consults; the assignment pins the
// method set at compile time.
var _ claude.PermissionDecider = (PermissionRegistry)(nil)

// pendingPermission is one waiting request plus the buffered channel its verdict is
// delivered on.
type pendingPermission struct {
	request claude.PermissionRequest
	verdict chan claude.PermissionDecision
}

// permissionRegistry is the per-service implementation of PermissionRegistry.
type permissionRegistry struct {
	mu      sync.Mutex
	pending map[string]*pendingPermission
}

// NewPermissionRegistry creates an empty registry. Construct one per service and pass
// the same instance to the session factory and to NewServiceWithPermissions, so a
// request raised by a session is served by that service's endpoint.
func NewPermissionRegistry() PermissionRegistry {
	return &permissionRegistry{pending: map[string]*pendingPermission{}}
}

// DecidePermission registers request as pending and blocks until a verdict is posted
// for it or ctx is cancelled.
//
// The map lock is held only around the map write that registers the entry; it is
// never held across the wait, so one paused turn cannot stall a lookup for another.
// The entry is removed exactly once: by Resolve when a verdict is delivered, or here
// when ctx is cancelled — whichever lands first deletes it and the loser finds no
// entry.
func (r *permissionRegistry) DecidePermission(
	ctx context.Context,
	request claude.PermissionRequest,
) (claude.PermissionDecision, error) {
	id := uuid.New().String()
	verdict := make(chan claude.PermissionDecision, 1)

	r.mu.Lock()
	r.pending[id] = &pendingPermission{request: request, verdict: verdict}
	r.mu.Unlock()

	select {
	case decision := <-verdict:
		return decision, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
		return claude.PermissionDecision{}, errors.Wrap(
			ctx,
			ctx.Err(),
			"permission request cancelled",
		)
	}
}

// List returns the requests currently pending. The result is never nil, so the
// endpoint always renders a JSON array.
func (r *permissionRegistry) List() []PendingPermission {
	r.mu.Lock()
	defer r.mu.Unlock()

	pending := make([]PendingPermission, 0, len(r.pending))
	for id, entry := range r.pending {
		pending = append(pending, PendingPermission{
			ID:           id,
			ToolName:     entry.request.ToolName,
			Description:  entry.request.Description,
			InputPreview: entry.request.InputPreview,
		})
	}
	return pending
}

// Resolve delivers decision to the pending request named by id and reports whether
// such a request was pending. It never blocks.
//
// The entry is removed under the lock and the decision is sent after the lock is
// released; because the channel is buffered with capacity one and the entry was just
// removed, the send never blocks and a second Resolve for the same id finds nothing.
func (r *permissionRegistry) Resolve(id string, decision claude.PermissionDecision) bool {
	r.mu.Lock()
	entry, ok := r.pending[id]
	if ok {
		delete(r.pending, id)
	}
	r.mu.Unlock()

	if !ok {
		return false
	}
	entry.verdict <- decision
	return true
}
