// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"fmt"

	"github.com/bborbe/errors"

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

// Name implements agentlib.Step.
func (s *agentStep) Name() string { return s.cfg.Name }

// ShouldRun returns false if the output section already exists.
//
// Single-step idempotency check: if the LLM already wrote its section in
// a prior Job that crashed before phase advance, skip the re-invocation.
// (For multi-step phases, decompose the work — don't rely on a single
// AgentStep to be partially-resumable.)
func (s *agentStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
	_, exists := md.FindSection(s.cfg.OutputSection)
	return !exists, nil
}

// Run marshals the task, calls Claude with the step's prompt + tools,
// writes the LLM's output under the configured section heading.
func (s *agentStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	taskContent, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "%s marshal task", s.cfg.Name)
	}

	prompt := BuildPrompt(s.cfg.Instructions.String(), s.cfg.EnvContext, taskContent)

	result, runErr := s.cfg.Runner.Run(ctx, prompt)
	if runErr != nil {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: fmt.Sprintf("%s claude run failed: %v", s.cfg.Name, runErr),
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
