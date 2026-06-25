// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery

import (
	"context"
	"strings"

	agentlib "github.com/bborbe/agent"
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
		// Clear assignee so the task surfaces in the operator inbox; preserve phase from
		// existing content — phase: human_review is reserved for Result.NextPhase handoffs.
		content = SetFrontmatterField(content, "status", "in_progress")
		content = SetFrontmatterField(content, "assignee", "")
		// phase is preserved from existing content — do NOT set to human_review
	case agentlib.AgentStatusInProgress:
		// Step-level progress save: keep status: in_progress, preserve phase from incoming task.
		// Multi-step phase handlers use this to commit ## Plan / ## Result / etc. mid-phase
		// without triggering a phase transition.
		content = SetFrontmatterField(content, "status", "in_progress")
		// phase intentionally not modified — preserves the agent's current phase for in-place save
	default:
		// Agent returned status: failed (or unknown). Clear assignee so the task
		// surfaces in the operator inbox; preserve phase from existing content. Retry
		// is the controller's job via trigger_count / max_triggers, not a phase loop.
		// The ## Failure body section carries the reason for the human reviewer.
		content = SetFrontmatterField(content, "status", "in_progress")
		content = SetFrontmatterField(content, "assignee", "")
		// phase is preserved from existing content — do NOT set to human_review
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
		b.WriteString("- **Reason:**\n\n")
		b.WriteString("```\n")
		b.WriteString(result.Message)
		b.WriteString("\n```\n")
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

// NewPassthroughContentGenerator creates a ContentGenerator that returns
// result.Output verbatim (with status/phase frontmatter applied on top).
//
// Used by the new agent framework (lib.NewAgent / lib.StepRunner): the
// step mutates a parsed Markdown via task.AddSection / ReplaceSection and
// the runner re-serializes the full task into result.Output. The deliverer
// must NOT splice the output into a "## Result" section — it must publish
// the agent-produced content directly.
//
// Status/phase frontmatter is still applied here so file delivery sets
// status: completed / phase: done on success without each agent having to
// mutate the frontmatter map manually. The Kafka deliverer overrides
// status/phase again after this generator runs (same end state).
//
// On AgentStatusFailed or AgentStatusNeedsInput, the passthrough generator
// splices a ## Failure section into result.Output so operators always see the
// failure reason — without this, early-step failures (where Output is empty)
// leave a body-less task. Mirrors fallback + section generators.
func NewPassthroughContentGenerator() ContentGenerator {
	return &passthroughContentGenerator{}
}

type passthroughContentGenerator struct{}

func (g *passthroughContentGenerator) Generate(
	_ context.Context,
	_ string,
	result agentlib.AgentResultInfo,
) (string, error) {
	updated := applyStatusFrontmatter(result.Output, result.Status)
	if result.Status == agentlib.AgentStatusFailed ||
		result.Status == agentlib.AgentStatusNeedsInput {
		// result.Output is unreliable on early-step failures — agents return
		// status=failed/needs_input WITHOUT having written anything to Output.
		// Surface result.Message via the shared ## Failure section so operators
		// can diagnose without log archaeology. Mirrors fallback + section generators.
		return ReplaceOrAppendSection(updated, "## Failure", buildFailureSection(result)), nil
	}
	return updated, nil
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
