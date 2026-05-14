// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck_test

import (
	"context"
	stderrors "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/claude"
	"github.com/bborbe/agent/lib/healthcheck"
	"github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("NewClaudeStep", func() {
	var (
		ctx        context.Context
		fakeRunner *mocks.ClaudeRunner
		step       agentlib.Step
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeRunner = &mocks.ClaudeRunner{}
		step = healthcheck.NewClaudeStep(fakeRunner)
	})

	Describe("Name", func() {
		It("returns healthcheck-claude", func() {
			Expect(step.Name()).To(Equal("healthcheck-claude"))
		})
	})

	Describe("ShouldRun", func() {
		It("always returns true", func() {
			ok, err := step.ShouldRun(ctx, nil)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})
	})

	Describe("Run", func() {
		It("returns done when the runner returns a non-empty result", func() {
			fakeRunner.RunReturns(&claude.ClaudeResult{Result: "ok"}, nil)
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.Message).To(Equal("ok"))
		})

		It("returns done and trims whitespace from the result", func() {
			fakeRunner.RunReturns(&claude.ClaudeResult{Result: "  ok  "}, nil)
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.Message).To(Equal("ok"))
		})

		It("returns failed when the runner returns an error", func() {
			fakeRunner.RunReturns(nil, stderrors.New("cli failed"))
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
			Expect(result.Message).To(ContainSubstring("healthcheck-claude run failed"))
		})

		It("returns failed when the runner returns an empty result", func() {
			fakeRunner.RunReturns(&claude.ClaudeResult{Result: ""}, nil)
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
			Expect(result.Message).To(ContainSubstring("reply empty"))
		})
	})
})
