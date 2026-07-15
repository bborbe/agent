// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package envparse provides simple parsers for KEY=VALUE-style CLI inputs.
package envparse

import "strings"

// KeyValuePairs parses a comma-separated KEY1=VALUE1,KEY2=VALUE2 string
// into a map. Empty input returns nil. Pairs without '=' are silently
// skipped. Whitespace around keys and values is trimmed.
func KeyValuePairs(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}
