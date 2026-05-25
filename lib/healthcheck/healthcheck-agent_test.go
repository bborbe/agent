// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck_test

import (
	"context"

	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/healthcheck"
	"github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("NewAgent", func() {
	var (
		ctx       context.Context
		fakeStep  *mocks.AgentStep
		agent     *agentlib.Agent
		deliverer *mocks.AgentResultDeliverer
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeStep = &mocks.AgentStep{}
		fakeStep.ShouldRunReturns(true, nil)
		fakeStep.RunReturns(&agentlib.Result{Status: agentlib.AgentStatusDone}, nil)
		agent = healthcheck.NewAgent(fakeStep)
		deliverer = &mocks.AgentResultDeliverer{}
	})

	It("returns a non-nil agent", func() {
		Expect(agent).NotTo(BeNil())
	})

	DescribeTable("dispatches to the wrapped step for each phase",
		func(phase domain.TaskPhase) {
			before := fakeStep.RunCallCount()
			result, err := agent.Run(ctx, phase, "# Task\n\ncontent\n", deliverer)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(fakeStep.RunCallCount()).To(Equal(before+1),
				"the same wrapped step must be reached for each phase — proves single-step is registered under all three phase names")
		},
		Entry("planning phase", domain.TaskPhase("planning")),
		Entry("execution phase", domain.TaskPhase("execution")),
		Entry("ai_review phase", domain.TaskPhase("ai_review")),
	)

	It("invokes the step once per agent.Run call", func() {
		_, _ = agent.Run(ctx, domain.TaskPhase("planning"), "# Task\n\ncontent\n", deliverer)
		Expect(fakeStep.RunCallCount()).To(Equal(1))
	})
})
