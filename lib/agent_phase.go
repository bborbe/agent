// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "github.com/bborbe/vault-cli/pkg/domain"

// Phase ties a phase name to an ordered list of steps.
//
// Compose with NewAgent:
//
//	NewAgent(
//	    NewPhase("planning",   NewParseStep(parser, "## Plan", "in_progress")),
//	    NewPhase("in_progress", NewExecuteStep(runner, fetcher)),
//	    NewPhase("ai_review",  NewVerifyStep(checker)),
//	)
type Phase struct {
	Name  domain.TaskPhase
	Steps []Step
}

// NewPhase constructs a Phase. Variadic steps for ergonomics.
func NewPhase(name domain.TaskPhase, steps ...Step) Phase {
	return Phase{Name: name, Steps: steps}
}
