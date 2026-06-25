// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "context"

//counterfeiter:generate -o mocks/agent-step.go --fake-name AgentStep . Step

// Step is one unit of work within a phase. Always code; may wrap AI calls.
//
// Three responsibilities:
//   - Name: identifies the step in logs and tests.
//   - ShouldRun: cheap guard that inspects task state to decide skip vs run.
//   - Run: performs work, mutating markdown in-place. Returns a Result
//     describing status + phase transition (NOT body content — body changes
//     happen via markdown.AddSection / ReplaceSection / Frontmatter mutation).
type Step interface {
	// Name identifies the step. Convention: lower-kebab-case.
	Name() string

	// ShouldRun returns true if the step should execute. Inspects markdown
	// state (frontmatter, sections) and returns false if the step has
	// already completed (idempotency guard).
	//
	// Guards must be cheap — no expensive I/O. Use existing markdown state.
	ShouldRun(ctx context.Context, md *Markdown) (bool, error)

	// Run performs the step's work, mutating markdown in-place. Returns a
	// Result describing status + phase transition. Body content changes
	// flow through markdown.AddSection / ReplaceSection; frontmatter
	// changes flow through direct map mutation.
	//
	// The framework re-serializes markdown via Marshal after Run returns
	// and publishes the new content via the deliverer.
	Run(ctx context.Context, md *Markdown) (*Result, error)
}

// Result tells the StepRunner what status to deliver and whether to advance.
//
// Body and frontmatter changes are NOT in Result — they're applied by
// mutating *Markdown in Run. This keeps the durability model clear: at any
// point during Run, the Markdown IS the durable view.
type Result struct {
	// Status: Done | InProgress | Failed | NeedsInput.
	Status AgentStatus

	// NextPhase advances the phase frontmatter on Status: Done. Empty
	// means "stay in current phase" — used for in-place saves between
	// steps in a multi-step phase.
	NextPhase string

	// Message is a human-readable status. Required for Failed/NeedsInput.
	Message string

	// ContinueToNext signals whether the StepRunner should proceed to
	// the next step in the same Job invocation (true) or exit and let
	// the controller re-trigger the same phase (false).
	//
	// Default is exit-after-save. Multi-step phases set this to true on
	// intermediate steps and let the last step decide.
	ContinueToNext bool
}
