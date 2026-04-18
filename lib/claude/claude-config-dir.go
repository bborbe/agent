// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

// ClaudeConfigDir is the path to the Claude Code configuration directory (~/.claude).
type ClaudeConfigDir string

// String returns the path as a string.
func (c ClaudeConfigDir) String() string { return string(c) }
