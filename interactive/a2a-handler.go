// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive

import (
	"context"
	"iter"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/bborbe/errors"
	"github.com/golang/glog"

	"github.com/bborbe/agent/claude"
)

// a2aPath is the route the A2A JSON-RPC binding is served at. It is registered on the
// router the authentication gate wraps, so it is gated like every other non-exempt
// route: an unauthenticated request is refused before the handler runs.
const a2aPath = "/a2a"

// midTurnStatusDelay is how long a turn must still be running before the executor yields its
// mid-turn working-state status update. It is a fixed constant, not a parameter: the consumer
// that reads this channel pins the same value, so a knob here would let one caller change
// another's contract, and no second consumer exists to justify it. The delay is measured from
// the start of the turn, so a turn that finishes first emits no mid-turn event at all.
const midTurnStatusDelay = 10 * time.Second

// midTurnStatusText is the text of the mid-turn working status. It is a fixed string that
// reports the state and nothing else, so it carries no credential and never echoes the
// caller's prompt.
const midTurnStatusText = "working"

// completionStatusText is the text of the terminal completion status. The completion event
// carries it so the event has a message, and therefore a messageId: a registered push
// callback's receiver records that id, and a nil message leaves it nothing to record.
const completionStatusText = "completed"

// a2aHandler serves POST /a2a: the A2A JSON-RPC binding over the session seam.
//
// The body is capped before the SDK handler sees it. The SDK decodes with
// json.NewDecoder and applies no limit of its own, so without this cap an
// authenticated caller could exhaust the pod's memory with an unbounded body. A
// truncated JSON body cannot parse, so an oversized request is refused rather than
// processed.
func (s *service) a2aHandler() http.Handler {
	inner := a2asrv.NewJSONRPCHandler(
		a2asrv.NewHandler(&a2aExecutor{cache: s.cache}, s.handlerOptions()...),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxPromptBytes)
		inner.ServeHTTP(w, r)
	})
}

// handlerOptions returns the SDK request-handler options this service builds its A2A handler
// with: the caller's push wiring when it was supplied, and nothing otherwise. It is the only
// place the SDK's push support is switched on.
//
// WithCapabilityChecks is deliberately NOT passed. It gates GetExtendedAgentCard as well as
// push, so passing it would change that method's error from the SDK's
// ErrExtendedCardNotConfigured to ErrUnsupportedOperation — an unrelated observable change.
// The fail-closed guarantee comes from the single decision the card and this method share:
// the SDK refuses a push-config registration when the store and sender are absent
// (a2asrv/handler.go, ErrPushNotificationNotSupported), so an unconfigured service refuses
// registrations with or without capability checks.
func (s *service) handlerOptions() []a2asrv.RequestHandlerOption {
	if s.push == nil {
		return nil
	}
	return []a2asrv.RequestHandlerOption{
		a2asrv.WithPushNotifications(s.push.store, s.push.sender),
	}
}

// a2aExecutor bridges an A2A request to the session seam. One SendMessage runs one turn on
// the conversation named by the request's contextId, through the same session cache and
// per-session lock POST /prompt uses.
type a2aExecutor struct {
	cache *sessionCache
}

var _ a2asrv.AgentExecutor = (*a2aExecutor)(nil)

// midTurnStatusEvent builds the mid-turn working-state status update: a working state carrying
// a non-empty text message. It is built through the SDK's own message constructors so the
// event marshals as the SDK's status-update shape, which is what a registered callback
// receives.
func midTurnStatusEvent(execCtx *a2asrv.ExecutorContext) a2a.Event {
	return a2a.NewStatusUpdateEvent(
		execCtx,
		a2a.TaskStateWorking,
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(midTurnStatusText)),
	)
}

// turnResult is one turn's outcome, delivered from the goroutine that runs the turn.
type turnResult struct {
	result string
	err    error
}

// runTurn runs the turn's work and yields the mid-turn working status if the turn is still
// running after midTurnStatusDelay. It returns the turn's result and whether the caller may
// keep yielding.
//
// The work runs on its own goroutine so the delay can elapse while the turn runs: the mid-turn
// event rides alongside the turn instead of blocking it, which is what keeps the turn's
// duration a function of the backend's own work time rather than of the delay. The delay
// therefore never lengthens a turn; it only decides whether the turn is still running when it
// elapses.
//
// The goroutine is raw on purpose — a deliberate deviation from the
// `go-concurrency/no-raw-go-func` MUST rule of `go-concurrency-patterns.md` — because no
// `github.com/bborbe/run` strategy expresses this shape: `run.CancelOnFirstFinishWait` would
// cancel the survivor, but here the turn must survive the timer firing and keep waiting for the
// work. Its lifetime is bounded by the results channel (capacity 1, so the work goroutine never
// blocks on a consumer that has stopped) and by the deferred timer Stop, so neither the
// goroutine nor the timer outlives the turn — the same bounded-lifetime deviation
// `interactive/service.go`'s reaper carries.
//
// The iterator's stop signal is honoured the same way the existing yields honour it: a yield
// that returns false ends the turn's iteration here, and the timer stops with the deferred
// Stop. A backend panic is recovered on this goroutine rather than escaping it, because the SDK
// recovers only the producer and consumer goroutines it starts itself — an unrecovered panic
// here would take the process down instead of failing the turn.
func (e *a2aExecutor) runTurn(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
	sessionID string,
	prompt string,
	yield func(a2a.Event, error) bool,
) (turnResult, bool) {
	results := make(chan turnResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				results <- turnResult{
					err: errors.Errorf(ctx, "prompt panicked: %v\n%s", r, debug.Stack()),
				}
			}
		}()
		result, err := e.cache.Get(ctx, sessionID).Prompt(ctx, prompt)
		results <- turnResult{result: result, err: err}
	}()

	timer := time.NewTimer(midTurnStatusDelay)
	defer timer.Stop()

	for {
		select {
		case result := <-results:
			return result, true
		case <-ctx.Done():
			return turnResult{}, false
		case <-timer.C:
			if !yield(midTurnStatusEvent(execCtx), nil) {
				return turnResult{}, false
			}
		}
	}
}

// Execute runs one turn on the conversation the caller named and yields the events that
// describe it: the submitted task, a mid-turn working status, the agent's reply as an
// artifact, and the terminal status. A request that fails validation, or a turn that fails,
// yields an error or a failed status instead — the backend's error text is logged and never
// returned.
func (e *a2aExecutor) Execute(
	ctx context.Context,
	execCtx *a2asrv.ExecutorContext,
) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		ctx = claude.WithPermissionSource(ctx, claude.A2APermissionSource)
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

		result, ok := e.runTurn(ctx, execCtx, sessionID, prompt, yield)
		if !ok {
			return
		}
		if result.err != nil {
			glog.Warningf("a2a message failed: %v", result.err)
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, nil), nil)
			return
		}
		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result.result)), nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(
			execCtx,
			a2a.TaskStateCompleted,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(completionStatusText)),
		), nil)
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

// promptFromMessage concatenates the text of every part of an A2A message, in order, with
// no separator inserted between them: `["Hello", "world"]` yields `Helloworld`, exactly as
// the single part `"Helloworld"` would. That is deliberate — the parts are the caller's own
// content and the service does not invent punctuation between them, so a client that wants
// a boundary sends one inside a part. A non-text part contributes nothing, and a nil
// element contributes nothing. An empty result means the message carries no text and the
// request is refused before any session is built.
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
