// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"context"
	"strings"

	agentlib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../mocks/delivery-content-generator.go --fake-name AgentContentGenerator . ContentGenerator

// ContentGenerator produces a complete updated task markdown document from the original content and agent result.
// The returned string must be valid markdown with YAML frontmatter.
type ContentGenerator interface {
	Generate(
		ctx context.Context,
		originalContent string,
		result agentlib.AgentResultInfo,
	) (string, error)
}

// NewFallbackContentGenerator creates a ContentGenerator that uses deterministic string concatenation.
func NewFallbackContentGenerator() ContentGenerator {
	return &fallbackContentGenerator{}
}

type fallbackContentGenerator struct{}

func (g *fallbackContentGenerator) Generate(
	_ context.Context,
	originalContent string,
	result agentlib.AgentResultInfo,
) (string, error) {
	updated := applyStatusFrontmatter(originalContent, result.Status)
	if result.Status == agentlib.AgentStatusFailed {
		section := buildFailureSection(result)
		return ReplaceOrAppendSection(updated, "## Failure", section), nil
	}
	section := result.Output
	if section == "" {
		section = buildMinimalResultSection(result)
	}
	return ReplaceOrAppendSection(updated, "## Result", section), nil
}

// applyStatusFrontmatter updates status+phase frontmatter fields based on agent result status.
func applyStatusFrontmatter(content string, status agentlib.AgentStatus) string {
	switch status {
	case agentlib.AgentStatusDone:
		content = SetFrontmatterField(content, "status", "completed")
		content = SetFrontmatterField(content, "phase", "done")
	case agentlib.AgentStatusNeedsInput:
		// task-level failure: agent ran cleanly but task is impossible/underspecified.
		// Route straight to human_review — retrying a semantically-wrong task wastes compute.
		content = SetFrontmatterField(content, "status", "in_progress")
		content = SetFrontmatterField(content, "phase", "human_review")
	case agentlib.AgentStatusInProgress:
		// Step-level progress save: keep status: in_progress, preserve phase from incoming task.
		// Multi-step phase handlers use this to commit ## Plan / ## Result / etc. mid-phase
		// without triggering a phase transition.
		content = SetFrontmatterField(content, "status", "in_progress")
		// phase intentionally not modified — preserves the agent's current phase for in-place save
	default:
		// Agent returned status: failed (or unknown). Route to human_review immediately —
		// retry is the controller's job via trigger_count / max_triggers, not a phase loop.
		// The ## Failure body section carries the reason for the human reviewer.
		content = SetFrontmatterField(content, "status", "in_progress")
		content = SetFrontmatterField(content, "phase", "human_review")
	}
	return content
}

// buildFailureSection renders a `## Failure` block with a human-readable
// reason extracted from the agent's result. Used when the agent returns
// status: failed — symmetric with PublishFailure's K8s-crash failure path.
func buildFailureSection(result agentlib.AgentResultInfo) string {
	var b strings.Builder
	b.WriteString("## Failure\n\n")
	if result.Message != "" {
		b.WriteString("- **Reason:** ")
		b.WriteString(result.Message)
		b.WriteString("\n")
	} else {
		b.WriteString("- **Reason:** agent returned status: failed (no message provided)\n")
	}
	return b.String()
}

// buildMinimalResultSection renders a fallback ## Result block when agentlib.AgentResultInfo.Output is empty.
// Callers that supply a non-empty Output are trusted to provide the full section verbatim.
func buildMinimalResultSection(result agentlib.AgentResultInfo) string {
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

// NewSectionContentGenerator creates a ContentGenerator that writes its output under a
// parameterized markdown heading (e.g. "## Plan", "## Review"). On agentlib.AgentStatusFailed
// it writes a "## Failure" section instead, regardless of the configured heading —
// the failure-section convention is repo-wide, not phase-specific.
//
// Use this for phase-aware agents whose phases write distinct sections (planning → ## Plan,
// execution → ## Result, review → ## Review).
func NewSectionContentGenerator(heading string) ContentGenerator {
	return &sectionContentGenerator{heading: heading}
}

type sectionContentGenerator struct {
	heading string
}

func (g *sectionContentGenerator) Generate(
	_ context.Context,
	originalContent string,
	result agentlib.AgentResultInfo,
) (string, error) {
	updated := applyStatusFrontmatter(originalContent, result.Status)
	if result.Status == agentlib.AgentStatusFailed {
		section := buildFailureSection(result)
		return ReplaceOrAppendSection(updated, "## Failure", section), nil
	}
	section := result.Output
	if section == "" {
		section = buildMinimalResultSection(result)
	}
	return ReplaceOrAppendSection(updated, g.heading, section), nil
}
