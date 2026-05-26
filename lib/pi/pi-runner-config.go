// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi

// PiRunnerConfig holds configuration for spawning the Pi CLI.
type PiRunnerConfig struct {
	// AgentDir is the working directory for the pi process. Pi's
	// context-file discovery walks AGENTS.md/CLAUDE.md from cwd toward /,
	// so place project guardrails as AGENTS.md inside this directory.
	// Pi's other config (settings, skills, sessions) is resolved via
	// $PI_CODING_AGENT_DIR or ~/.pi/agent/ and is independent of cwd.
	AgentDir string
	// AllowedTools is the comma-separated list of tool names to enable.
	AllowedTools string
	// Model selects the model (e.g. "MiniMax-M2.7-highspeed").
	Model string
	// Env holds extra KEY=VALUE entries appended to the subprocess environment.
	// Use for API keys, custom provider settings, etc.
	Env map[string]string
}
