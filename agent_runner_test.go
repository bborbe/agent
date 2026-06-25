// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent"
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
