// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import "strings"

// AllowedTools is a list of tool names permitted in headless Claude sessions.
type AllowedTools []string

// ParseAllowedTools parses a comma-separated string into AllowedTools.
func ParseAllowedTools(s string) AllowedTools {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// String returns the comma-separated tool list for the CLI --allowedTools flag.
func (a AllowedTools) String() string {
	return strings.Join(a, ",")
}
