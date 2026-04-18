// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"sort"
	"strings"
)

// BuildPrompt combines instructions with environment context and task content.
func BuildPrompt(
	instructions string,
	envContext map[string]string,
	taskContent string,
) string {
	var sb strings.Builder
	sb.WriteString(instructions)

	if len(envContext) > 0 {
		sb.WriteString("\n\n## Environment\n\n")
		keys := make([]string, 0, len(envContext))
		for k := range envContext {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(envContext[k])
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n\n## Task\n\n")
	sb.WriteString(taskContent)
	return sb.String()
}
