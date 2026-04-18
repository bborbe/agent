// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

// AgentDir is the path to the agent directory containing .claude/ config.
type AgentDir string

// String returns the path as a string.
func (a AgentDir) String() string { return string(a) }
