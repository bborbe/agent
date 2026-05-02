// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import "context"

// ClaudeConfigDir is the path to the Claude Code configuration directory (~/.claude).
type ClaudeConfigDir string

// String returns the raw path string as configured.
// Use Resolve when the path will cross into a subprocess or filesystem call —
// String preserves the literal "~/" prefix that environment variables and
// child processes generally do not expand.
func (c ClaudeConfigDir) String() string { return string(c) }

// Resolve expands a leading "~/" to the user's home directory. Empty input
// returns empty string. Absolute paths and paths without a tilde prefix are
// returned unchanged. Use at the trust boundary — when emitting the path
// into a subprocess env var, opening a file, or constructing a filesystem
// operation — so configuration like CLAUDE_CONFIG_DIR=~/.claude works on
// every consumer regardless of whether the consumer expands tildes itself.
func (c ClaudeConfigDir) Resolve(ctx context.Context) (string, error) {
	return expandTilde(ctx, string(c))
}
