// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import "encoding/json"

// claudeEvent represents a single event in the Claude CLI stream-json output.
type claudeEvent struct {
	Type    string    `json:"type"`
	Result  string    `json:"result"`
	Message claudeMsg `json:"message"`
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
