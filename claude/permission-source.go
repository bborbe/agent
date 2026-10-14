// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import "context"

// PermissionSource identifies which of the interactive service's turn-serving routes
// raised a permission request. It is the closed set {PromptPermissionSource,
// A2APermissionSource}: a turn is served by POST /prompt or by the A2A JSON-RPC binding,
// and no third route serves a turn today. A future third route must add its own value
// here and to docs/interactive-service.md.
type PermissionSource string

const (
	// PromptPermissionSource marks a request raised by a POST /prompt turn.
	PromptPermissionSource PermissionSource = "/prompt"
	// A2APermissionSource marks a request raised by a POST /a2a turn.
	A2APermissionSource PermissionSource = "/a2a"
)

// permissionSourceContextKey is the type of the context key a PermissionSource travels
// under. It is an unexported empty struct, so no other package can construct an equal
// key and no other package's value can be read back as a PermissionSource.
type permissionSourceContextKey struct{}

// WithPermissionSource returns a copy of ctx carrying source. The route that serves a
// turn sets it before calling Session.Prompt; the session reads it back in
// answerPermission and stamps it onto the PermissionRequest it hands the decider.
//
// A route must never populate it from request data. X-Session-Id is caller-controlled,
// so a caller that could choose its own source could make a pane-answered /prompt gate
// look like an A2A gate and route it to a channel the operator answers.
func WithPermissionSource(ctx context.Context, source PermissionSource) context.Context {
	return context.WithValue(ctx, permissionSourceContextKey{}, source)
}

// PermissionSourceFromContext returns the PermissionSource carried by ctx, or the empty
// PermissionSource when ctx carries none. The empty value is what a session whose
// caller named no route sees — a caller outside the interactive service, or a test that
// drives the session directly — and it is the value a decider written before this field
// existed already ignores.
func PermissionSourceFromContext(ctx context.Context) PermissionSource {
	source, _ := ctx.Value(permissionSourceContextKey{}).(PermissionSource)
	return source
}
