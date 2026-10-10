// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/claude"
)

// The fixtures below are captured verbatim from real failed runs (task files
// under private-agent/tasks, 2026-10-10). A hand-written fixture shaped the
// way the author imagines the CLI emits is exactly what let the sibling
// stream-json parser defect ship — the parser agreed with itself and with
// nothing the CLI produced. Do not "tidy" these into invented events.
var _ = Describe("FailureReason", func() {
	It("returns the retry storm's cause, keeping the last attempt", func() {
		tail := []string{
			`{"type":"system","subtype":"api_retry","attempt":8,"max_retries":10,"retry_delay_ms":35438,"error_status":529,"error":"overloaded","session_id":"f93ad82c","uuid":"048eb306"}`,
			`{"type":"system","subtype":"api_retry","attempt":9,"max_retries":10,"retry_delay_ms":38419,"error_status":529,"error":"overloaded","session_id":"f93ad82c","uuid":"6ab9376d"}`,
			`{"type":"system","subtype":"api_retry","attempt":10,"max_retries":10,"retry_delay_ms":36809,"error_status":529,"error":"overloaded","session_id":"f93ad82c","uuid":"078fe07f"}`,
		}

		Expect(
			claude.FailureReason(tail),
		).To(Equal("api_retry: overloaded (HTTP 529) — attempt 10/10"))
	})

	It("returns a rejected tool_result's reason", func() {
		tail := []string{
			`{"type":"assistant","message":{"id":"066efb9b","type":"message","role":"assistant","content":[{"type":"tool_use","id":"call_function_9cckh6psph7q_1","name":"Bash","input":{"command":"sg --version 2>&1; echo \"DONE\""}}],"model":"MiniMax-M3"}}`,
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"This Bash command contains multiple operations. The following part requires approval: sg --version","is_error":true,"tool_use_id":"call_function_9cckh6psph7q_1"}]},"session_id":"f54333ec","uuid":"a4f4ab5a"}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal(
			"This Bash command contains multiple operations. The following part requires approval: sg --version",
		))
	})

	It("returns a result event's summary when the event marks itself an error", func() {
		tail := []string{
			`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Credit balance is too low","session_id":"abc"}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal("Credit balance is too low"))
	})

	It("prefers a result event over a preceding retry storm", func() {
		tail := []string{
			`{"type":"system","subtype":"api_retry","attempt":10,"max_retries":10,"error_status":529,"error":"overloaded"}`,
			`{"type":"result","is_error":true,"result":"overloaded — giving up after 10 attempts"}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal("overloaded — giving up after 10 attempts"))
	})

	It("degrades explicitly when the tail carries only progress events", func() {
		// The real thinking_tokens flood: five events, no cause among them.
		tail := []string{
			`{"type":"system","subtype":"thinking_tokens","estimated_tokens":104,"estimated_tokens_delta":57,"uuid":"9517a053"}`,
			`{"type":"system","subtype":"thinking_tokens","estimated_tokens":154,"estimated_tokens_delta":50,"uuid":"ae2d7ec3"}`,
			`{"type":"system","subtype":"thinking_tokens","estimated_tokens":202,"estimated_tokens_delta":48,"uuid":"335e1c03"}`,
			`{"type":"system","subtype":"thinking_tokens","estimated_tokens":237,"estimated_tokens_delta":35,"uuid":"47662e0f"}`,
			`{"type":"system","subtype":"thinking_tokens","estimated_tokens":277,"estimated_tokens_delta":40,"uuid":"a9830d78"}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal("no parseable reason in CLI output"))
	})

	It("degrades explicitly for an empty tail", func() {
		Expect(claude.FailureReason(nil)).To(Equal("no parseable reason in CLI output"))
	})

	It("falls back to the raw tail when a line is not a stream-json event", func() {
		// Parsing must never swallow a diagnostic it did not anticipate: a
		// plain-text line means the tail is not the event protocol, so the
		// operator gets the text rather than a placeholder.
		tail := []string{
			`{"type":"system","subtype":"some_future_event","payload":{"nested":true}}`,
			`auth-failure: 401 Invalid authentication credentials`,
		}

		Expect(claude.FailureReason(tail)).To(Equal(
			`{"type":"system","subtype":"some_future_event","payload":{"nested":true}} | auth-failure: 401 Invalid authentication credentials`,
		))
	})

	It("returns the placeholder when every line is an event and none names a cause", func() {
		tail := []string{
			`{"type":"system","subtype":"some_future_event","payload":{"nested":true}}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"partial answer"}]}}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal("no parseable reason in CLI output"))
	})

	It("never crashes on malformed input", func() {
		Expect(claude.FailureReason([]string{`{not valid json`})).To(Equal(`{not valid json`))
	})

	It("collapses a multi-line reason to one line and caps its length", func() {
		long := ""
		for i := 0; i < 400; i++ {
			long += "x"
		}
		tail := []string{
			`{"type":"result","is_error":true,"result":"first line\nsecond   line\tthird"}`,
		}
		Expect(claude.FailureReason(tail)).To(Equal("first line second line third"))

		tail = []string{`{"type":"result","is_error":true,"result":"` + long + `"}`}
		reason := claude.FailureReason(tail)
		Expect(len(reason)).To(BeNumerically("<=", 304))
		Expect(reason).To(HaveSuffix("…"))
	})

	It("reads a tool_result whose content is a block array", func() {
		tail := []string{
			`{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":[{"type":"text","text":"command not found: sg"}]}]}}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal("command not found: sg"))
	})

	It("ignores a tool_result that is not an error", func() {
		tail := []string{
			`{"type":"user","message":{"content":[{"type":"tool_result","is_error":false,"content":"all good"}]}}`,
		}

		Expect(claude.FailureReason(tail)).To(Equal("no parseable reason in CLI output"))
	})
})
