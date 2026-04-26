// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/errors"
)

// StepRunner walks an ordered list of steps for a single phase invocation.
//
// On each iteration:
//  1. step.ShouldRun(markdown) — skip if false
//  2. step.Run(markdown) — mutates markdown in-place, returns Result
//  3. markdown.Marshal() → newContent
//  4. deliverer.DeliverResult(newContent, status, nextPhase)
//  5. Decides whether to continue based on Status, NextPhase, ContinueToNext
//
// Resume semantics: the runner does NOT track "current step" state. On
// re-invocation, all steps are re-walked; their ShouldRun guards skip
// completed work based on saved markdown state. Markdown state IS the
// resume cursor.
type StepRunner struct {
	steps     []Step
	deliverer ResultDeliverer
}

// NewStepRunner constructs a StepRunner with the given step list and deliverer.
func NewStepRunner(deliverer ResultDeliverer, steps ...Step) *StepRunner {
	return &StepRunner{steps: steps, deliverer: deliverer}
}

// Run walks the step list, calling guards, running, marshaling, and saving.
// Returns the last delivered Result, or nil if no step executed.
func (r *StepRunner) Run(ctx context.Context, md *Markdown) (*Result, error) {
	var lastResult *Result

	for _, s := range r.steps {
		shouldRun, err := s.ShouldRun(ctx, md)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "step %q ShouldRun", s.Name())
		}
		if !shouldRun {
			continue
		}

		result, err := s.Run(ctx, md)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "step %q Run", s.Name())
		}
		if result == nil {
			return nil, errors.Errorf(ctx, "step %q returned nil result", s.Name())
		}

		// Re-serialize the (possibly mutated) markdown and publish.
		newContent, err := md.Marshal(ctx)
		if err != nil {
			return result, errors.Wrapf(ctx, err, "step %q marshal markdown", s.Name())
		}

		if err := r.deliverer.DeliverResult(ctx, AgentResultInfo{
			Status:    result.Status,
			Output:    newContent,
			Message:   result.Message,
			NextPhase: result.NextPhase,
		}); err != nil {
			return result, errors.Wrapf(ctx, err, "step %q deliver", s.Name())
		}

		lastResult = result

		if shouldExitStepRunner(*result) {
			break
		}
	}

	return lastResult, nil
}

// shouldExitStepRunner returns true if the StepRunner should stop after
// delivering result.
func shouldExitStepRunner(r Result) bool {
	switch r.Status {
	case AgentStatusFailed, AgentStatusNeedsInput:
		return true
	case AgentStatusDone:
		// Done with NextPhase advances — exit Job, controller spawns next phase.
		// Done without NextPhase is in-place save; continue iff ContinueToNext.
		if r.NextPhase != "" {
			return true
		}
		return !r.ContinueToNext
	case AgentStatusInProgress:
		// In-place save: continue iff explicitly requested.
		return !r.ContinueToNext
	}
	return true
}
