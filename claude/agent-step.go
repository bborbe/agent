// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bborbe/errors"
	"github.com/golang/glog"

	agentlib "github.com/bborbe/agent"
)

// AgentStepConfig bundles everything an agent Step needs at construction time.
//
// The Runner is pre-configured with AllowedTools + Model via ClaudeRunnerConfig
// at construction; per-step Instructions + EnvContext are supplied here so a
// single Runner can serve multiple steps with different prompts.
type AgentStepConfig struct {
	// Name is the step name for logs.
	Name string

	// Runner is the Claude CLI invocation backend (pre-configured with
	// AllowedTools, Model, ClaudeConfigDir via ClaudeRunnerConfig).
	Runner ClaudeRunner

	// Instructions is the system prompt for this step.
	Instructions Instructions

	// EnvContext is forwarded to the Claude CLI tool-invocation environment.
	EnvContext map[string]string

	// OutputSection is the body section heading for the LLM's output
	// (e.g. "## Analysis", "## Review").
	OutputSection string

	// NextPhase is the phase to advance to on success. Empty means
	// in-place save (multi-step phase intermediate).
	NextPhase string
}

// NewAgentStep wraps a single Claude invocation as an agentlib.Step.
//
// Used by AI-heavy agents (trade-analysis, pr-reviewer style). The LLM
// reads the marshaled task content (frontmatter + body) and writes its
// output verbatim under the configured section heading.
//
// For boundary parsing (markdown → typed Go struct), use
// agentlib.NewParseStep with an AIParser implementation instead.
func NewAgentStep(cfg AgentStepConfig) agentlib.Step {
	return &agentStep{cfg: cfg}
}

type agentStep struct {
	cfg AgentStepConfig
}

// failureSectionHeading is the repo-wide failure-marker heading written by
// delivery/content-generator.go (and agent-task-executor's result publisher)
// on AgentStatusFailed / AgentStatusNeedsInput. Its presence means the prior
// run failed, so ShouldRun forces a re-run instead of treating the output
// section as completed work (spec 051).
const failureSectionHeading = "## Failure"

// Name implements agentlib.Step.
func (s *agentStep) Name() string { return s.cfg.Name }

// ShouldRun returns false only when the output section exists and represents a
// genuine success: a body that is not a needs_input/failed AgentResult, with no
// ## Failure section present.
//
// Single-step idempotency check: if the LLM already wrote its section in a
// prior Job that crashed before phase advance, skip the re-invocation.
// (For multi-step phases, decompose the work — don't rely on a single
// AgentStep to be partially-resumable.)
//
// A failure marker — a ## Failure section, or an output-section body that
// parses to a needs_input/failed AgentResult — forces a re-run: a failed run
// is not completed work, and re-dispatch must re-invoke claude (spec 051).
func (s *agentStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
	_, exists := md.FindSection(s.cfg.OutputSection)
	if !exists {
		return true, nil
	}
	if s.failureMarked(md) {
		return true, nil
	}
	return false, nil
}

// failureMarked reports whether the task carries a failure marker that must
// force a re-run despite an existing output section: a ## Failure section, or
// an output-section body that parses to a needs_input/failed AgentResult.
// Bodies that are not a failure marker (done JSON, unparseable prose, unknown
// status) are treated as a genuine success section (spec 051).
func (s *agentStep) failureMarked(md *agentlib.Markdown) bool {
	if _, exists := md.FindSection(failureSectionHeading); exists {
		return true
	}
	section, exists := md.FindSection(s.cfg.OutputSection)
	if !exists {
		return false
	}
	result, ok := parseAgentResultBody(section.Body)
	if !ok {
		return false
	}
	return result.Status == agentlib.AgentStatusNeedsInput ||
		result.Status == agentlib.AgentStatusFailed
}

// parseAgentResultBody extracts the last balanced JSON object from body and
// unmarshals it as an AgentResult. ok is false when no JSON object is present
// or unmarshal fails — callers treat an unparseable body as "not a failure
// marker" (best-effort parsing, spec 051). Unknown status values are not a
// failure marker and fall through to the success/skip path.
func parseAgentResultBody(body string) (AgentResult, bool) {
	blob, ok := extractLastJSONObject(body)
	if !ok {
		return AgentResult{}, false
	}
	var result AgentResult
	if err := json.Unmarshal([]byte(blob), &result); err != nil {
		return AgentResult{}, false
	}
	return result, true
}

// Run marshals the task, calls Claude with the step's prompt + tools, and
// writes the LLM's output under the configured section heading. On a
// needs_input/failed runner body the step returns that status WITHOUT writing
// the output section — the deliverer writes the ## Failure marker instead, so
// the section is never left looking like completed work (spec 051).
func (s *agentStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	taskContent, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "%s marshal task", s.cfg.Name)
	}

	prompt := BuildPrompt(s.cfg.Instructions.String(), s.cfg.EnvContext, taskContent)

	glog.Infof("%s: invoking claude runner (prompt=%d bytes)", s.cfg.Name, len(prompt))
	runStart := time.Now()
	result, runErr := s.cfg.Runner.Run(ctx, prompt)
	if runErr != nil {
		glog.Infof(
			"%s: claude runner failed after %s: %v",
			s.cfg.Name,
			time.Since(runStart),
			runErr,
		)
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: fmt.Sprintf("%s claude run failed: %v", s.cfg.Name, runErr),
		}, nil
	}
	glog.Infof(
		"%s: claude runner returned %d bytes in %s",
		s.cfg.Name,
		len(result.Result),
		time.Since(runStart),
	)

	// A needs_input/failed body is a failed run, not completed work — return
	// that status and let the deliverer write the ## Failure marker. Never
	// write a success-looking output section for a failed run: that section
	// would make ShouldRun skip every subsequent re-dispatch (spec 051).
	if parsed, ok := parseAgentResultBody(result.Result); ok &&
		(parsed.Status == agentlib.AgentStatusNeedsInput ||
			parsed.Status == agentlib.AgentStatusFailed) {
		msg := parsed.Message
		if msg == "" {
			msg = fmt.Sprintf("%s claude run returned status %s", s.cfg.Name, parsed.Status)
		}
		return &agentlib.Result{
			Status:  parsed.Status,
			Message: msg,
		}, nil
	}

	md.ReplaceSection(agentlib.Section{
		Heading: s.cfg.OutputSection,
		Body:    result.Result,
	})

	return &agentlib.Result{
		Status:    agentlib.AgentStatusDone,
		NextPhase: s.cfg.NextPhase,
	}, nil
}
