// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

// ClaudeResult holds the parsed output from a Claude Code CLI session.
type ClaudeResult struct {
	Result string `json:"result"`
	// InputTokens is the count of fresh (non-cached) input tokens the session consumed.
	InputTokens int64 `json:"input_tokens,omitempty"`
	// OutputTokens is the count of output tokens the session produced.
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// CacheCreationTokens is the count of input tokens written into the prompt cache.
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// CacheReadTokens is the count of input tokens served from the prompt cache.
	CacheReadTokens int64 `json:"cache_read_input_tokens,omitempty"`
	// NumTurns is the number of conversation turns the session took. Zero when the
	// CLI reported no usage summary.
	NumTurns int64 `json:"num_turns,omitempty"`
}
