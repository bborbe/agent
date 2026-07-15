// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"bufio"
	"context"
	"strings"

	"github.com/bborbe/errors"
	"gopkg.in/yaml.v3"
)

// Section is one heading-bounded block in a parsed markdown document.
//
// Heading is the exact heading line including '#' characters, e.g. "## Plan".
// Body is the content between this heading and the next at the same or
// higher level (no trailing newline).
//
// Section is the parsed structural unit. The CQRS BodySection (in
// lib/command/task) is a different concept: it carries the full
// serialized section text including the heading line, used as a partial
// frontmatter+body update payload.
type Section struct {
	Heading string
	Body    string
}

// Markdown is a parsed task document: frontmatter + preamble (text before
// the first section) + ordered list of sections.
//
// Steps mutate Markdown in place via the methods below. The framework
// re-serializes Markdown via Marshal after each step's Run and publishes
// the new content via the deliverer.
type Markdown struct {
	Frontmatter TaskFrontmatter
	Preamble    string
	Sections    []Section
}

// ParseMarkdown parses raw markdown into a Markdown document.
//
// Best-effort parsing: invalid YAML returns an empty Frontmatter without
// error. Sections are split at every "# " or "## " heading; "### " and
// deeper sub-headings are part of the parent section's Body.
func ParseMarkdown(_ context.Context, content string) (*Markdown, error) {
	fmStr, body := splitMarkdownFrontmatter(content)
	fm := parseMarkdownFrontmatter(fmStr)
	preamble, sections := splitMarkdownSections(body)
	return &Markdown{
		Frontmatter: fm,
		Preamble:    preamble,
		Sections:    sections,
	}, nil
}

// FindSection returns a pointer to the first section matching heading,
// and a bool indicating presence. Mutating the returned section's fields
// updates the Markdown in-place.
func (m *Markdown) FindSection(heading string) (*Section, bool) {
	for i := range m.Sections {
		if m.Sections[i].Heading == heading {
			return &m.Sections[i], true
		}
	}
	return nil, false
}

// AddSection appends a section to the end of the section list.
//
// Use ReplaceSection if a section with the same heading might already
// exist; AddSection does not deduplicate.
func (m *Markdown) AddSection(section Section) {
	m.Sections = append(m.Sections, section)
}

// ReplaceSection replaces the existing section with the same Heading,
// or appends if no match exists. Idempotent for "save my output" steps.
func (m *Markdown) ReplaceSection(section Section) {
	for i := range m.Sections {
		if m.Sections[i].Heading == section.Heading {
			m.Sections[i] = section
			return
		}
	}
	m.Sections = append(m.Sections, section)
}

// InsertSection inserts a section at the given position. Out-of-range
// positions clamp to [0, len(Sections)].
func (m *Markdown) InsertSection(pos int, section Section) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(m.Sections) {
		pos = len(m.Sections)
	}
	m.Sections = append(m.Sections[:pos], append([]Section{section}, m.Sections[pos:]...)...)
}

// Marshal serializes the Markdown back to a markdown string.
//
// Output: "---\n<yaml>\n---\n<preamble><section><section>..."
// Each section is rendered as "<heading>\n\n<body>\n" if body is non-empty,
// or "<heading>\n" if body is empty.
func (m *Markdown) Marshal(ctx context.Context) (string, error) {
	var b strings.Builder

	if len(m.Frontmatter) > 0 {
		fmBytes, err := yaml.Marshal(map[string]any(m.Frontmatter))
		if err != nil {
			return "", errors.Wrapf(ctx, err, "marshal frontmatter")
		}
		b.WriteString("---\n")
		b.Write(fmBytes)
		b.WriteString("---\n")
	}

	if m.Preamble != "" {
		b.WriteString(m.Preamble)
		if !strings.HasSuffix(m.Preamble, "\n") {
			b.WriteString("\n")
		}
	}

	for _, s := range m.Sections {
		b.WriteString(s.Heading)
		b.WriteString("\n")
		if s.Body != "" {
			b.WriteString("\n")
			b.WriteString(s.Body)
			if !strings.HasSuffix(s.Body, "\n") {
				b.WriteString("\n")
			}
		}
	}

	return b.String(), nil
}

// splitMarkdownFrontmatter separates `---\n<yaml>\n---\n<body>` into
// (yaml, body). If no frontmatter is present, returns ("", content).
func splitMarkdownFrontmatter(content string) (string, string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", content
	}
	return rest[:end], rest[end+5:]
}

// parseMarkdownFrontmatter parses YAML frontmatter; best-effort, returns
// an empty map on parse error.
func parseMarkdownFrontmatter(fm string) TaskFrontmatter {
	out := TaskFrontmatter{}
	if fm == "" {
		return out
	}
	if err := yaml.Unmarshal([]byte(fm), &out); err != nil {
		return TaskFrontmatter{}
	}
	return out
}

// splitMarkdownSections splits body content into preamble + sections at
// '# '/'## ' heading boundaries (level 1 or 2 headings start a new
// section; level 3+ stays inside the parent section).
func splitMarkdownSections(body string) (string, []Section) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var (
		preambleLines []string
		sections      []Section
		currentHead   string
		currentBody   []string
		inSection     bool
	)

	flush := func() {
		if !inSection {
			return
		}
		body := strings.TrimSuffix(strings.Join(currentBody, "\n"), "\n")
		sections = append(sections, Section{Heading: currentHead, Body: body})
		currentHead = ""
		currentBody = currentBody[:0]
	}

	for scanner.Scan() {
		line := scanner.Text()
		if isMarkdownSectionHeading(line) {
			flush()
			inSection = true
			currentHead = line
			continue
		}
		if !inSection {
			preambleLines = append(preambleLines, line)
			continue
		}
		currentBody = append(currentBody, line)
	}
	flush()

	preamble := strings.TrimSuffix(strings.Join(preambleLines, "\n"), "\n")
	return preamble, sections
}

// isMarkdownSectionHeading returns true for "# " or "## " level headings.
// "### " and deeper stay inside their parent section.
func isMarkdownSectionHeading(line string) bool {
	if !strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
		return false
	}
	count := 0
	for _, c := range line {
		if c == '#' {
			count++
			continue
		}
		break
	}
	return (count == 1 || count == 2) && len(line) > count && line[count] == ' '
}
