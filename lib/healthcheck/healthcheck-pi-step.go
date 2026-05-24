// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"
	"strings"

	"github.com/bborbe/errors"

	agentlib "github.com/bborbe/agent/lib"
	pilib "github.com/bborbe/agent/lib/pi"
)

// NewPiStep returns a Step that runs a "reply 'ok'" smoke prompt via the
// configured Pi CLI runner. Used by the agent-pi binary to verify
// that its Pi CLI dependency is reachable.
func NewPiStep(runner pilib.Runner) agentlib.Step {
	return &piStep{runner: runner}
}

type piStep struct {
	runner pilib.Runner
}

func (s *piStep) Name() string { return "healthcheck-pi" }

func (s *piStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
	return true, nil
}

func (s *piStep) Run(ctx context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
	result, err := s.runner.Run(ctx, "reply 'ok'")
	if err != nil {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: errors.Wrapf(ctx, err, "healthcheck-pi run failed").Error(),
		}, nil
	}
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: "healthcheck-pi reply empty",
		}, nil
	}
	return &agentlib.Result{
		Status:  agentlib.AgentStatusDone,
		Message: trimmed,
	}, nil
}
