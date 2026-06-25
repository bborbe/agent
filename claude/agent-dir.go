// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import "context"

// AgentDir is the path to the agent directory containing .claude/ config.
type AgentDir string

// String returns the raw path string as configured.
func (a AgentDir) String() string { return string(a) }

// Resolve expands a leading "~/" to the user's home directory.
// See ClaudeConfigDir.Resolve for full semantics.
func (a AgentDir) Resolve(ctx context.Context) (string, error) {
	return expandTilde(ctx, string(a))
}
