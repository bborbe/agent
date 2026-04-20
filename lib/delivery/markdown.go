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

// HasSection reports whether content contains at least one line that
// matches heading at line-start. A match requires the line to equal
// heading exactly, or to start with heading followed by a space or tab.
// Substrings like "## Results" do NOT match "## Result".
func HasSection(content, heading string) bool {
	for _, line := range strings.Split(content, "\n") {
		if isSectionStart(line, heading) {
			return true
		}
	}
	return false
}

// AppendSection appends newSection to content, ensuring a single blank
// line separator and exactly one trailing newline in the result.
func AppendSection(content, newSection string) string {
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n\n" + strings.TrimRight(newSection, "\n") + "\n"
}

// ReplaceSection removes every section whose heading line matches
// heading (line-start match) and appends newSection once at the end.
// If no section matches, behaves identically to AppendSection.
func ReplaceSection(content, heading, newSection string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		if isSectionStart(line, heading) {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(line, "## ") {
			skipping = false
		}
		if !skipping {
			kept = append(kept, line)
		}
	}
	return AppendSection(strings.Join(kept, "\n"), newSection)
}

// ReplaceOrAppendSection replaces every section matching heading with
// newSection, or appends newSection if no matching section exists.
// The result always contains exactly one section with this heading.
func ReplaceOrAppendSection(content, heading, newSection string) string {
	if HasSection(content, heading) {
		return ReplaceSection(content, heading, newSection)
	}
	return AppendSection(content, newSection)
}

func isSectionStart(line, heading string) bool {
	if !strings.HasPrefix(line, heading) {
		return false
	}
	if len(line) == len(heading) {
		return true
	}
	c := line[len(heading)]
	return c == ' ' || c == '\t'
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
