// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"
	"fmt"

	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
)

// Agent is a composed list of phases. Build via NewAgent.
type Agent struct {
	phases []Phase
}

// NewAgent constructs an Agent from one or more phases.
//
// Phase names must be unique. Duplicates are rejected at Run time.
func NewAgent(phases ...Phase) *Agent {
	return &Agent{phases: phases}
}

// Run dispatches by phase and walks the matching step list.
//
// phaseName is the requested phase from the K8s Job env (PHASE) or the
// CLI flag. Unknown or empty phaseName produces a Failed result via the
// deliverer (fail-loud sentinel — never a silent escalation).
//
// taskContent is parsed once into *Markdown; the parsed Markdown is
// mutated by successive steps and re-serialized for each save.
func (a *Agent) Run(
	ctx context.Context,
	phaseName domain.TaskPhase,
	taskContent string,
	deliverer ResultDeliverer,
) (*Result, error) {
	if err := a.validate(ctx); err != nil {
		return nil, err
	}

	p, ok := a.findPhase(phaseName)
	if !ok {
		return a.unsupportedPhase(ctx, phaseName, deliverer)
	}

	md, err := ParseMarkdown(ctx, taskContent)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "parse markdown")
	}

	runner := NewStepRunner(deliverer, p.Steps...)
	return runner.Run(ctx, md)
}

// validate checks for duplicate phase names.
func (a *Agent) validate(ctx context.Context) error {
	seen := map[domain.TaskPhase]bool{}
	for _, p := range a.phases {
		if seen[p.Name] {
			return errors.Errorf(ctx, "agent: duplicate phase %q", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

func (a *Agent) findPhase(name domain.TaskPhase) (Phase, bool) {
	for _, p := range a.phases {
		if p.Name == name {
			return p, true
		}
	}
	return Phase{}, false
}

// unsupportedPhase publishes a Failed result with a clear message.
func (a *Agent) unsupportedPhase(
	ctx context.Context,
	phaseName domain.TaskPhase,
	deliverer ResultDeliverer,
) (*Result, error) {
	display := string(phaseName)
	if display == "" {
		display = "(empty)"
	}
	result := &Result{
		Status:  AgentStatusFailed,
		Message: fmt.Sprintf("unsupported entry phase: %s", display),
	}
	if err := deliverer.DeliverResult(ctx, AgentResultInfo{
		Status:  result.Status,
		Message: result.Message,
	}); err != nil {
		return result, errors.Wrapf(ctx, err, "deliver unsupported-phase")
	}
	return result, nil
}
