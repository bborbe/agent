// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"

	agentlib "github.com/bborbe/agent"
)

// NewNopStep returns a Step that immediately returns done with output "ok".
// No external calls — reaching this step proves the binary booted and the
// framework wired the phase. Used by pure-Go agent binaries.
func NewNopStep() agentlib.Step {
	return &nopStep{}
}

type nopStep struct{}

func (s *nopStep) Name() string { return "healthcheck-nop" }

func (s *nopStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
	return true, nil
}

func (s *nopStep) Run(_ context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
	return &agentlib.Result{
		Status: agentlib.AgentStatusDone,
		// Explicit terminal phase: Done with empty NextPhase is an in-place save
		// (stay in current phase) — healthcheck tasks must actually complete.
		NextPhase: "done",
		Message:   "ok",
	}, nil
}
