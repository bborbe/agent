// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"errors"

	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("Agent.Run", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("phase loop", func() {
		It("runs A then B then C in one call", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)
			stepB.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "C"}, nil)

			stepC := &mocks.AgentStep{}
			stepC.NameReturns("step-c")
			stepC.ShouldRunReturns(true, nil)
			stepC.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "done"}, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
				lib.NewPhase(domain.TaskPhase("C"), stepC),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("done"))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(3))

			_, info0 := deliverer.DeliverResultArgsForCall(0)
			Expect(info0.NextPhase).To(Equal("B"))

			_, info1 := deliverer.DeliverResultArgsForCall(1)
			Expect(info1.NextPhase).To(Equal("C"))

			_, info2 := deliverer.DeliverResultArgsForCall(2)
			Expect(info2.NextPhase).To(Equal("done"))

			Expect(stepA.RunCallCount()).To(Equal(1))
			Expect(stepB.RunCallCount()).To(Equal(1))
			Expect(stepC.RunCallCount()).To(Equal(1))
		})

		It("cancels between phases", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunStub = func(_ context.Context, _ *lib.Markdown) (*lib.Result, error) {
				return &lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil
			}

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)

			cancelCtx, cancel := context.WithCancel(context.Background())
			// Cancel after step A but before the between-iterations check.
			// We hook it into step A's stub via a separate goroutine.
			go func() {
				cancel()
			}()

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
			)

			_, err := agent.Run(cancelCtx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).NotTo(BeNil())
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
			Expect(stepB.RunCallCount()).To(Equal(0))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
		})

		It("stops when NextPhase is unknown to this agent", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)
			stepB.RunReturns(
				&lib.Result{Status: lib.AgentStatusDone, NextPhase: "unknown-to-this-agent"},
				nil,
			)

			stepC := &mocks.AgentStep{}
			stepC.NameReturns("step-c")
			stepC.ShouldRunReturns(true, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
				// C is intentionally NOT registered — "unknown-to-this-agent" will not be found
				lib.NewPhase(domain.TaskPhase("C"), stepC),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.NextPhase).To(Equal("unknown-to-this-agent"))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(2))
			Expect(stepA.RunCallCount()).To(Equal(1))
			Expect(stepB.RunCallCount()).To(Equal(1))
			Expect(stepC.RunCallCount()).To(Equal(0))
		})

		It("stops when NextPhase is human_review", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)
			stepB.RunReturns(
				&lib.Result{Status: lib.AgentStatusDone, NextPhase: "human_review"},
				nil,
			)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.NextPhase).To(Equal("human_review"))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(2))
		})

		It("does not panic when middle phase has no steps", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil)

			stepC := &mocks.AgentStep{}
			stepC.NameReturns("step-c")
			stepC.ShouldRunReturns(true, nil)
			stepC.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "done"}, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B")), // empty — no steps
				lib.NewPhase(domain.TaskPhase("C"), stepC),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.NextPhase).To(Equal("B"))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			Expect(stepC.RunCallCount()).To(Equal(0))
		})

		It("stops loop when deliverer publish fails", func() {
			deliverer := &mocks.AgentResultDeliverer{}
			deliverer.DeliverResultReturnsOnCall(1, errors.New("kafka down"))

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "B"}, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)
			stepB.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "C"}, nil)

			stepC := &mocks.AgentStep{}
			stepC.NameReturns("step-c")
			stepC.ShouldRunReturns(true, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
				lib.NewPhase(domain.TaskPhase("C"), stepC),
			)

			_, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("kafka down"))
			Expect(stepC.RunCallCount()).To(Equal(0))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(2))
		})

		It("stops loop on Failed status", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusFailed, NextPhase: ""}, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusFailed))
			Expect(stepB.RunCallCount()).To(Equal(0))
		})

		It("stops loop on NeedsInput status", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)
			stepA.RunReturns(&lib.Result{Status: lib.AgentStatusNeedsInput, NextPhase: ""}, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("A"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusNeedsInput))
			Expect(stepB.RunCallCount()).To(Equal(0))
		})

		It("routes unsupported initial phase through unsupportedPhase", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			stepA := &mocks.AgentStep{}
			stepA.NameReturns("step-a")
			stepA.ShouldRunReturns(true, nil)

			stepB := &mocks.AgentStep{}
			stepB.NameReturns("step-b")
			stepB.ShouldRunReturns(true, nil)

			agent := lib.NewAgent(
				lib.NewPhase(domain.TaskPhase("A"), stepA),
				lib.NewPhase(domain.TaskPhase("B"), stepB),
			)

			result, err := agent.Run(ctx, domain.TaskPhase("Z"), "# Task\n", deliverer)

			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusFailed))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			Expect(stepA.RunCallCount()).To(Equal(0))
			Expect(stepB.RunCallCount()).To(Equal(0))
		})
	})
})
