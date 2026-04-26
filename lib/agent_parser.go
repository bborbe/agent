// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/errors"
)

//counterfeiter:generate -o mocks/agent-ai-parser.go --fake-name AgentAIParser . AIParser

// AIParser is the boundary translator: fuzzy markdown → typed Go struct.
//
// Concrete implementations wrap Gemini structured output, Claude with
// JSON mode, or any other LLM that produces structured outputs. They
// derive a JSON schema from the target type and instruct the LLM to
// emit conforming output.
//
// Concrete impls live alongside their AI provider (e.g. lib/gemini for
// Gemini-backed parser). The interface lives here so framework code
// (ParseStep) can compose any provider without coupling.
type AIParser interface {
	// Parse reads taskContent (markdown) and populates target (a pointer
	// to a typed struct).
	Parse(ctx context.Context, taskContent string, target any) error
}

// ParseStep wraps an AIParser as a Step.
//
// Boundary translator: markdown → typed Go struct → ## Section JSON.
// Use this for the planning phase of code-driven agents that take fuzzy
// human-written tasks.
type ParseStep[T any] struct {
	name      string
	parser    AIParser
	heading   string
	nextPhase string
}

// NewParseStep constructs a ParseStep[T].
//
// name:      step name for logs (e.g. "parse-plan")
// parser:    the AI parser implementation (Gemini structured output, etc.)
// heading:   the body section to write the typed result to (e.g. "## Plan")
// nextPhase: the phase to advance to on success
func NewParseStep[T any](
	name string,
	parser AIParser,
	heading string,
	nextPhase string,
) *ParseStep[T] {
	return &ParseStep[T]{
		name:      name,
		parser:    parser,
		heading:   heading,
		nextPhase: nextPhase,
	}
}

// Name implements Step.
func (s *ParseStep[T]) Name() string { return s.name }

// ShouldRun returns false if the target section already exists.
func (s *ParseStep[T]) ShouldRun(_ context.Context, md *Markdown) (bool, error) {
	_, exists := md.FindSection(s.heading)
	return !exists, nil
}

// Run invokes the parser, marshals the typed result as a Section, returns Result.
func (s *ParseStep[T]) Run(ctx context.Context, md *Markdown) (*Result, error) {
	taskContent, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "%s marshal task for parse", s.name)
	}

	var target T
	if parseErr := s.parser.Parse(ctx, taskContent, &target); parseErr != nil {
		return &Result{
			Status:  AgentStatusNeedsInput,
			Message: errors.Wrapf(ctx, parseErr, "parse %s", s.heading).Error(),
		}, nil
	}

	section, err := MarshalSectionTyped(ctx, s.heading, target)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "marshal %s", s.heading)
	}
	md.ReplaceSection(section)

	return &Result{
		Status:    AgentStatusDone,
		NextPhase: s.nextPhase,
	}, nil
}
