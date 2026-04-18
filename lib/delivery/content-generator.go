// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"context"
	"strings"
)

//counterfeiter:generate -o ../mocks/delivery-content-generator.go --fake-name AgentContentGenerator . ContentGenerator

// ContentGenerator produces a complete updated task markdown document from the original content and agent result.
// The returned string must be valid markdown with YAML frontmatter.
type ContentGenerator interface {
	Generate(ctx context.Context, originalContent string, result AgentResultInfo) (string, error)
}

// NewFallbackContentGenerator creates a ContentGenerator that uses deterministic string concatenation.
func NewFallbackContentGenerator() ContentGenerator {
	return &fallbackContentGenerator{}
}

type fallbackContentGenerator struct{}

func (g *fallbackContentGenerator) Generate(
	_ context.Context,
	originalContent string,
	result AgentResultInfo,
) (string, error) {
	updated := originalContent

	switch result.Status {
	case AgentStatusDone:
		updated = SetFrontmatterField(updated, "status", "completed")
		updated = SetFrontmatterField(updated, "phase", "done")
	default: // failed, needs_input, and any other status
		updated = SetFrontmatterField(updated, "status", "in_progress")
		updated = SetFrontmatterField(updated, "phase", "ai_review")
	}

	var section strings.Builder
	section.WriteString("## Result\n\n")
	if result.Output != "" {
		section.WriteString(result.Output)
		section.WriteString("\n")
	}
	if result.Message != "" {
		section.WriteString("**Message:** ")
		section.WriteString(result.Message)
		section.WriteString("\n")
	}

	updated = ReplaceOrAppendSection(updated, "## Result", section.String())
	return updated, nil
}
