// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steps holds the per-phase step impls for the agent-code reference.
//
// The reference agent is a math agent: read operation + operands from
// frontmatter, compute, verify. Three single-step phases, each step is
// pure Go with no AI calls. This proves the framework works end-to-end
// without any LLM dependency — useful template for orchestration agents,
// data agents, validation agents.
//
// Replace the math operation with your domain logic; the framework
// scaffolding (PlanStep / ExecuteStep / VerifyStep) is the part to copy.
package steps

import (
	"context"
	"fmt"

	"github.com/bborbe/errors"

	agentlib "github.com/bborbe/agent/lib"
)

// Plan is the planning-phase output. Read by execute step.
type Plan struct {
	Operation string `json:"operation"`
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

// PlanStep extracts operation + operands from frontmatter, writes ## Plan.
type PlanStep struct{}

// NewPlanStep constructs a PlanStep.
func NewPlanStep() *PlanStep { return &PlanStep{} }

// Name implements agentlib.Step.
func (s *PlanStep) Name() string { return "plan" }

// ShouldRun returns false if ## Plan already exists.
func (s *PlanStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
	_, exists := md.FindSection("## Plan")
	return !exists, nil
}

// Run reads operation + a + b from frontmatter, writes typed Plan.
func (s *PlanStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	op, ok := md.Frontmatter["operation"].(string)
	if !ok || op == "" {
		return needsInput("frontmatter missing 'operation' field")
	}
	a, aOK := readInt(md.Frontmatter, "a")
	b, bOK := readInt(md.Frontmatter, "b")
	if !aOK || !bOK {
		return needsInput("frontmatter missing or invalid 'a' / 'b' (must be integers)")
	}

	plan := Plan{Operation: op, A: a, B: b}
	section, err := agentlib.MarshalSectionTyped(ctx, "## Plan", plan)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "marshal plan")
	}
	md.ReplaceSection(section)

	return &agentlib.Result{
		Status:    agentlib.AgentStatusDone,
		NextPhase: "in_progress",
	}, nil
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
//
// Demonstrates that the "ai_review" phase doesn't have to use AI — pure
// code verifying typed data is a perfectly valid review pattern.
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

// readInt reads an integer field from a YAML-parsed frontmatter map.
func readInt(fm agentlib.TaskFrontmatter, key string) (int, bool) {
	v, ok := fm[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// needsInput is a small helper for needs_input results.
func needsInput(msg string) (*agentlib.Result, error) {
	return &agentlib.Result{
		Status:  agentlib.AgentStatusNeedsInput,
		Message: msg,
	}, nil
}
