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
	updated := applyStatusFrontmatter(originalContent, result.Status)
	section := result.Output
	if section == "" {
		section = buildMinimalResultSection(result)
	}
	return ReplaceOrAppendSection(updated, "## Result", section), nil
}

// applyStatusFrontmatter updates status+phase frontmatter fields based on agent result status.
func applyStatusFrontmatter(content string, status AgentStatus) string {
	switch status {
	case AgentStatusDone:
		content = SetFrontmatterField(content, "status", "completed")
		content = SetFrontmatterField(content, "phase", "done")
	case AgentStatusNeedsInput:
		// task-level failure: agent ran cleanly but task is impossible/underspecified.
		// Route straight to human_review — retrying a semantically-wrong task wastes compute.
		content = SetFrontmatterField(content, "status", "in_progress")
		content = SetFrontmatterField(content, "phase", "human_review")
	default: // failed and any other status — infra failure, eligible for retry
		content = SetFrontmatterField(content, "status", "in_progress")
		content = SetFrontmatterField(content, "phase", "ai_review")
	}
	return content
}

// buildMinimalResultSection renders a fallback ## Result block when AgentResultInfo.Output is empty.
// Callers that supply a non-empty Output are trusted to provide the full section verbatim.
func buildMinimalResultSection(result AgentResultInfo) string {
	var b strings.Builder
	b.WriteString("## Result\n\n")
	b.WriteString("**Status:** ")
	b.WriteString(string(result.Status))
	b.WriteString("\n")
	if result.Message != "" {
		b.WriteString("**Message:** ")
		b.WriteString(result.Message)
		b.WriteString("\n")
	}
	return b.String()
}
