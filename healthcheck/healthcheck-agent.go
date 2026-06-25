// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck

import (
	agentlib "github.com/bborbe/agent"
)

// NewAgent wraps any Step in a phase-agnostic *agentlib.Agent.
// The step is registered under all three canonical phase names so the
// healthcheck task succeeds regardless of which PHASE env the executor
// injects (planning, execution, ai_review per CLAUDE.md doctrine).
func NewAgent(step agentlib.Step) *agentlib.Agent {
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("execution", step),
		agentlib.NewPhase("ai_review", step),
	)
}
