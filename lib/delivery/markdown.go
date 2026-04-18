// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetFrontmatterField updates or adds a field in YAML frontmatter.
// Returns content unchanged if no frontmatter delimiters are found.
func SetFrontmatterField(content, key, value string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}

	end := strings.Index(content[3:], "---")
	if end == -1 {
		return content
	}
	end += 3 // offset for initial "---"

	frontmatter := content[3:end]
	rest := content[end+3:]

	if strings.Contains(frontmatter, key+":") {
		lines := strings.Split(frontmatter, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key+":") {
				lines[i] = key + ": " + value
			}
		}
		frontmatter = strings.Join(lines, "\n")
	} else {
		frontmatter = "\n" + key + ": " + value + frontmatter
	}

	return "---" + frontmatter + "---" + rest
}

// ReplaceOrAppendSection replaces an existing ## section or appends it at the end.
func ReplaceOrAppendSection(content, heading, newSection string) string {
	idx := strings.Index(content, heading)
	if idx == -1 {
		return strings.TrimRight(content, "\n") + "\n\n" + newSection
	}

	// Find the end of this section (next ## heading or EOF).
	after := content[idx+len(heading):]
	nextHeading := strings.Index(after, "\n## ")
	if nextHeading == -1 {
		// Last section — replace everything from heading to end.
		return strings.TrimRight(content[:idx], "\n") + "\n\n" + newSection
	}
	// Replace up to (but not including) the next heading.
	return strings.TrimRight(
		content[:idx],
		"\n",
	) + "\n\n" + newSection + "\n" + strings.TrimLeft(
		after[nextHeading+1:],
		"\n",
	)
}

// ParseMarkdownFrontmatter splits a markdown document with YAML frontmatter into
// a string map and the body. Returns empty map and full content if no frontmatter.
// Values are converted to strings: arrays become fmt representation, nested objects become fmt representation.
func ParseMarkdownFrontmatter(content string) (map[string]string, string) {
	if !strings.HasPrefix(content, "---") {
		return map[string]string{}, content
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return map[string]string{}, content
	}
	fmRaw := rest[:end]
	body := strings.TrimLeft(rest[end+4:], "\n")

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(fmRaw), &parsed); err != nil {
		return map[string]string{}, content
	}

	fm := make(map[string]string, len(parsed))
	for k, v := range parsed {
		switch val := v.(type) {
		case string:
			fm[k] = val
		case nil:
			// skip nil values
		default:
			fm[k] = fmt.Sprintf("%v", val)
		}
	}
	return fm, body
}

// IsValidMarkdownWithFrontmatter checks that the string has valid YAML frontmatter delimiters.
// The content must start with "---" followed by a newline and have a closing "---" on its own line.
func IsValidMarkdownWithFrontmatter(content string) bool {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return false
	}
	rest := content[3:] // keep the first newline so "\n---" matches
	return strings.Contains(rest, "\n---")
}

// StripMarkdownCodeFences removes surrounding ``` code fences from a string if present.
func StripMarkdownCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		end := strings.Index(s, "\n")
		if end == -1 {
			return s
		}
		s = s[end+1:]
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}
