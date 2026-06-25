// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package envparse

import "strings"

// sensitiveKeyMarkers is the case-insensitive substring set used to classify
// an env key as sensitive. The list is intentionally small and conservative;
// the cost of a false positive is "operator sees ***" and the cost of a
// false negative is "secret in Loki", so we err on the side of redacting.
var sensitiveKeyMarkers = []string{
	"TOKEN",
	"SECRET",
	"PASSWORD",
	"PASSWD",
	"CREDENTIAL",
	"API_KEY",
	"PRIVATE_KEY",
	"ACCESS_KEY",
}

// IsSensitiveKey reports whether key looks like it carries a secret value.
// Matching is case-insensitive substring against a fixed marker list.
func IsSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range sensitiveKeyMarkers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// RedactForLog returns a copy of env entries (each in "KEY=VALUE" form, the
// same shape exec.Cmd.Env uses) where values for sensitive keys are replaced
// with the literal "***". Keys remain visible so operators can confirm which
// vars are passed without exposing the values. Entries without '=' pass
// through unchanged. The input slice is not mutated.
func RedactForLog(env []string) []string {
	if env == nil {
		return nil
	}
	out := make([]string, len(env))
	for i, entry := range env {
		idx := strings.IndexByte(entry, '=')
		if idx < 0 {
			out[i] = entry
			continue
		}
		key := entry[:idx]
		if IsSensitiveKey(key) {
			out[i] = key + "=***"
			continue
		}
		out[i] = entry
	}
	return out
}
