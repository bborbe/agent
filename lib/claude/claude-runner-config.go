// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

// ClaudeRunnerConfig holds configuration for spawning Claude Code CLI.
type ClaudeRunnerConfig struct {
	ClaudeConfigDir  ClaudeConfigDir
	AllowedTools     AllowedTools
	Model            ClaudeModel
	WorkingDirectory AgentDir
	Env              map[string]string
}
