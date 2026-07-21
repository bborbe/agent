// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"

	"github.com/bborbe/errors"

	agentlib "github.com/bborbe/agent"
)

// replyTarget is the parse target for the Gemini healthcheck prompt.
// Kept unexported — only the step function is part of the public API.
type replyTarget struct {
	OK string `json:"ok"`
}

// NewGeminiStep returns a Step that calls the AIParser with a "reply 'ok'"
// prompt. Used by the agent-gemini binary to verify Gemini API reachability.
//
// On success the Result.Message is populated with the reply text —
// this is intentional to capture the liveness response for operator log/audit.
func NewGeminiStep(parser agentlib.AIParser) agentlib.Step {
	return &geminiStep{parser: parser}
}

type geminiStep struct {
	parser agentlib.AIParser
}

func (s *geminiStep) Name() string { return "healthcheck-gemini" }

func (s *geminiStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
	return true, nil
}

func (s *geminiStep) Run(ctx context.Context, _ *agentlib.Markdown) (*agentlib.Result, error) {
	var reply replyTarget
	if err := s.parser.Parse(ctx, "reply 'ok'", &reply); err != nil {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: errors.Wrapf(ctx, err, "healthcheck-gemini parse failed").Error(),
		}, nil
	}
	if reply.OK == "" {
		return &agentlib.Result{
			Status:  agentlib.AgentStatusFailed,
			Message: "gemini healthcheck reply empty",
		}, nil
	}
	return &agentlib.Result{
		Status: agentlib.AgentStatusDone,
		// Explicit terminal phase: Done with empty NextPhase is an in-place save
		// (stay in current phase) — healthcheck tasks must actually complete.
		NextPhase: "done",
		Message:   reply.OK,
	}, nil
}
