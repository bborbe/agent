// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"errors"

	"github.com/bborbe/collection"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
	"github.com/bborbe/agent/mocks"
)

var _ = Describe("StepRunner", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Run", func() {
		It("calls deliverer when step succeeds", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			step := &mocks.AgentStep{}
			step.NameReturns("test-step")
			step.ShouldRunReturns(true, nil)
			step.RunReturns(&lib.Result{Status: lib.AgentStatusDone, NextPhase: "next"}, nil)

			md := &lib.Markdown{}
			runner := lib.NewStepRunner(deliverer, step)

			result, err := runner.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(lib.AgentStatusDone))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
		})

		It("forwards ContinueToNext to the deliverer", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			step := &mocks.AgentStep{}
			step.NameReturns("preflight-step")
			step.ShouldRunReturns(true, nil)
			step.RunReturns(&lib.Result{
				Status:         lib.AgentStatusDone,
				ContinueToNext: true,
			}, nil)

			md := &lib.Markdown{}
			runner := lib.NewStepRunner(deliverer, step)

			_, err := runner.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, info := deliverer.DeliverResultArgsForCall(0)
			Expect(info.Status).To(Equal(lib.AgentStatusDone))
			Expect(info.NextPhase).To(Equal(""))
			Expect(info.ContinueToNext).To(BeTrue(),
				"deliverer must see ContinueToNext so Done+empty NextPhase preflight saves are distinguishable")
		})

		It("forwards the run's observed counts to the deliverer", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			step := &mocks.AgentStep{}
			step.NameReturns("counted-step")
			step.ShouldRunReturns(true, nil)
			step.RunReturns(&lib.Result{
				Status:           lib.AgentStatusDone,
				AgentTurns:       collection.Ptr(int64(7)),
				InteractionCount: collection.Ptr(int64(0)),
			}, nil)

			md := &lib.Markdown{}
			runner := lib.NewStepRunner(deliverer, step)

			_, err := runner.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, info := deliverer.DeliverResultArgsForCall(0)
			Expect(info.AgentTurns).NotTo(BeNil())
			Expect(*info.AgentTurns).To(Equal(int64(7)))
			Expect(info.InteractionCount).NotTo(BeNil())
			Expect(*info.InteractionCount).To(Equal(int64(0)))
		})

		It("forwards absence as absence", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			step := &mocks.AgentStep{}
			step.NameReturns("uncounted-step")
			step.ShouldRunReturns(true, nil)
			step.RunReturns(&lib.Result{Status: lib.AgentStatusDone}, nil)

			md := &lib.Markdown{}
			runner := lib.NewStepRunner(deliverer, step)

			_, err := runner.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, info := deliverer.DeliverResultArgsForCall(0)
			Expect(info.AgentTurns).To(BeNil())
			Expect(info.InteractionCount).To(BeNil())
		})

		It("returns error when step.Run returns error", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			step := &mocks.AgentStep{}
			step.NameReturns("failing-step")
			step.ShouldRunReturns(true, nil)
			step.RunReturns(nil, errors.New("step failed"))

			md := &lib.Markdown{}
			runner := lib.NewStepRunner(deliverer, step)

			_, err := runner.Run(ctx, md)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("failing-step"))
			Expect(err.Error()).To(ContainSubstring("step failed"))
		})

		It("returns early when ctx.Done fires", func() {
			deliverer := &mocks.AgentResultDeliverer{}

			cancelCtx, cancel := context.WithCancel(context.Background())
			cancel()

			step := &mocks.AgentStep{}
			step.NameReturns("test-step")
			step.ShouldRunReturns(true, nil)

			md := &lib.Markdown{}
			runner := lib.NewStepRunner(deliverer, step)

			_, err := runner.Run(cancelCtx, md)
			Expect(err).NotTo(BeNil())
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		})
	})
})
