// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/agent/gemini/pkg/factory"
	agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("CreateAgentProvider", func() {
	var (
		ctx      context.Context
		provider agentlib.AgentProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		// nil parser is safe — NewGeminiStep stores the parser without invoking it.
		provider = factory.CreateAgentProvider(nil)
	})

	It("returns a non-nil provider", func() {
		Expect(provider).NotTo(BeNil())
	})

	It("Get returns the liveness agent for TaskTypeHealthcheck", func() {
		agent, err := provider.Get(ctx, agentlib.TaskTypeHealthcheck)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	Describe("Get with unknown task_type", func() {
		DescribeTable(
			"error shape",
			func(taskType agentlib.TaskType, expectedSubstr string) {
				agent, err := provider.Get(ctx, taskType)
				Expect(agent).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unknown task_type"))
				Expect(err.Error()).To(ContainSubstring(expectedSubstr))
				Expect(err.Error()).To(ContainSubstring("agent-gemini"))
				Expect(err.Error()).To(ContainSubstring("[healthcheck]"))
			},
			Entry(
				"literal gemini rejected (no implicit domain type)",
				agentlib.TaskType("gemini"),
				`"gemini"`,
			),
			Entry("bogus value", agentlib.TaskType("bogus"), `"bogus"`),
			Entry("empty value", agentlib.TaskType(""), `""`),
		)
	})
})
