// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steps holds the per-phase step impls for the agent-gemini reference.
//
// Boundary-translator agent: takes fuzzy human-written markdown (e.g.
// "Compute 3 plus 5"), Gemini parses it into a typed Plan struct, then
// pure-Go steps compute and verify. Demonstrates the canonical AI usage
// — only the boundary needs an LLM, the rest is deterministic code.
//
// The planning step itself is wired in main.go via lib.NewParseStep[Plan]
// — no PlanStep type lives here. ExecuteStep + VerifyStep are pure Go.
package steps

import (
	"context"
	"fmt"

	"github.com/bborbe/errors"

	agentlib "github.com/bborbe/agent/lib"
)

// Plan is the planning-phase output. Populated by Gemini from fuzzy
// markdown input. Read by execute step.
type Plan struct {
	Operation string `json:"operation"` // "add" | "sub" | "mul"
	A         int    `json:"a"`
	B         int    `json:"b"`
}

// Result is the execute-phase output. Read by verify step.
type Result struct {
	Operation string `json:"operation"`
	Value     int    `json:"value"`
}

// Review is the verify-phase output. Terminal — read only by humans.
type Review struct {
	Verdict string `json:"verdict"` // "pass" | "fail"
	Reason  string `json:"reason,omitempty"`
}

// ExecuteStep reads ## Plan, computes the result, writes ## Result.
type ExecuteStep struct{}

// NewExecuteStep constructs an ExecuteStep.
func NewExecuteStep() *ExecuteStep { return &ExecuteStep{} }

// Name implements agentlib.Step.
func (s *ExecuteStep) Name() string { return "execute" }

// ShouldRun returns false if ## Result already exists.
func (s *ExecuteStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
	_, exists := md.FindSection("## Result")
	return !exists, nil
}

// Run extracts ## Plan, performs the operation, writes typed ## Result.
func (s *ExecuteStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	plan, err := agentlib.ExtractSection[Plan](ctx, md, "## Plan")
	if err != nil {
		return needsInput(err.Error())
	}

	value, err := compute(ctx, plan.Operation, plan.A, plan.B)
	if err != nil {
		return needsInput(err.Error())
	}

	result := Result{Operation: plan.Operation, Value: value}
	section, err := agentlib.MarshalSectionTyped(ctx, "## Result", result)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "marshal result")
	}
	md.ReplaceSection(section)

	return &agentlib.Result{
		Status:    agentlib.AgentStatusDone,
		NextPhase: "ai_review",
	}, nil
}

// VerifyStep reads ## Plan + ## Result, recomputes, writes ## Review verdict.
type VerifyStep struct{}

// NewVerifyStep constructs a VerifyStep.
func NewVerifyStep() *VerifyStep { return &VerifyStep{} }

// Name implements agentlib.Step.
func (s *VerifyStep) Name() string { return "verify" }

// ShouldRun always runs — final-phase verification is idempotent.
func (s *VerifyStep) ShouldRun(_ context.Context, _ *agentlib.Markdown) (bool, error) {
	return true, nil
}

// Run reads ## Plan + ## Result, recomputes the expected value, asserts equality.
func (s *VerifyStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	plan, err := agentlib.ExtractSection[Plan](ctx, md, "## Plan")
	if err != nil {
		return needsInput(err.Error())
	}
	result, err := agentlib.ExtractSection[Result](ctx, md, "## Result")
	if err != nil {
		return needsInput(err.Error())
	}

	expected, err := compute(ctx, plan.Operation, plan.A, plan.B)
	if err != nil {
		return needsInput(err.Error())
	}

	review := Review{Verdict: "pass"}
	if result.Value != expected {
		review.Verdict = "fail"
		review.Reason = fmt.Sprintf("expected %d, got %d", expected, result.Value)
	}

	section, err := agentlib.MarshalSectionTyped(ctx, "## Review", review)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "marshal review")
	}
	md.ReplaceSection(section)

	nextPhase := "done"
	if review.Verdict == "fail" {
		nextPhase = "human_review"
	}
	return &agentlib.Result{
		Status:    agentlib.AgentStatusDone,
		NextPhase: nextPhase,
		Message:   review.Reason,
	}, nil
}

// compute performs the arithmetic for ExecuteStep + VerifyStep.
func compute(ctx context.Context, op string, a, b int) (int, error) {
	switch op {
	case "add":
		return a + b, nil
	case "sub":
		return a - b, nil
	case "mul":
		return a * b, nil
	default:
		return 0, errors.Errorf(ctx, "unknown operation %q (expected add | sub | mul)", op)
	}
}

// needsInput is a small helper for needs_input results.
func needsInput(msg string) (*agentlib.Result, error) {
	return &agentlib.Result{
		Status:  agentlib.AgentStatusNeedsInput,
		Message: msg,
	}, nil
}
