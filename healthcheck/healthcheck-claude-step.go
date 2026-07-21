// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"
	"strings"

	"github.com/bborbe/errors"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// NewClaudeStep returns a Step that runs a "reply 'ok'" smoke prompt via the
// configured Claude CLI runner. Used by the agent-claude binary to verify
// that its Claude CLI dependency is reachable.
//
// On success the Result.Message is populated with the trimmed reply text —
// this is intentional to capture the liveness response for operator log/audit.
func NewClaudeStep(runner claudelib.ClaudeRunner) agentlib.Step {
	return &claudeStep{runner: runner}
}

type claudeStep struct {
	runner claudelib.ClaudeRunner
}

func (s *claudeStep) Name() string { return "healthcheck-claude" }

func (s *claudeStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
	return true, nil
}

func (s *claudeStep) Run(ctx context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
	result, err := s.runner.Run(ctx, "reply 'ok'")
	if err != nil {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: errors.Wrapf(ctx, err, "healthcheck-claude run failed").Error(),
		}, nil
	}
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: "healthcheck-claude reply empty",
		}, nil
	}
	return &agentlib.Result{
		Status: agentlib.AgentStatusDone,
		// Explicit terminal phase: Done with empty NextPhase is an in-place save
		// (stay in current phase) — healthcheck tasks must actually complete.
		NextPhase: "done",
		Message:   trimmed,
	}, nil
}
