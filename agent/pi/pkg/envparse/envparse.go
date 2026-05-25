// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package envparse provides simple parsers for KEY=VALUE-style CLI inputs.
package envparse

import "strings"

// KeyValuePairs parses "KEY=VALUE,KEY2=VALUE2" into a map.
// Whitespace around each pair is trimmed. Pairs without "=" are skipped.
// Returns nil for empty input.
func KeyValuePairs(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	result := map[string]string{}
	for p := range strings.SplitSeq(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}
