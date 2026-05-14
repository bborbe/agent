// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck

import (
	agentlib "github.com/bborbe/agent/lib"
)

// NewAgent wraps any Step in a phase-agnostic *agentlib.Agent.
// The step is registered under all three phase names so the healthcheck
// task succeeds regardless of which PHASE env the executor injects.
func NewAgent(step agentlib.Step) *agentlib.Agent {
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("in_progress", step),
		agentlib.NewPhase("ai_review", step),
	)
}
