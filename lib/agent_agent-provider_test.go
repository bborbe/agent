// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("AgentProvider", func() {
	var (
		ctx      context.Context
		domain   *lib.Agent
		liveness *lib.Agent
	)

	BeforeEach(func() {
		ctx = context.Background()
		domain = lib.NewAgent()
		liveness = lib.NewAgent()
	})

	Describe("Get", func() {
		It("returns the agent registered for the given task_type", func() {
			provider := lib.NewAgentProvider("test-binary", map[lib.TaskType]*lib.Agent{
				lib.TaskTypeLLM:         domain,
				lib.TaskTypeHealthcheck: liveness,
			})
			result, err := provider.Get(ctx, lib.TaskTypeLLM)
			Expect(err).To(BeNil())
			Expect(result).To(BeIdenticalTo(domain))
		})

		It("returns a different agent for a different task_type from the same provider", func() {
			provider := lib.NewAgentProvider("test-binary", map[lib.TaskType]*lib.Agent{
				lib.TaskTypeLLM:         domain,
				lib.TaskTypeHealthcheck: liveness,
			})
			result, err := provider.Get(ctx, lib.TaskTypeHealthcheck)
			Expect(err).To(BeNil())
			Expect(result).To(BeIdenticalTo(liveness))
		})

		Describe("miss path", func() {
			var provider lib.AgentProvider

			BeforeEach(func() {
				provider = lib.NewAgentProvider("test-binary", map[lib.TaskType]*lib.Agent{
					lib.TaskTypeLLM:         domain,
					lib.TaskTypeHealthcheck: liveness,
				})
			})

			It("returns nil agent on unknown task_type", func() {
				result, err := provider.Get(ctx, lib.TaskType("bogus"))
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			DescribeTable("error message contents",
				func(matcher types.GomegaMatcher) {
					_, err := provider.Get(ctx, lib.TaskType("bogus"))
					Expect(err.Error()).To(matcher)
				},
				Entry("literal 'unknown task_type'", ContainSubstring("unknown task_type")),
				Entry("offending value quoted", ContainSubstring(`"bogus"`)),
				Entry("provider name", ContainSubstring("test-binary")),
				Entry("accepted list contains llm", ContainSubstring("llm")),
				Entry("accepted list contains healthcheck", ContainSubstring("healthcheck")),
			)

			It("returns accepted-types list sorted alphabetically (deterministic)", func() {
				_, err := provider.Get(ctx, lib.TaskType("bogus"))
				Expect(err.Error()).To(ContainSubstring("[healthcheck llm]"))
			})
		})

		It("returns nil agent and an error when the map is empty", func() {
			provider := lib.NewAgentProvider("empty-binary", map[lib.TaskType]*lib.Agent{})
			result, err := provider.Get(ctx, lib.TaskTypeHealthcheck)
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("empty-binary"))
			Expect(err.Error()).To(ContainSubstring("accepted: []"))
		})
	})
})
