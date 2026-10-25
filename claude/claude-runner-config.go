// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"time"

	libtime "github.com/bborbe/time"
)

// ClaudeRunnerConfig holds configuration for spawning Claude Code CLI.
type ClaudeRunnerConfig struct {
	// CurrentDateTime is the clock the rate-limit retry loop reads when deciding
	// whether the context deadline still leaves room for a backoff. Nil means the
	// real clock; tests inject a fixed one so that decision is deterministic.
	CurrentDateTime libtime.CurrentDateTimeGetter
	// RateLimitBackoffBase overrides the first backoff before a rate-limit
	// re-invoke; each subsequent re-invoke doubles it. Zero means the 15s
	// default. Tests set it to milliseconds so the retry loop runs without
	// waiting on the wall clock.
	RateLimitBackoffBase time.Duration
	// ClaudeConfigDir overrides the Claude CLI config directory via CLAUDE_CONFIG_DIR.
	ClaudeConfigDir ClaudeConfigDir
	// AllowedTools is passed to claude --allowedTools to restrict tool access.
	AllowedTools AllowedTools
	// Model selects the Claude model (e.g. "sonnet", "opus").
	Model ClaudeModel
	// WorkingDirectory sets the CLI process working directory.
	WorkingDirectory AgentDir
	// Env holds extra KEY=VALUE entries appended to the subprocess environment
	// AFTER the allowlist + CLAUDE_CONFIG_DIR layers (see buildSubprocessEnv in claude-runner.go). This is
	// the escape hatch for passing through secrets or vars the allowlist would
	// otherwise strip (e.g. GH_TOKEN for gh CLI auth). Values here cross the
	// trust boundary into the Claude CLI subprocess — only add what the agent
	// genuinely needs.
	Env map[string]string
}
