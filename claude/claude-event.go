// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"encoding/json"
	"strconv"
)

// claudeEvent represents a single event in the Claude CLI stream-json output.
type claudeEvent struct {
	Type     string          `json:"type"`
	Result   string          `json:"result"`
	Message  claudeMsg       `json:"message"`
	Usage    json.RawMessage `json:"usage"`
	NumTurns json.Number     `json:"num_turns"`
}

// resultHolder safely extracts type and result from a JSON line without failing
// on schema-level errors (e.g. a json.Number field receiving a string).
type resultHolder struct {
	Type     string      `json:"type"`
	Result   string      `json:"result"`
	NumTurns json.Number `json:"num_turns"`
}

// sessionUsage is the token and turn summary captured from the Claude CLI's
// terminal result event. The zero value means no usage was reported and is a
// valid, non-error outcome.
type sessionUsage struct {
	inputTokens         int64
	outputTokens        int64
	cacheCreationTokens int64
	cacheReadTokens     int64
	numTurns            int64
}

// numberToInt64 converts a JSON number to int64, yielding 0 when the value is
// absent, non-integer, or otherwise unconvertible. Usage accounting is
// best-effort telemetry: a malformed count must never fail the run.
// It falls back to ParseFloat to handle decimal-formatted numbers like "100.0".
func numberToInt64(n json.Number) int64 {
	if n == "" {
		return 0
	}
	v, err := n.Int64()
	if err == nil {
		return v
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

type claudeMsg struct {
	Content []claudeContent `json:"content"`
}

type claudeContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
