// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

// ClaudeModel identifies which Claude model to use.
type ClaudeModel string

// String returns the model name.
func (c ClaudeModel) String() string { return string(c) }

const (
	SonnetClaudeModel ClaudeModel = "sonnet"
	OpusClaudeModel   ClaudeModel = "opus"
)
