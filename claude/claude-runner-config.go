// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"time"
)

// ClaudeRunnerConfig holds configuration for spawning Claude Code CLI.
type ClaudeRunnerConfig struct {
	// RateLimitBackoffBase overrides the first backoff before a rate-limit
	// re-invoke; each subsequent re-invoke doubles it, and equal jitter is applied
	// on top. Zero means the 15s default. The result is clamped to 2m, so a larger
	// value is silently shortened — tests set it to milliseconds so the retry loop
	// runs without waiting on the wall clock.
	RateLimitBackoffBase time.Duration
	// ClaudeConfigDir overrides the Claude CLI config directory via CLAUDE_CONFIG_DIR.
	ClaudeConfigDir ClaudeConfigDir
	// AllowedTools is passed to claude --allowedTools to restrict tool access.
	AllowedTools AllowedTools
	// Model selects the Claude model (e.g. "sonnet", "opus").
	Model ClaudeModel
	// WorkingDirectory sets the CLI process working directory.
	WorkingDirectory AgentDir
	// RunAsUID, when non-zero, runs the Claude CLI subprocess as this numeric
	// user id instead of inheriting the parent's. Zero means inherit and leaves
	// cmd.SysProcAttr untouched; a negative value is rejected rather than treated
	// as "off", so a misconfiguration cannot silently disable the drop.
	//
	// The parent must be root AND hold CAP_SETUID/CAP_SETGID. Without them
	// cmd.Start() fails with EPERM, which Run returns wrapped as "start claude CLI"
	// (one-shot) or "start claude process" (session). A future capabilities.drop:
	// [ALL] on the container turns this into that EPERM — it is a deployment
	// precondition, not a regression in this package.
	//
	// The caller must make every directory the CLI writes to — CLAUDE_CONFIG_DIR,
	// the working directory, HOME, and any scratch dir — owned by this user id,
	// because this package only performs the privilege drop; it does not chown
	// anything. HOME matters as much as the config dir: the CLI writes
	// ~/.claude.json and ~/.cache there. Do not point CLAUDE_CONFIG_DIR at a
	// directory that a root-run `claude` also reads: this package's own transcript
	// scan reads it as root, and a directory the unprivileged session can write is
	// not a safe source of settings or hooks for a root process.
	//
	// The child's primary group is set to the same numeric value and its
	// supplementary groups are cleared, so it does not inherit the parent's groups.
	// Capabilities are dropped by the setuid transition itself.
	RunAsUID int
	// Env holds extra KEY=VALUE entries appended to the subprocess environment
	// AFTER the allowlist + CLAUDE_CONFIG_DIR layers (see buildSubprocessEnv in claude-runner.go). This is
	// the escape hatch for passing through secrets or vars the allowlist would
	// otherwise strip (e.g. GH_TOKEN for gh CLI auth). Values here cross the
	// trust boundary into the Claude CLI subprocess — only add what the agent
	// genuinely needs.
	Env map[string]string
}
