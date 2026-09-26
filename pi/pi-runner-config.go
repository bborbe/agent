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
	// PersistSession keeps pi's session storage instead of discarding it, by
	// omitting the default --no-session flag.
	//
	// Default false, and it must stay false for a task-routed (job) agent: each
	// run of such an agent is an unrelated task, and a persisted session would
	// let a later run resume the previous task's conversation from the shared
	// ~/.pi/agent/ volume. Set it only for a long-running identity agent, where
	// continuity across prompts is the point.
	//
	// Persisting is necessary but **not sufficient** for continuity. `--no-session`
	// governs whether pi *writes* the transcript; reading it back is a different
	// flag. Setting this alone yields an agent that saves every conversation and
	// remembers none of them — which is exactly what happened: a two-prompt proof
	// returned the first answer and then "no token was previously requested".
	PersistSession bool

	// SessionID, when set, passes pi's --session-id: one stable identity whose
	// transcript is created on first use and continued on every later run.
	//
	// This is the flag that makes continuity real — see PersistSession, which only
	// stops pi discarding the transcript. It is `--session-id` rather than
	// `--continue` because it **creates the session when it is missing**, so the
	// first prompt of a brand-new agent behaves exactly like the thousandth;
	// `--continue` would be resuming nothing on that first run.
	//
	// Leave empty for a task-routed agent, where each run is an unrelated task.
	SessionID string
}
