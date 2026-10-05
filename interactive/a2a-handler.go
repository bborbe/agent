// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"iter"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/golang/glog"
)

// a2aPath is the route the A2A JSON-RPC binding is served at. It is registered on the
// router the authentication gate wraps, so it is gated like every other non-exempt
// route: an unauthenticated request is refused before the handler runs.
const a2aPath = "/a2a"

// a2aHandler serves POST /a2a: the A2A JSON-RPC binding over the session seam.
//
// The body is capped before the SDK handler sees it. The SDK decodes with
// json.NewDecoder and applies no limit of its own, so without this cap an
// authenticated caller could exhaust the pod's memory with an unbounded body. A
// truncated JSON body cannot parse, so an oversized request is refused rather than
// processed.
func (s *service) a2aHandler() http.Handler {
	inner := a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(&a2aExecutor{cache: s.cache}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxPromptBytes)
		inner.ServeHTTP(w, r)
	})
}

// a2aExecutor bridges an A2A request to the session seam. One SendMessage runs one turn on
// the conversation named by the request's contextId, through the same session cache and
// per-session lock POST /prompt uses.
type a2aExecutor struct {
	cache *sessionCache
}

var _ a2asrv.AgentExecutor = (*a2aExecutor)(nil)

// Execute runs one turn on the conversation the caller named and yields the events that
// describe it: the submitted task, the agent's reply as an artifact, and the terminal
// status. A request that fails validation, or a turn that fails, yields an error or a
// failed status instead — the backend's error text is logged and never returned.
func (e *a2aExecutor) Execute(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		sessionID := execCtx.Message.ContextID
		if sessionID == "" {
			sessionID = defaultSessionID
		}
		// The id is caller-controlled and reaches a backend CLI as an argument, so it
		// is matched against the anchored pattern before any session is built: a
		// leading '-' would be read as a flag and '.' or '/' would escape a session
		// directory.
		if !sessionIDPattern.MatchString(sessionID) {
			yield(nil, a2a.ErrInvalidParams)
			return
		}

		prompt := promptFromMessage(execCtx.Message)
		if prompt == "" {
			yield(nil, a2a.ErrInvalidParams)
			return
		}

		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		result, err := e.cache.Get(sessionID).Prompt(ctx, prompt)
		if err != nil {
			glog.Warningf("a2a message failed: %v", err)
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, nil), nil)
			return
		}

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result)), nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

// Cancel yields the cancelled status the SDK's default cancellation shape uses. A one-shot
// SendMessage never has a live turn to interrupt, so this is a total function that matches
// the interface without claiming any cancellation semantics this service does not have.
func (e *a2aExecutor) Cancel(
	_ context.Context,
	execCtx *a2asrv.ExecutorContext,
) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// promptFromMessage concatenates the text of every part of an A2A message, in order. A
// non-text part contributes nothing, and a nil element contributes nothing. An empty
// result means the message carries no text and the request is refused before any session
// is built.
func promptFromMessage(message *a2a.Message) string {
	var builder strings.Builder
	for _, part := range message.Parts {
		// The wire format permits a literal `null` inside the `parts` array, and the
		// SDK decodes into []*Part, so such an element arrives as a nil pointer rather
		// than as a decode error — the SDK's own validation only rejects an empty
		// array. (*Part).Text() dereferences its receiver, so without this guard a
		// body carrying `"parts":[null]` panics the handler: the panic is recovered
		// upstream and surfaced to the caller as an internal error, which is both the
		// wrong answer for a malformed request and a stack trace in the log.
		if part == nil {
			continue
		}
		builder.WriteString(part.Text())
	}
	return strings.TrimSpace(builder.String())
}
