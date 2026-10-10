// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

// noParseableReason is returned when every captured line is a stream-json
// event and none of them names a cause. Saying so beats emitting "": an empty
// reason reads as a defect in the reporter rather than as an unhelpful CLI.
const noParseableReason = "no parseable reason in CLI output"

// maxReasonBytes caps a PARSED reason. A result event can carry a whole model
// answer, so it is capped where it becomes a one-line message field. The raw
// fallback is deliberately NOT capped here — the tail ring buffer already
// bounds it, and re-capping would drop a diagnostic the parser did not
// recognize, which is the information this function exists to preserve.
const maxReasonBytes = 300

// failureEvent is the subset of a stream-json event needed to name a failure.
// One struct covers every shape because a line carries exactly one of them and
// an unrecognized event simply matches none — schema drift degrades to the raw
// fallback rather than to a crash.
type failureEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`

	// system/api_retry: the retry storm names its own cause and its cap.
	Error       string `json:"error"`
	ErrorStatus int    `json:"error_status"`
	Attempt     int    `json:"attempt"`
	MaxRetries  int    `json:"max_retries"`

	// Message is an object on assistant/user events and a plain string on a
	// CLI-level error event, so it stays raw and is interpreted on demand.
	Message json.RawMessage `json:"message"`
}

// FailureReason turns the bounded raw stream-json failure tail into a single
// operator-legible line naming the cause, so a task's `## Failure` entry reads
// as a reason rather than as a dump of `{"type":"assistant",...}` events.
//
// It recognizes, in precedence order:
//
//  1. a `result` event that marks itself an error (`is_error`), whose `result`
//     field is the CLI's own summary;
//  2. a `tool_result` content block with `is_error` — the shape a denied or
//     failed tool call produces, carrying the reason in its body;
//  3. a CLI-level `error` event, whose `message` is a plain string;
//  4. a `system`/`api_retry` event — a retry storm, where the LAST event is
//     kept because it carries the attempt count that was reached.
//
// When nothing structured matches, the fallback distinguishes two cases, and
// the distinction is the whole point:
//
//   - the tail holds a line that is not a stream-json event (plain text the
//     CLI wrote, e.g. `auth-failure: 401 Invalid authentication credentials`):
//     the RAW tail is returned, so an unrecognized diagnostic still reaches
//     the operator. Parsing must never swallow a cause it did not anticipate.
//   - every line is a recognized event and none names a cause (the
//     `thinking_tokens` progress flood is the real case): noParseableReason is
//     returned, because raw-joining here is exactly the defect this replaces.
//
// The returned string is NOT redacted: redaction is the caller's policy, since
// the tail may embed a credential only the caller knows how to scrub.
func FailureReason(tail []string) string {
	var retry string
	sawNonEvent := false

	for _, line := range tail {
		var event failureEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Type == "" {
			sawNonEvent = true
			continue
		}
		reason := event.failureReason()
		if reason == "" {
			continue
		}
		if event.Type == "system" && event.Subtype == "api_retry" {
			// Keep the LAST retry: it carries the attempt count reached.
			retry = reason
			continue
		}
		return reason
	}

	if retry != "" {
		return retry
	}
	if sawNonEvent {
		return strings.Join(tail, tailJoiner)
	}
	return noParseableReason
}

// failureReason returns the one-line cause this event names, or "" when the
// event names none. Precedence follows FailureReason's documented order.
func (e failureEvent) failureReason() string {
	if e.Type == "result" && e.IsError && e.Result != "" {
		return collapseReason(e.Result)
	}
	if e.Type == "error" {
		if text := collapseReason(e.messageText()); text != "" {
			return text
		}
	}
	for _, c := range e.messageContent() {
		if c.Type != "tool_result" || !c.IsError {
			continue
		}
		if text := collapseReason(c.toolResultText()); text != "" {
			return text
		}
	}
	if e.Type == "system" && e.Subtype == "api_retry" {
		return collapseReason(fmt.Sprintf(
			"api_retry: %s (HTTP %d) — attempt %d/%d",
			e.Error,
			e.ErrorStatus,
			e.Attempt,
			e.MaxRetries,
		))
	}
	return ""
}

// messageText returns the message when it is a plain JSON string, which is the
// shape a CLI-level error event uses. Returns "" when it is an object.
func (e failureEvent) messageText() string {
	if len(e.Message) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(e.Message, &text); err != nil {
		return ""
	}
	return text
}

// messageContent returns the message's content blocks, which is the shape an
// assistant/user event uses. Returns nil when message is a plain string.
func (e failureEvent) messageContent() []failureContent {
	if len(e.Message) == 0 {
		return nil
	}
	var message struct {
		Content []failureContent `json:"content"`
	}
	if err := json.Unmarshal(e.Message, &message); err != nil {
		return nil
	}
	return message.Content
}

// failureContent is one message.content item. A tool_result the CLI rejected
// carries its reason in Content (a JSON string or a block array) or in Text.
type failureContent struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

// toolResultText returns the text of a rejected tool_result, whose body is
// either a JSON string or an array of blocks. Text is the fallback for a shape
// that carries the reason directly.
func (c failureContent) toolResultText() string {
	if len(c.Content) > 0 {
		// A plain JSON string is the shape a CLI rejection uses; take it
		// verbatim rather than dropping the reason.
		var content string
		if err := json.Unmarshal(c.Content, &content); err == nil && content != "" {
			return content
		}

		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(c.Content, &blocks); err == nil {
			for _, b := range blocks {
				if b.Text != "" {
					return b.Text
				}
			}
		}
	}
	return c.Text
}

// collapseReason flattens a parsed reason to one line and caps its length, so a
// multi-line model answer still fits a message field. Returns "" for input that
// is empty once trimmed, so callers can treat "" as "this event names nothing".
func collapseReason(s string) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if collapsed == "" {
		return ""
	}
	if len(collapsed) > maxReasonBytes {
		return collapsed[:maxReasonBytes] + "…"
	}
	return collapsed
}
